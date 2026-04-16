package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// LLMCallRepository implements persist.LLMCallRepository using PostgreSQL.
type LLMCallRepository struct {
	pool pgxPool
}

// Compile-time interface assertion.
var _ persist.LLMCallRepository = (*LLMCallRepository)(nil)

// NewLLMCallRepository creates a new Postgres-backed per-call audit repository.
func NewLLMCallRepository(pool pgxPool) *LLMCallRepository {
	return &LLMCallRepository{pool: pool}
}

// Record inserts a single llm_calls audit row.
func (r *LLMCallRepository) Record(ctx context.Context, rec *persist.LLMCallRecord) error {
	if rec.ID == "" {
		var b [16]byte
		_, _ = rand.Read(b[:])
		rec.ID = hex.EncodeToString(b[:])
	}

	// session_id is a free-form text column (no FK), but the column itself is
	// nullable in the migration; pass NULL when empty so we don't pollute
	// indexes with empty strings.
	var sessionID any
	if rec.SessionID != "" {
		sessionID = rec.SessionID
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO llm_calls (
			id, session_id, transition_id, cpn_id,
			model_requested, model_resolved, endpoint, streamed,
			input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens, reasoning_tokens,
			cost_usd, request_messages, response_text, finish_reason, error, duration_ms
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7, $8,
			$9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18, $19
		)`,
		rec.ID, sessionID, rec.TransitionID, rec.CPNID,
		rec.ModelRequested, rec.ModelResolved, rec.Endpoint, rec.Streamed,
		rec.InputTokens, rec.OutputTokens, rec.CacheReadTokens, rec.CacheCreationTokens, rec.ReasoningTokens,
		rec.CostUSD, []byte(rec.RequestMessages), rec.ResponseText, rec.FinishReason, rec.Error, rec.DurationMs,
	)
	if err != nil {
		return fmt.Errorf("postgres llm_calls record: %w", err)
	}
	return nil
}
