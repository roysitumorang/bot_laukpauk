from db import get_db_connection

async def get_pelanggan(user_id: str):
    conn = await get_db_connection()
    row = await conn.fetchrow("SELECT * FROM pelanggan WHERE user_id = $1", str(user_id))
    await conn.close()
    return dict(row) if row else {}

async def simpan_data_pelanggan(data: dict):
    conn = await get_db_connection()
    query = """
    INSERT INTO pelanggan (
        user_id, nama, alamat, hp, detail_alamat,
        produk_terakhir, total_poin, rating, testimoni, status_pesanan
    )
    VALUES (
        $1, $2, $3, $4, $5,
        $6, $7, $8, $9, $10
    )
    ON CONFLICT (user_id)
    DO UPDATE SET
        nama = EXCLUDED.nama,
        alamat = EXCLUDED.alamat,
        hp = EXCLUDED.hp,
        detail_alamat = EXCLUDED.detail_alamat,
        produk_terakhir = EXCLUDED.produk_terakhir,
        total_poin = EXCLUDED.total_poin,
        rating = EXCLUDED.rating,
        testimoni = EXCLUDED.testimoni,
        status_pesanan = EXCLUDED.status_pesanan;
    """
    await conn.execute(
        query,
        str(data["user_id"]),
        data.get("nama"),
        data.get("alamat"),
        data.get("hp"),
        data.get("detail_alamat"),
        data.get("produk_terakhir"),
        data.get("total_poin", 0),
        data.get("rating", 0),
        data.get("testimoni", ""),
        data.get("status_pesanan", "Diproses")
    )
    await conn.close()
