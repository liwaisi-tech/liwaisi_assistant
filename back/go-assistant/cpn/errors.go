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

// ErrHITLMisconfigured is returned when a NodeKindHITL transition has
// nil HITLConfig or nil Channel.
var ErrHITLMisconfigured = errors.New("HITL transition misconfigured")

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

// ErrEdgeTimeout is returned for OpenRouter HTTP 524 responses.
var ErrEdgeTimeout = errors.New("OpenRouter 524: edge timeout")

// ErrProviderOverloaded is returned for OpenRouter HTTP 529 responses.
var ErrProviderOverloaded = errors.New("OpenRouter 529: provider overloaded")

// ── v1.3 Block 9 — fireLLM Errors ───────────────────────────────────────────

// ErrToolCallLoopExceeded is returned when the agentic tool-call loop
// exceeds MaxToolCallIterations without producing final content.
var ErrToolCallLoopExceeded = errors.New("tool call loop exceeded max iterations")

// ErrDisallowedTool is returned when the LLM requests a tool not in
// the transition's LLMTools allowlist.
var ErrDisallowedTool = errors.New("LLM requested disallowed tool")

// ── v1.4 Block 17 — Session Errors ─────────────────────────────────────────

// ErrSessionClosed is returned when operations are attempted on a closed session.
var ErrSessionClosed = errors.New("session is closed")

// ErrHITLAlreadyRegistered is returned when a HITL channel is registered
// for a transition ID that already has one.
var ErrHITLAlreadyRegistered = errors.New("HITL channel already registered for transition")

// ── v1.5 Personality Errors ───────────────────────────────────────────────

// ErrEticaViolation is returned when a modification violates base ethics constraints.
var ErrEticaViolation = errors.New("modification violates base ethics constraints")

// ErrInvalidHierarchy is returned when a principle hierarchy is invalid.
var ErrInvalidHierarchy = errors.New("invalid principle hierarchy")

// ErrEticaCannotBeLast is returned when etica principle is placed as lowest priority.
var ErrEticaCannotBeLast = errors.New("etica principle cannot be lowest priority")

// ── GAP-4 Synthesis + Instantiate Errors ───────────────────────────────────

// ErrUnsafePrimitive is returned when an agent-authored topology
// references a guard/executor/factory name not registered in SafeRegistry.
var ErrUnsafePrimitive = errors.New("topology references unsafe primitive")

// ErrTopologyTooLarge is returned when a synthesised topology exceeds the
// configured size cap (REQ-020, CON-001).
var ErrTopologyTooLarge = errors.New("topology exceeds size cap")

// ErrUnresolvedArc is returned when a transition arc references a place
// that does not exist in the topology.
var ErrUnresolvedArc = errors.New("topology arc references unknown place")

// ErrNoTerminal is returned when a topology has no terminal place
// (CON-002).
var ErrNoTerminal = errors.New("topology has no terminal place")

// ErrDisallowedKind is returned when a synthesised topology declares a
// NodeKind that is forbidden for agent-authored flows (e.g.
// register_tool per SEC-002).
var ErrDisallowedKind = errors.New("topology declares disallowed NodeKind")

// ErrTopologyParseFailed is returned when the synthesiser LLM response
// cannot be parsed as a CPNTopology document.
var ErrTopologyParseFailed = errors.New("topology parse failed")

// ErrTopologyRejected is returned when an admin has rejected a flow
// via the admin API and instantiation is attempted afterwards.
var ErrTopologyRejected = errors.New("topology rejected by admin")

// ErrDeprecatedDependency is returned when a topology references a tool
// (or primitive) that has since been deprecated.
var ErrDeprecatedDependency = errors.New("topology references deprecated dependency")

// ErrSpaceIsolationViolated is returned when a topology has an arc from
// a surface place into a computation place (except via HITL) — CON-003.
var ErrSpaceIsolationViolated = errors.New("topology violates space isolation")

// errNoMaterialiser is the internal sentinel used when a NodeKindInstantiate
// transition fires without a materialiser wired (dev-mode bootstrap bug).
var errNoMaterialiser = errors.New("topology materialiser not wired")
