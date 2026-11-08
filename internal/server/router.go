// Package server wires the HTTP API on top of the PDDIKTI client.
package server

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"auth-pddikti/internal/pddikti"
)

// Config controls the API's protection settings. The zero value is valid:
// no API key, CORS for any origin, no rate limit, no minimum query length.
type Config struct {
	// APIKey, when set, must be sent in the X-API-Key header on /api/*.
	APIKey string
	// CORSOrigins lists allowed origins. Empty or containing "*" allows any.
	CORSOrigins []string
	// RatePerMinute limits /api/* requests per client IP. 0 disables it.
	RatePerMinute int
	// MinQueryLen is the minimum length (in characters) of /api/search?q=.
	MinQueryLen int
	// TrustedProxies are proxies whose X-Forwarded-For is believed when
	// resolving the client IP. Empty trusts none.
	TrustedProxies []string
	// EnableSearch registers the open-ended /api/search endpoint. It is off
	// unless explicitly enabled: search returns candidate lists for arbitrary
	// keywords, which is broader exposure than single-NIM verification.
	EnableSearch bool
}

// Searcher is the subset of *pddikti.Client the API needs.
type Searcher interface {
	Search(keyword string) ([]pddikti.StudentSearchResult, error)
	GetDetail(id string) (*pddikti.StudentDetail, error)
	VerifyNIM(nim, campus string) (*pddikti.StudentDetail, error)
}

func NewRouter(client Searcher, cfg Config) (*gin.Engine, error) {
	r := gin.New()
	// No gin.Logger(): the default access log records the full request path,
	// which contains the NIM on /api/verify/:nim and the keyword on
	// /api/search?q= -- personal data must not land in logs.
	r.Use(gin.Recovery())
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		return nil, err
	}

	r.Use(corsMiddleware(cfg.CORSOrigins))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "healthy",
			"service": "auth-pddikti",
			"time":    time.Now().Format(time.RFC3339),
		})
	})

	api := r.Group("/api")
	if cfg.APIKey != "" {
		api.Use(apiKeyMiddleware(cfg.APIKey))
	}
	if cfg.RatePerMinute > 0 {
		api.Use(newIPRateLimiter(cfg.RatePerMinute).middleware())
	}

	// GET /api/search?q=keyword -> candidate list (only when EnableSearch)
	if cfg.EnableSearch {
		api.GET("/search", func(c *gin.Context) {
			query := strings.TrimSpace(c.Query("q"))
			if query == "" {
				badRequest(c, "Query parameter 'q' is required")
				return
			}
			if utf8.RuneCountInString(query) < cfg.MinQueryLen {
				badRequest(c, "Query parameter 'q' terlalu pendek")
				return
			}

			results, err := client.Search(query)
			if err != nil {
				writeError(c, err)
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

	// GET /api/detail/:id -> full detail by encrypted ID
	api.GET("/detail/:id", func(c *gin.Context) {
		detail, err := client.GetDetail(c.Param("id"))
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "success", "data": detail})
	})

	// GET /api/verify/:nim?kampus=...  |  POST /api/verify {"nim": "...", "kampus": "..."}
	handleVerify := func(c *gin.Context, nim, campus string) {
		nim = strings.TrimSpace(nim)
		if nim == "" {
			badRequest(c, "NIM is required")
			return
		}

		detail, err := client.VerifyNIM(nim, campus)
		if err != nil {
			writeError(c, err)
			return
		}

		isActive := detail.IsActive()
		c.JSON(http.StatusOK, gin.H{
			"status":    "success",
			"verified":  true,
			"is_active": isActive,
			// eligible is what callers should gate access on.
			"eligible": isActive,
			"student": gin.H{
				"nim":             detail.NIM,
				"nama":            detail.Nama,
				"kampus":          detail.NamaPT,
				"kode_pt":         strings.TrimSpace(detail.KodePT),
				"prodi":           detail.Prodi,
				"jenjang":         detail.Jenjang,
				"jenis_kelamin":   detail.JenisKelamin,
				"status_akademik": detail.StatusSaatIni,
				"tanggal_masuk":   detail.TanggalMasuk,
			},
		})
	}

	api.GET("/verify/:nim", func(c *gin.Context) {
		handleVerify(c, c.Param("nim"), c.Query("kampus"))
	})

	api.POST("/verify", func(c *gin.Context) {
		var req struct {
			NIM    string `json:"nim" binding:"required"`
			Kampus string `json:"kampus"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			badRequest(c, "JSON body with field 'nim' is required")
			return
		}
		handleVerify(c, req.NIM, req.Kampus)
	})

	return r, nil
}

func badRequest(c *gin.Context, msg string) {
	c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"status": "error", "code": "bad_request", "error": msg})
}

// writeError maps client errors to HTTP status codes and stable error codes.
func writeError(c *gin.Context, err error) {
	var amb *pddikti.AmbiguousError
	if errors.As(err, &amb) {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{
			"status":     "error",
			"code":       "ambiguous_nim",
			"error":      "NIM terdaftar di lebih dari satu perguruan tinggi; sertakan parameter 'kampus'",
			"candidates": amb.Candidates,
		})
		return
	}

	status, code := http.StatusBadGateway, "upstream_error"
	switch {
	case errors.Is(err, pddikti.ErrNotFound):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, pddikti.ErrInvalidID):
		status, code = http.StatusBadRequest, "invalid_id"
	case errors.Is(err, pddikti.ErrRateLimited):
		status, code = http.StatusTooManyRequests, "upstream_rate_limited"
		c.Header("Retry-After", "60")
	case errors.Is(err, pddikti.ErrBlocked):
		status, code = http.StatusBadGateway, "upstream_blocked"
	case errors.Is(err, pddikti.ErrTimeout):
		status, code = http.StatusGatewayTimeout, "upstream_timeout"
	}
	c.AbortWithStatusJSON(status, gin.H{"status": "error", "code": code, "error": err.Error()})
}

func corsMiddleware(origins []string) gin.HandlerFunc {
	allowAll := len(origins) == 0
	allowed := make(map[string]bool, len(origins))
	for _, o := range origins {
		o = strings.TrimRight(strings.TrimSpace(o), "/")
		if o == "*" {
			allowAll = true
		}
		allowed[o] = true
	}

	return func(c *gin.Context) {
		h := c.Writer.Header()
		if allowAll {
			h.Set("Access-Control-Allow-Origin", "*")
		} else {
			h.Add("Vary", "Origin")
			if origin := c.GetHeader("Origin"); allowed[origin] {
				h.Set("Access-Control-Allow-Origin", origin)
			}
		}
		h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func apiKeyMiddleware(key string) gin.HandlerFunc {
	want := []byte(key)
	return func(c *gin.Context) {
		got := []byte(c.GetHeader("X-API-Key"))
		if subtle.ConstantTimeCompare(got, want) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"status": "error", "code": "unauthorized", "error": "header X-API-Key tidak valid",
			})
			return
		}
		c.Next()
	}
}
