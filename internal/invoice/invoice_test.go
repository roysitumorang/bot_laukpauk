package invoice

import (
	"bytes"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/roysitumorang/bot_laukpauk/assets"
	"github.com/roysitumorang/bot_laukpauk/internal/order"
)

func TestRender(t *testing.T) {
	cart := order.Cart{}
	for i := 0; i < 45; i++ { // enough rows to need a second page
		cart[fmt.Sprintf("produk ñ %02d – spesial", i)] = order.Item{Jumlah: int64(i%3 + 1), Harga: 12500, Diskon: 500}
	}
	jkt, _ := time.LoadLocation("Asia/Jakarta")
	now := time.Date(2026, 10, 29, 18, 0, 0, 0, jkt)
	pdf, err := Render(Data{
		Number: Number(2000, now), Date: now, Nama: "Budi Santoso",
		Alamat:       "Jl. Setia Budi No.99, Tj. Sari, Kec. Medan Selayang, Kota Medan, Sumatera Utara 20132, Indonesia — dekat 🏪",
		DetailAlamat: "pagar hitam", HP: "0812-3456-7890", WaktuPengiriman: "30-10-2026 – 08:00",
		Cart: cart, Totals: order.Compute(cart, 12600, &order.Voucher{Kode: "ONGKIR5", Tipe: order.TipeOngkir, Nilai: 5000}, 10000),
		Voucher: "ONGKIR5",
	}, assets.Logo)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatal("not a PDF")
	}
	if out := os.Getenv("INVOICE_TEST_OUT"); out != "" {
		os.WriteFile(out, pdf, 0o644)
	}
	if got := Number(2000, now); got != "INV-2000/291026/Laukpauk" {
		t.Errorf("Number = %q", got)
	}
}
