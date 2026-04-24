package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Device mirrors the devices table. Kept deliberately small: this is an
// identity for rate-limiting and gentle "your pins" highlighting, nothing
// more. No display name, no profile, no account.
type Device struct {
	ID        uuid.UUID
	FirstSeen time.Time
	LastSeen  time.Time
	BlockedAt *time.Time
}

// DeviceStore is the persistence gate for the devices table.
type DeviceStore struct {
	db *sql.DB
}

func NewDeviceStore(db *sql.DB) *DeviceStore {
	return &DeviceStore{db: db}
}

// Upsert inserts a new device row if id is unseen, or advances last_seen on
// an existing row. first_seen is preserved across upserts.
//
// The id MUST be a UUIDv4. Storing a UUIDv7 here would silently leak the
// device's first-visit timestamp through its cookie — see spec.md § Identity.
func (s *DeviceStore) Upsert(ctx context.Context, id uuid.UUID) error {
	if id.Version() != 4 {
		return fmt.Errorf("store: device id must be UUIDv4, got v%d", id.Version())
	}
	const q = `
		INSERT INTO devices (id)
		VALUES ($1)
		ON CONFLICT (id) DO UPDATE SET last_seen = now()
	`
	if _, err := s.db.ExecContext(ctx, q, id); err != nil {
		return fmt.Errorf("upsert device: %w", err)
	}
	return nil
}

// Get returns the device row for id, or ErrNotFound if absent.
func (s *DeviceStore) Get(ctx context.Context, id uuid.UUID) (Device, error) {
	const q = `
		SELECT id, first_seen, last_seen, blocked_at
		FROM devices
		WHERE id = $1
	`
	var d Device
	err := s.db.QueryRowContext(ctx, q, id).
		Scan(&d.ID, &d.FirstSeen, &d.LastSeen, &d.BlockedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, fmt.Errorf("get device: %w", err)
	}
	return d, nil
}

// Block marks the device as blocked. Blocked devices can read the map but
// their writes are silently dropped into the moderation queue as pre-hidden
// (enforced at the write path in Phase H).
func (s *DeviceStore) Block(ctx context.Context, id uuid.UUID) error {
	const q = `UPDATE devices SET blocked_at = now() WHERE id = $1`
	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("block device: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("block device: rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
