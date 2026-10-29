// Package order holds the cart, voucher and checkout-total logic.
package order

import (
	"bytes"
	"encoding/json"
	"sort"

	"github.com/roysitumorang/bot_laukpauk/internal/money"
)

// Item is one cart line. The JSON shape matches the original bot's
// "produk_terakhir" column: {"brokoli": {"jumlah": 1, "harga": 12000, "diskon": 0}}.
type Item struct {
	Jumlah int64 `json:"jumlah"`
	Harga  int64 `json:"harga"`
	Diskon int64 `json:"diskon"`
}

// Net is the unit price after discount.
func (i Item) Net() int64 {
	if n := i.Harga - i.Diskon; n > 0 {
		return n
	}
	return 0
}

// Subtotal is Net * Jumlah.
func (i Item) Subtotal() int64 { return i.Net() * i.Jumlah }

// Cart maps product name (lower-case) to item.
type Cart map[string]Item

// Clone returns a deep copy.
func (c Cart) Clone() Cart {
	if c == nil {
		return nil
	}
	out := make(Cart, len(c))
	for k, v := range c {
		out[k] = v
	}
	return out
}

// Total is the sum of all subtotals (after product discounts).
func (c Cart) Total() int64 {
	var t int64
	for _, it := range c {
		t += it.Subtotal()
	}
	return t
}

// Names returns product names sorted alphabetically.
func (c Cart) Names() []string {
	out := make([]string, 0, len(c))
	for k := range c {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// JSON encodes the cart for the produk_terakhir column ("{}" when empty).
func (c Cart) JSON() string {
	if len(c) == 0 {
		return "{}"
	}
	b, err := json.Marshal(c)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// ParseCart decodes a produk_terakhir value. It tolerates the float and
// string numbers written by the old Python bot and skips malformed entries.
func ParseCart(raw []byte) Cart {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return Cart{}
	}
	// The column sometimes holds a JSON string that itself contains JSON.
	if raw[0] == '"' {
		var inner string
		if json.Unmarshal(raw, &inner) == nil {
			return ParseCart([]byte(inner))
		}
	}
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return Cart{}
	}
	out := Cart{}
	for name, v := range m {
		fields, ok := v.(map[string]any)
		if !ok {
			continue
		}
		it := Item{
			Jumlah: money.ToInt64(fields["jumlah"]),
			Harga:  money.ToInt64(fields["harga"]),
			Diskon: money.ToInt64(fields["diskon"]),
		}
		if it.Jumlah <= 0 {
			it.Jumlah = 1
		}
		out[name] = it
	}
	return out
}

// Voucher types.
const (
	TipeBelanja = "belanja" // reduces the goods total
	TipeOngkir  = "ongkir"  // reduces the delivery fee
)

// Voucher is a discount code.
type Voucher struct {
	Kode       string `json:"kode"`
	Tipe       string `json:"tipe"`
	Nilai      int64  `json:"nilai"`
	MinBelanja int64  `json:"min_belanja"`
	Batas      int64  `json:"batas,omitempty"` // kept from the original data; not enforced
}

// DefaultVouchers are the vouchers the original bot shipped with.
func DefaultVouchers() map[string]Voucher {
	return map[string]Voucher{
		"DISKON10": {Kode: "DISKON10", Tipe: TipeBelanja, Nilai: 1000, MinBelanja: 50000, Batas: 50},
		"ONGKIR5":  {Kode: "ONGKIR5", Tipe: TipeOngkir, Nilai: 5000, MinBelanja: 30000, Batas: 30},
		"DISKON25": {Kode: "DISKON25", Tipe: TipeBelanja, Nilai: 25000, MinBelanja: 100000, Batas: 20},
		"ONGKIR10": {Kode: "ONGKIR10", Tipe: TipeOngkir, Nilai: 10000, MinBelanja: 75000, Batas: 15},
	}
}

// Totals is the breakdown of what the customer pays.
type Totals struct {
	Belanja       int64 `json:"belanja"`        // goods after product discounts
	Ongkir        int64 `json:"ongkir"`         // delivery fee before voucher
	DiskonVoucher int64 `json:"diskon_voucher"` // voucher amount actually applied
	Poin          int64 `json:"poin"`           // points actually redeemed (1 poin = Rp 1)
	Bayar         int64 `json:"bayar"`          // amount due
}

// Compute applies the voucher once (to the goods or to the delivery fee,
// depending on its type) and then the points, never going below zero.
// Points beyond what is payable are not redeemed.
func Compute(cart Cart, ongkir int64, v *Voucher, poinDiminta int64) Totals {
	t := Totals{Belanja: cart.Total(), Ongkir: ongkir}
	belanja, kirim := t.Belanja, ongkir
	if v != nil && t.Belanja >= v.MinBelanja && v.Nilai > 0 {
		if v.Tipe == TipeOngkir {
			t.DiskonVoucher = min(v.Nilai, kirim)
			kirim -= t.DiskonVoucher
		} else {
			t.DiskonVoucher = min(v.Nilai, belanja)
			belanja -= t.DiskonVoucher
		}
	}
	sub := belanja + kirim
	if poinDiminta > 0 {
		t.Poin = min(poinDiminta, sub)
	}
	t.Bayar = sub - t.Poin
	return t
}
