# helpers.py

# Fungsi konversi aman ke float
# Digunakan untuk menghindari error Decimal + float

def f(x):
    """
    Konversi nilai ke float dengan aman, default 0 jika None, Decimal, atau invalid.
    """
    try:
        return float(x or 0)
    except Exception:
        return 0.0