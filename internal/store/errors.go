package store

import "errors"

// ErrNotFound is returned by repositories when a lookup by primary key finds
// no rows. Callers should use IsNotFound to check rather than comparing
// errors directly, so future wrapping remains a non-breaking change.
var ErrNotFound = errors.New("store: not found")

// IsNotFound reports whether err is, or wraps, ErrNotFound.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}
