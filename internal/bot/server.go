package bot

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"net/http"

	tg "github.com/roysitumorang/bot_laukpauk/internal/telegram"
	"github.com/roysitumorang/bot_laukpauk/internal/xendit"
)

const maxBody = 1 << 20

// TelegramWebhookPath and XenditWebhookPath are the HTTP routes.
const (
	TelegramWebhookPath = "/webhook/telegram"
	XenditWebhookPath   = "/webhook/xendit"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Handler returns the HTTP routes: health checks, the Telegram webhook and
// the Xendit invoice callback. base is the context background work derives
// from (cancelled on shutdown).
func (b *Bot) Handler(base context.Context) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"message": "API LaukPauk is running!"})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("POST "+TelegramWebhookPath, func(w http.ResponseWriter, r *http.Request) {
		if secret := b.cfg.WebhookSecret; secret != "" {
			got := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
			if subtle.ConstantTimeCompare([]byte(secret), []byte(got)) != 1 {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "unauthorized"})
				return
			}
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error"})
			return
		}
		var upd tg.Update
		if err := json.Unmarshal(body, &upd); err != nil {
			log.Printf("telegram webhook: %v", err)
			writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "message": "invalid update"})
			return
		}
		// Acknowledge immediately; Telegram retries slow webhooks.
		b.Go(func() { b.HandleUpdate(base, upd) })
		writeJSON(w, http.StatusOK, map[string]string{"status": "success"})
	})

	mux.HandleFunc("POST "+XenditWebhookPath, func(w http.ResponseWriter, r *http.Request) {
		if !xendit.ValidCallbackToken(b.cfg.XenditCallbackToken, r.Header.Get("X-Callback-Token")) {
			log.Printf("xendit webhook: callback token tidak valid")
			writeJSON(w, http.StatusUnauthorized, map[string]string{"status": "unauthorized"})
			return
		}
		var inv xendit.Invoice
		if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&inv); err != nil || inv.ID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"status": "error", "message": "invalid payload"})
			return
		}
		log.Printf("xendit webhook: invoice %s status %s", inv.ID, inv.Status)
		b.Go(func() { b.HandleInvoiceCallback(base, inv) })
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	return mux
}
