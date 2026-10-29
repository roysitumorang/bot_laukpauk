import os
import gspread
from telegram import Bot, Update, InlineKeyboardButton, InlineKeyboardMarkup  # ✅ Pastikan Bot diimport
from telegram.ext import ContextTypes
from testimoni import beri_testimoni  # ✅ Pastikan beri_testimoni diimport
from status_pesanan import update_status_pesanan  # ✅ Tambahkan ini!


# **Inisialisasi Variabel Environment**
TOKEN = os.getenv("BOT_TOKEN")
CREDENTIALS_PATH = os.getenv("CREDENTIALS_PATH")
SPREADSHEET_ID = os.getenv("SPREADSHEET_ID")
ADMIN_USER_ID = int(os.getenv("ADMIN_USER_ID", 5464224951))  # Default ke admin ID

# **Tambahkan pengecekan jika ada variabel kosong**
if not TOKEN:
    raise ValueError("❌ ERROR: BOT_TOKEN tidak ditemukan! Periksa file .env atau konfigurasi sistem.")
if not CREDENTIALS_PATH:
    raise ValueError("❌ ERROR: CREDENTIALS_PATH tidak ditemukan! Periksa file .env atau konfigurasi sistem.")
if not SPREADSHEET_ID:
    raise ValueError("❌ ERROR: SPREADSHEET_ID tidak ditemukan! Periksa file .env atau konfigurasi sistem.")

# **Inisialisasi Google Sheets**
gc = gspread.service_account(filename=CREDENTIALS_PATH)
sh = gc.open_by_key(SPREADSHEET_ID)

# **Inisialisasi bot**
bot = Bot(token=TOKEN)  # ✅ Harusnya sekarang tidak error lagi

print("✅ Bot berhasil diinisialisasi!")

# **Fungsi untuk menampilkan tombol 'Kirim Barang' ke admin**
async def tampilkan_tombol_kirim_barang(context: ContextTypes.DEFAULT_TYPE, user_id_pelanggan):
    keyboard = [[InlineKeyboardButton("🚚 Kirim Barang", callback_data=f"kirim_barang:{user_id_pelanggan}")]]
    reply_markup = InlineKeyboardMarkup(keyboard)

    await context.bot.send_message(
        ADMIN_USER_ID,
        f"📢 **Pesanan dari user {user_id_pelanggan} telah memiliki invoice.**\nSilakan kirim barang setelah verifikasi.",
        reply_markup=reply_markup
    )
    print(f"✅ Tombol 'Kirim Barang' dikirim ke admin untuk user {user_id_pelanggan}.")
    print(f"📤 Mengirim tombol kirim barang untuk user {user_id_pelanggan} ke ADMIN...")


# **Fungsi untuk menangani pengiriman barang oleh admin**
async def kirim_barang_callback(update: Update, context: ContextTypes.DEFAULT_TYPE):
    query = update.callback_query
    await query.answer()

    user_id = query.from_user.id

    if user_id != ADMIN_USER_ID:
        await query.edit_message_text("❌ Anda bukan admin. Tidak dapat mengirim barang.")
        return

    try:
        user_id_pelanggan = int(query.data.split(":")[1])  # Pastikan ini angka
    except (IndexError, ValueError):
        await query.edit_message_text("❌ Terjadi kesalahan dalam membaca data pelanggan.")
        return

    # **Update status pesanan menjadi 'Dikirim'**
    status_berhasil = update_status_pesanan(user_id_pelanggan, "Dikirim")

    if not status_berhasil:
        await query.edit_message_text(f"❌ ERROR: Tidak dapat memperbarui status pesanan untuk user {user_id_pelanggan}.")
        return

    # **Kirim notifikasi ke pelanggan bahwa barang sudah dikirim**
    await context.bot.send_message(user_id_pelanggan, "📦 Barang Anda telah dikirim! Terima kasih telah berbelanja di Laukpauk.")

    # **Perbarui pesan di admin**
    await query.edit_message_text(f"✅ Barang untuk user {user_id_pelanggan} telah dikirim.")

    # **Panggil beri_testimoni hanya jika `update.message` tersedia**
    if update and update.effective_user:
        print(f"✅ Memanggil beri_testimoni untuk user {user_id_pelanggan}...")
        try:
            await beri_testimoni(context=context, user_id=user_id_pelanggan)  # Panggil beri_testimoni dengan user_id
        except Exception as e:
            print(f"❌ ERROR: Gagal memanggil beri_testimoni: {e}")