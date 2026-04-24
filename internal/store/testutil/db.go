// Package testutil provides shared helpers for integration tests that talk
// to a real Postgres instance with PostGIS + h3-pg extensions installed.
//
// The helpers assume the dev-stack compose is up (scripts/dev-up.sh). They
// connect to the "postgres" maintenance database and create a fresh,
// randomly-named test database per test, so tests are isolated and safe to
// run in parallel.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const defaultAdminDSN = "postgres://jacaranda:jacaranda@localhost:55432/postgres?sslmode=disable"

// AdminDSN returns the DSN for the Postgres maintenance database. Override
// by setting JP_TEST_ADMIN_DSN in the environment (used by CI).
func AdminDSN() string {
	if v := os.Getenv("JP_TEST_ADMIN_DSN"); v != "" {
		return v
	}
	return defaultAdminDSN
}

// NewTestDB creates a new empty database with a random name, returns a DSN
// pointing to it, and schedules its drop at test end.
func NewTestDB(t *testing.T) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminConn, err := pgx.Connect(ctx, AdminDSN())
	if err != nil {
		t.Fatalf("connect admin db: %v (is docker compose up?)", err)
	}
	defer adminConn.Close(ctx)

	dbName := "jp_test_" + randHex(8)
	if _, err := adminConn.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %q`, dbName)); err != nil {
		t.Fatalf("create test db: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		adminConn, err := pgx.Connect(ctx, AdminDSN())
		if err != nil {
			return
		}
		defer adminConn.Close(ctx)
		_, _ = adminConn.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %q WITH (FORCE)`, dbName))
	})

	return fmt.Sprintf("postgres://jacaranda:jacaranda@localhost:55432/%s?sslmode=disable", dbName)
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
