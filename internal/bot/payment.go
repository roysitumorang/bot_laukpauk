package bot

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/roysitumorang/bot_laukpauk/internal/catalog"
	"github.com/roysitumorang/bot_laukpauk/internal/invoice"
	"github.com/roysitumorang/bot_laukpauk/internal/money"
	"github.com/roysitumorang/bot_laukpauk/internal/order"
	"github.com/roysitumorang/bot_laukpauk/internal/poin"
	"github.com/roysitumorang/bot_laukpauk/internal/sheets"
	"github.com/roysitumorang/bot_laukpauk/internal/state"
	"github.com/roysitumorang/bot_laukpauk/internal/store"
	"github.com/roysitumorang/bot_laukpauk/internal/xendit"
)

var metodeLabel = map[string]string{
	"pay_va":      "Transfer Bank",
	"pay_cc":      "Kartu Kredit",
	"pay_ewallet": "E-Wallet",
	"pay_qris":    "QRIS",
}

func (b *Bot) snapshot(uid int64, s *state.Session, t order.Totals, invoiceID, externalID, url string) *state.PendingOrder {
	return &state.PendingOrder{
		InvoiceID:       invoiceID,
		ExternalID:      externalID,
		InvoiceURL:      url,
		UserID:          uid,
		Nama:            s.Nama,
		Alamat:          s.Alamat,
		DetailAlamat:    s.DetailAlamat,
		HP:              s.HP,
		WaktuPengiriman: s.WaktuPengiriman,
		Voucher:         s.Voucher,
		JarakKm:         s.JarakKm,
		Cart:            s.Cart.Clone(),
		Totals:          t,
		CreatedAt:       time.Now(),
	}
}

func payNowKB(url string) *tgbotapi.InlineKeyboardMarkup {
	return kb(
		row(tgbotapi.NewInlineKeyboardButtonURL("🔗 Bayar Sekarang", url)),
		row(btn("❌ Batalkan Pesanan", "batal_pesanan")),
	)
}

func (b *Bot) cbPay(r *req, data string) {
	b.answer(r, "")
	s := r.sess
	switch s.CheckoutStep {
	case state.StepMetodeBayar:
	case state.StepInvoiceDikirim:
		if s.InvoiceID != "" {
			inv, err := b.Xendit.GetInvoice(r.ctx, s.InvoiceID)
			if err == nil && (inv.Status == xendit.StatusPending || inv.IsPaid()) {
				b.reply(r, fmt.Sprintf("⚠️ Anda sudah memiliki invoice yang masih aktif (status: %s).\nSilakan selesaikan pembayaran terlebih dahulu atau tunggu konfirmasi.", inv.Status), payNowKB(s.InvoiceURL))
				return
			}
		}
		b.reply(r, "⚠️ Invoice sebelumnya sudah tidak berlaku. Ketik /checkout untuk memesan lagi.", nil)
		s.ResetCheckout()
		return
	default:
		b.reply(r, "⚠️ Sesi checkout sudah berakhir. Ketik /checkout untuk memulai lagi.", nil)
		return
	}

	methods, ok := xendit.PaymentChannels[data]
	if !ok {
		b.reply(r, "❌ Metode pembayaran tidak valid atau belum tersedia.", nil)
		return
	}
	if len(s.Cart) == 0 {
		b.reply(r, "❌ Keranjang Anda kosong! Silakan tambahkan produk terlebih dahulu.", nil)
		s.ResetCheckout()
		return
	}
	t := b.totals(s)
	if t.Bayar <= 0 {
		b.reply(r, "❌ Total pembayaran tidak valid. Silakan ulangi pemesanan.", nil)
		return
	}

	// Remove the method buttons so the user cannot create two invoices.
	b.replyOrEdit(r, summary(s, t)+"\n\n✅ Metode pembayaran: "+metodeLabel[data]+"\nMohon tunggu instruksi pembayaran dikirim...", nil)

	externalID := fmt.Sprintf("invoice-%d-%d", r.uid, time.Now().Unix())
	inv, err := b.Xendit.CreateInvoice(r.ctx, externalID, t.Bayar, "Pembayaran LaukPauk", methods)
	if err != nil {
		log.Printf("create invoice for %d: %v", r.uid, err)
		b.reply(r, "❌ Gagal membuat invoice. Coba lagi nanti.", paymentKB)
		return
	}
	b.State.AddPending(b.snapshot(r.uid, s, t, inv.ID, externalID, inv.InvoiceURL))
	s.InvoiceID, s.InvoiceURL = inv.ID, inv.InvoiceURL
	s.CheckoutStep = state.StepInvoiceDikirim

	b.reply(r, fmt.Sprintf("💰 Total: %s\nSilakan selesaikan pembayaran melalui tombol di bawah. Invoice akan dikirim setelah pembayaran diterima.", money.Rupiah(t.Bayar)), payNowKB(inv.InvoiceURL))
}

// batalPesanan cancels the current order (command /batal or the button).
func (b *Bot) batalPesanan(r *req) {
	s := r.sess
	if s.InvoiceID != "" {
		inv, err := b.Xendit.GetInvoice(r.ctx, s.InvoiceID)
		if err == nil && inv.IsPaid() {
			b.reply(r, "✅ Pembayaran untuk pesanan ini sudah diterima, jadi pesanan tidak bisa dibatalkan. Hubungi admin jika ada kendala.", nil)
			return
		}
		if err == nil && inv.Status == xendit.StatusPending {
			if err := b.Xendit.ExpireInvoice(r.ctx, s.InvoiceID); err != nil {
				log.Printf("expire invoice %s: %v", s.InvoiceID, err)
			}
		} else if err != nil {
			log.Printf("cek invoice %s saat batal: %v", s.InvoiceID, err)
		}
		b.State.DropPending(s.InvoiceID)
	}
	s.ResetOrder()
	b.replyOrEdit(r, "✅ Pesanan dibatalkan. Anda bisa mulai kembali dengan /produk", nil)
}

// HandleInvoiceCallback processes a Xendit invoice callback payload.
func (b *Bot) HandleInvoiceCallback(ctx context.Context, inv xendit.Invoice) {
	switch {
	case inv.IsPaid():
		p, ok := b.State.ClaimPending(inv.ID)
		if !ok {
			log.Printf("xendit: invoice %s tidak dikenal atau sudah diproses", inv.ID)
			return
		}
		if paid := int64(inv.PaidAmount); paid > 0 && paid < p.Totals.Bayar {
			log.Printf("xendit: invoice %s dibayar %d, tagihan %d", inv.ID, paid, p.Totals.Bayar)
		}
		b.send(p.UserID, "✅ Pembayaran Anda berhasil! Pesanan Anda sedang diproses. 🚀", nil)
		b.completeOrder(ctx, p, false)
		// Webhooks never hold a user lock, so taking it here is safe.
		b.State.Update(p.UserID, func(s *state.Session) {
			if s.InvoiceID == p.InvoiceID {
				s.ResetOrder()
			}
		})
	case inv.Status == xendit.StatusExpired:
		p, ok := b.State.Pending(inv.ID)
		if !ok {
			return
		}
		b.State.DropPending(inv.ID)
		reset := false
		b.State.Update(p.UserID, func(s *state.Session) {
			if s.InvoiceID == inv.ID {
				s.ResetCheckout()
				reset = true
			}
		})
		if reset {
			b.send(p.UserID, "⌛ Invoice pembayaran Anda sudah kedaluwarsa. Keranjang Anda masih tersimpan — ketik /checkout untuk memesan lagi.", nil)
		}
	default:
		log.Printf("xendit: invoice %s status %s diabaikan", inv.ID, inv.Status)
	}
}

// completeOrder records a paid order: points, database, Google Sheets, PDF
// invoice for the customer and a shipping prompt for the admin.
func (b *Bot) completeOrder(ctx context.Context, p *state.PendingOrder, viaPoin bool) {
	uid := p.UserID
	now := b.now()
	t := p.Totals

	earned := poin.Hitung(t.Belanja)
	old, err := b.DB.GetPoin(ctx, uid)
	if err != nil {
		log.Printf("get poin %d: %v", uid, err)
	}
	newTotal := max(old+earned-t.Poin, 0)

	if err := b.DB.SavePelanggan(ctx, store.Pelanggan{
		UserID: uid, Nama: p.Nama, Alamat: p.Alamat, HP: p.HP, DetailAlamat: p.DetailAlamat,
		ProdukTerakhir: p.Cart, TotalPoin: newTotal,
	}); err != nil {
		log.Printf("simpan pelanggan %d: %v", uid, err)
		b.send(b.cfg.AdminUserID, fmt.Sprintf("⚠️ Gagal menyimpan data pelanggan %d (%s) ke database: %v", uid, p.Nama, err), nil)
	}
	if _, err := b.DB.SetStatusPesanan(ctx, uid, "Diproses"); err != nil {
		log.Printf("status pesanan %d: %v", uid, err)
	}
	b.mirrorOrder(ctx, p, newTotal, now)

	no := invoice.Number(uid, now)
	pdf, err := invoice.Render(invoice.Data{
		Number: no, Date: now, Nama: p.Nama, Alamat: p.Alamat, DetailAlamat: p.DetailAlamat,
		HP: p.HP, WaktuPengiriman: p.WaktuPengiriman, Cart: p.Cart, Totals: t, Voucher: p.Voucher,
	}, b.Logo)
	fileName := invoice.FileName(uid, now)
	if err != nil {
		log.Printf("render invoice %d: %v", uid, err)
	} else {
		b.sendDocument(uid, fileName, pdf, fmt.Sprintf("🧾 Invoice Pembelian Anda\n💰 Total: %s\n📦 Terima kasih telah berbelanja!", money.Rupiah(t.Bayar)))
		if b.cfg.InvoiceDir != "" {
			if err := os.MkdirAll(b.cfg.InvoiceDir, 0o755); err == nil {
				if err := os.WriteFile(filepath.Join(b.cfg.InvoiceDir, fileName), pdf, 0o644); err != nil {
					log.Printf("simpan invoice: %v", err)
				}
			}
		}
	}

	b.send(uid, fmt.Sprintf("🎉 Anda mendapatkan %s poin dari transaksi ini!\n💰 Total poin Anda sekarang: %s\n\n🛍️ Poin bisa digunakan untuk transaksi selanjutnya!",
		money.Thousands(earned), money.Thousands(newTotal)), nil)

	// Admin notification.
	admin := b.cfg.AdminUserID
	head := "✅ Pembayaran Lunas!"
	if viaPoin {
		head = "✅ Pesanan dibayar penuh dengan poin!"
	}
	if pdf != nil {
		b.sendDocument(admin, fileName, pdf, fmt.Sprintf("%s\n📢 Pesanan baru dari %s (%s)", head, p.Nama, money.Rupiah(t.Bayar)))
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "📢 Pesanan Baru Masuk!\n🧾 Invoice No: %s\n👤 Nama: %s\n📍 Alamat: %s\n📌 Detail: %s\n📱 No. HP: %s\n🚚 Pengiriman: %s\n",
		no, p.Nama, p.Alamat, p.DetailAlamat, p.HP, p.WaktuPengiriman)
	fmt.Fprintf(&sb, "🚚 Ongkos Kirim: %s\n", money.Rupiah(t.Ongkir))
	if t.DiskonVoucher > 0 {
		fmt.Fprintf(&sb, "🎟️ Voucher %s: - %s\n", p.Voucher, money.Rupiah(t.DiskonVoucher))
	}
	if t.Poin > 0 {
		fmt.Fprintf(&sb, "⭐ Poin: - %s\n", money.Rupiah(t.Poin))
	}
	fmt.Fprintf(&sb, "💰 Total Pembayaran: %s\n📦 Produk:\n", money.Rupiah(t.Bayar))
	for _, name := range p.Cart.Names() {
		it := p.Cart[name]
		fmt.Fprintf(&sb, "- %s x%d @ %s\n", catalog.Display(name), it.Jumlah, money.Rupiah(it.Net()))
	}
	text := sb.String()
	if len(text) > 3900 {
		text = text[:3900] + "\n…"
	}
	b.send(admin, text, kb(row(btn("🚚 Kirim Barang", fmt.Sprintf("kirim_barang:%d", uid)))))
}

// mirrorOrder copies the order to Google Sheets (best effort).
func (b *Bot) mirrorOrder(ctx context.Context, p *state.PendingOrder, totalPoin int64, now time.Time) {
	if b.Sheets == nil {
		return
	}
	if err := b.Sheets.UpsertPelanggan(ctx, sheets.Pelanggan{
		UserID: p.UserID, Nama: p.Nama, Alamat: p.Alamat, HP: p.HP, DetailAlamat: p.DetailAlamat,
		ProdukTerakhir: p.Cart.JSON(), TotalPoin: totalPoin,
	}); err != nil {
		log.Printf("sheets pelanggan %d: %v", p.UserID, err)
	}
	if err := b.Sheets.SetStatusPesanan(ctx, p.UserID, "Diproses"); err != nil {
		log.Printf("sheets status %d: %v", p.UserID, err)
	}
	if err := b.Sheets.AppendPesanan(ctx, sheets.Pesanan{
		UserID: p.UserID, Nama: p.Nama, Alamat: p.Alamat, HP: p.HP, KodeVoucher: p.Voucher,
		DiskonVoucher: p.Totals.DiskonVoucher, PoinDigunakan: p.Totals.Poin, TotalBelanja: p.Totals.Belanja,
		OngkosKirim: p.Totals.Ongkir, TotalPembayaran: p.Totals.Bayar, WaktuPesanan: now.Format("2006-01-02 15:04:05"),
	}); err != nil {
		log.Printf("sheets pesanan %d: %v", p.UserID, err)
	}
}
