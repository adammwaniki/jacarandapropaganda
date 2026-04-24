//go:build integration

package store_test

import (
	"context"
	"database/sql"
	"sort"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver

	"github.com/adammwaniki/jacarandapropaganda/internal/store"
	"github.com/adammwaniki/jacarandapropaganda/internal/store/testutil"
	"github.com/adammwaniki/jacarandapropaganda/migrations"
)

// expectedDomainTables is the exact set of product-model tables spec.md
// names as "four tables". Any addition to this list is a spec amendment.
var expectedDomainTables = []string{
	"devices",
	"moderation_queue",
	"observations",
	"trees",
}

// expectedOperationalTables are allowed by the spec but do not carry user-
// facing meaning. spec.md § Rate limiting permits "a small counter table".
var expectedOperationalTables = []string{
	"rate_events",
}

// expectedTables combines domain + operational (sorted alphabetically to
// match listAppTables' ordering) — the full set that must exist after
// migrations have run to head.
var expectedTables = sortedMerge(expectedDomainTables, expectedOperationalTables)

func sortedMerge(a, b []string) []string {
	out := append([]string{}, a...)
	out = append(out, b...)
	sort.Strings(out)
	return out
}

var expectedExtensions = []string{
	"postgis",
	"h3",
	"h3_postgis",
}

func TestMigrations_UpCreatesExpectedSchema(t *testing.T) {
	dsn := testutil.NewTestDB(t)
	db := openDB(t, dsn)
	defer db.Close()

	if err := store.MigrateUp(db); err != nil {
		t.Fatalf("goose up: %v", err)
	}

	assertExtensions(t, db, expectedExtensions)
	assertAppTables(t, db, expectedTables)
	assertBloomStateEnum(t, db)
}

func TestMigrations_UpDownUpRoundTripIsReversible(t *testing.T) {
	dsn := testutil.NewTestDB(t)
	db := openDB(t, dsn)
	defer db.Close()

	if err := store.MigrateUp(db); err != nil {
		t.Fatalf("first up: %v", err)
	}
	if err := store.MigrateDownTo(db, 0); err != nil {
		t.Fatalf("down to 0: %v", err)
	}
	// After a full down, no app tables remain.
	assertAppTables(t, db, nil)

	if err := store.MigrateUp(db); err != nil {
		t.Fatalf("second up: %v", err)
	}
	assertAppTables(t, db, expectedTables)
}

func TestMigrations_DomainTablesAreExactlyFour(t *testing.T) {
	// spec.md's "four tables" invariant applies to the product data model.
	// Operational tables (rate_events) are permitted by the spec but are
	// tracked separately in expectedOperationalTables.
	dsn := testutil.NewTestDB(t)
	db := openDB(t, dsn)
	defer db.Close()

	if err := store.MigrateUp(db); err != nil {
		t.Fatalf("up: %v", err)
	}

	all := listAppTables(t, db)
	var domain []string
	opSet := map[string]bool{}
	for _, t := range expectedOperationalTables {
		opSet[t] = true
	}
	for _, name := range all {
		if !opSet[name] {
			domain = append(domain, name)
		}
	}
	if len(domain) != 4 {
		t.Fatalf("domain tables: got %d (%v), want exactly 4", len(domain), domain)
	}
}

func TestMigrations_NoUnknownTables(t *testing.T) {
	// Complements the domain-tables test: if a rogue migration adds a 6th
	// operational table, this fails loudly.
	dsn := testutil.NewTestDB(t)
	db := openDB(t, dsn)
	defer db.Close()

	if err := store.MigrateUp(db); err != nil {
		t.Fatalf("up: %v", err)
	}

	allowed := map[string]bool{}
	for _, t := range expectedTables {
		allowed[t] = true
	}
	for _, got := range listAppTables(t, db) {
		if !allowed[got] {
			t.Errorf("unknown table %q — update expectedTables after amending the spec", got)
		}
	}
}

// --- helpers --------------------------------------------------------------

func openDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return db
}

func assertExtensions(t *testing.T, db *sql.DB, want []string) {
	t.Helper()
	for _, ext := range want {
		var exists bool
		if err := db.QueryRow(
			`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = $1)`, ext,
		).Scan(&exists); err != nil {
			t.Fatalf("query extension %q: %v", ext, err)
		}
		if !exists {
			t.Errorf("extension %q missing", ext)
		}
	}
}

func assertAppTables(t *testing.T, db *sql.DB, want []string) {
	t.Helper()
	got := listAppTables(t, db)
	if !equalStringSets(got, want) {
		t.Errorf("app tables: got %v, want %v", got, want)
	}
}

func listAppTables(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`
		SELECT tablename
		FROM pg_tables
		WHERE schemaname = 'public'
		  AND tablename NOT LIKE 'goose_%'
		  AND tablename NOT LIKE 'spatial_%'
		ORDER BY tablename
	`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, name)
	}
	return out
}

func assertBloomStateEnum(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`
		SELECT unnest(enum_range(NULL::bloom_state))::text
		ORDER BY 1
	`)
	if err != nil {
		t.Fatalf("query bloom_state enum: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, v)
	}
	want := []string{"budding", "fading", "full", "partial"} // alphabetical
	if !equalStringSets(got, want) {
		t.Errorf("bloom_state values: got %v, want %v", got, want)
	}
}

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Silence unused-import warnings if migrations package is empty initially.
var _ = migrations.Embed
