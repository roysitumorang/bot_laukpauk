import os
import gspread
from telegram import Update
from telegram.ext import ContextTypes
from dotenv import load_dotenv  # Memuat variabel dari file .env

# Memuat variabel dari file .env
load_dotenv()

# Ambil path ke credentials dan spreadsheet ID dari .env
CREDENTIALS_PATH = os.getenv("CREDENTIALS_PATH")
SPREADSHEET_ID = os.getenv("SPREADSHEET_ID")

# Inisialisasi Google Sheets dengan gspread
gc = gspread.service_account(filename=CREDENTIALS_PATH)
sh = gc.open_by_key(SPREADSHEET_ID)

def update_status_pesanan(user_id, status):
    try:
        worksheet_pesanan = sh.worksheet("Pelanggan")
        cell = worksheet_pesanan.find(str(user_id))  # Cari user_id di Google Sheets
        
        if not cell:
            print(f"❌ ERROR: User ID {user_id} tidak ditemukan di Google Sheets!")
            return False  # Hentikan proses jika user_id tidak ditemukan

        # Update status pesanan di kolom J (kolom ke-10)
        worksheet_pesanan.update_cell(cell.row, 10, status)  
        print(f"✅ Status pesanan user {user_id} diperbarui ke '{status}'.")
        return True
    except gspread.exceptions.CellNotFound:
        print(f"❌ ERROR: User ID {user_id} tidak ditemukan di Google Sheets!")
        return False
    except Exception as e:
        print(f"❌ ERROR: Gagal memperbarui status pesanan: {e}")
        return False


# Fungsi untuk melihat status pesanan
async def status_pesanan(update: Update, context: ContextTypes.DEFAULT_TYPE):
    user_id = update.effective_user.id
    worksheet_pesanan = sh.worksheet("Pelanggan")
    
    try:
        cell = worksheet_pesanan.find(str(user_id))
        status = worksheet_pesanan.cell(cell.row, 10).value  # Mengambil status dari kolom 10 (Status Pesanan)
        await update.message.reply_text(f"Status Pesanan Anda: {status}")
    except gspread.exceptions.CellNotFound:
        await update.message.reply_text("❌ Pesanan tidak ditemukan.")