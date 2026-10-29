package poin

import "testing"

func TestHitung(t *testing.T) {
	cases := map[int64]int64{0: 0, 49999: 0, 50000: 500, 74999: 500, 75000: 1500, 200000: 3000, 249999: 3000, 250000: 2500, 1000000: 20000, 5000000: 20000}
	for in, want := range cases {
		if got := Hitung(in); got != want {
			t.Errorf("Hitung(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestRekomendasi(t *testing.T) {
	if tambah, p, ok := Rekomendasi(40000); !ok || tambah != 10000 || p != 500 {
		t.Errorf("Rekomendasi(40000) = %d, %d, %v", tambah, p, ok)
	}
	// 250k gives fewer points than 200k, so no recommendation at 200k.
	if _, _, ok := Rekomendasi(200000); ok {
		t.Error("Rekomendasi(200000) should not recommend a lower tier")
	}
	if _, _, ok := Rekomendasi(1000000); ok {
		t.Error("top tier should have no recommendation")
	}
}
