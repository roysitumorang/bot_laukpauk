import json
import os

MAPPING_FILE = "invoice_mapping.json"

def simpan_mapping_ke_file(invoice_id, user_id):
    data = {}
    if os.path.exists(MAPPING_FILE):
        with open(MAPPING_FILE, "r") as f:
            try:
                data = json.load(f)
            except json.JSONDecodeError:
                pass

    data[invoice_id] = user_id
    with open(MAPPING_FILE, "w") as f:
        json.dump(data, f)


def ambil_user_dari_invoice(invoice_id):
    if not os.path.exists(MAPPING_FILE):
        return None
    with open(MAPPING_FILE, "r") as f:
        try:
            data = json.load(f)
            return data.get(invoice_id)
        except json.JSONDecodeError:
            return None
