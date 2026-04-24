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

	"github.com/adammwaniki/jacarandapropaganda/internal/geo"
	"github.com/adammwaniki/jacarandapropaganda/internal/id"
	"github.com/adammwaniki/jacarandapropaganda/internal/store"
)

// stubTreeService captures calls and lets tests script return values.
type stubTreeService struct {
	candidates      []store.CandidateTree
	candidatesErr   error
	lastCandidatesQ struct {
		lat, lng, radius float64
		species          string
	}
	insertErr error
	inserts   []struct {
		tp store.InsertTreeParams
		op store.InsertObservationParams
	}
}

func (s *stubTreeService) ByBbox(ctx context.Context, b geo.Bbox) ([]store.TreeWithState, error) {
	return nil, nil
}
func (s *stubTreeService) Candidates(ctx context.Context, lat, lng float64, species string, radius float64) ([]store.CandidateTree, error) {
	s.lastCandidatesQ.lat = lat
	s.lastCandidatesQ.lng = lng
	s.lastCandidatesQ.species = species
	s.lastCandidatesQ.radius = radius
	return s.candidates, s.candidatesErr
}
func (s *stubTreeService) InsertWithObservation(ctx context.Context,
	tp store.InsertTreeParams, op store.InsertObservationParams) error {
	s.inserts = append(s.inserts, struct {
		tp store.InsertTreeParams
		op store.InsertObservationParams
	}{tp, op})
	return s.insertErr
}
func (s *stubTreeService) ByID(ctx context.Context, idv uuid.UUID) (store.Tree, error) {
	return store.Tree{}, store.ErrNotFound
}

// postForm builds a request with a device cookie so the middleware has
// something to put into context.
func postForm(t *testing.T, h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path,
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: DeviceCookieName, Value: uuid.New().String()})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPostTrees_CreatesTreeWhenNoCandidates(t *testing.T) {
	t.Parallel()

	stub := &stubTreeService{candidates: nil}
	h := NewRouter(Deps{
		Devices: &stubDeviceStore{},
		Trees:   stub,
	})

	form := url.Values{}
	form.Set("lat", "-1.2921")
	form.Set("lng", "36.8219")
	form.Set("bloom_state", "full")
	// species omitted — default jacaranda

	rec := postForm(t, h, "/trees", form)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201 (body=%q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type: got %q, want text/html", ct)
	}
	if len(stub.inserts) != 1 {
		t.Fatalf("expected 1 insert, got %d", len(stub.inserts))
	}
	got := stub.inserts[0]
	if got.tp.Species != "jacaranda" {
		t.Errorf("species default: got %q, want jacaranda", got.tp.Species)
	}
	if got.tp.ID.Version() != 7 {
		t.Errorf("tree id must be v7, got v%d", got.tp.ID.Version())
	}
	if got.op.TreeID != got.tp.ID {
		t.Errorf("observation TreeID must match: tp.ID=%v op.TreeID=%v", got.tp.ID, got.op.TreeID)
	}
	if got.op.BloomState != store.BloomFull {
		t.Errorf("bloom_state: got %q, want full", got.op.BloomState)
	}
	// Dedup must run before insert.
	if stub.lastCandidatesQ.radius != 3.0 {
		t.Errorf("dedup radius: got %.2f, want 3.0", stub.lastCandidatesQ.radius)
	}
	if stub.lastCandidatesQ.species != "jacaranda" {
		t.Errorf("dedup species: got %q, want jacaranda", stub.lastCandidatesQ.species)
	}
}

func TestPostTrees_ReturnsDedupFragmentWhenCandidatesFound(t *testing.T) {
	t.Parallel()

	nearbyID := id.NewTree()
	photoKey := "photos/2026/04/nearby.jpg"
	stub := &stubTreeService{
		candidates: []store.CandidateTree{{
			TreeWithState: store.TreeWithState{
				Tree: store.Tree{ID: nearbyID, Species: "jacaranda", Lat: -1.2921, Lng: 36.8219},
				Latest: &store.Observation{
					BloomState: store.BloomPartial,
					PhotoR2Key: &photoKey,
				},
			},
			DistanceMeters: 2.4,
		}},
	}

	h := NewRouter(Deps{Devices: &stubDeviceStore{}, Trees: stub})

	form := url.Values{}
	form.Set("lat", "-1.29212")
	form.Set("lng", "36.82191")
	form.Set("bloom_state", "full")

	rec := postForm(t, h, "/trees", form)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (body=%q)", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "same tree") {
		t.Errorf("fragment should offer 'same tree' action, got body=%q", body)
	}
	if !strings.Contains(body, nearbyID.String()) {
		t.Errorf("fragment should include candidate id, got %q", body)
	}
	if !strings.Contains(lower, "2.4") {
		t.Errorf("fragment should show distance in meters, got %q", body)
	}
	if len(stub.inserts) != 0 {
		t.Errorf("no insert should happen when candidates found, got %d", len(stub.inserts))
	}
}

func TestPostTrees_ForceSkipsDedup(t *testing.T) {
	t.Parallel()

	stub := &stubTreeService{
		// Even with candidates present, force=1 must skip them.
		candidates: []store.CandidateTree{{
			TreeWithState:  store.TreeWithState{Tree: store.Tree{ID: id.NewTree()}},
			DistanceMeters: 1.0,
		}},
	}
	h := NewRouter(Deps{Devices: &stubDeviceStore{}, Trees: stub})

	form := url.Values{}
	form.Set("lat", "-1.2921")
	form.Set("lng", "36.8219")
	form.Set("bloom_state", "full")
	form.Set("force", "1")

	rec := postForm(t, h, "/trees", form)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201 (body=%q)", rec.Code, rec.Body.String())
	}
	if len(stub.inserts) != 1 {
		t.Errorf("force should insert anyway, got %d inserts", len(stub.inserts))
	}
}

func TestPostTrees_RejectsInvalidInput(t *testing.T) {
	t.Parallel()

	cases := map[string]url.Values{
		"missing bloom":   {"lat": {"-1.29"}, "lng": {"36.82"}},
		"invalid bloom":   {"lat": {"-1.29"}, "lng": {"36.82"}, "bloom_state": {"exploding"}},
		"missing lat":     {"lng": {"36.82"}, "bloom_state": {"full"}},
		"missing lng":     {"lat": {"-1.29"}, "bloom_state": {"full"}},
		"lat oor":         {"lat": {"-200"}, "lng": {"36.82"}, "bloom_state": {"full"}},
		"lng oor":         {"lat": {"-1.29"}, "lng": {"400"}, "bloom_state": {"full"}},
		"non numeric lat": {"lat": {"abc"}, "lng": {"36.82"}, "bloom_state": {"full"}},
	}
	h := NewRouter(Deps{Devices: &stubDeviceStore{}, Trees: &stubTreeService{}})

	for name, form := range cases {
		name, form := name, form
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := postForm(t, h, "/trees", form)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status: got %d, want 400 (body=%q)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestPostTrees_PropagatesInsertFailureAsServerError(t *testing.T) {
	t.Parallel()

	stub := &stubTreeService{insertErr: errors.New("db down")}
	h := NewRouter(Deps{Devices: &stubDeviceStore{}, Trees: stub})

	form := url.Values{}
	form.Set("lat", "-1.2921")
	form.Set("lng", "36.8219")
	form.Set("bloom_state", "full")

	rec := postForm(t, h, "/trees", form)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status: got %d, want 500", rec.Code)
	}
}

func TestPostTrees_StampsDeviceFromContext(t *testing.T) {
	t.Parallel()

	deviceID := uuid.New()
	stub := &stubTreeService{}
	h := NewRouter(Deps{Devices: &stubDeviceStore{}, Trees: stub})

	req := httptest.NewRequest(http.MethodPost, "/trees",
		strings.NewReader((url.Values{
			"lat": {"-1.2921"}, "lng": {"36.8219"}, "bloom_state": {"full"},
		}).Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: DeviceCookieName, Value: deviceID.String()})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: got %d, want 201 (body=%q)", rec.Code, rec.Body.String())
	}
	if len(stub.inserts) != 1 {
		t.Fatalf("want 1 insert, got %d", len(stub.inserts))
	}
	if stub.inserts[0].tp.CreatedBy != deviceID {
		t.Errorf("tree.CreatedBy: got %v, want %v", stub.inserts[0].tp.CreatedBy, deviceID)
	}
	if stub.inserts[0].op.ReportedBy != deviceID {
		t.Errorf("obs.ReportedBy: got %v, want %v", stub.inserts[0].op.ReportedBy, deviceID)
	}
}
