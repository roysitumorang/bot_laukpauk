from flask import Flask, request, jsonify

app = Flask(__name__)

@app.route("/", methods=["GET"])
def health_check():
    return "✅ Flask is running!", 200  # Health check route

@app.route("/webhook/telegram", methods=["POST"])
def telegram_webhook():
    """Receive updates from Telegram and process them"""
    data = request.get_json(silent=True)
    print(f"📥 Webhook diterima: {data}")  # Debugging log

    if data:
        return jsonify({"status": "received"}), 200

    print("⚠️ Tidak ada data yang diterima dari webhook!")
    return jsonify({"error": "No data received"}), 400

if __name__ == "__main__":
    print("🚀 Menjalankan Flask untuk webhook...")
    app.run(host="0.0.0.0", port=8080, debug=True)
