package app

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// PersistDeps holds optional persistence repositories for SessionService.
// All fields are nil-safe: when nil, the corresponding persistence is skipped
// and SessionService operates in pure in-memory mode (backward compatible).
type PersistDeps struct {
	Sessions     persist.SessionRepository
	Events       persist.EventRepository
	Ledger       persist.LedgerRepository
	Flows        persist.FlowRepository
	Intelligence persist.IntelligenceRepository
	Users        persist.UserRepository
	FuncRegistry *persist.FuncRegistry
}

// HealthChecker checks the health of external dependencies.
type HealthChecker interface {
	Health(ctx context.Context) error
}

// TokenLedgerReader reads token usage from the in-memory LLM ledger.
type TokenLedgerReader interface {
	Get(sessionID string) *TokenUsage
}

// TokenUsage is a snapshot of token usage for a session.
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	Calls        int
	TotalCostUSD float64
}
