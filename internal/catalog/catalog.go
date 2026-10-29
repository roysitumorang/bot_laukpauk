// Package catalog keeps the in-memory product list loaded from PostgreSQL.
package catalog

import (
	"crypto/sha1"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Product is one row of the produk table. Nama is lower-cased.
type Product struct {
	Nama     string
	Kategori string
	Harga    int64
	Diskon   int64
	Stok     int64
}

// Net is the price after the product discount.
func (p Product) Net() int64 {
	if p.Diskon <= 0 {
		return p.Harga
	}
	if n := p.Harga - p.Diskon; n > 0 {
		return n
	}
	return 0
}

// Key returns a short, stable identifier for s that is safe to put in
// Telegram callback data (which is limited to 64 bytes).
func Key(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])[:10]
}

// Display capitalises the first letter of a (lower-cased) product name.
func Display(name string) string {
	r, n := utf8.DecodeRuneInString(name)
	if r == utf8.RuneError {
		return name
	}
	return string(unicode.ToUpper(r)) + name[n:]
}

// Catalog is safe for concurrent use.
type Catalog struct {
	mu       sync.RWMutex
	byCat    map[string][]Product
	byKey    map[string]Product // Key(nama) -> product
	catByKey map[string]string  // Key(kategori) -> kategori
}

// New returns an empty catalog.
func New() *Catalog {
	return &Catalog{byCat: map[string][]Product{}, byKey: map[string]Product{}, catByKey: map[string]string{}}
}

// Replace swaps the catalog contents and returns the previous products keyed
// by name.
func (c *Catalog) Replace(products []Product) map[string]Product {
	byCat := map[string][]Product{}
	byKey := map[string]Product{}
	catByKey := map[string]string{}
	for _, p := range products {
		p.Nama = strings.ToLower(strings.TrimSpace(p.Nama))
		if p.Nama == "" {
			continue
		}
		byCat[p.Kategori] = append(byCat[p.Kategori], p)
		byKey[Key(p.Nama)] = p
		catByKey[Key(p.Kategori)] = p.Kategori
	}
	for k := range byCat {
		ps := byCat[k]
		sort.Slice(ps, func(i, j int) bool { return ps[i].Nama < ps[j].Nama })
	}
	c.mu.Lock()
	old := make(map[string]Product, len(c.byKey))
	for _, p := range c.byKey {
		old[p.Nama] = p
	}
	c.byCat, c.byKey, c.catByKey = byCat, byKey, catByKey
	c.mu.Unlock()
	return old
}

// Len returns the number of products.
func (c *Catalog) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.byKey)
}

// Categories returns the category names, sorted.
func (c *Catalog) Categories() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]string, 0, len(c.byCat))
	for k := range c.byCat {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// CategoryByKey resolves a Key(kategori).
func (c *Catalog) CategoryByKey(k string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.catByKey[k]
	return v, ok
}

// InCategory returns the products of a category, sorted by name.
func (c *Catalog) InCategory(kategori string) []Product {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]Product(nil), c.byCat[kategori]...)
}

// ByKey resolves a Key(nama).
func (c *Catalog) ByKey(k string) (Product, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.byKey[k]
	return p, ok
}

// Find looks a product up by (case-insensitive) name.
func (c *Catalog) Find(nama string) (Product, bool) {
	return c.ByKey(Key(strings.ToLower(strings.TrimSpace(nama))))
}

// Search returns products whose name contains q (case-insensitive).
func (c *Catalog) Search(q string) []Product {
	q = strings.ToLower(strings.TrimSpace(q))
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []Product
	if q == "" {
		return out
	}
	for _, p := range c.byKey {
		if strings.Contains(p.Nama, q) {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Nama < out[j].Nama })
	return out
}
