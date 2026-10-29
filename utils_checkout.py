import os
import json
from reportlab.pdfgen import canvas
from reportlab.lib.pagesizes import A4
from reportlab.lib.utils import ImageReader
from datetime import datetime
from poin import hitung_poin
from product_service import get_db_connection
import gspread
from decimal import Decimal

def format_rupiah(angka):
    try:
        if isinstance(angka, str):
            angka = angka.replace("Rp", "").replace(".", "").replace(",", "").strip()
            angka = float(angka)
        return f"Rp {int(angka):,}".replace(",", ".")
    except Exception as e:
        print(f"❌ [format_rupiah ERROR] {e} | input: {angka}")
        return "Rp 0"

def reset_checkout(user_data: dict):
    """
    Membersihkan data checkout dari context.user_data
    agar user bisa memulai proses baru tanpa terblokir.
    """
    for key in ["checkout_step", "checkout_done", "invoice_id", "invoice_url", "keranjang", "metode_pembayaran"]:
        user_data.pop(key, None)


class DecimalEncoder(json.JSONEncoder):
    def default(self, obj):
        if isinstance(obj, Decimal):
            return float(obj)
        return super().default(obj)

async def get_poin_pelanggan(user_id):
    conn = await get_db_connection()
    result = await conn.fetchrow("SELECT total_poin FROM pelanggan WHERE user_id = $1", str(user_id))
    await conn.close()
    return result["total_poin"] if result else 0

# Ambil sheet pelanggan (bisa dipassing dari luar jika perlu)
gc = gspread.service_account(filename="/home/user/bot_laukpauk/credentials.json")
sh = gc.open_by_key("157srz4epQMUJ5q_fo3Q8FkrBdo40qmfq2VsL2FtKV7Q")
worksheet_pelanggan = sh.worksheet("Pelanggan")

def generate_invoice(user_id, user_data, keranjang, jam_pesan):
    """
    Membuat file PDF invoice berdasarkan data transaksi.
    """

    # **Ambil produk dari keranjang user**
    produk_keranjang = keranjang.get(user_id, {})

    if not produk_keranjang:
        print(f"⚠️ [ERROR] Tidak ada produk dalam keranjang untuk user_id {user_id}")
        return None  # **Jangan lanjutkan jika keranjang kosong**

    # **Format Nomor Invoice**
    tanggal_str = datetime.now().strftime("%d%m%y")
    invoice_no = f"INV-{user_id}/{tanggal_str}/Laukpauk"

    # **Pastikan direktori penyimpanan ada**
    invoice_dir = "/home/user/Laukpauk/invoices"
    os.makedirs(invoice_dir, exist_ok=True)

    # **Nama file PDF**
    pdf_filename = f"Invoice_{user_id}_{tanggal_str}.pdf"
    pdf_path = os.path.join(invoice_dir, pdf_filename)

    # **Buat PDF**
    c = canvas.Canvas(pdf_path, pagesize=A4)
    width, height = A4

    # **Tambahkan Logo**
    logo_path = "/home/user/bot_laukpauk/logoLP.jpg"
    if os.path.exists(logo_path):
        c.drawImage(ImageReader(logo_path), 40, height - 80, width=100, height=50, preserveAspectRatio=True)

    # **Tulis Judul Invoice**
    c.setFont("Helvetica-Bold", 14)
    c.drawString(150, height - 50, "INVOICE PEMBELIAN")
    c.setFont("Helvetica", 10)
    c.drawString(150, height - 65, f"No: {invoice_no}")

    # **Gambar Garis Pemisah**
    c.line(40, height - 90, width - 40, height - 90)

    # **Tampilkan Data Pembeli**
    c.setFont("Helvetica-Bold", 12)
    c.drawString(40, height - 110, "Detail Pembeli")
    c.setFont("Helvetica", 10)
    c.drawString(40, height - 125, f"Nama: {user_data.get('nama', '-')}")
    c.drawString(40, height - 140, f"Alamat: {user_data.get('alamat', '-')}")

    # ✅ Debugging
    print(f"✅ [DEBUG] Data untuk Invoice - Detail Alamat: {user_data.get('detail_alamat', '-')}")

    # 🔄 Perbaikan Detail Alamat
    c.drawString(40, height - 155, f"Detail: {user_data.get('detail_alamat', '-')}")
    c.drawString(40, height - 170, f"No. HP: {user_data.get('hp', '-')}")

    # 🔄 Pastikan tanggal tetap muncul jika hanya jam dipilih
    tanggal_invoice = datetime.now().strftime('%d-%m-%Y')
    waktu_pengiriman = user_data.get('waktu_pengiriman', '-')
    now = datetime.now() # ✅ Definisikan `now` sebelum digunakan

    # ✅ Jika pemesanan setelah jam 22:00, tambahkan tanggal besok
    if "-" not in waktu_pengiriman and int(jam_pesan) >= 22:
        besok = now + datetime.timedelta(days=1)  # Tambahkan 1 hari ke tanggal sekarang
        waktu_pengiriman = f"{besok.strftime('%d-%m-%Y')} - {waktu_pengiriman}"
        
    # ✅ Jika pemesanan sebelum jam 22:00, gunakan tanggal hari ini
    elif "-" not in waktu_pengiriman:
        waktu_pengiriman = f"{now.strftime('%d-%m-%Y')} - {waktu_pengiriman}"


    # 🔄 Pindahkan informasi pengiriman ke bawah HP
    c.drawString(40, height - 185, f"🚚 Pengiriman: {waktu_pengiriman}")

    # 🔄 **Mulai daftar produk lebih dekat**
    y_position = height - 210

    # **Gambar Garis Pemisah**
    c.line(40, y_position, width - 40, y_position)
    y_position -= 20

    # **Tampilkan Produk yang Dibeli**
    c.setFont("Helvetica-Bold", 12)
    c.drawString(40, y_position, "🛒 Daftar Produk")
    y_position -= 15

    c.setFont("Helvetica", 10)
    total_belanja = 0

    for produk, data in produk_keranjang.items():
        harga_asli = data.get("harga", 0)
        diskon = data.get("diskon", 0)
        jumlah = data.get("jumlah", 0)
        harga_setelah_diskon = harga_asli - diskon
        subtotal = jumlah * harga_setelah_diskon
        total_belanja += subtotal

        c.drawString(40, y_position, f"{produk} x{jumlah}  @ Rp {harga_setelah_diskon:,}")
        c.drawString(350, y_position, f"Rp {int(subtotal):,}")
        y_position -= 20  # Kurangi jarak antar produk agar lebih rapat

    # **Hitung Total Pembayaran**
    ongkir = user_data.get("ongkir", 0)
    voucher_diskon = user_data.get("diskon_voucher", 0)
    poin = user_data.get("poin", 0)
    
    total_pembayaran = max(0, 
        Decimal(str(total_belanja)) +
        Decimal(str(ongkir)) -
        Decimal(str(voucher_diskon)) -
        Decimal(str(poin))
    )


    # 🔄 Tambahkan total pembayaran ke dalam invoice
    y_position -= 10
    c.line(40, y_position, width - 40, y_position)
    y_position -= 20

    c.setFont("Helvetica-Bold", 10)
    c.drawString(40, y_position, "Subtotal:")
    c.drawString(350, y_position, f"Rp {int(total_belanja):,}")
    y_position -= 20

    c.drawString(40, y_position, "Ongkos Kirim:")
    c.drawString(350, y_position, f"Rp {int(ongkir):,}")
    y_position -= 20

    c.drawString(40, y_position, "Diskon Voucher:")
    c.drawString(350, y_position, f"- Rp {int(voucher_diskon):,}")
    y_position -= 20

    c.drawString(40, y_position, "Poin Digunakan:")
    c.drawString(350, y_position, f"- Rp {int(poin):,}")
    y_position -= 20

    y_position -= 10
    c.line(40, y_position, width - 40, y_position)
    y_position -= 25

    c.setFont("Helvetica-Bold", 14)
    c.drawString(40, y_position, "💰 Total Pembayaran:")
    c.drawString(350, y_position, f"Rp {int(total_pembayaran):,}")  # ✅ Sekarang sudah aman digunakan

    # 🔄 **Tambahkan jarak sebelum teks ucapan terima kasih**
    y_position -= 25
    c.setFont("Helvetica", 10)
    c.drawString(40, y_position, "Terima kasih telah berbelanja di Laukpauk! 🙏")

    # **Simpan PDF**
    c.save()

    print(f"✅ [DEBUG] Invoice berhasil dibuat: {pdf_path}")
    return pdf_path




# ✅ **Pastikan data pesanan terakhir tersimpan dengan benar**
def simpan_data_pelanggan(user_id, nama, alamat, hp, detail_alamat, produk_terakhir, total_poin_baru):
    """Menyimpan atau memperbarui data pelanggan di Google Sheets."""
    try:
        data_pelanggan = worksheet_pelanggan.get_all_records(expected_headers=["User ID", "Nama", "Alamat", "HP", "Detail Alamat", "Produk Terakhir", "Total Poin"])
        row_index = None

        produk_terakhir_str = json.dumps(produk_terakhir, ensure_ascii=False, cls=DecimalEncoder)

        # ✅ Konversi poin ke int agar tidak error saat dikirim ke Google Sheets
        total_poin_baru = int(total_poin_baru)
        
        if not detail_alamat or detail_alamat.strip() == "":
            detail_alamat = "TIDAK DIISI"

        for index, row in enumerate(data_pelanggan, start=2):  
            if str(row["User ID"]) == str(user_id):
                row_index = index
                break

        if row_index:
            worksheet_pelanggan.update(f"A{row_index}:G{row_index}", [[
                user_id, nama, alamat, hp, detail_alamat, produk_terakhir_str, total_poin_baru
            ]])
            print(f"✅ [DEBUG] Data pelanggan diperbarui di Google Sheets untuk user {user_id}")
        else:
            worksheet_pelanggan.insert_row(
                [user_id, nama, alamat, hp, detail_alamat, produk_terakhir_str, total_poin_baru],
                index=2  # ⬅️ Menyisipkan tepat di bawah header
            )

            print(f"✅ [DEBUG] Data pelanggan baru ditambahkan untuk user {user_id}")

    except Exception as e:
        print(f"❌ [ERROR] Gagal menyimpan data pelanggan: {e}")
