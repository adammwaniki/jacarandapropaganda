package id

import "testing"

func TestNewTree_IsV7(t *testing.T) {
	t.Parallel()
	if got := NewTree().Version(); got != 7 {
		t.Fatalf("NewTree version: got v%d, want v7", got)
	}
}

func TestNewObservation_IsV7(t *testing.T) {
	t.Parallel()
	if got := NewObservation().Version(); got != 7 {
		t.Fatalf("NewObservation version: got v%d, want v7", got)
	}
}

func TestNewModerationRow_IsV7(t *testing.T) {
	t.Parallel()
	if got := NewModerationRow().Version(); got != 7 {
		t.Fatalf("NewModerationRow version: got v%d, want v7", got)
	}
}

func TestNewRateEvent_IsV7(t *testing.T) {
	t.Parallel()
	if got := NewRateEvent().Version(); got != 7 {
		t.Fatalf("NewRateEvent version: got v%d, want v7", got)
	}
}

func TestNewDevice_IsV4(t *testing.T) {
	t.Parallel()
	// Privacy invariant: device ids must be random, not time-ordered.
	if got := NewDevice().Version(); got != 4 {
		t.Fatalf("NewDevice version: got v%d, want v4", got)
	}
}

func TestNewTree_IsMonotonic(t *testing.T) {
	t.Parallel()
	a := NewTree()
	b := NewTree()
	// UUIDv7 encodes ms timestamp in its prefix; two ids from back-to-back
	// calls must sort a <= b. This is the index-locality property.
	if a.String() > b.String() {
		t.Errorf("UUIDv7 should be monotonic within a ms window: %s > %s", a, b)
	}
}
