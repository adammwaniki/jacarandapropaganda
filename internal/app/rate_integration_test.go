package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/adammwaniki/jacarandapropaganda/internal/id"
	"github.com/adammwaniki/jacarandapropaganda/internal/rate"
	"github.com/adammwaniki/jacarandapropaganda/internal/store"
)

// stubLimiter lets tests drive the 429 path deterministically.
type stubLimiter struct {
	treeErr   error
	obsErr    error
	treeCalls int
	obsCalls  int
}

func (s *stubLimiter) CheckAndRecordTreeCreate(ctx context.Context, dev uuid.UUID, ip netip.Addr) error {
	s.treeCalls++
	return s.treeErr
}
func (s *stubLimiter) CheckAndRecordObservationCreate(ctx context.Context, dev uuid.UUID) error {
	s.obsCalls++
	return s.obsErr
}

func TestPostTrees_Returns429WhenDeviceLimited(t *testing.T) {
	t.Parallel()

	trees := &stubTreeService{}
	lim := &stubLimiter{treeErr: rate.LimitedError{
		Kind: rate.KindTreeCreate, Scope: rate.ScopeDevice, Limit: 10,
	}}
	h := NewRouter(Deps{Devices: &stubDeviceStore{}, Trees: trees, RateLimiter: lim})

	form := url.Values{"lat": {"-1.2921"}, "lng": {"36.8219"}, "bloom_state": {"full"}}
	rec := postForm(t, h, "/trees", form)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status: got %d, want 429 (body=%q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type: got %q, want text/html", ct)
	}
	body := strings.ToLower(rec.Body.String())
	if !strings.Contains(body, "tomorrow") && !strings.Contains(body, "24") {
		t.Errorf("429 body should mention the 24h window; got %q", rec.Body.String())
	}
	if len(trees.inserts) != 0 {
		t.Errorf("no insert should happen when limited, got %d", len(trees.inserts))
	}
}

func TestPostTrees_Returns429WhenIPLimited(t *testing.T) {
	t.Parallel()

	lim := &stubLimiter{treeErr: rate.LimitedError{
		Kind: rate.KindTreeCreate, Scope: rate.ScopeIP, Limit: 30,
	}}
	h := NewRouter(Deps{
		Devices:     &stubDeviceStore{},
		Trees:       &stubTreeService{},
		RateLimiter: lim,
	})

	form := url.Values{"lat": {"-1.2921"}, "lng": {"36.8219"}, "bloom_state": {"full"}}
	rec := postForm(t, h, "/trees", form)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status: got %d, want 429", rec.Code)
	}
	// IP-scope message must differ from device-scope — a user seeing this
	// on a phone deserves to know the limit is shared network-wide, not theirs.
	body := strings.ToLower(rec.Body.String())
	if !strings.Contains(body, "network") && !strings.Contains(body, "ip") {
		t.Errorf("IP-scope 429 body should mention the network context; got %q", rec.Body.String())
	}
}

func TestPostTrees_PropagatesGenericLimiterError(t *testing.T) {
	t.Parallel()

	lim := &stubLimiter{treeErr: errors.New("db down")}
	h := NewRouter(Deps{
		Devices:     &stubDeviceStore{},
		Trees:       &stubTreeService{},
		RateLimiter: lim,
	})

	form := url.Values{"lat": {"-1.2921"}, "lng": {"36.8219"}, "bloom_state": {"full"}}
	rec := postForm(t, h, "/trees", form)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", rec.Code)
	}
}

func TestPostTrees_ForceStillHitsLimiter(t *testing.T) {
	t.Parallel()

	// Even when the user bypasses dedup with force=1, the rate limiter
	// still runs — otherwise force would become the abuse vector.
	lim := &stubLimiter{treeErr: rate.LimitedError{
		Kind: rate.KindTreeCreate, Scope: rate.ScopeDevice, Limit: 10,
	}}
	h := NewRouter(Deps{
		Devices:     &stubDeviceStore{},
		Trees:       &stubTreeService{},
		RateLimiter: lim,
	})

	form := url.Values{
		"lat": {"-1.2921"}, "lng": {"36.8219"},
		"bloom_state": {"full"}, "force": {"1"},
	}
	rec := postForm(t, h, "/trees", form)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status: got %d, want 429", rec.Code)
	}
}

func TestPostObservation_Returns429WhenLimited(t *testing.T) {
	t.Parallel()

	treeID := id.NewTree()
	obs := &stubObservationService{}
	lim := &stubLimiter{obsErr: rate.LimitedError{
		Kind: rate.KindObservationCreate, Scope: rate.ScopeDevice, Limit: 60,
	}}
	h := NewRouter(Deps{
		Devices:      &stubDeviceStore{},
		Trees:        &stubTreeWithByID{tree: &store.Tree{ID: treeID, Species: "jacaranda"}},
		Observations: obs,
		RateLimiter:  lim,
	})

	form := url.Values{"bloom_state": {"full"}}
	rec := postObsForm(t, h, treeID, form)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status: got %d, want 429 (body=%q)", rec.Code, rec.Body.String())
	}
	if len(obs.inserts) != 0 {
		t.Errorf("no insert should happen when limited, got %d", len(obs.inserts))
	}
}

func TestPostTrees_LimiterRunsBeforeDedup(t *testing.T) {
	t.Parallel()

	// Dedup is cheaper to short-circuit, but the rate limiter is the
	// abuse control — it must run first so a limited user never learns
	// which neighboring trees exist via probing.
	trees := &stubTreeService{
		candidates: []store.CandidateTree{{
			TreeWithState:  store.TreeWithState{Tree: store.Tree{ID: id.NewTree()}},
			DistanceMeters: 1.0,
		}},
	}
	lim := &stubLimiter{treeErr: rate.LimitedError{
		Kind: rate.KindTreeCreate, Scope: rate.ScopeDevice, Limit: 10,
	}}
	h := NewRouter(Deps{
		Devices: &stubDeviceStore{}, Trees: trees, RateLimiter: lim,
	})

	form := url.Values{"lat": {"-1.2921"}, "lng": {"36.8219"}, "bloom_state": {"full"}}
	rec := postForm(t, h, "/trees", form)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status: got %d, want 429", rec.Code)
	}
	// Dedup lookup must NOT have happened — body must not leak neighbors.
	if strings.Contains(strings.ToLower(rec.Body.String()), "same tree") {
		t.Errorf("429 response must not reveal dedup candidates: %q", rec.Body.String())
	}
}

func TestPostTrees_NilLimiterAllowsThrough(t *testing.T) {
	// Tests that configure no limiter (e.g. earlier D-phase tests) must
	// still work — a missing Deps.RateLimiter is treated as "no limits".
	// Production wiring always supplies a real limiter.
	t.Parallel()

	trees := &stubTreeService{}
	h := NewRouter(Deps{Devices: &stubDeviceStore{}, Trees: trees})

	form := url.Values{"lat": {"-1.2921"}, "lng": {"36.8219"}, "bloom_state": {"full"}}
	rec := postForm(t, h, "/trees", form)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201 (no limiter should be permissive)", rec.Code)
	}
}

// Avoid unused-import warnings in early iterations.
var _ = httptest.NewRequest
