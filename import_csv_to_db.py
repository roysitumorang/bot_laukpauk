import csv
import asyncio
from product_service import bulk_upsert_products

def load_csv_data(filepath: str) -> list[dict]:
    """Membaca data produk dari file CSV dan mengubah jadi list of dict."""
    with open(filepath, newline='', encoding='utf-8') as csvfile:
        reader = csv.DictReader(csvfile)
        data = [row for row in reader if row.get("Nama Produk")]
    return data

async def main():
    filepath = "Produk Laukpauk.csv"  # 🔁 Ganti dengan path sesuai filemu
    print(f"📂 Membaca file: {filepath}")
    
    data = load_csv_data(filepath)
    if not data:
        print("❌ Tidak ada data yang ditemukan dalam CSV.")
        return

    print(f"📦 Menyimpan {len(data)} produk ke database...")
    inserted_count = await bulk_upsert_products(data)
    print(f"✅ Berhasil menyimpan {inserted_count} produk ke database PostgreSQL.")

if __name__ == "__main__":
    asyncio.run(main())
