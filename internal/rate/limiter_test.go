//go:build integration

package rate_test

import (
	"context"
	"database/sql"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/adammwaniki/jacarandapropaganda/internal/id"
	"github.com/adammwaniki/jacarandapropaganda/internal/rate"
	"github.com/adammwaniki/jacarandapropaganda/internal/store"
	"github.com/adammwaniki/jacarandapropaganda/internal/store/testutil"
)

// Limits from spec.md § Rate limiting and abuse. If these change, the
// spec is the source of truth; update the constants in internal/rate too.
const (
	treePerDevice = 10
	treePerIP     = 30
	obsPerDevice  = 60
)

func freshDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := testutil.NewTestDB(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := store.MigrateUp(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestLimiter_DeviceTreeLimit_ExhaustsAt10(t *testing.T) {
	db := freshDB(t)
	l := rate.NewLimiter(db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dev := id.NewDevice()
	ip := netip.MustParseAddr("192.0.2.1")

	for i := 0; i < treePerDevice; i++ {
		if err := l.CheckAndRecordTreeCreate(ctx, dev, ip); err != nil {
			t.Fatalf("pin %d: unexpected error %v", i+1, err)
		}
	}
	// 11th should fail.
	err := l.CheckAndRecordTreeCreate(ctx, dev, ip)
	if err == nil {
		t.Fatalf("pin 11 should be rate-limited, got nil")
	}
	if !errors.Is(err, rate.ErrLimited) {
		t.Fatalf("expected ErrLimited, got %v", err)
	}
	var limErr rate.LimitedError
	if !errors.As(err, &limErr) {
		t.Fatalf("expected LimitedError, got %T", err)
	}
	if limErr.Kind != rate.KindTreeCreate {
		t.Errorf("kind: got %q, want %q", limErr.Kind, rate.KindTreeCreate)
	}
	if limErr.Scope != rate.ScopeDevice {
		t.Errorf("scope: got %q, want %q", limErr.Scope, rate.ScopeDevice)
	}
}

func TestLimiter_IPTreeLimit_ExhaustsAt30(t *testing.T) {
	db := freshDB(t)
	l := rate.NewLimiter(db)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ip := netip.MustParseAddr("203.0.113.7")

	// 30 pins spread across 30 devices so the per-device limit never fires.
	for i := 0; i < treePerIP; i++ {
		dev := id.NewDevice()
		if err := l.CheckAndRecordTreeCreate(ctx, dev, ip); err != nil {
			t.Fatalf("pin %d via fresh device: unexpected %v", i+1, err)
		}
	}

	// 31st pin from a fresh device should still fail because the IP is at its cap.
	err := l.CheckAndRecordTreeCreate(ctx, id.NewDevice(), ip)
	if err == nil {
		t.Fatalf("pin 31 should be IP-limited, got nil")
	}
	var limErr rate.LimitedError
	if !errors.As(err, &limErr) {
		t.Fatalf("expected LimitedError, got %v", err)
	}
	if limErr.Scope != rate.ScopeIP {
		t.Errorf("scope: got %q, want %q", limErr.Scope, rate.ScopeIP)
	}
}

func TestLimiter_ObservationLimit_ExhaustsAt60(t *testing.T) {
	db := freshDB(t)
	l := rate.NewLimiter(db)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dev := id.NewDevice()

	for i := 0; i < obsPerDevice; i++ {
		if err := l.CheckAndRecordObservationCreate(ctx, dev); err != nil {
			t.Fatalf("obs %d: %v", i+1, err)
		}
	}
	err := l.CheckAndRecordObservationCreate(ctx, dev)
	if err == nil {
		t.Fatalf("obs 61 should be rate-limited, got nil")
	}
	if !errors.Is(err, rate.ErrLimited) {
		t.Fatalf("expected ErrLimited, got %v", err)
	}
	var limErr rate.LimitedError
	if !errors.As(err, &limErr) {
		t.Fatalf("expected LimitedError, got %v", err)
	}
	if limErr.Kind != rate.KindObservationCreate {
		t.Errorf("kind: got %q, want %q", limErr.Kind, rate.KindObservationCreate)
	}
}

func TestLimiter_TreeAndObservationLimitsAreIndependent(t *testing.T) {
	db := freshDB(t)
	l := rate.NewLimiter(db)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dev := id.NewDevice()
	ip := netip.MustParseAddr("192.0.2.2")

	// Max out tree-creates for this device.
	for i := 0; i < treePerDevice; i++ {
		if err := l.CheckAndRecordTreeCreate(ctx, dev, ip); err != nil {
			t.Fatalf("pin %d: %v", i+1, err)
		}
	}
	// Observations must still succeed — a user who pinned their cap should
	// still be able to update bloom state on existing trees.
	for i := 0; i < 20; i++ {
		if err := l.CheckAndRecordObservationCreate(ctx, dev); err != nil {
			t.Fatalf("obs %d: %v", i+1, err)
		}
	}
}

func TestLimiter_RollingWindowNotCalendarDay(t *testing.T) {
	// A tree recorded 25 hours ago must NOT count against the rolling
	// 24h window. A tree recorded 23 hours ago must count.
	db := freshDB(t)
	l := rate.NewLimiter(db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dev := id.NewDevice()
	ip := netip.MustParseAddr("192.0.2.3")

	// Seed 10 events, 9 at 25 hours ago (outside window) and 1 at 23h ago.
	for i := 0; i < 9; i++ {
		seedEvent(t, ctx, db, rate.KindTreeCreate, dev, ip, -25*time.Hour)
	}
	seedEvent(t, ctx, db, rate.KindTreeCreate, dev, ip, -23*time.Hour)

	// With the old events counted we'd be at 10 and the next would fail.
	// Rolling window means only the one at -23h counts, leaving 9 of 10
	// in the last 24h — so 1 more should succeed.
	if err := l.CheckAndRecordTreeCreate(ctx, dev, ip); err != nil {
		t.Fatalf("11th (but 10th within window) unexpectedly limited: %v", err)
	}
}

func TestLimiter_PruneRemovesExpiredRows(t *testing.T) {
	db := freshDB(t)
	l := rate.NewLimiter(db)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dev := id.NewDevice()
	ip := netip.MustParseAddr("192.0.2.4")

	// 5 ancient, 5 recent.
	for i := 0; i < 5; i++ {
		seedEvent(t, ctx, db, rate.KindTreeCreate, dev, ip, -48*time.Hour)
	}
	for i := 0; i < 5; i++ {
		if err := l.CheckAndRecordTreeCreate(ctx, dev, ip); err != nil {
			t.Fatalf("seed recent %d: %v", i+1, err)
		}
	}

	removed, err := l.Prune(ctx)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if removed != 5 {
		t.Errorf("prune removed: got %d, want 5", removed)
	}

	var n int
	if err := db.QueryRow(`SELECT count(*) FROM rate_events`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 5 {
		t.Errorf("remaining rows: got %d, want 5", n)
	}
}

// seedEvent writes a rate_events row with a specific created_at offset.
// Lets tests simulate aged events without sleeping.
func seedEvent(t *testing.T, ctx context.Context, db *sql.DB,
	kind rate.Kind, dev uuid.UUID, ip netip.Addr, ageOffset time.Duration) {
	t.Helper()
	_, err := db.ExecContext(ctx,
		`INSERT INTO rate_events (id, kind, device_id, ip_addr, created_at)
		 VALUES ($1, $2, $3, $4, now() + $5::interval)`,
		id.NewRateEvent(), string(kind), dev, ip.String(), ageOffset.String())
	if err != nil {
		t.Fatalf("seed event: %v", err)
	}
}
