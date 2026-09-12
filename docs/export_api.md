# SmartGrid Export API

Dokumentasi untuk membaca dan mengekspor data telemetry historis dari TimescaleDB melalui Export Service.

## 1. Base URL

Pada deployment VPS saat ini, akses API melalui Nginx:

```text
http://167.205.44.103:3001
```

Endpoint Export Service menggunakan prefix versioning `/api/v1/export`:

```text
http://smartgrid.almuzky.my.id:3001/api/v1/export
```

Port `8080` adalah port internal container dan tidak digunakan oleh client dari luar Docker.

> Ganti alamat IP pada contoh jika deployment menggunakan domain atau IP berbeda.

## 2. Prasyarat akses

Endpoint export membutuhkan access token JWT dari Auth Service. User harus memiliki salah satu role berikut:

- `admin`
- `operator`

Header yang wajib dikirim:

```http
Authorization: Bearer ACCESS_TOKEN
```

## 3. Mendapatkan access token

Login melalui Auth Service:

```bash
curl -sS -X POST \
  "http://smartgrid.almuzky.my.id:3001/api/v1/auth/login" \
  -H "Content-Type: application/json" \
  -d '{
    "identifier": "USERNAME_ATAU_EMAIL",
    "password": "PASSWORD"
  }'
```

Respons berhasil berisi token pair:

```json
{
  "access_token": "eyJ...",
  "refresh_token": "...",
  "expires_in": 900
}
```

Gunakan nilai `access_token` pada request export. Jangan menyimpan token di repository atau membagikannya di log publik.

## 4. Health check

Endpoint ini tidak membutuhkan token:

```bash
curl -i "http://smartgrid.almuzky.my.id:3001/api/v1/export/health"
```

Respons normal:

```json
{
  "success": true,
  "data": {
    "status": "ok"
  }
}
```

## 5. Melihat node dan metric tersedia

Endpoint ini menampilkan node yang memiliki data telemetry beserta metric yang pernah tersimpan:

```bash
curl -sS \
  -H "Authorization: Bearer ACCESS_TOKEN" \
  "http://smartgrid.almuzky.my.id:3001/api/v1/export/nodes"
```

Contoh respons:

```json
{
  "success": true,
  "data": {
    "nodes": [
      {
        "node_id": "SmartGrid-01",
        "module_id": "d73f79bc-0757-4c42-8534-709ff9762a32",
        "metrics": ["reg504", "reg509"]
      }
    ]
  }
}
```

Gunakan nilai `node_id` dan `metrics` dari respons ini untuk menyusun query telemetry.

## 6. Preview metadata

`/api/v1/export/meta` menghitung jumlah data tanpa mengunduh baris telemetry.

```bash
curl -sS -G \
  -H "Authorization: Bearer ACCESS_TOKEN" \
  --data-urlencode "node_id=SmartGrid-01" \
  --data-urlencode "metric=reg504" \
  --data-urlencode "from=2026-09-12T00:00:00Z" \
  --data-urlencode "to=2026-09-12T23:59:59Z" \
  "http://smartgrid.almuzky.my.id:3001/api/v1/export/meta"
```

Contoh respons:

```json
{
  "success": true,
  "data": {
    "node_ids": ["SmartGrid-01"],
    "metrics": ["reg504"],
    "from": "2026-09-12T00:00:00Z",
    "to": "2026-09-12T23:59:59Z",
    "total": 120
  }
}
```

## 7. Query telemetry sebagai JSON

Endpoint utama adalah `/api/v1/export/telemetry`.

```bash
curl -sS -G \
  -H "Authorization: Bearer ACCESS_TOKEN" \
  --data-urlencode "node_id=SmartGrid-01" \
  --data-urlencode "metric=reg504" \
  --data-urlencode "from=2026-09-12T00:00:00Z" \
  --data-urlencode "to=2026-09-12T23:59:59Z" \
  --data-urlencode "format=json" \
  --data-urlencode "limit=100" \
  "http://smartgrid.almuzky.my.id:3001/api/v1/export/telemetry"
```

Contoh respons:

```json
{
  "success": true,
  "data": {
    "rows": [
      {
        "time": "2026-09-12T07:13:51.228871Z",
        "node_id": "SmartGrid-01",
        "module_id": null,
        "metric": "reg504",
        "value": 51.79999924
      }
    ],
    "total": 1,
    "has_more": false,
    "next_cursor": ""
  }
}
```

Parameter query:

| Parameter | Wajib | Keterangan |
|---|---:|---|
| `node_id` | Ya | Satu node, beberapa node dipisah koma, atau `*` untuk semua node. |
| `metric` | Tidak | Satu metric, beberapa metric dipisah koma, atau `*` untuk semua metric. Default `*`. |
| `from` | Tidak | Waktu awal: RFC3339, `YYYY-MM-DD`, atau Unix timestamp detik. Default 24 jam terakhir. |
| `to` | Tidak | Waktu akhir: RFC3339, `YYYY-MM-DD`, atau Unix timestamp detik. Default waktu sekarang. |
| `format` | Tidak | `json` atau `csv`. Default `csv`. |
| `limit` | Tidak | Jumlah baris per halaman. Default `10000`, maksimum `100000`. |
| `cursor` | Tidak | Cursor dari respons halaman sebelumnya. |

Metric harus menggunakan nama yang tersimpan di kolom `metric`, misalnya `reg504`, bukan nama `source_key` MQTT jika keduanya berbeda.

## 8. Query beberapa node atau metric

```bash
curl -sS -G \
  -H "Authorization: Bearer ACCESS_TOKEN" \
  --data-urlencode "node_id=SmartGrid-01,SmartGrid-02" \
  --data-urlencode "metric=reg504,reg509" \
  --data-urlencode "format=json" \
  "http://smartgrid.almuzky.my.id:3001/api/v1/export/telemetry"
```

Semua metric pada satu node:

```bash
curl -sS -G \
  -H "Authorization: Bearer ACCESS_TOKEN" \
  --data-urlencode "node_id=SmartGrid-01" \
  --data-urlencode "metric=*" \
  --data-urlencode "format=json" \
  "http://smartgrid.almuzky.my.id:3001/api/v1/export/telemetry"
```

Semua node dan semua metric:

```bash
curl -sS -G \
  -H "Authorization: Bearer ACCESS_TOKEN" \
  --data-urlencode "node_id=*" \
  --data-urlencode "metric=*" \
  --data-urlencode "format=json" \
  "http://smartgrid.almuzky.my.id:3001/api/v1/export/telemetry"
```

## 9. Pagination dengan cursor

Jika respons memiliki:

```json
{
  "has_more": true,
  "next_cursor": "CURSOR_TOKEN"
}
```

Kirim `next_cursor` sebagai parameter `cursor` pada request berikutnya. Filter `node_id`, `metric`, `from`, dan `to` harus tetap sama.

```bash
curl -sS -G \
  -H "Authorization: Bearer ACCESS_TOKEN" \
  --data-urlencode "node_id=SmartGrid-01" \
  --data-urlencode "metric=reg504" \
  --data-urlencode "from=2026-09-12T00:00:00Z" \
  --data-urlencode "to=2026-09-12T23:59:59Z" \
  --data-urlencode "format=json" \
  --data-urlencode "limit=100" \
  --data-urlencode "cursor=CURSOR_TOKEN" \
  "http://smartgrid.almuzky.my.id:3001/api/v1/export/telemetry"
```

Untuk format JSON, cursor berada di `data.next_cursor`. Untuk format CSV, cursor berada pada response header `X-Export-Next-Cursor`.

## 10. Download telemetry sebagai CSV

Format CSV adalah format default dan dikembalikan sebagai file attachment:

```bash
curl -sS -G \
  -H "Authorization: Bearer ACCESS_TOKEN" \
  --data-urlencode "node_id=SmartGrid-01" \
  --data-urlencode "metric=reg504" \
  --data-urlencode "from=2026-09-12T00:00:00Z" \
  --data-urlencode "to=2026-09-12T23:59:59Z" \
  --data-urlencode "format=csv" \
  -o telemetry.csv \
  "http://smartgrid.almuzky.my.id:3001/api/v1/export/telemetry"
```

CSV memiliki bentuk wide table. Contoh:

```csv
time,node_id,module_id,reg504
2026-09-12T07:13:51.228871Z,SmartGrid-01,,51.79999924
```

Periksa header cursor jika data lebih banyak daripada halaman saat ini:

```bash
curl -sS -D headers.txt -G \
  -H "Authorization: Bearer ACCESS_TOKEN" \
  --data-urlencode "node_id=SmartGrid-01" \
  --data-urlencode "format=csv" \
  -o telemetry.csv \
  "http://smartgrid.almuzky.my.id:3001/api/v1/export/telemetry"

grep -i "X-Export-Next-Cursor" headers.txt
```

## 11. Format waktu

Format yang didukung:

```text
2026-09-12T07:00:00Z       RFC3339
2026-09-12                 tanggal UTC
1789196400                 Unix timestamp dalam detik
```

Rentang export maksimum adalah **366 hari**. Untuk hasil yang konsisten, gunakan RFC3339 dengan timezone UTC (`Z`).

## 12. Error umum

### 400 Bad Request

Parameter tidak valid, `node_id` tidak diisi, atau rentang waktu melebihi 366 hari.

```json
{
  "success": false,
  "error": {
    "code": "BAD_REQUEST",
    "message": "node_id is required"
  }
}
```

### 401 Unauthorized

Token tidak ada, tidak valid, atau sudah kedaluwarsa.

```json
{
  "success": false,
  "error": {
    "code": "UNAUTHORIZED",
    "message": "missing or invalid Authorization header"
  }
}
```

### 403 Forbidden

JWT valid, tetapi role user bukan `admin` atau `operator`.

```json
{
  "success": false,
  "error": {
    "code": "FORBIDDEN",
    "message": "forbidden: insufficient role"
  }
}
```

### 500 Internal Server Error

Export Service gagal membaca TimescaleDB atau mengalami error internal. Periksa log container:

```bash
docker logs --tail 100 smartgrid-export
```

## 13. Query langsung untuk membandingkan hasil API

API membaca tabel hypertable `public.telemetry` pada database `module_ts`. Query langsung dari PostgreSQL:

```bash
docker exec -it smartgrid-timescaledb-module \
  psql -U module_user -d module_ts \
  -c "SELECT time, node_id, module_id, metric, value FROM telemetry ORDER BY time DESC LIMIT 20;"
```

Query API dan query SQL dapat menampilkan jumlah berbeda jika filter node, metric, atau rentang waktu yang digunakan berbeda.

## 14. OpenAPI

Spesifikasi OpenAPI dapat diakses tanpa token:

```bash
curl -sS "http://smartgrid.almuzky.my.id:3001/api/v1/export/openapi"
```

Kontrak sumber berada di:

```text
services/export/openapi.yaml
```
