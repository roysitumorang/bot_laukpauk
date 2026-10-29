package bot

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/roysitumorang/bot_laukpauk/internal/delivery"
	"github.com/roysitumorang/bot_laukpauk/internal/gmaps"
	"github.com/roysitumorang/bot_laukpauk/internal/money"
	"github.com/roysitumorang/bot_laukpauk/internal/order"
	"github.com/roysitumorang/bot_laukpauk/internal/poin"
	"github.com/roysitumorang/bot_laukpauk/internal/state"
	tg "github.com/roysitumorang/bot_laukpauk/internal/telegram"
)

// startCheckout begins checkout from /checkout or the Checkout button.
func (b *Bot) startCheckout(r *req) {
	if b.blockedByInvoice(r) {
		return
	}
	if len(r.sess.Cart) == 0 {
		b.replyOrEdit(r, "❌ Keranjang Anda kosong! Silakan tambahkan produk terlebih dahulu.", nil)
		return
	}
	if b.shopClosed(r, "Mohon lakukan pemesanan di luar tanggal ini.") {
		return
	}
	r.sess.ResetCheckout()

	p, err := b.DB.GetPelanggan(r.ctx, r.uid)
	if err != nil {
		log.Printf("get pelanggan %d: %v", r.uid, err)
	}
	if p != nil && p.Nama != "" && p.Alamat != "" {
		r.sess.Nama, r.sess.Alamat, r.sess.HP = p.Nama, p.Alamat, p.HP
		r.sess.DetailAlamat = p.DetailAlamat
		if r.sess.DetailAlamat == "" {
			r.sess.DetailAlamat = "TIDAK DIISI"
		}
		b.replyOrEdit(r, fmt.Sprintf(
			"🔄 Data Anda yang tersimpan:\n👤 Nama: %s\n📍 Alamat: %s\n📌 Detail: %s\n📱 No. HP: %s\n\nSilakan pilih opsi di bawah ini:",
			r.sess.Nama, r.sess.Alamat, r.sess.DetailAlamat, r.sess.HP),
			kb(row(btn("✅ Gunakan Alamat ini", "confirm_old_data")), row(btn("✏️ Ubah Data", "edit_data"))))
		return
	}
	r.sess.CheckoutStep = state.StepNama
	b.replyOrEdit(r, "Silakan isi data berikut:\n📌 Nama:", nil)
}

func (b *Bot) cbCheckoutOption(r *req, data string) {
	b.answer(r, "")
	if r.sess.CheckoutStep != "" {
		// Buttons from an earlier checkout message.
		b.reply(r, "⚠️ Pilihan ini sudah tidak berlaku. Ketik /checkout untuk memulai lagi.", nil)
		return
	}
	if len(r.sess.Cart) == 0 {
		b.replyOrEdit(r, "❌ Keranjang Anda kosong! Silakan tambahkan produk terlebih dahulu.", nil)
		return
	}
	if data == "edit_data" {
		r.sess.CheckoutStep = state.StepNama
		b.replyOrEdit(r, "✏️ Silakan masukkan data baru.", nil)
		b.reply(r, "Nama:", nil)
		return
	}

	p, err := b.DB.GetPelanggan(r.ctx, r.uid)
	if err != nil || p == nil {
		if err != nil {
			log.Printf("get pelanggan %d: %v", r.uid, err)
		}
		r.sess.CheckoutStep = state.StepNama
		b.replyOrEdit(r, "⚠️ Data lama tidak ditemukan. Silakan isi data berikut:\n📌 Nama:", nil)
		return
	}
	r.sess.Nama, r.sess.Alamat, r.sess.HP = p.Nama, p.Alamat, p.HP
	r.sess.DetailAlamat = p.DetailAlamat
	if r.sess.DetailAlamat == "" {
		r.sess.DetailAlamat = "-"
	}
	r.sess.TotalPoin = p.TotalPoin

	km, err := b.Maps.DrivingKm(r.ctx, b.cfg.StoreLat, b.cfg.StoreLng, p.Alamat)
	if err != nil {
		log.Printf("distance for %d: %v", r.uid, err)
		b.replyOrEdit(r, "❌ Gagal menghitung jarak pengiriman. Pastikan alamat Anda valid.",
			kb(row(btn("✏️ Ubah Data", "edit_data"))))
		return
	}
	r.sess.JarakKm = km
	r.sess.Ongkir = b.ongkir(km)
	b.replyOrEdit(r, fmt.Sprintf("✅ Data lama digunakan. Melanjutkan ke checkout...\n🚚 Jarak: %.2f km\n💰 Ongkos kirim: %s",
		km, money.Rupiah(r.sess.Ongkir)), nil)
	b.showVoucher(r)
}

func (b *Bot) ongkir(km float64) int64 { return int64(km * float64(b.cfg.OngkosPerKm)) }

var phoneRe = regexp.MustCompile(`^\+?[0-9][0-9 \-]{6,18}[0-9]$`)

func validPhone(s string) bool {
	if !phoneRe.MatchString(s) {
		return false
	}
	digits := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			digits++
		}
	}
	return digits >= 8 && digits <= 15
}

func (b *Bot) checkoutInput(r *req, text string) {
	s := r.sess
	switch s.CheckoutStep {
	case state.StepNama:
		if text == "" || len([]rune(text)) > 100 {
			b.reply(r, "❌ Nama tidak valid. Masukkan nama Anda:", nil)
			return
		}
		s.Nama = text
		s.CheckoutStep = state.StepAlamat
		b.reply(r, "📍 Masukkan Alamat Anda (mis. Jalan Setia Budi no.99 Medan - Wajib pakai nama kota):", nil)

	case state.StepAlamat:
		s.AlamatRaw = text
		alamat, err := b.Maps.Geocode(r.ctx, text)
		if err != nil {
			if !errors.Is(err, gmaps.ErrNotFound) {
				log.Printf("geocode %q: %v", text, err)
			}
			b.reply(r, "❌ Alamat tidak valid. Coba masukkan kembali.", nil)
			return
		}
		km, err := b.Maps.DrivingKm(r.ctx, b.cfg.StoreLat, b.cfg.StoreLng, alamat)
		if err != nil {
			log.Printf("distance %q: %v", alamat, err)
			b.reply(r, "❌ Gagal menghitung jarak pengiriman ke alamat tersebut. Coba tulis alamat yang lebih lengkap.", nil)
			return
		}
		s.Alamat, s.JarakKm, s.Ongkir = alamat, km, b.ongkir(km)
		s.CheckoutStep = state.StepDetailAlamat
		b.reply(r, fmt.Sprintf(
			"📍 Alamat berdasarkan Google Maps: %s\n🚚 Perkiraan jarak: %.2f km\n💰 Ongkos kirim: %s\n\n"+
				"🏠 Silakan tambahkan alamat detail Anda (contoh: rumah no.10 warna orange, dekat minimarket, lantai 2, dll.):",
			alamat, km, money.Rupiah(s.Ongkir)), nil)

	case state.StepDetailAlamat:
		s.DetailAlamat = text
		if s.DetailAlamat == "" {
			s.DetailAlamat = "TIDAK DIISI"
		}
		s.CheckoutStep = state.StepHP
		b.reply(r, fmt.Sprintf(
			"🏡 Alamat Utama: %s\n📌 Detail Alamat: %s\n🚚 Jarak: %.2f km\n💰 Ongkos Kirim: %s\n\n📱 Masukkan Nomor HP Anda:",
			s.Alamat, s.DetailAlamat, s.JarakKm, money.Rupiah(s.Ongkir)), nil)

	case state.StepHP:
		if !validPhone(text) {
			b.reply(r, "❌ Nomor HP tidak valid. Contoh: 081234567890. Coba lagi:", nil)
			return
		}
		s.HP = text
		b.reply(r, "✅ Nomor HP telah disimpan. Silakan lanjut ke langkah berikutnya.", nil)
		if s.Voucher != "" {
			b.reply(r, "⚠️ Anda sudah menggunakan voucher. Lanjut ke langkah berikutnya.", nil)
			b.askPoin(r)
			return
		}
		b.showVoucher(r)

	case state.StepPoin:
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil || n < 0 {
			b.reply(r, "❌ Masukkan angka yang valid (contoh: 0, 10000, 20000). Coba lagi:", nil)
			return
		}
		if n > s.TotalPoin {
			b.reply(r, fmt.Sprintf("❌ Anda hanya memiliki %s poin. Masukkan jumlah yang sesuai.", money.Thousands(s.TotalPoin)), nil)
			return
		}
		if n > 0 && n < poin.MinTukar {
			b.reply(r, "❌ Penukaran poin minimal 10.000. Masukkan jumlah yang valid atau ketik angka 0 jika tidak ingin menggunakan poin.", nil)
			return
		}
		s.Poin = n
		b.showDelivery(r)
	}
}

func (b *Bot) showVoucher(r *req) {
	total := r.sess.Cart.Total()
	vs := b.State.Vouchers()
	codes := make([]string, 0, len(vs))
	for k, v := range vs {
		if total >= v.MinBelanja {
			codes = append(codes, k)
		}
	}
	sort.Strings(codes)
	r.sess.CheckoutStep = state.StepVoucher
	if len(codes) == 0 {
		b.reply(r, "❌ Tidak ada voucher yang tersedia. Klik 'Lanjut' untuk melewati.", kb(row(btn("Lanjut", "skip_voucher"))))
		return
	}
	var rows [][]tg.InlineKeyboardButton
	for _, k := range codes {
		v := vs[k]
		label := fmt.Sprintf("%s - Diskon %s", k, money.Rupiah(v.Nilai))
		if v.Tipe == order.TipeOngkir {
			label = fmt.Sprintf("%s - Potongan ongkir %s", k, money.Rupiah(v.Nilai))
		}
		rows = append(rows, row(btn(label, "voucher:"+k)))
	}
	rows = append(rows, row(btn("Lewati ➡️", "skip_voucher")))
	b.reply(r, "🎟️ Pilih voucher yang tersedia:", kb(rows...))
}

func (b *Bot) cbVoucher(r *req, kode string) {
	if r.sess.CheckoutStep != state.StepVoucher {
		b.alert(r, "⚠️ Voucher hanya bisa dipilih saat checkout. Ketik /checkout untuk memulai.")
		return
	}
	v, ok := b.State.Voucher(kode)
	if !ok {
		b.alert(r, "❌ Voucher tidak valid!")
		return
	}
	if r.sess.Cart.Total() < v.MinBelanja {
		b.alert(r, fmt.Sprintf("⚠️ Minimal belanja %s untuk voucher ini!", money.Rupiah(v.MinBelanja)))
		return
	}
	b.answer(r, "")
	r.sess.Voucher = kode
	b.replyOrEdit(r, fmt.Sprintf("🎟️ Voucher %s telah digunakan! Diskon: %s", kode, money.Rupiah(v.Nilai)), nil)
	b.askPoin(r)
}

func (b *Bot) cbSkipVoucher(r *req) {
	if r.sess.CheckoutStep != state.StepVoucher {
		b.alert(r, "⚠️ Langkah ini sudah lewat. Ketik /checkout untuk memulai lagi.")
		return
	}
	b.answer(r, "")
	r.sess.Voucher = ""
	b.replyOrEdit(r, "📌 Anda memilih untuk melewati voucher. Lanjut ke poin.", nil)
	b.askPoin(r)
}

// askPoin shows the point balance and asks how many to redeem. Customers
// below the redemption minimum go straight to the delivery schedule.
func (b *Bot) askPoin(r *req) {
	total, err := b.DB.GetPoin(r.ctx, r.uid)
	if err != nil {
		log.Printf("get poin %d: %v", r.uid, err)
		total = r.sess.TotalPoin
	}
	r.sess.TotalPoin = total
	r.sess.Poin = 0
	switch {
	case total == 0:
		b.reply(r, "🔷 Anda belum memiliki poin. Poin akan dikumpulkan setelah transaksi ini.", nil)
		b.showDelivery(r)
	case total < poin.MinTukar:
		b.reply(r, fmt.Sprintf("📑 Poin Anda: %s\n❌ Penukaran poin minimal 10.000.", money.Thousands(total)), nil)
		b.showDelivery(r)
	default:
		r.sess.CheckoutStep = state.StepPoin
		b.reply(r, fmt.Sprintf("📑 Poin Anda: %s\n✅ Masukkan jumlah poin yang ingin ditukarkan.\n\n💳 Masukkan Poin yang ingin digunakan (jika tidak, ketik angka 0):",
			money.Thousands(total)), nil)
	}
}

func (b *Bot) showDelivery(r *req) {
	r.sess.CheckoutStep = state.StepWaktuPengiriman
	slots := delivery.Slots(b.now(), b.Holidays.IsHoliday)
	if len(slots) == 0 {
		b.reply(r, fmt.Sprintf("🚨 Belum ada jadwal pengiriman yang tersedia (toko libur: %s). Silakan coba lagi nanti atau /batal.", b.Holidays.Describe()), batalKB)
		return
	}
	var rows [][]tg.InlineKeyboardButton
	for _, s := range slots {
		rows = append(rows, row(btn(s.Label, s.Data)))
	}
	b.reply(r, "🚚 Pilih waktu pengiriman pesanan Anda:", kb(rows...))
}

func (b *Bot) cbDelivery(r *req, data string) {
	switch r.sess.CheckoutStep {
	case state.StepWaktuPengiriman, state.StepMetodeBayar:
	case state.StepInvoiceDikirim, state.StepDone:
		b.alert(r, "✅ Checkout sudah selesai. Anda tidak bisa mengubah waktu pengiriman lagi.")
		return
	default:
		b.alert(r, "⚠️ Sesi checkout sudah berakhir. Ketik /checkout untuk memulai lagi.")
		return
	}
	valid := false
	for _, s := range delivery.Slots(b.now(), b.Holidays.IsHoliday) {
		if s.Data == data {
			valid = true
			break
		}
	}
	if !valid {
		b.answer(r, "")
		b.replyOrEdit(r, "⚠️ Jadwal tersebut sudah tidak tersedia. Silakan pilih lagi.", nil)
		b.showDelivery(r)
		return
	}
	b.answer(r, "")
	r.sess.WaktuPengiriman = delivery.LabelFromData(data)
	b.replyOrEdit(r, "🚚 Anda memilih pengiriman pada: "+r.sess.WaktuPengiriman, nil)
	b.processCheckout(r)
}

// totals computes what the user owes for the current session.
func (b *Bot) totals(s *state.Session) order.Totals {
	var vp *order.Voucher
	if s.Voucher != "" {
		if v, ok := b.State.Voucher(s.Voucher); ok {
			vp = &v
		}
	}
	return order.Compute(s.Cart, s.Ongkir, vp, s.Poin)
}

func summary(s *state.Session, t order.Totals) string {
	var sb strings.Builder
	sb.WriteString("🧾 Ringkasan Pesanan\n")
	sb.WriteString(cartLines(s.Cart))
	fmt.Fprintf(&sb, "\n🛒 Belanja: %s\n🚚 Ongkir: %s\n", money.Rupiah(t.Belanja), money.Rupiah(t.Ongkir))
	if t.DiskonVoucher > 0 {
		fmt.Fprintf(&sb, "🎟️ Voucher %s: - %s\n", s.Voucher, money.Rupiah(t.DiskonVoucher))
	}
	if t.Poin > 0 {
		fmt.Fprintf(&sb, "⭐ Poin: - %s\n", money.Rupiah(t.Poin))
	}
	fmt.Fprintf(&sb, "💰 Total Pembayaran: %s\n🚚 Pengiriman: %s", money.Rupiah(t.Bayar), s.WaktuPengiriman)
	return sb.String()
}

var paymentKB = kb(
	row(btn("🏦 Transfer Bank", "pay_va")),
	row(btn("💳 Kartu Kredit", "pay_cc")),
	row(btn("📲 E-Wallet", "pay_ewallet")),
	row(btn("📸 QRIS", "pay_qris")),
	row(btn("❌ Batalkan Pesanan", "batal_pesanan")),
)

// processCheckout runs after the delivery slot is chosen: it either completes
// an order fully covered by points or asks for a payment method.
func (b *Bot) processCheckout(r *req) {
	if len(r.sess.Cart) == 0 {
		b.reply(r, "❌ Keranjang Anda kosong! Silakan tambahkan produk terlebih dahulu.", nil)
		r.sess.ResetCheckout()
		return
	}
	t := b.totals(r.sess)
	if t.Bayar == 0 {
		b.reply(r, "✅ Poin Anda mencukupi! Pesanan diproses tanpa pembayaran. 🚀\n\nAdmin akan segera menyiapkan pesanan Anda!", nil)
		p := b.snapshot(r.uid, r.sess, t, fmt.Sprintf("POIN-%d-%d", r.uid, b.now().Unix()), "", "")
		b.State.MarkProcessed(p.InvoiceID)
		b.completeOrder(r.ctx, p, true)
		r.sess.ResetOrder()
		return
	}
	r.sess.CheckoutStep = state.StepMetodeBayar
	b.reply(r, summary(r.sess, t)+"\n\n💳 Silakan pilih metode pembayaran:", paymentKB)
}
