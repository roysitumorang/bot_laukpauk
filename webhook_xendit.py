import os
from fastapi import APIRouter, Request
import json
from telegram import Bot
from dotenv import load_dotenv
from invoice_mapping import ambil_user_dari_invoice
from utils_checkout import generate_invoice, simpan_data_pelanggan, get_poin_pelanggan, hitung_poin
from kirim_barang import tampilkan_tombol_kirim_barang
import datetime
from utils_checkout import DecimalEncoder, format_rupiah, reset_checkout
from decimal import Decimal


load_dotenv()


# Ganti dengan token bot Telegram kamu
TOKEN = os.getenv("BOT_TOKEN")
bot = Bot(token=TOKEN)
CALLBACK_TOKEN = os.getenv("XENDIT_CALLBACK_TOKEN")

# ✅ Gunakan APIRouter agar bisa dipasang ke FastAPI utama
router = APIRouter()

# ⛑️ Kelas Context Palsu untuk dipakai di webhook
class FakeContext:
    def __init__(self, user_data):
        self.user_data = user_data
        self.bot = bot

@router.post("/webhook/xendit")
async def handle_xendit_webhook(request: Request):
    # ✅ Validasi callback token (keamanan)
    if request.headers.get("X-CALLBACK-TOKEN") != CALLBACK_TOKEN:
        print("❌ Token Xendit tidak valid.")
        return {"status": "unauthorized"}, 401

    payload = await request.json()
    invoice_id = payload.get("id")
    print(f"📡 Invoice ID: {invoice_id}")

    user_id = ambil_user_dari_invoice(invoice_id)

    if not user_id:
        print(f"❌ User tidak ditemukan untuk invoice {invoice_id}")
        return {"status": "user_not_found"}

    # ✅ Cek user_data file
    user_data_file = f"user_data_{user_id}.json"
    user_data = {}

    if os.path.exists(user_data_file):
        try:
            with open(user_data_file, "r") as f:
                user_data = json.load(f)
            from utils_checkout import DecimalEncoder

            print(f"📥 [WEBHOOK] User Data yang dibaca:\n{json.dumps(user_data, indent=2, cls=DecimalEncoder)}")


        except Exception as e:
            print(f"❌ Error membaca user_data file: {e}")
    else:
        print(f"❌ File user_data tidak ditemukan untuk user {user_id}")

    # ✅ Cek keranjang file
    keranjang_file = f"keranjang_{user_id}.json"
    from laukpauk import keranjang

    if os.path.exists(keranjang_file):
        try:
            with open(keranjang_file, "r") as f:
                keranjang[user_id] = json.load(f)
        except Exception as e:
            print(f"❌ Error membaca keranjang file: {e}")
    else:
        print(f"❌ File keranjang tidak ditemukan untuk user {user_id}")

    # ✅ Kirim notifikasi ke user
    await bot.send_message(chat_id=user_id, text="✅ Pembayaran Anda berhasil! Pesanan Anda sedang diproses. 🚀")

    print(f"📦 [WEBHOOK] Melanjutkan ke pembuatan invoice untuk user {user_id}")

    # Generate Invoice PDF
    if not user_data:
        print(f"❌ Tidak bisa generate invoice: user_data kosong untuk user {user_id}")
        return {"status": "failed", "message": "user_data kosong"}

    pdf_path = generate_invoice(user_id, user_data, keranjang, datetime.datetime.now().hour)
    print(f"📄 Invoice berhasil dibuat di: {pdf_path}")


    # Kirim ke user (dan admin jika perlu)
    if pdf_path:
        await bot.send_document(chat_id=user_id, document=open(pdf_path, "rb"),
            caption=f"🧾 *Invoice Pembelian Anda*\nTotal: {format_rupiah(user_data.get('total_pembayaran', 0))}\nTerima kasih telah berbelanja!",
            parse_mode="Markdown")

        # Admin notifikasi juga (optional)
        ADMIN_ID = 5464224951
        await bot.send_document(chat_id=ADMIN_ID, document=open(pdf_path, "rb"),
            caption=f"🧾 *Order Baru dari {user_data.get('nama', 'User')}*", parse_mode="Markdown")

        fake_context = FakeContext(user_data)
        await tampilkan_tombol_kirim_barang(fake_context, user_id)


    # Hitung ulang poin
    total_belanja = sum(
        (item["harga"] - item["diskon"]) * item["jumlah"]
        for item in keranjang.get(user_id, {}).values()
    )

    poin_didapat = hitung_poin(total_belanja)
    total_poin_lama = Decimal(str(await get_poin_pelanggan(user_id)))
    poin_digunakan = Decimal(str(user_data.get("poin", 0)))
    total_poin_baru = max(Decimal("0"), total_poin_lama + poin_didapat - poin_digunakan)

    # Simpan ke Google Sheets
    simpan_data_pelanggan(
        user_id,
        user_data.get("nama", "-"),
        user_data.get("alamat", "-"),
        user_data.get("hp", "-"),
        user_data.get("detail_alamat", "-"),
        keranjang.get(user_id, {}),
        total_poin_baru
    )

    # ✅ SIMPAN KE DATABASE
    from db_pelanggan import insert_pelanggan
    insert_pelanggan(
        user_id=user_id,
        nama=user_data.get("nama", "-"),
        alamat=user_data.get("alamat", "-"),
        hp=user_data.get("hp", "-"),
        detail_alamat=user_data.get("detail_alamat", "-"),
        produk_terakhir=json.dumps(keranjang.get(user_id, {})),
        total_poin=int(total_poin_baru)
    )

    # ✅ Tandai status checkout selesai agar tidak dipanggil ulang
    user_data["checkout_step"] = "done"
    with open(f"user_data_{user_id}.json", "w") as f:
        json.dump(user_data, f, cls=DecimalEncoder)


    


