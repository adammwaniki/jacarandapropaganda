package geo

import (
	"math"
	"testing"
)

func TestOffset_NorthIsPositiveLat(t *testing.T) {
	t.Parallel()
	lat, lng := Offset(-1.2921, 36.8219, 100, 0)
	if lat <= -1.2921 {
		t.Errorf("north offset must increase latitude: got %.7f, was -1.2921", lat)
	}
	if math.Abs(lng-36.8219) > 1e-9 {
		t.Errorf("north offset must not change longitude: got %.7f, was 36.8219", lng)
	}
}

func TestOffset_EastIsPositiveLng(t *testing.T) {
	t.Parallel()
	lat, lng := Offset(-1.2921, 36.8219, 100, math.Pi/2)
	if lng <= 36.8219 {
		t.Errorf("east offset must increase longitude: got %.7f", lng)
	}
	if math.Abs(lat - -1.2921) > 1e-9 {
		t.Errorf("east offset must not change latitude: got %.7f", lat)
	}
}

func TestOffset_DistanceApproximatelyMatches(t *testing.T) {
	t.Parallel()
	origLat, origLng := -1.2921, 36.8219
	// 100 meters north.
	lat, lng := Offset(origLat, origLng, 100, 0)
	// Haversine check.
	d := haversine(origLat, origLng, lat, lng)
	if math.Abs(d-100) > 1 {
		t.Errorf("offset distance: got %.2fm, want 100 ± 1", d)
	}
}

func haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6_371_000.0
	φ1 := lat1 * math.Pi / 180
	φ2 := lat2 * math.Pi / 180
	dφ := (lat2 - lat1) * math.Pi / 180
	dλ := (lng2 - lng1) * math.Pi / 180
	a := math.Sin(dφ/2)*math.Sin(dφ/2) +
		math.Cos(φ1)*math.Cos(φ2)*math.Sin(dλ/2)*math.Sin(dλ/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}
