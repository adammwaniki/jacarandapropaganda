package app

import (
	"encoding/json"
	"net/http"

	"github.com/adammwaniki/jacarandapropaganda/internal/geo"
)

// featureCollection is the GeoJSON root we emit for tree reads.
// Extensions in later phases may attach foreign members; keep it minimal.
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
	Coordinates [2]float64 `json:"coordinates"`
}

// handleTrees is the read path: GET /trees?bbox=minLon,minLat,maxLon,maxLat.
// Walking skeleton — returns an empty FeatureCollection until the trees
// repository is wired in Phase C.
func handleTrees(w http.ResponseWriter, r *http.Request) {
	bboxParam := r.URL.Query().Get("bbox")
	if _, err := geo.ParseBbox(bboxParam); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	fc := featureCollection{
		Type:     "FeatureCollection",
		Features: []feature{},
	}
	w.Header().Set("Content-Type", "application/geo+json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=30")
	_ = json.NewEncoder(w).Encode(fc)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
