package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/adammwaniki/jacarandapropaganda/internal/id"
	"github.com/adammwaniki/jacarandapropaganda/internal/store"
)

func TestGetTreeDetail_RendersFragmentWithLatest(t *testing.T) {
	t.Parallel()

	treeID := id.NewTree()
	photo := "photos/2026/04/tree.jpg"
	treeStub := &stubTreeWithByID{
		tree: &store.Tree{ID: treeID, Species: "jacaranda"},
	}
	obsStub := &stubObservationService{
		current: &store.Observation{
			ID: id.NewObservation(), TreeID: treeID,
			BloomState: store.BloomFull, PhotoR2Key: &photo,
		},
	}
	h := NewRouter(Deps{
		Devices: &stubDeviceStore{}, Trees: treeStub, Observations: obsStub,
		PhotoURLPrefix: "https://cdn.example.com/",
	})

	req := httptest.NewRequest(http.MethodGet, "/trees/"+treeID.String(), nil)
	req.AddCookie(&http.Cookie{Name: DeviceCookieName, Value: uuid.New().String()})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body=%q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type: got %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, treeID.String()) {
		t.Errorf("fragment should reference tree id, got %q", body)
	}
	if !strings.Contains(body, "full") {
		t.Errorf("fragment should show bloom_state, got %q", body)
	}
	if !strings.Contains(body, "https://cdn.example.com/"+photo) {
		t.Errorf("fragment should render photo URL, got %q", body)
	}
	// Update form must be present with all four bloom buttons.
	for _, b := range []string{"budding", "partial", "full", "fading"} {
		if !strings.Contains(body, b) {
			t.Errorf("fragment missing bloom button %q", b)
		}
	}
}

func TestGetTreeDetail_RendersWhenNoObservation(t *testing.T) {
	t.Parallel()

	treeID := id.NewTree()
	treeStub := &stubTreeWithByID{tree: &store.Tree{ID: treeID, Species: "jacaranda"}}
	obsStub := &stubObservationService{} // CurrentForTree returns ErrNotFound
	h := NewRouter(Deps{
		Devices: &stubDeviceStore{}, Trees: treeStub, Observations: obsStub,
	})

	req := httptest.NewRequest(http.MethodGet, "/trees/"+treeID.String(), nil)
	req.AddCookie(&http.Cookie{Name: DeviceCookieName, Value: uuid.New().String()})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not yet observed") {
		t.Errorf("unobserved tree should say so, got %q", rec.Body.String())
	}
}

func TestGetTreeDetail_UnknownTree_404(t *testing.T) {
	t.Parallel()

	h := NewRouter(Deps{
		Devices:      &stubDeviceStore{},
		Trees:        &stubTreeWithByID{}, // ByID → ErrNotFound
		Observations: &stubObservationService{},
	})
	req := httptest.NewRequest(http.MethodGet, "/trees/"+id.NewTree().String(), nil)
	req.AddCookie(&http.Cookie{Name: DeviceCookieName, Value: uuid.New().String()})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", rec.Code)
	}
}

func TestGetTreeDetail_MalformedID_400(t *testing.T) {
	t.Parallel()

	h := NewRouter(Deps{
		Devices:      &stubDeviceStore{},
		Trees:        &stubTreeWithByID{},
		Observations: &stubObservationService{},
	})
	req := httptest.NewRequest(http.MethodGet, "/trees/not-a-uuid", nil)
	req.AddCookie(&http.Cookie{Name: DeviceCookieName, Value: uuid.New().String()})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
}
