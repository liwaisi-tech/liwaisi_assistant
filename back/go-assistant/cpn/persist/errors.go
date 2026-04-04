package persist

import "errors"

// Sentinel errors for persistence operations.
// Each error is distinct and compared via errors.Is().
// Note: ErrSessionClosed is NOT equal to cpn.ErrSessionClosed;
// cpn.ErrSessionClosed is for runtime HITL/session operations,
// while persist.ErrSessionClosed is for persistence operations
// on closed or expired sessions.
var (
	// ErrSessionNotFound is returned when a session ID does not exist.
	ErrSessionNotFound = errors.New("persist: session not found")

	// ErrSessionExists is returned when creating a session with a duplicate ID.
	ErrSessionExists = errors.New("persist: session already exists")

	// ErrSessionClosed is returned when mutating a closed or expired session.
	ErrSessionClosed = errors.New("persist: session is closed or expired")

	// ErrFlowNotFound is returned when a flow hash does not exist or is soft-deleted.
	ErrFlowNotFound = errors.New("persist: flow not found")

	// ErrEventAppendFailed is returned when event append validation fails.
	ErrEventAppendFailed = errors.New("persist: event append failed")

	// ErrHITLNotFound is returned when a HITL request does not exist.
	ErrHITLNotFound = errors.New("persist: HITL request not found")

	// ErrHITLExpired is returned when a HITL request has passed its expiry.
	ErrHITLExpired = errors.New("persist: HITL request expired")

	// ErrLedgerNotFound is returned when a ledger record does not exist.
	ErrLedgerNotFound = errors.New("persist: ledger record not found")

	// ErrInvalidInput is returned when method arguments fail validation.
	ErrInvalidInput = errors.New("persist: invalid input")
)
