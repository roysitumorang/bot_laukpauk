def hitung_poin(total_belanja):
    """
    Menghitung jumlah poin berdasarkan sistem loncat.
    """
    tabel_poin = [
        (50000, 500), (75000, 1500), (150000, 2500), (200000, 3000),
        (250000, 2500), (300000, 5000), (350000, 3500), (400000, 7000),
        (450000, 4500), (500000, 10000), (550000, 5500), (600000, 12000),
        (650000, 6500), (700000, 14000), (750000, 7500), (800000, 16000),
        (850000, 8500), (900000, 18000), (950000, 9500), (1000000, 20000)
    ]
    
    poin_didapat = 0
    for batas, poin in tabel_poin:
        if total_belanja >= batas:
            poin_didapat = poin
        else:
            break
    return poin_didapat


def rekomendasi_tambahan_belanja(total_belanja):
    """
    Menghitung berapa tambahan belanja yang dibutuhkan untuk mendapatkan poin lebih tinggi.
    """
    tabel_poin = [
        (50000, 500), (75000, 1500), (150000, 2500), (200000, 3000),
        (250000, 2500), (300000, 5000), (350000, 3500), (400000, 7000),
        (450000, 4500), (500000, 10000), (550000, 5500), (600000, 12000),
        (650000, 6500), (700000, 14000), (750000, 7500), (800000, 16000),
        (850000, 8500), (900000, 18000), (950000, 9500), (1000000, 20000)
    ]

    poin_sekarang = hitung_poin(total_belanja)
    for batas, poin in tabel_poin:
        if batas > total_belanja:
            return batas - total_belanja, poin
    return None, None
