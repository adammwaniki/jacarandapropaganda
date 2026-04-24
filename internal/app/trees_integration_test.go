//go:build integration

package app_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/adammwaniki/jacarandapropaganda/internal/app"
	"github.com/adammwaniki/jacarandapropaganda/internal/id"
	"github.com/adammwaniki/jacarandapropaganda/internal/store"
	"github.com/adammwaniki/jacarandapropaganda/internal/store/testutil"
)

func TestEndToEnd_TreesBboxReturnsSeededData(t *testing.T) {
	dsn := testutil.NewTestDB(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.MigrateUp(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ds := store.NewDeviceStore(db)
	ts := store.NewTreeStore(db)
	os := store.NewObservationStore(db)

	dev := id.NewDevice()
	if err := ds.Upsert(ctx, dev); err != nil {
		t.Fatalf("upsert device: %v", err)
	}

	insertTree := func(treeID uuid.UUID, lat, lng float64) {
		t.Helper()
		if err := ts.Insert(ctx, store.InsertTreeParams{
			ID: treeID, Lat: lat, Lng: lng,
			Species: "jacaranda", CreatedBy: dev,
		}); err != nil {
			t.Fatalf("insert tree: %v", err)
		}
	}

	a := id.NewTree()
	b := id.NewTree()
	hidden := id.NewTree()
	outside := id.NewTree()

	insertTree(a, -1.2921, 36.8219)
	insertTree(b, -1.2670, 36.8120)
	insertTree(hidden, -1.3000, 36.8300)
	insertTree(outside, -1.5000, 36.8000) // south of bbox below

	photoKey := "photos/2026/04/abc.jpg"
	if err := os.Insert(ctx, store.InsertObservationParams{
		ID: id.NewObservation(), TreeID: a, BloomState: store.BloomFull,
		PhotoR2Key: &photoKey, ReportedBy: dev,
	}); err != nil {
		t.Fatalf("insert obs: %v", err)
	}
	if err := ts.Hide(ctx, hidden); err != nil {
		t.Fatalf("hide: %v", err)
	}

	handler := app.NewRouter(app.Deps{Devices: ds, Trees: ts})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/trees?bbox=36.6,-1.4,37.0,-1.1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct == "" || ct[:20] != "application/geo+json" {
		t.Errorf("content-type: got %q, want application/geo+json", ct)
	}

	var fc struct {
		Type     string `json:"type"`
		Features []struct {
			Type     string `json:"type"`
			ID       string `json:"id"`
			Geometry struct {
				Type        string     `json:"type"`
				Coordinates [2]float64 `json:"coordinates"`
			} `json:"geometry"`
			Properties map[string]any `json:"properties"`
		} `json:"features"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&fc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if fc.Type != "FeatureCollection" {
		t.Errorf("type: got %q, want FeatureCollection", fc.Type)
	}
	if len(fc.Features) != 2 {
		t.Fatalf("features: got %d, want 2 (visible inside bbox)", len(fc.Features))
	}

	var sawA bool
	for _, f := range fc.Features {
		if f.Type != "Feature" {
			t.Errorf("feature type: got %q, want Feature", f.Type)
		}
		if f.Geometry.Type != "Point" {
			t.Errorf("geometry type: got %q, want Point", f.Geometry.Type)
		}
		lng, lat := f.Geometry.Coordinates[0], f.Geometry.Coordinates[1]
		if lng < 36.6 || lng > 37.0 || lat < -1.4 || lat > -1.1 {
			t.Errorf("feature %s outside bbox: [%.4f, %.4f]", f.ID, lng, lat)
		}
		if f.ID == a.String() {
			sawA = true
			if got := f.Properties["bloom_state"]; got != "full" {
				t.Errorf("tree A bloom_state: got %v, want full", got)
			}
			if got := f.Properties["species"]; got != "jacaranda" {
				t.Errorf("tree A species: got %v, want jacaranda", got)
			}
			if _, ok := f.Properties["observed_at"].(string); !ok {
				t.Errorf("tree A missing observed_at string, properties=%v", f.Properties)
			}
		}
	}
	if !sawA {
		t.Errorf("feature with id %s not found in response", a)
	}
}

// TestEndToEnd_TreesBboxOmitsPropertiesForUnobserved ensures that a tree
// without any observation still renders (so users see their own just-placed
// pin) but without bloom_state populated.
func TestEndToEnd_TreesBboxOmitsPropertiesForUnobserved(t *testing.T) {
	dsn := testutil.NewTestDB(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.MigrateUp(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ds := store.NewDeviceStore(db)
	ts := store.NewTreeStore(db)
	dev := id.NewDevice()
	if err := ds.Upsert(ctx, dev); err != nil {
		t.Fatalf("upsert device: %v", err)
	}
	treeID := id.NewTree()
	if err := ts.Insert(ctx, store.InsertTreeParams{
		ID: treeID, Lat: -1.2921, Lng: 36.8219, Species: "jacaranda", CreatedBy: dev,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	handler := app.NewRouter(app.Deps{Devices: ds, Trees: ts})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/trees?bbox=36.6,-1.4,37.0,-1.1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	var fc struct {
		Features []struct {
			Properties map[string]any `json:"properties"`
		} `json:"features"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&fc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(fc.Features) != 1 {
		t.Fatalf("features: got %d, want 1", len(fc.Features))
	}
	if _, ok := fc.Features[0].Properties["bloom_state"]; ok {
		t.Errorf("unobserved tree must not carry bloom_state, got %v",
			fc.Features[0].Properties["bloom_state"])
	}
}
