package persist

import (
	"context"
	"time"
)

// SessionRepository manages user session persistence.
type SessionRepository interface {
	// Create persists a new session. Returns ErrSessionExists if the ID is taken.
	Create(ctx context.Context, session *SessionRecord) error
	// Get retrieves a session by ID. Returns ErrSessionNotFound if absent.
	Get(ctx context.Context, sessionID string) (*SessionRecord, error)
	// GetByUserID returns all sessions for a user (not paginated; bounded).
	GetByUserID(ctx context.Context, userID string) ([]*SessionRecord, error)
	// AppendMessage adds a message to a session. Returns ErrSessionNotFound or ErrSessionClosed.
	AppendMessage(ctx context.Context, sessionID string, msg *MessageRecord) error
	// UpdateState changes the session lifecycle state.
	UpdateState(ctx context.Context, sessionID string, state SessionState) error
	// Touch updates the session's LastActivityAt timestamp.
	Touch(ctx context.Context, sessionID string) error
	// Close marks a session as closed and sets ClosedAt.
	Close(ctx context.Context, sessionID string) error
	// ListExpired returns IDs of sessions with LastActivityAt before the given time.
	ListExpired(ctx context.Context, before time.Time) ([]string, error)
	// Delete permanently removes a session.
	Delete(ctx context.Context, sessionID string) error
}

// EventRepository manages append-only CPN event persistence (Axiom A9).
// No Update or Delete methods.
type EventRepository interface {
	// Append persists one or more events. Returns ErrEventAppendFailed on validation error.
	Append(ctx context.Context, events ...*EventRecord) error
	// QueryBySession returns events for a session with cursor pagination.
	QueryBySession(ctx context.Context, sessionID string, opts *EventQueryOpts) (*Page[*EventRecord], error)
	// QueryByCPN returns events for a CPN instance with cursor pagination.
	QueryByCPN(ctx context.Context, cpnID string, opts *EventQueryOpts) (*Page[*EventRecord], error)
	// QueryByType returns events of a type within a time range with cursor pagination.
	QueryByType(ctx context.Context, eventType string, from, to time.Time, opts *EventQueryOpts) (*Page[*EventRecord], error)
	// Count returns the number of events matching the query options.
	Count(ctx context.Context, opts *EventQueryOpts) (int64, error)
}

// LedgerRepository manages token usage and cost tracking.
type LedgerRepository interface {
	// Record upserts a ledger entry: creates if new, increments if existing.
	Record(ctx context.Context, rec *LedgerRecord) error
	// GetBySession retrieves the ledger for a session. Returns ErrLedgerNotFound if absent.
	GetBySession(ctx context.Context, sessionID string) (*LedgerRecord, error)
	// QueryByDate returns all ledger records updated on the given date.
	QueryByDate(ctx context.Context, date time.Time) ([]*LedgerRecord, error)
	// SetDailyTotal sets the daily total cost for a session on a date.
	SetDailyTotal(ctx context.Context, sessionID string, date time.Time, totalUSD float64) error
	// AggregateByDateRange returns aggregated metrics over a date range.
	AggregateByDateRange(ctx context.Context, from, to time.Time) (*LedgerAggregate, error)
}

// FlowRepository manages crystallized CPN topology persistence.
type FlowRepository interface {
	// Save persists a flow record.
	Save(ctx context.Context, flow *FlowRecord) error
	// GetByHash retrieves a flow by hash. Returns ErrFlowNotFound if absent or soft-deleted.
	GetByHash(ctx context.Context, hash string) (*FlowRecord, error)
	// List returns flows with optional role filter and cursor pagination.
	List(ctx context.Context, opts *FlowListOpts) (*Page[*FlowRecord], error)
	// UpdateStats updates the execution statistics for a flow.
	UpdateStats(ctx context.Context, hash string, stats *FlowStats) error
	// Delete soft-deletes a flow by setting DeletedAt.
	Delete(ctx context.Context, hash string) error
}

// IntelligenceRepository manages CPN execution intelligence and ranking.
type IntelligenceRepository interface {
	// RecordExecution persists an execution record.
	RecordExecution(ctx context.Context, rec *ExecutionRecord) error
	// QueryByRole returns executions for a role within a time range.
	QueryByRole(ctx context.Context, role string, from, to time.Time) ([]*ExecutionRecord, error)
	// Aggregate computes ranking metrics for a role within a time range.
	Aggregate(ctx context.Context, role string, from, to time.Time) (*RankingMetrics, error)
	// TopFlows returns the top n flows by ranking score.
	TopFlows(ctx context.Context, n int) ([]*RankedFlow, error)
}

// HITLRepository manages human-in-the-loop pending request persistence.
type HITLRepository interface {
	// Enqueue adds a pending HITL request.
	Enqueue(ctx context.Context, req *HITLPendingRequest) error
	// Dequeue retrieves and removes a pending request. Returns ErrHITLNotFound if absent.
	Dequeue(ctx context.Context, sessionID, transitionID string) (*HITLPendingRequest, error)
	// ListPending returns all pending requests for a session.
	ListPending(ctx context.Context, sessionID string) ([]*HITLPendingRequest, error)
	// Expire removes requests older than the given duration. Returns the count removed.
	Expire(ctx context.Context, olderThan time.Duration) (int64, error)
}
