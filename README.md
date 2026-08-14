# SmartGrid

SmartGrid adalah project aplikasi dan infrastruktur berbasis Docker untuk mengelola layanan seperti Nginx, Mosquitto, dan integrasi Cloudflare Tunnel.

## 1. Tujuan Proyek

Project ini dirancang untuk:
- menjalankan layanan aplikasi dalam environment yang konsisten,
- mempermudah kolaborasi antar developer,
- menstandarkan proses deployment menggunakan pola CI/CD,
- menjaga struktur konfigurasi yang aman dan mudah dikelola.

## 2. Prasyarat

Pastikan perangkat Anda sudah memiliki:
- Docker
- Docker Compose
- Git
- Node.js / runtime lain sesuai kebutuhan aplikasi (jika project backend/frontend ditambahkan)
- File `.env` yang berisi konfigurasi sensitif seperti token tunnel

## 3. Setup Awal

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
CLOUDFLARED_TUNNEL_TOKEN=your_cloudflared_tunnel_token_here
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

## 4. Struktur Project

```text
smartgrid/
├── docker-compose.yml
├── .env
├── README.md
├── infra/
│   ├── nginx/
│   │   └── default.conf
│   └── mosquitto/
│       └── config/
│           └── mosquitto.conf
├── services/
├── volumes/
│   └── mosquitto/
│       ├── data/
│       └── logs/
└── docs/
```

## 5. Standar Kolaborasi Developer

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
git commit -m "feat: add nginx default config"
git commit -m "fix: resolve mosquitto volume path"
git commit -m "docs: update collaboration workflow"
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
- kode sudah di-review oleh rekan tim,
- tidak ada konflik merge,
- konfigurasi environment aman dan tidak di-commit token sensitif,
- validasi Docker Compose berhasil,
- dokumentasi diperbarui bila perlu,
- semua test / validation pipeline berhasil.

## 6. CI/CD

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

## 7. Keamanan dan Environment

Beberapa aturan penting:
- Jangan commit token atau secret ke repository.
- Selalu simpan value sensitif di file `.env` atau secret manager.
- Pastikan `.env` masuk ke `.gitignore`.
- Gunakan variabel environment pada pipeline CI/CD dan deployment.
- Hindari hardcode URL atau credential di source code.

## 8. Deployment Notes

Untuk deployment:
- pastikan Docker Compose berjalan di environment target,
- validasi port yang digunakan tidak bentrok,
- pastikan volume data dan konfigurasi bersifat persistent,
- cocokkan token Cloudflare atau secret pada environment deployment.

## 9. Rekomendasi Tim

Untuk menjaga kualitas proyek:
- gunakan review code secara rutin,
- maintain dokumentasi project secara berkala,
- buat changelog atau release notes untuk setiap milestone,
- lakukan testing sebelum merge ke branch utama,
- update README ketika arsitektur atau setup berubah.

## 10. Quick Start

```bash
docker compose up -d
docker compose ps
```

Jika ada perubahan konfigurasi atau service baru, lakukan validasi terlebih dahulu:

```bash
docker compose config
```

---

Dokumentasi ini bertujuan agar proses pengembangan dan deployment konsisten, aman, dan mudah dikelola oleh seluruh developer yang terlibat dalam project SmartGrid.
