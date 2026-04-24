package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRouter_IndexRunsDeviceMiddleware confirms the wiring: a fresh GET /
// should produce a device cookie, proving the middleware runs ahead of
// the index handler.
func TestRouter_IndexRunsDeviceMiddleware(t *testing.T) {
	t.Parallel()

	stub := &stubDeviceStore{}
	h := NewRouter(Deps{Devices: stub})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var found bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == DeviceCookieName {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected device cookie on /, got none")
	}
	if len(stub.upserts) != 1 {
		t.Errorf("expected 1 device upsert, got %d", len(stub.upserts))
	}
}

// TestRouter_HealthSkipsDeviceMiddleware keeps /health as a pure liveness
// probe. Uptime Kuma and other pingers must not churn the devices table.
func TestRouter_HealthSkipsDeviceMiddleware(t *testing.T) {
	t.Parallel()

	stub := &stubDeviceStore{}
	h := NewRouter(Deps{Devices: stub})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == DeviceCookieName {
			t.Errorf("/health must not issue a device cookie")
		}
	}
	if len(stub.upserts) != 0 {
		t.Errorf("expected 0 device upserts on /health, got %d", len(stub.upserts))
	}
}

// TestRouter_TreesRunsDeviceMiddleware confirms the map's read path also
// attributes requests to a device. Rate-limiting observations in Phase E
// will read from context; the stamp must be there.
func TestRouter_TreesRunsDeviceMiddleware(t *testing.T) {
	t.Parallel()

	stub := &stubDeviceStore{}
	h := NewRouter(Deps{Devices: stub})

	req := httptest.NewRequest(http.MethodGet, "/trees?bbox=36.6,-1.4,37.0,-1.1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	if len(stub.upserts) != 1 {
		t.Errorf("expected 1 device upsert on /trees, got %d", len(stub.upserts))
	}
}
