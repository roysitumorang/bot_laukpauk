package sheets

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/roysitumorang/bot_laukpauk/internal/catalog"
	"github.com/roysitumorang/bot_laukpauk/internal/money"
)

// Sheet names and layouts, unchanged from the Python bot.
const (
	SheetPelanggan = "Pelanggan"
	SheetPesanan   = "Pesanan"
	SheetProduk    = "Laukpauk"
)

var pelangganHeader = []string{
	"User ID", "Nama", "Alamat", "HP", "Detail Alamat", "Produk Terakhir",
	"Total Poin", "Rating", "Testimoni", "Status Pesanan",
}

var pesananHeader = []string{
	"User ID", "Nama", "Alamat", "HP", "Kode Voucher", "Diskon Voucher",
	"Poin Digunakan", "Total Belanja", "Ongkos Kirim", "Total Pembayaran", "Waktu Pesanan",
}

// ErrNotFound is returned when a customer or product row does not exist.
var ErrNotFound = errors.New("tidak ditemukan di Google Sheets")

func toRow(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// EnsureLayout creates the Pelanggan and Pesanan sheets when missing and
// fills in blank header cells.
func (c *Client) EnsureLayout(ctx context.Context) error {
	if c == nil {
		return nil
	}
	ids, err := c.sheetIDs(ctx)
	if err != nil {
		return err
	}
	for _, s := range []struct {
		title  string
		header []string
	}{{SheetPelanggan, pelangganHeader}, {SheetPesanan, pesananHeader}} {
		if _, ok := ids[s.title]; !ok {
			_, err := c.batchUpdate(ctx, map[string]any{"addSheet": map[string]any{
				"properties": map[string]any{
					"title":          s.title,
					"gridProperties": map[string]any{"rowCount": 1000, "columnCount": len(s.header)},
				},
			}})
			if err != nil {
				return err
			}
		}
		rows, err := c.getValues(ctx, s.title+"!1:1")
		if err != nil {
			return err
		}
		var cur []any
		if len(rows) > 0 {
			cur = rows[0]
		}
		header := make([]any, len(s.header))
		changed := false
		for i, h := range s.header {
			if i < len(cur) && cellString(cur[i]) != "" {
				header[i] = cur[i]
				continue
			}
			header[i] = h
			changed = true
		}
		if changed {
			if err := c.updateValues(ctx, s.title+"!A1", [][]any{header}); err != nil {
				return err
			}
		}
	}
	return nil
}

// findUserRow returns the 1-based row of userID in Pelanggan column A.
func (c *Client) findUserRow(ctx context.Context, userID int64) (int, error) {
	col, err := c.getValues(ctx, SheetPelanggan+"!A:A")
	if err != nil {
		return 0, err
	}
	want := strconv.FormatInt(userID, 10)
	for i, r := range col {
		if i == 0 || len(r) == 0 {
			continue
		}
		if cellString(r[0]) == want {
			return i + 1, nil
		}
	}
	return 0, ErrNotFound
}

// Pelanggan is the A:G part of a Pelanggan row.
type Pelanggan struct {
	UserID         int64
	Nama           string
	Alamat         string
	HP             string
	DetailAlamat   string
	ProdukTerakhir string // JSON
	TotalPoin      int64
}

// UpsertPelanggan updates the customer's row, or inserts it right below the
// header when it does not exist yet.
func (c *Client) UpsertPelanggan(ctx context.Context, p Pelanggan) error {
	if c == nil {
		return nil
	}
	if strings.TrimSpace(p.DetailAlamat) == "" {
		p.DetailAlamat = "TIDAK DIISI"
	}
	row := []any{p.UserID, p.Nama, p.Alamat, p.HP, p.DetailAlamat, p.ProdukTerakhir, p.TotalPoin}
	r, err := c.findUserRow(ctx, p.UserID)
	if err == nil {
		return c.updateValues(ctx, SheetPelanggan+"!A"+strconv.Itoa(r)+":G"+strconv.Itoa(r), [][]any{row})
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	ids, err := c.sheetIDs(ctx)
	if err != nil {
		return err
	}
	sheetID, ok := ids[SheetPelanggan]
	if !ok {
		return errors.New("sheet Pelanggan tidak ada")
	}
	if _, err := c.batchUpdate(ctx, map[string]any{"insertDimension": map[string]any{
		"range":             map[string]any{"sheetId": sheetID, "dimension": "ROWS", "startIndex": 1, "endIndex": 2},
		"inheritFromBefore": false,
	}}); err != nil {
		return err
	}
	return c.updateValues(ctx, SheetPelanggan+"!A2:G2", [][]any{row})
}

// SetStatusPesanan writes column J (Status Pesanan).
func (c *Client) SetStatusPesanan(ctx context.Context, userID int64, status string) error {
	if c == nil {
		return nil
	}
	r, err := c.findUserRow(ctx, userID)
	if err != nil {
		return err
	}
	return c.updateValues(ctx, SheetPelanggan+"!J"+strconv.Itoa(r), [][]any{{status}})
}

// SaveTestimoni writes columns H (Rating) and I (Testimoni).
func (c *Client) SaveTestimoni(ctx context.Context, userID int64, rating int, text string) error {
	if c == nil {
		return nil
	}
	r, err := c.findUserRow(ctx, userID)
	if err != nil {
		return err
	}
	rs := strconv.Itoa(r)
	return c.updateValues(ctx, SheetPelanggan+"!H"+rs+":I"+rs, [][]any{{rating, text}})
}

// Pesanan is one row of the Pesanan sheet.
type Pesanan struct {
	UserID          int64
	Nama            string
	Alamat          string
	HP              string
	KodeVoucher     string
	DiskonVoucher   int64
	PoinDigunakan   int64
	TotalBelanja    int64
	OngkosKirim     int64
	TotalPembayaran int64
	WaktuPesanan    string
}

// AppendPesanan adds an order row.
func (c *Client) AppendPesanan(ctx context.Context, p Pesanan) error {
	if c == nil {
		return nil
	}
	if p.KodeVoucher == "" {
		p.KodeVoucher = "-"
	}
	return c.appendValues(ctx, SheetPesanan+"!A:K", [][]any{{
		p.UserID, p.Nama, p.Alamat, p.HP, p.KodeVoucher, p.DiskonVoucher, p.PoinDigunakan,
		p.TotalBelanja, p.OngkosKirim, p.TotalPembayaran, p.WaktuPesanan,
	}})
}

// produkColumns locates the product sheet columns by header name.
func produkColumns(header []any) map[string]int {
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.ToLower(cellString(h))] = i
	}
	return idx
}

func colLetter(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

// SetHargaProduk changes the price of a product in the Laukpauk sheet.
func (c *Client) SetHargaProduk(ctx context.Context, nama string, harga int64) error {
	if c == nil {
		return errors.New("Google Sheets belum dikonfigurasi")
	}
	rows, err := c.getValues(ctx, SheetProduk)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return ErrNotFound
	}
	cols := produkColumns(rows[0])
	nameCol, ok := cols["nama produk"]
	if !ok {
		nameCol = 1 // column B, as in the original bot
	}
	priceCol, ok := cols["harga"]
	if !ok {
		priceCol = 2 // column C
	}
	want := strings.ToLower(strings.TrimSpace(nama))
	for i, r := range rows[1:] {
		if nameCol < len(r) && strings.ToLower(cellString(r[nameCol])) == want {
			cell := SheetProduk + "!" + colLetter(priceCol) + strconv.Itoa(i+2)
			return c.updateValues(ctx, cell, [][]any{{harga}})
		}
	}
	return ErrNotFound
}

// Produk reads every product from the Laukpauk sheet (columns
// "Nama Produk", "Kategori", "Harga", "Diskon", "Stok").
func (c *Client) Produk(ctx context.Context) ([]catalog.Product, error) {
	if c == nil {
		return nil, errors.New("Google Sheets belum dikonfigurasi")
	}
	rows, err := c.getValues(ctx, SheetProduk)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	cols := produkColumns(rows[0])
	get := func(r []any, name string) any {
		i, ok := cols[name]
		if !ok || i >= len(r) {
			return nil
		}
		return r[i]
	}
	var out []catalog.Product
	for _, r := range rows[1:] {
		nama := cellString(get(r, "nama produk"))
		if nama == "" {
			continue
		}
		out = append(out, catalog.Product{
			Nama:     nama,
			Kategori: cellString(get(r, "kategori")),
			Harga:    money.ToInt64(get(r, "harga")),
			Diskon:   money.ToInt64(get(r, "diskon")),
			Stok:     money.ToInt64(get(r, "stok")),
		})
	}
	return out, nil
}
