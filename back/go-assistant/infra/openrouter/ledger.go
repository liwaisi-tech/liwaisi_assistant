package openrouter

import (
	"sync"
	"time"
)

// ── TokenLedger ──────────────────────────────────────────────────────────────

// TokenLedger tracks per-session token usage and cost. Thread-safe.
type TokenLedger struct {
	records     map[string]*SessionTokenRecord
	dailyTotals map[string]float64 // date (YYYY-MM-DD) → authoritative USD from /activity
	mu          sync.RWMutex
}

// SessionTokenRecord accumulates usage for one session.
type SessionTokenRecord struct {
	SessionID           string
	InputTokens         int
	OutputTokens        int
	TotalCostUSD        float64
	Calls               int
	LastUpdated         time.Time
	CacheReadTokens     int
	CacheCreationTokens int
}

// NewTokenLedger creates an initialized TokenLedger.
func NewTokenLedger() *TokenLedger {
	return &TokenLedger{
		records:     make(map[string]*SessionTokenRecord),
		dailyTotals: make(map[string]float64),
	}
}

// Record accumulates token usage and cost for a session. Thread-safe.
func (l *TokenLedger) Record(sessionID string, in, out int, costUSD float64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	rec, ok := l.records[sessionID]
	if !ok {
		rec = &SessionTokenRecord{SessionID: sessionID}
		l.records[sessionID] = rec
	}

	rec.InputTokens += in
	rec.OutputTokens += out
	rec.TotalCostUSD += costUSD
	rec.Calls++
	rec.LastUpdated = time.Now()
}

// Get returns a snapshot of the session record, or nil for unknown sessions.
// The returned value is a copy — safe to read without holding any lock.
func (l *TokenLedger) Get(sessionID string) *SessionTokenRecord {
	l.mu.RLock()
	defer l.mu.RUnlock()
	rec, ok := l.records[sessionID]
	if !ok {
		return nil
	}
	cp := *rec
	return &cp
}

// RecordWithCache accumulates token usage including cache token fields. Thread-safe.
func (l *TokenLedger) RecordWithCache(sessionID string, in, out, cacheRead, cacheCreate int, costUSD float64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	rec, ok := l.records[sessionID]
	if !ok {
		rec = &SessionTokenRecord{SessionID: sessionID}
		l.records[sessionID] = rec
	}

	rec.InputTokens += in
	rec.OutputTokens += out
	rec.CacheReadTokens += cacheRead
	rec.CacheCreationTokens += cacheCreate
	rec.TotalCostUSD += costUSD
	rec.Calls++
	rec.LastUpdated = time.Now()
}

// SetDailyTotal stores the authoritative daily cost from /activity. Thread-safe.
func (l *TokenLedger) SetDailyTotal(date string, totalUSD float64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.dailyTotals[date] = totalUSD
}

// GetDailyTotal returns the authoritative daily cost, or (0, false) if not synced.
func (l *TokenLedger) GetDailyTotal(date string) (float64, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	total, ok := l.dailyTotals[date]
	return total, ok
}
