import os
import json
from utils_checkout import DecimalEncoder

def reset_checkout_state(user_id: int):
    """Reset semua state checkout dan keranjang setelah transaksi selesai atau dibatalkan."""
    user_file = f"user_data_{user_id}.json"
    keranjang_file = f"keranjang_{user_id}.json"

    if os.path.exists(user_file):
        try:
            with open(user_file, "r") as f:
                user_data = json.load(f)
            user_data["checkout_step"] = None
            user_data["checkout_state"] = None
            with open(user_file, "w") as f:
                json.dump(user_data, f, cls=DecimalEncoder)
            print(f"🧹 user_data untuk user {user_id} berhasil direset.")
        except Exception as e:
            print(f"❌ Gagal reset user_data untuk user {user_id}: {e}")

    if os.path.exists(keranjang_file):
        try:
            with open(keranjang_file, "w") as f:
                json.dump({}, f)
            print(f"🧺 keranjang user {user_id} berhasil dikosongkan.")
        except Exception as e:
            print(f"❌ Gagal reset keranjang untuk user {user_id}: {e}")


def is_user_in_checkout(user_id: int) -> bool:
    """Cek apakah user masih dalam proses checkout."""
    user_file = f"user_data_{user_id}.json"
    if os.path.exists(user_file):
        with open(user_file, "r") as f:
            user_data = json.load(f)
        return user_data.get("checkout_step") in ("invoice_dikirim", "menunggu_pembayaran")
    return False
