import os
import math
from datetime import datetime, timedelta
import time
from apscheduler.schedulers.asyncio import AsyncIOScheduler
import pytz
from fastapi import FastAPI, Request
from starlette.middleware.trustedhost import TrustedHostMiddleware
from dotenv import load_dotenv
from telegram import Update, InlineKeyboardButton, InlineKeyboardMarkup
from telegram.ext import Application, CommandHandler, CallbackQueryHandler, MessageHandler, filters, ContextTypes
from status_pesanan import status_pesanan
from status_pesanan import update_status_pesanan
from testimoni import beri_testimoni
from testimoni import lihat_testimoni
from testimoni import handle_rating
from testimoni import handle_testimoni
from kirim_barang import kirim_barang_callback, tampilkan_tombol_kirim_barang
from xendit import buat_invoice, cek_status_pembayaran
from xendit import XENDIT_API_KEY
from xendit import cek_status_invoice_xendit
from invoice_mapping import simpan_mapping_ke_file
from utils_checkout import generate_invoice, get_poin_pelanggan, simpan_data_pelanggan, format_rupiah, reset_checkout
from product_service import get_all_products, get_db_connection, get_pelanggan_from_db
from decimal import Decimal
import sys
sys.path.append("/home/user/bot_laukpauk/")
from tutup_toko import is_holiday, set_holiday_dates, get_holiday_dates, save_holidays
from utils_checkout import DecimalEncoder
from poin import hitung_poin, rekomendasi_tambahan_belanja
from product_service import simpan_pelanggan_ke_postgres
from handle_payment import (
    tampilkan_metode_pembayaran,  # ✅ Tambahkan ini
    proses_pembayaran,
    konfirmasi_pembayaran,
    handle_pilihan_pembayaran
)
import gspread
import googlemaps
from reportlab.lib.pagesizes import A4
from reportlab.pdfgen import canvas
from reportlab.lib.utils import ImageReader
from multiprocessing import Process
import uvicorn
import traceback
import threading
import asyncio
import orjson
import logging
import importlib
import json

produk = {}  # deklarasi awal, biar gak error di handler

# ✅ Load variabel dari file .env
load_dotenv()

# ✅ Ambil API key dan token dari environment variables
GOOGLE_MAPS_API_KEY = os.getenv("GOOGLE_MAPS_API_KEY")
TOKEN = os.getenv("BOT_TOKEN")

app = FastAPI()

bot_app = Application.builder().token(TOKEN).build()

@app.get("/")
def home():
    return {"message": "API LaukPauk is running!"}

# Middleware harus ditambahkan setelah app dideklarasikan
app.add_middleware(TrustedHostMiddleware, allowed_hosts=["*"])

logging.basicConfig(level=logging.DEBUG)

async def error_handler(update: Update, context: ContextTypes.DEFAULT_TYPE):
    print("❌ Unhandled error:", context.error)
    traceback.print_exc()
    # Optional: kirim ke admin atau log file


bot_app.add_error_handler(error_handler)

# Set zona waktu Asia/Jakarta
JAKARTA_TZ = pytz.timezone("Asia/Jakarta")

scheduler = AsyncIOScheduler(timezone=JAKARTA_TZ)

ADMIN_USER_ID = 5464224951  # Gantilah dengan ID admin yang valid

WEBHOOK_URL = "https://bot.laukpauk.id/webhook/telegram"

@app.on_event("startup")
async def on_startup():
    webhook_module = importlib.import_module("webhook_xendit")
    app.include_router(webhook_module.router)
    logging.info("✅ Router Xendit webhook berhasil di-load.")

    # ✅ Ambil data produk dari database saat startup
    global produk
    produk = await get_all_products()
    logging.info("✅ Data produk berhasil dimuat dari PostgreSQL")


@app.on_event("shutdown")
async def shutdown():
    await bot_app.stop()
    await bot_app.shutdown()


# ✅ Telegram Webhook Handler
@app.post("/webhook/telegram")
async def telegram_webhook(request: Request):
    try:
        data = await request.json()
        logging.info(f"✅ Webhook received: {json.dumps(data, indent=2)}")

        update = Update.de_json(data, bot_app.bot)  # ✅ tambahkan bot

        logging.info("➡️  Processing update via queue...")
        await bot_app.update_queue.put(update)  # ✅ cara aman dan disarankan
        return {"status": "success"}
    except Exception as e:
        logging.error(f"❌ Error processing webhook: {e}")
        return {"status": "error", "message": str(e)}


# ✅ Fungsi untuk Setup Webhook Telegram
async def set_webhook():
    await bot_app.bot.delete_webhook(drop_pending_updates=True)
    success = await bot_app.bot.set_webhook(url=WEBHOOK_URL)

    if success:
        logging.info(f"✅ Webhook berhasil diatur: {WEBHOOK_URL}")
    else:
        logging.error("❌ Gagal mengatur webhook!")




# ✅ **Cache untuk Produk dan Pelanggan**
produk_cache = {}
pelanggan_cache = {}
cache_expiry = 300  # Waktu kedaluwarsa cache dalam detik
last_cache_update = 0



# 🔥 **Konfigurasi Debugging**
DEBUG_MODE = False  # Ubah ke True hanya saat debugging

if DEBUG_MODE:
    print(f"🔍 API Key: {GOOGLE_MAPS_API_KEY}")
    print(f"🔍 Bot Token: {TOKEN}")

# 🛡️ **Validasi jika API Key atau Token tidak ditemukan**
if not GOOGLE_MAPS_API_KEY:
    raise ValueError("❌ GOOGLE_MAPS_API_KEY tidak ditemukan! Pastikan sudah ada di .env")

if not TOKEN:
    raise ValueError("❌ BOT_TOKEN tidak ditemukan! Pastikan sudah ada di .env")


# ✅ Inisialisasi Google Maps API dengan API Key dari .env
gmaps = googlemaps.Client(key=GOOGLE_MAPS_API_KEY)


# ✅ Konfigurasi Google Sheets
SPREADSHEET_ID = "157srz4epQMUJ5q_fo3Q8FkrBdo40qmfq2VsL2FtKV7Q"
CREDENTIALS_PATH = "/home/user/bot_laukpauk/credentials.json"
RANGE_NAME = "Laukpauk!A:E"

# ✅ Inisialisasi Google Sheets
gc = gspread.service_account(filename="/home/user/bot_laukpauk/credentials.json")
sh = gc.open_by_key(SPREADSHEET_ID)

ONGKOS_PER_KM = 3000  # Bisa diatur manual
LOKASI_TOKO = (3.539174131612625, 98.64453142910467)  # Koordinat toko

ADMIN_USER_ID = 5464224951




async def tutup_toko(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Admin menutup toko dengan menentukan tanggal libur."""
    user_id = update.effective_user.id
    if user_id != ADMIN_USER_ID:
        await update.message.reply_text("❌ Anda tidak memiliki izin untuk menutup toko.")
        return

    if len(context.args) != 2:
        await update.message.reply_text("❌ Format salah! Gunakan: /tutup_toko DD-MM-YYYY DD-MM-YYYY")
        return

    try:
        start_date = datetime.strptime(context.args[0], "%d-%m-%Y").strftime("%Y-%m-%d")
        end_date = datetime.strptime(context.args[1], "%d-%m-%Y").strftime("%Y-%m-%d")
    except ValueError:
        await update.message.reply_text("❌ Format tanggal salah! Gunakan: /tutup_toko DD-MM-YYYY DD-MM-YYYY")
        return

    message = set_holiday_dates(start_date, end_date)
    await update.message.reply_text(f"✅ {message}")

def baca_data_dari_db():
    try:
        loop = asyncio.get_running_loop()
    except RuntimeError:
        loop = None

    if loop and loop.is_running():
        # Kalau loop sedang jalan, jalankan async lewat task
        return asyncio.ensure_future(get_all_products())
    else:
        return asyncio.run(get_all_products())

async def buka_toko(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Admin membuka kembali toko dengan menghapus semua tanggal libur."""
    user_id = update.effective_user.id
    if user_id != ADMIN_USER_ID:
        await update.message.reply_text("❌ Anda tidak memiliki izin untuk membuka toko.")
        return
    
    # ✅ Hapus semua tanggal libur dengan menyimpan array kosong di JSON
    save_holidays([])

    await update.message.reply_text("✅ Toko telah dibuka kembali.")



def get_delivery_times():
    now = datetime.now(JAKARTA_TZ)
    current_hour = now.hour
    today = now.strftime("%d-%m-%Y")
    besok = (now + timedelta(days=1)).strftime("%d-%m-%Y")
    lusa = (now + timedelta(days=2)).strftime("%d-%m-%Y")
    
    # Cek apakah toko libur
    if is_holiday(now):
        return []  # Tidak ada opsi pengiriman saat toko libur
    
    # Jika hari ini Minggu dan pesanan dibuat setelah 22:00, alihkan ke Senin pagi
    if now.weekday() == 6 and current_hour >= 22:
        return [
            [InlineKeyboardButton(f"{besok} – 08:00", callback_data=f"delivery_{besok}_08:00")],
            [InlineKeyboardButton(f"{besok} – 10:00", callback_data=f"delivery_{besok}_10:00")],
            [InlineKeyboardButton(f"{besok} – 12:00", callback_data=f"delivery_{besok}_12:00")],
        ]
    
    if current_hour < 10:  # Pesanan sebelum 10:00
        return [
            [InlineKeyboardButton(f"{today} – 12:00", callback_data=f"delivery_{today}_12:00")],
            [InlineKeyboardButton(f"{besok} – 08:00", callback_data=f"delivery_{besok}_08:00")],
            [InlineKeyboardButton(f"{besok} – 10:00", callback_data=f"delivery_{besok}_10:00")],
            [InlineKeyboardButton(f"{besok} – 12:00", callback_data=f"delivery_{besok}_12:00")],
        ]
    elif 10 <= current_hour < 22:  # Pesanan antara 10:01 - 21:59
        return [
            [InlineKeyboardButton(f"{besok} – 08:00", callback_data=f"delivery_{besok}_08:00")],
            [InlineKeyboardButton(f"{besok} – 10:00", callback_data=f"delivery_{besok}_10:00")],
            [InlineKeyboardButton(f"{besok} – 12:00", callback_data=f"delivery_{besok}_12:00")],
        ]
    else:  # Pesanan setelah 22:01
        return [
            [InlineKeyboardButton(f"{besok} – 12:00", callback_data=f"delivery_{besok}_12:00")],
            [InlineKeyboardButton(f"{lusa} – 08:00", callback_data=f"delivery_{lusa}_08:00")],
            [InlineKeyboardButton(f"{lusa} – 10:00", callback_data=f"delivery_{lusa}_10:00")],
            [InlineKeyboardButton(f"{lusa} – 12:00", callback_data=f"delivery_{lusa}_12:00")],
        ]

async def announce_holiday(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Menampilkan pengumuman jika toko sedang libur."""
    now = datetime.now(JAKARTA_TZ)
    if is_holiday(now):
        holiday_dates = get_holiday_dates()
        await update.message.reply_text(f"🚨 Toko sedang libur dari {holiday_dates}. Mohon lakukan pemesanan di luar tanggal ini.")
        return



# ✅ **Inisialisasi Google Sheets untuk pelanggan**
try:
    worksheet_pelanggan = sh.worksheet("Pelanggan")
except gspread.exceptions.WorksheetNotFound:
    worksheet_pelanggan = sh.add_worksheet(title="Pelanggan", rows="1000", cols="7")
    worksheet_pelanggan.append_row(["User ID", "Nama", "Alamat", "HP", "Detail Alamat", "Produk Terakhir", "Total Poin"])
    print("✅ [DEBUG] Sheet 'Pelanggan' dibuat dengan header lengkap")

# ✅ **Pastikan header 'Total Poin' ada di kolom G**
header_pelanggan = worksheet_pelanggan.row_values(1)

if len(header_pelanggan) < 7:  # Jika jumlah kolom kurang dari 7
    worksheet_pelanggan.update("G1", [["Total Poin"]])  # Tambahkan judul di kolom G
    print("✅ [DEBUG] Header 'Total Poin' ditambahkan di kolom G")
elif header_pelanggan[6] != "Total Poin":  # Jika kolom G bukan 'Total Poin'
    worksheet_pelanggan.update("G1", [["Total Poin"]])
    print("✅ [DEBUG] Header 'Total Poin' diperbaiki di kolom G")


# Cek dan buat sheet "Pesanan" jika belum ada
try:
    worksheet = sh.worksheet("Pesanan")
except gspread.exceptions.WorksheetNotFound:
    worksheet = sh.add_worksheet(title="Pesanan", rows="1000", cols="11")  # 11 kolom sesuai dengan header
    worksheet.append_row(["User ID", "Nama", "Alamat", "HP", "Kode Voucher", "Diskon Voucher", "Poin Digunakan", "Total Belanja", "Ongkos Kirim", "Total Pembayaran", "Waktu Pesanan"])



keranjang = {}
pesanan = {}
harga_lama = {}
vouchers = {
    "DISKON10": {"tipe": "belanja", "nilai": 1000, "min_belanja": 50000, "batas": 50},
    "ONGKIR5": {"tipe": "ongkir", "nilai": 5000, "min_belanja": 30000, "batas": 30},
    "DISKON25": {"tipe": "belanja", "nilai": 25000, "min_belanja": 100000, "batas": 20},
    "ONGKIR10": {"tipe": "ongkir", "nilai": 10000, "min_belanja": 75000, "batas": 15},
}


async def start(update: Update, context: ContextTypes.DEFAULT_TYPE):
    context.user_data.pop("checkout_step", None)
    user_id = update.effective_user.id
    print(f"📥 Menerima /start dari User ID: {user_id}")  # 🔍 Debugging
    

    # 🔄 Ambil data pelanggan dari Database
    data_pelanggan = await get_pelanggan_from_db(user_id)
    print(f"🔍 Data pelanggan: {data_pelanggan}")  # 🔍 Debugging

    # ✅ Jika pelanggan tidak ditemukan, hapus cache agar tidak ada data lama
    if not data_pelanggan:  
        context.user_data.clear()

    # 🔍 Jika pelanggan lama, gunakan data yang sudah ada
    if data_pelanggan:
        context.user_data.update(data_pelanggan)  # Simpan di cache lokal
        pesan = f"👋 Selamat datang kembali, {data_pelanggan['nama']}!\n\n✅ langsung cuss belanja!"
    else:
        pesan = "👋 Selamat datang di Bot Laukpauk!\nKetik aja produk yang ingin dibeli ya. Mis: Brokoli"

    print(f"📤 Mengirim pesan: {pesan}")  # 🔍 Debugging
    await update.message.reply_text(pesan)



async def update_harga(update: Update, context: ContextTypes.DEFAULT_TYPE):
    global produk
    """Admin memperbarui harga dan mengirim notifikasi ke pelanggan."""
    user_id = update.effective_user.id

    if user_id != ADMIN_USER_ID:
        await update.message.reply_text("⚠️ Anda tidak memiliki izin untuk memperbarui harga.")
        return

    print("✅ [DEBUG] Admin menjalankan /update_harga")
    await update.message.reply_text("🔄 Memeriksa perubahan harga...")

    global produk  
    produk_lama = produk.copy()  # Ambil snapshot produk lama

    # ✅ Ambil produk baru dari database
    produk_baru = await get_all_products()

    if not produk_baru:
        await update.message.reply_text("❌ ERROR: Data produk tidak ditemukan di database!")
        print("❌ [DEBUG] Data produk kosong, tidak bisa update harga.")
        return

    perubahan_harga = []
    for kategori, daftar_produk in produk_baru.items():
        for nama, data in daftar_produk.items():
            harga_sebelumnya = produk_lama.get(kategori, {}).get(nama, {}).get("harga", data["harga"])
            harga_baru = data["harga"]

            if harga_baru < harga_sebelumnya:  # Hanya harga turun
                perubahan_harga.append(f"🔻 {nama.capitalize()}: Rp {harga_sebelumnya:,} ➝ Rp {harga_baru:,}")

    # ✅ Update cache global produk
    produk = produk_baru

    if perubahan_harga:
        pesan = "📢 **Harga Turun! Yuk, Belanja Sekarang!**\n\n" + "\n".join(perubahan_harga)

        print(f"✅ [DEBUG] Update harga berhasil. Jumlah perubahan: {len(perubahan_harga)}")
        await update.message.reply_text(f"✅ **Update Harga Berhasil!**\n\n{pesan}")

        # ✅ Ambil daftar pelanggan dari database
        conn = await get_db_connection()
        rows = await conn.fetch("SELECT user_id FROM pelanggan")
        pelanggan_ids = [int(row["user_id"]) for row in rows if str(row["user_id"]).isdigit()]
        await conn.close()

        print(f"✅ [DEBUG] Ditemukan {len(pelanggan_ids)} pelanggan untuk notifikasi.")

        # Kirim notifikasi ke pelanggan (kecuali admin)
        for pelanggan_id in pelanggan_ids:
            if pelanggan_id != ADMIN_USER_ID:
                try:
                    await context.bot.send_message(chat_id=pelanggan_id, text=pesan)
                    print(f"✅ [DEBUG] Notifikasi dikirim ke pelanggan ID {pelanggan_id}")
                except Exception as e:
                    print(f"❌ [ERROR] Gagal mengirim notifikasi ke {pelanggan_id}: {e}")

        await update.message.reply_text("📢 **Notifikasi harga turun telah dikirim ke pelanggan.**")
    
    else:
        print("✅ [DEBUG] Tidak ada harga yang turun.")
        await update.message.reply_text("✅ **Tidak ada perubahan harga terbaru.**")


# Fungsi untuk admin menambahkan voucher
async def tambah_voucher(update: Update, context: ContextTypes.DEFAULT_TYPE):
    user_id = update.effective_user.id
    if user_id != ADMIN_USER_ID:
        await update.message.reply_text("⚠️ Anda tidak memiliki izin untuk menambahkan voucher.")
        return

    try:
        args = context.args
        if len(args) < 4:
            await update.message.reply_text("❌ Format salah! Gunakan: /tambah_voucher [kode] [tipe] [nilai] [min_belanja]")
            return

        kode, tipe, nilai, min_belanja = args[0].upper(), args[1].lower(), int(args[2]), int(args[3])
        if tipe not in ["belanja", "ongkir"]:
            await update.message.reply_text("⚠️ Tipe harus 'belanja' atau 'ongkir'.")
            return

        vouchers[kode] = {"tipe": tipe, "nilai": nilai, "min_belanja": min_belanja}
        await update.message.reply_text(f"✅ Voucher {kode} berhasil ditambahkan!")
    except Exception as e:
        await update.message.reply_text(f"❌ Terjadi kesalahan: {e}")


# Fungsi untuk Menampilkan Daftar Kategori
async def kategori(update: Update, context: ContextTypes.DEFAULT_TYPE):
    global produk
    print("🔍 Debugging Produk di /kategori:", produk)  # Debugging

    # ✅ Blokir pencarian produk jika sudah buat invoice
    if context.user_data.get("checkout_step") == "invoice_dikirim":
        await update.message.reply_text(
            "⚠️ Kamu sedang dalam proses checkout.\n"
            "Selesaikan pesanan, atau buat pesanan baru jika ingin mulai ulang.",
            reply_markup=InlineKeyboardMarkup([
                [InlineKeyboardButton("❌ Batalkan Pesanan", callback_data="batal_pesanan")]
            ])
        )
        return

    if not isinstance(produk, dict) or not produk:
        await update.message.reply_text("⚠️ Data produk tidak valid atau kosong. Coba muat ulang bot.")
        print("❌ ERROR: Produk tidak valid atau kosong di kategori!")  # Debugging
        return

    print("✅ Data kategori yang ditemukan:", produk.keys())  # Debugging

    keyboard = [[InlineKeyboardButton(kat, callback_data=f"kategori_{kat}")] for kat in produk.keys()]
    reply_markup = InlineKeyboardMarkup(keyboard)
    await update.message.reply_text("📦 Silakan pilih kategori:", reply_markup=reply_markup)

# Fungsi untuk Memuat Ulang Data Produk dari Google Sheets
async def reload_produk(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Memuat ulang data produk tanpa restart bot"""
    global produk
    produk = baca_data_google_sheets()  # Muat ulang data produk
    await update.message.reply_text("🔄 Data produk telah diperbarui dari Google Sheets!")

async def show_kategori(update: Update, context: ContextTypes.DEFAULT_TYPE):
    global produk
    """Menampilkan daftar produk dalam kategori yang dipilih."""
    query = update.callback_query
    await query.answer()

    kategori = query.data.replace("kategori_", "")
    if kategori in produk:
        print(f"📂 Menampilkan produk untuk kategori: {kategori}")  # Debugging

        keyboard = [[InlineKeyboardButton(f"{p} - Rp {data['harga']:,}", callback_data=f"produk_{kategori}_{p}")]
                    for p, data in produk[kategori].items()]
        
        reply_markup = InlineKeyboardMarkup(keyboard)
        await query.message.reply_text(f"🛍️ Produk dalam kategori {kategori}:", reply_markup=reply_markup)
    else:
        await query.message.reply_text("⚠️ Kategori tidak ditemukan.")

# Fungsi untuk Menangani Pencarian Produk
async def menu_produk(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Meminta pengguna memasukkan nama produk untuk mencari."""
    await update.message.reply_text("Silakan masukkan nama produk yang Anda cari:")

# Fungsi Pencarian Produk
async def cari_produk(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Menampilkan warning jika toko tutup sebelum user mencari produk."""

    # ✅ Blokir pencarian produk jika masih ada invoice aktif
    invoice_id = context.user_data.get("invoice_id")
    if context.user_data.get("checkout_step") == "invoice_dikirim" and invoice_id:
        status = cek_status_invoice_xendit(invoice_id)
        print(f"🔍 [DEBUG] Status invoice saat cari produk: {status}")

        if status in ["PENDING"]:
            await update.message.reply_text(
                "⚠️ Kamu sedang dalam proses checkout.\n"
                "Selesaikan pesanan, atau buat pesanan baru jika ingin mulai ulang.",
                reply_markup=InlineKeyboardMarkup([
                    [InlineKeyboardButton("❌ Batalkan Pesanan", callback_data="batal_pesanan")]
                ])
            )
            return

    now = datetime.now(JAKARTA_TZ)

    checkout_step = context.user_data.get("checkout_step")
    text = update.message.text.strip()  # ✅ Tambahan ini penting!

    # ✅ 1. Jika sedang di waktu_pengiriman atau metode_pembayaran dan user ngetik teks
    if checkout_step in ["waktu_pengiriman", "metode_pembayaran"] and text.isalpha():
        print(f"⛔ [BLOCK] User ketik produk saat checkout_step = {checkout_step}")

        # Pilih warning sesuai step
        if checkout_step == "waktu_pengiriman":
            warning_text = (
                "⚠️ Kamu sedang dalam proses checkout.\n"
                "Selesaikan pesanan, atau buat pesanan baru jika ingin mulai ulang."
            )
        else:
            warning_text = (
                "⚠️ Kamu sedang dalam proses checkout.\n"
                "Selesaikan pesanan, atau buat pesanan baru jika ingin mulai ulang."
            )

        keyboard = [[InlineKeyboardButton("❌ Batalkan Pesanan", callback_data="batal_pesanan")]]
        reply_markup = InlineKeyboardMarkup(keyboard)

        await update.message.reply_text(warning_text, reply_markup=reply_markup)
        return

    if is_holiday(now):
        holiday_dates = get_holiday_dates()
        await update.message.reply_text(f"🚨 Toko sedang libur dari {holiday_dates}. Mohon lakukan pemesanan di luar tanggal ini.")
        return  # Stop pencarian produk jika toko tutup
    
    # Lanjutkan dengan pencarian produk jika toko tidak tutup

    # **Debugging apakah fungsi ini tetap dipanggil setelah input checkout**
    print(f"🔎 [DEBUG] Menerima input di cari_produk: {update.message.text}")

    # **Cegah pencarian produk jika user sedang dalam sesi checkout atau baru selesai checkout**
    CHECKOUT_STEPS_BLOCKED = {
    "nama", "alamat", "detail_alamat", "hp", "poin",
    "waktu_pengiriman", "metode_pembayaran"
}

    if context.user_data.get("checkout_step") in CHECKOUT_STEPS_BLOCKED:
        print("⚠️ Checkout sedang berlangsung. Cegah pencarian produk.")
        return


    if context.user_data.get("checkout_done"):
        print(f"✅ [DEBUG] Checkout selesai. Pencarian produk diizinkan kembali.")
        context.user_data.pop("checkout_done", None)  # Hapus flag agar bisa cari produk

    query_text = update.message.text.lower().strip()

    if not isinstance(produk, dict) or not produk:
        await update.message.reply_text("⚠️ Data produk tidak valid atau kosong.")
        return

    hasil = []
    # Cari produk berdasarkan input pengguna
    for kategori, daftar_produk in produk.items():
        for nama_produk, data in daftar_produk.items():
            if query_text in nama_produk:
                harga_diskon = data['harga'] - data['diskon']
                hasil.append({
                    "nama": nama_produk.capitalize(),
                    "harga": harga_diskon,
                    "diskon": data['diskon'],
                    "kategori": kategori
                })

    if not hasil:
        await update.message.reply_text("❌ Produk tidak ditemukan. Pastikan ejaan benar atau coba nama produk lain.")
        return

    if len(hasil) == 1:
        produk_terpilih = hasil[0]
        reply_markup = InlineKeyboardMarkup([
            [InlineKeyboardButton(f"🛒 Beli {produk_terpilih['nama']}",
                                  callback_data=f"beli_{produk_terpilih['kategori']}_{produk_terpilih['nama'].lower()}_1")]
        ])
        await update.message.reply_text(
            f"{produk_terpilih['nama']} - Rp {produk_terpilih['harga']:,} (Diskon Rp {produk_terpilih['diskon']:,})",
            reply_markup=reply_markup
        )
        return

    keyboard = [
        [InlineKeyboardButton(f"{p['nama']} - Rp {p['harga']:,}",
                              callback_data=f"beli_{p['kategori']}_{p['nama'].lower()}_1")]
        for p in hasil
    ]
    reply_markup = InlineKeyboardMarkup(keyboard)
    await update.message.reply_text("🔍 Beberapa produk ditemukan, pilih yang ingin Anda beli:", reply_markup=reply_markup)


# Fungsi untuk Menghitung Ongkir Berdasarkan Jarak
def hitung_ongkir(jarak_km):
    """
    Menghitung ongkos kirim berdasarkan jarak.
    """
    ONGKOS_DASAR = 5600  # Ongkos dasar baru
    TARIF_PER_KM = 2000  # Tarif per km baru

    return ONGKOS_DASAR + (math.ceil(jarak_km) * TARIF_PER_KM)  # Pembulatan ke atas setiap km

# Fungsi untuk Menampilkan Detail Produk
async def show_produk(update: Update, context: ContextTypes.DEFAULT_TYPE):
    global produk
    """Menampilkan detail produk setelah dipilih dari kategori."""
    query = update.callback_query
    await query.answer()

    _, kategori, nama_produk = query.data.split("_")
    if kategori in produk and nama_produk in produk[kategori]:
        data = produk[kategori][nama_produk]
        harga_asli = data["harga"]
        diskon = data["diskon"]
        stok = data["stok"]
        harga_diskon = harga_asli - diskon if diskon > 0 else harga_asli

        harga_tampil = f"Rp {harga_diskon:,}" + (f" (Rp {harga_asli:,})" if diskon > 0 else "")
        reply_markup = InlineKeyboardMarkup([
            [InlineKeyboardButton("Beli Cepat", callback_data=f"beli_{kategori}_{nama_produk}_1")]
        ])
        await query.message.reply_text(
            f"Produk: {nama_produk}\nHarga: {harga_tampil}\nStok: {stok} pcs\n\nGunakan tombol di bawah untuk beli cepat:", 
            reply_markup=reply_markup
        )

# Fungsi untuk Melihat Keranjang
async def lihat_keranjang(update: Update, context: ContextTypes.DEFAULT_TYPE):
    context.user_data.pop("checkout_done", None)  # Reset flag agar pencarian produk bisa berjalan kembali
    """Menampilkan isi keranjang belanja."""
    user_id = update.effective_user.id
    if user_id not in keranjang or not keranjang[user_id]:
        await update.message.reply_text("Keranjang Anda kosong!")
        return

    total_belanja = sum(
        Decimal(str(item["jumlah"])) * Decimal(str(item["harga"]))
        for item in keranjang[user_id].values()
    )
    poin_sekarang = hitung_poin(total_belanja)

    # ✅ Cek apakah ada peluang menaikkan poin dengan tambahan belanja
    tambahan, poin_naik = rekomendasi_tambahan_belanja(total_belanja)
    
    pesan = f"🛒 **Total Belanja Anda: {format_rupiah(total_belanja)}**\n"
    pesan += f"🎯 Anda akan mendapatkan **{poin_sekarang} poin** dari belanjaan ini.\n"

    if tambahan and poin_naik:
        pesan += f"\n💡 **Dengan menambah belanja {format_rupiah(tambahan)}, Anda bisa mendapatkan {poin_naik} poin!**"

    await update.message.reply_text(pesan)

    pesan = "🛍 **Keranjang Anda:**\n"
    total = 0
    for produk_item, detail in keranjang[user_id].items():
        jumlah = Decimal(str(detail["jumlah"]))
        harga = Decimal(str(detail["harga"]))
        diskon = Decimal(str(detail["diskon"]))
        subtotal = jumlah * (harga - diskon)
        total += subtotal
        pesan += f"- {produk_item}: {format_rupiah(jumlah)} x {format_rupiah(harga - diskon)} = {format_rupiah(subtotal)}\n"


    pesan += f"\n💰 **Total: {format_rupiah(total)}**"


    # ✅ **Tambahkan tombol Edit Keranjang sebelum Checkout**
    keyboard = [
        [InlineKeyboardButton("🛒 Edit Keranjang", callback_data="edit_keranjang")],
        [InlineKeyboardButton("✅ Checkout", callback_data="checkout")],
    ]
    reply_markup = InlineKeyboardMarkup(keyboard)

    await update.message.reply_text(pesan, reply_markup=reply_markup)

# Fungsi untuk Beli Cepat Produk
async def beli_cepat(update: Update, context: ContextTypes.DEFAULT_TYPE):
    global produk
    query = update.callback_query
    await query.answer()
    
    try:
        _, kategori, nama_produk, jumlah = query.data.split("_")
        jumlah = int(jumlah)
    except ValueError:
        await query.message.reply_text("⚠️ Format data tidak valid. Coba lagi.")
        return

    nama_produk = nama_produk.lower()  # Pastikan nama produk selalu lowercase

    if kategori not in produk or nama_produk not in produk[kategori]:
        await query.message.reply_text("⚠️ Produk tidak ditemukan atau sudah dihapus.")
        return

    data = produk[kategori][nama_produk]  # Ambil data produk dari kategori

    if data["stok"] < jumlah:
        await query.message.reply_text(f"⚠️ Stok {nama_produk} tidak mencukupi! Sisa stok: {data['stok']} pcs")
        return

        print(f"[DEBUG] Produk Ditambahkan ke Keranjang: {keranjang[user_id]}")

    # Simpan ke keranjang pengguna
    user_id = update.effective_user.id
    if user_id not in keranjang:
        keranjang[user_id] = {}

    if nama_produk not in keranjang[user_id]:
        keranjang[user_id][nama_produk] = {
            "jumlah": 0,
            "harga": data["harga"],
            "diskon": data["diskon"]
        }

    keranjang[user_id][nama_produk]["jumlah"] += jumlah
    produk[kategori][nama_produk]["stok"] -= jumlah  # Update stok

    await query.message.reply_text(f"✅ {jumlah} {nama_produk} berhasil ditambahkan ke keranjang! Klik /keranjang untuk melihat belanjaan Anda.")



async def tampilkan_poin(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Menampilkan jumlah poin pelanggan setelah memilih voucher."""
    query = update.callback_query  # Gunakan query dari callback
    await query.answer()  # Hindari error callback query tidak dijawab

    user_id = update.effective_user.id
    total_poin = context.user_data.get("total_poin", 0)

    if total_poin == 0:
        pesan_poin = "🔷 Anda belum memiliki poin. Poin akan dikumpulkan setelah transaksi ini."
    elif total_poin < 10000:
        pesan_poin = f"📑 Poin Anda: **{total_poin}**\n❌ Penukaran poin minimal 10.000."
    else:
        pesan_poin = f"📑 Poin Anda: **{total_poin}**\n✅ Masukkan jumlah poin yang ingin ditukarkan."

    # ✅ Gunakan query.message.reply_text() untuk callback query
    await query.message.reply_text(f"{pesan_poin}\n\n💳 Masukkan Poin yang ingin digunakan (jika ada, jika tidak ketik angka 0):")

    context.user_data["checkout_step"] = "poin"



# Fungsi untuk Mengecek Data Pelanggan
async def cek_data_pelanggan(user_id):
    """
    Mengecek apakah user ID sudah pernah berbelanja dan mengambil data mereka dari Database PostgreSQL.
    """
    try:
        row = await get_pelanggan_from_db(user_id)
        if not row:
            return None

        produk_terakhir = row.get("produk_terakhir", {})

        if DEBUG_MODE:  # ✅ Debugging Aktif
            print(f"✅ [DEBUG] Data pelanggan ditemukan: {row}")

        return {
            "nama": row["nama"],
            "alamat": row["alamat"],
            "hp": row["hp"],
            "detail_alamat": row.get("detail_alamat", "-"),
            "total_poin": row.get("total_poin", 0),
            "produk_terakhir": produk_terakhir
        }

    except Exception as e:
        print(f"❌ [ERROR] Gagal membaca data pelanggan dari DB: {e}")
    
    return None


# ✅ **Optimasi JSON Parsing pada repeat_order**
async def repeat_order(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Menambahkan kembali produk terakhir ke keranjang tanpa duplikasi, dengan pengecekan toko tutup."""

    # ✅ Blokir pencarian produk jika sudah buat invoice
    if context.user_data.get("checkout_step") == "invoice_dikirim":
        await update.message.reply_text(
            "⚠️ Kamu sedang dalam proses checkout.\n"
            "Selesaikan pesanan, atau buat pesanan baru jika ingin mulai ulang.",
            reply_markup=InlineKeyboardMarkup([
                [InlineKeyboardButton("❌ Batalkan Pesanan", callback_data="batal_pesanan")]
            ])
        )
        return

    user_id = update.effective_user.id
    now = datetime.now(JAKARTA_TZ)

    # ✅ **Cek apakah toko sedang tutup sebelum memproses repeat order**
    if is_holiday(now):
        holiday_dates = get_holiday_dates()
        await update.message.reply_text(f"🚨 Toko sedang libur dari {holiday_dates}. Anda tidak bisa melakukan pemesanan saat ini.")
        return

    """Menambahkan kembali produk terakhir ke keranjang tanpa duplikasi."""
    user_id = update.effective_user.id
    data_pelanggan = await get_pelanggan_from_db(user_id)

    if not data_pelanggan or not data_pelanggan.get("produk_terakhir"):
        await update.message.reply_text("⚠️ Anda belum memiliki pesanan sebelumnya.")
        return

    try:
        produk_terakhir_raw = data_pelanggan.get("produk_terakhir", "{}")
        produk_terakhir = orjson.loads(produk_terakhir_raw) if isinstance(produk_terakhir_raw, str) else produk_terakhir_raw

        print(f"📥 [DEBUG] Produk Terakhir ditemukan:\n{produk_terakhir}")

    except orjson.JSONDecodeError:
        print(f"❌ [ERROR] Gagal membaca Produk Terakhir untuk user {user_id}. Format JSON tidak valid.")
        await update.message.reply_text("⚠️ Data pesanan sebelumnya rusak atau tidak bisa diproses.")
        return

    if not isinstance(produk_terakhir, dict) or not produk_terakhir:
        print(f"⚠️ [DEBUG] Data Produk Terakhir bukan dictionary atau kosong: {produk_terakhir}")
        await update.message.reply_text("⚠️ Tidak ada produk sebelumnya yang bisa dipesan ulang.")
        return

    # ✅ **Hindari duplikasi produk** dengan mengecek apakah user sudah punya keranjang aktif
    if user_id not in keranjang:
        keranjang[user_id] = produk_terakhir  # Jika belum ada keranjang, langsung tambahkan

    else:
        # **Cek apakah produk sudah ada di keranjang, lalu update jumlahnya**
        for produk, data in produk_terakhir.items():
            if not isinstance(data, dict):
                print(f"⚠️ [DEBUG] Format produk tidak sesuai: {produk}: {data}")
                continue
            
            if produk in keranjang[user_id]:  # Produk sudah ada, update jumlah
                print(f"🔄 [DEBUG] Menambahkan jumlah produk: {produk} +{data.get('jumlah', 1)}")
                keranjang[user_id][produk]["jumlah"] = max(
                    keranjang[user_id][produk]["jumlah"], data.get("jumlah", 1)
                )  # Hindari penambahan ganda
            
            else:  # Produk baru, tambahkan ke keranjang
                keranjang[user_id][produk] = {
                    "jumlah": data.get("jumlah", 1),
                    "harga": data.get("harga", 0),
                    "diskon": data.get("diskon", 0),
                }

    await asyncio.sleep(0.1)
    await update.message.reply_text("🔄 Produk dari pesanan terakhir telah ditambahkan ke keranjang!\nKetik /keranjang untuk melihat belanjaan Anda.")







async def edit_keranjang(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Menampilkan isi keranjang dengan opsi edit."""
    query = update.callback_query
    await query.answer()

    user_id = update.effective_user.id
    if user_id not in keranjang or not keranjang[user_id]:
        await update.message.reply_text("⚠️ Keranjang Anda kosong! Tambahkan produk terlebih dahulu.")
        return

    pesan = "🛍 **Keranjang Anda:**\n"
    keyboard = []

    for produk, detail in keranjang[user_id].items():
        subtotal = detail["jumlah"] * (detail["harga"] - detail["diskon"])
        pesan += f"- {produk}: {detail['jumlah']} x Rp {detail['harga']-detail['diskon']:,} = Rp {subtotal:,}\n"

        # Tambahkan tombol edit produk
        keyboard.append([
            InlineKeyboardButton(f"➖ Kurangi {produk}", callback_data=f"kurang_{produk}"),
            InlineKeyboardButton(f"➕ Tambah {produk}", callback_data=f"tambah_{produk}"),
        ])
        keyboard.append([InlineKeyboardButton(f"❌ Hapus {produk}", callback_data=f"hapus_{produk}")])

    pesan += "\n⚙️ Gunakan tombol di bawah untuk edit pesanan:"
    keyboard.append([InlineKeyboardButton("✅ Selesai Edit", callback_data="proses_checkout")])
    reply_markup = InlineKeyboardMarkup(keyboard)

    await query.message.edit_text(pesan, reply_markup=reply_markup)

async def handle_edit_keranjang(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Menangani perubahan jumlah produk atau penghapusan di keranjang."""
    query = update.callback_query
    await query.answer()

    user_id = update.effective_user.id
    action, produk = query.data.split("_", 1)

    if user_id not in keranjang or not keranjang[user_id]:
        await query.message.edit_text("⚠️ Keranjang Anda kosong! Tambahkan produk sebelum checkout.")
        return

    if action == "tambah":
        keranjang[user_id][produk]["jumlah"] += 1
    elif action == "kurang":
        if keranjang[user_id][produk]["jumlah"] > 1:
            keranjang[user_id][produk]["jumlah"] -= 1
        else:
            del keranjang[user_id][produk]  # Hapus jika jumlahnya tinggal 1
    elif action == "hapus":
        del keranjang[user_id][produk]

    # Jika tombol "Selesai Edit" ditekan, langsung masuk ke checkout
    if action == "selesai_edit":
        await checkout(update, context)  # Langsung panggil fungsi checkout
        return

    # Jika masih ada item di keranjang, tetap tampilkan opsi edit
    await edit_keranjang(update, context)


# Fungsi untuk Checkout
async def checkout(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Menangani checkout, baik dari perintah maupun tombol checkout."""
    # Cek apakah ini dari perintah atau callback query
    if update.callback_query:
        query = update.callback_query
        user_id = query.from_user.id
        await query.answer()  # Hindari error karena callback tidak direspons
    else:
        user_id = update.effective_user.id

    # Cek apakah pelanggan sudah pernah berbelanja
    data_pelanggan = await get_pelanggan_from_db(user_id)

    if data_pelanggan:
        context.user_data["nama"] = data_pelanggan["nama"]
        context.user_data["alamat"] = data_pelanggan["alamat"]
        context.user_data["hp"] = data_pelanggan["hp"]
        context.user_data["detail_alamat"] = data_pelanggan.get("detail_alamat", "TIDAK DIISI")

        keyboard = [
            [InlineKeyboardButton("✅ Gunakan Alamat ini", callback_data="confirm_old_data")],
            [InlineKeyboardButton("✏️ Ubah Data", callback_data="edit_data")]
        ]
        reply_markup = InlineKeyboardMarkup(keyboard)

        pesan = (f"🔄 **Data Anda yang tersimpan:**\n"
                 f"👤 Nama: {context.user_data['nama']}\n"
                 f"📍 Alamat: {context.user_data['alamat']}\n"
                 f"📌 Detail: {context.user_data['detail_alamat']}\n"
                 f"📱 No. HP: {context.user_data['hp']}\n\n"
                 "Silakan pilih opsi di bawah ini:")

        if update.callback_query:
            await query.message.edit_text(pesan, reply_markup=reply_markup)
        else:
            await update.message.reply_text(pesan, reply_markup=reply_markup)
    else:
        context.user_data["checkout_active"] = True
        context.user_data["checkout_step"] = "nama"
        if update.callback_query:
            await query.message.edit_text("Silakan isi data berikut:\n📌 Nama:")
        else:
            await update.message.reply_text("Silakan isi data berikut:\n📌 Nama:")



# Fungsi untuk Menangani Input Pengguna
async def handle_user_input(update: Update, context: ContextTypes.DEFAULT_TYPE):
    text = update.message.text.strip()
    checkout_step = context.user_data.get("checkout_step")

    print(f"🧪 [DEBUG] handle_user_input() | Input: '{text}' | Checkout Step: {checkout_step}")

    # ✅ Reset flag pencarian setelah checkout selesai
    if context.user_data.get("checkout_done"):
        print("🔄 [DEBUG] checkout_done = True, reset flag...")
        context.user_data.pop("checkout_done", None)

    text_lower = text.lower()

    # 🧭 Perintah eksplisit tetap diizinkan
    if text_lower in ["produk", "/produk"]:
        await menu_produk(update, context)
        return

    if text_lower in ["kategori", "/kategori"]:
        await kategori(update, context)
        return

    # ✅ Blokir pencarian produk jika sudah buat invoice
    if context.user_data.get("checkout_step") == "invoice_dikirim":
        await update.message.reply_text(
            "⚠️ Kamu sedang dalam proses checkout.\n"
            "Selesaikan pesanan, atau buat pesanan baru jika ingin mulai ulang.",
            reply_markup=InlineKeyboardMarkup([
                [InlineKeyboardButton("❌ Batalkan Pesanan", callback_data="batal_pesanan")]
            ])
        )
        return

    # 🛑 Jika masih dalam proses checkout selain 'done', arahkan ke handler checkout
    if checkout_step and checkout_step not in ("done", None):
        print(f"🛑 [DEBUG] Input diarahkan ke handle_checkout_input karena masih di step: {checkout_step}")
        await handle_checkout_input(update, context)
        return

    # 🔍 Pencarian manual (input berupa produk)
    if text_lower.isalpha():
        print("🔎 [DEBUG] Input dianggap pencarian produk...")
        await cari_produk(update, context)
        return

    # 🌀 Fallback jika input tidak dikenali
    await update.message.reply_text("⚠️ Perintah tidak dikenali. Gunakan /start untuk memulai.")






async def handle_delivery_selection(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Menangani pemilihan waktu pengiriman dari callback query."""
    query = update.callback_query
    user_id = query.from_user.id

    # 🚨 **Cek apakah checkout sudah selesai**
    if context.user_data.get("checkout_step") == "done":
        await query.answer("✅ Checkout sudah selesai. Anda tidak bisa mengubah waktu pengiriman lagi.", show_alert=True)
        return

    # ✅ Jika checkout belum selesai, lanjutkan proses pemilihan jam
    selected_time = query.data.replace("delivery_", "").strip()
    context.user_data["waktu_pengiriman"] = selected_time

    await query.answer()  # Hindari error callback query tidak dijawab
    selected_time = query.data.replace("delivery_", "").strip()  # Ambil waktu pengiriman dari callback data

    context.user_data["waktu_pengiriman"] = selected_time  # Simpan pilihan user

    # 🔄 **Tambahkan Debugging**
    print(f"✅ [DEBUG] User {user_id} memilih waktu pengiriman: {selected_time}")

    await query.message.reply_text(f"🚚 Anda memilih pengiriman pada: {selected_time}")

    # 🔄 **Langsung panggil `process_checkout()` untuk menyelesaikan checkout**
    await process_checkout(user_id, context)

    


def hitung_diskon(kode_voucher, total_belanja):
    """Menghitung diskon berdasarkan kode voucher yang digunakan."""
    if kode_voucher not in vouchers:
        return 0  # Jika voucher tidak ditemukan, diskon 0

    voucher = vouchers[kode_voucher]

    if total_belanja < voucher["min_belanja"]:
        return 0  # Tidak memenuhi syarat minimum belanja

    return voucher["nilai"]  # Kembalikan nilai diskon


async def process_checkout(user_id, context):
    # 🚫 Cegah proses ulang jika status sudah 'done'
    if context.user_data.get("checkout_step") == "done":
        print(f"⚠️ [SKIP] process_checkout dilewati untuk user {user_id}, karena sudah selesai.")
        return

    # 🚫 Cegah jika user masih punya invoice aktif
    if "invoice_id" in context.user_data:
        from xendit import cek_status_invoice_xendit
        status_invoice = cek_status_invoice_xendit(context.user_data["invoice_id"])

        if status_invoice in ["PENDING"]:
            await context.bot.send_message(
                chat_id=user_id,
                text=(
                    "⚠️ Anda masih memiliki invoice yang belum dibayar (Status: PENDING).\n"
                    "Silakan selesaikan pembayaran atau batalkan pesanan terlebih dahulu sebelum membuat pesanan baru.\n\n"
                    "Gunakan tombol berikut:"
                ),
                reply_markup=InlineKeyboardMarkup([
                    [InlineKeyboardButton("❌ Batalkan Pesanan", switch_inline_query_current_chat="/batal")]
                ])

            )
            return


    # 🔎 **Cek apakah user_id ada dalam keranjang**
    if user_id not in keranjang or not keranjang[user_id]:
        await context.bot.send_message(user_id, "❌ Keranjang Anda kosong! Silakan tambahkan produk terlebih dahulu.")
        print(f"❌ [ERROR] Keranjang tidak ditemukan untuk user {user_id}.")
        return  # **Hentikan proses checkout jika keranjang kosong**

    # ✅ SIMPAN KERANJANG USER KE FILE agar bisa dipakai webhook nanti
    with open(f"keranjang_{user_id}.json", "w") as f:
        json.dump(keranjang.get(user_id, {}), f, cls=DecimalEncoder)

    # ✅ Hitung total belanja
    total_belanja = sum(
        (Decimal(str(item["harga"])) - Decimal(str(item["diskon"]))) * Decimal(str(item["jumlah"]))
        for item in keranjang[user_id].values()
    )

    # ✅ Ambil diskon voucher dan ongkir dengan aman
    diskon_voucher = Decimal(str(context.user_data.get("diskon_voucher", 0)))
    total_ongkir = max(Decimal("0"), Decimal(str(context.user_data.get("ongkir", 0))) - diskon_voucher)

    # ✅ Pastikan `poin_digunakan` tidak lebih besar dari total pembayaran
    poin_digunakan = min(Decimal(str(context.user_data.get("poin", 0))), total_belanja + total_ongkir - diskon_voucher)

    # ✅ Hitung total pembayaran dengan batas minimum 0
    total_pembayaran = max(Decimal("0"), total_belanja + total_ongkir - poin_digunakan - diskon_voucher)


    # 📝 Debug log untuk melihat perhitungan sebelum error
    print(f"🔎 [DEBUG] Total Belanja: Rp {total_belanja:,}")
    print(f"🔎 [DEBUG] Ongkir: Rp {total_ongkir:,}")
    print(f"🔎 [DEBUG] Poin Digunakan: Rp {poin_digunakan:,}")
    print(f"🔎 [DEBUG] Diskon Voucher: Rp {diskon_voucher:,}")
    print(f"🔎 [DEBUG] Total Pembayaran: Rp {total_pembayaran:,}")

    # 🚨 **Jika total pembayaran Rp 0, langsung proses pesanan tanpa pembayaran**
    if total_pembayaran == 0:
        await context.bot.send_message(
            user_id, 
            "✅ Poin Anda mencukupi! Pesanan diproses tanpa pembayaran. 🚀\n\n"
            "Admin akan segera menyiapkan pesanan Anda!"
        )
        update_status_pesanan(user_id, "Diproses")  # ✅ Tandai pesanan langsung diproses
        print(f"✅ [INFO] Pesanan user {user_id} diproses tanpa pembayaran.")
        return  # 🚀 **Hentikan proses, tidak perlu membuat invoice**

    context.user_data["total_pembayaran"] = total_pembayaran
    print(f"💰 [DEBUG] Total pembayaran dihitung: {total_pembayaran}")

    # ✅ Pastikan user sudah memilih metode pembayaran
    if "metode_pembayaran" not in context.user_data:
        await context.bot.send_message(
            user_id,
            "💳 Silakan pilih metode pembayaran terlebih dahulu:",
            reply_markup=InlineKeyboardMarkup([
                [InlineKeyboardButton("🏦 Transfer Bank", callback_data="pay_va")],
                [InlineKeyboardButton("💳 Kartu Kredit", callback_data="pay_cc")],
                [InlineKeyboardButton("📲 E-Wallet", callback_data="pay_ewallet")],
                [InlineKeyboardButton("📸 QRIS", callback_data="pay_qris")]
            ])
        )
        return  # 🚫 Jangan lanjutkan proses sampai user pilih metode


    # 🚀 Jika masih ada pembayaran, buat invoice
    print(f"📡 [DEBUG] Mengirim total_pembayaran ke buat_invoice: Rp {total_pembayaran:,}")

    # Tampilkan metode pembayaran HANYA jika belum dibayar
    if "invoice_id" not in context.user_data:
        await tampilkan_metode_pembayaran(context.bot, user_id)
        return  # Jangan lanjut jika belum pilih metode
    else:
        print(f"✅ [DEBUG] Pembayaran sudah dibuat untuk user {user_id}, skip metode.")


    # ✅ Invoice ID tersedia, lanjutkan cek status pembayaran
    invoice_id = context.user_data.get("invoice_id")
    if not invoice_id:
        await context.bot.send_message(user_id, "❌ Pembayaran belum diproses. Silakan pilih metode pembayaran terlebih dahulu.")
        return

    status = cek_status_invoice_xendit(invoice_id)


    # ✅ **Hitung poin dari transaksi ini**
    poin_didapat = hitung_poin(total_belanja)
    context.user_data["poin_didapat"] = poin_didapat

    # Ambil total poin sebelumnya
    total_poin_sebelumnya = await get_poin_pelanggan(user_id)

    # Ambil poin yang digunakan dalam transaksi ini
    poin_digunakan = context.user_data.get("poin", 0)

    # Hitung poin baru setelah transaksi
    poin_didapat = hitung_poin(total_belanja)  # Poin yang diperoleh dari belanja
    total_poin_baru = max(0, total_poin_sebelumnya + poin_didapat - poin_digunakan)

    # 🔥 **Tampilkan poin yang diperoleh setelah checkout**
    await context.bot.send_message(user_id, 
        f"🎉 Anda mendapatkan **{poin_didapat} poin** dari transaksi ini!\n"
        f"💰 Total poin Anda sekarang: **{total_poin_baru}**\n\n"
        f"🛍️ Poin bisa digunakan untuk transaksi selanjutnya!"
    )

    try:
        # ✅ Simpan produk terakhir dalam format JSON
        produk_terakhir = keranjang.get(user_id, {})
        produk_terakhir_str = json.dumps(produk_terakhir, ensure_ascii=False, cls=DecimalEncoder) if produk_terakhir else "{}"

        # 🔄 **Simpan Data Pelanggan ke Google Sheets (Pastikan Semua Data Tersimpan)**
        await simpan_data_pelanggan(
            user_id,
            context.user_data["nama"],
            context.user_data["alamat"],
            context.user_data["hp"],
            context.user_data.get("detail_alamat", "-"),
            keranjang.get(user_id, {}),
            total_poin_baru  # Pastikan yang disimpan adalah total setelah pengurangan poin yang digunakan
        )

        print("➡️ [TRACE] Memanggil simpan_pelanggan_ke_postgres()...")
        # 2. Simpan ke PostgreSQL
        await simpan_pelanggan_ke_postgres(
            user_id,
            context.user_data['nama'],
            context.user_data['alamat'],
            context.user_data['hp'],
            context.user_data.get('detail_alamat', '-'),
            produk_terakhir_str,
            total_poin_baru
        )

        # ✅ Logging sukses
        print(f"✔️ Pelanggan {user_id} berhasil disimpan ke Sheet & PostgreSQL")

    except Exception as e:
        await context.bot.send_message(user_id, "❌ Terjadi kesalahan saat menyimpan data pelanggan. Coba lagi nanti.")
        print(f"❌ [ERROR] Gagal menyimpan data pelanggan: {e}")
        return  # Hentikan proses lebih lanjut jika gagal menyimpan data

    # Perbarui status pesanan menjadi "Diproses" setelah data pelanggan disimpan
    try:
        update_status_pesanan(user_id, "Diproses")  # Update status pesanan menjadi 'Diproses'
    except Exception as e:
        await context.bot.send_message(user_id, "❌ Terjadi kesalahan saat memperbarui status pesanan.")
        print(f"❌ [ERROR] Gagal memperbarui status pesanan: {e}")
        return  # Hentikan proses lebih lanjut jika gagal memperbarui status

    await context.bot.send_message(user_id, "🧾 **Membuat Invoice...**")

    print(f"[DEBUG] Produk dalam keranjang sebelum membuat invoice: {keranjang.get(user_id, {})}")

    # **Panggil fungsi generate_invoice() langsung**
    jam_pesan = datetime.now().hour # Ambil jam pemesanan saat ini
    pdf_invoice = generate_invoice(user_id, context.user_data, keranjang, jam_pesan)

    # **Cek apakah invoice berhasil dibuat**
    if not pdf_invoice or not os.path.exists(pdf_invoice):
        await context.bot.send_message(user_id, "❌ Terjadi kesalahan saat membuat invoice. Coba lagi nanti.")
        return

    # **Kirim Invoice PDF ke User**
    await context.bot.send_document(
        user_id, 
        document=open(pdf_invoice, "rb"), 
        filename=os.path.basename(pdf_invoice),
        caption=f"🧾 **Invoice Pembelian Anda**\n💰 Total: Rp {total_pembayaran:,}\n📦 Terima kasih telah berbelanja!"
)

    # ✅ **Tunggu Pembayaran Lunas Sebelum Memproses Pesanan**
    await context.bot.send_message(user_id, "⏳ Menunggu pembayaran...")

    for _ in range(10):  # Coba cek 10 kali dalam 30 detik
        status = cek_status_invoice_xendit(invoice_id)

        
        if status == "PAID":
            await context.bot.send_message(user_id, "✅ Pembayaran berhasil! Pesanan Anda sedang diproses. 🚀")

            # ✅ Buat dan kirim invoice setelah pembayaran lunas
            jam_pesan = datetime.now().hour
            pdf_invoice = generate_invoice(user_id, context.user_data, keranjang, jam_pesan)

            if not pdf_invoice or not os.path.exists(pdf_invoice):
                await context.bot.send_message(user_id, "❌ Gagal membuat invoice setelah pembayaran.")
                return

            await context.bot.send_document(
                user_id,
                document=open(pdf_invoice, "rb"),
                filename=os.path.basename(pdf_invoice),
                caption=f"🧾 **Invoice Pembelian Anda**\n💰 Total: Rp {total_pembayaran:,}\n📦 Terima kasih telah berbelanja!"
            )

            # ✅ Reset checkout state
            reset_checkout(context.user_data)

            with open(f"user_data_{user_id}.json", "w") as f:
                json.dump(context.user_data, f, cls=DecimalEncoder)

            print(f"🧹 Checkout state dibersihkan untuk user: {user_id}")

        
            break

        elif status == "PENDING":
            await asyncio.sleep(30)  # Tunggu 30 detik sebelum cek lagi
        else:
            await context.bot.send_message(user_id, "❌ Gagal mengecek status pembayaran. Silakan coba lagi nanti.")
            return

    # ✅ Tandai bahwa proses checkout selesai
    context.user_data["checkout_step"] = "done"
    context.user_data["checkout_done"] = True
    print("✅ [DEBUG] Checkout selesai, user bisa mencari produk kembali.")


    # ✅ **Jika Lunas, Kirim Konfirmasi ke Admin**
    await context.bot.send_message(
        ADMIN_USER_ID,
        f"✅ **Pembayaran Lunas!**\n"
        f"📌 Pesanan dari {context.user_data['nama']} telah dibayar."
    )

    # ✅ **Kirim Invoice ke Admin**
    try:
        await context.bot.send_document(
            chat_id=ADMIN_USER_ID,
            document=open(pdf_invoice, "rb"),
            filename=os.path.basename(pdf_invoice),
            caption=f"📢 **Pesanan Baru Masuk!**\n"
                    f"🧾 **Invoice No: {context.user_data.get('invoice_no')}**\n"
                    f"👤 Nama: {context.user_data.get('nama')}\n"
                    f"📍 Alamat: {context.user_data.get('alamat')}\n"
                    f"📌 Detail: {context.user_data.get('detail_alamat', '-')}\n"
                    f"📱 No. HP: {context.user_data.get('hp')}\n"
                    f"🚚 Pengiriman: {context.user_data.get('waktu_pengiriman')}\n"
                    f"🚚 Ongkos Kirim: Rp {context.user_data.get('ongkir'):,}\n"
                    f"💰 Total Pembayaran: Rp {total_pembayaran:,}\n"
                    f"📦 Produk:\n" + "\n".join([f"- {p} x{d['jumlah']} @ Rp {d['harga']-d['diskon']:,}" for p, d in keranjang[user_id].items()])
        )
        print(f"✅ [DEBUG] Invoice berhasil dikirim ke Admin: {ADMIN_USER_ID}")
    except Exception as e:
        print(f"❌ [ERROR] Gagal mengirim invoice ke admin: {e}")

    # ✅ **Panggil fungsi untuk menampilkan tombol "Kirim Barang" ke admin setelah invoice dikirim**
    await tampilkan_tombol_kirim_barang(context, user_id)


    # ✅ Simpan produk terakhir dalam format JSON sebelum disimpan ke Google Sheets
    produk_terakhir = keranjang.get(user_id, {})
    produk_terakhir_str = json.dumps(produk_terakhir, ensure_ascii=False, cls=DecimalEncoder) if produk_terakhir else "{}"

    # ✅ Simpan data pelanggan ke Google Sheets dengan benar
    simpan_data_pelanggan(
        user_id,
        context.user_data['nama'],
        context.user_data['alamat'],
        context.user_data['hp'],
        context.user_data.get('detail_alamat', '-'),
        produk_terakhir_str,  # 💥 PENTING: kirim string JSON, bukan dict
        total_poin_baru
    )

    print(f"✅ [DEBUG] Data yang dikirim ke Google Sheets:")
    print(f"Nama: {context.user_data['nama']}")
    print(f"Alamat: {context.user_data['alamat']}")
    print(f"Detail Alamat: {context.user_data['detail_alamat']}")
    print(f"HP: {context.user_data['hp']}")

    # 🔄 Simpan pesanan ke Sheet Pesanan
    worksheet.append_row([
        user_id, 
        context.user_data.get("nama", "-"),  # Hindari KeyError jika data kosong
        context.user_data.get("alamat", "-"),  
        context.user_data.get("hp", "-"),
        context.user_data.get("voucher", "-"),  # ✅ Hindari KeyError jika voucher tidak ada
        context.user_data.get("diskon_voucher", 0),  # ✅ Set diskon default ke 0 jika tidak ada voucher
        context.user_data.get("poin", 0),  # ✅ Set poin default ke 0 jika tidak ada poin yang digunakan
        total_belanja, 
        total_ongkir, 
        total_pembayaran, 
        str(datetime.now())  # Simpan waktu transaksi
    ])


    print(f"✅ [DEBUG] Pesanan {user_id} berhasil dicatat di Sheet Pesanan.")

    # **Hapus status checkout agar pencarian produk bisa berjalan lagi**
    context.user_data["checkout_step"] = "done"
    context.user_data.pop("invoice_id", None)  # ✅ Hapus invoice_id setelah transaksi sukses
    print("✅ [DEBUG] Checkout selesai, user bisa mencari produk kembali.")

 
  
    keranjang[user_id] = {}  # Kosongkan keranjang setelah transaksi selesai

    await asyncio.sleep(1)  # **Tambahkan sedikit delay agar handler lain tidak menangkap input terakhir**
    return  # **Pastikan fungsi berhenti di sini**


# Fungsi untuk Menangani Input Checkout
async def handle_checkout_input(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Menangani input teks pengguna dalam proses checkout."""
    
    valid_checkout_steps = ["nama", "alamat", "detail_alamat", "hp", "poin", "waktu_pengiriman"]

    if context.user_data.get("checkout_step") not in valid_checkout_steps:
        print("⚠️ [DEBUG] Tidak dalam sesi checkout atau step tidak valid.")
        return


    user_id = update.effective_user.id
    step = context.user_data.get("checkout_step", "")
    text = update.message.text.strip()

    print(f"📌 [DEBUG] Checkout Step: {step}, Input: {text}")  # Debugging

    if not step:
        await update.message.reply_text("❌ Terjadi kesalahan. Mulai ulang dengan /checkout")
        return

    # ✅ 1. Input Nama
    if step == "nama":
        context.user_data["nama"] = text
        context.user_data["checkout_step"] = "alamat"
        await update.message.reply_text("📍 Masukkan Alamat Anda (mis. Jalan Setia Budi no.99 Medan - Wajib pakai nama kota):")
        return

    # ✅ 2. Input Alamat Utama (Menggunakan Google Maps)
    elif step == "alamat":
        context.user_data["alamat_raw"] = text  # Simpan alamat mentah dari user
        alamat_tujuan = gmaps.geocode(text)

        if not alamat_tujuan:
            await update.message.reply_text("❌ Alamat tidak valid. Coba masukkan kembali.")
            return

        # Gunakan formatted address dari Google Maps
        alamat_bersih = alamat_tujuan[0]['formatted_address']
        context.user_data["alamat"] = alamat_bersih  

        hasil_jarak = gmaps.distance_matrix(LOKASI_TOKO, text, mode="driving")
        jarak_km = hasil_jarak["rows"][0]["elements"][0]["distance"]["value"] / 1000
        ongkir = int(jarak_km * ONGKOS_PER_KM)

        context.user_data["jarak_km"] = jarak_km
        context.user_data["ongkir"] = ongkir  

        await update.message.reply_text(
            f"📍 Alamat berdasarkan Google Maps: **{alamat_bersih}**\n"
            f"🚚 Perkiraan jarak: {jarak_km:.2f} km\n"
            f"💰 Ongkos kirim: Rp {ongkir:,}\n\n"
            "🏠 Silakan tambahkan alamat detail Anda (contoh: rumah no.10 warna orange, dekat minimarket, lantai 2, dll.):"
        )
        context.user_data["checkout_step"] = "detail_alamat"
        return

    # ✅ 3. Input Alamat Detail
    elif step == "detail_alamat":
        context.user_data["detail_alamat"] = text.strip() or "TIDAK DIISI"

        await update.message.reply_text(
            f"🏡 **Alamat Utama:** {context.user_data['alamat']}\n"
            f"📌 **Detail Alamat:** {context.user_data['detail_alamat']}\n"
            f"🚚 **Jarak:** {context.user_data['jarak_km']:.2f} km\n"
            f"💰 **Ongkos Kirim:** Rp {context.user_data['ongkir']:,}\n\n"
            "📱 Masukkan Nomor HP Anda:"
        )
        context.user_data["checkout_step"] = "hp"
        return  


    # ✅ **4. Input Nomor HP**
    if step == "hp":
        context.user_data["hp"] = text
        await update.message.reply_text("✅ Nomor HP telah disimpan. Silakan lanjut ke langkah berikutnya.")

        # ✅ **Cek apakah pengguna sudah menggunakan voucher**
        if "voucher" in context.user_data:
            await update.message.reply_text("⚠️ Anda sudah menggunakan voucher. Lanjut ke langkah berikutnya.")
            context.user_data["checkout_step"] = "poin"
            await handle_checkout_input(update, context)  # **Langsung lanjut ke poin**
            return

        # ✅ **Tampilkan pilihan voucher**
        await tampilkan_voucher(update, context)
        return

    elif step == "poin":
        total_poin = context.user_data.get("total_poin", 0)

        if not text.isdigit():
            await update.message.reply_text("❌ Masukkan angka yang valid (contoh: 0, 10000, 20000). Coba lagi:")
            return

        poin_digunakan = int(text)

        if poin_digunakan > total_poin:
            await update.message.reply_text(f"❌ Anda hanya memiliki {total_poin} poin. Masukkan jumlah yang sesuai.")
            return

        if 0 < poin_digunakan < 10000:
            await update.message.reply_text("❌ Penukaran poin minimal 10.000. Masukkan jumlah yang valid atau ketik angka 0 jika tidak ingin menggunakan poin.")
            return

        context.user_data["poin"] = poin_digunakan
        print(f"✅ [DEBUG] Poin diterima: {context.user_data['poin']}")

        # ✅ **Pindah ke tahap pemilihan waktu pengiriman**
        context.user_data["checkout_step"] = "waktu_pengiriman"

        # ✅ **Panggil fungsi untuk menampilkan waktu pengiriman**
        await tampilkan_waktu_pengiriman(update, context)



async def tampilkan_voucher(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Menampilkan pilihan voucher yang tersedia kepada pengguna."""
    
    query = update.callback_query  # Untuk callback query
    user_id = update.effective_user.id
    total_belanja = sum(
        (Decimal(str(item["harga"])) - Decimal(str(item["diskon"]))) * Decimal(str(item["jumlah"]))
        for item in keranjang[user_id].values()
    )

    keyboard = []
    for kode, voucher in vouchers.items():
        if total_belanja >= voucher["min_belanja"]:
            keyboard.append([InlineKeyboardButton(f"{kode} - Diskon Rp {voucher['nilai']:,}", callback_data=f"voucher_{kode}")])

    # ✅ Cek apakah update dari chat biasa atau inline button
    message = update.message if update.message else query.message

    if keyboard:
        reply_markup = InlineKeyboardMarkup(keyboard)
        await message.reply_text("🎟️ Pilih voucher yang tersedia:", reply_markup=reply_markup)
    else:
        # ✅ Jika tidak ada voucher, tampilkan tombol "Lanjut"
        keyboard = [[InlineKeyboardButton("Lanjut", callback_data="skip_voucher")]]
        reply_markup = InlineKeyboardMarkup(keyboard)
        await message.reply_text("❌ Tidak ada voucher yang tersedia. Klik 'Lanjut' untuk melewati.", reply_markup=reply_markup)

        # ✅ Pastikan state pindah ke poin
        context.user_data["checkout_step"] = "poin"

async def handle_voucher_selection(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Menangani pemilihan voucher oleh pengguna."""
    query = update.callback_query
    await query.answer()  # Hindari error callback query tidak dijawab

    selected_voucher = query.data.replace("voucher_", "")  # Ambil kode voucher
    context.user_data["voucher"] = selected_voucher  # Simpan voucher yang dipilih

    # Tambahkan diskon voucher ke context agar bisa digunakan di checkout
    if selected_voucher in vouchers:
        context.user_data["diskon_voucher"] = vouchers[selected_voucher]["nilai"]
        context.user_data["tipe_voucher"] = vouchers[selected_voucher]["tipe"]

    await query.message.edit_text(f"🎟️ Voucher **{selected_voucher}** telah digunakan! Diskon: Rp {context.user_data['diskon_voucher']:,}")

    # **Langsung lanjut ke poin setelah memilih voucher**
    await tampilkan_poin(update, context)  # Pindah ke poin





# Fungsi untuk Menangani Voucher
async def gunakan_voucher(update: Update, context: ContextTypes.DEFAULT_TYPE):
    query = update.callback_query
    user_id = query.from_user.id
    await query.answer()

    # Cek apakah voucher sudah digunakan sebelumnya
    if "voucher" in context.user_data:
        await query.message.edit_text("⚠️ Anda sudah menggunakan voucher. Lanjut ke langkah berikutnya.")
        return

    kode_voucher = query.data.replace("voucher_", "").upper()

    if kode_voucher not in vouchers:
        await query.message.edit_text("❌ Voucher tidak valid!")
        return

    voucher = vouchers[kode_voucher]
    total_belanja = sum(
        (Decimal(str(item["harga"])) - Decimal(str(item["diskon"]))) * Decimal(str(item["jumlah"]))
        for item in keranjang[user_id].values()
    )

    if total_belanja < voucher["min_belanja"]:
        await query.message.edit_text(f"⚠️ Minimal belanja Rp {voucher['min_belanja']:,} untuk voucher ini!")
        return

    diskon_voucher = voucher["nilai"]
    context.user_data["voucher"] = kode_voucher
    context.user_data["diskon_voucher"] = diskon_voucher
    context.user_data["checkout_step"] = "poin"

async def skip_voucher(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Menangani pengguna yang memilih untuk melewati voucher."""
    query = update.callback_query
    await query.answer()

    user_id = query.from_user.id

    # ✅ **Langsung lanjut ke tahap poin**
    context.user_data["checkout_step"] = "poin"

    # ✅ **Ambil total poin pelanggan dari Google Sheets**
    total_poin = await get_poin_pelanggan(user_id)
    context.user_data["total_poin"] = total_poin

    # ✅ **Tampilkan poin pelanggan di bot**
    if total_poin == 0:
        pesan_poin = "🔷 Anda belum memiliki poin. Poin akan dikumpulkan setelah transaksi ini."
    elif total_poin < 10000:
        pesan_poin = f"📑 Poin Anda: **{total_poin}**\n❌ Penukaran poin minimal 10.000."
    else:
        pesan_poin = f"📑 Poin Anda: **{total_poin}**\n✅ Masukkan jumlah poin yang ingin ditukarkan."

    await query.message.edit_text("📌 Anda memilih untuk melewati voucher. Lanjut ke poin.")
    await query.message.reply_text(f"{pesan_poin}\n\n💳 Masukkan Poin yang ingin digunakan (jika ada, jika tidak ketik angka 0):")


async def tampilkan_waktu_pengiriman(update: Update, context: ContextTypes.DEFAULT_TYPE):
    query = update.callback_query
    message = update.message if update.message else query.message
    
    if query:
        await query.answer()
    
    keyboard = get_delivery_times()
    reply_markup = InlineKeyboardMarkup(keyboard)
    await message.reply_text("🚚 Pilih waktu pengiriman pesanan Anda:", reply_markup=reply_markup)
    context.user_data["checkout_step"] = "waktu_pengiriman"

async def pilih_waktu_pengiriman(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Menangani pilihan waktu pengiriman saat checkout."""
    query = update.callback_query
    await query.answer()

    waktu_pengiriman = query.data.replace("delivery_", "")
    context.user_data["waktu_pengiriman"] = waktu_pengiriman

    await query.message.edit_text(f"🚚 Anda memilih pengiriman pada: {waktu_pengiriman}")


async def handle_checkout_options(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """Menangani opsi checkout (Gunakan Data Lama atau Ubah Data)."""
    query = update.callback_query
    await query.answer()

    user_id = update.effective_user.id
    data_pelanggan = await get_pelanggan_from_db(user_id)

    # **Ambil poin pelanggan sebelum checkout dimulai**
    total_poin_sebelumnya = await get_poin_pelanggan(user_id)
    print(f"✅ [DEBUG] Total poin sebelum transaksi: {total_poin_sebelumnya}")
    context.user_data["total_poin"] = total_poin_sebelumnya

    if query.data == "confirm_old_data":
        if data_pelanggan:
            context.user_data["nama"] = data_pelanggan["nama"]
            context.user_data["alamat"] = data_pelanggan["alamat"]
            context.user_data["hp"] = data_pelanggan["hp"]
            context.user_data["detail_alamat"] = data_pelanggan.get("detail_alamat", "-")

            print(f"✅ [DEBUG] Detail Alamat disimpan: {context.user_data.get('detail_alamat', '-')}")

            # ✅ **Hitung ulang ongkir berdasarkan alamat lama dengan validasi response API**
            hasil_jarak = gmaps.distance_matrix(LOKASI_TOKO, data_pelanggan["alamat"], mode="driving")

            # 🛠 Pastikan response API valid sebelum mengakses distance
            if hasil_jarak.get("rows") and len(hasil_jarak["rows"]) > 0:
                elements = hasil_jarak["rows"][0].get("elements", [{}])[0]

                if "distance" in elements:
                    jarak_km = elements["distance"]["value"] / 1000
                    ongkir = int(jarak_km * ONGKOS_PER_KM)

                    context.user_data["jarak_km"] = jarak_km
                    context.user_data["ongkir"] = ongkir  

                    print(f"✅ [DEBUG] Menggunakan alamat lama. Jarak: {jarak_km} km, Ongkir: Rp {ongkir:,}")

                else:
                    await context.bot.send_message(user_id, "❌ Gagal menghitung jarak pengiriman. Pastikan alamat Anda valid.")
                    print("❌ [ERROR] Response Google Maps tidak memiliki data 'distance'")
                    return  # 🚫 Hentikan proses jika jarak tidak bisa dihitung
            else:
                await context.bot.send_message(user_id, "❌ Google Maps tidak memberikan respons yang valid. Silakan coba lagi.")
                print("❌ [ERROR] Respons dari Google Maps kosong atau tidak valid.")
                return  # 🚫 Hentikan proses jika response kosong

            # 🔥 **Tampilkan daftar voucher langsung sebagai pilihan tombol**
            await query.message.edit_text("✅ Data lama digunakan. Melanjutkan ke checkout...")

            context.user_data["checkout_step"] = "voucher"

            # ✅ **Tambahkan debugging sebelum memanggil tampilkan_voucher()**
            print("🟢 [DEBUG] Memanggil tampilkan_voucher() setelah menggunakan data lama...")

            await tampilkan_voucher(update, context)

    elif query.data == "edit_data":
        context.user_data["checkout_step"] = "nama"
        await query.message.edit_text("✏️ Silakan masukkan data baru.")
        await query.message.reply_text("Nama:")

# ✅ Define Telegram Handlers
def register_handlers():

    bot_app.add_handler(CommandHandler("start", start))
    bot_app.add_handler(CommandHandler("produk", menu_produk))
    bot_app.add_handler(CommandHandler("kategori", kategori))
    bot_app.add_handler(CommandHandler("update_harga", update_harga))
    bot_app.add_handler(CommandHandler("tambah_voucher", tambah_voucher))
    bot_app.add_handler(CommandHandler("gunakan_voucher", gunakan_voucher))
    bot_app.add_handler(CommandHandler("reload", reload_produk)) 
    bot_app.add_handler(CommandHandler("keranjang", lihat_keranjang))
    bot_app.add_handler(CommandHandler("edit_keranjang", edit_keranjang))    
    bot_app.add_handler(CommandHandler("checkout", checkout))
    bot_app.add_handler(CommandHandler("pesan_ulang", repeat_order))
    bot_app.add_handler(CommandHandler("status_pesanan", status_pesanan))
    bot_app.add_handler(CommandHandler("beri_testimoni", beri_testimoni))
    bot_app.add_handler(CommandHandler("lihat_testimoni", lihat_testimoni))

    bot_app.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, handle_checkout_input), group=1)
    bot_app.add_handler(MessageHandler(filters.TEXT & ~filters.COMMAND, cari_produk), group=2)

    bot_app.add_handler(CommandHandler("tutup_toko", tutup_toko))
    bot_app.add_handler(CommandHandler("buka_toko", buka_toko))

    bot_app.add_handler(CallbackQueryHandler(show_kategori, pattern="^kategori_"))
    bot_app.add_handler(CallbackQueryHandler(show_produk, pattern="^produk_"))  
    bot_app.add_handler(CallbackQueryHandler(beli_cepat, pattern="^beli_"))

    bot_app.add_handler(CallbackQueryHandler(edit_keranjang, pattern="^edit_keranjang$"))  
    bot_app.add_handler(CallbackQueryHandler(handle_edit_keranjang, pattern="^(tambah|kurang|hapus)_"))

    bot_app.add_handler(CallbackQueryHandler(handle_voucher_selection, pattern="^voucher_"))
    bot_app.add_handler(CallbackQueryHandler(gunakan_voucher, pattern="^voucher_"))
    bot_app.add_handler(CallbackQueryHandler(checkout, pattern="^checkout$"))
    bot_app.add_handler(CallbackQueryHandler(checkout, pattern="^back_to_checkout$"))
    bot_app.add_handler(CallbackQueryHandler(checkout, pattern="^proses_checkout$"))

    bot_app.add_handler(CallbackQueryHandler(batal_pesanan_callback, pattern="^batal_pesanan$"))
    bot_app.add_handler(CallbackQueryHandler(handle_batal_pesanan_callback, pattern="^batal_pesanan$"))

    bot_app.add_handler(CallbackQueryHandler(handle_pilihan_pembayaran, pattern="^pay_"))
    bot_app.add_handler(CallbackQueryHandler(lambda update, ctx: update.callback_query.message.delete(), pattern="^lanjut_checkout$"))
    bot_app.add_handler(CallbackQueryHandler(handle_checkout_options, pattern="^(confirm_old_data|edit_data)$"))
    bot_app.add_handler(CallbackQueryHandler(skip_voucher, pattern="skip_voucher"))
    bot_app.add_handler(CallbackQueryHandler(skip_voucher, pattern="lanjut"))
    bot_app.add_handler(CallbackQueryHandler(handle_delivery_selection, pattern="^delivery_"))
    bot_app.add_handler(CallbackQueryHandler(kirim_barang_callback, pattern="^kirim_barang:"))

    bot_app.add_handler(MessageHandler(filters.TEXT, handle_rating))
    bot_app.add_handler(MessageHandler(filters.TEXT, handle_testimoni))

    logging.info("✅ Semua Telegram handler berhasil didaftarkan.")

# Fungsi bantu untuk reset sesi checkout
def reset_checkout(user_id, context):
    context.user_data.clear()
    keranjang.pop(user_id, None)

# Untuk /batal
async def batal_checkout(update: Update, context: ContextTypes.DEFAULT_TYPE):
    user_id = update.effective_user.id
    reset_checkout(user_id, context)
    await update.message.reply_text("✅ Pesanan dibatalkan. Anda bisa mulai kembali dengan /produk")

# Untuk tombol inline
async def batal_pesanan_callback(update: Update, context: ContextTypes.DEFAULT_TYPE):
    print("🔥 [DEBUG] Callback berhasil dipanggil!")  # Tambahkan ini

    query = update.callback_query
    await query.answer()
    
    user_id = query.from_user.id
    reset_checkout(user_id, context)
    
    await query.edit_message_text("✅ Pesanan dibatalkan. Anda bisa mulai kembali dengan /produk")

async def handle_batal_pesanan_callback(update: Update, context: ContextTypes.DEFAULT_TYPE):
    query = update.callback_query
    await query.answer()

    user_id = query.from_user.id
    reset_checkout(user_id, context)

    await query.message.reply_text("✅ Pesanan dibatalkan. Anda bisa mulai kembali dengan /produk")


# ✅ Fungsi untuk mendaftarkan handler & menjalankan bot via webhook
async def startup_all():
    register_handlers()
    await bot_app.initialize()
    register_handlers()  # ✅ Penting! Tanpa ini /start gak akan dikenali
    await bot_app.start()
    logging.info("✅ Bot Telegram siap menerima webhook!")


# ✅ Pastikan ini hanya satu kali didefinisikan
@app.on_event("startup")
async def startup_all():
    await bot_app.initialize()
    register_handlers()
    await bot_app.start()
    await set_webhook()  # ✅ Panggil DI SINI
    logging.info("✅ Bot Telegram siap menerima webhook!")


# ✅ Run Both FastAPI and Bot Together
async def main():
    """Menjalankan bot Telegram dan FastAPI secara bersamaan"""
    loop = asyncio.get_running_loop()
    task1 = loop.create_task(run_bot())  # Menjalankan bot
    task2 = loop.create_task(run_fastapi())  # Menjalankan FastAPI
    await asyncio.gather(task1, task2)

# ✅ Function to Start Fastapi
async def run_fastapi():
    """Menjalankan FastAPI secara async tanpa memblokir event loop"""
    config = uvicorn.Config(app, host="0.0.0.0", port=5000, log_level="info")
    server = uvicorn.Server(config)
    await server.serve()

if __name__ == "__main__":
    import uvicorn
    uvicorn.run("laukpauk:app", host="0.0.0.0", port=5000, reload=False)


