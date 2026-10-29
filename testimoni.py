import os
import gspread
from dotenv import load_dotenv
from telegram import Bot, Update
from telegram.ext import ContextTypes  # ✅ Pastikan ini ada!

# ✅ Muat .env
load_dotenv()

# ✅ Ambil variabel dari .env
TOKEN = os.getenv("BOT_TOKEN")
SPREADSHEET_ID = os.getenv("SPREADSHEET_ID")
CREDENTIALS_PATH = os.getenv("CREDENTIALS_PATH")

if not TOKEN:
    raise ValueError("❌ ERROR: BOT_TOKEN tidak ditemukan! Periksa file .env atau konfigurasi sistem.")
else:
    print(f"✅ BOT_TOKEN ditemukan: {TOKEN}")

if not SPREADSHEET_ID:
    raise ValueError("❌ ERROR: SPREADSHEET_ID tidak ditemukan! Periksa file .env atau konfigurasi sistem.")
else:
    print(f"✅ SPREADSHEET_ID ditemukan: {SPREADSHEET_ID}")

if not CREDENTIALS_PATH:
    raise ValueError("❌ ERROR: CREDENTIALS_PATH tidak ditemukan! Periksa file .env atau konfigurasi sistem.")
else:
    print(f"✅ CREDENTIALS_PATH ditemukan: {CREDENTIALS_PATH}")

# Tambahkan ini di awal file
DEBUG_MODE = True  # Ubah ke False untuk pakai waktu asli

# ✅ Inisialisasi bot
bot = Bot(token=TOKEN)
print("✅ Bot berhasil diinisialisasi di testimoni.py!")

# ✅ Inisialisasi Google Sheets dengan CREDENTIALS_PATH
gc = gspread.service_account(filename=CREDENTIALS_PATH)
spreadsheet = gc.open_by_key(SPREADSHEET_ID)
print("✅ Google Sheets berhasil dibuka di testimoni.py!")


spreadsheet = gc.open_by_key(SPREADSHEET_ID)
worksheet = spreadsheet.worksheet('Pelanggan')  # Gunakan worksheet 'Pelanggan'

# 🟢 **Meminta testimoni & rating bintang**
async def beri_testimoni(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """
    Memulai sesi pemberian testimoni dengan meminta rating terlebih dahulu.
    """
    user_id = update.effective_user.id if update.effective_user else None
    if not user_id:
        print("❌ ERROR: User ID tidak ditemukan saat meminta testimoni.")
        return

    # ✅ Setel langkah ke "rating" agar angka berikutnya diproses sebagai rating
    context.user_data["rate_step"] = "rating"

    await update.message.reply_text(
        "🌟 Silakan beri rating bintang dari 1 hingga 5, lalu berikan testimoni Anda!"
    )

    print(f"✅ [DEBUG] Memulai sesi rating untuk user {user_id}")


def simpan_testimoni(user_id, rating, testimoni):
    try:
        worksheet = gc.open_by_key(SPREADSHEET_ID).worksheet("Pelanggan")  # Worksheet "Pelanggan"
        cell = worksheet.find(str(user_id))  # Cari user_id dalam kolom pertama

        if cell:
            row = cell.row  # Ambil baris tempat user_id ditemukan
            worksheet.update_cell(row, 8, rating)  # Kolom H (index ke-8)
            worksheet.update_cell(row, 9, testimoni)  # Kolom I (index ke-9)

            print(f"✅ Testimoni berhasil disimpan untuk user {user_id} | Rating: {rating}, Testimoni: {testimoni}")
        else:
            print(f"❌ ERROR: User ID {user_id} tidak ditemukan di Google Sheets!")
    except Exception as e:
        print(f"❌ ERROR: Gagal menyimpan testimoni di Google Sheets: {e}")

# 🟢 **Menerima input rating bintang**
async def handle_rating(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """
    Menangani input rating bintang dari user setelah barang dikirim.
    """
    user_id = update.effective_user.id if update.effective_user else None
    if not user_id:
        print("❌ ERROR: User ID tidak ditemukan saat menangani rating.")
        return

    # ✅ Periksa apakah input berasal dari pesan teks (bukan callback query)
    if update.message:
        rating_text = update.message.text.strip()
    else:
        print("❌ ERROR: update.message tidak tersedia, input tidak valid.")
        return

    # ✅ Cek apakah user sedang dalam sesi rating
    if context.user_data.get("rate_step") != "rating":
        print(f"✅ [DEBUG] Tidak dalam sesi rating. Input '{rating_text}' dianggap sebagai pencarian produk.")
        return  # **Hentikan fungsi agar tidak salah memproses rating**

    if not rating_text.isdigit():
        await update.message.reply_text("❌ Masukkan angka 1-5 untuk memberi rating.")
        return

    rating = int(rating_text)

    if rating < 1 or rating > 5:
        await update.message.reply_text("❌ Rating bintang hanya boleh antara 1 hingga 5.")
        return

    # ✅ Simpan rating dan lanjutkan ke sesi testimoni
    context.user_data["rating"] = rating
    context.user_data["rate_step"] = "testimoni"  # Pindah ke langkah testimoni

    await update.message.reply_text("⭐ Terima kasih atas rating Anda! Sekarang silakan berikan testimoni Anda.")

    print(f"✅ User {user_id} memberikan rating: {rating}")


# 🟢 **Menerima input testimoni**
async def handle_testimoni(update: Update, context: ContextTypes.DEFAULT_TYPE):
    """
    Menangani input testimoni dari user dan menyimpannya ke Google Sheets.
    """
    user_id = update.effective_user.id if update.effective_user else None
    if not user_id:
        print("❌ ERROR: User ID tidak ditemukan saat menyimpan testimoni.")
        return

    testimoni = update.message.text.strip()
    if not testimoni:
        await update.message.reply_text("❌ Testimoni tidak boleh kosong. Silakan coba lagi.")
        return

    # ✅ Ambil rating yang sudah disimpan
    rating = context.user_data.get("rating", None)
    if rating:
        # **Simpan ke Google Sheets**
        simpan_testimoni(user_id, rating, testimoni)

        # **Kirim konfirmasi ke pelanggan**
        await update.message.reply_text(f"🎉 Terima kasih atas testimoni dan rating {rating} bintang Anda!\n\n\"{testimoni}\"")

        # **Reset data pelanggan**
        context.user_data.clear()
    else:
        await update.message.reply_text("❌ Terjadi kesalahan. Mohon coba lagi.")


async def lihat_testimoni(update: Update, context: ContextTypes.DEFAULT_TYPE):
    try:
        worksheet = gc.open_by_key(SPREADSHEET_ID).worksheet('Pelanggan')
        testimoni_list = worksheet.get_all_records()  # Ambil semua data pelanggan

        pesan = "💬 **Testimoni Pelanggan:**\n"
        found = False  # Untuk cek apakah ada testimoni

        for row in testimoni_list:
            user_id = row.get("User ID")
            testimoni = row.get("Testimoni")  # Ambil dari kolom I (Testimoni)

            if testimoni:  # Hanya tampilkan jika ada testimoni
                found = True
                pesan += f"\n⭐ {user_id}: {testimoni}\n"

        if found:
            await update.message.reply_text(pesan)
        else:
            await update.message.reply_text("❌ Belum ada testimoni yang tersedia.")

    except Exception as e:
        print(f"❌ ERROR: Gagal mengambil testimoni: {e}")
        await update.message.reply_text("❌ Terjadi kesalahan saat mengambil testimoni.")