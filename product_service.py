import asyncpg
import os
from dotenv import load_dotenv
import gspread
import json
from pathlib import Path
load_dotenv(dotenv_path=Path('.') / '.env')  # ✅ Ini memastikan file benar-benar diload


POSTGRES_URI = os.getenv("POSTGRES_URI")
print(f"📡 POSTGRES_URI: {POSTGRES_URI}")


async def get_db_connection():
    return await asyncpg.connect(POSTGRES_URI)

async def get_pelanggan_from_db(user_id):
    conn = await get_db_connection()
    try:
        row = await conn.fetchrow("SELECT * FROM pelanggan WHERE user_id = $1", str(user_id))
        if not row:
            return None

        produk_terakhir = row.get("produk_terakhir", "{}")
        try:
            produk_terakhir = json.loads(produk_terakhir) if isinstance(produk_terakhir, str) else produk_terakhir
        except json.JSONDecodeError:
            produk_terakhir = {}

        return {
            "nama": row["nama"],
            "alamat": row["alamat"],
            "hp": row["hp"],
            "detail_alamat": row["detail_alamat"],
            "total_poin": row["total_poin"],
            "produk_terakhir": produk_terakhir
        }

    finally:
        await conn.close()


async def update_product_price(name: str, price: float) -> bool:
    conn = await get_db_connection()
    result = await conn.execute("""
        UPDATE products SET price = $1 WHERE name = $2
    """, price, name)
    await conn.close()
    return result.startswith("UPDATE")

async def bulk_upsert_products(data: list[dict]) -> int:
    conn = await get_db_connection()
    count = 0

    for row in data:
        if not row.get("Nama Produk"):
            continue  # ✅ Skip baris kosong (misalnya row dari Google Sheets)

        await conn.execute("""
            INSERT INTO produk (nama_produk, kategori, harga, diskon, stok)
            VALUES ($1, $2, $3, $4, $5)
            ON CONFLICT (nama_produk) DO UPDATE
            SET kategori = EXCLUDED.kategori,
                harga = EXCLUDED.harga,
                diskon = EXCLUDED.diskon,
                stok = EXCLUDED.stok;
        """, 
            row.get("Nama Produk"),
            row.get("Kategori"),
            float(row.get("Harga") or 0),
            float(row.get("Diskon") or 0),
            int(row.get("Stok") or 0)
        )
        count += 1

    await conn.close()
    return count


async def update_single_product(nama_produk: str, harga: float = None, stok: int = None):
    conn = await get_db_connection()

    if harga is not None:
        await conn.execute("UPDATE produk SET harga = $1 WHERE nama_produk = $2", harga, nama_produk)

    if stok is not None:
        await conn.execute("UPDATE produk SET stok = $1 WHERE nama_produk = $2", stok, nama_produk)

    await conn.close()

async def bulk_upsert_customers(data: list[dict]) -> int:
    conn = await get_db_connection()
    count = 0

    for row in data:
        user_id = str(row.get("User ID") or "0")
        nama = row.get("Nama") or ""
        alamat = row.get("Alamat") or ""
        hp = row.get("HP") or ""
        detail_alamat = row.get("Detail Alamat") or ""
        produk_terakhir = row.get("Produk Terakhir") or "{}"
        total_poin = int(row.get("Total Poin") or 0)
        rating = int(row.get("Rating") or 0)
        testimoni = row.get("Testimoni") or ""
        status_pesanan = row.get("Status Pesanan") or ""

        # Skip baris kosong total (kalau user_id kosong atau '0')
        if not user_id.strip() or user_id == "0":
            continue

        try:
            await conn.execute("""
                INSERT INTO pelanggan (
                    user_id, nama, alamat, hp, detail_alamat,
                    produk_terakhir, total_poin, rating, testimoni, status_pesanan
                )
                VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10)
                ON CONFLICT (user_id) DO UPDATE
                SET nama = EXCLUDED.nama,
                    alamat = EXCLUDED.alamat,
                    hp = EXCLUDED.hp,
                    detail_alamat = EXCLUDED.detail_alamat,
                    produk_terakhir = EXCLUDED.produk_terakhir,
                    total_poin = EXCLUDED.total_poin,
                    rating = EXCLUDED.rating,
                    testimoni = EXCLUDED.testimoni,
                    status_pesanan = EXCLUDED.status_pesanan;
            """, 
                user_id,
                nama,
                alamat,
                hp,
                detail_alamat,
                produk_terakhir,
                total_poin,
                rating,
                testimoni,
                status_pesanan
            )
            count += 1

        except Exception as e:
            print(f"❌ Error saat insert pelanggan user_id={user_id}: {e}")

    await conn.close()
    return count

async def simpan_pelanggan_ke_postgres(user_id, nama, alamat, hp, detail_alamat, produk_terakhir, total_poin):
    print(f"🚀 [TRACE] Masuk fungsi simpan_pelanggan_ke_postgres untuk user {user_id}")
    print(f"📝 [TRACE] produk_terakhir_str: {produk_terakhir}")
    print(f"📝 [TRACE] total_poin_baru: {total_poin}")

    conn = await get_db_connection()
    try:
        await conn.execute("""
            INSERT INTO pelanggan (
                user_id, nama, alamat, hp, detail_alamat,
                produk_terakhir, total_poin
            ) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)
            ON CONFLICT (user_id) DO UPDATE
            SET nama = EXCLUDED.nama,
                alamat = EXCLUDED.alamat,
                hp = EXCLUDED.hp,
                detail_alamat = EXCLUDED.detail_alamat,
                produk_terakhir = EXCLUDED.produk_terakhir,
                total_poin = EXCLUDED.total_poin
        """, str(user_id), nama, alamat, hp, detail_alamat,
             json.dumps(produk_terakhir, ensure_ascii=False, cls=DecimalEncoder),
             int(total_poin)
        )  # ✅ TUTUPIN KURUNGNYA DI SINI
    except Exception as e:
        import traceback
        traceback.print_exc()
        print(f"❌ [PostgreSQL] Gagal simpan pelanggan {user_id}: {e}")


def update_google_sheet_harga(nama_produk: str, harga_baru: int):
    gc = gspread.service_account(filename="/home/user/bot_laukpauk/credentials.json")
    sh = gc.open_by_key("157srz4epQMUJ5q_fo3Q8FkrBdo40qmfq2VsL2FtKV7Q")
    worksheet = sh.worksheet("Laukpauk")
    data = worksheet.get_all_values()

    for idx, row in enumerate(data[1:], start=2):  # Hindari baris header
        if len(row) >= 2 and row[1].strip().lower() == nama_produk.strip().lower():
            worksheet.update_cell(idx + 1, 3, harga_baru)  # Kolom C = Harga
            print(f"📝 Harga Google Sheet '{nama_produk}' diubah jadi {harga_baru}")
            return True

    print("❌ Produk tidak ditemukan di Google Sheets.")
    return False

async def get_all_products():
    conn = await get_db_connection()
    rows = await conn.fetch("SELECT * FROM produk")
    await conn.close()

    produk = {}
    for row in rows:
        kategori = row["kategori"]
        nama = row["nama_produk"].lower()
        harga = row["harga"]
        diskon = row["diskon"]
        stok = row["stok"]

        if kategori not in produk:
            produk[kategori] = {}
        produk[kategori][nama] = {
            "harga": harga,
            "diskon": diskon,
            "stok": stok
        }
    return produk


