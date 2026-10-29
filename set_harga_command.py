from telegram import Update
from telegram.ext import ContextTypes
from product_service import update_single_product, bulk_upsert_products
import gspread
import os

ADMIN_USER_ID = 5464224951
SPREADSHEET_ID = "157srz4epQMUJ5q_fo3Q8FkrBdo40qmfq2VsL2FtKV7Q"
CREDENTIALS_PATH = "/home/user/bot_laukpauk/credentials.json"

async def set_harga(update: Update, context: ContextTypes.DEFAULT_TYPE):
    user_id = update.effective_user.id
    if user_id != ADMIN_USER_ID:
        await update.message.reply_text("❌ Anda tidak punya izin untuk mengubah harga.")
        return

    try:
        *nama_parts, harga_baru = context.args
        nama_produk = " ".join(nama_parts).strip()
        harga_baru = int(float(harga_baru))

        # ✅ 1. Update Google Sheets
        gc = gspread.service_account(filename=CREDENTIALS_PATH)
        sh = gc.open_by_key(SPREADSHEET_ID)
        worksheet = sh.worksheet("Laukpauk")
        data = worksheet.get_all_values()

        found = False
        for i, row in enumerate(data[1:], start=2):  # mulai dari baris ke-2 (skip header)
            if row[1].strip().lower() == nama_produk.lower():
                worksheet.update_cell(i, 3, harga_baru)  # kolom C = Harga
                found = True
                break

        if not found:
            await update.message.reply_text("❌ Produk tidak ditemukan di Google Sheets.")
            return

        # ✅ 2. Ambil semua data baru & Sync ke PostgreSQL
        records = worksheet.get_all_records()
        await bulk_upsert_products(records)

        await update.message.reply_text(
            f"✅ Harga produk *{nama_produk}* berhasil diubah menjadi *Rp {harga_baru:,}*\n🗂️ Data sudah di-sync ke database.",
            parse_mode="Markdown"
        )

    except ValueError:
        await update.message.reply_text("⚠️ Format salah. Gunakan: /set_harga [Nama Produk] [Harga Baru]")
    except Exception as e:
        await update.message.reply_text(f"❌ Terjadi error: {e}")
