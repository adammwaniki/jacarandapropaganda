//go:build integration

package store_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/adammwaniki/jacarandapropaganda/internal/id"
	"github.com/adammwaniki/jacarandapropaganda/internal/store"
)

// TestObservations_BloomStateIsImmutable proves the DB trigger blocks an
// attempted mutation of a past observation's bloom_state. The archive
// invariant is that a pinned state is a historical fact — you overwrite it
// by inserting a new observation, never by rewriting the old one.
func TestObservations_BloomStateIsImmutable(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)
	os := store.NewObservationStore(db)

	treeID := id.NewTree()
	mustInsertTree(t, ctx, ts, treeID, -1.2921, 36.8219, dev)

	obsID := id.NewObservation()
	if err := os.Insert(ctx, store.InsertObservationParams{
		ID: obsID, TreeID: treeID, BloomState: store.BloomFull, ReportedBy: dev,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Bypass the Go API deliberately — this is the backstop test.
	_, err := db.ExecContext(ctx,
		`UPDATE observations SET bloom_state = 'fading' WHERE id = $1`, obsID)
	if err == nil {
		t.Fatalf("expected trigger to block mutation of bloom_state, got no error")
	}
	if !strings.Contains(err.Error(), "append-only") {
		t.Errorf("error message should mention append-only: %v", err)
	}
}

func TestObservations_DeleteIsForbidden(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)
	os := store.NewObservationStore(db)

	treeID := id.NewTree()
	mustInsertTree(t, ctx, ts, treeID, -1.2921, 36.8219, dev)

	obsID := id.NewObservation()
	if err := os.Insert(ctx, store.InsertObservationParams{
		ID: obsID, TreeID: treeID, BloomState: store.BloomFull, ReportedBy: dev,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	_, err := db.ExecContext(ctx, `DELETE FROM observations WHERE id = $1`, obsID)
	if err == nil {
		t.Fatalf("expected trigger to block DELETE, got no error")
	}
}

func TestObservations_HiddenAtIsSetOnce(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)
	os := store.NewObservationStore(db)

	treeID := id.NewTree()
	mustInsertTree(t, ctx, ts, treeID, -1.2921, 36.8219, dev)

	obsID := id.NewObservation()
	if err := os.Insert(ctx, store.InsertObservationParams{
		ID: obsID, TreeID: treeID, BloomState: store.BloomFull, ReportedBy: dev,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := os.Hide(ctx, obsID); err != nil {
		t.Fatalf("hide: %v", err)
	}

	// Un-hiding is forbidden.
	_, err := db.ExecContext(ctx,
		`UPDATE observations SET hidden_at = NULL WHERE id = $1`, obsID)
	if err == nil {
		t.Fatalf("expected trigger to reject un-hide, got no error")
	}
}

// Keep the "exactly four app tables" invariant alive after adding trigger
// functions and migrations. Functions are not tables, but this is a cheap
// second check that migration 0002 did not slip in a fifth table.
func TestSchema_StillExactlyFourTables(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()

	got := listAppTables(t, db)
	if len(got) != 4 {
		t.Fatalf("app tables: got %d (%v), want 4", len(got), got)
	}
}
