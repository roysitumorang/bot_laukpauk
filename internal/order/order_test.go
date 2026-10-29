package order

import "testing"

func cart() Cart {
	return Cart{
		"brokoli": {Jumlah: 2, Harga: 15000, Diskon: 2000}, // 26000
		"tahu":    {Jumlah: 5, Harga: 5000},                // 25000
	}
}

func TestCompute(t *testing.T) {
	c := cart()
	if got := c.Total(); got != 51000 {
		t.Fatalf("Total = %d", got)
	}
	belanja := Voucher{Kode: "DISKON10", Tipe: TipeBelanja, Nilai: 1000, MinBelanja: 50000}
	ongkir := Voucher{Kode: "ONGKIR5", Tipe: TipeOngkir, Nilai: 5000, MinBelanja: 30000}
	tooHigh := Voucher{Kode: "X", Tipe: TipeBelanja, Nilai: 25000, MinBelanja: 100000}

	cases := []struct {
		name string
		v    *Voucher
		ong  int64
		poin int64
		want Totals
	}{
		{"no voucher", nil, 9000, 0, Totals{Belanja: 51000, Ongkir: 9000, Bayar: 60000}},
		{"belanja voucher applied once", &belanja, 9000, 0, Totals{Belanja: 51000, Ongkir: 9000, DiskonVoucher: 1000, Bayar: 59000}},
		{"ongkir voucher capped at ongkir", &ongkir, 3000, 0, Totals{Belanja: 51000, Ongkir: 3000, DiskonVoucher: 3000, Bayar: 51000}},
		{"voucher below minimum ignored", &tooHigh, 9000, 0, Totals{Belanja: 51000, Ongkir: 9000, Bayar: 60000}},
		{"points", nil, 9000, 10000, Totals{Belanja: 51000, Ongkir: 9000, Poin: 10000, Bayar: 50000}},
		{"points capped at total", &ongkir, 9000, 100000, Totals{Belanja: 51000, Ongkir: 9000, DiskonVoucher: 5000, Poin: 55000, Bayar: 0}},
	}
	for _, c := range cases {
		if got := Compute(cart(), c.ong, c.v, c.poin); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestParseCartLegacy(t *testing.T) {
	// Shapes written by the Python bot: Decimal -> float, sometimes double-encoded.
	for _, raw := range []string{
		`{"brokoli": {"jumlah": 2, "harga": 15000.0, "diskon": 2000.0}}`,
		`"{\"brokoli\": {\"jumlah\": 2, \"harga\": \"15000\", \"diskon\": 2000}}"`,
	} {
		c := ParseCart([]byte(raw))
		it, ok := c["brokoli"]
		if !ok || it.Jumlah != 2 || it.Harga != 15000 || it.Diskon != 2000 {
			t.Errorf("ParseCart(%s) = %+v", raw, c)
		}
	}
	if c := ParseCart([]byte(`not json`)); len(c) != 0 {
		t.Errorf("garbage should give empty cart, got %+v", c)
	}
	if got := ParseCart([]byte(cart().JSON())); len(got) != 2 || got["tahu"].Jumlah != 5 {
		t.Errorf("round trip: %+v", got)
	}
}
