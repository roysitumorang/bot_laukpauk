// Package config loads settings from the environment (and an optional .env).
package config

import (
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config holds every runtime setting.
type Config struct {
	BotToken      string
	WebhookURL    string // public URL of /webhook/telegram; empty = long polling
	WebhookSecret string // optional Telegram secret_token
	ListenAddr    string
	AdminUserID   int64

	PostgresURI string

	GoogleMapsAPIKey string
	StoreLat         float64
	StoreLng         float64
	OngkosPerKm      int64

	XenditAPIKey        string
	XenditCallbackToken string
	XenditRedirectURL   string

	CredentialsPath string // Google service-account JSON; empty disables Sheets
	SpreadsheetID   string

	StateDir    string
	HolidayFile string
	InvoiceDir  string // optional: also keep a copy of every PDF invoice here

	Location *time.Location
	Debug    bool
}

// Load reads the configuration from environment variables.
func Load() (*Config, error) {
	c := &Config{
		BotToken:            env("BOT_TOKEN", ""),
		WebhookURL:          env("WEBHOOK_URL", ""),
		WebhookSecret:       env("TELEGRAM_WEBHOOK_SECRET", ""),
		ListenAddr:          env("LISTEN_ADDR", ":5000"),
		PostgresURI:         env("POSTGRES_URI", ""),
		GoogleMapsAPIKey:    env("GOOGLE_MAPS_API_KEY", ""),
		XenditAPIKey:        env("XENDIT_API_KEY", ""),
		XenditCallbackToken: env("XENDIT_CALLBACK_TOKEN", ""),
		XenditRedirectURL:   env("XENDIT_REDIRECT_URL", "https://t.me/Laukpauk_Bot"),
		CredentialsPath:     env("CREDENTIALS_PATH", ""),
		SpreadsheetID:       env("SPREADSHEET_ID", ""),
		StateDir:            env("STATE_DIR", "data"),
		InvoiceDir:          env("INVOICE_DIR", ""),
		Debug:               envBool("DEBUG"),
	}

	var errs []error
	var err error
	if c.AdminUserID, err = envInt("ADMIN_USER_ID", 5464224951); err != nil {
		errs = append(errs, err)
	}
	if c.OngkosPerKm, err = envInt("ONGKOS_PER_KM", 3000); err != nil {
		errs = append(errs, err)
	}
	if c.StoreLat, err = envFloat("STORE_LAT", 3.539174131612625); err != nil {
		errs = append(errs, err)
	}
	if c.StoreLng, err = envFloat("STORE_LNG", 98.64453142910467); err != nil {
		errs = append(errs, err)
	}
	c.HolidayFile = env("HOLIDAY_FILE", filepath.Join(c.StateDir, "holidays.json"))

	tz := env("TZ_NAME", "Asia/Jakarta")
	if c.Location, err = time.LoadLocation(tz); err != nil {
		errs = append(errs, fmt.Errorf("TZ_NAME: %w", err))
	}

	if c.PostgresURI == "" {
		c.PostgresURI = postgresFromParts()
	}

	for _, req := range []struct{ name, value string }{
		{"BOT_TOKEN", c.BotToken},
		{"POSTGRES_URI", c.PostgresURI},
		{"GOOGLE_MAPS_API_KEY", c.GoogleMapsAPIKey},
		{"XENDIT_API_KEY", c.XenditAPIKey},
	} {
		if req.value == "" {
			errs = append(errs, fmt.Errorf("%s belum diisi", req.name))
		}
	}
	if c.XenditCallbackToken == "" {
		log.Printf("peringatan: XENDIT_CALLBACK_TOKEN kosong, semua callback Xendit akan ditolak")
	}
	if (c.CredentialsPath == "") != (c.SpreadsheetID == "") {
		errs = append(errs, errors.New("CREDENTIALS_PATH dan SPREADSHEET_ID harus diisi bersamaan (atau dikosongkan keduanya)"))
	}
	return c, errors.Join(errs...)
}

// SheetsEnabled reports whether Google Sheets mirroring is configured.
func (c *Config) SheetsEnabled() bool { return c.CredentialsPath != "" && c.SpreadsheetID != "" }

// postgresFromParts builds a URI from DB_HOST/DB_PORT/DB_NAME/DB_USER/DB_PASS.
func postgresFromParts() string {
	name := env("DB_NAME", "")
	if name == "" {
		return ""
	}
	pass := env("DB_PASS", env("DB_PASSWORD", ""))
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(env("DB_USER", "postgres"), pass),
		Host:   env("DB_HOST", "localhost") + ":" + env("DB_PORT", "5432"),
		Path:   "/" + name,
	}
	q := url.Values{}
	q.Set("sslmode", env("DB_SSLMODE", "disable"))
	u.RawQuery = q.Encode()
	return u.String()
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envBool(key string) bool {
	b, _ := strconv.ParseBool(env(key, "false"))
	return b
}

func envInt(key string, def int64) (int64, error) {
	v := env(key, "")
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func envFloat(key string, def float64) (float64, error) {
	v := env(key, "")
	if v == "" {
		return def, nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return f, nil
}
