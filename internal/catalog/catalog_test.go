package catalog

import "testing"

func TestCatalog(t *testing.T) {
	c := New()
	c.Replace([]Product{
		{Nama: "Brokoli Hijau", Kategori: "Sayur", Harga: 15000, Diskon: 2000, Stok: 10},
		{Nama: "brokoli", Kategori: "Sayur", Harga: 12000, Stok: 3},
		{Nama: "Tahu", Kategori: "Lauk", Harga: 5000, Stok: 0},
		{Nama: "  ", Kategori: "x"},
	})
	if c.Len() != 3 {
		t.Fatalf("Len = %d", c.Len())
	}
	if got := c.Search("BROK"); len(got) != 2 || got[0].Nama != "brokoli" {
		t.Errorf("Search = %+v", got)
	}
	p, ok := c.ByKey(Key("brokoli hijau"))
	if !ok || p.Net() != 13000 {
		t.Errorf("ByKey = %+v, %v", p, ok)
	}
	if k, ok := c.CategoryByKey(Key("Lauk")); !ok || k != "Lauk" {
		t.Error("CategoryByKey")
	}
	old := c.Replace([]Product{{Nama: "Tahu", Kategori: "Lauk", Harga: 4000}})
	if old["tahu"].Harga != 5000 || c.Len() != 1 {
		t.Error("Replace should return the previous catalog")
	}
	if len("beli:"+Key("x")+":1") > 64 {
		t.Error("key too long for callback data")
	}
	if Display("brokoli") != "Brokoli" {
		t.Error("Display")
	}
}
