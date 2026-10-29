# db_pelanggan.py
import psycopg2
from psycopg2 import sql
import os
from dotenv import load_dotenv
load_dotenv("/home/user/bot_laukpauk/.env")


def insert_pelanggan(user_id, nama, alamat, hp, detail_alamat, produk_terakhir, total_poin):
    try:

         # Tambahan log debug sebelum koneksi
        print("[DEBUG] Menghubungkan ke DB dengan config:")
        print("DB_NAME:", os.getenv("DB_NAME"))
        print("DB_USER:", os.getenv("DB_USER"))
        print("DB_HOST:", os.getenv("DB_HOST"))

        conn = psycopg2.connect(
            dbname=os.getenv("DB_NAME"),
            user=os.getenv("DB_USER"),
            password=os.getenv("DB_PASS"),
            host=os.getenv("DB_HOST"),
            port=os.getenv("DB_PORT"),
        )
        cur = conn.cursor()

        # Convert produk_terakhir jadi string kalau masih dict
        if isinstance(produk_terakhir, dict):
            produk_terakhir = json.dumps(produk_terakhir)

        print(f"[DEBUG] Data yang dikirim ke DB:\n"
              f"user_id: {user_id}, nama: {nama}, alamat: {alamat}, hp: {hp}, "
              f"detail_alamat: {detail_alamat}, produk_terakhir: {produk_terakhir}, total_poin: {total_poin}")


        query = sql.SQL("""
            INSERT INTO pelanggan (user_id, nama, alamat, hp, detail_alamat, produk_terakhir, total_poin)
            VALUES (%s, %s, %s, %s, %s, %s, %s)
            ON CONFLICT (user_id) DO UPDATE
            SET nama = EXCLUDED.nama,
                alamat = EXCLUDED.alamat,
                hp = EXCLUDED.hp,
                detail_alamat = EXCLUDED.detail_alamat,
                produk_terakhir = EXCLUDED.produk_terakhir,
                total_poin = EXCLUDED.total_poin
        """)

        cur.execute(query, (user_id, nama, alamat, hp, detail_alamat, produk_terakhir, total_poin))
        conn.commit()
        cur.close()
        conn.close()
    except Exception as e:
        import traceback
        print("[DB ERROR] Gagal simpan pelanggan:")
        traceback.print_exc()

