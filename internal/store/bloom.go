package store

// BloomState mirrors the Postgres bloom_state enum. Values are from
// spec.md (newer, 4-state) and are the source of truth.
type BloomState string

const (
	BloomBudding BloomState = "budding"
	BloomPartial BloomState = "partial"
	BloomFull    BloomState = "full"
	BloomFading  BloomState = "fading"
)

// ValidBloomStates lists all accepted values in the order the user picks
// them in the UI (budding → fading reads as a season's arc).
var ValidBloomStates = []BloomState{
	BloomBudding, BloomPartial, BloomFull, BloomFading,
}

// Valid reports whether b is a member of the enum. Useful at API boundaries
// so bad input is rejected before a SQL round-trip.
func (b BloomState) Valid() bool {
	switch b {
	case BloomBudding, BloomPartial, BloomFull, BloomFading:
		return true
	default:
		return false
	}
}
