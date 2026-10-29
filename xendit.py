import requests
import os
import base64
import time
import json
from telegram import Update
from telegram.ext import CallbackContext
from dotenv import load_dotenv
from utils_checkout import DecimalEncoder, format_rupiah, reset_checkout


# Load API Key dari .env
dotenv_path = "/home/user/bot_laukpauk/.env"
load_dotenv(dotenv_path)

XENDIT_API_KEY = os.getenv("XENDIT_API_KEY")

if not XENDIT_API_KEY:
    raise ValueError("❌ [ERROR] API Key Xendit tidak ditemukan! Periksa file .env Anda.")

# ✅ Perbaikan: API Key tidak boleh memiliki ':' sebelum encoding
encoded_api_key = base64.b64encode(f"{XENDIT_API_KEY}:".encode()).decode()

HEADERS = {
    "Authorization": f"Basic {encoded_api_key}",
    "Content-Type": "application/json"
}

XENDIT_BASE_URL = "https://api.xendit.co/v2/invoices"

def buat_invoice(user_id, total_pembayaran, metode_pembayaran_list):
    """Membuat invoice Xendit dengan metode pembayaran yang dipilih"""

    print(f"📡 [DEBUG] Memanggil buat_invoice() untuk User {user_id}")
    
    external_id = f"invoice-{user_id}-{int(time.time())}"
    print(f"🔹 [DEBUG] External ID: {external_id}")

    data = {
        "external_id": external_id,
        "amount": float(total_pembayaran),
        "payer_email": f"user{user_id}@example.com",
        "description": "Pembayaran LaukPauk",
        "currency": "IDR",
        "success_redirect_url": "https://t.me/Laukpauk_Bot",
        "failure_redirect_url": "https://t.me/Laukpauk_Bot",
        "payment_methods": metode_pembayaran_list
    }

    print(f"📡 [DEBUG] Request ke Xendit:\n{json.dumps(data, indent=2, cls=DecimalEncoder)}")


    try:
        response = requests.post(XENDIT_BASE_URL, json=data, headers=HEADERS)

        print(f"📡 [DEBUG] Status Code: {response.status_code}")
        print(f"📡 [DEBUG] Response Text: {response.text}")  # Tambahan ini penting

        response_data = response.json()
        invoice_id = response_data.get("id")
        invoice_url = response_data.get("invoice_url")

        if invoice_id:
            print(f"✅ [DEBUG] Invoice berhasil dibuat: ID={invoice_id}")
            return invoice_url, invoice_id
        else:
            print("❌ [ERROR] invoice_id tidak ditemukan dalam response.")
    except Exception as e:
        print(f"❌ [ERROR] Terjadi kesalahan saat membuat invoice: {e}")
        print(f"📡 [RAW RESPONSE]: {response.text}")

    return None, None

def cek_status_invoice_xendit(invoice_id):
    url = f"https://api.xendit.co/v2/invoices/{invoice_id}"
    response = requests.get(url, headers=HEADERS)

    if response.status_code == 200:
        return response.json().get("status")
    else:
        print(f"❌ Gagal mengecek status invoice: {response.text}")
        return None



async def cek_status_pembayaran(update: Update, context: CallbackContext):
    await update.message.reply_text("Cek status pembayaran...")
    """Mengecek status pembayaran berdasarkan invoice_id"""
    user_id = update.message.chat_id
    invoice_id = context.user_data.get("invoice_id")

    if not invoice_id:
        await update.message.reply_text("❌ Anda belum memiliki pembayaran yang perlu dicek.")
        return

    status = cek_status_invoice_xendit(invoice_id)  # Panggil fungsi dari xendit.py

    if status == "PAID":
        await update.message.reply_text("✅ Pembayaran Anda telah berhasil! Terima kasih.")
        context.user_data["checkout_step"] = "done"  # Tandai sebagai selesai
    elif status == "PENDING":
        await update.message.reply_text("⏳ Pembayaran masih dalam proses. Silakan tunggu beberapa saat.")
    else:
        await update.message.reply_text("❌ Pembayaran belum dilakukan atau gagal. Silakan coba lagi.")




