//go:build integration

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/adammwaniki/jacarandapropaganda/internal/id"
	"github.com/adammwaniki/jacarandapropaganda/internal/store"
)

// TestTreeStore_InsertWithObservation_AtomicSuccess writes a tree and its
// first observation in one transaction. Both are visible after commit.
func TestTreeStore_InsertWithObservation_AtomicSuccess(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)
	os := store.NewObservationStore(db)

	treeID := id.NewTree()
	obsID := id.NewObservation()
	if err := ts.InsertWithObservation(ctx,
		store.InsertTreeParams{
			ID: treeID, Lat: -1.2921, Lng: 36.8219,
			Species: "jacaranda", CreatedBy: dev,
		},
		store.InsertObservationParams{
			ID: obsID, TreeID: treeID, BloomState: store.BloomFull, ReportedBy: dev,
		},
	); err != nil {
		t.Fatalf("insert with observation: %v", err)
	}

	got, err := ts.ByID(ctx, treeID)
	if err != nil {
		t.Fatalf("tree should exist: %v", err)
	}
	if got.Species != "jacaranda" {
		t.Errorf("species: got %q", got.Species)
	}
	obs, err := os.CurrentForTree(ctx, treeID)
	if err != nil {
		t.Fatalf("current obs: %v", err)
	}
	if obs.ID != obsID || obs.BloomState != store.BloomFull {
		t.Errorf("current obs: got id=%v bloom=%q, want id=%v bloom=full",
			obs.ID, obs.BloomState, obsID)
	}
}

// TestTreeStore_InsertWithObservation_RollsBackOnObservationFailure
// proves atomicity: if the observation insert fails (e.g. invalid bloom
// state sneaks past Go validation), the tree insert must not persist.
func TestTreeStore_InsertWithObservation_RollsBackOnObservationFailure(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)

	treeID := id.NewTree()
	err := ts.InsertWithObservation(ctx,
		store.InsertTreeParams{
			ID: treeID, Lat: -1.2921, Lng: 36.8219,
			Species: "jacaranda", CreatedBy: dev,
		},
		store.InsertObservationParams{
			ID: id.NewObservation(), TreeID: treeID,
			BloomState: store.BloomState("exploding"), // invalid
			ReportedBy: dev,
		},
	)
	if err == nil {
		t.Fatalf("expected error from invalid bloom_state, got nil")
	}

	if _, err := ts.ByID(ctx, treeID); !store.IsNotFound(err) {
		t.Errorf("tree must not persist when observation fails: got err=%v", err)
	}
}

// TestTreeStore_InsertWithObservation_RejectsMismatchedIDs guards against
// a caller copy-paste bug where the observation's TreeID does not match
// the tree being inserted.
func TestTreeStore_InsertWithObservation_RejectsMismatchedIDs(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)

	treeID := id.NewTree()
	otherID := id.NewTree()
	err := ts.InsertWithObservation(ctx,
		store.InsertTreeParams{
			ID: treeID, Lat: -1.2921, Lng: 36.8219,
			Species: "jacaranda", CreatedBy: dev,
		},
		store.InsertObservationParams{
			ID:         id.NewObservation(),
			TreeID:     otherID, // mismatch
			BloomState: store.BloomFull,
			ReportedBy: dev,
		},
	)
	if err == nil {
		t.Fatalf("expected error for mismatched ids, got nil")
	}
}
