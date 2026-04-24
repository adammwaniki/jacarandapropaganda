package app

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/adammwaniki/jacarandapropaganda/internal/store"
)

// handleTreeDetail implements GET /trees/{id}. Returns the pin-detail HTML
// fragment. This is what Alpine AJAX swaps in when the user taps a pin.
func handleTreeDetail(trees TreeService, obs ObservationService, photoURLPrefix string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		treeID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid tree id")
			return
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

		var latestPtr *store.Observation
		latest, err := obs.CurrentForTree(r.Context(), treeID)
		if err == nil {
			latestPtr = &latest
		} else if !errors.Is(err, store.ErrNotFound) {
			slog.WarnContext(r.Context(), "current observation lookup", "err", err)
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		var buf bytes.Buffer
		if err := renderFragment(&buf, "pin_detail.html", map[string]any{
			"Tree":           tree,
			"Latest":         latestPtr,
			"PhotoURLPrefix": photoURLPrefix,
		}); err != nil {
			slog.ErrorContext(r.Context(), "render pin_detail", "err", err)
			writeError(w, http.StatusInternalServerError, "render failed")
			return
		}
		_, _ = w.Write(buf.Bytes())
	})
}
