from laukpauk import keranjang, hitung_poin, get_poin_pelanggan, simpan_data_pelanggan, update_status_pesanan, generate_invoice, ADMIN_USER_ID, bot_app
from datetime import datetime
import os

async def selesaikan_checkout_setelah_webhook(user_id):
    try:
        total_belanja = sum((item["harga"] - item["diskon"]) * item["jumlah"] for item in keranjang.get(user_id, {}).values())
        poin_didapat = hitung_poin(total_belanja)
        total_poin_lama = get_poin_pelanggan(user_id)
        total_poin_baru = total_poin_lama + poin_didapat

        simpan_data_pelanggan(
            user_id,
            nama="Pelanggan Laukpauk",
            alamat="-",
            hp="-",
            detail_alamat="-",
            produk_terakhir=keranjang.get(user_id, {}),
            total_poin_baru=total_poin_baru
        )

        update_status_pesanan(user_id, "Diproses")

        jam_pesan = datetime.now().hour
        pdf_invoice = generate_invoice(user_id, {}, keranjang, jam_pesan)

        if pdf_invoice and os.path.exists(pdf_invoice):
            await bot_app.bot.send_document(
                chat_id=user_id,
                document=open(pdf_invoice, "rb"),
                filename=os.path.basename(pdf_invoice),
                caption="🧾 Terima kasih! Berikut invoice pembelian Anda."
            )

            await bot_app.bot.send_document(
                chat_id=ADMIN_USER_ID,
                document=open(pdf_invoice, "rb"),
                filename=os.path.basename(pdf_invoice),
                caption=f"📦 Pesanan baru dari User {user_id} telah lunas dan siap diproses!"
            )

        keranjang[user_id] = {}
        print(f"✅ [WEBHOOK] Checkout selesai untuk user {user_id}")
    except Exception as e:
        print(f"❌ [WEBHOOK ERROR] Gagal menyelesaikan checkout webhook: {e}")
