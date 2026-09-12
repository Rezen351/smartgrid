# SmartGrid

<div align="center">

[![Docker](https://img.shields.io/badge/Docker-Containers-2496ED?logo=docker&logoColor=white)](https://www.docker.com/)
[![Docker Compose](https://img.shields.io/badge/Docker_Compose-Orchestration-2496ED?logo=docker&logoColor=white)](https://docs.docker.com/compose/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-Relational-4169E1?logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![Grafana](https://img.shields.io/badge/Grafana-Visualization-F46800?logo=grafana&logoColor=white)](https://grafana.com/)
[![Prometheus](https://img.shields.io/badge/Prometheus-Metrics-E6522C?logo=prometheus&logoColor=white)](https://prometheus.io/)
[![MQTT](https://img.shields.io/badge/MQTT-Mosquitto-660066?logo=eclipse&logoColor=white)](https://mosquitto.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

**Infrastruktur Docker Compose untuk platform IoT Smart Grid, messaging, time-series database, dan monitoring stack.**

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
git clone https://github.com/Rezen351/smartgrid.git
cd smartgrid
```

Salin konfigurasi env:

```bash
cp .env.example .env
```

Edit `.env` dan ganti semua nilai `CHANGE_ME_*` dengan nilai yang aman. Jangan commit file `.env`.

Variabel yang didukung tersedia di `.env.example`; konfigurasi DSN bersifat opsional dan hanya diperlukan jika ingin mengganti nilai default Compose.

Contoh konfigurasi minimum:

```env
# Cloudflare Tunnel
CLOUDFLARE_TUNNEL_TOKEN=your_tunnel_token_here
CLOUDFLARE_DOMAIN=your-domain.com

# Grafana
GRAFANA_ADMIN_USER=admin
GRAFANA_ADMIN_PASSWORD=<grafana_password>

# Dashboard PostgreSQL
POSTGRES_USER=smartgrid
POSTGRES_PASSWORD=CHANGE_ME_postgres_password
POSTGRES_DB=smartgrid

# Auth PostgreSQL
AUTH_DB_USER=auth_user
AUTH_DB_PASSWORD=<auth_db_password>
AUTH_DB_NAME=auth_db
JWT_SECRET=CHANGE_ME_jwt_secret
AUTH_ADMIN_USERNAME=admin
AUTH_ADMIN_EMAIL=admin@smartgrid.local
AUTH_ADMIN_PASSWORD=CHANGE_ME_admin_password

# Module Service
MODULE_DB_USER=module_user
MODULE_DB_PASSWORD=CHANGE_ME_module_db_password
MODULE_DB_NAME=module_db
TIMESCALE_DB_USER=module_user
TIMESCALE_DB_PASSWORD=CHANGE_ME_timescale_password
TIMESCALE_DB_NAME=module_ts

# Redis Shared Cache
REDIS_PASSWORD=
REDIS_DB=0

# Mosquitto MQTT
MQTT_USER=
MQTT_PASS=
MQTT_TOPIC_PREFIX=smartfarm
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
| **cloudflared** | - | Cloudflare tunnel | Aktif |
| **grafana** | `3000` | Visualization dashboard | Aktif |
| **prometheus** | `9090` | Metrics scraping & alerting | Aktif |
| **cadvisor** | `8080` | Container metrics exporter | Aktif |
| **dashboard** | `5000` (internal) | Flask web app (HMI/API) | Aktif, via nginx /dashboard/ |
| **auth** | `8080` (internal) | Auth service (JWT, RBAC) | Aktif, via nginx /auth/ |
| **module** | `8080` (internal) | Module service (IoT logic) | Aktif, via nginx /modules/ |
| **nats** | `4222`, `8222` | Messaging (JetStream) | Aktif |
| **mosquitto** | `1883`, `9001` | MQTT broker untuk IoT | Aktif |
| **redis-shared** | `6379` | Shared cache | Aktif |
| **postgres-dashboard** | `5432` (internal) | Metadata untuk dashboard | Aktif |
| **postgres-auth** | `5432` (internal) | Metadata untuk auth | Aktif |
| **mariadb-module** | `3306` (internal) | Relational data untuk module | Aktif |
| **timescaledb-module** | `5432` (internal) | Time-series data untuk module | Aktif |
| **nginx-exporter** | `9113` (internal) | Nginx metrics | Aktif |
| **redis-exporter** | `9121` (internal) | Redis metrics | Aktif |
| **mosquitto-exporter** | `9123` (internal) | Mosquitto metrics | Aktif |
| **postgres-dashboard-exporter** | `9187` (internal) | PostgreSQL dashboard metrics | Aktif |
| **postgres-auth-exporter** | `9187` (internal) | PostgreSQL auth metrics | Aktif |
| **mariadb-module-exporter** | `9104` (internal) | MariaDB module metrics | Aktif |
| **timescaledb-exporter** | `9187` (internal) | TimescaleDB module metrics | Aktif |

> **Catatan:**
> - Akses dashboard, auth, dan module melalui reverse proxy nginx di `http://localhost:3001/`.
> - Cloudflare tunnel menyediakan akses eksternal yang aman.

### Data Flow Monitoring

```text
cadvisor:8080 ─┐
               ├──► prometheus:9090 ──► grafana:3000 (via nginx /grafana/)
postgres-dashboard-exporter:9187 ─┘
postgres-auth-exporter:9187 ───┘
mariadb-module-exporter:9104 ──┘
timescaledb-exporter:9187 ─────┘
nginx-exporter:9113 ───────────┘
redis-exporter:9121 ────────────┘
mosquitto-exporter:9123 ────────┘
```

### Data Flow Application

```text
dashboard:5000 ◄── nginx:3001 ──► auth:8080
                         │
                         ├──► module:8080 ◄── nats:4222
                         │         │
                         │         ├──► mosquitto:1883 (MQTT)
                         │         ├──► mariadb-module:3306
                         │         └──► timescaledb-module:5432
                         │
                         └──► grafana:3000
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
│   ├── mariadb/
│   │   └── module/
│   │       └── init.sql
│   ├── grafana/
│   │   ├── provisioning/
│   │   └── dashboards/
│   ├── prometheus/
│   │   └── prometheus.yml
│   └── cloudflared/
│       └── config.yml
├── services/
│   ├── auth/          # Go service: authentication & RBAC
│   ├── module/        # Go service: IoT module logic
│   ├── export/        # Go service: data export
│   └── dashboard/     # Flask app: HMI & API
├── tests/
├── volumes/
│   ├── mosquitto/
│   ├── redis/
│   ├── postgres/
│   ├── postgres-auth/
│   ├── grafana/
│   ├── prometheus/
│   ├── nats/
│   ├── mariadb-module/
│   └── timescaledb-module/
└── docs/
```

## Monitoring

Stack monitoring menggunakan **Prometheus** + **Grafana**:

### Prometheus
- Scrape metrics dari cadvisor, postgres-exporter, grafana, nginx, redis, mosquitto, dashboard, auth, module, nats
- Konfigurasi scrape di `infra/prometheus/prometheus.yml`
- UI: `http://localhost:9090`

### Grafana
- Dashboard visualization
- Data source: Prometheus
- Akses via nginx: `http://localhost:3001/grafana/`
- Dashboard System Monitoring: `http://localhost:3001/grafana/d/system-monitoring/system-monitoring`

### cAdvisor
- Ekspos metrics container Docker (CPU, memory, network, disk)
- UI: `http://localhost:8080`

## Layanan Aplikasi

### Dashboard (Flask)
- Web app untuk HMI dan REST API
- Akses via nginx: `http://localhost:3001/dashboard/`
- API endpoint: `http://localhost:3001/api/v1/`
- Health check: `http://localhost:3001/health/ready`

### Auth (Go)
- Service untuk autentikasi dan otorisasi (JWT, RBAC)
- Akses via nginx: `http://localhost:3001/auth/`

### Module (Go)
- Service untuk logika IoT module
- Berkomunikasi dengan NATS, Mosquitto, MariaDB, dan TimescaleDB
- Akses via nginx: `http://localhost:3001/modules/` atau `http://localhost:3001/api/module/`

## Messaging

### NATS
- Messaging system dengan JetStream untuk komunikasi antar service
- Port: `4222` (client), `8222` (HTTP monitoring)

### Mosquitto MQTT
- MQTT broker untuk komunikasi dengan perangkat IoT
- Port: `1883` (TCP; dipublish ke host)
- WebSocket port `9001` belum dipublish pada Compose utama; aktifkan route dan port secara eksplisit sebelum digunakan.

### Export (Go)
- Service untuk export data time-series
- Akses melalui `http://localhost:3001/export/`
- API versi: `http://localhost:3001/api/v1/export/`
- Health check: `http://localhost:3001/api/v1/export/health`
- Spesifikasi API: [docs/export_api.md](docs/export_api.md)

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

## Testing

Validasi yang sama dengan pipeline CI dapat dijalankan secara lokal:

```bash
docker compose config

for service in auth module export; do
    (cd "services/$service" && go test -v ./... && go vet ./...)
done

(cd services/dashboard && python -m py_compile app.py history.py research.py)
```

## CI/CD

Project ini menggunakan GitHub Actions dengan tahapan berikut:

### Pipeline

1. Trigger otomatis pada push atau pull request ke branch `main` dan `develop`.
2. Validasi Docker Compose configuration.
3. Build Docker services.
4. Run Go tests untuk service `auth`, `module`, dan `export`.
5. Run Python syntax check untuk service `dashboard`.

### Contoh Command Validasi

```bash
docker compose config
```

## Keamanan dan Environment

Beberapa aturan penting:
- Jangan commit token atau secret ke repository.
- Selalu simpan value sensitif di file `.env` atau secret manager.
- Pastikan `.env` masuk ke `.gitignore`.
- Gunakan variabel environment pada pipeline CI/CD dan deployment.
- Hindari hardcode URL atau credential di source code.
- Gunakan Cloudflare Tunnel untuk akses eksternal dan batasi port host sesuai kebutuhan.
- Ganti seluruh nilai `CHANGE_ME_*` sebelum menjalankan deployment bersama atau production.
- Jangan expose port database, NATS monitoring, Prometheus, atau Grafana ke internet tanpa autentikasi dan pembatasan jaringan.

## Kontribusi

1. Buat branch dari `develop` dengan pola `feature/<nama>` atau `fix/<nama>`.
2. Jalankan validasi pada bagian [Testing](#testing).
3. Buat commit menggunakan [Conventional Commits](https://www.conventionalcommits.org/), misalnya `feat: add export filter`.
4. Buka Pull Request ke `develop` dan jelaskan perubahan, pengujian, serta dampak konfigurasi.
5. Jangan menyertakan secret, data production, atau perubahan volume pada Pull Request.

## Dukungan dan Pelaporan Keamanan

Gunakan GitHub Issues untuk bug dan pertanyaan penggunaan. Jangan membuat issue publik untuk credential, token, atau kerentanan yang belum diperbaiki; laporkan melalui kanal privat repository dan sertakan langkah reproduksi yang aman.

## Lisensi

Project ini dirilis di bawah [MIT License](LICENSE).

## Deployment Notes

Untuk deployment:
- pastikan Docker Compose berjalan di environment target,
- validasi port yang digunakan tidak bentrok,
- pastikan volume data dan konfigurasi bersifat persistent,
- cocokkan token atau secret pada environment deployment,
- konfigurasi Cloudflare Tunnel sudah benar di `infra/cloudflared/config.yml`.

## Rekomendasi Tim

Untuk menjaga kualitas proyek:
- gunakan review code secara rutin,
- maintain dokumentasi project secara berkala,
- buat changelog atau release notes untuk setiap milestone,
- lakukan testing sebelum merge ke branch utama,
- update README ketika arsitektur atau setup berubah.
