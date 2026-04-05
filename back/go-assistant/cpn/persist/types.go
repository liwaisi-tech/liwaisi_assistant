package persist

import (
	"encoding/json"
	"time"
)

// Page holds a paginated result set with cursor-based navigation.
type Page[T any] struct {
	Items      []T
	NextCursor string // empty when no more results
	HasMore    bool
}

// SessionState represents the lifecycle state of a session.
type SessionState string

const (
	// SessionActive indicates the session is open and accepting messages.
	SessionActive SessionState = "active"
	// SessionClosed indicates the session was explicitly closed.
	SessionClosed SessionState = "closed"
	// SessionExpired indicates the session timed out.
	SessionExpired SessionState = "expired"
)

// SessionRecord is the persistence DTO for a user session.
// It combines data from cpn.Session (ID, UserID, Channel, CreatedAt),
// internal/app.sessionState (State), and the integration layer
// (LastActivityAt, ClosedAt).
type SessionRecord struct {
	ID             string
	UserID         string
	Channel        string
	State          SessionState
	CreatedAt      time.Time
	LastActivityAt time.Time
	ClosedAt       *time.Time
	Metadata       json.RawMessage
	Messages       []*MessageRecord

	// Chat management fields (migration 008).
	Title               string     // Auto-generated or user-set chat title.
	DeletedAt           *time.Time // Soft-delete timestamp; nil = active.
	ForkedFromSessionID string     // Source session ID if forked; empty = original.
	ForkMessageCount    int        // Number of messages copied at fork time.
}

// SessionListItem is a lightweight DTO for session listing.
// Avoids loading full message history; enriched via JOINs with
// token_ledger and messages tables.
type SessionListItem struct {
	ID                  string
	Title               string
	State               SessionState
	LastMessagePreview  string // First 120 chars of last message content.
	LastActivityAt      time.Time
	CreatedAt           time.Time
	TotalCostUSD        float64 // From token_ledger LEFT JOIN.
	MessageCount        int     // COUNT(messages) for this session.
	ForkedFromSessionID string  // Empty if original.
}

// SessionListOpts provides filtering and pagination for session listing.
type SessionListOpts struct {
	Limit  int    // Default: 50, max: 100.
	Cursor string // Keyset cursor: "last_activity_at_unix|session_id".
}

// MessageRecord is the persistence DTO for a conversation message.
// cpn.Message does not carry SessionID; it is injected by the integration
// layer via MessageToRecord.
type MessageRecord struct {
	ID        string
	SessionID string
	Role      string
	Content   string
	CPNID     string
	CPNRole   string
	CPNDepth  int
	Timestamp time.Time
}

// EventRecord is the persistence DTO for a CPN event.
// Fields like TokenSnapshot and Payload are serialized to json.RawMessage
// from cpn.Event's *Token and any types via EventToRecord.
type EventRecord struct {
	ID             string
	Type           string
	SessionID      string
	CPNID          string
	CPNRole        string
	CPNDepth       int
	TransitionID   string
	TransitionKind string
	TokenSnapshot  json.RawMessage
	Payload        json.RawMessage
	Timestamp      time.Time
}

// EventQueryOpts provides filtering and pagination for event queries.
type EventQueryOpts struct {
	SessionID string
	CPNID     string
	EventType string
	From      time.Time
	To        time.Time
	Limit     int
	Cursor    string
}

// LedgerRecord is the persistence DTO for token usage and cost tracking.
type LedgerRecord struct {
	SessionID     string
	InputTokens   int64
	OutputTokens  int64
	Calls         int64
	TotalCostUSD  float64
	DailyTotalUSD float64
	LastUpdated   time.Time
}

// LedgerAggregate holds aggregated ledger metrics over a date range.
type LedgerAggregate struct {
	TotalInputTokens  int64
	TotalOutputTokens int64
	TotalCalls        int64
	TotalCostUSD      float64
}

// FlowRecord is the persistence DTO for a crystallized CPN topology.
// Delete sets DeletedAt (soft delete); GetByHash and List skip soft-deleted records.
type FlowRecord struct {
	Hash            string
	Role            string
	TopologyJSON    json.RawMessage
	FunctionMapping json.RawMessage
	Stats           FlowStats
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
}

// FlowStats holds execution statistics for a flow.
type FlowStats struct {
	ExecutionCount int64
	SuccessRate    float64
	AvgCostUSD     float64
	AvgDurationMs  int64
}

// FlowListOpts provides filtering and pagination for flow listing.
type FlowListOpts struct {
	Role   string
	Limit  int
	Cursor string
}

// ExecutionRecord is the persistence DTO for a CPN execution.
// Aligned with cpn.ExecutionRecord fields; DurationMs is converted
// from time.Duration via Milliseconds(). ID is generated at the
// persistence layer.
type ExecutionRecord struct {
	ID               string
	CPNID            string
	CPNRole          string
	CPNDepth         int
	SessionID        string
	TransitionsFired int
	LLMCalls         int
	ToolCalls        int
	TokensProduced   int
	TotalCostUSD     float64
	DurationMs       int64
	Success          bool
	StartedAt        time.Time
	CompletedAt      time.Time
}

// RankingMetrics holds aggregated execution metrics for a role.
type RankingMetrics struct {
	Role           string
	AvgLLMCalls    float64
	AvgToolCalls   float64
	AvgCostUSD     float64
	AvgDurationMs  float64
	SuccessRate    float64
	ExecutionCount int64
}

// RankedFlow is a flow with a computed ranking score.
type RankedFlow struct {
	Hash  string
	Role  string
	Score float64
	Stats FlowStats
}

// UserRecord is the persistence DTO for an authenticated user.
// ID is Google's 'sub' claim (immutable, unique per account).
type UserRecord struct {
	ID        string
	Email     string
	Name      string
	Picture   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// HITLPendingRequest is the persistence DTO for a pending human-in-the-loop request.
// Proposal is populated by the integration layer from the HITL transition's
// input place tokens.
type HITLPendingRequest struct {
	SessionID    string
	TransitionID string
	CPNID        string
	CPNRole      string
	Proposal     json.RawMessage
	CreatedAt    time.Time
	ExpiresAt    time.Time
}
