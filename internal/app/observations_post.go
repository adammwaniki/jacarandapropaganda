package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/adammwaniki/jacarandapropaganda/internal/id"
	"github.com/adammwaniki/jacarandapropaganda/internal/store"
)

// ObservationService is the write + read surface the observations handler
// depends on. *store.ObservationStore satisfies it.
type ObservationService interface {
	Insert(ctx context.Context, p store.InsertObservationParams) error
	CurrentForTree(ctx context.Context, treeID uuid.UUID) (store.Observation, error)
	Hide(ctx context.Context, obsID uuid.UUID) error
}

// handlePostObservation implements POST /trees/{id}/observations. The tree
// must already exist; this is the "update bloom state" path for an existing
// pin. Returns 201 + a pin-detail HTML fragment reflecting the new state.
func handlePostObservation(trees TreeService, obs ObservationService, limiter RateLimiter, photoURLPrefix string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		treeIDStr := r.PathValue("id")
		treeID, err := uuid.Parse(treeIDStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid tree id")
			return
		}

		if err := r.ParseForm(); err != nil {
			writeError(w, http.StatusBadRequest, "malformed form")
			return
		}
		bloom := store.BloomState(r.PostForm.Get("bloom_state"))
		if !bloom.Valid() {
			writeError(w, http.StatusBadRequest,
				"bloom_state must be one of budding, partial, full, fading")
			return
		}
		photoKey := r.PostForm.Get("photo_key")

		deviceID, ok := DeviceFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusInternalServerError, "missing device context")
			return
		}

		if limiter != nil {
			if !enforceRateLimit(w, r, func() error {
				return limiter.CheckAndRecordObservationCreate(r.Context(), deviceID)
			}) {
				return
			}
		}

		tree, err := trees.ByID(r.Context(), treeID)
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "tree not found")
			return
		}
		if err != nil {
			slog.ErrorContext(r.Context(), "tree lookup", "err", err)
			writeError(w, http.StatusInternalServerError, "tree lookup failed")
			return
		}

		var photoPtr *string
		if photoKey != "" {
			photoPtr = &photoKey
		}
		ins := store.InsertObservationParams{
			ID: id.NewObservation(), TreeID: treeID,
			BloomState: bloom, PhotoR2Key: photoPtr,
			ReportedBy: deviceID,
		}
		if err := obs.Insert(r.Context(), ins); err != nil {
			slog.ErrorContext(r.Context(), "insert observation", "err", err)
			writeError(w, http.StatusInternalServerError, "insert failed")
			return
		}

		// Return the freshly-updated pin-detail fragment so the client
		// swap shows the new state without a follow-up round trip.
		latest, err := obs.CurrentForTree(r.Context(), treeID)
		var latestPtr *store.Observation
		if err == nil {
			latestPtr = &latest
		} else if !errors.Is(err, store.ErrNotFound) {
			slog.WarnContext(r.Context(), "current after insert", "err", err)
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Observation-Id", ins.ID.String())
		w.WriteHeader(http.StatusCreated)
		var buf bytes.Buffer
		if err := renderFragment(&buf, "pin_detail.html", map[string]any{
			"Tree":           tree,
			"Latest":         latestPtr,
			"PhotoURLPrefix": photoURLPrefix,
		}); err != nil {
			slog.ErrorContext(r.Context(), "render pin_detail", "err", err)
			return
		}
		_, _ = w.Write(buf.Bytes())
	})
}
