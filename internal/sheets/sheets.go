// Package sheets mirrors customer and order data to Google Sheets.
//
// It talks to the Sheets v4 REST API directly and authenticates with a
// service-account key (the same credentials.json the Python bot used), so
// no Google SDK is needed.
package sheets

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	apiBase = "https://sheets.googleapis.com/v4/spreadsheets/"
	scope   = "https://www.googleapis.com/auth/spreadsheets"
)

// Client is a minimal Sheets API client. A nil *Client is valid and turns
// every mirroring call into a no-op, so callers do not need to check whether
// Sheets is configured.
type Client struct {
	spreadsheetID string
	email         string
	tokenURI      string
	key           *rsa.PrivateKey
	http          *http.Client

	mu     sync.Mutex
	token  string
	expiry time.Time
}

type serviceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// New loads the service-account key from credentialsPath.
func New(credentialsPath, spreadsheetID string) (*Client, error) {
	raw, err := os.ReadFile(credentialsPath)
	if err != nil {
		return nil, fmt.Errorf("baca credentials: %w", err)
	}
	var sa serviceAccount
	if err := json.Unmarshal(raw, &sa); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return nil, errors.New("private_key di credentials tidak valid")
	}
	var key *rsa.PrivateKey
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rk, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("private_key bukan RSA")
		}
		key = rk
	} else if rk, err2 := x509.ParsePKCS1PrivateKey(block.Bytes); err2 == nil {
		key = rk
	} else {
		return nil, fmt.Errorf("parse private_key: %w", err)
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return &Client{
		spreadsheetID: spreadsheetID,
		email:         sa.ClientEmail,
		tokenURI:      sa.TokenURI,
		key:           key,
		http:          &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func (c *Client) accessToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Until(c.expiry) > time.Minute {
		return c.token, nil
	}
	now := time.Now()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{
		"iss":   c.email,
		"scope": scope,
		"aud":   c.tokenURI,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	})
	unsigned := b64(header) + "." + b64(claims)
	sum := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, c.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {unsigned + "." + b64(sig)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token google: %s: %s", resp.Status, body)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", err
	}
	c.token = tok.AccessToken
	c.expiry = now.Add(time.Duration(tok.ExpiresIn) * time.Second)
	return c.token, nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	tok, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	u := apiBase + url.PathEscape(c.spreadsheetID) + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("sheets %s %s: %s: %s", method, path, resp.Status, raw)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func rangePath(rng string) string { return "/values/" + url.PathEscape(rng) }

// getValues returns cell values (unformatted: numbers stay numbers).
func (c *Client) getValues(ctx context.Context, rng string) ([][]any, error) {
	var res struct {
		Values [][]any `json:"values"`
	}
	q := url.Values{"valueRenderOption": {"UNFORMATTED_VALUE"}}
	err := c.do(ctx, http.MethodGet, rangePath(rng), q, nil, &res)
	return res.Values, err
}

func (c *Client) updateValues(ctx context.Context, rng string, values [][]any) error {
	q := url.Values{"valueInputOption": {"RAW"}}
	return c.do(ctx, http.MethodPut, rangePath(rng), q, map[string]any{"range": rng, "values": values}, nil)
}

func (c *Client) appendValues(ctx context.Context, rng string, values [][]any) error {
	q := url.Values{"valueInputOption": {"RAW"}, "insertDataOption": {"INSERT_ROWS"}}
	return c.do(ctx, http.MethodPost, rangePath(rng)+":append", q, map[string]any{"values": values}, nil)
}

func (c *Client) batchUpdate(ctx context.Context, requests ...map[string]any) (map[string]any, error) {
	var out map[string]any
	err := c.do(ctx, http.MethodPost, ":batchUpdate", nil, map[string]any{"requests": requests}, &out)
	return out, err
}

// sheetIDs maps sheet title -> numeric sheetId.
func (c *Client) sheetIDs(ctx context.Context) (map[string]int64, error) {
	var res struct {
		Sheets []struct {
			Properties struct {
				SheetID int64  `json:"sheetId"`
				Title   string `json:"title"`
			} `json:"properties"`
		} `json:"sheets"`
	}
	q := url.Values{"fields": {"sheets.properties(sheetId,title)"}}
	if err := c.do(ctx, http.MethodGet, "", q, nil, &res); err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, s := range res.Sheets {
		out[s.Properties.Title] = s.Properties.SheetID
	}
	return out, nil
}

// cellString renders an unformatted cell value as text.
func cellString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%v", x)
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}
