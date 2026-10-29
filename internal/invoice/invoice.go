// Package invoice renders the PDF invoice sent to the customer and admin.
package invoice

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"

	"github.com/roysitumorang/bot_laukpauk/internal/catalog"
	"github.com/roysitumorang/bot_laukpauk/internal/money"
	"github.com/roysitumorang/bot_laukpauk/internal/order"
)

// Data is everything printed on the invoice.
type Data struct {
	Number          string
	Date            time.Time
	Nama            string
	Alamat          string
	DetailAlamat    string
	HP              string
	WaktuPengiriman string
	Cart            order.Cart
	Totals          order.Totals
	Voucher         string
}

// Number builds the invoice number "INV-<user>/<ddmmyy>/Laukpauk".
func Number(userID int64, t time.Time) string {
	return fmt.Sprintf("INV-%d/%s/Laukpauk", userID, t.Format("020106"))
}

// FileName is the attachment name for the PDF.
func FileName(userID int64, t time.Time) string {
	return fmt.Sprintf("Invoice_%d_%s.pdf", userID, t.Format("020106_1504"))
}

func dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// wrap breaks already-translated (cp1252) text into lines no wider than w.
// fpdf's SplitText cannot be used because it expects UTF-8 and panics on
// characters outside Latin-1.
func wrap(pdf *fpdf.Fpdf, text string, w float64) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	cur := words[0]
	for _, word := range words[1:] {
		if pdf.GetStringWidth(cur+" "+word) > w {
			lines = append(lines, cur)
			cur = word
			continue
		}
		cur += " " + word
	}
	return append(lines, cur)
}

// Render returns the PDF bytes. logo may be nil.
func Render(d Data, logo []byte) (out []byte, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			out, err = nil, fmt.Errorf("render invoice: %v", rec)
		}
	}()
	pdf := fpdf.New("P", "pt", "A4", "")
	pdf.SetMargins(40, 40, 40)
	pdf.SetAutoPageBreak(true, 40)
	pdf.AddPage()
	tr := pdf.UnicodeTranslatorFromDescriptor("") // UTF-8 -> cp1252 for core fonts
	w, _ := pdf.GetPageSize()
	right := w - 40

	if len(logo) > 0 {
		opt := fpdf.ImageOptions{ImageType: "JPG", ReadDpi: false}
		pdf.RegisterImageOptionsReader("logo", opt, bytes.NewReader(logo))
		if pdf.Ok() {
			pdf.ImageOptions("logo", 40, 25, 50, 50, false, opt, 0, "")
		} else {
			pdf.ClearError()
		}
	}

	pdf.SetFont("Helvetica", "B", 14)
	pdf.Text(150, 50, "INVOICE PEMBELIAN")
	pdf.SetFont("Helvetica", "", 10)
	pdf.Text(150, 65, tr("No: "+d.Number))
	pdf.Text(150, 78, "Tanggal: "+d.Date.Format("02-01-2006 15:04"))
	pdf.Line(40, 90, right, 90)

	y := 110.0
	pdf.SetFont("Helvetica", "B", 12)
	pdf.Text(40, y, "Detail Pembeli")
	pdf.SetFont("Helvetica", "", 10)
	for _, line := range []string{
		"Nama: " + dash(d.Nama),
		"Alamat: " + dash(d.Alamat),
		"Detail: " + dash(d.DetailAlamat),
		"No. HP: " + dash(d.HP),
		"Pengiriman: " + dash(d.WaktuPengiriman),
	} {
		for _, l := range wrap(pdf, tr(line), right-40) {
			y += 14
			pdf.Text(40, y, l)
		}
	}

	y += 15
	pdf.Line(40, y, right, y)
	y += 20
	pdf.SetFont("Helvetica", "B", 12)
	pdf.Text(40, y, "Daftar Produk")
	y += 15
	pdf.SetFont("Helvetica", "", 10)

	_, pageH := pdf.GetPageSize()
	for _, name := range d.Cart.Names() {
		it := d.Cart[name]
		if y > pageH-80 {
			pdf.AddPage()
			y = 60
		}
		pdf.Text(40, y, tr(fmt.Sprintf("%s x%d @ %s", catalog.Display(name), it.Jumlah, money.Rupiah(it.Net()))))
		pdf.Text(350, y, money.Rupiah(it.Subtotal()))
		y += 18
	}

	if y > pageH-180 {
		pdf.AddPage()
		y = 60
	}
	y += 2
	pdf.Line(40, y, right, y)
	y += 20
	pdf.SetFont("Helvetica", "B", 10)
	row := func(label, value string) {
		pdf.Text(40, y, tr(label))
		pdf.Text(350, y, value)
		y += 18
	}
	t := d.Totals
	row("Subtotal:", money.Rupiah(t.Belanja))
	row("Ongkos Kirim:", money.Rupiah(t.Ongkir))
	voucherLabel := "Diskon Voucher:"
	if d.Voucher != "" {
		voucherLabel = "Diskon Voucher (" + d.Voucher + "):"
	}
	row(voucherLabel, "- "+money.Rupiah(t.DiskonVoucher))
	row("Poin Digunakan:", "- "+money.Rupiah(t.Poin))

	y += 2
	pdf.Line(40, y, right, y)
	y += 24
	pdf.SetFont("Helvetica", "B", 14)
	pdf.Text(40, y, "Total Pembayaran:")
	pdf.Text(350, y, money.Rupiah(t.Bayar))

	y += 28
	pdf.SetFont("Helvetica", "", 10)
	pdf.Text(40, y, "Terima kasih telah berbelanja di Laukpauk!")

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
