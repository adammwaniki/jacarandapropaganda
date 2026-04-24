package app

import (
	"encoding/json"
	"net/http"
)

// Deps bundles the collaborators the HTTP layer needs. Keeping it as a
// plain struct keeps call sites legible and makes tests easy to seed with
// focused stubs.
type Deps struct {
	Devices        DeviceUpserter
	Trees          TreeService
	Observations   ObservationService
	RateLimiter    RateLimiter // may be nil in tests; nil means "no limits"
	PhotoURLPrefix string
}

// NewRouter builds the top-level handler. The device middleware is wrapped
// around the full app routes but not around /health — liveness probes must
// not mutate the devices table.
func NewRouter(deps Deps) http.Handler {
	withDevice := WithDevice(deps.Devices)

	app := http.NewServeMux()
	app.HandleFunc("GET /", handleIndex)
	app.Handle("GET /trees", handleTreesBbox(deps.Trees))
	app.Handle("POST /trees",
		handlePostTrees(deps.Trees, deps.RateLimiter, deps.PhotoURLPrefix))
	app.Handle("GET /trees/{id}",
		handleTreeDetail(deps.Trees, deps.Observations, deps.PhotoURLPrefix))
	app.Handle("POST /trees/{id}/observations",
		handlePostObservation(deps.Trees, deps.Observations, deps.RateLimiter, deps.PhotoURLPrefix))

	top := http.NewServeMux()
	top.HandleFunc("GET /health", handleHealth)
	top.Handle("/", withDevice(app))
	return top
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
