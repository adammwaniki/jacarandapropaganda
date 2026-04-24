// Package id centralizes UUID generation so call sites read as the spec
// reads ("a new tree id", "a new observation id") rather than as calls into
// a third-party library. The underlying implementation is google/uuid.
//
// Spec invariant: public-facing, append-heavy tables (trees, observations,
// moderation_queue) use UUIDv7 for index locality. Device IDs use UUIDv4 to
// avoid leaking first-visit timestamps through a cookie.
package id

import "github.com/google/uuid"

// NewTree returns a UUIDv7 intended for the trees table.
func NewTree() uuid.UUID { return mustV7() }

// NewObservation returns a UUIDv7 intended for the observations table.
func NewObservation() uuid.UUID { return mustV7() }

// NewModerationRow returns a UUIDv7 intended for the moderation_queue table.
func NewModerationRow() uuid.UUID { return mustV7() }

// NewDevice returns a UUIDv4 for device identity. A separate helper makes
// the privacy invariant loud at call sites.
func NewDevice() uuid.UUID { return uuid.New() }

func mustV7() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		// uuid.NewV7 only fails if the system has no source of entropy. At
		// that point nothing interesting can continue; surface it loudly.
		panic("id: NewV7 failed: " + err.Error())
	}
	return id
}
