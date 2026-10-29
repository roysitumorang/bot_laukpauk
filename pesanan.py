import os
import json
import time
from datetime import datetime, timedelta
from apscheduler.schedulers.asyncio import AsyncIOScheduler
from telegram import Update
from telegram.ext import ContextTypes
from utils_checkout import format_rupiah

scheduler = AsyncIOScheduler()
scheduler.start()

# ✅ Fungsi untuk simpan invoice baru ke file pending
def tambah_invoice_ke_pending(user_id, invoice_data):
    filename = f"pending_orders_{user_id}.json"
    data = []

    if os.path.exists(filename):
        with open(filename, "r") as f:
            data = json.load(f)

    data.append(invoice_data)
    with open(filename, "w") as f:
        json.dump(data, f)

    # ⏳ Jadwalkan penghapusan invoice ini dalam 1 jam
    scheduler.add_job(
        hapus_invoice_otomatis,
        "date",
        run_date=datetime.now() + timedelta(hours=1),
        args=[user_id, invoice_data["invoice_id"]]
    )

# 🗑 Hapus invoice otomatis jika tidak dibayar
def hapus_invoice_otomatis(user_id, invoice_id):
    filename = f"pending_orders_{user_id}.json"
    if not os.path.exists(filename):
        return

    with open(filename, "r") as f:
        data = json.load(f)

    data = [inv for inv in data if inv["invoice_id"] != invoice_id]

    if data:
        with open(filename, "w") as f:
            json.dump(data, f)
    else:
        os.remove(filename)

    print(f"🗑️ [EXPIRE] Invoice {invoice_id} user {user_id} dihapus otomatis.")

# ✅ Command /pesanan untuk menampilkan semua pesanan aktif
async def lihat_pesanan(update: Update, context: ContextTypes.DEFAULT_TYPE):
    user_id = update.effective_user.id
    filename = f"pending_orders_{user_id}.json"

    if not os.path.exists(filename):
        await update.message.reply_text("📭 Kamu belum punya pesanan tertunda.")
        return

    with open(filename, "r") as f:
        data = json.load(f)

    if not data:
        await update.message.reply_text("📭 Semua pesanan sudah dibayar atau dibatalkan.")
        return

    pesan = "🧾 *Daftar Pesanan Aktif Kamu:*"
    for i, inv in enumerate(data, start=1):
        waktu = datetime.fromtimestamp(inv["created_at"]).strftime("%H:%M")
        pesan += (
            f"\n{i}. 💰 {format_rupiah(inv['total'])} – {inv['metode']}\n"
            f"   🕒 {waktu} WIB\n"
            f"   🔗 [Bayar Sekarang]({inv['invoice_url']})\n"
            f"   🆔 ID: `{inv['invoice_id']}`\n"
        )

    pesan += "\nKetik `/batalkan [invoice_id]` untuk membatalkan salah satu."
    await update.message.reply_text(pesan, parse_mode="Markdown")

# ✅ Command /batalkan inv-id123
async def batalkan_invoice(update: Update, context: ContextTypes.DEFAULT_TYPE):
    user_id = update.effective_user.id
    if len(context.args) != 1:
        await update.message.reply_text("❌ Format salah! Gunakan: /batalkan [invoice_id]")
        return

    invoice_id = context.args[0]
    filename = f"pending_orders_{user_id}.json"

    if not os.path.exists(filename):
        await update.message.reply_text("📭 Tidak ada pesanan yang bisa dibatalkan.")
        return

    with open(filename, "r") as f:
        data = json.load(f)

    new_data = [inv for inv in data if inv["invoice_id"] != invoice_id]

    if len(data) == len(new_data):
        await update.message.reply_text("⚠️ Invoice ID tidak ditemukan.")
        return

    with open(filename, "w") as f:
        json.dump(new_data, f)

    await update.message.reply_text(f"✅ Pesanan dengan ID `{invoice_id}` telah dibatalkan.", parse_mode="Markdown")
