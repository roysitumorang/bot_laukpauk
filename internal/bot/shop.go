package bot

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/roysitumorang/bot_laukpauk/internal/catalog"
	"github.com/roysitumorang/bot_laukpauk/internal/money"
	"github.com/roysitumorang/bot_laukpauk/internal/order"
	"github.com/roysitumorang/bot_laukpauk/internal/poin"
	"github.com/roysitumorang/bot_laukpauk/internal/state"
	"github.com/roysitumorang/bot_laukpauk/internal/xendit"
)

const (
	maxSearchResults = 30
	buttonsPerMsg    = 50
)

func (b *Bot) cmdStart(r *req) {
	r.sess.CheckoutStep = clearIfNotPaying(r.sess.CheckoutStep)
	p, err := b.DB.GetPelanggan(r.ctx, r.uid)
	if err != nil {
		log.Printf("get pelanggan %d: %v", r.uid, err)
	}
	help := "\n\nPerintah: /kategori, /keranjang, /checkout, /pesan_ulang, /status_pesanan"
	if p != nil {
		r.sess.Nama, r.sess.Alamat, r.sess.HP = p.Nama, p.Alamat, p.HP
		r.sess.DetailAlamat, r.sess.TotalPoin = p.DetailAlamat, p.TotalPoin
		b.reply(r, fmt.Sprintf("👋 Selamat datang kembali, %s!\n\n✅ langsung cuss belanja!%s", p.Nama, help), nil)
		return
	}
	b.reply(r, "👋 Selamat datang di Bot Laukpauk!\nKetik aja produk yang ingin dibeli ya. Mis: Brokoli"+help, nil)
}

// clearIfNotPaying keeps the invoice_dikirim step (an unpaid invoice must be
// paid or cancelled explicitly) and clears any other checkout step.
func clearIfNotPaying(step string) string {
	if step == state.StepInvoiceDikirim {
		return step
	}
	return ""
}

// blockedByInvoice reports (and tells the user) whether an unpaid invoice is
// still open. Expired or failed invoices are cleaned up so the user can
// continue shopping.
func (b *Bot) blockedByInvoice(r *req) bool {
	if r.sess.CheckoutStep != state.StepInvoiceDikirim || r.sess.InvoiceID == "" {
		if r.sess.CheckoutStep == state.StepInvoiceDikirim {
			r.sess.CheckoutStep = ""
		}
		return false
	}
	inv, err := b.Xendit.GetInvoice(r.ctx, r.sess.InvoiceID)
	if err != nil {
		log.Printf("cek invoice %s: %v", r.sess.InvoiceID, err)
		b.warnCheckoutInProgress(r)
		return true
	}
	switch {
	case inv.Status == xendit.StatusPending:
		markup := kb(
			row(tgbotapi.NewInlineKeyboardButtonURL("🔗 Bayar Sekarang", r.sess.InvoiceURL)),
			row(btn("❌ Batalkan Pesanan", "batal_pesanan")),
		)
		if r.sess.InvoiceURL == "" {
			markup = batalKB
		}
		b.reply(r, "⚠️ Kamu sedang dalam proses checkout.\nSelesaikan pesanan, atau buat pesanan baru jika ingin mulai ulang.", markup)
		return true
	case inv.IsPaid():
		b.reply(r, "✅ Pembayaran Anda sudah kami terima dan pesanan sedang diproses. Tunggu sebentar ya.", nil)
		return true
	default: // EXPIRED or anything else
		b.State.DropPending(r.sess.InvoiceID)
		r.sess.ResetCheckout()
		return false
	}
}

func (b *Bot) cmdKategori(r *req) {
	if b.blockedByInvoice(r) {
		return
	}
	cats := b.Catalog.Categories()
	if len(cats) == 0 {
		b.reply(r, "⚠️ Data produk tidak valid atau kosong. Coba muat ulang bot.", nil)
		return
	}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, c := range cats {
		label := c
		if label == "" {
			label = "Lainnya"
		}
		rows = append(rows, row(btn(label, "kat:"+catalog.Key(c))))
	}
	b.reply(r, "📦 Silakan pilih kategori:", kb(rows...))
}

func (b *Bot) cbKategori(r *req, key string) {
	b.answer(r, "")
	kat, ok := b.Catalog.CategoryByKey(key)
	if !ok {
		b.reply(r, "⚠️ Kategori tidak ditemukan.", nil)
		return
	}
	ps := b.Catalog.InCategory(kat)
	for i := 0; i < len(ps); i += buttonsPerMsg {
		end := min(i+buttonsPerMsg, len(ps))
		var rows [][]tgbotapi.InlineKeyboardButton
		for _, p := range ps[i:end] {
			rows = append(rows, row(btn(fmt.Sprintf("%s - %s", catalog.Display(p.Nama), money.Rupiah(p.Net())), "prd:"+catalog.Key(p.Nama))))
		}
		title := fmt.Sprintf("🛍️ Produk dalam kategori %s:", kat)
		if i > 0 {
			title = fmt.Sprintf("🛍️ Produk dalam kategori %s (lanjutan):", kat)
		}
		b.reply(r, title, kb(rows...))
	}
}

func (b *Bot) cbProduk(r *req, key string) {
	b.answer(r, "")
	p, ok := b.Catalog.ByKey(key)
	if !ok {
		b.reply(r, "⚠️ Produk tidak ditemukan atau sudah dihapus.", nil)
		return
	}
	harga := money.Rupiah(p.Net())
	if p.Diskon > 0 {
		harga += fmt.Sprintf(" (%s)", money.Rupiah(p.Harga))
	}
	b.reply(r,
		fmt.Sprintf("Produk: %s\nHarga: %s\nStok: %d pcs\n\nGunakan tombol di bawah untuk beli cepat:", catalog.Display(p.Nama), harga, p.Stok),
		kb(row(btn("Beli Cepat", "beli:"+key+":1"))))
}

func (b *Bot) cariProduk(r *req, query string) {
	if b.blockedByInvoice(r) {
		return
	}
	if b.shopClosed(r, "Mohon lakukan pemesanan di luar tanggal ini.") {
		return
	}
	if b.Catalog.Len() == 0 {
		b.reply(r, "⚠️ Data produk tidak valid atau kosong.", nil)
		return
	}
	hasil := b.Catalog.Search(query)
	switch len(hasil) {
	case 0:
		b.reply(r, "❌ Produk tidak ditemukan. Pastikan ejaan benar atau coba nama produk lain.", nil)
		return
	case 1:
		p := hasil[0]
		b.reply(r,
			fmt.Sprintf("%s - %s (Diskon %s)", catalog.Display(p.Nama), money.Rupiah(p.Net()), money.Rupiah(p.Diskon)),
			kb(row(btn("🛒 Beli "+catalog.Display(p.Nama), "beli:"+catalog.Key(p.Nama)+":1"))))
		return
	}
	title := "🔍 Beberapa produk ditemukan, pilih yang ingin Anda beli:"
	if len(hasil) > maxSearchResults {
		title = fmt.Sprintf("🔍 Ditemukan %d produk, menampilkan %d pertama. Ketik nama yang lebih spesifik untuk mempersempit:", len(hasil), maxSearchResults)
		hasil = hasil[:maxSearchResults]
	}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, p := range hasil {
		rows = append(rows, row(btn(fmt.Sprintf("%s - %s", catalog.Display(p.Nama), money.Rupiah(p.Net())), "beli:"+catalog.Key(p.Nama)+":1")))
	}
	b.reply(r, title, kb(rows...))
}

func (b *Bot) cbBeli(r *req, arg string) {
	key, qtyStr, _ := strings.Cut(arg, ":")
	qty, err := strconv.ParseInt(qtyStr, 10, 64)
	if err != nil || qty <= 0 {
		b.answer(r, "")
		b.reply(r, "⚠️ Format data tidak valid. Coba lagi.", nil)
		return
	}
	if r.sess.CheckoutStep == state.StepInvoiceDikirim && b.blockedByInvoice(r) {
		b.answer(r, "")
		return
	}
	p, ok := b.Catalog.ByKey(key)
	if !ok {
		b.answer(r, "")
		b.reply(r, "⚠️ Produk tidak ditemukan atau sudah dihapus.", nil)
		return
	}
	if r.sess.Cart == nil {
		r.sess.Cart = order.Cart{}
	}
	it := r.sess.Cart[p.Nama]
	if it.Jumlah+qty > p.Stok {
		sisa := max(p.Stok-it.Jumlah, 0)
		b.alert(r, fmt.Sprintf("⚠️ Stok %s tidak mencukupi! Sisa stok: %d pcs", catalog.Display(p.Nama), sisa))
		return
	}
	b.answer(r, "✅ Ditambahkan ke keranjang")
	it.Jumlah += qty
	it.Harga, it.Diskon = p.Harga, p.Diskon
	r.sess.Cart[p.Nama] = it
	b.reply(r, fmt.Sprintf("✅ %d %s berhasil ditambahkan ke keranjang! Klik /keranjang untuk melihat belanjaan Anda.", qty, catalog.Display(p.Nama)), nil)
}

func cartLines(c order.Cart) string {
	var sb strings.Builder
	for _, name := range c.Names() {
		it := c[name]
		fmt.Fprintf(&sb, "- %s: %d x %s = %s\n", catalog.Display(name), it.Jumlah, money.Rupiah(it.Net()), money.Rupiah(it.Subtotal()))
	}
	return sb.String()
}

func (b *Bot) cmdKeranjang(r *req) {
	c := r.sess.Cart
	if len(c) == 0 {
		b.reply(r, "Keranjang Anda kosong!", nil)
		return
	}
	total := c.Total()
	var sb strings.Builder
	sb.WriteString("🛍 Keranjang Anda:\n")
	sb.WriteString(cartLines(c))
	fmt.Fprintf(&sb, "\n💰 Total: %s\n", money.Rupiah(total))
	fmt.Fprintf(&sb, "🎯 Anda akan mendapatkan %s poin dari belanjaan ini.", money.Thousands(poin.Hitung(total)))
	if tambah, p, ok := poin.Rekomendasi(total); ok {
		fmt.Fprintf(&sb, "\n\n💡 Dengan menambah belanja %s, Anda bisa mendapatkan %s poin!", money.Rupiah(tambah), money.Thousands(p))
	}
	b.reply(r, sb.String(), kb(
		row(btn("🛒 Edit Keranjang", "edit_keranjang")),
		row(btn("✅ Checkout", "checkout")),
	))
}

func (b *Bot) showEditKeranjang(r *req) {
	c := r.sess.Cart
	if len(c) == 0 {
		b.replyOrEdit(r, "⚠️ Keranjang Anda kosong! Tambahkan produk terlebih dahulu.", nil)
		return
	}
	var rows [][]tgbotapi.InlineKeyboardButton
	for _, name := range c.Names() {
		k := catalog.Key(name)
		d := catalog.Display(name)
		rows = append(rows,
			row(btn("➖ Kurangi "+d, "kurang:"+k), btn("➕ Tambah "+d, "tambah:"+k)),
			row(btn("❌ Hapus "+d, "hapus:"+k)),
		)
	}
	rows = append(rows, row(btn("✅ Selesai Edit", "proses_checkout")))
	text := "🛍 Keranjang Anda:\n" + cartLines(c) + fmt.Sprintf("\n💰 Total: %s\n\n⚙️ Gunakan tombol di bawah untuk edit pesanan:", money.Rupiah(c.Total()))
	b.replyOrEdit(r, text, kb(rows...))
}

func (b *Bot) cbEditItem(r *req, action, key string) {
	var name string
	for n := range r.sess.Cart {
		if catalog.Key(n) == key {
			name = n
			break
		}
	}
	if name == "" {
		b.answer(r, "⚠️ Produk tidak ada di keranjang.")
		b.showEditKeranjang(r)
		return
	}
	it := r.sess.Cart[name]
	switch action {
	case "tambah":
		if p, ok := b.Catalog.Find(name); ok && it.Jumlah+1 > p.Stok {
			b.alert(r, fmt.Sprintf("⚠️ Stok %s tidak mencukupi! Sisa stok: %d pcs", catalog.Display(name), p.Stok))
			return
		}
		it.Jumlah++
		r.sess.Cart[name] = it
	case "kurang":
		if it.Jumlah > 1 {
			it.Jumlah--
			r.sess.Cart[name] = it
		} else {
			delete(r.sess.Cart, name)
		}
	case "hapus":
		delete(r.sess.Cart, name)
	}
	b.answer(r, "")
	if len(r.sess.Cart) == 0 {
		b.replyOrEdit(r, "🛒 Keranjang Anda sekarang kosong. Ketik nama produk untuk mulai belanja lagi.", nil)
		return
	}
	b.showEditKeranjang(r)
}

func (b *Bot) cmdPesanUlang(r *req) {
	if b.blockedByInvoice(r) {
		return
	}
	if b.shopClosed(r, "Anda tidak bisa melakukan pemesanan saat ini.") {
		return
	}
	p, err := b.DB.GetPelanggan(r.ctx, r.uid)
	if err != nil {
		log.Printf("get pelanggan %d: %v", r.uid, err)
		b.reply(r, "⚠️ Data pesanan sebelumnya tidak bisa dibaca. Coba lagi nanti.", nil)
		return
	}
	if p == nil || len(p.ProdukTerakhir) == 0 {
		b.reply(r, "⚠️ Anda belum memiliki pesanan sebelumnya.", nil)
		return
	}
	if r.sess.Cart == nil {
		r.sess.Cart = order.Cart{}
	}
	var notes []string
	added := 0
	for _, name := range p.ProdukTerakhir.Names() {
		last := p.ProdukTerakhir[name]
		cur, ok := b.Catalog.Find(name)
		if !ok {
			notes = append(notes, fmt.Sprintf("• %s sudah tidak tersedia", catalog.Display(name)))
			continue
		}
		it := r.sess.Cart[cur.Nama]
		want := max(it.Jumlah, last.Jumlah) // avoid doubling if already in cart
		if want > cur.Stok {
			if cur.Stok <= 0 {
				notes = append(notes, fmt.Sprintf("• %s sedang habis", catalog.Display(name)))
				continue
			}
			notes = append(notes, fmt.Sprintf("• %s hanya tersisa %d pcs", catalog.Display(name), cur.Stok))
			want = cur.Stok
		}
		it.Jumlah, it.Harga, it.Diskon = want, cur.Harga, cur.Diskon
		r.sess.Cart[cur.Nama] = it
		added++
	}
	if added == 0 {
		b.reply(r, "⚠️ Tidak ada produk sebelumnya yang bisa dipesan ulang.\n"+strings.Join(notes, "\n"), nil)
		return
	}
	msg := "🔄 Produk dari pesanan terakhir telah ditambahkan ke keranjang!\nKetik /keranjang untuk melihat belanjaan Anda."
	if len(notes) > 0 {
		msg += "\n\nCatatan:\n" + strings.Join(notes, "\n")
	}
	b.reply(r, msg, nil)
}
