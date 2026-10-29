import os
import requests
import base64
import json
from telegram import InlineKeyboardButton, InlineKeyboardMarkup, Update
from telegram.ext import CallbackContext
from dotenv import load_dotenv
from xendit import buat_invoice, cek_status_invoice_xendit
from invoice_mapping import simpan_mapping_ke_file
from utils_checkout import DecimalEncoder, format_rupiah, reset_checkout
from decimal import Decimal



# Load .env
dotenv_path = "/home/user/bot_laukpauk/.env"
load_dotenv(dotenv_path)

XENDIT_API_KEY = os.getenv("XENDIT_API_KEY")

# 🔐 Channel pembayaran yang diizinkan (aktif di Xendit dashboard)
PAYMENT_CHANNELS = {
    "pay_va": ["BNI", "BRI", "BSI", "MANDIRI"],
    "pay_ewallet": ["OVO", "SHOPEEPAY"],
    "pay_qris": ["QRIS"],
    "pay_cc": ["CREDIT_CARD"]
}

# ✅ Tampilkan metode pembayaran ke user
async def tampilkan_metode_pembayaran(bot, user_id):
    keyboard = [
        [InlineKeyboardButton("🏦 Transfer Bank", callback_data="pay_va")],
        [InlineKeyboardButton("💳 Kartu Kredit", callback_data="pay_cc")],
        [InlineKeyboardButton("📲 E-Wallet", callback_data="pay_ewallet")],
        [InlineKeyboardButton("📸 QRIS", callback_data="pay_qris")]
    ]
    reply_markup = InlineKeyboardMarkup(keyboard)

    await bot.send_message(
        chat_id=user_id,
        text="📌 *Pilih metode pembayaran:*",
        reply_markup=reply_markup,
        parse_mode="Markdown"
    )

# ✅ Tangani klik tombol metode pembayaran
async def handle_pilihan_pembayaran(update: Update, context: CallbackContext):
    query = update.callback_query
    user_id = query.from_user.id
    pilihan = query.data

    await query.answer()

    # ✅ CEGAH proses ulang jika user sudah bayar
    if context.user_data.get("checkout_step") == "done":
        await query.message.reply_text("✅ Anda sudah menyelesaikan pembayaran sebelumnya.")
        return

    total_pembayaran = Decimal(str(context.user_data.get("total_pembayaran", 0)))
    if total_pembayaran is None or total_pembayaran <= 0:
        await query.message.reply_text("❌ Total pembayaran tidak valid. Silakan ulangi pemesanan.")
        return

    metode_list = PAYMENT_CHANNELS.get(pilihan)
    if not metode_list:
        await query.message.reply_text("❌ Metode pembayaran tidak valid atau belum tersedia.")
        return

    # ⛔ CEK APAKAH SUDAH ADA INVOICE AKTIF
    if "invoice_id" in context.user_data:
        existing_invoice_id = context.user_data["invoice_id"]
        status = cek_status_invoice_xendit(existing_invoice_id)
        print(f"🔎 [DEBUG] Status invoice sebelumnya ({existing_invoice_id}): {status}")

        if status in ["PENDING", "PAID"]:
            await query.message.reply_text(
                f"⚠️ Anda sudah memiliki invoice yang masih aktif (status: {status}).\n"
                f"Silakan selesaikan pembayaran terlebih dahulu atau tunggu konfirmasi."
            )
            return
        else:
            # Bersihkan invoice lama yang gagal
            context.user_data.pop("invoice_id", None)
            context.user_data.pop("invoice_url", None)

    # ✅ Buat invoice baru
    await query.message.reply_text("✅ Metode pembayaran dipilih. Mohon tunggu instruksi pembayaran dikirim...")

    invoice_url, invoice_id = buat_invoice(user_id, total_pembayaran, metode_list)
    if not invoice_id:
        await query.message.reply_text("❌ Gagal membuat invoice. Coba lagi nanti.")
        return

    # Simpan invoice ke context dan file
    context.user_data["invoice_id"] = invoice_id
    context.user_data["invoice_url"] = invoice_url
    simpan_mapping_ke_file(invoice_id, user_id)

    await query.message.reply_text(
        f"💰 Total: {format_rupiah(total_pembayaran)}",
        reply_markup=InlineKeyboardMarkup([
            [InlineKeyboardButton("🔗 Bayar Sekarang", url=invoice_url)]
        ])
    )

    # 🛡️ Cek apakah invoice sudah dibayar → jangan timpa ulang kalau sudah lunas
    if context.user_data.get("invoice_id") and context.user_data.get("checkout_step") == "invoice_dikirim":
        status_invoice = cek_status_invoice_xendit(context.user_data["invoice_id"])
        if status_invoice == "PAID":
            print("🚫 Invoice sudah dibayar, tidak perlu simpan ulang invoice_dikirim.")
            return  # Stop di sini supaya tidak menimpa state yang sudah selesai

    # ✅ Lanjutkan hanya jika belum dibayar
    if context.user_data.get("checkout_step") not in [None, "done", "invoice_dikirim"]:
        context.user_data["checkout_step"] = "invoice_dikirim"


    with open(f"user_data_{user_id}.json", "w") as f:
        print(f"🛡️ [WRITE] Menyimpan checkout_step = {context.user_data.get('checkout_step')}")
        json.dump(context.user_data, f, cls=DecimalEncoder)



# ✅ (Optional) Fungsi jika dipanggil manual
async def proses_pembayaran(user_id, total_pembayaran, metode_pembayaran, context):
    metode_list = [metode_pembayaran]
    invoice_url, invoice_id = buat_invoice(user_id, total_pembayaran, metode_list)

    if not invoice_id:
        await context.bot.send_message(user_id, "❌ Gagal membuat invoice. Silakan coba lagi nanti.")
        return False

    context.user_data["invoice_id"] = invoice_id
    context.user_data["invoice_url"] = invoice_url

    await context.bot.send_message(
        user_id,
        f"✅ Pembayaran Anda telah dibuat!\n\n💰 *Total:* {format_rupiah(total_pembayaran)}\n"
        f"🔗 Klik untuk membayar: {invoice_url}",
        parse_mode="Markdown"
    )
    
    # 🛡️ Simpan data pelanggan ke database
    nama = context.user_data.get("nama")
    alamat = context.user_data.get("alamat")
    nomor_hp = context.user_data.get("hp")
    detail_alamat = context.user_data.get("detail_alamat")
    produk_terakhir = context.user_data.get("cart")
    total_poin = context.user_data.get("total_poin", 0)

    insert_pelanggan(
        user_id=user_id,
        nama=nama,
        alamat=alamat,
        hp=nomor_hp,
        detail_alamat=detail_alamat,
        produk_terakhir=json.dumps(produk_terakhir),
        total_poin=total_poin
    )

    return True

# ✅ Fungsi cadangan (jika suatu saat sistem manual)
async def konfirmasi_pembayaran(update, context, user_id, metode_pembayaran):
    await context.bot.send_message(user_id, f"✅ Pembayaran dengan {metode_pembayaran} berhasil! Invoice akan segera dikirim...")
    context.user_data["checkout_step"] = "done"
