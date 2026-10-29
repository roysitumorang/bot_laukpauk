// Package store is the PostgreSQL layer (tables produk and pelanggan).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver

	"github.com/roysitumorang/bot_laukpauk/internal/catalog"
	"github.com/roysitumorang/bot_laukpauk/internal/order"
)

// DB wraps a connection pool.
type DB struct {
	db *sql.DB
}

// Open connects to PostgreSQL. When the URI does not set sslmode, it
// defaults to DB_SSLMODE (or "disable"), since lib/pq would otherwise require
// TLS even for a local server.
func Open(ctx context.Context, uri string) (*DB, error) {
	dsn, err := withSSLMode(uri)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxIdleTime(5 * time.Minute)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &DB{db: db}, nil
}

func withSSLMode(uri string) (string, error) {
	mode := os.Getenv("DB_SSLMODE")
	if mode == "" {
		mode = "disable"
	}
	if strings.HasPrefix(uri, "postgres://") || strings.HasPrefix(uri, "postgresql://") {
		u, err := url.Parse(uri)
		if err != nil {
			return "", fmt.Errorf("POSTGRES_URI tidak valid: %w", err)
		}
		q := u.Query()
		if q.Get("sslmode") == "" {
			q.Set("sslmode", mode)
			u.RawQuery = q.Encode()
		}
		return u.String(), nil
	}
	// key=value DSN
	if !strings.Contains(uri, "sslmode=") {
		uri += " sslmode=" + mode
	}
	return uri, nil
}

// Close closes the pool.
func (d *DB) Close() error { return d.db.Close() }

// Migrate runs the bundled schema (CREATE TABLE IF NOT EXISTS ...).
func (d *DB) Migrate(ctx context.Context, schema string) error {
	_, err := d.db.ExecContext(ctx, schema)
	return err
}

// LoadProducts reads the whole produk table.
func (d *DB) LoadProducts(ctx context.Context) ([]catalog.Product, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT nama_produk,
		       COALESCE(kategori, ''),
		       COALESCE(harga, 0)::float8,
		       COALESCE(diskon, 0)::float8,
		       COALESCE(stok, 0)::bigint
		FROM produk`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []catalog.Product
	for rows.Next() {
		var p catalog.Product
		var harga, diskon float64
		if err := rows.Scan(&p.Nama, &p.Kategori, &harga, &diskon, &p.Stok); err != nil {
			return nil, err
		}
		p.Harga, p.Diskon = round(harga), round(diskon)
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpsertProducts inserts or updates products by nama_produk.
func (d *DB) UpsertProducts(ctx context.Context, ps []catalog.Product) (int, error) {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO produk (nama_produk, kategori, harga, diskon, stok)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (nama_produk) DO UPDATE
		SET kategori = EXCLUDED.kategori,
		    harga    = EXCLUDED.harga,
		    diskon   = EXCLUDED.diskon,
		    stok     = EXCLUDED.stok`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	n := 0
	for _, p := range ps {
		if strings.TrimSpace(p.Nama) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, p.Nama, p.Kategori, p.Harga, p.Diskon, p.Stok); err != nil {
			return n, fmt.Errorf("produk %q: %w", p.Nama, err)
		}
		n++
	}
	return n, tx.Commit()
}

// Pelanggan is one customer row.
type Pelanggan struct {
	UserID         int64
	Nama           string
	Alamat         string
	HP             string
	DetailAlamat   string
	TotalPoin      int64
	ProdukTerakhir order.Cart
	StatusPesanan  string
}

func uid(id int64) string { return strconv.FormatInt(id, 10) }

// GetPelanggan returns nil (and no error) when the customer does not exist.
func (d *DB) GetPelanggan(ctx context.Context, userID int64) (*Pelanggan, error) {
	var p Pelanggan
	var produk string
	err := d.db.QueryRowContext(ctx, `
		SELECT COALESCE(nama, ''), COALESCE(alamat, ''), COALESCE(hp, ''),
		       COALESCE(detail_alamat, ''), COALESCE(total_poin, 0)::bigint,
		       COALESCE(produk_terakhir::text, '{}'), COALESCE(status_pesanan, '')
		FROM pelanggan WHERE user_id = $1`, uid(userID)).
		Scan(&p.Nama, &p.Alamat, &p.HP, &p.DetailAlamat, &p.TotalPoin, &produk, &p.StatusPesanan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.UserID = userID
	p.ProdukTerakhir = order.ParseCart([]byte(produk))
	return &p, nil
}

// GetPoin returns the customer's point balance (0 if unknown).
func (d *DB) GetPoin(ctx context.Context, userID int64) (int64, error) {
	var n int64
	err := d.db.QueryRowContext(ctx,
		`SELECT COALESCE(total_poin, 0)::bigint FROM pelanggan WHERE user_id = $1`, uid(userID)).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

// SavePelanggan upserts the customer's profile, last order and points.
func (d *DB) SavePelanggan(ctx context.Context, p Pelanggan) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO pelanggan (user_id, nama, alamat, hp, detail_alamat, produk_terakhir, total_poin)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
		ON CONFLICT (user_id) DO UPDATE
		SET nama            = EXCLUDED.nama,
		    alamat          = EXCLUDED.alamat,
		    hp              = EXCLUDED.hp,
		    detail_alamat   = EXCLUDED.detail_alamat,
		    produk_terakhir = EXCLUDED.produk_terakhir,
		    total_poin      = EXCLUDED.total_poin`,
		uid(p.UserID), p.Nama, p.Alamat, p.HP, p.DetailAlamat, p.ProdukTerakhir.JSON(), p.TotalPoin)
	return err
}

// SetStatusPesanan updates status_pesanan. It reports false when the
// customer row does not exist.
func (d *DB) SetStatusPesanan(ctx context.Context, userID int64, status string) (bool, error) {
	res, err := d.db.ExecContext(ctx,
		`UPDATE pelanggan SET status_pesanan = $2 WHERE user_id = $1`, uid(userID), status)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SaveTestimoni stores a rating and testimonial.
func (d *DB) SaveTestimoni(ctx context.Context, userID int64, rating int, text string) (bool, error) {
	res, err := d.db.ExecContext(ctx,
		`UPDATE pelanggan SET rating = $2, testimoni = $3 WHERE user_id = $1`, uid(userID), rating, text)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Testimoni is one published review.
type Testimoni struct {
	Nama   string
	Rating int
	Teks   string
}

// ListTestimoni returns up to limit non-empty testimonials.
func (d *DB) ListTestimoni(ctx context.Context, limit int) ([]Testimoni, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT COALESCE(nama, ''), COALESCE(rating, 0)::int, testimoni
		FROM pelanggan
		WHERE testimoni IS NOT NULL AND btrim(testimoni) <> ''
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Testimoni
	for rows.Next() {
		var t Testimoni
		if err := rows.Scan(&t.Nama, &t.Rating, &t.Teks); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListUserIDs returns all numeric customer IDs.
func (d *DB) ListUserIDs(ctx context.Context) ([]int64, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT user_id::text FROM pelanggan`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		if id, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil && id > 0 {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

func round(f float64) int64 {
	if f < 0 {
		return int64(f - 0.5)
	}
	return int64(f + 0.5)
}

// SetHarga changes a product's price by (case-insensitive) name.
func (d *DB) SetHarga(ctx context.Context, nama string, harga int64) (bool, error) {
	res, err := d.db.ExecContext(ctx,
		`UPDATE produk SET harga = $2 WHERE lower(nama_produk) = lower($1)`, strings.TrimSpace(nama), harga)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
