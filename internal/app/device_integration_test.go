//go:build integration

package app_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/adammwaniki/jacarandapropaganda/internal/app"
	"github.com/adammwaniki/jacarandapropaganda/internal/store"
	"github.com/adammwaniki/jacarandapropaganda/internal/store/testutil"
)

// TestEndToEnd_DeviceCookieRoundTrip drives the full stack — router +
// middleware + real DeviceStore + real Postgres. First GET issues a cookie
// and writes a row; second GET with that cookie hits the same row and
// advances last_seen. This is the device-identity exit criterion for Phase B.
func TestEndToEnd_DeviceCookieRoundTrip(t *testing.T) {
	dsn := testutil.NewTestDB(t)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := store.MigrateUp(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ds := store.NewDeviceStore(db)
	handler := app.NewRouter(app.Deps{Devices: ds})

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	client := ts.Client()
	client.Jar = newCookieJar(t, ts.URL)

	// --- First request: no cookie ---
	resp, err := client.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("first GET: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first GET status: got %d, want 200", resp.StatusCode)
	}

	id := deviceIDFromJar(t, client.Jar, ts.URL)
	if id.Version() != 4 {
		t.Fatalf("cookie version: got v%d, want v4", id.Version())
	}

	// Confirm the device row exists.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := ds.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after first request: %v", err)
	}

	// --- Second request: reuses cookie ---
	time.Sleep(20 * time.Millisecond)

	resp2, err := client.Get(ts.URL + "/trees?bbox=36.6,-1.4,37.0,-1.1")
	if err != nil {
		t.Fatalf("second GET: %v", err)
	}
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("second GET status: got %d, want 200", resp2.StatusCode)
	}

	idAfter := deviceIDFromJar(t, client.Jar, ts.URL)
	if idAfter != id {
		t.Errorf("cookie changed between requests: first=%v second=%v", id, idAfter)
	}

	second, err := ds.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get after second request: %v", err)
	}
	if !second.FirstSeen.Equal(first.FirstSeen) {
		t.Errorf("first_seen should not move: first=%v second=%v",
			first.FirstSeen, second.FirstSeen)
	}
	if !second.LastSeen.After(first.LastSeen) {
		t.Errorf("last_seen should advance: first=%v second=%v",
			first.LastSeen, second.LastSeen)
	}
}

// TestEndToEnd_HealthDoesNotUpsertDevice keeps liveness probes out of the
// devices table. If this ever starts to fail, Uptime Kuma would churn the
// table every 30s forever.
func TestEndToEnd_HealthDoesNotUpsertDevice(t *testing.T) {
	dsn := testutil.NewTestDB(t)

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.MigrateUp(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ds := store.NewDeviceStore(db)
	handler := app.NewRouter(app.Deps{Devices: ds})
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	for i := 0; i < 5; i++ {
		resp, err := ts.Client().Get(ts.URL + "/health")
		if err != nil {
			t.Fatalf("health get: %v", err)
		}
		_ = resp.Body.Close()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices`).Scan(&count); err != nil {
		t.Fatalf("count devices: %v", err)
	}
	if count != 0 {
		t.Errorf("/health populated devices table: %d rows, want 0", count)
	}
}

// --- test helpers ---------------------------------------------------------

func newCookieJar(t *testing.T, _ string) http.CookieJar {
	t.Helper()
	jar, err := cookieJar()
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return jar
}

func deviceIDFromJar(t *testing.T, jar http.CookieJar, serverURL string) uuid.UUID {
	t.Helper()
	u := parseURL(t, serverURL)
	for _, c := range jar.Cookies(u) {
		if c.Name == app.DeviceCookieName {
			id, err := uuid.Parse(c.Value)
			if err != nil {
				t.Fatalf("cookie value not UUID: %v", err)
			}
			return id
		}
	}
	t.Fatalf("no %q cookie in jar", app.DeviceCookieName)
	return uuid.Nil
}
