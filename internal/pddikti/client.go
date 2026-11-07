package pddikti

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://pddikti.kemdiktisaintek.go.id/api"
	UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36 Edg/131.0.0.0"
	Origin    = "https://pddikti.kemdiktisaintek.go.id"
	Referer   = "https://pddikti.kemdiktisaintek.go.id/"
)

// upstreamBaseURL adalah alamat API PDDIKTI yang aktif. Variabel paket
// (bukan konstanta) supaya pengujian bisa mengarahkan klien ke server uji
// lokal; produksi selalu memakai defaultBaseURL.
var upstreamBaseURL = defaultBaseURL

func baseURL() string { return upstreamBaseURL }

type Client struct {
	httpClient *http.Client
}

func NewClient(timeout time.Duration) *Client {
	if timeout == 0 {
		timeout = 8 * time.Second
	}
	return &Client{
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// StudentSearchResult represents an item returned from /api/pencarian/mhs/{keyword}
type StudentSearchResult struct {
	ID         string `json:"id"`
	Nama       string `json:"nama"`
	NIM        string `json:"nim"`
	NamaPT     string `json:"nama_pt"`
	SingkatanPT string `json:"sinkatan_pt"`
	NamaProdi  string `json:"nama_prodi"`
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

type DetailResponse struct {
	Status  string        `json:"status"`
	Message string        `json:"message,omitempty"`
	Data    StudentDetail `json:"data"`
}

func (c *Client) prepareRequest(endpoint string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Origin", Origin)
	req.Header.Set("Referer", Referer)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	return req, nil
}

// Search queries student by NIM or Name
func (c *Client) Search(keyword string) ([]StudentSearchResult, error) {
	cleanKeyword := strings.TrimSpace(keyword)
	// Clean slashes to avoid routing errors
	cleanKeyword = strings.ReplaceAll(cleanKeyword, "/", " ")
	encoded := url.PathEscape(cleanKeyword)

	endpoint := fmt.Sprintf("%s/pencarian/mhs/%s", baseURL(), encoded)
	req, err := c.prepareRequest(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http error reaching pddikti: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("pddikti WAF challenge or missing headers (403 Forbidden)")
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("pddikti rate limit exceeded (60 req/min limit)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pddikti returned status %d", resp.StatusCode)
	}

	var searchResp SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return searchResp.Data, nil
}

// GetDetail retrieves full student information using encrypted ID
func (c *Client) GetDetail(id string) (*StudentDetail, error) {
	endpoint := fmt.Sprintf("%s/detail/mhs/%s", baseURL(), url.PathEscape(id))
	req, err := c.prepareRequest(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("detail endpoint returned status %d", resp.StatusCode)
	}

	var detailResp DetailResponse
	if err := json.NewDecoder(resp.Body).Decode(&detailResp); err != nil {
		return nil, fmt.Errorf("failed to decode detail response: %w", err)
	}

	return &detailResp.Data, nil
}

// VerifyNIM searches by exact NIM and immediately pulls full student detail
func (c *Client) VerifyNIM(nim string) (*StudentDetail, error) {
	candidates, err := c.Search(nim)
	if err != nil {
		return nil, err
	}

	cleanNIM := strings.TrimSpace(nim)
	var targetID string
	for _, item := range candidates {
		if strings.EqualFold(strings.TrimSpace(item.NIM), cleanNIM) {
			targetID = item.ID
			break
		}
	}

	if targetID == "" {
		// JANGAN mengambil kandidat pertama: hasil pencarian boleh berisi
		// mahasiswa lain yang namanya mirip. Bila tidak ada NIM yang cocok
		// persis, verifikasi GAGAL (tidak ditemukan), bukan "terverifikasi"
		// atas data orang lain. (Perbaikan audit Lemper 8 Nov 2026)
		return nil, fmt.Errorf("mahasiswa dengan NIM '%s' tidak ditemukan di PDDIKTI", nim)
	}

	return c.GetDetail(targetID)
}
