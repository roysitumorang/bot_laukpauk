package money

import (
	"encoding/json"
	"testing"
)

func TestRupiah(t *testing.T) {
	cases := map[int64]string{0: "Rp 0", 999: "Rp 999", 1000: "Rp 1.000", 1234567: "Rp 1.234.567", -25000: "Rp -25.000"}
	for in, want := range cases {
		if got := Rupiah(in); got != want {
			t.Errorf("Rupiah(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestToInt64(t *testing.T) {
	cases := []struct {
		in   any
		want int64
	}{
		{12000.0, 12000}, {12000.6, 12001}, {json.Number("15000"), 15000}, {json.Number("1500.5"), 1501},
		{"12.000", 12000}, {"Rp 12,500", 12500}, {"12000.0", 12000}, {"", 0}, {"abc", 0}, {nil, 0}, {7, 7},
	}
	for _, c := range cases {
		if got := ToInt64(c.in); got != c.want {
			t.Errorf("ToInt64(%#v) = %d, want %d", c.in, got, c.want)
		}
	}
}
