// Package xendit is a small client for the Xendit Invoice API.
package xendit

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Invoice statuses.
const (
	StatusPending = "PENDING"
	StatusPaid    = "PAID"
	StatusSettled = "SETTLED"
	StatusExpired = "EXPIRED"
)

// PaymentChannels maps the bot's payment buttons to Xendit payment methods.
var PaymentChannels = map[string][]string{
	"pay_va":      {"BNI", "BRI", "BSI", "MANDIRI"},
	"pay_ewallet": {"OVO", "SHOPEEPAY"},
	"pay_qris":    {"QRIS"},
	"pay_cc":      {"CREDIT_CARD"},
}

// Invoice is the subset of the Xendit invoice object the bot uses. It is
// also the payload of the invoice callback.
type Invoice struct {
	ID         string  `json:"id"`
	ExternalID string  `json:"external_id"`
	Status     string  `json:"status"`
	Amount     float64 `json:"amount"`
	InvoiceURL string  `json:"invoice_url"`
	PaidAmount float64 `json:"paid_amount"`
}

// IsPaid reports whether the invoice has been paid.
func (i *Invoice) IsPaid() bool { return i.Status == StatusPaid || i.Status == StatusSettled }

// Client calls https://api.xendit.co with a secret API key.
type Client struct {
	apiKey      string
	redirectURL string
	http        *http.Client
	base        string
}

// New returns a client. redirectURL is where Xendit sends the payer after
// paying (or failing to pay).
func New(apiKey, redirectURL string) *Client {
	return &Client{apiKey: apiKey, redirectURL: redirectURL, http: &http.Client{Timeout: 20 * time.Second}, base: "https://api.xendit.co"}
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.apiKey, "")
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
		return fmt.Errorf("xendit %s %s: %s: %s", method, path, resp.Status, raw)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// CreateInvoice creates an invoice restricted to the given payment methods.
func (c *Client) CreateInvoice(ctx context.Context, externalID string, amount int64, description string, methods []string) (*Invoice, error) {
	req := map[string]any{
		"external_id":          externalID,
		"amount":               amount,
		"description":          description,
		"currency":             "IDR",
		"success_redirect_url": c.redirectURL,
		"failure_redirect_url": c.redirectURL,
		"payment_methods":      methods,
	}
	var inv Invoice
	if err := c.do(ctx, http.MethodPost, "/v2/invoices", req, &inv); err != nil {
		return nil, err
	}
	if inv.ID == "" || inv.InvoiceURL == "" {
		return nil, fmt.Errorf("xendit: respons invoice tidak lengkap")
	}
	return &inv, nil
}

// GetInvoice fetches an invoice by its Xendit ID.
func (c *Client) GetInvoice(ctx context.Context, id string) (*Invoice, error) {
	var inv Invoice
	err := c.do(ctx, http.MethodGet, "/v2/invoices/"+url.PathEscape(id), nil, &inv)
	return &inv, err
}

// ExpireInvoice cancels an unpaid invoice so it can no longer be paid.
func (c *Client) ExpireInvoice(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/invoices/"+url.PathEscape(id)+"/expire!", nil, nil)
}

// ValidCallbackToken compares the X-CALLBACK-TOKEN header in constant time.
// An empty expected token never validates.
func ValidCallbackToken(expected, got string) bool {
	if expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(got)) == 1
}

// SetBaseURL overrides the API endpoint (for tests).
func (c *Client) SetBaseURL(u string) { c.base = u }
