package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"auth-pddikti/internal/pddikti"
)

// All data below is fictitious.

type stubClient struct {
	searchRes []pddikti.StudentSearchResult
	detail    *pddikti.StudentDetail
	err       error
	gotNIM    string
	gotCampus string
}

func (s *stubClient) Search(string) ([]pddikti.StudentSearchResult, error) {
	return s.searchRes, s.err
}
func (s *stubClient) GetDetail(string) (*pddikti.StudentDetail, error) { return s.detail, s.err }
func (s *stubClient) VerifyNIM(nim, campus string) (*pddikti.StudentDetail, error) {
	s.gotNIM, s.gotCampus = nim, campus
	return s.detail, s.err
}

func init() { gin.SetMode(gin.TestMode) }

func newTestRouter(t *testing.T, c Searcher, cfg Config) http.Handler {
	t.Helper()
	r, err := NewRouter(c, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func do(h http.Handler, method, path, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("invalid JSON %q: %v", w.Body.String(), err)
	}
	return m
}

func activeStudent() *pddikti.StudentDetail {
	return &pddikti.StudentDetail{ID: "id-a", NIM: "23010000001", Nama: "CONTOH MAHASISWA", NamaPT: "Universitas Contoh", KodePT: "001001 ", StatusSaatIni: "Aktif-2026/2027 Ganjil"}
}

func TestHealth(t *testing.T) {
	w := do(newTestRouter(t, &stubClient{}, Config{APIKey: "secret"}), "GET", "/health", "")
	if w.Code != http.StatusOK {
		t.Fatalf("health should be open even with API key, got %d", w.Code)
	}
}

func TestVerify_Success(t *testing.T) {
	stub := &stubClient{detail: activeStudent()}
	h := newTestRouter(t, stub, Config{})

	w := do(h, "GET", "/api/verify/23010000001?kampus=UC", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	m := decode(t, w)
	if m["verified"] != true || m["is_active"] != true || m["eligible"] != true {
		t.Fatalf("unexpected flags: %v", m)
	}
	if kode := m["student"].(map[string]any)["kode_pt"]; kode != "001001" {
		t.Fatalf("kode_pt not trimmed: %q", kode)
	}
	if stub.gotCampus != "UC" {
		t.Fatalf("kampus not forwarded: %q", stub.gotCampus)
	}

	w = do(h, "POST", "/api/verify", `{"nim":"23010000001","kampus":"Universitas Contoh"}`)
	if w.Code != http.StatusOK || stub.gotCampus != "Universitas Contoh" {
		t.Fatalf("POST status %d campus %q", w.Code, stub.gotCampus)
	}
}

func TestVerify_InactiveIsNotEligible(t *testing.T) {
	d := activeStudent()
	d.StatusSaatIni = "Dikeluarkan-2024/2025 Genap"
	m := decode(t, do(newTestRouter(t, &stubClient{detail: d}, Config{}), "GET", "/api/verify/23010000001", ""))
	if m["is_active"] != false || m["eligible"] != false {
		t.Fatalf("inactive student must not be eligible: %v", m)
	}
}

func TestVerify_BadRequests(t *testing.T) {
	h := newTestRouter(t, &stubClient{}, Config{})
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/api/verify", `{}`},
		{"POST", "/api/verify", `not json`},
		{"POST", "/api/verify", `{"nim":"   "}`},
		{"GET", "/api/verify/%20", ""},
	} {
		if w := do(h, tc.method, tc.path, tc.body); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s %s: got %d, want 400", tc.method, tc.path, tc.body, w.Code)
		}
	}
}

func TestErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		code int
		slug string
	}{
		{pddikti.ErrNotFound, http.StatusNotFound, "not_found"},
		{pddikti.ErrInvalidID, http.StatusBadRequest, "invalid_id"},
		{pddikti.ErrRateLimited, http.StatusTooManyRequests, "upstream_rate_limited"},
		{pddikti.ErrBlocked, http.StatusBadGateway, "upstream_blocked"},
		{pddikti.ErrTimeout, http.StatusGatewayTimeout, "upstream_timeout"},
		{fmt.Errorf("%w: status 500", pddikti.ErrUpstream), http.StatusBadGateway, "upstream_error"},
		{errors.New("unknown"), http.StatusBadGateway, "upstream_error"},
	}
	for _, tc := range cases {
		h := newTestRouter(t, &stubClient{err: tc.err}, Config{EnableSearch: true})
		for _, path := range []string{"/api/verify/123", "/api/detail/abc", "/api/search?q=abc"} {
			w := do(h, "GET", path, "")
			if w.Code != tc.code || decode(t, w)["code"] != tc.slug {
				t.Errorf("%v on %s: got %d %s, want %d %s", tc.err, path, w.Code, w.Body, tc.code, tc.slug)
			}
		}
	}
}

func TestErrorMapping_Ambiguous(t *testing.T) {
	err := &pddikti.AmbiguousError{NIM: "12345", Candidates: []pddikti.Campus{{NamaPT: "A"}, {NamaPT: "B"}}}
	w := do(newTestRouter(t, &stubClient{err: err}, Config{}), "GET", "/api/verify/12345", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("got %d", w.Code)
	}
	m := decode(t, w)
	if m["code"] != "ambiguous_nim" || len(m["candidates"].([]any)) != 2 {
		t.Fatalf("unexpected body: %v", m)
	}
}

func TestSearch(t *testing.T) {
	stub := &stubClient{searchRes: []pddikti.StudentSearchResult{{ID: "x", NIM: "1"}}}
	h := newTestRouter(t, stub, Config{MinQueryLen: 3, EnableSearch: true})

	if w := do(h, "GET", "/api/search", ""); w.Code != http.StatusBadRequest {
		t.Errorf("missing q: got %d", w.Code)
	}
	if w := do(h, "GET", "/api/search?q=%20ab%20", ""); w.Code != http.StatusBadRequest {
		t.Errorf("short q: got %d", w.Code)
	}
	w := do(h, "GET", "/api/search?q=abc", "")
	if w.Code != http.StatusOK || decode(t, w)["total"] != float64(1) {
		t.Errorf("valid q: got %d %s", w.Code, w.Body)
	}
}

func TestAPIKey(t *testing.T) {
	h := newTestRouter(t, &stubClient{detail: activeStudent()}, Config{APIKey: "secret"})
	if w := do(h, "GET", "/api/verify/1", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("no key: got %d", w.Code)
	}
	if w := do(h, "GET", "/api/verify/1", "", "X-API-Key", "wrong"); w.Code != http.StatusUnauthorized {
		t.Errorf("wrong key: got %d", w.Code)
	}
	if w := do(h, "GET", "/api/verify/1", "", "X-API-Key", "secret"); w.Code != http.StatusOK {
		t.Errorf("right key: got %d", w.Code)
	}
}

func TestCORS(t *testing.T) {
	w := do(newTestRouter(t, &stubClient{}, Config{}), "OPTIONS", "/api/verify", "", "Origin", "https://a.example")
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("default CORS: %d %q", w.Code, w.Header().Get("Access-Control-Allow-Origin"))
	}

	h := newTestRouter(t, &stubClient{}, Config{CORSOrigins: []string{"https://app.example/"}})
	if got := do(h, "OPTIONS", "/api/verify", "", "Origin", "https://app.example").Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Errorf("allowed origin: got %q", got)
	}
	if got := do(h, "OPTIONS", "/api/verify", "", "Origin", "https://evil.example").Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("disallowed origin: got %q", got)
	}
}

func TestRateLimit(t *testing.T) {
	h := newTestRouter(t, &stubClient{detail: activeStudent()}, Config{RatePerMinute: 3})
	for i := 0; i < 3; i++ {
		if w := do(h, "GET", "/api/verify/1", ""); w.Code != http.StatusOK {
			t.Fatalf("request %d: got %d", i+1, w.Code)
		}
	}
	w := do(h, "GET", "/api/verify/1", "")
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatalf("4th request: got %d", w.Code)
	}
	// X-Forwarded-For must not bypass the limit when no proxy is trusted.
	if w := do(h, "GET", "/api/verify/1", "", "X-Forwarded-For", "203.0.113.9"); w.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed XFF bypassed limit: got %d", w.Code)
	}
	if w := do(h, "GET", "/health", ""); w.Code != http.StatusOK {
		t.Fatalf("health must not be rate limited: got %d", w.Code)
	}
}

func TestRateLimiter_RefillsAndSweeps(t *testing.T) {
	now := time.Unix(0, 0)
	l := newIPRateLimiter(60) // 1 token/second, burst 60
	l.now = func() time.Time { return now }

	for i := 0; i < 60; i++ {
		l.allow("1.1.1.1")
	}
	if l.allow("1.1.1.1") {
		t.Fatal("bucket should be empty")
	}
	now = now.Add(time.Second)
	if !l.allow("1.1.1.1") {
		t.Fatal("bucket should refill after 1s")
	}

	now = now.Add(limiterIdleTTL + 2*limiterSweepEach)
	l.allow("2.2.2.2")
	if _, ok := l.clients["1.1.1.1"]; ok {
		t.Fatal("idle client should be swept")
	}
}
