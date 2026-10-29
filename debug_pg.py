import asyncio
import asyncpg
import os
from dotenv import load_dotenv

load_dotenv()

async def cek_tipe_harga():
    conn = await asyncpg.connect(os.getenv("POSTGRES_URI"))
    row = await conn.fetchrow("SELECT harga FROM produk LIMIT 1")
    print("👉 Value:", row['harga'])
    print("📦 Type :", type(row['harga']))
    await conn.close()

asyncio.run(cek_tipe_harga())
