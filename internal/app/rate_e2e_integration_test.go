//go:build integration

package app_test

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/adammwaniki/jacarandapropaganda/internal/app"
	"github.com/adammwaniki/jacarandapropaganda/internal/rate"
	"github.com/adammwaniki/jacarandapropaganda/internal/store"
	"github.com/adammwaniki/jacarandapropaganda/internal/store/testutil"
)

// TestEndToEnd_RateLimit_TreeCreateDevice exhausts the 10-per-device cap
// via HTTP and confirms the 11th request returns a 429 HTML fragment.
func TestEndToEnd_RateLimit_TreeCreateDevice(t *testing.T) {
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
		RateLimiter:  rate.NewLimiter(db),
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client := srv.Client()
	jar, _ := cookieJar()
	client.Jar = jar

	// Place 10 pins at distinct locations so dedup never fires.
	for i := 0; i < rate.TreePerDevicePer24h; i++ {
		form := url.Values{
			// Walk west ~50m per pin — well outside the 3m dedup radius.
			"lat":         {"-1.2921"},
			"lng":         {fmt.Sprintf("%.5f", 36.8000+float64(i)*0.0005)},
			"bloom_state": {"budding"},
		}
		resp, err := client.PostForm(srv.URL+"/trees", form)
		if err != nil {
			t.Fatalf("pin %d: %v", i+1, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("pin %d: status %d, want 201", i+1, resp.StatusCode)
		}
	}

	// 11th should return 429.
	form := url.Values{
		"lat": {"-1.2921"}, "lng": {"36.9000"}, "bloom_state": {"budding"},
	}
	resp, err := client.PostForm(srv.URL+"/trees", form)
	if err != nil {
		t.Fatalf("11th post: %v", err)
	}
	body := mustReadAll(t, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("11th post: status %d (want 429), body=%s", resp.StatusCode, body)
	}
	if ra := resp.Header.Get("Retry-After"); ra == "" {
		t.Errorf("Retry-After header missing on 429")
	}
	if !strings.Contains(strings.ToLower(body), "tomorrow") {
		t.Errorf("429 body should advise coming back tomorrow, got %q", body)
	}
}

// TestEndToEnd_RateLimit_ObservationNotBlockedByTreeLimit confirms that a
// user at their tree-create cap can still update bloom state on trees
// that exist. The two limits are independent (spec.md).
func TestEndToEnd_RateLimit_ObservationNotBlockedByTreeLimit(t *testing.T) {
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
		RateLimiter:  rate.NewLimiter(db),
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client := srv.Client()
	jar, _ := cookieJar()
	client.Jar = jar

	// Seed one tree we will update, then max out this device's tree-create cap.
	resp, err := client.PostForm(srv.URL+"/trees", url.Values{
		"lat": {"-1.2921"}, "lng": {"36.8219"}, "bloom_state": {"budding"},
	})
	if err != nil {
		t.Fatalf("seed tree: %v", err)
	}
	treeID := resp.Header.Get("X-Tree-Id")
	_ = resp.Body.Close()
	if treeID == "" {
		t.Fatal("seed tree: no X-Tree-Id header")
	}

	// That counted as 1 of 10 tree creates. Max out the other 9.
	for i := 0; i < rate.TreePerDevicePer24h-1; i++ {
		form := url.Values{
			"lat":         {"-1.2921"},
			"lng":         {fmt.Sprintf("%.5f", 36.8000+float64(i)*0.0005)},
			"bloom_state": {"budding"},
		}
		resp, err := client.PostForm(srv.URL+"/trees", form)
		if err != nil || resp.StatusCode != http.StatusCreated {
			t.Fatalf("fill pin %d: err=%v status=%d", i+1, err, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}

	// 11th tree creation must fail.
	resp, err = client.PostForm(srv.URL+"/trees", url.Values{
		"lat": {"-1.2921"}, "lng": {"36.9000"}, "bloom_state": {"budding"},
	})
	if err != nil || resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 on 11th tree, got status=%d err=%v", resp.StatusCode, err)
	}
	_ = resp.Body.Close()

	// But an observation on the existing tree must succeed.
	resp, err = client.PostForm(srv.URL+"/trees/"+treeID+"/observations",
		url.Values{"bloom_state": {"partial"}})
	if err != nil {
		t.Fatalf("obs: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("observation blocked by tree limit: status %d", resp.StatusCode)
	}
}
