package cpn

import (
	"sync"
	"sync/atomic"
	"time"
)

// ExecutionRecord holds immutable metrics from a single CPN execution.
// Created by executionTracker.Finalize() at the end of Run().
// Value type — cannot be mutated after creation (SEC-002).
type ExecutionRecord struct {
	CPNID            string
	CPNRole          string
	CPNDepth         int
	SessionID        string
	TransitionsFired int
	LLMCallCount     int
	TotalCostUSD     float64
	TokensProduced   int
	Duration         time.Duration
	Success          bool
	StartedAt        time.Time
	CompletedAt      time.Time
}

// executionTracker is a mutable, thread-safe counter object for one Run() call.
// Uses atomic.Int64 for hot-path counters to avoid mutex on the firing loop (PAT-001).
// Unexported — internal to the executor (GUD-001).
type executionTracker struct {
	cpnID, cpnRole   string
	cpnDepth         int
	sessionID        string
	startedAt        time.Time
	transitionsFired atomic.Int64
	llmCallCount     atomic.Int64
	tokensProduced   atomic.Int64
}

// newExecutionTracker creates a tracker with identity fields and start timestamp.
func newExecutionTracker(cpnID, cpnRole string, cpnDepth int, sessionID string) *executionTracker {
	return &executionTracker{
		cpnID:     cpnID,
		cpnRole:   cpnRole,
		cpnDepth:  cpnDepth,
		sessionID: sessionID,
		startedAt: time.Now(),
	}
}

// RecordTransitionFired increments transition count.
// If kind == NodeKindLLM, also increments LLM call count (REQ-003).
func (et *executionTracker) RecordTransitionFired(kind NodeKind) {
	et.transitionsFired.Add(1)
	if kind == NodeKindLLM {
		et.llmCallCount.Add(1)
	}
}

// RecordTokensProduced increments the token count by n.
func (et *executionTracker) RecordTokensProduced(n int) {
	et.tokensProduced.Add(int64(n))
}

// Finalize creates an immutable ExecutionRecord from the tracker state (REQ-004).
// Duration is computed from startedAt to now. Timestamps are set.
func (et *executionTracker) Finalize(success bool, costUSD float64) ExecutionRecord {
	now := time.Now()
	return ExecutionRecord{
		CPNID:            et.cpnID,
		CPNRole:          et.cpnRole,
		CPNDepth:         et.cpnDepth,
		SessionID:        et.sessionID,
		TransitionsFired: int(et.transitionsFired.Load()),
		LLMCallCount:     int(et.llmCallCount.Load()),
		TotalCostUSD:     costUSD,
		TokensProduced:   int(et.tokensProduced.Load()),
		Duration:         now.Sub(et.startedAt),
		Success:          success,
		StartedAt:        et.startedAt,
		CompletedAt:      now,
	}
}

// CostProvider abstracts cost lookup for a session.
// Block 7 (TokenLedger) will implement this interface (REQ-007).
type CostProvider interface {
	SessionCostUSD(sessionID string) float64
}

// MetricsRecorder is an append-only store of CPN execution records (REQ-005).
// Thread-safe. No update or delete methods (SEC-001).
// Exported — shared across CPNs for aggregate ranking (GUD-002).
type MetricsRecorder struct {
	records []ExecutionRecord
	mu      sync.RWMutex
}

// NewMetricsRecorder creates an empty MetricsRecorder.
func NewMetricsRecorder() *MetricsRecorder {
	return &MetricsRecorder{}
}

// Append adds an ExecutionRecord to the store.
// This is the only mutation method — append-only (PAT-002, SEC-001).
func (mr *MetricsRecorder) Append(rec *ExecutionRecord) {
	mr.mu.Lock()
	defer mr.mu.Unlock()
	mr.records = append(mr.records, *rec)
}

// Records returns a copy of all records (REQ-006).
// Mutating the returned slice does not affect internal state (PAT-004).
func (mr *MetricsRecorder) Records() []ExecutionRecord {
	mr.mu.RLock()
	defer mr.mu.RUnlock()
	if len(mr.records) == 0 {
		return nil
	}
	cp := make([]ExecutionRecord, len(mr.records))
	copy(cp, mr.records)
	return cp
}

// Len returns the number of recorded executions.
func (mr *MetricsRecorder) Len() int {
	mr.mu.RLock()
	defer mr.mu.RUnlock()
	return len(mr.records)
}
