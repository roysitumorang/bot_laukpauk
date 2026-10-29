// Command laukpauk runs the Laukpauk Telegram shop bot together with the
// HTTP endpoints for the Telegram and Xendit webhooks.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // Asia/Jakarta even on minimal images

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"

	"github.com/roysitumorang/bot_laukpauk/assets"
	"github.com/roysitumorang/bot_laukpauk/internal/bot"
	"github.com/roysitumorang/bot_laukpauk/internal/catalog"
	"github.com/roysitumorang/bot_laukpauk/internal/config"
	"github.com/roysitumorang/bot_laukpauk/internal/gmaps"
	"github.com/roysitumorang/bot_laukpauk/internal/holiday"
	"github.com/roysitumorang/bot_laukpauk/internal/sheets"
	"github.com/roysitumorang/bot_laukpauk/internal/state"
	"github.com/roysitumorang/bot_laukpauk/internal/store"
	"github.com/roysitumorang/bot_laukpauk/internal/xendit"
	"github.com/roysitumorang/bot_laukpauk/migrations"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("laukpauk: ")

	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("membaca .env: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("konfigurasi tidak lengkap:\n%v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, cfg.PostgresURI)
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx, migrations.Init); err != nil {
		log.Printf("peringatan: migrasi skema gagal (lanjut dengan tabel yang ada): %v", err)
	}

	st, err := state.Open(cfg.StateDir)
	if err != nil {
		log.Fatalf("state: %v", err)
	}

	var sh *sheets.Client
	if cfg.SheetsEnabled() {
		if sh, err = sheets.New(cfg.CredentialsPath, cfg.SpreadsheetID); err != nil {
			log.Fatalf("google sheets: %v", err)
		}
		if err := sh.EnsureLayout(ctx); err != nil {
			log.Printf("peringatan: tidak bisa menyiapkan Google Sheets: %v", err)
		}
	} else {
		log.Printf("Google Sheets tidak dikonfigurasi; data hanya disimpan di PostgreSQL")
	}

	api, err := tgbotapi.NewBotAPI(cfg.BotToken)
	if err != nil {
		log.Fatalf("telegram: %v", err)
	}
	api.Debug = cfg.Debug
	log.Printf("login sebagai @%s", api.Self.UserName)

	b := bot.New(bot.Deps{
		Config:   cfg,
		API:      api,
		DB:       db,
		Sheets:   sh,
		Maps:     gmaps.New(cfg.GoogleMapsAPIKey),
		Xendit:   xendit.New(cfg.XenditAPIKey, cfg.XenditRedirectURL),
		State:    st,
		Holidays: holiday.New(cfg.HolidayFile, cfg.Location),
		Catalog:  catalog.New(),
		Logo:     assets.Logo,
	})
	if _, n, err := b.ReloadCatalog(ctx); err != nil {
		log.Printf("peringatan: produk belum dimuat: %v", err)
	} else {
		log.Printf("%d produk dimuat dari PostgreSQL", n)
	}
	b.SetCommands()

	// Background work keeps running during shutdown until it finishes or the
	// grace period below expires.
	workCtx, cancelWork := context.WithCancel(context.Background())
	defer cancelWork()

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           b.Handler(workCtx),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	go func() {
		log.Printf("HTTP server mendengarkan di %s", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http: %v", err)
		}
	}()

	if cfg.WebhookURL != "" {
		params := tgbotapi.Params{
			"url":             cfg.WebhookURL,
			"allowed_updates": `["message","callback_query"]`,
		}
		if cfg.WebhookSecret != "" {
			params["secret_token"] = cfg.WebhookSecret
		}
		if _, err := api.MakeRequest("setWebhook", params); err != nil {
			log.Fatalf("setWebhook: %v", err)
		}
		log.Printf("webhook Telegram diatur: %s", cfg.WebhookURL)
	} else {
		if _, err := api.Request(tgbotapi.DeleteWebhookConfig{}); err != nil {
			log.Printf("deleteWebhook: %v", err)
		}
		u := tgbotapi.NewUpdate(0)
		u.Timeout = 50
		u.AllowedUpdates = []string{"message", "callback_query"}
		updates := api.GetUpdatesChan(u)
		go func() {
			for upd := range updates {
				b.Go(func() { b.HandleUpdate(workCtx, upd) })
			}
		}()
		log.Printf("WEBHOOK_URL kosong: menerima update lewat long polling")
	}

	<-ctx.Done()
	log.Printf("berhenti...")
	if cfg.WebhookURL == "" {
		api.StopReceivingUpdates()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
	done := make(chan struct{})
	go func() { b.Wait(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		log.Printf("waktu habis menunggu pekerjaan latar belakang")
		cancelWork()
	}
	log.Printf("selesai")
}
