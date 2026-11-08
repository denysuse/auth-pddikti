package pddikti

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://pddikti.kemdiktisaintek.go.id/api"
	DefaultTimeout = 8 * time.Second
	UserAgent      = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0"
	Origin         = "https://pddikti.kemdiktisaintek.go.id"
	Referer        = "https://pddikti.kemdiktisaintek.go.id/"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL points the client at another upstream (used by tests).
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithTimeout sets the upstream HTTP timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.httpClient.Timeout = d
		}
	}
}

func NewClient(opts ...Option) *Client {
	c := &Client{
		baseURL:    DefaultBaseURL,
		httpClient: &http.Client{Timeout: DefaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// StudentSearchResult represents an item returned from /api/pencarian/mhs/{keyword}
type StudentSearchResult struct {
	ID          string `json:"id"`
	Nama        string `json:"nama"`
	NIM         string `json:"nim"`
	NamaPT      string `json:"nama_pt"`
	SingkatanPT string `json:"sinkatan_pt"` // upstream spelling
	NamaProdi   string `json:"nama_prodi"`
}

type SearchResponse struct {
	Status  string                `json:"status"`
	Message string                `json:"message,omitempty"`
	Data    []StudentSearchResult `json:"data"`
}

// StudentDetail represents full academic details returned from /api/detail/mhs/{id}
type StudentDetail struct {
	ID            string `json:"id"`
	NamaPT        string `json:"nama_pt"`
	KodePT        string `json:"kode_pt"`
	KodeProdi     string `json:"kode_prodi"`
	Prodi         string `json:"prodi"`
	Nama          string `json:"nama"`
	NIM           string `json:"nim"`
	JenisDaftar   string `json:"jenis_daftar"`
	IDPT          string `json:"id_pt"`
	IDSMS         string `json:"id_sms"`
	JenisKelamin  string `json:"jenis_kelamin"`
	Jenjang       string `json:"jenjang"`
	StatusSaatIni string `json:"status_saat_ini"` // e.g. "Aktif-2026/2027 Ganjil"
	TanggalMasuk  string `json:"tanggal_masuk"`
}

// IsActive reports whether the current academic status is "Aktif-...".
func (d *StudentDetail) IsActive() bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(d.StatusSaatIni)), "aktif")
}

type DetailResponse struct {
	Status  string        `json:"status"`
	Message string        `json:"message,omitempty"`
	Data    StudentDetail `json:"data"`
}

// get performs a GET with browser-like headers, maps upstream failures to
// sentinel errors and decodes a 200 response into out. badRequest is the
// error returned for upstream 400/404 responses.
func (c *Client) get(path string, out any, badRequest error) error {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Origin", Origin)
	req.Header.Set("Referer", Referer)
	req.Header.Set("Accept", "application/json, text/plain, */*")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) && uerr.Timeout() {
			return ErrTimeout
		}
		return fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusForbidden:
		return ErrBlocked
	case resp.StatusCode == http.StatusTooManyRequests:
		return ErrRateLimited
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusNotFound:
		return badRequest
	default:
		return fmt.Errorf("%w: status %d", ErrUpstream, resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: gagal decode JSON: %v", ErrUpstream, err)
	}
	return nil
}

// Search queries students by NIM or name. An empty result is not an error.
func (c *Client) Search(keyword string) ([]StudentSearchResult, error) {
	clean := strings.TrimSpace(keyword)
	// Slashes would change the upstream route.
	clean = strings.ReplaceAll(clean, "/", " ")

	var sr SearchResponse
	// Upstream 400/404 on search means "no result", so it maps to a nil error.
	if err := c.get("/pencarian/mhs/"+url.PathEscape(clean), &sr, nil); err != nil {
		return nil, err
	}
	if sr.Data == nil {
		return []StudentSearchResult{}, nil
	}
	return sr.Data, nil
}

// GetDetail retrieves full student information using an encrypted ID.
func (c *Client) GetDetail(id string) (*StudentDetail, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrInvalidID
	}
	var dr DetailResponse
	if err := c.get("/detail/mhs/"+url.PathEscape(id), &dr, ErrInvalidID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(dr.Data.ID) == "" && strings.TrimSpace(dr.Data.NIM) == "" {
		return nil, ErrNotFound
	}
	return &dr.Data, nil
}

// VerifyNIM finds the student whose NIM matches nim exactly and returns the
// full detail. It never falls back to a partial or fuzzy match.
//
// NIM is not unique nationwide: when several campuses share the same NIM,
// campus (nama PT or singkatan, case-insensitive; empty = no filter) is used
// to narrow it down, otherwise an *AmbiguousError is returned.
func (c *Client) VerifyNIM(nim, campus string) (*StudentDetail, error) {
	cleanNIM := strings.TrimSpace(nim)
	if cleanNIM == "" {
		return nil, ErrNotFound
	}

	candidates, err := c.Search(cleanNIM)
	if err != nil {
		return nil, err
	}

	var matches []StudentSearchResult
	for _, item := range candidates {
		if strings.TrimSpace(item.NIM) == cleanNIM && matchesCampus(item, campus) {
			matches = append(matches, item)
		}
	}

	switch len(matches) {
	case 0:
		return nil, ErrNotFound
	case 1:
	default:
		amb := &AmbiguousError{NIM: cleanNIM}
		for _, m := range matches {
			amb.Candidates = append(amb.Candidates, Campus{
				NamaPT:      strings.TrimSpace(m.NamaPT),
				SingkatanPT: strings.TrimSpace(m.SingkatanPT),
				NamaProdi:   strings.TrimSpace(m.NamaProdi),
			})
		}
		return nil, amb
	}

	detail, err := c.GetDetail(matches[0].ID)
	if err != nil {
		return nil, err
	}
	// Defence in depth: the detail must belong to the NIM that was asked for.
	if strings.TrimSpace(detail.NIM) != cleanNIM {
		return nil, ErrNotFound
	}
	return detail, nil
}

func matchesCampus(item StudentSearchResult, campus string) bool {
	campus = strings.ToUpper(strings.TrimSpace(campus))
	if campus == "" {
		return true
	}
	nama := strings.ToUpper(strings.TrimSpace(item.NamaPT))
	singkatan := strings.ToUpper(strings.TrimSpace(item.SingkatanPT))
	return (singkatan != "" && singkatan == campus) || strings.Contains(nama, campus)
}
