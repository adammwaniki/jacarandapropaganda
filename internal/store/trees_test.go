//go:build integration

package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/adammwaniki/jacarandapropaganda/internal/geo"
	"github.com/adammwaniki/jacarandapropaganda/internal/id"
	"github.com/adammwaniki/jacarandapropaganda/internal/store"
)

// Representative jacaranda-lined locations in Nairobi. Used in dedup tests
// to guarantee the 3m radius behaves correctly across different latitudes,
// so a bug that assumes meters == degrees gets caught on the equator.
var nairobiFixtures = []struct {
	name     string
	lat, lng float64
}{
	{"CBD", -1.2921, 36.8219},
	{"Karura", -1.2393, 36.8347},
	{"Westlands", -1.2670, 36.8120},
	{"Karen", -1.3194, 36.7074},
}

// ---------- Insert and read back ----------------------------------------

func TestTreeStore_InsertAndByID(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)

	treeID := id.NewTree()
	if err := ts.Insert(ctx, store.InsertTreeParams{
		ID:        treeID,
		Lat:       -1.2921,
		Lng:       36.8219,
		Species:   "jacaranda",
		CreatedBy: dev,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := ts.ByID(ctx, treeID)
	if err != nil {
		t.Fatalf("by id: %v", err)
	}
	if got.ID != treeID {
		t.Errorf("id: got %v, want %v", got.ID, treeID)
	}
	if got.Species != "jacaranda" {
		t.Errorf("species: got %q, want jacaranda", got.Species)
	}
	if got.CreatedByDevice != dev {
		t.Errorf("created_by: got %v, want %v", got.CreatedByDevice, dev)
	}
	if got.HiddenAt != nil {
		t.Errorf("hidden_at must be nil on creation, got %v", *got.HiddenAt)
	}
	if got.CreatedAt.IsZero() {
		t.Errorf("created_at must be set")
	}
	// Location must round-trip to within ~1e-6 deg (~10cm), well under 1m.
	if absf(got.Lat - -1.2921) > 1e-6 {
		t.Errorf("lat round-trip: got %.7f, want %.7f", got.Lat, -1.2921)
	}
	if absf(got.Lng-36.8219) > 1e-6 {
		t.Errorf("lng round-trip: got %.7f, want %.7f", got.Lng, 36.8219)
	}
}

func TestTreeStore_InsertRejectsNonV7ID(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)

	// A UUIDv4 would undermine the index-locality rationale for trees.
	if err := ts.Insert(ctx, store.InsertTreeParams{
		ID:        uuid.New(), // v4
		Lat:       -1.2921,
		Lng:       36.8219,
		Species:   "jacaranda",
		CreatedBy: dev,
	}); err == nil {
		t.Fatalf("expected error for UUIDv4 tree id, got nil")
	}
}

func TestTreeStore_PopulatesH3Cells(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)

	for _, fx := range nairobiFixtures {
		fx := fx
		t.Run(fx.name, func(t *testing.T) {
			treeID := id.NewTree()
			if err := ts.Insert(ctx, store.InsertTreeParams{
				ID: treeID, Lat: fx.lat, Lng: fx.lng,
				Species: "jacaranda", CreatedBy: dev,
			}); err != nil {
				t.Fatalf("insert: %v", err)
			}

			var r9, r7 int64
			if err := db.QueryRowContext(ctx,
				`SELECT h3_cell_r9, h3_cell_r7 FROM trees WHERE id = $1`,
				treeID,
			).Scan(&r9, &r7); err != nil {
				t.Fatalf("query cells: %v", err)
			}
			if r9 == 0 {
				t.Errorf("h3_cell_r9 must be populated, got 0")
			}
			if r7 == 0 {
				t.Errorf("h3_cell_r7 must be populated, got 0")
			}
			// r9 is finer than r7, so their cells must differ for any real point.
			if r9 == r7 {
				t.Errorf("r9 and r7 cells should differ: both %d", r9)
			}
		})
	}
}

// ---------- Dedup (spec's central spatial invariant) ---------------------

func TestTreeStore_Candidates_Within3mReturns(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)

	for _, fx := range nairobiFixtures {
		fx := fx
		t.Run(fx.name, func(t *testing.T) {
			seedID := id.NewTree()
			mustInsertTree(t, ctx, ts, seedID, fx.lat, fx.lng, dev)

			// 2.5m to the north.
			lat2, lng2 := geo.Offset(fx.lat, fx.lng, 2.5, 0)
			cands, err := ts.Candidates(ctx, lat2, lng2, "jacaranda", 3.0)
			if err != nil {
				t.Fatalf("candidates: %v", err)
			}
			if len(cands) != 1 {
				t.Fatalf("want 1 candidate at 2.5m, got %d", len(cands))
			}
			if cands[0].Tree.ID != seedID {
				t.Errorf("candidate id: got %v, want %v", cands[0].Tree.ID, seedID)
			}
			if cands[0].DistanceMeters < 2.0 || cands[0].DistanceMeters > 3.0 {
				t.Errorf("distance: got %.2fm, expected ~2.5m", cands[0].DistanceMeters)
			}
		})
	}
}

func TestTreeStore_Candidates_Beyond3mReturnsNothing(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)

	for _, fx := range nairobiFixtures {
		fx := fx
		t.Run(fx.name, func(t *testing.T) {
			mustInsertTree(t, ctx, ts, id.NewTree(), fx.lat, fx.lng, dev)

			// 3.5m away — past the 3.0m boundary.
			lat2, lng2 := geo.Offset(fx.lat, fx.lng, 3.5, 0)
			cands, err := ts.Candidates(ctx, lat2, lng2, "jacaranda", 3.0)
			if err != nil {
				t.Fatalf("candidates: %v", err)
			}
			if len(cands) != 0 {
				t.Fatalf("want 0 candidates at 3.5m, got %d", len(cands))
			}
		})
	}
}

func TestTreeStore_Candidates_ScopedBySpecies(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)

	// A jacaranda and a (hypothetical future) nandi_flame at the same spot.
	mustInsertTreeSpecies(t, ctx, ts, id.NewTree(), -1.2921, 36.8219, "jacaranda", dev)
	mustInsertTreeSpecies(t, ctx, ts, id.NewTree(), -1.2921, 36.8219, "nandi_flame", dev)

	cands, err := ts.Candidates(ctx, -1.29211, 36.82191, "jacaranda", 3.0)
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("species filter failed: got %d candidates, want 1", len(cands))
	}
	if cands[0].Tree.Species != "jacaranda" {
		t.Errorf("species: got %q, want jacaranda", cands[0].Tree.Species)
	}
}

func TestTreeStore_Candidates_IgnoresHidden(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)

	hiddenID := id.NewTree()
	mustInsertTree(t, ctx, ts, hiddenID, -1.2921, 36.8219, dev)
	if err := ts.Hide(ctx, hiddenID); err != nil {
		t.Fatalf("hide: %v", err)
	}

	cands, err := ts.Candidates(ctx, -1.29211, 36.82191, "jacaranda", 3.0)
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	if len(cands) != 0 {
		t.Fatalf("hidden trees must be excluded from dedup: got %d", len(cands))
	}
}

// ---------- Bbox viewport reads -----------------------------------------

func TestTreeStore_ByBbox_ReturnsInsideExcludesOutside(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)

	// Nairobi-sized box.
	bbox := geo.Bbox{MinLon: 36.6, MinLat: -1.4, MaxLon: 37.0, MaxLat: -1.1}

	// Five inside, three outside.
	inside := []struct{ lat, lng float64 }{
		{-1.2921, 36.8219},
		{-1.2670, 36.8120},
		{-1.3194, 36.7074},
		{-1.2393, 36.8347},
		{-1.3500, 36.9000},
	}
	outside := []struct{ lat, lng float64 }{
		{-1.5000, 36.8000}, // south of box
		{-1.2000, 37.1000}, // east of box
		{-1.2000, 36.5000}, // west of box
	}
	for _, p := range inside {
		mustInsertTree(t, ctx, ts, id.NewTree(), p.lat, p.lng, dev)
	}
	for _, p := range outside {
		mustInsertTree(t, ctx, ts, id.NewTree(), p.lat, p.lng, dev)
	}

	results, err := ts.ByBbox(ctx, bbox)
	if err != nil {
		t.Fatalf("bybbox: %v", err)
	}
	if len(results) != len(inside) {
		t.Errorf("bbox hits: got %d, want %d", len(results), len(inside))
	}
}

func TestTreeStore_ByBbox_ExcludesHidden(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)
	bbox := geo.Bbox{MinLon: 36.6, MinLat: -1.4, MaxLon: 37.0, MaxLat: -1.1}

	visibleID := id.NewTree()
	hiddenID := id.NewTree()
	mustInsertTree(t, ctx, ts, visibleID, -1.2921, 36.8219, dev)
	mustInsertTree(t, ctx, ts, hiddenID, -1.2800, 36.8100, dev)
	if err := ts.Hide(ctx, hiddenID); err != nil {
		t.Fatalf("hide: %v", err)
	}

	results, err := ts.ByBbox(ctx, bbox)
	if err != nil {
		t.Fatalf("bybbox: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 visible tree, got %d", len(results))
	}
	if results[0].Tree.ID != visibleID {
		t.Errorf("wrong tree returned: got %v, want %v", results[0].Tree.ID, visibleID)
	}
}

// ---------- helpers ------------------------------------------------------

func mustUpsertDevice(t *testing.T, ctx context.Context, db *sql.DB) uuid.UUID {
	t.Helper()
	ds := store.NewDeviceStore(db)
	d := id.NewDevice()
	if err := ds.Upsert(ctx, d); err != nil {
		t.Fatalf("upsert device: %v", err)
	}
	return d
}

func mustInsertTree(t *testing.T, ctx context.Context, ts *store.TreeStore,
	treeID uuid.UUID, lat, lng float64, dev uuid.UUID) {
	t.Helper()
	mustInsertTreeSpecies(t, ctx, ts, treeID, lat, lng, "jacaranda", dev)
}

func mustInsertTreeSpecies(t *testing.T, ctx context.Context, ts *store.TreeStore,
	treeID uuid.UUID, lat, lng float64, species string, dev uuid.UUID) {
	t.Helper()
	if err := ts.Insert(ctx, store.InsertTreeParams{
		ID: treeID, Lat: lat, Lng: lng, Species: species, CreatedBy: dev,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
