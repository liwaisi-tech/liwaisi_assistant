package gate

import (
	"sync"
)

// sessionCounters holds the per-session usage aggregates the gate
// compares against the policy defaults. All fields are monotonically
// non-decreasing during a session's lifetime.
type sessionCounters struct {
	CPUSeconds       int
	BytesWritten     int64
	OutboundRequests int
}

// BudgetTracker is an in-memory, session-keyed counter set. The gate
// pre-flight checks the estimate against (defaults - current) and the
// caller records the actual consumption post-flight via RecordActual so
// subsequent checks shrink the available budget accordingly.
//
// Concurrency: safe for unbounded concurrent callers. One mutex covers
// the whole map; contention is negligible because the critical section
// is ~O(1) arithmetic.
type BudgetTracker struct {
	mu       sync.RWMutex
	sessions map[string]*sessionCounters
}

// NewBudgetTracker returns an empty tracker.
func NewBudgetTracker() *BudgetTracker {
	return &BudgetTracker{sessions: make(map[string]*sessionCounters)}
}

// Current returns a copy of the current counters for sessionID (all zeros
// when the session has never been seen).
func (b *BudgetTracker) Current(sessionID string) BudgetEstimate {
	b.mu.RLock()
	defer b.mu.RUnlock()
	c, ok := b.sessions[sessionID]
	if !ok {
		return BudgetEstimate{}
	}
	return BudgetEstimate{
		CPUSeconds:       c.CPUSeconds,
		BytesWritten:     c.BytesWritten,
		OutboundRequests: c.OutboundRequests,
	}
}

// CheckEstimate reports whether sessionID has enough headroom for
// estimate given the policy defaults. Returns ("", true) on headroom,
// (reason, false) on exhaustion. The reason string is suitable for the
// audit log (e.g. "cpu_budget_exceeded").
func (b *BudgetTracker) CheckEstimate(sessionID string, defaults PolicyDefaults, estimate BudgetEstimate) (string, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	c, ok := b.sessions[sessionID]
	if !ok {
		c = &sessionCounters{}
	}
	if c.CPUSeconds+estimate.CPUSeconds > defaults.CPUSecondsBudget {
		return "cpu_budget_exceeded", false
	}
	if c.BytesWritten+estimate.BytesWritten > defaults.BytesWrittenBudget {
		return "bytes_budget_exceeded", false
	}
	if c.OutboundRequests+estimate.OutboundRequests > defaults.OutboundRequestsBudget {
		return "outbound_budget_exceeded", false
	}
	return "", true
}

// RecordActual adds actual to sessionID's counters (creating the entry on
// first call). Always returns nil; kept as (err) to mirror the HostGate
// interface.
func (b *BudgetTracker) RecordActual(sessionID string, actual BudgetEstimate) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.sessions[sessionID]
	if !ok {
		c = &sessionCounters{}
		b.sessions[sessionID] = c
	}
	c.CPUSeconds += actual.CPUSeconds
	c.BytesWritten += actual.BytesWritten
	c.OutboundRequests += actual.OutboundRequests
}

// Reset clears sessionID's counters. Called when a session terminates.
func (b *BudgetTracker) Reset(sessionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.sessions, sessionID)
}

// DefaultEstimate returns a minimal budget estimate for a bash op with
// no caller-supplied numbers. 1 CPU-second is a reasonable floor for the
// pre-flight check, 0 bytes written, 0 outbound requests. Callers that
// want to enforce AC-003 (512 MiB write) build their own estimate.
func DefaultEstimate() BudgetEstimate {
	return BudgetEstimate{CPUSeconds: 1}
}
