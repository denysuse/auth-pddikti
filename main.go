package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"auth-pddikti/internal/pddikti"
)


// validasiNIM: NIM hanya boleh digit, panjang wajar 5-25 karakter.
// (Perbaikan audit Lemper: sebelumnya input tidak divalidasi sama sekali)
func validasiNIM(nim string) bool {
	if len(nim) < 5 || len(nim) > 25 {
		return false
	}
	for _, r := range nim {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// pesanGalatUmum tidak meneruskan pesan error internal/upstream mentah ke
// pemanggil (kode status upstream, tantangan WAF, dsb. hanya untuk log
// server). (Perbaikan audit Lemper)
func pesanGalatUmum(c *gin.Context, err error, status int, pesan string) {
	log.Printf("error: %v", err)
	c.JSON(status, gin.H{"error": pesan})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8090"
	}

	client := pddikti.NewClient(10 * time.Second)

	// gin.New() + Recovery() saja: Logger bawaan gin.Default() mencatat
	// path lengkap termasuk NIM pada /api/verify/:nim ke stdout.
	// Data pribadi tidak boleh masuk log. (Perbaikan audit Lemper)
	router := gin.New()
	router.Use(gin.Recovery())

	// Batas laju lokal: maks 1 request/detik, jauh di bawah batas upstream
	// 60 req/menit per IP. (Perbaikan audit Lemper)
	limiter := rate.NewLimiter(rate.Limit(1), 1)

	// Kunci API opsional lewat env API_KEY. WAJIB diisi bila server dibuka
	// ke jaringan; kosong = hanya aman untuk pemakaian lokal tertutup.
	apiKey := os.Getenv("API_KEY")

	// CORS hanya untuk origin milik sendiri lewat env ALLOWED_ORIGIN
	// (boleh beberapa, pisahkan koma). Kosong = tidak mengizinkan origin
	// lintas-situs mana pun (bukan bintang lagi).
	allowedOrigins := map[string]bool{}
	for _, o := range strings.Split(os.Getenv("ALLOWED_ORIGIN"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			allowedOrigins[o] = true
		}
	}

	router.Use(func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && allowedOrigins[origin] {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Vary", "Origin")
			c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
		}
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		// /health selalu bebas (pemeriksaan ketersediaan)
		if c.Request.URL.Path == "/health" {
			c.Next()
			return
		}
		if !limiter.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "terlalu banyak permintaan, coba lagi sebentar"})
			return
		}
		if apiKey != "" && c.GetHeader("X-API-Key") != apiKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	})

	// Health Check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": "auth-pddikti",
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	// 1. Search student (by NIM or Name) -> returns candidate array
	// GET /api/search?q=akbar
	// Nonaktif secara bawaan (potensi enumerasi data pribadi). Aktifkan
	// hanya bila benar-benar perlu: ENABLE_SEARCH=1. (Audit Lemper)
	if os.Getenv("ENABLE_SEARCH") == "1" {
		router.GET("/api/search", func(c *gin.Context) {
		query := c.Query("q")
		if strings.TrimSpace(query) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Query parameter 'q' is required"})
			return
		}

		results, err := client.Search(query)
		if err != nil {
			pesanGalatUmum(c, err, http.StatusBadGateway, "layanan PDDIKTI tidak dapat dihubungi saat ini")
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"query":  query,
			"total":  len(results),
			"data":   results,
		})
		})
	}

	// 2. Get full student detail by encrypted ID
	// GET /api/detail/:id
	router.GET("/api/detail/:id", func(c *gin.Context) {
		id := c.Param("id")
		detail, err := client.GetDetail(id)
		if err != nil {
			pesanGalatUmum(c, err, http.StatusBadGateway, "layanan PDDIKTI tidak dapat dihubungi saat ini")
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   detail,
		})
	})

	// 3. Instant Verification & Auto-fill by NIM (All-In-One Bridge)
	// GET /api/verify/:nim
	// POST /api/verify {"nim": "23010000001"}
	handleVerify := func(c *gin.Context, nim string) {
		nim = strings.TrimSpace(nim)
		if nim == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "NIM is required"})
			return
		}
		if !validasiNIM(nim) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "format NIM tidak valid"})
			return
		}

		detail, err := client.VerifyNIM(nim)
		if err != nil {
			pesanGalatUmum(c, err, http.StatusNotFound, "mahasiswa tidak ditemukan")
			return
		}

		// Check active status
		isActive := strings.HasPrefix(strings.ToLower(detail.StatusSaatIni), "aktif")

		c.JSON(http.StatusOK, gin.H{
			"status":    "success",
			"verified":  true,
			"is_active": isActive,
			"student": gin.H{
				"nim":            detail.NIM,
				"nama":           detail.Nama,
				"kampus":         detail.NamaPT,
				"kode_pt":        strings.TrimSpace(detail.KodePT),
				"prodi":          detail.Prodi,
				"jenjang":        detail.Jenjang,
				"jenis_kelamin":  detail.JenisKelamin,
				"status_akademik": detail.StatusSaatIni,
				"tanggal_masuk":  detail.TanggalMasuk,
			},
		})
	}

	router.GET("/api/verify/:nim", func(c *gin.Context) {
		handleVerify(c, c.Param("nim"))
	})

	router.POST("/api/verify", func(c *gin.Context) {
		var req struct {
			NIM string `json:"nim" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "JSON body with field 'nim' is required"})
			return
		}
		handleVerify(c, req.NIM)
	})

	// Bawaan: hanya localhost. Membuka ke jaringan harus EKSPLISIT lewat
	// env HOST=0.0.0.0 dan sangat dianjurkan bersama API_KEY.
	// (Perbaikan audit Lemper: sebelumnya listen di semua antarmuka)
	host := os.Getenv("HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	log.Printf("[+] auth-pddikti service running on http://%s:%s", host, port)
	if err := router.Run(host + ":" + port); err != nil {
		log.Fatalf("[-] Failed to run server: %v", err)
	}
}
