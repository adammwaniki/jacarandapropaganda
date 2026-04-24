package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/adammwaniki/jacarandapropaganda/internal/id"
	"github.com/adammwaniki/jacarandapropaganda/internal/store"
)

// stubObservationService captures calls. The real *store.ObservationStore
// satisfies this interface.
type stubObservationService struct {
	inserts    []store.InsertObservationParams
	insertErr  error
	current    *store.Observation
	currentErr error
}

func (s *stubObservationService) Insert(ctx context.Context, p store.InsertObservationParams) error {
	s.inserts = append(s.inserts, p)
	return s.insertErr
}
func (s *stubObservationService) CurrentForTree(ctx context.Context, treeID uuid.UUID) (store.Observation, error) {
	if s.currentErr != nil {
		return store.Observation{}, s.currentErr
	}
	if s.current != nil {
		return *s.current, nil
	}
	return store.Observation{}, store.ErrNotFound
}
func (s *stubObservationService) Hide(ctx context.Context, obsID uuid.UUID) error { return nil }

// stubTreeServiceWithByID extends the post-tree stub so pin-detail-adjacent
// paths can find tree rows by id.
type stubTreeWithByID struct {
	stubTreeService
	tree    *store.Tree
	byIDErr error
}

func (s *stubTreeWithByID) ByID(ctx context.Context, treeID uuid.UUID) (store.Tree, error) {
	if s.byIDErr != nil {
		return store.Tree{}, s.byIDErr
	}
	if s.tree != nil {
		return *s.tree, nil
	}
	return store.Tree{}, store.ErrNotFound
}

func postObsForm(t *testing.T, h http.Handler, treeID uuid.UUID, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/trees/"+treeID.String()+"/observations",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: DeviceCookieName, Value: uuid.New().String()})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPostObservation_HappyPath(t *testing.T) {
	t.Parallel()

	treeID := id.NewTree()
	treeStub := &stubTreeWithByID{tree: &store.Tree{
		ID: treeID, Species: "jacaranda", Lat: -1.2921, Lng: 36.8219,
	}}
	obsStub := &stubObservationService{}
	h := NewRouter(Deps{
		Devices:      &stubDeviceStore{},
		Trees:        treeStub,
		Observations: obsStub,
	})

	form := url.Values{}
	form.Set("bloom_state", "partial")

	rec := postObsForm(t, h, treeID, form)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201 (body=%q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type: got %q, want text/html", ct)
	}
	if len(obsStub.inserts) != 1 {
		t.Fatalf("expected 1 observation insert, got %d", len(obsStub.inserts))
	}
	ins := obsStub.inserts[0]
	if ins.TreeID != treeID {
		t.Errorf("tree_id: got %v, want %v", ins.TreeID, treeID)
	}
	if ins.BloomState != store.BloomPartial {
		t.Errorf("bloom_state: got %q, want partial", ins.BloomState)
	}
	if ins.ID.Version() != 7 {
		t.Errorf("id must be v7, got v%d", ins.ID.Version())
	}
}

func TestPostObservation_RejectsInvalidBloom(t *testing.T) {
	t.Parallel()

	treeID := id.NewTree()
	h := NewRouter(Deps{
		Devices:      &stubDeviceStore{},
		Trees:        &stubTreeWithByID{tree: &store.Tree{ID: treeID, Species: "jacaranda"}},
		Observations: &stubObservationService{},
	})

	form := url.Values{}
	form.Set("bloom_state", "flourishing")
	rec := postObsForm(t, h, treeID, form)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
}

func TestPostObservation_UnknownTreeReturns404(t *testing.T) {
	t.Parallel()

	treeID := id.NewTree()
	h := NewRouter(Deps{
		Devices:      &stubDeviceStore{},
		Trees:        &stubTreeWithByID{}, // ByID returns ErrNotFound
		Observations: &stubObservationService{},
	})

	form := url.Values{}
	form.Set("bloom_state", "full")
	rec := postObsForm(t, h, treeID, form)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d, want 404", rec.Code)
	}
}

func TestPostObservation_MalformedTreeIDReturns400(t *testing.T) {
	t.Parallel()

	h := NewRouter(Deps{
		Devices:      &stubDeviceStore{},
		Trees:        &stubTreeWithByID{},
		Observations: &stubObservationService{},
	})

	req := httptest.NewRequest(http.MethodPost, "/trees/not-a-uuid/observations",
		strings.NewReader("bloom_state=full"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: DeviceCookieName, Value: uuid.New().String()})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want 400", rec.Code)
	}
}

func TestPostObservation_StampsDeviceFromContext(t *testing.T) {
	t.Parallel()

	treeID := id.NewTree()
	deviceID := uuid.New()
	obsStub := &stubObservationService{}
	h := NewRouter(Deps{
		Devices:      &stubDeviceStore{},
		Trees:        &stubTreeWithByID{tree: &store.Tree{ID: treeID, Species: "jacaranda"}},
		Observations: obsStub,
	})

	req := httptest.NewRequest(http.MethodPost,
		"/trees/"+treeID.String()+"/observations",
		strings.NewReader("bloom_state=full"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: DeviceCookieName, Value: deviceID.String()})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201 (body=%q)", rec.Code, rec.Body.String())
	}
	if obsStub.inserts[0].ReportedBy != deviceID {
		t.Errorf("reported_by: got %v, want %v", obsStub.inserts[0].ReportedBy, deviceID)
	}
}

func TestPostObservation_500OnStoreError(t *testing.T) {
	t.Parallel()

	treeID := id.NewTree()
	obsStub := &stubObservationService{insertErr: errors.New("db down")}
	h := NewRouter(Deps{
		Devices:      &stubDeviceStore{},
		Trees:        &stubTreeWithByID{tree: &store.Tree{ID: treeID, Species: "jacaranda"}},
		Observations: obsStub,
	})

	form := url.Values{}
	form.Set("bloom_state", "full")
	rec := postObsForm(t, h, treeID, form)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", rec.Code)
	}
}
