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

// TestSchema_DomainStillExactlyFourTables re-asserts the domain-model
// invariant from the store package. rate_events is operational (spec.md
// § Rate limiting) and is excluded from the count.
func TestSchema_DomainStillExactlyFourTables(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()

	all := listAppTables(t, db)
	operational := map[string]bool{"rate_events": true}
	var domain []string
	for _, name := range all {
		if !operational[name] {
			domain = append(domain, name)
		}
	}
	if len(domain) != 4 {
		t.Fatalf("domain tables: got %d (%v), want 4", len(domain), domain)
	}
}
