# auth-pddikti

Microservice bridge dan HTTP client Go untuk verifikasi data mahasiswa Indonesia berbasis portal PDDIKTI (Kemdiktisaintek RI).

Layanan ini dirancang khusus untuk memvalidasi keaktifan mahasiswa, mengambil identitas akademik resmi (nama lengkap, NIM, perguruan tinggi, program studi, jenjang, status semester), dan melakukan pencarian sivitas akademika secara on-demand tanpa proses penambangan data massal.

## Karakteristik Teknis

- On-demand single lookup: Menghindari beban scraping skala besar dengan mengeksekusi request langsung saat user mendaftar.
- Header compliance: Menyertakan header origin, referer, dan user-agent peramban untuk mencegah blokir HTTP 403 dari WAF upstream.
- Deterministic match: Pencarian berbasis NIM langsung memetakan satu mahasiswa spesifik secara presisi.
- Cross-origin resource sharing: Mendukung integrasi langsung dari antarmuka web maupun mobile.
- Cakupan nasional: Berlaku untuk seluruh perguruan tinggi negeri, swasta, dan politeknik di Indonesia.

## Persyaratan

- Go 1.27 atau lebih baru

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

Contoh response (200 OK):

```json
{
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

### 2. Pencarian Kandidat Mahasiswa (Berdasarkan Nama atau NIM)

Mengembalikan daftar mahasiswa yang cocok dengan kata kunci pencarian.

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

## Batasan Operasional Upstream

- Rate limit upstream PDDIKTI berada pada batas 60 request per menit per alamat IP publik.
- Layanan ini diperuntukkan bagi verifikasi alur registrasi/autentikasi pengguna secara bertahap, bukan untuk penarikan data massal (bulk harvesting).


## Catatan Fork (denysuse/auth-pddikti)

Fork ini menerapkan perbaikan dari audit penuh tim kami atas repo asal
(akbarhann/auth-pddikti, audit 8 Nov 2026, vonis: LAYAK DENGAN CATATAN).
Repo asal tidak berlisensi; fork ini untuk pemakaian pribadi sambil
menunggu izin tertulis dari pemilik repo asal.

Perbaikan yang diterapkan:
1. **Bug verifikasi salah orang diperbaiki.** Sebelumnya, bila NIM tidak
   cocok persis, kode mengambil kandidat pertama hasil pencarian dan
   mengembalikannya sebagai terverifikasi. Sekarang: tidak cocok persis =
   tidak ditemukan (404).
2. **Server hanya mendengarkan 127.0.0.1 secara bawaan.** Membuka ke
   jaringan harus eksplisit: `HOST=0.0.0.0`, dan sangat dianjurkan bersama
   `API_KEY`.
3. **Kunci API opsional** lewat env `API_KEY`, diperiksa dari header
   `X-API-Key` pada semua endpoint kecuali `/health`.
4. **Batas laju lokal** 1 request/detik, jauh di bawah batas upstream
   60 req/menit per IP.
5. **CORS tidak lagi bintang.** Hanya origin yang didaftarkan lewat env
   `ALLOWED_ORIGIN` (pisahkan koma) yang diizinkan.
6. **Log tidak lagi mencatat NIM.** Logger bawaan yang mencatat path
   lengkap diganti `gin.New()` + Recovery saja.
7. **Validasi NIM**: hanya digit, panjang 5-25, sebelum diteruskan ke
   upstream.
8. **`/api/search` nonaktif secara bawaan** (risiko enumerasi data
   pribadi). Aktifkan dengan `ENABLE_SEARCH=1` hanya bila perlu.
9. **Pesan error ke pemanggil digeneralisasi**; detail internal/upstream
   hanya masuk log server.
10. **Dependensi dibersihkan** dengan `go mod tidy` (mongo-driver, quic-go,
    protobuf yang tidak dipakai dibuang) dan syarat versi Go diselaraskan
    dengan README.

Contoh menjalankan dengan pengaman:

```
API_KEY=isi-kunci-sendiri ALLOWED_ORIGIN=http://localhost:3000 go run main.go
```

Peringatan pemakaian: data yang diambil adalah data pribadi mahasiswa.
Gunakan hanya untuk memverifikasi orang yang mendaftar ke layananmu
sendiri, dengan dasar pemrosesan yang sah menurut UU No. 27 Tahun 2022
(Pelindungan Data Pribadi), tampilkan seminimal mungkin, dan jangan
menyimpan hasil lookup.
