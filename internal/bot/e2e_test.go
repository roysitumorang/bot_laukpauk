package bot

// End-to-end test of the shopping, checkout, payment, shipping and review
// flow against a real PostgreSQL and fake Telegram, Google Maps and Xendit
// servers. It runs only when LAUKPAUK_TEST_POSTGRES points at a database the
// test may freely modify, e.g.
//
//	LAUKPAUK_TEST_POSTGRES=postgres://postgres@127.0.0.1:5432/laukpauk_test go test ./internal/bot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"


	"github.com/roysitumorang/bot_laukpauk/assets"
	"github.com/roysitumorang/bot_laukpauk/internal/catalog"
	"github.com/roysitumorang/bot_laukpauk/internal/config"
	"github.com/roysitumorang/bot_laukpauk/internal/gmaps"
	"github.com/roysitumorang/bot_laukpauk/internal/holiday"
	"github.com/roysitumorang/bot_laukpauk/internal/poin"
	"github.com/roysitumorang/bot_laukpauk/internal/state"
	"github.com/roysitumorang/bot_laukpauk/internal/store"
	tg "github.com/roysitumorang/bot_laukpauk/internal/telegram"
	"github.com/roysitumorang/bot_laukpauk/internal/xendit"
	"github.com/roysitumorang/bot_laukpauk/migrations"
)

const (
	adminID    int64 = 1000
	customerID int64 = 2000
	cbToken          = "cb-secret"
)

type tgCall struct {
	Method   string
	ChatID   string
	Text     string
	Markup   string
	FileName string
	FileSize int
}

type fakeTelegram struct {
	mu    sync.Mutex
	calls []tgCall
	next  int
}

func (f *fakeTelegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	method := parts[len(parts)-1]
	if !strings.HasPrefix(r.URL.Path, "/botTOKEN/") || r.Method != http.MethodPost {
		http.Error(w, "bad request path", http.StatusNotFound)
		return
	}
	var c tgCall
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		_ = r.ParseMultipartForm(20 << 20)
		c = tgCall{Method: method, ChatID: r.FormValue("chat_id"), Text: r.FormValue("caption")}
	} else {
		// JSON body, as sent by the telegram client.
		var p map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var text string
		_ = json.Unmarshal(p["text"], &text)
		c = tgCall{Method: method, ChatID: string(p["chat_id"]), Text: text, Markup: string(p["reply_markup"])}
	}
	if r.MultipartForm != nil {
		for _, fhs := range r.MultipartForm.File {
			for _, fh := range fhs {
				c.FileName, c.FileSize = fh.Filename, int(fh.Size)
			}
		}
	}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.next++
	id := f.next
	f.mu.Unlock()

	var result any = true
	switch method {
	case "getMe":
		result = map[string]any{"id": 1, "is_bot": true, "first_name": "Laukpauk", "username": "Laukpauk_Bot"}
	case "sendMessage", "editMessageText", "sendDocument":
		chat := c.ChatID
		if chat == "" {
			chat = "0"
		}
		result = json.RawMessage(fmt.Sprintf(`{"message_id":%d,"date":0,"chat":{"id":%s,"type":"private"},"text":%q}`, id, chat, c.Text))
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

func (f *fakeTelegram) take() []tgCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

type fakeXendit struct {
	mu      sync.Mutex
	status  string
	amount  int64
	created int
	expired int
}

func (x *fakeXendit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	x.mu.Lock()
	defer x.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v2/invoices":
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		x.amount = int64(req["amount"].(float64))
		x.created++
		x.status = xendit.StatusPending
		id := fmt.Sprintf("inv%d", x.created)
		fmt.Fprintf(w, `{"id":%q,"external_id":%q,"status":"PENDING","amount":%d,"invoice_url":"https://checkout.xendit.test/%s"}`, id, req["external_id"], x.amount, id)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v2/invoices/"):
		id := strings.TrimPrefix(r.URL.Path, "/v2/invoices/")
		fmt.Fprintf(w, `{"id":%q,"status":%q,"amount":%d}`, id, x.status, x.amount)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/expire!"):
		x.expired++
		x.status = xendit.StatusExpired
		fmt.Fprint(w, `{"status":"EXPIRED"}`)
	default:
		http.NotFound(w, r)
	}
}

func fakeMaps() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/geocode/json", func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Query().Get("address"), "antah") {
			fmt.Fprint(w, `{"status":"ZERO_RESULTS","results":[]}`)
			return
		}
		fmt.Fprint(w, `{"status":"OK","results":[{"formatted_address":"Jl. Setia Budi No.99, Medan, Sumatera Utara"}]}`)
	})
	mux.HandleFunc("/distancematrix/json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"OK","rows":[{"elements":[{"status":"OK","distance":{"value":4200}}]}]}`)
	})
	return mux
}

type harness struct {
	t      *testing.T
	b      *Bot
	tg     *fakeTelegram
	xen    *fakeXendit
	db     *store.DB
	http   http.Handler
	update int
}

func newHarness(t *testing.T) *harness {
	uri := os.Getenv("LAUKPAUK_TEST_POSTGRES")
	if uri == "" {
		t.Skip("set LAUKPAUK_TEST_POSTGRES to run the end-to-end test")
	}
	ctx := context.Background()
	db, err := store.Open(ctx, uri)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	reset := `DROP TABLE IF EXISTS produk, pelanggan;` + migrations.Init + `
		INSERT INTO produk (nama_produk, kategori, harga, diskon, stok) VALUES
		('Brokoli', 'Sayur', 15000, 2000, 10),
		('Brokoli Hijau Super', 'Sayur', 20000, 0, 5),
		('Tahu Putih', 'Lauk', 5000, 0, 50),
		('Ikan Tongkol', 'Lauk', 40000, 0, 1);`
	if err := db.Migrate(ctx, reset); err != nil {
		t.Fatal(err)
	}

	fake := &fakeTelegram{}
	tgSrv := httptest.NewServer(fake)
	t.Cleanup(tgSrv.Close)
	api := tg.NewWithEndpoint("TOKEN", tgSrv.URL)
	xen := &fakeXendit{}
	xenSrv := httptest.NewServer(xen)
	t.Cleanup(xenSrv.Close)
	mapsSrv := httptest.NewServer(fakeMaps())
	t.Cleanup(mapsSrv.Close)

	jkt, _ := time.LoadLocation("Asia/Jakarta")
	dir := t.TempDir()
	st, err := state.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	maps := gmaps.New("KEY")
	maps.SetBaseURL(mapsSrv.URL)
	xc := xendit.New("KEY", "https://t.me/Laukpauk_Bot")
	xc.SetBaseURL(xenSrv.URL)
	cfg := &config.Config{
		AdminUserID: adminID, OngkosPerKm: 3000, StoreLat: 3.5, StoreLng: 98.6,
		XenditCallbackToken: cbToken, Location: jkt, StateDir: dir,
	}
	b := New(Deps{
		Config: cfg, API: api, DB: db, Maps: maps, Xendit: xc, State: st,
		Holidays: holiday.New(dir+"/holidays.json", jkt), Catalog: catalog.New(), Logo: assets.Logo,
	})
	if _, _, err := b.ReloadCatalog(ctx); err != nil {
		t.Fatal(err)
	}
	fake.take()
	return &harness{t: t, b: b, tg: fake, xen: xen, db: db, http: b.Handler(ctx)}
}

func (h *harness) msg(from int64, text string) []tgCall {
	h.update++
	m := &tg.Message{MessageID: h.update, From: &tg.User{ID: from}, Chat: tg.Chat{ID: from}, Text: text}
	if strings.HasPrefix(text, "/") {
		cmd := strings.Fields(text)[0]
		m.Entities = []tg.MessageEntity{{Type: "bot_command", Offset: 0, Length: len(cmd)}}
	}
	h.b.HandleUpdate(context.Background(), tg.Update{UpdateID: h.update, Message: m})
	return h.tg.take()
}

func (h *harness) press(from int64, data string) []tgCall {
	h.update++
	cb := &tg.CallbackQuery{ID: fmt.Sprint(h.update), From: &tg.User{ID: from}, Data: data,
		Message: &tg.Message{MessageID: 1, Chat: tg.Chat{ID: from}, Text: "previous"}}
	h.b.HandleUpdate(context.Background(), tg.Update{UpdateID: h.update, CallbackQuery: cb})
	return h.tg.take()
}

// button finds the callback data of the first button whose text contains label.
func (h *harness) button(calls []tgCall, label string) string {
	h.t.Helper()
	for i := len(calls) - 1; i >= 0; i-- {
		var mk tg.InlineKeyboardMarkup
		if json.Unmarshal([]byte(calls[i].Markup), &mk) != nil {
			continue
		}
		for _, row := range mk.InlineKeyboard {
			for _, b := range row {
				if strings.Contains(b.Text, label) && b.CallbackData != "" {
					return b.CallbackData
				}
			}
		}
	}
	h.t.Fatalf("no button %q in %+v", label, calls)
	return ""
}

func texts(calls []tgCall) string {
	var sb strings.Builder
	for _, c := range calls {
		if c.Method == "sendMessage" || c.Method == "editMessageText" || c.Method == "sendDocument" || c.Method == "answerCallbackQuery" {
			sb.WriteString(c.Method + "[" + c.ChatID + "]: " + c.Text + "\n")
		}
	}
	return sb.String()
}

func (h *harness) expect(calls []tgCall, wants ...string) {
	h.t.Helper()
	all := texts(calls)
	for _, w := range wants {
		if !strings.Contains(all, w) {
			h.t.Fatalf("expected %q in:\n%s", w, all)
		}
	}
}

func (h *harness) xenditCallback(token, id, status string) int {
	body := fmt.Sprintf(`{"id":%q,"status":%q,"paid_amount":%d}`, id, status, h.xen.amount)
	req := httptest.NewRequest(http.MethodPost, XenditWebhookPath, strings.NewReader(body))
	req.Header.Set("X-CALLBACK-TOKEN", token)
	rec := httptest.NewRecorder()
	h.http.ServeHTTP(rec, req)
	h.b.Wait()
	return rec.Code
}

func TestEndToEnd(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	h.expect(h.msg(customerID, "/start"), "Selamat datang di Bot Laukpauk")

	// Search with several matches, then buy.
	calls := h.msg(customerID, "brok")
	h.expect(calls, "Beberapa produk ditemukan")
	h.expect(h.press(customerID, h.button(calls, "Brokoli - Rp 13.000")), "1 Brokoli berhasil ditambahkan")
	calls = h.msg(customerID, "tahu")
	h.press(customerID, h.button(calls, "Beli Tahu putih"))
	for i := 0; i < 7; i++ {
		h.press(customerID, h.button(calls, "Beli Tahu putih"))
	}
	// Stock check: only one Ikan Tongkol.
	calls = h.msg(customerID, "tongkol")
	data := h.button(calls, "Beli Ikan tongkol")
	h.press(customerID, data)
	h.expect(h.press(customerID, data), "Stok Ikan tongkol tidak mencukupi")

	// Category browsing.
	calls = h.msg(customerID, "/kategori")
	calls = h.press(customerID, h.button(calls, "Sayur"))
	h.expect(calls, "Produk dalam kategori Sayur", "")
	h.expect(h.press(customerID, h.button(calls, "Brokoli hijau super")), "Stok: 5 pcs")

	// Cart: 1 brokoli (13.000) + 8 tahu (40.000) + 1 tongkol (40.000) = 93.000
	calls = h.msg(customerID, "/keranjang")
	h.expect(calls, "Total: Rp 93.000", "1.500 poin")
	calls = h.press(customerID, h.button(calls, "Edit Keranjang"))
	h.button(calls, "Kurangi Brokoli")
	h.press(customerID, h.button(calls, "Hapus Ikan tongkol")) // 53.000 left
	h.expect(h.msg(customerID, "/keranjang"), "Total: Rp 53.000")

	// Checkout as a new customer.
	h.expect(h.msg(customerID, "/checkout"), "Nama:")
	h.expect(h.msg(customerID, "Budi Santoso"), "Masukkan Alamat")
	h.expect(h.msg(customerID, "negeri antah berantah"), "Alamat tidak valid")
	h.expect(h.msg(customerID, "Jl Setia Budi 99 Medan"), "Perkiraan jarak: 4.20 km", "Ongkos kirim: Rp 12.600")
	h.expect(h.msg(customerID, "pagar hitam"), "Masukkan Nomor HP")
	h.expect(h.msg(customerID, "halo"), "Nomor HP tidak valid")
	calls = h.msg(customerID, "0812-3456-7890")
	h.expect(calls, "Pilih voucher")
	// DISKON10 (belanja, min 50k) and ONGKIR5 (min 30k) qualify; DISKON25/ONGKIR10 don't.
	if strings.Contains(calls[len(calls)-1].Markup, "DISKON25") {
		t.Fatal("DISKON25 should not be offered below its minimum")
	}
	calls = h.press(customerID, h.button(calls, "ONGKIR5"))
	h.expect(calls, "Voucher ONGKIR5 telah digunakan", "belum memiliki poin", "Pilih waktu pengiriman")
	calls = h.press(customerID, h.button(calls, "– "))
	// 53.000 + 12.600 - 5.000 = 60.600
	h.expect(calls, "Anda memilih pengiriman", "Total Pembayaran: Rp 60.600", "pilih metode pembayaran")

	// Typing during payment selection is blocked.
	h.expect(h.msg(customerID, "bayam"), "sedang dalam proses checkout")

	calls = h.press(customerID, "pay_qris")
	h.expect(calls, "Metode pembayaran: QRIS", "selesaikan pembayaran")
	if h.xen.amount != 60600 {
		t.Fatalf("invoice amount = %d", h.xen.amount)
	}
	// A second click must not create a second invoice.
	h.expect(h.press(customerID, "pay_qris"), "masih aktif")
	if h.xen.created != 1 {
		t.Fatalf("created %d invoices", h.xen.created)
	}
	h.expect(h.msg(customerID, "bayam"), "sedang dalam proses checkout")

	// Webhook security.
	if code := h.xenditCallback("wrong", "inv1", "PAID"); code != http.StatusUnauthorized {
		t.Fatalf("bad token gave %d", code)
	}
	h.xen.status = xendit.StatusPaid
	if code := h.xenditCallback(cbToken, "inv1", "PAID"); code != http.StatusOK {
		t.Fatalf("callback gave %d", code)
	}
	calls = h.tg.take()
	h.expect(calls, "Pembayaran Anda berhasil", "Invoice Pembelian Anda", "Total: Rp 60.600",
		"Anda mendapatkan 500 poin", "Pesanan Baru Masuk", "Total Pembayaran: Rp 60.600", "Voucher ONGKIR5")
	var pdfs int
	for _, c := range calls {
		if c.Method == "sendDocument" {
			pdfs++
			if c.FileSize < 1000 || !strings.HasSuffix(c.FileName, ".pdf") {
				t.Fatalf("bad pdf %+v", c)
			}
		}
	}
	if pdfs != 2 {
		t.Fatalf("expected PDF to customer and admin, got %d", pdfs)
	}
	// Retried callbacks are ignored.
	h.xenditCallback(cbToken, "inv1", "PAID")
	if extra := h.tg.take(); len(extra) != 0 {
		t.Fatalf("duplicate callback processed: %s", texts(extra))
	}

	p, err := h.db.GetPelanggan(ctx, customerID)
	if err != nil || p == nil {
		t.Fatalf("pelanggan not saved: %v", err)
	}
	if p.TotalPoin != poin.Hitung(53000) || p.StatusPesanan != "Diproses" || p.HP != "0812-3456-7890" || len(p.ProdukTerakhir) != 2 {
		t.Fatalf("pelanggan = %+v", p)
	}
	if s := h.b.State.Get(customerID); len(s.Cart) != 0 || s.CheckoutStep != "" {
		t.Fatalf("session not reset: %+v", s)
	}
	h.expect(h.msg(customerID, "/status_pesanan"), "Status Pesanan Anda: Diproses")

	// Shipping and review.
	h.expect(h.press(customerID, fmt.Sprintf("kirim_barang:%d", customerID)), "bukan admin")
	calls = h.press(adminID, fmt.Sprintf("kirim_barang:%d", customerID))
	h.expect(calls, "Barang Anda telah dikirim", "telah dikirim.", "beri rating bintang")
	h.expect(h.msg(customerID, "9"), "antara 1 hingga 5")
	h.expect(h.press(customerID, "rate:5"), "silakan berikan testimoni")
	h.expect(h.msg(customerID, "Sayurnya segar!"), "rating 5 bintang")
	h.expect(h.msg(customerID, "/lihat_testimoni"), "⭐⭐⭐⭐⭐ Budi: Sayurnya segar!")
	h.expect(h.msg(customerID, "/status_pesanan"), "Dikirim")

	// Repeat order, returning-customer checkout, then cancel with an open invoice.
	h.expect(h.msg(customerID, "/pesan_ulang"), "ditambahkan ke keranjang")
	h.expect(h.msg(customerID, "/keranjang"), "Total: Rp 53.000")
	calls = h.msg(customerID, "/checkout")
	h.expect(calls, "Data Anda yang tersimpan", "Budi Santoso")
	calls = h.press(customerID, "confirm_old_data")
	h.expect(calls, "Data lama digunakan", "Pilih voucher")
	calls = h.press(customerID, "skip_voucher")
	h.expect(calls, "Poin Anda: 500", "minimal 10.000", "Pilih waktu pengiriman")
	calls = h.press(customerID, h.button(calls, "– "))
	h.expect(calls, "Total Pembayaran: Rp 65.600")
	h.press(customerID, "pay_va")
	h.expect(h.msg(customerID, "/batal"), "Pesanan dibatalkan")
	if h.xen.expired != 1 {
		t.Fatal("open invoice was not expired at Xendit")
	}
	if _, ok := h.b.State.Pending("inv2"); ok {
		t.Fatal("cancelled order still pending")
	}
	h.expect(h.msg(customerID, "/keranjang"), "Keranjang Anda kosong")

	// Admin commands.
	h.expect(h.msg(customerID, "/tutup_toko 01-01-2030 02-01-2030"), "tidak memiliki izin")
	h.expect(h.msg(adminID, "/tutup_toko 01-01-2030"), "Format salah")
	h.expect(h.msg(adminID, "/tambah_voucher hemat belanja 3000 10000"), "Voucher HEMAT berhasil")
	if _, err := h.db.SetHarga(ctx, "tahu putih", 4000); err != nil {
		t.Fatal(err)
	}
	h.db.Migrate(ctx, `INSERT INTO pelanggan (user_id, nama) VALUES ('3000', 'Sari')`)
	calls = h.msg(adminID, "/update_harga")
	h.b.Wait()
	calls = append(calls, h.tg.take()...)
	h.expect(calls, "Tahu putih: Rp 5.000 ➝ Rp 4.000", "dikirim ke 2 pelanggan")
}
