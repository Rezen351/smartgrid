# SmartGrid Energy Switching Machine Learning Microservice

Microservice Machine Learning berbasis Python dan FastAPI untuk mengoptimalkan keputusan **Switching On-Off Generator vs Solar Panel (PV) & Baterai (BESS)** pada sistem jaringan listrik cerdas SmartGrid.

---

## Fitur Utama

1. **Model Machine Learning (Random Forest Classifier)**:
   - Terlatih pada dataset riset SmartGrid (8.928 sampel kurva 15-menitan multi-skenario).
   - **Performa Model:**
     - **Akurasi:** 97.70%
     - **Precision:** 99.25%
     - **Recall:** 97.72%
     - **F1-Score:** 98.48%
   - **Tingkat Kepentingan Fitur (*Feature Importance*):**
     - Kapasitas Baterai (`soc_pct`): 32.0%
     - Produksi Panel Surya (`pv_kw`): 26.7%
     - Selisih Beban Bersih (`net_load_kw`): 22.4%
     - Indikator Siang/Malam (`is_daytime`): 9.7%
     - Jam Operasional (`hour`): 5.4%
     - Beban Listrik (`load_kw`): 3.8%

2. **Lapisan Proteksi Kelistrikan (*Safety Guardrails Layer*)**:
   - **Proteksi Baterai Kritis (*Deep Discharge*):** Jika `soc_pct <= 15.0%`, genset WAJIB `ON` demi melindungi baterai dari kerusakan permanen dan mencegah *blackout*.
   - **Proteksi Pemborosan Energi Surya:** Jika `pv_kw >= load_kw` dan `soc_pct >= 25.0%`, genset WAJIB `OFF` demi penghematan bahan bakar diesel.
   - **Baterai Penuh (*High SOC*):** Jika `soc_pct >= 90.0%`, genset `OFF` dan beban dipasok penuh oleh baterai & PV.

3. **REST API (FastAPI) & Integrasi Server TF**:
   - Mendukung inferensi cepat via HTTP POST.
   - Otomatis terhubung dengan SmartGrid Export API (`api_client.py`) untuk menarik telemetri langsung dari server TF.
   - Dilengkapi `Dockerfile` dan `docker-compose.yml` siap deploy di Server TF.

---

## Struktur File

```text
machine-learning-smartgrid-sinergi-2026/
├── .env                  # Kredensial URL API, Username, Password
├── .gitignore            # Filter file sensitif, model, dan cache
├── requirements.txt      # Dependensi pustaka Python
├── data_generator.py     # Pembangkit dataset riset (8.928 baris)
├── train.py              # Script pelatihan model Random Forest
├── predictor.py          # Mesin inferensi ML + Safety Guardrails
├── app.py                # FastAPI REST API Microservice
├── api_client.py         # Klien koneksi ke SmartGrid Export API
├── test_connection.py    # Skrip pengujian koneksi ke API server TF
├── Dockerfile            # Container image build spec
├── docker-compose.yml    # Konfigurasi container service
└── models/
    └── switching_model.joblib  # Model biner terkompresi
```

---

## Cara Menjalankan

### 1. Pelatihan Model (Jika ingin train ulang)
```bash
python3 train.py
```

### 2. Menjalankan REST API Server
```bash
python3 -m uvicorn app:app --host 0.0.0.0 --port 8000
```
Dokumentasi interaktif Swagger UI akan otomatis tersedia di: [http://localhost:8000/docs](http://localhost:8000/docs).

Spesifikasi OpenAPI statis:
- YAML: [http://localhost:8000/openapi.yaml](http://localhost:8000/openapi.yaml)
- JSON via FastAPI: [http://localhost:8000/openapi.json](http://localhost:8000/openapi.json)

Melalui reverse proxy nginx:
- `curl -sS http://localhost:3001/api/v1/ml/openapi`

### 3. Menggunakan Docker
```bash
# Build dan jalankan container
docker compose up -d --build

# Cek logs container
docker logs -f smartgrid-ml-service
```

---

## Contoh Penggunaan API

### A. Endpoint `/predict-switch` (Manual Parameter)
Kirim beban (`load_kw`), produksi PV (`pv_kw`), dan level baterai (`soc_pct`):

```bash
curl -X POST http://localhost:8000/predict-switch \
  -H "Content-Type: application/json" \
  -d '{
    "load_kw": 40.0,
    "pv_kw": 10.0,
    "soc_pct": 55.0,
    "hour": 14.0
  }'
```

**Contoh Respons JSON:**
```json
{
  "success": true,
  "data": {
    "genset_switch": 1,
    "action": "GENSET_ON",
    "power_source": "GENERATOR",
    "confidence": 0.9729,
    "guardrail_triggered": false,
    "guardrail_rule": null,
    "reason": "Model ML menyarankan Genset MENYALA (Confidence: 97.3%) berdasarkan optimalisasi pola historis smartgrid.",
    "input_parameters": {
      "load_kw": 40.0,
      "pv_kw": 10.0,
      "net_load_kw": 30.0,
      "soc_pct": 55.0,
      "hour": 14.0,
      "is_daytime": 1
    }
  }
}
```

### B. Endpoint `/sync-and-predict` (Otomatis dari Sensor Server TF)
Menarik sensor telemetri terakhir dari node `SmartGrid-01` di server TF lalu langsung menghitung keputusan switching:

```bash
curl -X POST http://localhost:8000/sync-and-predict \
  -H "Content-Type: application/json" \
  -d '{
    "node_id": "SmartGrid-01",
    "fallback_pv_kw": 15.0,
    "fallback_soc_pct": 60.0
  }'
```
