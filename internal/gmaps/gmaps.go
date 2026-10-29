// Package gmaps wraps the Google Maps Geocoding and Distance Matrix APIs.
package gmaps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// ErrNotFound means the address could not be resolved or routed.
var ErrNotFound = errors.New("alamat tidak ditemukan")

// Client calls the Maps web services with an API key.
type Client struct {
	key  string
	http *http.Client
	base string
}

// New returns a client.
func New(apiKey string) *Client {
	return &Client{key: apiKey, http: &http.Client{Timeout: 15 * time.Second}, base: "https://maps.googleapis.com/maps/api"}
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	q.Set("key", c.key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("google maps %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Geocode returns Google's formatted address for a free-form address.
func (c *Client) Geocode(ctx context.Context, address string) (string, error) {
	var res struct {
		Status       string `json:"status"`
		ErrorMessage string `json:"error_message"`
		Results      []struct {
			FormattedAddress string `json:"formatted_address"`
		} `json:"results"`
	}
	q := url.Values{"address": {address}, "region": {"id"}, "language": {"id"}}
	if err := c.get(ctx, "/geocode/json", q, &res); err != nil {
		return "", err
	}
	switch res.Status {
	case "OK":
		if len(res.Results) == 0 {
			return "", ErrNotFound
		}
		return res.Results[0].FormattedAddress, nil
	case "ZERO_RESULTS":
		return "", ErrNotFound
	default:
		return "", fmt.Errorf("geocode: %s %s", res.Status, res.ErrorMessage)
	}
}

// DrivingKm returns the driving distance in kilometres from (lat, lng) to
// destination.
func (c *Client) DrivingKm(ctx context.Context, lat, lng float64, destination string) (float64, error) {
	var res struct {
		Status       string `json:"status"`
		ErrorMessage string `json:"error_message"`
		Rows         []struct {
			Elements []struct {
				Status   string `json:"status"`
				Distance struct {
					Value float64 `json:"value"` // metres
				} `json:"distance"`
			} `json:"elements"`
		} `json:"rows"`
	}
	origin := strconv.FormatFloat(lat, 'f', -1, 64) + "," + strconv.FormatFloat(lng, 'f', -1, 64)
	q := url.Values{"origins": {origin}, "destinations": {destination}, "mode": {"driving"}, "region": {"id"}}
	if err := c.get(ctx, "/distancematrix/json", q, &res); err != nil {
		return 0, err
	}
	if res.Status != "OK" {
		return 0, fmt.Errorf("distance matrix: %s %s", res.Status, res.ErrorMessage)
	}
	if len(res.Rows) == 0 || len(res.Rows[0].Elements) == 0 || res.Rows[0].Elements[0].Status != "OK" {
		return 0, ErrNotFound
	}
	return res.Rows[0].Elements[0].Distance.Value / 1000, nil
}

// SetBaseURL overrides the API endpoint (for tests).
func (c *Client) SetBaseURL(u string) { c.base = u }
