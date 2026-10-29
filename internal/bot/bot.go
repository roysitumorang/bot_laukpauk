// Package bot implements the Laukpauk Telegram shop bot.
package bot

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/roysitumorang/bot_laukpauk/internal/catalog"
	"github.com/roysitumorang/bot_laukpauk/internal/config"
	"github.com/roysitumorang/bot_laukpauk/internal/gmaps"
	"github.com/roysitumorang/bot_laukpauk/internal/holiday"
	"github.com/roysitumorang/bot_laukpauk/internal/sheets"
	"github.com/roysitumorang/bot_laukpauk/internal/state"
	"github.com/roysitumorang/bot_laukpauk/internal/store"
	tg "github.com/roysitumorang/bot_laukpauk/internal/telegram"
	"github.com/roysitumorang/bot_laukpauk/internal/xendit"
)

// Deps are the bot's collaborators.
type Deps struct {
	Config   *config.Config
	API      *tg.Client
	DB       *store.DB
	Sheets   *sheets.Client // nil when Google Sheets is not configured
	Maps     *gmaps.Client
	Xendit   *xendit.Client
	State    *state.Store
	Holidays *holiday.Store
	Catalog  *catalog.Catalog
	Logo     []byte
}

// Bot handles Telegram updates and payment callbacks.
type Bot struct {
	Deps
	cfg *config.Config

	bg sync.WaitGroup // background work (webhook processing, broadcasts)
}

// New returns a Bot.
func New(d Deps) *Bot {
	return &Bot{Deps: d, cfg: d.Config}
}

// Wait blocks until background work has finished.
func (b *Bot) Wait() { b.bg.Wait() }

// Go runs fn in the background, tracked by Wait.
func (b *Bot) Go(fn func()) {
	b.bg.Add(1)
	go func() {
		defer b.bg.Done()
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v\n%s", rec, debug.Stack())
			}
		}()
		fn()
	}()
}

// req is the context of one incoming update.
type req struct {
	ctx    context.Context
	uid    int64
	chatID int64
	sess   *state.Session
	msg    *tg.Message       // set for messages
	cb     *tg.CallbackQuery // set for callback queries
}

func (r *req) isCallback() bool { return r.cb != nil }

// HandleUpdate processes one Telegram update. Updates for the same user are
// handled one at a time so their conversation state stays consistent.
func (b *Bot) HandleUpdate(parent context.Context, upd tg.Update) {
	r := &req{}
	switch {
	case upd.Message != nil && upd.Message.From != nil:
		r.msg = upd.Message
		r.uid = upd.Message.From.ID
		r.chatID = upd.Message.Chat.ID
	case upd.CallbackQuery != nil && upd.CallbackQuery.From != nil:
		r.cb = upd.CallbackQuery
		r.uid = upd.CallbackQuery.From.ID
		r.chatID = r.uid
		if upd.CallbackQuery.Message != nil {
			r.chatID = upd.CallbackQuery.Message.Chat.ID
		}
	default:
		return
	}

	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	r.ctx = ctx

	unlock := b.State.LockUser(r.uid)
	defer unlock()
	sess := b.State.Get(r.uid)
	r.sess = &sess
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("panic handling update %d from %d: %v\n%s", upd.UpdateID, r.uid, rec, debug.Stack())
			b.send(r.chatID, "❌ Terjadi kesalahan. Silakan coba lagi.", nil)
		}
		b.State.Put(r.uid, sess)
	}()

	if r.msg != nil {
		if b.cfg.Debug {
			log.Printf("message from %d: %q", r.uid, r.msg.Text)
		}
		if r.msg.IsCommand() {
			b.command(r)
		} else if strings.TrimSpace(r.msg.Text) != "" {
			b.text(r)
		}
		return
	}
	if b.cfg.Debug {
		log.Printf("callback from %d: %q", r.uid, r.cb.Data)
	}
	b.callback(r)
}

func (b *Bot) isAdmin(uid int64) bool { return uid == b.cfg.AdminUserID }

func (b *Bot) command(r *req) {
	switch r.msg.Command() {
	case "start":
		b.cmdStart(r)
	case "produk":
		b.reply(r, "Silakan masukkan nama produk yang Anda cari:", nil)
	case "kategori":
		b.cmdKategori(r)
	case "keranjang":
		b.cmdKeranjang(r)
	case "edit_keranjang":
		b.showEditKeranjang(r)
	case "checkout":
		b.startCheckout(r)
	case "pesan_ulang":
		b.cmdPesanUlang(r)
	case "batal":
		b.batalPesanan(r)
	case "status_pesanan":
		b.cmdStatusPesanan(r)
	case "beri_testimoni":
		b.askRating(r.ctx, r.uid, r.sess)
	case "lihat_testimoni":
		b.cmdLihatTestimoni(r)
	// admin
	case "update_harga":
		b.cmdUpdateHarga(r)
	case "reload":
		b.cmdReload(r)
	case "tambah_voucher":
		b.cmdTambahVoucher(r)
	case "tutup_toko":
		b.cmdTutupToko(r)
	case "buka_toko":
		b.cmdBukaToko(r)
	case "set_harga":
		b.cmdSetHarga(r)
	default:
		b.reply(r, "⚠️ Perintah tidak dikenali. Gunakan /start untuk memulai.", nil)
	}
}

func (b *Bot) text(r *req) {
	text := strings.TrimSpace(r.msg.Text)
	switch r.sess.CheckoutStep {
	case state.StepNama, state.StepAlamat, state.StepDetailAlamat, state.StepHP, state.StepPoin:
		b.checkoutInput(r, text)
		return
	}
	switch r.sess.RateStep {
	case state.RateRating:
		b.handleRatingText(r, text)
		return
	case state.RateTestimoni:
		b.handleTestimoni(r, text)
		return
	}
	switch r.sess.CheckoutStep {
	case state.StepWaktuPengiriman, state.StepMetodeBayar:
		b.warnCheckoutInProgress(r)
		return
	}
	switch strings.ToLower(text) {
	case "produk":
		b.reply(r, "Silakan masukkan nama produk yang Anda cari:", nil)
		return
	case "kategori":
		b.cmdKategori(r)
		return
	}
	b.cariProduk(r, text)
}

func (b *Bot) callback(r *req) {
	data := r.cb.Data
	prefix, arg, _ := strings.Cut(data, ":")
	switch {
	case prefix == "kat":
		b.cbKategori(r, arg)
	case prefix == "prd":
		b.cbProduk(r, arg)
	case prefix == "beli":
		b.cbBeli(r, arg)
	case data == "edit_keranjang":
		b.answer(r, "")
		b.showEditKeranjang(r)
	case prefix == "tambah" || prefix == "kurang" || prefix == "hapus":
		b.cbEditItem(r, prefix, arg)
	case data == "checkout" || data == "proses_checkout" || data == "back_to_checkout":
		b.answer(r, "")
		b.startCheckout(r)
	case data == "confirm_old_data" || data == "edit_data":
		b.cbCheckoutOption(r, data)
	case prefix == "voucher":
		b.cbVoucher(r, arg)
	case data == "skip_voucher" || data == "lanjut":
		b.cbSkipVoucher(r)
	case strings.HasPrefix(data, "delivery_"):
		b.cbDelivery(r, data)
	case strings.HasPrefix(data, "pay_"):
		b.cbPay(r, data)
	case data == "batal_pesanan":
		b.answer(r, "")
		b.batalPesanan(r)
	case data == "lanjut_checkout":
		b.answer(r, "")
		if r.cb.Message != nil {
			b.deleteMessage(r.chatID, r.cb.Message.MessageID)
		}
	case prefix == "kirim_barang":
		b.cbKirimBarang(r, arg)
	case prefix == "rate":
		b.cbRate(r, arg)
	default:
		b.answer(r, "⚠️ Tombol ini sudah tidak berlaku.")
	}
}

// ---- Telegram helpers -------------------------------------------------------

func btn(text, data string) tg.InlineKeyboardButton { return tg.CallbackButton(text, data) }

func kb(rows ...[]tg.InlineKeyboardButton) *tg.InlineKeyboardMarkup {
	return &tg.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func row(buttons ...tg.InlineKeyboardButton) []tg.InlineKeyboardButton { return buttons }

var batalKB = kb(row(btn("❌ Batalkan Pesanan", "batal_pesanan")))

// apiCtx bounds a single outgoing Bot API call.
func apiCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// send sends a plain-text message.
func (b *Bot) send(chatID int64, text string, markup *tg.InlineKeyboardMarkup) bool {
	ctx, cancel := apiCtx()
	defer cancel()
	_, err := b.API.SendMessage(ctx, tg.SendMessageParams{
		ChatID: chatID, Text: text, DisableWebPagePreview: true, ReplyMarkup: markup,
	})
	if err != nil {
		log.Printf("send to %d: %v", chatID, err)
		return false
	}
	return true
}

// sendLong splits text at line breaks to respect Telegram's 4096-char limit.
func (b *Bot) sendLong(chatID int64, text string) {
	const limit = 4000
	var chunk strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if chunk.Len()+len(line)+1 > limit && chunk.Len() > 0 {
			b.send(chatID, chunk.String(), nil)
			chunk.Reset()
		}
		if chunk.Len() > 0 {
			chunk.WriteByte('\n')
		}
		chunk.WriteString(line)
	}
	if chunk.Len() > 0 {
		b.send(chatID, chunk.String(), nil)
	}
}

func (b *Bot) reply(r *req, text string, markup *tg.InlineKeyboardMarkup) {
	b.send(r.chatID, text, markup)
}

// replyOrEdit edits the message that carried the pressed button, or sends a
// new message when handling a command.
func (b *Bot) replyOrEdit(r *req, text string, markup *tg.InlineKeyboardMarkup) {
	if r.cb == nil || r.cb.Message == nil {
		b.send(r.chatID, text, markup)
		return
	}
	ctx, cancel := apiCtx()
	defer cancel()
	err := b.API.EditMessageText(ctx, tg.EditMessageTextParams{
		ChatID: r.chatID, MessageID: r.cb.Message.MessageID, Text: text,
		DisableWebPagePreview: true, ReplyMarkup: markup,
	})
	if err != nil && !tg.IsNotModified(err) {
		log.Printf("edit message: %v", err)
		b.send(r.chatID, text, markup)
	}
}

func (b *Bot) answerCallback(r *req, text string, alert bool) {
	ctx, cancel := apiCtx()
	defer cancel()
	if err := b.API.AnswerCallbackQuery(ctx, r.cb.ID, text, alert); err != nil {
		log.Printf("answer callback: %v", err)
	}
}

// answer acknowledges a callback query (optionally with a toast).
func (b *Bot) answer(r *req, text string) {
	if r.cb != nil {
		b.answerCallback(r, text, false)
	}
}

// alert acknowledges a callback query with a pop-up.
func (b *Bot) alert(r *req, text string) {
	if r.cb == nil {
		b.reply(r, text, nil)
		return
	}
	b.answerCallback(r, text, true)
}

func (b *Bot) deleteMessage(chatID int64, messageID int) {
	ctx, cancel := apiCtx()
	defer cancel()
	if err := b.API.DeleteMessage(ctx, chatID, messageID); err != nil {
		log.Printf("delete message: %v", err)
	}
}

func (b *Bot) sendDocument(chatID int64, name string, data []byte, caption string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := b.API.SendDocument(ctx, chatID, name, data, caption); err != nil {
		log.Printf("send document to %d: %v", chatID, err)
		return false
	}
	return true
}

func (b *Bot) now() time.Time { return time.Now().In(b.cfg.Location) }

// shopClosed tells the user the shop is closed and returns true when today
// is a holiday.
func (b *Bot) shopClosed(r *req, suffix string) bool {
	if !b.Holidays.IsHoliday(b.now()) {
		return false
	}
	b.reply(r, fmt.Sprintf("🚨 Toko sedang libur dari %s. %s", b.Holidays.Describe(), suffix), nil)
	return true
}

func (b *Bot) warnCheckoutInProgress(r *req) {
	b.reply(r, "⚠️ Kamu sedang dalam proses checkout.\nSelesaikan pesanan, atau buat pesanan baru jika ingin mulai ulang.", batalKB)
}

// SetCommands registers the command menu shown by Telegram clients.
func (b *Bot) SetCommands(ctx context.Context) {
	err := b.API.SetMyCommands(ctx, []tg.BotCommand{
		{Command: "start", Description: "Mulai belanja"},
		{Command: "kategori", Description: "Lihat kategori produk"},
		{Command: "produk", Description: "Cari produk"},
		{Command: "keranjang", Description: "Lihat keranjang"},
		{Command: "checkout", Description: "Checkout pesanan"},
		{Command: "pesan_ulang", Description: "Pesan ulang pesanan terakhir"},
		{Command: "status_pesanan", Description: "Cek status pesanan"},
		{Command: "batal", Description: "Batalkan pesanan"},
		{Command: "beri_testimoni", Description: "Beri rating & testimoni"},
		{Command: "lihat_testimoni", Description: "Lihat testimoni pelanggan"},
	})
	if err != nil {
		log.Printf("setMyCommands: %v", err)
	}
}
