// Command migrate applies or rolls back database migrations. Kept as a
// separate binary from cmd/server because spec.md requires migrations to be
// a deliberate manual act, not part of the service's startup path.
//
// Usage:
//
//	migrate up       # apply all pending migrations
//	migrate down     # roll back the most recent migration
//	migrate reset    # roll back every migration (destructive)
//	migrate status   # show migration state
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/adammwaniki/jacarandapropaganda/migrations"
)

func main() {
	if len(os.Args) != 2 {
		usage()
		os.Exit(2)
	}

	dbURL := os.Getenv("JP_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://jacaranda:jacaranda@localhost:55432/jacaranda?sslmode=disable"
	}

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		fatal("open db: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := db.PingContext(ctx); err != nil {
		cancel()
		fatal("ping db: %v", err)
	}
	cancel()

	if err := goose.SetDialect("postgres"); err != nil {
		fatal("set dialect: %v", err)
	}
	goose.SetBaseFS(migrations.Embed)

	switch os.Args[1] {
	case "up":
		must("up", goose.Up(db, "."))
	case "down":
		must("down", goose.Down(db, "."))
	case "reset":
		must("reset", goose.DownTo(db, ".", 0))
	case "status":
		must("status", goose.Status(db, "."))
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: migrate {up|down|reset|status}")
}

func must(op string, err error) {
	if err != nil {
		fatal("%s: %v", op, err)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
