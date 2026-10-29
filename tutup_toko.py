import json
from datetime import datetime
import pytz

JAKARTA_TZ = pytz.timezone("Asia/Jakarta")  # ✅ Pastikan semua tanggal menggunakan zona waktu Jakarta

HOLIDAY_FILE = "holidays.json"

def is_holiday(date):
    """Memeriksa apakah tanggal tertentu masuk dalam periode libur toko."""
    date = date.astimezone(JAKARTA_TZ)  # ✅ Pastikan parameter `date` memiliki zona waktu
    
    holidays = load_holidays()
    
    for holiday in holidays:
        start = holiday.get("start", "").strip()
        end = holiday.get("end", "").strip()

        if not start or not end:
            continue  # Abaikan jika tanggal kosong

        try:
            start_date = datetime.strptime(start, "%Y-%m-%d").replace(tzinfo=JAKARTA_TZ)
            end_date = datetime.strptime(end, "%Y-%m-%d").replace(tzinfo=JAKARTA_TZ)
        except ValueError:
            try:
                start_date = datetime.strptime(start, "%d-%m-%Y").replace(tzinfo=JAKARTA_TZ)
                end_date = datetime.strptime(end, "%d-%m-%Y").replace(tzinfo=JAKARTA_TZ)
            except ValueError:
                continue  # Abaikan jika format tidak valid

        if start_date <= date <= end_date:
            return True
    return False


def save_holidays(holidays):
    """Menyimpan daftar tanggal libur ke file JSON. Jika kosong, hapus semua libur."""
    with open(HOLIDAY_FILE, "w") as file:
        json.dump(holidays, file, indent=4)

def load_holidays():
    """Membaca daftar tanggal libur dari file JSON."""
    try:
        with open(HOLIDAY_FILE, "r") as file:
            return json.load(file)
    except (FileNotFoundError, json.JSONDecodeError):
        return []

def get_holiday_dates():
    """Mengembalikan daftar tanggal libur yang sudah diformat dengan benar, serta menangani kasus daftar kosong."""
    holidays = load_holidays()

    date_ranges = set()
    for h in holidays:
        start_str = h.get("start", "").strip()
        end_str = h.get("end", "").strip()

        if not start_str or not end_str:
            continue  # ✅ Abaikan jika salah satu tanggal kosong

        try:
            # ✅ Coba parsing dengan format YYYY-MM-DD
            start_date = datetime.strptime(start_str, "%Y-%m-%d")
            end_date = datetime.strptime(end_str, "%Y-%m-%d")
        except ValueError:
            try:
                # ✅ Jika gagal, coba parsing dengan format DD-MM-YYYY
                start_date = datetime.strptime(start_str, "%d-%m-%Y")
                end_date = datetime.strptime(end_str, "%d-%m-%Y")
            except ValueError:
                continue  # ✅ Abaikan jika format tidak valid

        date_ranges.add((start_date, end_date))

    # ✅ Urutkan tanggal dan gabungkan rentang yang berdekatan
    date_ranges = sorted(date_ranges, key=lambda x: x[0])
    merged_ranges = []
    for start, end in date_ranges:
        if merged_ranges and merged_ranges[-1][1] >= start:
            merged_ranges[-1] = (merged_ranges[-1][0], max(merged_ranges[-1][1], end))
        else:
            merged_ranges.append((start, end))

    # ✅ Format ulang ke "DD-MM-YYYY s/d DD-MM-YYYY"
    formatted_holidays = [f"{s.strftime('%d-%m-%Y')} s/d {e.strftime('%d-%m-%Y')}" for s, e in merged_ranges]

    return " | ".join(formatted_holidays) if formatted_holidays else "Tidak ada libur saat ini."


def set_holiday_dates(start_date, end_date):
    """Menambahkan rentang tanggal libur baru."""
    holidays = load_holidays()
    holidays.append({"start": start_date, "end": end_date})
    save_holidays(holidays)
    return f"Toko akan libur dari {start_date} sampai {end_date}."
