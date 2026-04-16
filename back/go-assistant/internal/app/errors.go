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

// ErrPersistenceUnavailable is returned when the persistence layer fails or is
// unreachable during rehydration. It is distinct from ErrNoPersistence, which
// indicates the deployment is running without persistence by design. Handlers
// MUST map this to HTTP 503 per spec §4.2.
var ErrPersistenceUnavailable = errors.New("persistence unavailable")
