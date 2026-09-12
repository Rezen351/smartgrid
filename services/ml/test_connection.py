#!/usr/bin/env python3
"""
Skrip Uji Coba Integrasi API SmartGrid untuk Machine Learning
"""

import sys
import json
from api_client import SmartGridClient

def main():
    print("=" * 60)
    print("⚡ PENGUJIAN INTEGRASI SMARTGRID API -> MACHINE LEARNING ⚡")
    print("=" * 60)

    # Inisialisasi client
    client = SmartGridClient()
    print(f"Target Base URL : {client.base_url}")
    print(f"Username        : {client.username}")

    # 1. Health Check
    print("\n[1/4] Mengecek Status Server & Export Service...")
    try:
        health = client.check_health()
        print(f"  Status: {health}")
    except Exception as e:
        print(f"  ❌ Gagal menghubungi server: {e}")
        sys.exit(1)

    # 2. Login
    print("\n[2/4] Melakukan Autentikasi (Login)...")
    try:
        client.login()
        print(f"  Token berhasil didapatkan! (Prefix: {client.access_token[:20]}...)")
    except Exception as e:
        print(f"  ❌ Login gagal: {e}")
        sys.exit(1)

    # 3. Cek Node & Metrik
    print("\n[3/4] Mengambil Daftar Node & Metrik Sensor yang Tersedia...")
    try:
        nodes = client.get_nodes()
        print(f"  Ditemukan {len(nodes)} node:")
        for n in nodes:
            print(f"    - Node ID : {n.get('node_id')}")
            print(f"      Module  : {n.get('module_id')}")
            print(f"      Metrics : {n.get('metrics')}")
    except Exception as e:
        print(f"  ❌ Gagal mengambil nodes: {e}")
        sys.exit(1)

    # 4. Ambil Sampel Telemetri
    print("\n[4/4] Mengambil Sampel Data Telemetri (5 baris terakhir)...")
    try:
        sample = client.get_telemetry(node_id="SmartGrid-01", limit=5, output_format="json")
        data_rows = sample.get("data", {}).get("rows", [])
        total_rows = sample.get("data", {}).get("total", 0)
        print(f"  Total baris diterima : {len(data_rows)} (has_more: {sample.get('data', {}).get('has_more')})")
        print("\n  Cuplikan Data Telemetri:")
        print("  " + "-" * 56)
        for row in data_rows:
            print(f"  Waktu: {row.get('time')} | Node: {row.get('node_id')} | Metrik: {row.get('metric')} | Nilai: {row.get('value')}")
        print("  " + "-" * 56)
    except Exception as e:
        print(f"  ❌ Gagal mengambil data telemetri: {e}")
        sys.exit(1)

    print("\n" + "=" * 60)
    print(" INTEGRASI BERHASIL! DATA SIAP DIOLAH OLEH MACHINE LEARNING.")
    print("=" * 60)

if __name__ == "__main__":
    main()
