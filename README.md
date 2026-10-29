# bot_laukpauk

Bot Telegram toko sayur & lauk **Laukpauk** — ditulis ulang dalam Go (sebelumnya Python/FastAPI).

Pelanggan bisa mencari produk, menyusun keranjang, checkout dengan alamat dari Google Maps
(ongkir dihitung per km), memakai voucher dan poin, memilih jadwal kirim, lalu membayar lewat
Xendit (VA, e-wallet, QRIS, kartu kredit). Setelah lunas, bot mengirim invoice PDF ke pelanggan
dan admin, menyimpan data ke PostgreSQL (dan Google Sheets bila diaktifkan), lalu admin menekan
**Kirim Barang** dan pelanggan diminta memberi rating & testimoni.

## Menjalankan

```bash
cp .env.sample .env        # lalu isi nilainya
go run ./cmd/laukpauk
```

Atau build binari / Docker:

```bash
go build -o laukpauk ./cmd/laukpauk
docker build -t laukpauk . && docker run --env-file .env -p 5000:5000 -v $PWD/data:/app/data laukpauk
```

Saat start, bot:

1. menjalankan `migrations/001_init.sql` (semua `IF NOT EXISTS`, tabel lama tetap aman),
2. memuat produk dari tabel `produk`,
3. menyiapkan sheet `Pelanggan` dan `Pesanan` (bila Google Sheets aktif),
4. mendaftarkan webhook Telegram ke `WEBHOOK_URL` — atau memakai long polling bila kosong
   (praktis untuk development lokal).

### Endpoint HTTP (default `:5000`)

| Method | Path                | Fungsi                                                         |
| ------ | ------------------- | -------------------------------------------------------------- |
| GET    | `/`                 | health check (`API LaukPauk is running!`)                      |
| GET    | `/healthz`          | health check                                                   |
| POST   | `/webhook/telegram` | update Telegram (cek `TELEGRAM_WEBHOOK_SECRET` bila diisi)     |
| POST   | `/webhook/xendit`   | callback invoice Xendit (wajib header `X-CALLBACK-TOKEN`)      |

Di dashboard Xendit, arahkan *Invoice paid* callback ke `https://<domain>/webhook/xendit`.

## Konfigurasi

Lihat `.env.sample`. Yang wajib: `BOT_TOKEN`, `GOOGLE_MAPS_API_KEY`, `XENDIT_API_KEY`,
`XENDIT_CALLBACK_TOKEN`, dan `POSTGRES_URI` (atau `DB_*`). Google Sheets opsional: isi
`CREDENTIALS_PATH` (service account JSON) dan `SPREADSHEET_ID` bersamaan.

Sesi pengguna, keranjang, voucher tambahan dan invoice yang menunggu pembayaran disimpan di
`STATE_DIR/state.json`; jadwal libur di `STATE_DIR/holidays.json` (format sama dengan versi
Python, jadi file lama bisa langsung dipakai). Simpan folder ini di volume persisten.

## Perintah

Pelanggan: `/start`, `/kategori`, `/produk`, `/keranjang`, `/edit_keranjang`, `/checkout`,
`/pesan_ulang`, `/status_pesanan`, `/batal`, `/beri_testimoni`, `/lihat_testimoni`.
Mengetik teks biasa = mencari produk.

Admin (`ADMIN_USER_ID`):

| Perintah | Fungsi |
| --- | --- |
| `/update_harga` | muat ulang produk dari DB; jika ada harga turun, umumkan ke semua pelanggan |
| `/reload` | muat ulang produk tanpa notifikasi |
| `/set_harga <nama produk> <harga>` | ubah harga di sheet `Laukpauk` lalu sync ke DB (atau langsung di DB bila Sheets nonaktif) |
| `/tambah_voucher <kode> <belanja\|ongkir> <nilai> <min_belanja>` | tambah voucher |
| `/tutup_toko DD-MM-YYYY DD-MM-YYYY` | set periode libur |
| `/buka_toko` | hapus semua periode libur |

## Struktur

```
cmd/laukpauk/        main: konfigurasi, HTTP server, webhook/polling, graceful shutdown
internal/bot/        handler Telegram (belanja, checkout, pembayaran, admin, testimoni) + HTTP routes
internal/telegram/   klien Bot API minimal (net/http + encoding/json, tanpa library pihak ketiga)
internal/order/      keranjang, voucher, perhitungan total
internal/poin/       tabel poin loyalitas
internal/delivery/   jadwal pengiriman
internal/holiday/    periode libur toko
internal/catalog/    cache produk di memori
internal/store/      PostgreSQL (lib/pq)
internal/sheets/     Google Sheets REST API + autentikasi service account
internal/gmaps/      Google Maps Geocoding & Distance Matrix
internal/xendit/     Xendit Invoice API
internal/invoice/    PDF invoice
internal/state/      sesi per pengguna & order tertunda (JSON)
migrations/          skema SQL
assets/              logo invoice
```

## Tes

```bash
go test ./...
# tes end-to-end (butuh PostgreSQL kosong yang boleh diubah):
LAUKPAUK_TEST_POSTGRES=postgres://postgres@127.0.0.1:5432/laukpauk_test go test ./internal/bot
```

Tes end-to-end menjalankan alur lengkap (cari → keranjang → checkout → voucher → bayar →
callback Xendit → invoice PDF → kirim barang → testimoni → pesan ulang → batal) memakai server
palsu untuk Telegram, Google Maps dan Xendit.

## Perbedaan dari versi Python

Alur dan teks pesan dipertahankan, tetapi beberapa bug ikut diperbaiki:

- **Voucher dihitung sekali.** Dulu diskon voucher dipotong dua kali (dari ongkir *dan* dari
  total). Sekarang voucher `ongkir` memotong ongkir, voucher `belanja` memotong belanja.
- **Poin** yang dipotong dari saldo = poin yang benar-benar dipakai (dibatasi total bayar).
  Pelanggan dengan saldo < 10.000 langsung lanjut ke jadwal kirim.
- **Callback Xendit** hanya memproses status `PAID`/`SETTLED` (dulu `EXPIRED` pun dianggap lunas),
  token dibandingkan dengan aman dan dibalas 401 bila salah, dan callback ganda diabaikan.
  Pesanan diselesaikan dari *snapshot* saat invoice dibuat, jadi perubahan keranjang setelahnya
  tidak memengaruhi pesanan yang dibayar.
- Setelah lunas, sesi checkout direset (dulu pelanggan bisa "terkunci" di status checkout).
- `/batal` membatalkan invoice di Xendit agar tidak bisa dibayar lagi.
- Voucher tidak lagi terbawa ke pesanan berikutnya; ada tombol **Lewati** voucher.
- Hari terakhir periode libur ikut dihitung libur; jadwal kirim di hari libur disembunyikan.
- Rating bisa lewat tombol ⭐1–⭐5; testimoni (yang dulu tidak pernah tersimpan karena handler
  tertimpa) kini tersimpan ke DB dan Sheets. Admin menekan **Kirim Barang** → pelanggan langsung
  diminta rating.
- `/pesan_ulang` memakai harga & stok terbaru dan melewati produk yang sudah tidak dijual.
- Status pesanan, rating dan testimoni dibaca dari PostgreSQL (kolom `status_pesanan`, `rating`,
  `testimoni`); Google Sheets menjadi salinan.
- `/set_harga` (sebelumnya ada tapi tidak terdaftar) kini aktif. `/reload` kini memuat dari DB.
- Data callback tombol memakai ID pendek (batas Telegram 64 byte), jadi nama produk panjang atau
  yang mengandung `_` tidak lagi merusak tombol. Tombol dari versi lama di riwayat chat tidak
  berlaku lagi.
- Kode yang tidak terpakai (`flask_app.py`, `fastapi_test.py`, `pesanan.py`, `checkout_final.py`,
  dsb.) tidak di-port.
