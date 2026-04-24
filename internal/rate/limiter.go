// Package rate enforces the two-signal rate limits from spec.md
// § Rate limiting and abuse: 10 new trees per device per rolling 24h,
// 30 per IP per rolling 24h, and 60 observations per device per rolling 24h.
//
// State lives in the rate_events Postgres table, not Redis. At expected
// scale the table stays small (last 24 hours of writes, pruned nightly)
// and the indexed count-queries are single-digit milliseconds.
package rate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"

	"github.com/google/uuid"

	"github.com/adammwaniki/jacarandapropaganda/internal/id"
)

// Kind discriminates between the limited actions.
type Kind string

const (
	KindTreeCreate        Kind = "tree_create"
	KindObservationCreate Kind = "observation_create"
)

// Scope distinguishes which limit fired — used in 429 messages so the UI
// can explain why a write was rejected.
type Scope string

const (
	ScopeDevice Scope = "device"
	ScopeIP     Scope = "ip"
)

// Numeric caps. If the spec changes, update here; there is no runtime knob
// on purpose — a limit that flexes per-request is a limit that is not a limit.
const (
	TreePerDevicePer24h        = 10
	TreePerIPPer24h            = 30
	ObservationPerDevicePer24h = 60
)

// ErrLimited is the sentinel for "call was rejected by a rate limit". Use
// errors.Is to detect; use errors.As(&LimitedError) to inspect kind/scope.
var ErrLimited = errors.New("rate: limited")

// LimitedError carries context about which limit fired. Implements error
// and wraps ErrLimited so errors.Is(err, ErrLimited) is true.
type LimitedError struct {
	Kind  Kind
	Scope Scope
	Limit int
}

func (e LimitedError) Error() string {
	return fmt.Sprintf("rate: %s per %s cap of %d reached", e.Kind, e.Scope, e.Limit)
}
func (e LimitedError) Unwrap() error { return ErrLimited }

// Limiter is the public handle to the limiter. Construct once per process.
type Limiter struct {
	db *sql.DB
}

func NewLimiter(db *sql.DB) *Limiter { return &Limiter{db: db} }

// CheckAndRecordTreeCreate is called on POST /trees before the insert.
// It atomically reads the two counts (device, IP) in the last rolling 24h
// and, iff both are under cap, appends a new rate_events row.
//
// If the call is rejected, no row is written and the returned error is a
// LimitedError wrapping ErrLimited.
func (l *Limiter) CheckAndRecordTreeCreate(ctx context.Context, device uuid.UUID, ip netip.Addr) error {
	return l.checkAndRecord(ctx, checkArgs{
		kind:    KindTreeCreate,
		device:  device,
		ip:      ip,
		devCap:  TreePerDevicePer24h,
		ipCap:   TreePerIPPer24h,
		checkIP: true,
	})
}

// CheckAndRecordObservationCreate is called on POST /trees/{id}/observations.
// Observations are rate-limited only per device — the spec treats updating
// an existing tree as a lower-risk action than placing a new pin.
func (l *Limiter) CheckAndRecordObservationCreate(ctx context.Context, device uuid.UUID) error {
	return l.checkAndRecord(ctx, checkArgs{
		kind:    KindObservationCreate,
		device:  device,
		devCap:  ObservationPerDevicePer24h,
		checkIP: false,
	})
}

// Prune deletes rate_events rows older than 48 hours (double the 24h
// window, so a just-after-midnight prune never races in-flight inserts).
// Returns the number of rows deleted.
func (l *Limiter) Prune(ctx context.Context) (int64, error) {
	res, err := l.db.ExecContext(ctx,
		`DELETE FROM rate_events WHERE created_at < now() - interval '48 hours'`)
	if err != nil {
		return 0, fmt.Errorf("prune rate_events: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune rate_events: rows: %w", err)
	}
	return n, nil
}

type checkArgs struct {
	kind    Kind
	device  uuid.UUID
	ip      netip.Addr
	devCap  int
	ipCap   int
	checkIP bool
}

// checkAndRecord runs both reads and the conditional insert in one round
// trip. See the SQL below — the CTE inserts only when both caps are under
// their limit, and returns both counts so Go can decide which scope fired.
func (l *Limiter) checkAndRecord(ctx context.Context, a checkArgs) error {
	if a.checkIP && !a.ip.IsValid() {
		return fmt.Errorf("rate: ip address required for kind %q", a.kind)
	}

	var (
		devCount int
		ipCount  int
		inserted bool
	)

	// We pass the IP as text and let Postgres cast to inet. For kinds that
	// don't check IP, we still insert the IP column (NOT NULL); an unset IP
	// falls back to a non-routable placeholder. This is harmless because no
	// query reads IPs for KindObservationCreate.
	ip := a.ip
	if !a.checkIP && !ip.IsValid() {
		ip = netip.MustParseAddr("::")
	}

	const sqlTmpl = `
WITH
  dev AS (
    SELECT count(*)::int AS n FROM rate_events
    WHERE kind = $2::rate_event_kind
      AND device_id = $1
      AND created_at > now() - interval '24 hours'
  ),
  ipq AS (
    SELECT count(*)::int AS n FROM rate_events
    WHERE kind = $2::rate_event_kind
      AND ip_addr = $3::inet
      AND created_at > now() - interval '24 hours'
  ),
  ok AS (
    SELECT
      (dev.n < $5) AS dev_ok,
      (CASE WHEN $7 THEN ipq.n < $6 ELSE TRUE END) AS ip_ok
    FROM dev, ipq
  ),
  inserted AS (
    INSERT INTO rate_events (id, kind, device_id, ip_addr)
    SELECT $4, $2::rate_event_kind, $1, $3::inet
    FROM ok WHERE dev_ok AND ip_ok
    RETURNING 1
  )
SELECT
  (SELECT n FROM dev),
  (SELECT n FROM ipq),
  (SELECT EXISTS (SELECT 1 FROM inserted))
`

	err := l.db.QueryRowContext(ctx, sqlTmpl,
		a.device,          // $1
		string(a.kind),    // $2
		ip.String(),       // $3
		id.NewRateEvent(), // $4
		a.devCap,          // $5
		a.ipCap,           // $6
		a.checkIP,         // $7
	).Scan(&devCount, &ipCount, &inserted)
	if err != nil {
		return fmt.Errorf("rate: check/record %s: %w", a.kind, err)
	}
	if inserted {
		return nil
	}
	if devCount >= a.devCap {
		return LimitedError{Kind: a.kind, Scope: ScopeDevice, Limit: a.devCap}
	}
	if a.checkIP && ipCount >= a.ipCap {
		return LimitedError{Kind: a.kind, Scope: ScopeIP, Limit: a.ipCap}
	}
	// Should be unreachable: not inserted and no cap reached. Surface as an
	// internal error rather than a silent success.
	return fmt.Errorf("rate: insert skipped unexpectedly (dev=%d ip=%d)", devCount, ipCount)
}
