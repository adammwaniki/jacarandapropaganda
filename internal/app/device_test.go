package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// stubDeviceStore records calls so middleware tests can assert behavior
// without a database. Device-store correctness is covered separately by
// integration tests in internal/store.
type stubDeviceStore struct {
	upserts []uuid.UUID
	upsert  func(context.Context, uuid.UUID) error
}

func (s *stubDeviceStore) Upsert(ctx context.Context, id uuid.UUID) error {
	s.upserts = append(s.upserts, id)
	if s.upsert != nil {
		return s.upsert(ctx, id)
	}
	return nil
}

func TestDeviceMiddleware_SetsCookieOnFirstRequest(t *testing.T) {
	t.Parallel()

	stub := &stubDeviceStore{}
	mw := WithDevice(stub)

	var handlerCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		if _, ok := DeviceFromContext(r.Context()); !ok {
			t.Fatalf("expected device in context, got none")
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, req)

	if !handlerCalled {
		t.Fatalf("next handler was not called")
	}

	cookies := rec.Result().Cookies()
	var deviceCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == DeviceCookieName {
			deviceCookie = c
			break
		}
	}
	if deviceCookie == nil {
		t.Fatalf("no %q cookie set", DeviceCookieName)
	}

	id, err := uuid.Parse(deviceCookie.Value)
	if err != nil {
		t.Fatalf("cookie value is not a UUID: %v", err)
	}
	// Spec invariant: UUIDv4 (random), NOT UUIDv7 (time-ordered).
	if got := id.Version(); got != 4 {
		t.Fatalf("device cookie must be UUIDv4, got v%d", got)
	}

	if !deviceCookie.HttpOnly {
		t.Errorf("cookie must be HttpOnly")
	}
	if deviceCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie SameSite: got %v, want Lax", deviceCookie.SameSite)
	}
	if deviceCookie.Path != "/" {
		t.Errorf("cookie path: got %q, want %q", deviceCookie.Path, "/")
	}
	if deviceCookie.MaxAge < 365*24*60*60 {
		t.Errorf("cookie MaxAge too short: %d seconds (want at least 1 year)", deviceCookie.MaxAge)
	}

	if len(stub.upserts) != 1 {
		t.Fatalf("Upsert called %d times, want 1", len(stub.upserts))
	}
	if stub.upserts[0] != id {
		t.Errorf("Upsert id: got %v, want %v (matches cookie)", stub.upserts[0], id)
	}
}

func TestDeviceMiddleware_ReusesExistingCookie(t *testing.T) {
	t.Parallel()

	stub := &stubDeviceStore{}
	mw := WithDevice(stub)

	existing := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: DeviceCookieName, Value: existing.String()})

	var gotInContext uuid.UUID
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := DeviceFromContext(r.Context())
		if !ok {
			t.Fatalf("expected device in context")
		}
		gotInContext = id
	})

	rec := httptest.NewRecorder()
	mw(next).ServeHTTP(rec, req)

	if gotInContext != existing {
		t.Errorf("context device: got %v, want %v", gotInContext, existing)
	}

	// Optimisation invariant: if the cookie is already valid, do not rewrite it.
	for _, c := range rec.Result().Cookies() {
		if c.Name == DeviceCookieName {
			t.Errorf("middleware rewrote cookie on request that already had one")
		}
	}

	if len(stub.upserts) != 1 || stub.upserts[0] != existing {
		t.Errorf("Upsert calls: got %v, want [%v]", stub.upserts, existing)
	}
}

func TestDeviceMiddleware_ReplacesMalformedCookie(t *testing.T) {
	t.Parallel()

	stub := &stubDeviceStore{}
	mw := WithDevice(stub)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: DeviceCookieName, Value: "not-a-uuid"})

	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

	var replaced *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == DeviceCookieName {
			replaced = c
		}
	}
	if replaced == nil {
		t.Fatalf("middleware did not replace malformed cookie")
	}
	id, err := uuid.Parse(replaced.Value)
	if err != nil {
		t.Fatalf("replacement cookie not a UUID: %v", err)
	}
	if id.Version() != 4 {
		t.Errorf("replacement must be UUIDv4, got v%d", id.Version())
	}
}

func TestDeviceMiddleware_RejectsNonV4CookieAndReplaces(t *testing.T) {
	// A cookie whose value parses as a UUID but is v7 (time-ordered) must be
	// replaced. The spec's privacy argument only holds if v4 is the only
	// accepted shape.
	t.Parallel()

	stub := &stubDeviceStore{}
	mw := WithDevice(stub)

	v7, _ := uuid.NewV7()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: DeviceCookieName, Value: v7.String()})

	rec := httptest.NewRecorder()
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

	var replaced *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == DeviceCookieName {
			replaced = c
		}
	}
	if replaced == nil {
		t.Fatalf("middleware must replace a v7 cookie with a v4 one")
	}
	id, err := uuid.Parse(replaced.Value)
	if err != nil {
		t.Fatalf("replacement not a UUID: %v", err)
	}
	if id.Version() != 4 {
		t.Errorf("replacement must be UUIDv4, got v%d", id.Version())
	}
}
