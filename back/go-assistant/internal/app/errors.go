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
