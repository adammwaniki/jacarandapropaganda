package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/adammwaniki/jacarandapropaganda/internal/geo"
	"github.com/adammwaniki/jacarandapropaganda/internal/store"
)

// TreeReader is the subset of the tree store used by read-only endpoints.
// Write-path collaborators extend this via TreeService in trees_post.go.
type TreeReader interface {
	ByBbox(ctx context.Context, b geo.Bbox) ([]store.TreeWithState, error)
}

// featureCollection is the GeoJSON root we emit for tree reads.
type featureCollection struct {
	Type     string    `json:"type"`
	Features []feature `json:"features"`
}

type feature struct {
	Type       string         `json:"type"`
	ID         string         `json:"id,omitempty"`
	Geometry   pointGeometry  `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

type pointGeometry struct {
	Type        string     `json:"type"`
	Coordinates [2]float64 `json:"coordinates"` // [lng, lat] per GeoJSON §3.1.1
}

// handleTreesBbox returns a handler that serves GET /trees?bbox=... from
// the given tree store. When trees is nil — e.g. from a test that has not
// seeded a store — the handler falls back to an empty FeatureCollection so
// walking-skeleton tests still pass.
func handleTreesBbox(trees TreeReader) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bbox, err := geo.ParseBbox(r.URL.Query().Get("bbox"))
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		var results []store.TreeWithState
		if trees != nil {
			results, err = trees.ByBbox(r.Context(), bbox)
			if err != nil {
				slog.ErrorContext(r.Context(), "trees by bbox", "err", err)
				writeError(w, http.StatusInternalServerError, "trees lookup failed")
				return
			}
		}

		fc := buildFeatureCollection(results)
		w.Header().Set("Content-Type", "application/geo+json; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=30")
		_ = json.NewEncoder(w).Encode(fc)
	})
}

func buildFeatureCollection(trees []store.TreeWithState) featureCollection {
	fc := featureCollection{Type: "FeatureCollection", Features: []feature{}}
	for _, tw := range trees {
		props := map[string]any{
			"species": tw.Tree.Species,
		}
		if tw.Latest != nil {
			props["bloom_state"] = string(tw.Latest.BloomState)
			props["observed_at"] = tw.Latest.ObservedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
			if tw.Latest.PhotoR2Key != nil {
				props["photo_key"] = *tw.Latest.PhotoR2Key
			}
		}
		fc.Features = append(fc.Features, feature{
			Type: "Feature",
			ID:   tw.Tree.ID.String(),
			Geometry: pointGeometry{
				Type:        "Point",
				Coordinates: [2]float64{tw.Tree.Lng, tw.Tree.Lat},
			},
			Properties: props,
		})
	}
	return fc
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
