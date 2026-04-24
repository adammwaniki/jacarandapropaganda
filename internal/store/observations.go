package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// InsertObservationParams is the write-path input for appending an
// observation. PhotoR2Key is optional: a pin without a photo is allowed
// for the "update bloom state" flow where a user reports a new state on
// an existing tree they are walking past.
type InsertObservationParams struct {
	ID         uuid.UUID // UUIDv7
	TreeID     uuid.UUID
	BloomState BloomState
	PhotoR2Key *string
	ReportedBy uuid.UUID // device id
}

// ObservationStore is the repository for the observations table.
//
// DELIBERATE ABSENCE: no Update* / Set* / Modify* methods. Observations are
// append-only — they are the archive the spec calls "the flowing river of
// bloom-state reports". To change a tree's current state, a caller inserts
// a new observation. To remove one, Hide sets hidden_at. Never mutate history.
// This invariant is pinned by TestObservationStore_NoUpdateMethodExists.
type ObservationStore struct {
	db *sql.DB
}

func NewObservationStore(db *sql.DB) *ObservationStore {
	return &ObservationStore{db: db}
}

// Insert appends a new observation. Rejects non-UUIDv7 ids and bloom
// states that are not members of the Go enum (the DB enum is the final
// gate, but failing in Go is a cheaper and clearer error).
func (s *ObservationStore) Insert(ctx context.Context, p InsertObservationParams) error {
	if p.ID.Version() != 7 {
		return fmt.Errorf("store: observation id must be UUIDv7, got v%d", p.ID.Version())
	}
	if !p.BloomState.Valid() {
		return fmt.Errorf("store: invalid bloom_state %q", p.BloomState)
	}
	if _, err := s.db.ExecContext(ctx, insertObservationSQL,
		p.ID, p.TreeID, string(p.BloomState), p.PhotoR2Key, p.ReportedBy,
	); err != nil {
		return fmt.Errorf("insert observation: %w", err)
	}
	return nil
}

const insertObservationSQL = `
	INSERT INTO observations
		(id, tree_id, bloom_state, photo_r2_key, reported_by_device)
	VALUES ($1, $2, $3, $4, $5)
`

// CurrentForTree returns the most recent non-hidden observation for a tree,
// or ErrNotFound if none exists.
func (s *ObservationStore) CurrentForTree(ctx context.Context, treeID uuid.UUID) (Observation, error) {
	const q = `
		SELECT id, tree_id, bloom_state, photo_r2_key,
		       observed_at, reported_by_device, hidden_at
		FROM observations
		WHERE tree_id = $1 AND hidden_at IS NULL
		ORDER BY observed_at DESC
		LIMIT 1
	`
	var (
		o     Observation
		bloom string
		photo sql.NullString
	)
	err := s.db.QueryRowContext(ctx, q, treeID).Scan(
		&o.ID, &o.TreeID, &bloom, &photo,
		&o.ObservedAt, &o.ReportedByDevice, &o.HiddenAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Observation{}, ErrNotFound
	}
	if err != nil {
		return Observation{}, fmt.Errorf("current: %w", err)
	}
	o.BloomState = BloomState(bloom)
	if photo.Valid {
		o.PhotoR2Key = &photo.String
	}
	return o, nil
}

// Hide soft-deletes a single observation. The most common trigger is a
// moderator's "hide" action from /admin/queue; reports auto-hide when three
// distinct devices flag the same observation (Phase H).
func (s *ObservationStore) Hide(ctx context.Context, obsID uuid.UUID) error {
	const q = `UPDATE observations SET hidden_at = now() WHERE id = $1 AND hidden_at IS NULL`
	res, err := s.db.ExecContext(ctx, q, obsID)
	if err != nil {
		return fmt.Errorf("hide observation: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("hide observation: rows: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
