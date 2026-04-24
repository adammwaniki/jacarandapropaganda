package geo

import "math"

// earthRadiusMeters is the WGS-84 mean radius. Accurate enough for nudging
// a coordinate a few meters during tests; not a substitute for PostGIS.
const earthRadiusMeters = 6_371_000.0

// Offset returns (lat, lng) displaced from the given origin by distance
// meters along bearing radians (0 = north, π/2 = east). The approximation
// is the "flat earth" small-angle form — good to within a cm at <100m,
// which is all we need for seeding dedup test fixtures.
func Offset(lat, lng, meters, bearingRadians float64) (float64, float64) {
	dLat := meters * math.Cos(bearingRadians) / earthRadiusMeters
	dLng := meters * math.Sin(bearingRadians) / (earthRadiusMeters * math.Cos(lat*math.Pi/180))
	return lat + dLat*180/math.Pi, lng + dLng*180/math.Pi
}
