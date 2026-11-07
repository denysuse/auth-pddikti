package pddikti

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Uji regresi bug audit Lemper 8 Nov 2026: VerifyNIM TIDAK BOLEH
// mengembalikan kandidat pertama bila NIM tidak cocok persis.
// Kode asli mengambil candidates[0] sehingga data orang lain bisa
// terverifikasi; kode perbaikan harus mengembalikan error.
func TestVerifyNIMTidakFallbackKeKandidatLain(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/pencarian/mhs/") {
			json.NewEncoder(w).Encode(SearchResponse{Data: []StudentSearchResult{
				{ID: "id-orang-lain", Nama: "ORANG LAIN", NIM: "999999999"},
			}})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/detail/mhs/") {
			json.NewEncoder(w).Encode(DetailResponse{Data: StudentDetail{
				ID: "id-orang-lain", Nama: "ORANG LAIN", NIM: "999999999",
			}})
			return
		}
		http.NotFound(w, r)
	}))
	defer upstream.Close()

	old := upstreamBaseURL
	upstreamBaseURL = upstream.URL
	defer func() { upstreamBaseURL = old }()

	c := NewClient(2 * time.Second)
	_, err := c.VerifyNIM("23010000001")
	if err == nil {
		t.Fatal("VerifyNIM harus error bila NIM tidak cocok persis, bukan mengambil kandidat lain")
	}
	if !strings.Contains(err.Error(), "tidak ditemukan") {
		t.Fatalf("pesan error tak terduga: %v", err)
	}
}
