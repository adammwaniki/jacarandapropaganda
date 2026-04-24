package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTreesEndpoint_EmptyBbox is the walking-skeleton shape of the read
// path. Backed by an empty (or not-yet-wired) data source, GET /trees must
// still return a valid, empty GeoJSON FeatureCollection. Downstream phases
// wire this handler to the real trees repository.
func TestTreesEndpoint_EmptyBbox(t *testing.T) {
	t.Parallel()

	h := NewRouter(Deps{Devices: &stubDeviceStore{}})

	// Nairobi-sized bbox: minLon, minLat, maxLon, maxLat.
	req := httptest.NewRequest(http.MethodGet, "/trees?bbox=36.6,-1.4,37.0,-1.1", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status: got %d, want %d (body=%q)", got, want, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/geo+json") {
		t.Fatalf("Content-Type: got %q, want application/geo+json prefix", ct)
	}

	var fc struct {
		Type     string           `json:"type"`
		Features []map[string]any `json:"features"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &fc); err != nil {
		t.Fatalf("body not valid JSON: %v", err)
	}
	if got, want := fc.Type, "FeatureCollection"; got != want {
		t.Fatalf("type: got %q, want %q", got, want)
	}
	if len(fc.Features) != 0 {
		t.Fatalf("features: got %d, want 0", len(fc.Features))
	}
}

func TestTreesEndpoint_RejectsMissingBbox(t *testing.T) {
	t.Parallel()

	h := NewRouter(Deps{Devices: &stubDeviceStore{}})
	req := httptest.NewRequest(http.MethodGet, "/trees", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status: got %d, want %d", got, want)
	}
}

func TestTreesEndpoint_RejectsMalformedBbox(t *testing.T) {
	t.Parallel()

	h := NewRouter(Deps{Devices: &stubDeviceStore{}})
	req := httptest.NewRequest(http.MethodGet, "/trees?bbox=not,a,real,bbox", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status: got %d, want %d", got, want)
	}
}

func TestTreesEndpoint_RejectsInvertedBbox(t *testing.T) {
	t.Parallel()

	// minLon > maxLon should fail before any store hit — cheap validation.
	h := NewRouter(Deps{Devices: &stubDeviceStore{}})
	req := httptest.NewRequest(http.MethodGet, "/trees?bbox=37.0,-1.1,36.6,-1.4", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status: got %d, want %d", got, want)
	}
}
