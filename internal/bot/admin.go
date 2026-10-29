package bot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/roysitumorang/bot_laukpauk/internal/catalog"
	"github.com/roysitumorang/bot_laukpauk/internal/money"
	"github.com/roysitumorang/bot_laukpauk/internal/order"
	"github.com/roysitumorang/bot_laukpauk/internal/sheets"
)

// ReloadCatalog loads the products from PostgreSQL into memory and returns
// the previous catalog keyed by product name.
func (b *Bot) ReloadCatalog(ctx context.Context) (map[string]catalog.Product, int, error) {
	ps, err := b.DB.LoadProducts(ctx)
	if err != nil {
		return nil, 0, err
	}
	if len(ps) == 0 {
		return nil, 0, errors.New("tabel produk kosong")
	}
	old := b.Catalog.Replace(ps)
	return old, len(ps), nil
}

func (b *Bot) requireAdmin(r *req, denied string) bool {
	if b.isAdmin(r.uid) {
		return true
	}
	b.reply(r, denied, nil)
	return false
}

func (b *Bot) cmdReload(r *req) {
	if !b.requireAdmin(r, "⚠️ Anda tidak memiliki izin untuk memuat ulang produk.") {
		return
	}
	_, n, err := b.ReloadCatalog(r.ctx)
	if err != nil {
		b.reply(r, "❌ Gagal memuat produk: "+err.Error(), nil)
		return
	}
	b.reply(r, fmt.Sprintf("🔄 Data produk telah diperbarui dari database! (%d produk)", n), nil)
}

func (b *Bot) cmdUpdateHarga(r *req) {
	if !b.requireAdmin(r, "⚠️ Anda tidak memiliki izin untuk memperbarui harga.") {
		return
	}
	b.reply(r, "🔄 Memeriksa perubahan harga...", nil)
	old, _, err := b.ReloadCatalog(r.ctx)
	if err != nil {
		log.Printf("update harga: %v", err)
		b.reply(r, "❌ ERROR: Data produk tidak ditemukan di database!", nil)
		return
	}

	var turun []string
	for _, kat := range b.Catalog.Categories() {
		for _, p := range b.Catalog.InCategory(kat) {
			prev, ok := old[p.Nama]
			if ok && p.Net() < prev.Net() {
				turun = append(turun, fmt.Sprintf("🔻 %s: %s ➝ %s", catalog.Display(p.Nama), money.Rupiah(prev.Net()), money.Rupiah(p.Net())))
			}
		}
	}
	if len(turun) == 0 {
		b.reply(r, "✅ Tidak ada perubahan harga terbaru.", nil)
		return
	}
	pesan := "📢 Harga Turun! Yuk, Belanja Sekarang!\n\n" + strings.Join(turun, "\n")
	b.sendLong(r.chatID, "✅ Update Harga Berhasil!\n\n"+pesan)

	admin := r.uid
	b.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		ids, err := b.DB.ListUserIDs(ctx)
		if err != nil {
			log.Printf("broadcast: %v", err)
			b.send(admin, "❌ Gagal mengambil daftar pelanggan untuk notifikasi.", nil)
			return
		}
		sent := 0
		for _, id := range ids {
			if id == b.cfg.AdminUserID {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond): // stay under Telegram's rate limit
			}
			if b.send(id, pesan, nil) {
				sent++
			}
		}
		b.send(admin, fmt.Sprintf("📢 Notifikasi harga turun telah dikirim ke %d pelanggan.", sent), nil)
	})
}

func (b *Bot) cmdTambahVoucher(r *req) {
	if !b.requireAdmin(r, "⚠️ Anda tidak memiliki izin untuk menambahkan voucher.") {
		return
	}
	usage := "❌ Format salah! Gunakan: /tambah_voucher [kode] [tipe] [nilai] [min_belanja]\nTipe: belanja atau ongkir"
	args := strings.Fields(r.msg.CommandArguments())
	if len(args) < 4 {
		b.reply(r, usage, nil)
		return
	}
	kode, tipe := strings.ToUpper(args[0]), strings.ToLower(args[1])
	if tipe != order.TipeBelanja && tipe != order.TipeOngkir {
		b.reply(r, "⚠️ Tipe harus 'belanja' atau 'ongkir'.", nil)
		return
	}
	nilai, err1 := strconv.ParseInt(args[2], 10, 64)
	minBelanja, err2 := strconv.ParseInt(args[3], 10, 64)
	if err1 != nil || err2 != nil || nilai <= 0 || minBelanja < 0 {
		b.reply(r, usage, nil)
		return
	}
	b.State.PutVoucher(order.Voucher{Kode: kode, Tipe: tipe, Nilai: nilai, MinBelanja: minBelanja})
	b.reply(r, fmt.Sprintf("✅ Voucher %s berhasil ditambahkan!", kode), nil)
}

func (b *Bot) cmdTutupToko(r *req) {
	if !b.requireAdmin(r, "❌ Anda tidak memiliki izin untuk menutup toko.") {
		return
	}
	usage := "❌ Format salah! Gunakan: /tutup_toko DD-MM-YYYY DD-MM-YYYY"
	args := strings.Fields(r.msg.CommandArguments())
	if len(args) != 2 {
		b.reply(r, usage, nil)
		return
	}
	start, err1 := time.ParseInLocation("02-01-2006", args[0], b.cfg.Location)
	end, err2 := time.ParseInLocation("02-01-2006", args[1], b.cfg.Location)
	if err1 != nil || err2 != nil {
		b.reply(r, "❌ Format tanggal salah! Gunakan: /tutup_toko DD-MM-YYYY DD-MM-YYYY", nil)
		return
	}
	if err := b.Holidays.Add(start, end); err != nil {
		b.reply(r, "❌ "+err.Error(), nil)
		return
	}
	b.reply(r, fmt.Sprintf("✅ Toko akan libur dari %s sampai %s.", start.Format("02-01-2006"), end.Format("02-01-2006")), nil)
}

func (b *Bot) cmdBukaToko(r *req) {
	if !b.requireAdmin(r, "❌ Anda tidak memiliki izin untuk membuka toko.") {
		return
	}
	if err := b.Holidays.Clear(); err != nil {
		b.reply(r, "❌ Gagal membuka toko: "+err.Error(), nil)
		return
	}
	b.reply(r, "✅ Toko telah dibuka kembali.", nil)
}

// cmdSetHarga: /set_harga <nama produk> <harga baru>. Updates the Laukpauk
// sheet and syncs it to PostgreSQL (or updates PostgreSQL directly when
// Sheets is not configured).
func (b *Bot) cmdSetHarga(r *req) {
	if !b.requireAdmin(r, "❌ Anda tidak punya izin untuk mengubah harga.") {
		return
	}
	usage := "⚠️ Format salah. Gunakan: /set_harga [Nama Produk] [Harga Baru]"
	args := strings.Fields(r.msg.CommandArguments())
	if len(args) < 2 {
		b.reply(r, usage, nil)
		return
	}
	harga := money.ParseString(args[len(args)-1])
	nama := strings.Join(args[:len(args)-1], " ")
	if harga <= 0 || nama == "" {
		b.reply(r, usage, nil)
		return
	}

	if b.Sheets == nil {
		ok, err := b.DB.SetHarga(r.ctx, nama, harga)
		switch {
		case err != nil:
			b.reply(r, "❌ Terjadi error: "+err.Error(), nil)
		case !ok:
			b.reply(r, "❌ Produk tidak ditemukan di database.", nil)
		default:
			b.reply(r, fmt.Sprintf("✅ Harga produk %s berhasil diubah menjadi %s.\nKetik /update_harga untuk memperbarui katalog bot dan memberi tahu pelanggan jika harga turun.", nama, money.Rupiah(harga)), nil)
		}
		return
	}

	if err := b.Sheets.SetHargaProduk(r.ctx, nama, harga); err != nil {
		if errors.Is(err, sheets.ErrNotFound) {
			b.reply(r, "❌ Produk tidak ditemukan di Google Sheets.", nil)
			return
		}
		b.reply(r, "❌ Terjadi error: "+err.Error(), nil)
		return
	}
	ps, err := b.Sheets.Produk(r.ctx)
	if err != nil {
		b.reply(r, "❌ Harga di Google Sheets sudah diubah, tetapi gagal membaca ulang sheet: "+err.Error(), nil)
		return
	}
	if _, err := b.DB.UpsertProducts(r.ctx, ps); err != nil {
		b.reply(r, "❌ Harga di Google Sheets sudah diubah, tetapi gagal sync ke database: "+err.Error(), nil)
		return
	}
	b.reply(r, fmt.Sprintf("✅ Harga produk %s berhasil diubah menjadi %s\n🗂️ Data sudah di-sync ke database.\nKetik /update_harga untuk memperbarui katalog bot dan memberi tahu pelanggan jika harga turun.", nama, money.Rupiah(harga)), nil)
}
