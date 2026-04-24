package app

import (
	"html/template"
	"net/http"
	"sync"

	"github.com/adammwaniki/jacarandapropaganda/web"
)

var (
	indexTmplOnce sync.Once
	indexTmpl     *template.Template
	indexTmplErr  error
)

func getIndexTmpl() (*template.Template, error) {
	indexTmplOnce.Do(func() {
		indexTmpl, indexTmplErr = template.ParseFS(web.Templates, "templates/index.html")
	})
	return indexTmpl, indexTmplErr
}

type indexData struct {
	// PMTilesURL is the full HTTPS URL to the Nairobi vector tiles on R2
	// (served through Cloudflare). Empty in local dev or tests, in which
	// case the map still renders with the background layer only.
	PMTilesURL string
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	// http.ServeMux's "GET /" pattern also matches unknown paths. Distinguish.
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	tmpl, err := getIndexTmpl()
	if err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, indexData{
		PMTilesURL: pmTilesURL(),
	})
}

// pmTilesURL is extracted so the handler stays pure for tests. In tests it
// returns "" unless JP_PMTILES_URL is set.
func pmTilesURL() string {
	// Deliberate read-through: no cached config yet. Phase A-level.
	return envOr("JP_PMTILES_URL", "")
}
