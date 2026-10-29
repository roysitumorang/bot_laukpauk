// Package poin implements the loyalty-point tiers ("sistem loncat").
package poin

// MinTukar is the minimum number of points that can be redeemed at once.
const MinTukar = 10000

type tier struct {
	batas int64 // minimum belanja (Rp)
	poin  int64
}

// Tiers are evaluated in order; the last tier whose threshold is reached
// wins. The table is intentionally kept identical to the original bot.
var tiers = []tier{
	{50000, 500}, {75000, 1500}, {150000, 2500}, {200000, 3000},
	{250000, 2500}, {300000, 5000}, {350000, 3500}, {400000, 7000},
	{450000, 4500}, {500000, 10000}, {550000, 5500}, {600000, 12000},
	{650000, 6500}, {700000, 14000}, {750000, 7500}, {800000, 16000},
	{850000, 8500}, {900000, 18000}, {950000, 9500}, {1000000, 20000},
}

// Hitung returns the points earned for a purchase total.
func Hitung(totalBelanja int64) int64 {
	var p int64
	for _, t := range tiers {
		if totalBelanja < t.batas {
			break
		}
		p = t.poin
	}
	return p
}

// Rekomendasi returns how much more the customer needs to spend to reach
// the next tier and how many points that tier gives. ok is false when the
// customer is already at the top tier or the next tier would not give more
// points than they currently earn.
func Rekomendasi(totalBelanja int64) (tambahan, poinBaru int64, ok bool) {
	sekarang := Hitung(totalBelanja)
	for _, t := range tiers {
		if t.batas > totalBelanja {
			if t.poin > sekarang {
				return t.batas - totalBelanja, t.poin, true
			}
			return 0, 0, false
		}
	}
	return 0, 0, false
}
