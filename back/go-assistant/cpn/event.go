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
)

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
