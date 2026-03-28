package cpn

// Mode determines the firing semantics of the CPN.
type Mode string

const (
	// ModeMAS enables independent agent mode where transitions fire autonomously.
	ModeMAS Mode = "mas"

	// ModeCentaurian enables co-trigger mode where human approval gates transition firing.
	ModeCentaurian Mode = "centaurian"
)

// centaurianGuard returns true only when tokens contain BOTH a human-origin
// and a non-human-origin token. This is the core Centaurian invariant:
// neither human nor AI can trigger computation alone.
//
// Used by effectiveGuard for automatic injection in ModeCentaurian.
func centaurianGuard(tokens []*Token) bool {
	var hasHuman, hasAI bool
	for _, tok := range tokens {
		if tok.IsHumanOrigin() {
			hasHuman = true
		} else {
			hasAI = true
		}
		if hasHuman && hasAI {
			return true
		}
	}
	return false
}

// State represents the lifecycle state of a CPN instance.
type State string

const (
	// StateIdle indicates the CPN has not started firing.
	StateIdle State = "idle"

	// StateRunning indicates the CPN is actively firing transitions.
	StateRunning State = "running"

	// StateWaiting indicates the CPN is blocked on external input (HITL or sub-CPN).
	StateWaiting State = "waiting"

	// StateCompleted indicates the CPN has reached a terminal marking.
	StateCompleted State = "completed"

	// StateFailed indicates the CPN terminated due to an unrecoverable error.
	StateFailed State = "failed"
)
