package app

import "errors"

// ErrSessionNotFound is returned when a session ID does not exist in the service.
var ErrSessionNotFound = errors.New("session not found")

// ErrSessionClosed is returned when an operation is attempted on a closed session.
var ErrSessionClosed = errors.New("session is closed")

// ErrInvalidInput is returned when required input parameters are missing or invalid.
var ErrInvalidInput = errors.New("invalid input")

// ErrSessionBusy is returned when a message is sent while the CPN is already running.
var ErrSessionBusy = errors.New("session is busy")

// ErrNoPersistence is returned when an operation requires persistence but none is configured.
var ErrNoPersistence = errors.New("persistence not configured")

// ErrSessionInactive is returned when a session exists in persistence but has no
// live in-memory execution state required for the requested operation. Callers
// (typically stream-only or HITL paths) should surface this to the user so they
// can prompt a new run rather than silently failing.
//
// Introduced by the ghost-session rehydration bugfix (spec REQ-003, §4.1, §4.2).
var ErrSessionInactive = errors.New("session inactive")

// ErrTransitionOrphaned is returned by ResolveHITL when the session is alive
// (or was alive in-process) but the specific HITL transition cannot be
// resolved because its token has already been consumed or the flow moved past
// the gate. Distinct from ErrSessionInactive, which represents a dead/idle
// session with no live producer at all (e.g. fresh rehydration).
//
// Frontends SHOULD treat this as a benign race (stale card, multi-tab, etc.)
// and dismiss the card with a neutral notice instead of a hard error banner.
// See spec-process-bugfix-tool-hitl-single-gate.md §4.3 / §9.4 (REQ-006).
var ErrTransitionOrphaned = errors.New("hitl transition already resolved or orphaned")

// ErrPersistenceUnavailable is returned when the persistence layer fails or is
// unreachable during rehydration. It is distinct from ErrNoPersistence, which
// indicates the deployment is running without persistence by design. Handlers
// MUST map this to HTTP 503 per spec §4.2.
var ErrPersistenceUnavailable = errors.New("persistence unavailable")
