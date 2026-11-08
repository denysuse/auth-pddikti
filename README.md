> **Fork dari [akbarhann/auth-pddikti](https://github.com/akbarhann/auth-pddikti) (MIT).**
> Disinkronkan ke upstream per 8 Nov 2026, termasuk perbaikan bug verifikasi
> NIM oleh pembuatnya dan lisensi MIT. Perbedaan fork ini hanya pada bawaan
> yang lebih ketat:
>
> - Bind bawaan `127.0.0.1` (upstream: semua antarmuka). Atur `HOST=0.0.0.0` hanya bila perlu.
> - `/api/search` nonaktif secara bawaan; aktifkan dengan `ENABLE_SEARCH=1`.
> - Access log gin dimatikan: path `/api/verify/:nim` dan query pencarian memuat data pribadi, tidak boleh tercatat di log.
>
> Sisanya identik dengan upstream. Terima kasih kepada pembuat aslinya.

# auth-pddikti

Microservice bridge dan HTTP client Go untuk verifikasi identitas dan status aktif mahasiswa Indonesia berbasis PDDIKTI (Kemdiktisaintek RI).

Layanan ini berfungsi sebagai lapisan verifikasi data (student verification / e-KYC layer) yang dirancang untuk melengkapi alur autentikasi aplikasi utama. Sistem memvalidasi keaktifan akademik, menarik identitas resmi (nama lengkap, NIM, perguruan tinggi, program studi, jenjang, status semester), dan menyediakan pencarian sivitas akademika secara on-demand tanpa scraping data massal

## Karakteristik Teknis

- On-demand single lookup: Menghindari beban scraping skala besar dengan mengeksekusi request langsung saat user mendaftar
- Role & eligibility verification: Memvalidasi keabsahan status mahasiswa aktif sebelum hak akses atau role diberikan oleh sistem utama
- Header compliance: Menyertakan header origin, referer, dan user-agent peramban untuk mencegah blokir HTTP 403 dari WAF upstream
- Deterministic match: Pencarian berbasis NIM memetakan satu mahasiswa spesifik secara presisi
- Cross-origin resource sharing: Mendukung integrasi langsung dari antarmuka web maupun mobile
- Cakupan nasional: Berlaku untuk seluruh perguruan tinggi negeri, swasta, dan politeknik di Indonesia

## Catatan Arsitektur

Layanan ini melakukan verifikasi data terhadap pangkalan data nasional secara read-only. Untuk menegakkan kepemilikan akun (identity ownership) secara mutlak, layanan ini direkomendasikan untuk dipasangkan bersama mekanisme autentikasi utama seperti Google Workspace OAuth institusi (`@kampus.ac.id`) atau verifikasi email kampus

## Persyaratan

- Go 1.26 atau lebih baru

## Menjalankan Layanan

```bash
git clone https://github.com/akbarhann/auth-pddikti.git
cd auth-pddikti
go mod tidy
go run main.go
```

Secara default layanan berjalan pada port 8090. Port dapat disesuaikan melalui environment variable:

```bash
PORT=8090 go run main.go
```

### Konfigurasi

Semua variabel opsional. Contoh lengkap ada di [.env.example](.env.example).

| Variabel | Default | Keterangan |
|---|---|---|
| `PORT` | `8090` | Port HTTP |
| `API_KEY` | kosong | Jika diisi, semua endpoint `/api/*` wajib menyertakan header `X-API-Key` |
| `CORS_ORIGINS` | kosong (`*`) | Daftar origin yang diizinkan, dipisah koma |
| `RATE_LIMIT_PER_MINUTE` | `30` | Batas request `/api/*` per IP klien per menit (`0` = nonaktif) |
| `SEARCH_MIN_QUERY_LEN` | `3` | Panjang minimal parameter `q` pada `/api/search` |
| `TRUSTED_PROXIES` | kosong | IP/CIDR reverse proxy yang dipercaya untuk `X-Forwarded-For`. Isi jika layanan berada di balik proxy, agar rate limit membaca IP klien yang benar |

## Testing

Unit test memakai upstream tiruan (`httptest`) dan data fiktif, sehingga tidak mengirim request ke PDDIKTI:

```bash
go test -race -cover ./...
```

Test live terhadap PDDIKTI asli dipisahkan dengan build tag `live`. Data uji diberikan lewat environment variable agar data pribadi tidak pernah masuk ke repository:

```bash
PDDIKTI_TEST_NIM=<nim-anda> go test -tags=live -v ./internal/pddikti/
```

## Spesifikasi API

### 1. Verifikasi dan Pengambilan Data Mahasiswa (Berdasarkan NIM)

Mengambil data mahasiswa secara langsung dan mengembalikan status keaktifan semester berjalan.

- Method: GET atau POST
- Endpoint: `/api/verify/:nim` atau `/api/verify`

Contoh request GET:

```bash
curl -s http://localhost:8090/api/verify/23010000001
```

Contoh request POST:

```bash
curl -s -X POST http://localhost:8090/api/verify \
  -H "Content-Type: application/json" \
  -d '{"nim": "23010000001"}'
```

NIM harus cocok persis. NIM yang terpotong atau mirip tidak akan pernah dicocokkan ke mahasiswa lain. Karena NIM tidak unik secara nasional, sertakan parameter opsional `kampus` (nama atau singkatan perguruan tinggi, tidak case-sensitive) bila NIM yang sama terdaftar di lebih dari satu kampus:

```bash
curl -s "http://localhost:8090/api/verify/23010000001?kampus=UCI"
curl -s -X POST http://localhost:8090/api/verify \
  -H "Content-Type: application/json" \
  -d '{"nim": "23010000001", "kampus": "Universitas Contoh Indonesia"}'
```

Contoh response (200 OK):

```json
{
  "eligible": true,
  "is_active": true,
  "status": "success",
  "student": {
    "jenis_kelamin": "L",
    "jenjang": "Sarjana",
    "kampus": "Universitas Contoh Indonesia",
    "kode_pt": "001001",
    "nama": "CONTOH MAHASISWA",
    "nim": "23010000001",
    "prodi": "Teknik Informatika",
    "status_akademik": "Aktif-2026/2027 Ganjil",
    "tanggal_masuk": "2023-08-15"
  },
  "verified": true
}
```

> **Penting:** `verified: true` hanya berarti NIM ditemukan di PDDIKTI, termasuk mahasiswa yang sudah lulus, cuti, atau dikeluarkan. Gunakan `eligible` (bernilai `true` hanya jika status akademik `Aktif`) untuk memberikan hak akses atau role.

Contoh response bila NIM terdaftar di lebih dari satu kampus (409 Conflict):

```json
{
  "candidates": [
    { "nama_pt": "UNIVERSITAS CONTOH INDONESIA", "nama_prodi": "TEKNIK INFORMATIKA", "singkatan_pt": "UCI" },
    { "nama_pt": "POLITEKNIK CONTOH", "nama_prodi": "TEKNIK MESIN", "singkatan_pt": "PC" }
  ],
  "code": "ambiguous_nim",
  "error": "NIM terdaftar di lebih dari satu perguruan tinggi; sertakan parameter 'kampus'",
  "status": "error"
}
```

### 2. Pencarian Kandidat Mahasiswa (Berdasarkan Nama atau NIM)

Mengembalikan daftar mahasiswa yang cocok dengan kata kunci pencarian

- Method: GET
- Endpoint: `/api/search?q={keyword}`

Contoh request:

```bash
curl -s "http://localhost:8090/api/search?q=budi%20santoso"
```

Contoh response (200 OK):

```json
{
  "data": [
    {
      "id": "sample_encrypted_id_hash_value==",
      "nama": "BUDI SANTOSO",
      "nama_prodi": "TEKNIK INFORMATIKA",
      "nama_pt": "UNIVERSITAS CONTOH INDONESIA",
      "nim": "23010000001",
      "sinkatan_pt": "UCI"
    }
  ],
  "query": "budi santoso",
  "status": "success",
  "total": 1
}
```

### 3. Detail Mahasiswa Berdasarkan ID Terenkripsi

- Method: GET
- Endpoint: `/api/detail/:id`

Contoh request:

```bash
curl -s "http://localhost:8090/api/detail/sample_encrypted_id_hash_value=="
```

## Format Error

Semua error memakai bentuk `{"status": "error", "code": "<kode>", "error": "<pesan>"}`. Gunakan `code` untuk logika di aplikasi klien, karena isi `error` bisa berubah.

| HTTP | `code` | Arti |
|---|---|---|
| 400 | `bad_request` | Parameter tidak lengkap atau `q` terlalu pendek |
| 400 | `invalid_id` | ID terenkripsi tidak valid |
| 401 | `unauthorized` | `API_KEY` aktif dan header `X-API-Key` salah atau tidak ada |
| 404 | `not_found` | Tidak ada mahasiswa dengan NIM yang cocok persis (dan kampus, bila diisi) |
| 409 | `ambiguous_nim` | NIM terdaftar di lebih dari satu kampus. Ulangi dengan parameter `kampus` |
| 429 | `rate_limited` | Batas request per IP pada layanan ini terlampaui |
| 429 | `upstream_rate_limited` | Batas 60 request/menit PDDIKTI terlampaui |
| 502 | `upstream_blocked` / `upstream_error` | PDDIKTI menolak request atau merespons tidak valid |
| 504 | `upstream_timeout` | PDDIKTI tidak merespons |

## Batasan Operasional Upstream

- Rate limit upstream PDDIKTI berada pada batas 60 request per menit per alamat IP publik
- Layanan ini diperuntukkan bagi verifikasi alur registrasi/autentikasi pengguna secara bertahap, bukan untuk penarikan data massal (bulk harvesting)

## Lisensi

Dirilis di bawah [MIT License](LICENSE).
