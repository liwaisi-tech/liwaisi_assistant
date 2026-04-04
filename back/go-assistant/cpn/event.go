package cpn

import "time"

// EventType classifies events emitted by CPN transitions and sub-CPNs.
type EventType string

const (
	// EventTransitionFired is emitted when a transition completes firing.
	EventTransitionFired EventType = "transition_fired"

	// EventSubNetStarted is emitted when a sub-CPN begins execution.
	EventSubNetStarted EventType = "subnet_started"

	// EventSubNetCompleted is emitted when a sub-CPN reaches a terminal marking.
	EventSubNetCompleted EventType = "subnet_completed"

	// EventSubNetFailed is emitted when a sub-CPN terminates with an error.
	EventSubNetFailed EventType = "subnet_failed"

	// EventHITLRequested is emitted when a HITL transition blocks for human input.
	EventHITLRequested EventType = "hitl_requested"

	// EventHITLResolved is emitted when a HITL transition receives human input.
	EventHITLResolved EventType = "hitl_resolved"

	// EventTokenDeposited is emitted when a token is deposited into a place.
	EventTokenDeposited EventType = "token_deposited"

	// EventModeSwitch is emitted when the CPN switches between MAS and Centaurian modes.
	EventModeSwitch EventType = "mode_switch"

	// EventStreamChunk is emitted for each chunk of an LLM streaming response.
	EventStreamChunk EventType = "stream_chunk"

	// EventTransitionStarted is emitted after tokens are consumed from input
	// places and before the transition dispatch goroutine begins execution.
	// Carries a TransitionStartedPayload with input token snapshots.
	EventTransitionStarted EventType = "transition_started"

	// EventTransitionCompleted is emitted after the transition dispatch
	// returns (success or failure). Carries a TransitionCompletedPayload
	// with output token snapshots, cost, duration, and optional error.
	EventTransitionCompleted EventType = "transition_completed"
)

// TransitionStartedPayload captures input tokens consumed before firing.
type TransitionStartedPayload struct {
	InputTokens []TokenSnapshot `json:"input_tokens"`
}

// TransitionCompletedPayload captures results after a transition fires.
type TransitionCompletedPayload struct {
	OutputTokens []TokenSnapshot `json:"output_tokens"`
	CostUSD      float64         `json:"cost_usd"`
	DurationMs   int64           `json:"duration_ms"`
	Error        string          `json:"error,omitempty"`
}

// TokenSnapshot is a serializable, truncated representation of a Token.
// Used in event payloads to provide observability without transmitting
// full (potentially large) payloads.
type TokenSnapshot struct {
	Color          string `json:"color"`
	PayloadPreview string `json:"payload_preview"`
	Space          string `json:"space"`
	OriginID       string `json:"origin_id"`
	OriginKind     string `json:"origin_kind"`
}

// Event is an append-only record emitted by transitions and sub-CPNs,
// consumed by observer transitions in the observation space.
type Event struct {
	// ID uniquely identifies this event.
	ID string

	// Type classifies the event.
	Type EventType

	// SessionID links this event to a user session.
	SessionID string

	// CPNID identifies the CPN instance that emitted this event.
	CPNID string

	// CPNDepth is the depth of the emitting CPN (0=root, 1=domain, 2+=worker).
	CPNDepth int

	// CPNRole is the role label of the emitting CPN.
	CPNRole string

	// TransitionID identifies the transition that triggered this event.
	TransitionID string

	// TransitionKind is the kind of the transition that triggered this event.
	TransitionKind NodeKind

	// Token is an optional snapshot of the token involved in this event.
	// Nil when the event does not carry a token snapshot.
	Token *Token

	// Payload carries event-specific data. Consumers must type-assert.
	Payload any

	// Timestamp records when this event was created.
	Timestamp time.Time
}
