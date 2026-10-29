package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/roysitumorang/bot_laukpauk/internal/state"
	"github.com/roysitumorang/bot_laukpauk/internal/xendit"
)

var ratingKB = kb(
	row(btn("⭐1", "rate:1"), btn("⭐2", "rate:2"), btn("⭐3", "rate:3"), btn("⭐4", "rate:4"), btn("⭐5", "rate:5")),
	row(btn("Nanti saja", "rate:skip")),
)

const ratingPrompt = "🌟 Silakan beri rating bintang dari 1 hingga 5, lalu berikan testimoni Anda!"

// askRating starts the rating conversation for uid, whose session is sess.
func (b *Bot) askRating(_ context.Context, uid int64, sess *state.Session) {
	sess.RateStep = state.RateRating
	sess.Rating = 0
	b.send(uid, ratingPrompt, ratingKB)
}

func (b *Bot) setRating(r *req, rating int) {
	r.sess.Rating = rating
	r.sess.RateStep = state.RateTestimoni
	b.reply(r, "⭐ Terima kasih atas rating Anda! Sekarang silakan berikan testimoni Anda.", nil)
}

func (b *Bot) handleRatingText(r *req, text string) {
	n, err := strconv.Atoi(text)
	if err != nil {
		b.reply(r, "❌ Masukkan angka 1-5 untuk memberi rating.", ratingKB)
		return
	}
	if n < 1 || n > 5 {
		b.reply(r, "❌ Rating bintang hanya boleh antara 1 hingga 5.", ratingKB)
		return
	}
	b.setRating(r, n)
}

func (b *Bot) cbRate(r *req, arg string) {
	if r.sess.RateStep != state.RateRating {
		b.answer(r, "")
		return
	}
	if arg == "skip" {
		b.answer(r, "")
		r.sess.RateStep, r.sess.Rating = "", 0
		b.replyOrEdit(r, "👍 Oke! Anda bisa memberi testimoni kapan saja dengan /beri_testimoni", nil)
		return
	}
	n, err := strconv.Atoi(arg)
	if err != nil || n < 1 || n > 5 {
		b.answer(r, "")
		return
	}
	b.answer(r, "")
	b.replyOrEdit(r, fmt.Sprintf("%s\n\nRating Anda: %s", ratingPrompt, strings.Repeat("⭐", n)), nil)
	b.setRating(r, n)
}

func (b *Bot) handleTestimoni(r *req, text string) {
	if text == "" {
		b.reply(r, "❌ Testimoni tidak boleh kosong. Silakan coba lagi.", nil)
		return
	}
	rating := r.sess.Rating
	if rating == 0 {
		r.sess.RateStep = ""
		b.reply(r, "❌ Terjadi kesalahan. Mohon coba lagi dengan /beri_testimoni", nil)
		return
	}
	ok, err := b.DB.SaveTestimoni(r.ctx, r.uid, rating, text)
	if err != nil {
		log.Printf("simpan testimoni %d: %v", r.uid, err)
		b.reply(r, "❌ Gagal menyimpan testimoni. Coba lagi nanti.", nil)
		return
	}
	if !ok {
		r.sess.RateStep, r.sess.Rating = "", 0
		b.reply(r, "❌ Testimoni hanya bisa diberikan oleh pelanggan yang sudah pernah berbelanja.", nil)
		return
	}
	if b.Sheets != nil {
		if err := b.Sheets.SaveTestimoni(r.ctx, r.uid, rating, text); err != nil {
			log.Printf("sheets testimoni %d: %v", r.uid, err)
		}
	}
	r.sess.RateStep, r.sess.Rating = "", 0
	b.reply(r, fmt.Sprintf("🎉 Terima kasih atas testimoni dan rating %d bintang Anda!\n\n\"%s\"", rating, text), nil)
}

func (b *Bot) cmdLihatTestimoni(r *req) {
	list, err := b.DB.ListTestimoni(r.ctx, 20)
	if err != nil {
		log.Printf("list testimoni: %v", err)
		b.reply(r, "❌ Terjadi kesalahan saat mengambil testimoni.", nil)
		return
	}
	if len(list) == 0 {
		b.reply(r, "❌ Belum ada testimoni yang tersedia.", nil)
		return
	}
	var sb strings.Builder
	sb.WriteString("💬 Testimoni Pelanggan:\n")
	for _, t := range list {
		nama := "Pelanggan"
		if f := strings.Fields(t.Nama); len(f) > 0 {
			nama = f[0]
		}
		stars := strings.Repeat("⭐", max(min(t.Rating, 5), 1))
		fmt.Fprintf(&sb, "\n%s %s: %s\n", stars, nama, t.Teks)
	}
	b.sendLong(r.chatID, sb.String())
}

func (b *Bot) cmdStatusPesanan(r *req) {
	if r.sess.CheckoutStep == state.StepInvoiceDikirim && r.sess.InvoiceID != "" {
		if inv, err := b.Xendit.GetInvoice(r.ctx, r.sess.InvoiceID); err == nil && inv.Status == xendit.StatusPending {
			b.reply(r, "⏳ Pesanan Anda menunggu pembayaran.", payNowKB(r.sess.InvoiceURL))
			return
		}
	}
	p, err := b.DB.GetPelanggan(r.ctx, r.uid)
	if err != nil {
		log.Printf("status pesanan %d: %v", r.uid, err)
		b.reply(r, "❌ Gagal membaca status pesanan. Coba lagi nanti.", nil)
		return
	}
	if p == nil || p.StatusPesanan == "" {
		b.reply(r, "❌ Pesanan tidak ditemukan.", nil)
		return
	}
	b.reply(r, "Status Pesanan Anda: "+p.StatusPesanan, nil)
}

// cbKirimBarang: the admin marks an order as shipped.
func (b *Bot) cbKirimBarang(r *req, arg string) {
	b.answer(r, "")
	if !b.isAdmin(r.uid) {
		b.replyOrEdit(r, "❌ Anda bukan admin. Tidak dapat mengirim barang.", nil)
		return
	}
	customer, err := strconv.ParseInt(arg, 10, 64)
	if err != nil {
		b.replyOrEdit(r, "❌ Terjadi kesalahan dalam membaca data pelanggan.", nil)
		return
	}
	ok, err := b.DB.SetStatusPesanan(r.ctx, customer, "Dikirim")
	if err != nil || !ok {
		if err != nil {
			log.Printf("status dikirim %d: %v", customer, err)
		}
		b.reply(r, fmt.Sprintf("❌ ERROR: Tidak dapat memperbarui status pesanan untuk user %d.", customer), nil)
		return
	}
	if b.Sheets != nil {
		if err := b.Sheets.SetStatusPesanan(r.ctx, customer, "Dikirim"); err != nil {
			log.Printf("sheets status dikirim %d: %v", customer, err)
		}
	}

	b.send(customer, "📦 Barang Anda telah dikirim! Terima kasih telah berbelanja di Laukpauk.", nil)

	done := fmt.Sprintf("✅ Barang untuk user %d telah dikirim.", customer)
	if r.cb.Message != nil && r.cb.Message.Text != "" {
		done = r.cb.Message.Text + "\n\n" + done
	}
	b.replyOrEdit(r, done, nil)

	// Ask the customer for a rating.
	if customer == r.uid {
		b.askRating(r.ctx, customer, r.sess)
		return
	}
	b.State.Update(customer, func(s *state.Session) { b.askRating(r.ctx, customer, s) })
}
