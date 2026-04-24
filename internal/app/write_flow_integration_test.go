//go:build integration

package app_test

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/adammwaniki/jacarandapropaganda/internal/app"
	"github.com/adammwaniki/jacarandapropaganda/internal/store"
	"github.com/adammwaniki/jacarandapropaganda/internal/store/testutil"
)

// TestEndToEnd_WriteFlow drives the full Phase D pipeline:
//  1. First POST /trees creates a new tree + observation.
//  2. Second POST /trees within 3m returns a dedup fragment.
//  3. Submitting the "same tree" form lands an observation on the existing tree.
//  4. GET /trees/{id} renders the updated pin-detail fragment.
//  5. GET /trees?bbox=... shows the updated bloom state.
func TestEndToEnd_WriteFlow(t *testing.T) {
	dsn := testutil.NewTestDB(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.MigrateUp(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := app.NewRouter(app.Deps{
		Devices:        store.NewDeviceStore(db),
		Trees:          store.NewTreeStore(db),
		Observations:   store.NewObservationStore(db),
		PhotoURLPrefix: "https://cdn.example.com/",
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client := srv.Client()
	jar, err := cookieJar()
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client.Jar = jar

	// --- 1. First pin ---
	form := url.Values{}
	form.Set("lat", "-1.2921")
	form.Set("lng", "36.8219")
	form.Set("bloom_state", "budding")
	resp, err := client.PostForm(srv.URL+"/trees", form)
	if err != nil {
		t.Fatalf("first post: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first post: status %d, body=%s", resp.StatusCode, body)
	}
	firstTreeID := resp.Header.Get("X-Tree-Id")
	if firstTreeID == "" {
		t.Fatal("first post: X-Tree-Id header missing")
	}

	// --- 2. Second pin 1.5m away — triggers dedup ---
	form2 := url.Values{}
	form2.Set("lat", "-1.29211") // ~1m displacement
	form2.Set("lng", "36.82191") // ~1m displacement
	form2.Set("bloom_state", "full")
	resp2, err := client.PostForm(srv.URL+"/trees", form2)
	if err != nil {
		t.Fatalf("second post: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second post: status %d (want 200 dedup), body=%s", resp2.StatusCode, body2)
	}
	if !strings.Contains(string(body2), firstTreeID) {
		t.Errorf("dedup fragment must mention existing tree id; body=%s", body2)
	}
	if !strings.Contains(strings.ToLower(string(body2)), "same tree") {
		t.Errorf("dedup fragment must offer 'same tree' action; body=%s", body2)
	}

	// --- 3. "Same tree" submit: POST /trees/{firstTreeID}/observations ---
	form3 := url.Values{}
	form3.Set("bloom_state", "full")
	resp3, err := client.PostForm(
		srv.URL+"/trees/"+firstTreeID+"/observations", form3)
	if err != nil {
		t.Fatalf("same-tree post: %v", err)
	}
	body3, _ := io.ReadAll(resp3.Body)
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusCreated {
		t.Fatalf("same-tree post: status %d, body=%s", resp3.StatusCode, body3)
	}
	if !strings.Contains(string(body3), "full") {
		t.Errorf("pin-detail fragment should show new bloom state 'full'; body=%s", body3)
	}

	// --- 4. GET /trees/{id} renders latest state ---
	resp4, err := client.Get(srv.URL + "/trees/" + firstTreeID)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	body4, _ := io.ReadAll(resp4.Body)
	resp4.Body.Close()
	if resp4.StatusCode != http.StatusOK {
		t.Fatalf("get detail: status %d", resp4.StatusCode)
	}
	if !strings.Contains(string(body4), "full") {
		t.Errorf("detail should reflect 'full' bloom state after update; body=%s", body4)
	}

	// --- 5. GET /trees?bbox=... reflects updated bloom_state ---
	resp5, err := client.Get(srv.URL + "/trees?bbox=36.6,-1.4,37.0,-1.1")
	if err != nil {
		t.Fatalf("get bbox: %v", err)
	}
	body5, _ := io.ReadAll(resp5.Body)
	resp5.Body.Close()
	if resp5.StatusCode != http.StatusOK {
		t.Fatalf("get bbox: status %d", resp5.StatusCode)
	}
	var fc struct {
		Features []struct {
			ID         string         `json:"id"`
			Properties map[string]any `json:"properties"`
		} `json:"features"`
	}
	if err := json.Unmarshal(body5, &fc); err != nil {
		t.Fatalf("decode bbox: %v", err)
	}
	if len(fc.Features) != 1 {
		t.Fatalf("bbox: got %d features, want 1", len(fc.Features))
	}
	if got := fc.Features[0].Properties["bloom_state"]; got != "full" {
		t.Errorf("bbox bloom_state: got %v, want full", got)
	}
	if fc.Features[0].ID != firstTreeID {
		t.Errorf("bbox id: got %s, want %s", fc.Features[0].ID, firstTreeID)
	}
}

// TestEndToEnd_ForceSkipsDedup confirms that the "None of these — pin new"
// path actually creates a second, nearby tree when the user insists.
func TestEndToEnd_ForceSkipsDedup(t *testing.T) {
	dsn := testutil.NewTestDB(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.MigrateUp(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	handler := app.NewRouter(app.Deps{
		Devices:      store.NewDeviceStore(db),
		Trees:        store.NewTreeStore(db),
		Observations: store.NewObservationStore(db),
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client := srv.Client()
	jar, _ := cookieJar()
	client.Jar = jar

	first := url.Values{"lat": {"-1.2921"}, "lng": {"36.8219"}, "bloom_state": {"budding"}}
	resp, err := client.PostForm(srv.URL+"/trees", first)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("first: err=%v status=%d", err, resp.StatusCode)
	}
	resp.Body.Close()

	forced := url.Values{
		"lat": {"-1.29211"}, "lng": {"36.82191"},
		"bloom_state": {"full"}, "force": {"1"},
	}
	resp2, err := client.PostForm(srv.URL+"/trees", forced)
	if err != nil {
		t.Fatalf("forced post: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("forced post: status %d, want 201", resp2.StatusCode)
	}

	// bbox should now show two trees, one budding, one full.
	resp3, err := client.Get(srv.URL + "/trees?bbox=36.6,-1.4,37.0,-1.1")
	if err != nil {
		t.Fatalf("bbox: %v", err)
	}
	defer resp3.Body.Close()
	var fc struct {
		Features []struct {
			Properties map[string]any `json:"properties"`
		} `json:"features"`
	}
	if err := json.NewDecoder(resp3.Body).Decode(&fc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(fc.Features) != 2 {
		t.Fatalf("bbox: got %d features, want 2", len(fc.Features))
	}
}
