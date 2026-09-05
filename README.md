# SmartGrid

<div align="center">

[![Docker](https://img.shields.io/badge/Docker-Containers-2496ED?logo=docker&logoColor=white)](https://www.docker.com/)
[![Docker Compose](https://img.shields.io/badge/Docker_Compose-Orchestration-2496ED?logo=docker&logoColor=white)](https://docs.docker.com/compose/)
[![InfluxDB](https://img.shields.io/badge/InfluxDB-Time_Series-22BCF2?logo=influxdb&logoColor=white)](https://www.influxdata.com/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-Relational-4169E1?logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![Grafana](https://img.shields.io/badge/Grafana-Visualization-F46800?logo=grafana&logoColor=white)](https://grafana.com/)
[![Prometheus](https://img.shields.io/badge/Prometheus-Metrics-E6522C?logo=prometheus&logoColor=white)](https://prometheus.io/)
[![MQTT](https://img.shields.io/badge/MQTT-Mosquitto-660066?logo=eclipse&logoColor=white)](https://mosquitto.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

**Infrastruktur Docker Compose untuk layanan Smart Grid IoT, time-series database, dan monitoring stack.**

</div>

## Tujuan Proyek

Project ini dirancang untuk:
- Menjalankan layanan aplikasi dalam environment yang konsisten,
- Mempermudah kolaborasi antar developer,
- Menstandarkan proses deployment menggunakan pola CI/CD,
- Menjaga struktur konfigurasi yang aman dan mudah dikelola.

## Prasyarat

Pastikan perangkat Anda sudah memiliki:

- [Docker](https://www.docker.com/get-started)
- [Docker Compose](https://docs.docker.com/compose/install/)
- [Git](https://git-scm.com/downloads)
- File `.env` yang berisi konfigurasi sensitif

## Setup Awal

Clone repository:

```bash
git clone <repository-url>
cd smartgrid
```

Salin konfigurasi env:

```bash
cp .env.example .env
```

Atau buat file `.env` jika belum ada dengan konten seperti:

```env
INFLUXDB_ADMIN_USER=admin
INFLUXDB_ADMIN_PASSWORD=<influxdb_password>
INFLUXDB_ORG=smartgrid
INFLUXDB_BUCKET=smartgrid_data

POSTGRES_USER=smartgrid
POSTGRES_PASSWORD=<postgres_password>
POSTGRES_DB=smartgrid

GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=<grafana_password>
```

Jalankan layanan:

```bash
docker compose up -d
```

Cek status container:

```bash
docker compose ps
```

Cek konfigurasi Docker Compose:

```bash
docker compose config
```

## Arsitektur

Stack SmartGrid terdiri dari:

| Service | Port | Fungsi | Status |
|---|---|---|---|
| **nginx** | `3001` | Reverse proxy | Aktif |
| **mosquitto** | `1883` | MQTT broker untuk IoT | Aktif |
| **influxdb3** | `8181` | Time-series database untuk sensor data | Aktif |
| **postgres** | `5432` | Relational database untuk metadata | Aktif |
| **grafana** | `3000` (internal) | Visualization dashboard | Aktif, via nginx /grafana/ |
| **prometheus** | `9090` | Metrics scraping & alerting | Aktif |
| **cadvisor** | `8080` | Container metrics exporter | Aktif |
| **postgres-exporter** | `9187` | PostgreSQL metrics exporter | Aktif |

> **Catatan:** 
> - InfluxDB digunakan dalam mode v3 Core. Port yang dipublish adalah `8181` (HTTP).
> - Grafana tidak dipublish ke host, diakses melalui reverse proxy nginx di `http://localhost:3001/grafana/`.

### Data Flow Monitoring

```text
cadvisor:8080 ─┐
               ├──► prometheus:9090 ──► grafana:3000 (internal, via nginx /grafana/)
postgres-exporter:9187 ─┘
```

## Struktur Project

```text
smartgrid/
├── docker-compose.yml
├── .env
├── .env.example
├── README.md
├── infra/
│   ├── nginx/
│   │   └── default.conf
│   ├── mosquitto/
│   │   └── config/
│   │       ├── mosquitto.conf
│   │       ├── acl.acl
│   │       └── passwd
│   ├── postgres/
│   │   └── init/
│   ├── grafana/
│   │   ├── provisioning/
│   │   └── dashboards/
│   └── prometheus/
│       └── prometheus.yml
├── services/
├── volumes/
│   ├── mosquitto/
│   │   ├── data/
│   │   └── log/
│   ├── influxdb/
│   ├── postgres/
│   ├── grafana/
│   └── prometheus/
└── docs/
```

## Monitoring

Stack monitoring menggunakan **Prometheus** + **Grafana**:

### Prometheus
- Scrape metrics dari cadvisor, postgres-exporter, influxdb, grafana
- Konfigurasi scrape di `infra/prometheus/prometheus.yml`
- UI: `http://localhost:9090`

### cAdvisor
- Ekspos metrics container Docker (CPU, memory, network, disk)
- UI: `http://localhost:8080`

### Grafana
- Dashboard visualization
- Bisa diintegrasikan dengan Prometheus sebagai data source
- Akses via nginx: `http://localhost:3001/grafana/`
- Dashboard System Monitoring: `http://localhost:3001/grafana/d/system-monitoring/system-monitoring`

## Standar Kolaborasi Developer

### Branching Strategy

Gunakan pola branch berikut:
- `main` : branch produksi, hanya menerima merge yang sudah diverifikasi dan siap deploy.
- `develop` : branch integrasi untuk pengembangan dan testing sebelum release.
- `feature/<nama-fiturnya>` : branch untuk pengembangan fitur baru.
- `fix/<nama-bug>` : branch untuk perbaikan bug.
- `hotfix/<nama-hotfix>` : branch untuk penanganan incident segera.

### Alur Kerja Kolaborasi

1. Pull branch utama terbaru.
2. Buat branch baru sesuai jenis kerja.
3. Lakukan perubahan dengan commit yang jelas.
4. Jalankan validasi lokal sebelum push.
5. Buat Pull Request ke branch target.
6. Lakukan review oleh minimal 1 reviewer.
7. Pastikan pipeline CI/CD berhasil sebelum merge.
8. Merge ke branch utama hanya setelah approval dan validasi lengkap.

### Commit Convention

Gunakan pesan commit yang konsisten, misalnya:

```bash
git commit -m "feat: add prometheus monitoring"
git commit -m "fix: resolve postgres volume path"
git commit -m "docs: update architecture diagram"
```

Format rekomendasi:
- `feat:` untuk fitur baru
- `fix:` untuk perbaikan bug
- `docs:` untuk perubahan dokumentasi
- `chore:` untuk maintenance atau konfigurasi
- `refactor:` untuk refactor kode
- `ci:` untuk perubahan pipeline CI/CD

### Pull Request Checklist

Sebelum merge, pastikan:
- [ ] Kode sudah di-review oleh rekan tim
- [ ] Tidak ada konflik merge
- [ ] Konfigurasi environment aman dan tidak di-commit token sensitif
- [ ] Validasi Docker Compose berhasil
- [ ] Dokumentasi diperbarui bila perlu
- [ ] Semua test / validation pipeline berhasil

## CI/CD

Project ini mengikuti pola CI/CD dasar dengan tahapan berikut:

### Pipeline yang Direkomendasikan

1. Trigger otomatis pada push atau pull request.
2. Checkout repository.
3. Setup environment.
4. Validasi lint dan format.
5. Build aplikasi atau image Docker.
6. Run unit / integration test bila tersedia.
7. Validasi konfigurasi Docker Compose.
8. Publish artifact / image jika build sukses.
9. Deploy ke environment target berdasarkan branch.

### Rekomendasi Pipeline

- `main` -> deploy production
- `develop` -> deploy staging/testing
- `feature/*` -> build validation only

### Contoh Command Validasi

```bash
docker compose config
```

Untuk project yang memiliki aplikasi backend/frontend, tambahkan validasi seperti:

```bash
npm install
npm run lint
npm run test
npm run build
```

## Keamanan dan Environment

Beberapa aturan penting:
- Jangan commit token atau secret ke repository.
- Selalu simpan value sensitif di file `.env` atau secret manager.
- Pastikan `.env` masuk ke `.gitignore`.
- Gunakan variabel environment pada pipeline CI/CD dan deployment.
- Hindari hardcode URL atau credential di source code.

## Deployment Notes

Untuk deployment:
- pastikan Docker Compose berjalan di environment target,
- validasi port yang digunakan tidak bentrok,
- pastikan volume data dan konfigurasi bersifat persistent,
- cocokkan token atau secret pada environment deployment.

## Rekomendasi Tim

Untuk menjaga kualitas proyek:
- gunakan review code secara rutin,
- maintain dokumentasi project secara berkala,
- buat changelog atau release notes untuk setiap milestone,
- lakukan testing sebelum merge ke branch utama,
- update README ketika arsitektur atau setup berubah.

## Quick Start

```bash
docker compose up -d
docker compose ps
```

Jika ada perubahan konfigurasi atau service baru, lakukan validasi terlebih dahulu:

```bash
docker compose config
```

---
