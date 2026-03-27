package cpn

// Mode determines the firing semantics of the CPN.
type Mode string

const (
	// ModeMAS enables independent agent mode where transitions fire autonomously.
	ModeMAS Mode = "mas"

	// ModeCentaurian enables co-trigger mode where human approval gates transition firing.
	ModeCentaurian Mode = "centaurian"
)

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
