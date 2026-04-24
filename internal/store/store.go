// Package store holds the persistence layer: pgx-backed repositories for
// trees, observations, devices, and the moderation queue, plus the migration
// runner.
//
// No ORM. The spec keeps the data model to four tables; SQL is the right
// interface. Callers outside /internal must not import this package.
package store

import (
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"

	"github.com/adammwaniki/jacarandapropaganda/migrations"
)

const dialect = "postgres"

// MigrateUp applies all pending migrations from the embedded migrations FS.
func MigrateUp(db *sql.DB) error {
	if err := setProvider(); err != nil {
		return err
	}
	if err := goose.SetDialect(dialect); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}
	goose.SetBaseFS(migrations.Embed)
	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// MigrateDownTo rolls the schema back to the given version. Passing 0 rolls
// back every migration.
func MigrateDownTo(db *sql.DB, version int64) error {
	if err := setProvider(); err != nil {
		return err
	}
	if err := goose.SetDialect(dialect); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}
	goose.SetBaseFS(migrations.Embed)
	if err := goose.DownTo(db, ".", version); err != nil {
		return fmt.Errorf("goose down-to %d: %w", version, err)
	}
	return nil
}

// setProvider is a no-op for now. A future refactor may move to
// goose.NewProvider so callers hold the provider; keeping a seam.
func setProvider() error { return nil }
