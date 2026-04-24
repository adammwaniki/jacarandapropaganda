// Package geo holds pure spatial helpers: bbox parsing, GeoJSON encoding,
// H3 cell math. No database access.
package geo

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Bbox is a WGS-84 bounding box. Longitudes in [-180, 180], latitudes in
// [-90, 90]. Semantics: min corner inclusive, max corner inclusive.
type Bbox struct {
	MinLon float64
	MinLat float64
	MaxLon float64
	MaxLat float64
}

// ParseBbox parses a "minLon,minLat,maxLon,maxLat" string as used by
// standard web-mapping APIs. The ordering follows the GeoJSON bbox
// convention (RFC 7946 §5). Rejects inverted, out-of-range, and
// malformed inputs.
func ParseBbox(s string) (Bbox, error) {
	if s == "" {
		return Bbox{}, errors.New("bbox is required")
	}
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return Bbox{}, fmt.Errorf("bbox must have 4 comma-separated values, got %d", len(parts))
	}
	vals := make([]float64, 4)
	for i, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return Bbox{}, fmt.Errorf("bbox value %d: %w", i, err)
		}
		vals[i] = v
	}
	b := Bbox{MinLon: vals[0], MinLat: vals[1], MaxLon: vals[2], MaxLat: vals[3]}

	if b.MinLon < -180 || b.MinLon > 180 || b.MaxLon < -180 || b.MaxLon > 180 {
		return Bbox{}, errors.New("longitude out of range [-180, 180]")
	}
	if b.MinLat < -90 || b.MinLat > 90 || b.MaxLat < -90 || b.MaxLat > 90 {
		return Bbox{}, errors.New("latitude out of range [-90, 90]")
	}
	if b.MinLon >= b.MaxLon {
		return Bbox{}, errors.New("minLon must be strictly less than maxLon")
	}
	if b.MinLat >= b.MaxLat {
		return Bbox{}, errors.New("minLat must be strictly less than maxLat")
	}
	return b, nil
}
