//go:build integration

package store_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/adammwaniki/jacarandapropaganda/internal/id"
	"github.com/adammwaniki/jacarandapropaganda/internal/store"
)

func TestObservationStore_InsertAndCurrent(t *testing.T) {
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
	photoKey := "photos/2026/04/abc.jpg"
	if err := os.Insert(ctx, store.InsertObservationParams{
		ID:         obsID,
		TreeID:     treeID,
		BloomState: store.BloomFull,
		PhotoR2Key: &photoKey,
		ReportedBy: dev,
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := os.CurrentForTree(ctx, treeID)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if got.ID != obsID {
		t.Errorf("id: got %v, want %v", got.ID, obsID)
	}
	if got.BloomState != store.BloomFull {
		t.Errorf("bloom_state: got %q, want full", got.BloomState)
	}
	if got.PhotoR2Key == nil || *got.PhotoR2Key != photoKey {
		t.Errorf("photo_key: got %v, want %q", got.PhotoR2Key, photoKey)
	}
}

func TestObservationStore_CurrentIsMostRecentVisible(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)
	os := store.NewObservationStore(db)

	treeID := id.NewTree()
	mustInsertTree(t, ctx, ts, treeID, -1.2921, 36.8219, dev)

	// Three observations in bud → partial → full order. Sleep a touch
	// between them so timestamptz ordering is unambiguous.
	states := []store.BloomState{store.BloomBudding, store.BloomPartial, store.BloomFull}
	ids := make([]uuid.UUID, len(states))
	for i, s := range states {
		ids[i] = id.NewObservation()
		if err := os.Insert(ctx, store.InsertObservationParams{
			ID: ids[i], TreeID: treeID, BloomState: s, ReportedBy: dev,
		}); err != nil {
			t.Fatalf("insert %s: %v", s, err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	got, err := os.CurrentForTree(ctx, treeID)
	if err != nil {
		t.Fatalf("current: %v", err)
	}
	if got.BloomState != store.BloomFull {
		t.Errorf("current bloom_state: got %q, want full", got.BloomState)
	}

	// Hide the most recent — current should fall back to partial.
	if err := os.Hide(ctx, ids[2]); err != nil {
		t.Fatalf("hide: %v", err)
	}
	got, err = os.CurrentForTree(ctx, treeID)
	if err != nil {
		t.Fatalf("current after hide: %v", err)
	}
	if got.BloomState != store.BloomPartial {
		t.Errorf("current after hide: got %q, want partial", got.BloomState)
	}

	// Hide everything — CurrentForTree should return ErrNotFound.
	if err := os.Hide(ctx, ids[0]); err != nil {
		t.Fatalf("hide[0]: %v", err)
	}
	if err := os.Hide(ctx, ids[1]); err != nil {
		t.Fatalf("hide[1]: %v", err)
	}
	if _, err := os.CurrentForTree(ctx, treeID); !store.IsNotFound(err) {
		t.Errorf("expected ErrNotFound when all observations hidden, got %v", err)
	}
}

func TestObservationStore_InsertRejectsInvalidBloomState(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)
	os := store.NewObservationStore(db)

	treeID := id.NewTree()
	mustInsertTree(t, ctx, ts, treeID, -1.2921, 36.8219, dev)

	err := os.Insert(ctx, store.InsertObservationParams{
		ID:         id.NewObservation(),
		TreeID:     treeID,
		BloomState: store.BloomState("exploding"),
		ReportedBy: dev,
	})
	if err == nil {
		t.Fatalf("expected error for invalid bloom_state, got nil")
	}
}

func TestObservationStore_InsertRejectsNonV7ID(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	dev := mustUpsertDevice(t, ctx, db)
	ts := store.NewTreeStore(db)
	os := store.NewObservationStore(db)

	treeID := id.NewTree()
	mustInsertTree(t, ctx, ts, treeID, -1.2921, 36.8219, dev)

	// UUIDv4 rejected; tree and observation must share the locality benefit.
	if err := os.Insert(ctx, store.InsertObservationParams{
		ID: uuid.New(), TreeID: treeID, BloomState: store.BloomFull, ReportedBy: dev,
	}); err == nil {
		t.Fatalf("expected error for UUIDv4 observation id, got nil")
	}
}

// TestObservationStore_NoUpdateMethodExists is an architectural test: the
// spec says observations are append-only (they are the archive). A future
// well-intentioned refactor must not silently add a mutation method.
func TestObservationStore_NoUpdateMethodExists(t *testing.T) {
	t.Parallel()
	tp := reflect.TypeOf(&store.ObservationStore{})
	for i := 0; i < tp.NumMethod(); i++ {
		name := tp.Method(i).Name
		if strings.HasPrefix(name, "Update") ||
			strings.HasPrefix(name, "Set") ||
			strings.HasPrefix(name, "Modify") {
			t.Errorf("ObservationStore must not have a mutation method, found %q", name)
		}
	}
}
