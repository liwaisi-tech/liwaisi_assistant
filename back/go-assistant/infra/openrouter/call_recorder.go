package openrouter

import (
	"context"
	"time"
)

// CallRecord is the per-call audit payload emitted after every LLM invocation.
// It is intentionally defined in this package so the openrouter Client has zero
// dependency on the persistence layer; main.go wires an adapter that converts
// CallRecord into the persist.LLMCallRecord DTO.
type CallRecord struct {
	SessionID      string
	TransitionID   string
	CPNID          string
	ModelRequested string
	ModelResolved  string
	Endpoint       string
	Streamed       bool

	// RequestMessages is the full conversation sent to the model
	// (system + history + user). Each entry is {role, content}.
	RequestMessages []CallMessage

	ResponseText        string
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
	ReasoningTokens     int
	CostUSD             float64

	FinishReason string
	Error        string
	Duration     time.Duration
	CreatedAt    time.Time
}

// CallMessage is a minimal {role, content} pair for the audit payload.
type CallMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CallRecorder persists per-call audit records. Implementations MUST be
// non-blocking from the caller's perspective (use a background goroutine
// or buffered channel internally) — the LLM hot path must not wait on the DB.
type CallRecorder interface {
	RecordCall(ctx context.Context, rec CallRecord)
}
