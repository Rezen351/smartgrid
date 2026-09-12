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

> **⚠️ ML Service** berjalan pada host terpisah menggunakan `docker-compose.ml-host.yml`. Jalankan main stack terlebih dahulu sebelum menjalankan ML service.

## Prasyarat

- [Docker](https://www.docker.com/get-started)
- [Docker Compose](https://docs.docker.com/compose/install/)
- [Git](https://git-scm.com/downloads)
- File `.env` berisi konfigurasi sensitif

## Setup Awal

```bash
git clone https://github.com/Rezen351/smartgrid.git
cd smartgrid
cp .env.example .env
```

Edit `.env` dan ganti semua nilai `CHANGE_ME_*` dengan nilai yang aman.

## Jalankan

**Main stack:**
```bash
docker compose up -d
```

**ML service (host terpisah):**
```bash
docker compose -f docker-compose.ml-host.yml up -d
```

**Cek status:**
```bash
docker compose ps
docker compose -f docker-compose.ml-host.yml ps
```

## Arsitektur

| Service | Port | Fungsi |
|---|---|---|
| **nginx** | `3001` | Reverse proxy |
| **cloudflared** | - | Cloudflare tunnel |
| **grafana** | `3000` | Visualization dashboard |
| **prometheus** | `9090` | Metrics & alerting |
| **cadvisor** | `8080` | Container metrics |
| **dashboard** | `5000` | Flask HMI/API |
| **auth** | `8080` | Auth service (JWT, RBAC) |
| **module** | `8080` | IoT module logic |
| **export** | `8080` | Time-series export |
| **ml** | `8000` | ML service *(via `docker-compose.ml-host.yml`)* |
| **nats** | `4222`, `8222` | Messaging (JetStream) |
| **mosquitto** | `1883` | MQTT broker |
| **redis-shared** | `6379` | Shared cache |
| **postgres-dashboard** | `5432` | Dashboard DB |
| **postgres-auth** | `5432` | Auth DB |
| **mariadb-module** | `3306` | Module relational DB |
| **timescaledb-module** | `5432` | Module time-series DB |
| ***-exporter** | `9xxx` | Prometheus exporters |

> Akses semua service melalui `http://localhost:3001/`. ML service diakses via `http://localhost:8000` atau domain Cloudflare Tunnel.

## Struktur Project

```text
smartgrid/
├── docker-compose.yml              # Main stack (tanpa ML)
├── docker-compose.ml-host.yml      # ML service (host terpisah)
├── .env
├── .env.example
├── README.md
├── infra/
│   ├── nginx/default.conf
│   ├── cloudflared/config.yml
│   ├── cloudflared/ml-config.yml
│   ├── mosquitto/config/
│   ├── grafana/provisioning/
│   ├── prometheus/prometheus.yml
│   └── mariadb/module/init.sql
├── services/
│   ├── auth/          # Go: authentication & RBAC
│   ├── module/        # Go: IoT module logic
│   ├── export/        # Go: data export
│   └── dashboard/     # Flask: HMI & API
├── tests/
├── volumes/
└── docs/
```

## Monitoring

- **Prometheus** — `http://localhost:9090`
- **Grafana** — `http://localhost:3001/grafana/`
- **cAdvisor** — `http://localhost:8080`

## Layanan Aplikasi

- **Dashboard** — `http://localhost:3001/dashboard/`
- **Auth** — `http://localhost:3001/auth/`
- **Module** — `http://localhost:3001/modules/`
- **ML Service** — `http://localhost:8000` *(host terpisah)*

## Messaging

- **NATS** — JetStream messaging, port `4222` / `8222`
- **Mosquitto MQTT** — port `1883`
- **Export** — `http://localhost:3001/export/`

## Testing

```bash
docker compose config
docker compose -f docker-compose.ml-host.yml config

for service in auth module export; do
    (cd "services/$service" && go test -v ./... && go vet ./...)
done

(cd services/dashboard && python -m py_compile app.py history.py research.py)
```

## Keamanan

- Jangan commit token atau secret ke repository
- Gunakan `.env` untuk nilai sensitif
- Gunakan Cloudflare Tunnel untuk akses eksternal
- Ganti seluruh nilai `CHANGE_ME_*` sebelum deployment
- Jangan expose port database tanpa autentikasi dan pembatasan jaringan

## Deployment Notes

1. Jalankan main stack terlebih dahulu: `docker compose up -d`
2. Pastikan network `smartgrid-network` sudah ada
3. Jalankan ML service: `docker compose -f docker-compose.ml-host.yml up -d`
4. Validasi Cloudflare Tunnel di `infra/cloudflared/config.yml` dan `infra/cloudflared/ml-config.yml`

## Lisensi

Project ini dirilis di bawah [MIT License](LICENSE).
