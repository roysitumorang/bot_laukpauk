-- Schema used by bot_laukpauk. Every statement is idempotent, so the bot runs
-- this file at startup; existing tables created by the Python version are kept.

CREATE TABLE IF NOT EXISTS produk (
    nama_produk TEXT PRIMARY KEY,
    kategori    TEXT NOT NULL DEFAULT '',
    harga       NUMERIC(14, 2) NOT NULL DEFAULT 0,
    diskon      NUMERIC(14, 2) NOT NULL DEFAULT 0,
    stok        INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS pelanggan (
    user_id         TEXT PRIMARY KEY,
    nama            TEXT,
    alamat          TEXT,
    hp              TEXT,
    detail_alamat   TEXT,
    produk_terakhir JSONB NOT NULL DEFAULT '{}'::jsonb,
    total_poin      INTEGER NOT NULL DEFAULT 0,
    rating          INTEGER,
    testimoni       TEXT,
    status_pesanan  TEXT
);

ALTER TABLE pelanggan ADD COLUMN IF NOT EXISTS rating         INTEGER;
ALTER TABLE pelanggan ADD COLUMN IF NOT EXISTS testimoni      TEXT;
ALTER TABLE pelanggan ADD COLUMN IF NOT EXISTS status_pesanan TEXT;
