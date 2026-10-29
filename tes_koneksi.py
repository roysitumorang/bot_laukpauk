import psycopg2

try:
    conn = psycopg2.connect(
        dbname="laukpaukdb",
        user="laukpauk",
        password="At3n4J3i88899$",
        host="localhost",
        port="5432"
    )
    print("✅ Koneksi berhasil ke database!")
    conn.close()

except Exception as e:
    print("❌ Koneksi gagal:", e)
