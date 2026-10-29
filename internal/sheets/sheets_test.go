package sheets

import (
	"context"
	"testing"
)

func TestColLetter(t *testing.T) {
	for i, want := range map[int]string{0: "A", 2: "C", 25: "Z", 26: "AA", 27: "AB", 701: "ZZ", 702: "AAA"} {
		if got := colLetter(i); got != want {
			t.Errorf("colLetter(%d) = %q, want %q", i, got, want)
		}
	}
}

func TestNilClientIsNoop(t *testing.T) {
	var c *Client
	if err := c.UpsertPelanggan(context.Background(), Pelanggan{}); err != nil {
		t.Error(err)
	}
	if err := c.AppendPesanan(context.Background(), Pesanan{}); err != nil {
		t.Error(err)
	}
}
