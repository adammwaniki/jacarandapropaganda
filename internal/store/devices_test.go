//go:build integration

package store_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/adammwaniki/jacarandapropaganda/internal/store"
	"github.com/adammwaniki/jacarandapropaganda/internal/store/testutil"
)

func TestDeviceStore_UpsertCreatesRowOnFirstCall(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()

	ds := store.NewDeviceStore(db)
	id := uuid.New() // UUIDv4 per spec

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := ds.Upsert(ctx, id); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	got, err := ds.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != id {
		t.Fatalf("id: got %v, want %v", got.ID, id)
	}
	if got.FirstSeen.IsZero() {
		t.Errorf("first_seen must be set")
	}
	if got.LastSeen.IsZero() {
		t.Errorf("last_seen must be set")
	}
	if got.BlockedAt != nil {
		t.Errorf("blocked_at must be null on creation, got %v", got.BlockedAt)
	}
}

func TestDeviceStore_UpsertPreservesFirstSeenAndAdvancesLastSeen(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()

	ds := store.NewDeviceStore(db)
	id := uuid.New()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := ds.Upsert(ctx, id); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	first, err := ds.Get(ctx, id)
	if err != nil {
		t.Fatalf("get after first: %v", err)
	}

	// A modest sleep is enough because timestamptz has microsecond resolution.
	time.Sleep(20 * time.Millisecond)

	if err := ds.Upsert(ctx, id); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	second, err := ds.Get(ctx, id)
	if err != nil {
		t.Fatalf("get after second: %v", err)
	}

	if !second.FirstSeen.Equal(first.FirstSeen) {
		t.Errorf("first_seen must be immutable: first=%v second=%v",
			first.FirstSeen, second.FirstSeen)
	}
	if !second.LastSeen.After(first.LastSeen) {
		t.Errorf("last_seen must advance: first=%v second=%v",
			first.LastSeen, second.LastSeen)
	}
}

func TestDeviceStore_RejectsNonV4ID(t *testing.T) {
	// The spec deliberately uses UUIDv4 for device IDs to avoid leaking the
	// first-visit timestamp through the cookie. A UUIDv7 (time-ordered)
	// device ID would silently undermine that. The store is the last gate.
	db := freshMigratedDB(t)
	defer db.Close()

	ds := store.NewDeviceStore(db)

	// Craft a UUIDv7-shaped ID.
	v7, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("gen v7: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := ds.Upsert(ctx, v7); err == nil {
		t.Fatalf("expected error for UUIDv7 device ID, got nil")
	}
}

func TestDeviceStore_GetUnknownReturnsNotFound(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()

	ds := store.NewDeviceStore(db)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := ds.Get(ctx, uuid.New())
	if err == nil {
		t.Fatalf("expected error for unknown id, got nil")
	}
	if !store.IsNotFound(err) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeviceStore_BlockedAtRoundTrip(t *testing.T) {
	db := freshMigratedDB(t)
	defer db.Close()

	ds := store.NewDeviceStore(db)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	id := uuid.New()
	if err := ds.Upsert(ctx, id); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := ds.Block(ctx, id); err != nil {
		t.Fatalf("block: %v", err)
	}
	got, err := ds.Get(ctx, id)
	if err != nil {
		t.Fatalf("get after block: %v", err)
	}
	if got.BlockedAt == nil {
		t.Fatalf("blocked_at must be set after Block()")
	}
}

// freshMigratedDB creates a throwaway test DB and runs migrations. Shared
// helper for device-store tests.
func freshMigratedDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := testutil.NewTestDB(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := store.MigrateUp(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}
