package rate

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// advisoryLockKey is the pg_try_advisory_lock key the pruner uses to
// ensure only one server process runs the prune even if the operator
// scales to multiple replicas later. Arbitrary but fixed.
const advisoryLockKey int64 = 0x4a41435052 // "JACPR"

// RunPruneLoop blocks until ctx is done, calling Prune every tick. The
// first tick fires immediately so a deploy doesn't leave stale rows
// around for the full interval.
//
// It uses pg_try_advisory_lock so that if this process is one of N
// replicas, only one executes the prune per tick. A failure to acquire
// the lock is silent and expected.
func (l *Limiter) RunPruneLoop(ctx context.Context, interval time.Duration) {
	tick := func() {
		if err := l.pruneWithLock(ctx); err != nil {
			slog.WarnContext(ctx, "rate prune", "err", err)
		}
	}

	tick()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}

func (l *Limiter) pruneWithLock(ctx context.Context) error {
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var got bool
	if err := tx.QueryRowContext(ctx,
		`SELECT pg_try_advisory_xact_lock($1)`, advisoryLockKey,
	).Scan(&got); err != nil {
		return err
	}
	if !got {
		// Another replica is pruning — that's fine.
		return nil
	}

	res, err := tx.ExecContext(ctx,
		`DELETE FROM rate_events WHERE created_at < now() - interval '48 hours'`)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if n > 0 {
		slog.InfoContext(ctx, "rate prune", "removed", n)
	}
	return nil
}

// discardDB is a test-friendly way to substitute the pool — unused in prod.
var _ = (*sql.DB)(nil)
