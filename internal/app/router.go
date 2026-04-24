package app

import (
	"encoding/json"
	"net/http"
)

// Deps bundles the collaborators the HTTP layer needs. Keeping it as a
// plain struct keeps call sites legible and makes tests easy to seed with
// focused stubs.
type Deps struct {
	Devices DeviceUpserter
	Trees   TreeReader
}

// NewRouter builds the top-level handler. The device middleware is wrapped
// around the full app routes but not around /health — liveness probes must
// not mutate the devices table.
func NewRouter(deps Deps) http.Handler {
	withDevice := WithDevice(deps.Devices)

	app := http.NewServeMux()
	app.HandleFunc("GET /", handleIndex)
	app.Handle("GET /trees", handleTreesBbox(deps.Trees))

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
