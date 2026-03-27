package cpn

import "errors"

// ── v1.0 CPN Core Errors ──────────────────────────────────────────────────

// ErrColorMismatch is returned when a token's color does not match
// the place's color set constraint.
var ErrColorMismatch = errors.New("token color does not match place color set")

// ErrEmptyPlace is returned when a transition attempts to consume
// from a place that has no tokens.
var ErrEmptyPlace = errors.New("place has no tokens to consume")

// ErrInvalidArc is returned when an arc references a non-existent
// place or transition.
var ErrInvalidArc = errors.New("arc references non-existent place or transition")

// ErrDeadlock is returned when no transitions are enabled and the CPN
// has not reached a terminal marking.
var ErrDeadlock = errors.New("no enabled transitions and no terminal marking")

// ErrTimeout is returned when a transition exceeds its configured
// execution timeout.
var ErrTimeout = errors.New("transition execution exceeded timeout")

// ErrSpaceMismatch is returned when an arc connects a place and
// transition in incompatible communication spaces.
var ErrSpaceMismatch = errors.New("arc crosses incompatible communication spaces")

// ErrSpaceViolation is returned when a token is deposited into a place
// whose space does not accept the token's space attribute.
var ErrSpaceViolation = errors.New("token space does not match place space")

// ErrSubNetFailed is returned when a child CPN terminates with an error.
var ErrSubNetFailed = errors.New("child CPN terminated with error")

// ErrNoHITLWaiting is returned when a HITL response arrives but no
// transition is waiting for human input.
var ErrNoHITLWaiting = errors.New("no HITL transition waiting for input")

// ErrInvalidNodeKind is returned when a transition's NodeKind is not
// recognized by the executor dispatch.
var ErrInvalidNodeKind = errors.New("unrecognized transition node kind")

// ErrCentaurianGuard is returned when a transition fires in Centaurian
// mode without the required human approval token.
var ErrCentaurianGuard = errors.New("centaurian mode requires human approval token")

// ── v1.1 Building Block Errors ─────────────────────────────────────────────

// ErrValidationFailed is returned when a token fails schema validation
// after exhausting all correction attempts.
var ErrValidationFailed = errors.New("token failed schema validation after max corrections")

// ErrCircuitOpen is returned when a circuit breaker is in the open state
// and rejects the request.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// ErrRateLimited is returned when a request exceeds the configured
// rate limit.
var ErrRateLimited = errors.New("request exceeds rate limit")

// ErrProviderUnavailable is returned when no LLM provider is reachable
// after exhausting retries.
var ErrProviderUnavailable = errors.New("no LLM provider available after retries")

// ErrBudgetExceeded is returned when the token or cost budget for a
// session or CPN is exhausted.
var ErrBudgetExceeded = errors.New("token or cost budget exceeded")

// ErrHITLRejected is returned when a human explicitly rejects a
// proposed action.
var ErrHITLRejected = errors.New("human rejected proposed action")

// ErrHITLMaxRevisions is returned when a HITL revision loop exceeds
// the maximum allowed iterations.
var ErrHITLMaxRevisions = errors.New("HITL revision loop exceeded max iterations")

// ── v1.2 OpenRouter HTTP Errors ────────────────────────────────────────────

// ErrBadRequest is returned for OpenRouter HTTP 400 responses.
var ErrBadRequest = errors.New("OpenRouter 400: invalid request parameters")

// ErrUnauthorized is returned for OpenRouter HTTP 401 responses.
var ErrUnauthorized = errors.New("OpenRouter 401: invalid API key")

// ErrInsufficientCredits is returned for OpenRouter HTTP 402 responses.
var ErrInsufficientCredits = errors.New("OpenRouter 402: insufficient credits")

// ErrForbidden is returned for OpenRouter HTTP 403 responses.
var ErrForbidden = errors.New("OpenRouter 403: forbidden")

// ErrNotFound is returned for OpenRouter HTTP 404 responses.
var ErrNotFound = errors.New("OpenRouter 404: model or endpoint not found")

// ErrRequestTimeout is returned for OpenRouter HTTP 408 responses.
var ErrRequestTimeout = errors.New("OpenRouter 408: request timeout")

// ErrPayloadTooLarge is returned for OpenRouter HTTP 413 responses.
var ErrPayloadTooLarge = errors.New("OpenRouter 413: payload too large")

// ErrUnprocessableEntity is returned for OpenRouter HTTP 422 responses.
var ErrUnprocessableEntity = errors.New("OpenRouter 422: unprocessable entity")

// ErrEdgeTimeout is returned for OpenRouter HTTP 504 responses.
var ErrEdgeTimeout = errors.New("OpenRouter 504: edge timeout")

// ErrProviderOverloaded is returned for OpenRouter HTTP 503 responses.
var ErrProviderOverloaded = errors.New("OpenRouter 503: provider overloaded")
