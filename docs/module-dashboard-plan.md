# Rencana Penyesuaian UI Dashboard Modul & Service Module

## 1. Tujuan
Menyelaraskan halaman **Manajemen Modul & Node** (`/dashboard/modules`) dengan kemampuan yang sudah ada di **Module Service**, serta menutup gap fungsional, UX, dan observabilitas yang mengganggu operasional IoT.

## 2. Cakupan
- Halaman modules di `services/dashboard/static/partials/modules.html` + `services/dashboard/static/js/modules.js`.
- Backend module service (`services/module/*`) yang diakses via proxy `/api/module/*`.
- Integrasi dashboard ke layanan pendukung (health, telemetry history, export).

## 3. Ringkasan Temuan
| Status | Jumlah |
|---|---|
| Fitur sudah diakomodir dan sesuai | CRUD modul, onboarding node, live telemetry raw, konfigurasi tag sensor/aktuator dasar |
| Fitur ada di backend tapi UI belum/parsial | Config module, edit tag, enable/disable tag, detail node/module, telemetry history, export, health module, RBAC-aware UI |
| Belum ada di backend/UI | Aktuator execution, monitoring/validity khusus module, audit/outbox viewer, bulk onboarding |
| Ketidaksesuaian/bug | Config module tertimpa kosong, auto-detect source key bisa mengembalikan path array/objek, live telemetry timestamp tidak akurat, orphan tag saat hapus node, token refresh tidak dijalankan di `apiCall`, duplikasi partial HTML |

## 4. Prioritas
1. **High** — Perbaiki bug/config overwrite + auto-detect validation + orphan tag + token refresh.
2. **Medium** — Tambah edit tag (sensor), toggle enable/disable, detail node/module, server-side filter/pagination, health module di dashboard.
3. **Low** — Telemetry history/export, actuator execution, audit viewer, bulk operations, role-aware UI, API docs.

## 5. Rencana Implementasi

### 5.1 High Priority
1. **Module Config Preservation**
   - Backend: `services/module/internal/repository/repository.go:119-140` normalisasi `config` kosong jadi `{}` pada update.
   - Frontend: `services/dashboard/static/js/modules.js:508-513` kirim config hanya jika diubah, atau tambahkan editor JSON + validasi sebelum submit.
   - Acceptance: Edit modul tidak lagi menghapus config yang sudah ada; config invalid ditolak.

2. **Auto-Detect Source Key Valid**
   - Frontend: `services/dashboard/static/js/modules.js:195-259` filter hasil `flattenObject` hanya leaf scalar (angka/bool), drop objek dan array.
   - Backend: tambahkan validasi `source_key` pada `POST /tags` untuk menolak path yang tidak bisa di-resolve atau non-numeric.
   - Acceptance: Auto-detect hanya menampilkan kunci yang bisa diingest.

3. **Live Telemetry Timestamp**
   - Frontend: `modules.js:417-432` parsing `timestamp`/`ts` dari payload Redis sebagai sumber waktu; fallback ke browser hanya jika absent.
   - Acceptance: “Update” menampilkan waktu sebenarnya dari telemetri.

4. **Orphan Tag saat Delete Node**
   - Backend: `services/module/internal/repository/repository.go:437-445` tambahkan penghapusan tag terkait sebelum/bersamaan hapus node, atau Foreign Key cascade.
   - Acceptance: Menghapus node membersihkan tag-nya.

5. **Token Refresh di Module Calls**
   - Frontend: `services/dashboard/static/js/research.js:190-214` gunakan `ensureValidToken()` seperti `fetchJSON` sebelum memanggil module API.
   - Acceptance: Sesi tidak terputus saat edit module/node/tag selama token kadaluarsa.

### 5.2 Medium Priority
6. **Edit Tag + Enable/Disable**
   - Backend: pertimbangkan `PUT /nodes/{node_id}/tags` atau tambah `PATCH /nodes/{node_id}/tags/{id}` untuk update field tag; ekspos `enabled`.
   - Frontend: modal edit tag, toggle Aktif/Nonaktif, simpan via upsert.
   - Acceptance: Tag bisa diedit dan dinonaktifkan tanpa hapus/buat ulang.

7. **Detail Node & Module**
   - Backend: sudah ada `GET /modules/{id}` dan `GET /nodes/{node_id}`.
   - Frontend: modal detail menampilkan metadata + tag + telemetry terbaru.
   - Acceptance: Klik kartu modul / baris node membuka detail.

8. **Server-Side Filter + Pagination**
   - Backend: tetap gunakan query params `paired`, `module_id`, `status`, tambah `page`, `limit`.
   - Frontend: ganti filter lokal dengan query string; tampilkan pagination.
   - Acceptance: Skala ribuan node tidak memuat seluruh data di browser.

9. **Module Health di System Dashboard**
   - Dashboard: `services/dashboard/app.py:554-593` tambahkan `module` ke daftar RELEVANT.
   - Dashboard: `services/dashboard/app.py:630-705` tambahkan sumber validitas Redis, MQTT, NATS, TimescaleDB.
   - Acceptance: Halaman modules menampilkan status kesehatan service terkait.

10. **Role-Aware Controls**
    - Backend: ekspos role user via `/auth/me` (sudah ada).
    - Frontend: sembunyikan/disable aksi write jika role bukan admin/operator.
    - Acceptance: User non-admin melihat module tapi tombol write disembunyikan.

### 5.3 Low Priority
11. **Telemetry History + Export**
    - Hubungkan halaman modules ke Export Service (`/api/v1/export/telemetry`) untuk riwayat node.
    - Tampilkan chart & tombol export CSV per node.
    - Acceptance: Bisa melihat dan mengunduh histori telemetri node.

12. **Actuator Execution**
    - Backend: tambahkan endpoint perintah aktutor (mis. `POST /nodes/{node_id}/actuators/{id}/command`) yang publish ke MQTT/NATS.
    - Frontend: form eksekusi aktuator + feedback status.
    - Acceptance: Ada alur kontrol aktuator end-to-end.

13. **Audit/Outbox Viewer**
    - Backend: endpoint baca audit/outbox untuk module service.
    - Frontend: panel event log sederhana.
    - Acceptance: Admin bisa melihat riwayat perubahan.

14. **Bulk Operations**
    - Backend: bulk pair/unpair, bulk tag replace.
    - Frontend: checkbox selection + action massal.
    - Acceptance: Onboarding banyak node menjadi satu langkah.

15. **API Docs & Duplicate Partial**
    - Konsolidasikan partial HTML menjadi satu sumber (`static/partials/modules.html`).
    - Dokumentasikan proxy path di OpenAPI dashboard atau buat spesifikasi gabungan.
    - Acceptance: Single source of truth untuk partial dan docs.

## 6. Validasi & Testing
- Unit test backend untuk perubahan repo/service (config normalize, delete tag cascade, validasi source_key).
- Manual smoke test module page: create/edit/delete modul, pair/unpair node, live telemetry, tag sensor/aktuator.
- Auth test: expired token memicu refresh sebelum module call.
- Performance test: filter + pagination dengan dataset besar.
- Backend: jalankan `cd services/module && go test -v ./...`.

## 7. Risiko & Catatan
- **Backward compatibility**: perubahan endpoint atau response shape harus tetap kompatibel dengan module UI yang ada.
- **Kepercayaan telemetry**: live raw payload tidak bisa jadi source of truth untuk timestamp sampai payload `timestamp` dibaca.
- **Keamanan**: pastikan role check konsisten; hindari expose metric/health tanpa auth jika production.
- **Maintainability**: hindari duplikasi partial/template yang menyebabkan drift.
