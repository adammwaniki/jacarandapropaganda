package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestIndex_RendersShell is the walking-skeleton frontend check. The index
// handler must serve an HTML document with the map container and the
// MapLibre + PMTiles dependencies referenced. It does not yet render real
// data — that is Phase C onward.
func TestIndex_RendersShell(t *testing.T) {
	t.Parallel()

	h := NewRouter(Deps{Devices: &stubDeviceStore{}})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status: got %d, want %d", got, want)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type: got %q, want text/html prefix", ct)
	}

	body := rec.Body.String()
	for _, marker := range []string{
		`<!doctype html>`,
		`<div id="map"`,
		`maplibre-gl`,
		`pmtiles`,
		`alpinejs`,
	} {
		if !strings.Contains(strings.ToLower(body), strings.ToLower(marker)) {
			t.Errorf("index body missing marker %q", marker)
		}
	}
}

func TestIndex_CentersOnNairobi(t *testing.T) {
	t.Parallel()

	// The initial map view should be over Nairobi CBD (approx -1.2921, 36.8219).
	// We assert via the embedded center coordinates in the page source.
	h := NewRouter(Deps{Devices: &stubDeviceStore{}})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "36.82") {
		t.Errorf("page does not embed Nairobi longitude (expected ~36.82)")
	}
	if !strings.Contains(body, "-1.29") {
		t.Errorf("page does not embed Nairobi latitude (expected ~-1.29)")
	}
}
