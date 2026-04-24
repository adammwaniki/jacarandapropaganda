package main

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver

	"github.com/adammwaniki/jacarandapropaganda/internal/app"
	"github.com/adammwaniki/jacarandapropaganda/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	addr := envOr("JP_ADDR", ":8080")
	dbURL := envOr("JP_DATABASE_URL",
		"postgres://jacaranda:jacaranda@localhost:55432/jacaranda?sslmode=disable")

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		logger.Error("open db", "err", err)
		os.Exit(1)
	}
	// Conservative pool sizing for a single VPS. Phase E (rate limiting) and
	// Phase I (load testing) will tune this with real numbers.
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := db.PingContext(pingCtx); err != nil {
		pingCancel()
		logger.Error("ping db", "err", err, "db_url_redacted", redactDSN(dbURL))
		os.Exit(1)
	}
	pingCancel()

	deps := app.Deps{
		Devices: store.NewDeviceStore(db),
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           app.NewRouter(deps),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("server starting", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "err", err)
		os.Exit(1)
	}
	if err := db.Close(); err != nil {
		logger.Warn("db close error", "err", err)
	}
	logger.Info("server stopped")
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

// redactDSN keeps the host/db parts of a connection string but drops the
// password. Useful when logging connection failures.
func redactDSN(dsn string) string {
	// Rough and cheap — good enough for log output; do not use for parsing.
	// Replaces "://user:password@" with "://user:***@".
	atIdx := -1
	for i := 0; i < len(dsn); i++ {
		if dsn[i] == '@' {
			atIdx = i
			break
		}
	}
	if atIdx < 0 {
		return dsn
	}
	colonIdx := -1
	for i := atIdx - 1; i >= 0; i-- {
		if dsn[i] == ':' {
			colonIdx = i
			break
		}
		if dsn[i] == '/' {
			return dsn // no password segment
		}
	}
	if colonIdx < 0 {
		return dsn
	}
	return dsn[:colonIdx+1] + "***" + dsn[atIdx:]
}
