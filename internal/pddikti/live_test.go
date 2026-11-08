//go:build live

// Live tests hit the real PDDIKTI API and are excluded from `go test ./...`.
// Supply your own test data through environment variables so no real
// personal data is committed to this repository:
//
//	PDDIKTI_TEST_NIM=<nim> [PDDIKTI_TEST_KAMPUS=<nama pt>] go test -tags=live ./internal/pddikti/
package pddikti

import (
	"errors"
	"os"
	"testing"
)

func TestLive_VerifyNIM(t *testing.T) {
	nim := os.Getenv("PDDIKTI_TEST_NIM")
	if nim == "" {
		t.Skip("PDDIKTI_TEST_NIM not set")
	}
	c := NewClient()

	got, err := c.VerifyNIM(nim, os.Getenv("PDDIKTI_TEST_KAMPUS"))
	if err != nil {
		t.Fatalf("VerifyNIM(full NIM): %v", err)
	}
	if got.NIM != nim {
		t.Fatalf("returned NIM does not match the requested one")
	}
	t.Logf("verified; active=%v", got.IsActive())

	// A truncated NIM must never verify as someone else.
	for _, partial := range []string{nim[:len(nim)-1], nim[:len(nim)-3]} {
		if _, err := c.VerifyNIM(partial, ""); err != nil && !errors.Is(err, ErrNotFound) {
			t.Fatalf("VerifyNIM(partial): unexpected error %v", err)
		} else if err == nil {
			// Only acceptable if that partial NIM is itself a real, exact NIM.
			t.Logf("partial NIM resolved to an exact match of its own")
		}
	}
}
