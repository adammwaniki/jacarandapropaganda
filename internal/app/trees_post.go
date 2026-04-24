package app

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/adammwaniki/jacarandapropaganda/internal/id"
	"github.com/adammwaniki/jacarandapropaganda/internal/store"
)

// Dedup radius is load-bearing — see spec.md § Tree identity and
// deduplication. Do not widen without amending the spec first.
const dedupRadiusMeters = 3.0

// TreeWriter covers the write path the POST /trees handler depends on.
type TreeWriter interface {
	Candidates(ctx context.Context, lat, lng float64, species string, radiusMeters float64) ([]store.CandidateTree, error)
	InsertWithObservation(ctx context.Context, tp store.InsertTreeParams, op store.InsertObservationParams) error
}

// TreeService is the union of read and write capabilities the app layer
// uses on the tree store. Tests provide a stub; production uses
// *store.TreeStore.
type TreeService interface {
	TreeReader
	TreeWriter
	ByID(ctx context.Context, id uuidID) (store.Tree, error)
}

// uuidID is a local alias so we can keep the handler's import list short;
// aliased to uuid.UUID via google/uuid elsewhere.
type uuidID = uuidFromGoogle

// handlePostTrees implements POST /trees. Two outcomes:
//   - dedup candidates found and no force flag → 200 + HTML comparison sheet
//   - otherwise → 201 + HTML "created" fragment with X-Tree-Id header
func handlePostTrees(trees TreeService, photoURLPrefix string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := parsePostTreeForm(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		deviceID, ok := DeviceFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusInternalServerError, "missing device context")
			return
		}

		if !p.force {
			cands, err := trees.Candidates(r.Context(), p.lat, p.lng, p.species, dedupRadiusMeters)
			if err != nil {
				slog.ErrorContext(r.Context(), "candidates lookup", "err", err)
				writeError(w, http.StatusInternalServerError, "candidates lookup failed")
				return
			}
			if len(cands) > 0 {
				renderDedupSheet(w, r, cands, p, photoURLPrefix)
				return
			}
		}

		treeID := id.NewTree()
		obsID := id.NewObservation()
		var photoPtr *string
		if p.photoKey != "" {
			photoPtr = &p.photoKey
		}
		if err := trees.InsertWithObservation(r.Context(),
			store.InsertTreeParams{
				ID: treeID, Lat: p.lat, Lng: p.lng,
				Species: p.species, CreatedBy: deviceID,
			},
			store.InsertObservationParams{
				ID: obsID, TreeID: treeID, BloomState: p.bloomState,
				PhotoR2Key: photoPtr, ReportedBy: deviceID,
			},
		); err != nil {
			slog.ErrorContext(r.Context(), "insert tree+obs", "err", err)
			writeError(w, http.StatusInternalServerError, "insert failed")
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Tree-Id", treeID.String())
		w.WriteHeader(http.StatusCreated)
		var buf bytes.Buffer
		if err := renderFragment(&buf, "tree_created.html", map[string]any{"ID": treeID}); err != nil {
			// Headers already written; log and give up on the body.
			slog.ErrorContext(r.Context(), "render created fragment", "err", err)
			return
		}
		_, _ = w.Write(buf.Bytes())
	})
}

func renderDedupSheet(w http.ResponseWriter, r *http.Request,
	cands []store.CandidateTree, p postTreeForm, photoURLPrefix string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	var buf bytes.Buffer
	data := map[string]any{
		"Candidates":     cands,
		"Lat":            p.lat,
		"Lng":            p.lng,
		"Species":        p.species,
		"BloomState":     string(p.bloomState),
		"PhotoKey":       p.photoKey,
		"PhotoURLPrefix": photoURLPrefix,
	}
	if err := renderFragment(&buf, "dedup.html", data); err != nil {
		slog.ErrorContext(r.Context(), "render dedup fragment", "err", err)
		return
	}
	_, _ = w.Write(buf.Bytes())
}

type postTreeForm struct {
	lat, lng   float64
	species    string
	bloomState store.BloomState
	photoKey   string
	force      bool
}

func parsePostTreeForm(r *http.Request) (postTreeForm, error) {
	if err := r.ParseForm(); err != nil {
		return postTreeForm{}, httpError{http.StatusBadRequest, "malformed form: " + err.Error()}
	}
	latS := r.PostForm.Get("lat")
	lngS := r.PostForm.Get("lng")
	if latS == "" || lngS == "" {
		return postTreeForm{}, httpError{http.StatusBadRequest, "lat and lng are required"}
	}
	lat, err := strconv.ParseFloat(latS, 64)
	if err != nil {
		return postTreeForm{}, httpError{http.StatusBadRequest, "lat is not a number"}
	}
	lng, err := strconv.ParseFloat(lngS, 64)
	if err != nil {
		return postTreeForm{}, httpError{http.StatusBadRequest, "lng is not a number"}
	}
	if lat < -90 || lat > 90 {
		return postTreeForm{}, httpError{http.StatusBadRequest, "lat out of range"}
	}
	if lng < -180 || lng > 180 {
		return postTreeForm{}, httpError{http.StatusBadRequest, "lng out of range"}
	}

	species := r.PostForm.Get("species")
	if species == "" {
		species = "jacaranda"
	}

	bloom := store.BloomState(r.PostForm.Get("bloom_state"))
	if !bloom.Valid() {
		return postTreeForm{}, httpError{http.StatusBadRequest, "bloom_state must be one of budding, partial, full, fading"}
	}

	return postTreeForm{
		lat:        lat,
		lng:        lng,
		species:    species,
		bloomState: bloom,
		photoKey:   r.PostForm.Get("photo_key"),
		force:      r.PostForm.Get("force") == "1",
	}, nil
}

// httpError carries a status code through strings.Error so parse helpers
// can return a single value and the handler still responds with the right
// status. Only used internally.
type httpError struct {
	status int
	msg    string
}

func (e httpError) Error() string { return e.msg }
