package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// GateDecisionsStore persists the host_gate_decisions audit log (GAP-6).
type GateDecisionsStore struct {
	pool pgxPool
}

var _ persist.GateDecisionRepository = (*GateDecisionsStore)(nil)

// NewGateDecisionsStore constructs a store bound to pool.
func NewGateDecisionsStore(pool pgxPool) *GateDecisionsStore {
	return &GateDecisionsStore{pool: pool}
}

// Insert appends one decision row. PAT-001: accepts empty string for
// optional fields and normalises them to SQL NULL via pointer handling.
func (s *GateDecisionsStore) Insert(ctx context.Context, rec *persist.GateDecisionRecord) error {
	if rec == nil {
		return fmt.Errorf("gate_decisions: nil record")
	}
	var hitlPtr *string
	if rec.HITLResponseID != nil && *rec.HITLResponseID != "" {
		hitlPtr = rec.HITLResponseID
	}
	decidedAt := rec.DecidedAt
	if decidedAt.IsZero() {
		decidedAt = time.Now().UTC()
	}
	const q = `
		INSERT INTO host_gate_decisions (
			id, session_id, op_kind, command_hash, path, sandbox,
			decision, reason, risk_band, decided_at, hitl_response_id
		) VALUES (
			COALESCE(NULLIF($1, '')::uuid, gen_random_uuid()),
			$2, $3, $4, NULLIF($5, ''), NULLIF($6, ''),
			$7, $8, $9, $10, $11
		)
	`
	_, err := s.pool.Exec(ctx, q,
		rec.ID,
		rec.SessionID,
		rec.OpKind,
		rec.CommandHash,
		rec.Path,
		rec.Sandbox,
		rec.Decision,
		rec.Reason,
		rec.RiskBand,
		decidedAt,
		hitlPtr,
	)
	if err != nil {
		return fmt.Errorf("gate_decisions: insert: %w", err)
	}
	return nil
}

// List returns up to limit rows ordered by decided_at DESC.
func (s *GateDecisionsStore) List(ctx context.Context, limit int) ([]*persist.GateDecisionRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	const q = `
		SELECT id, session_id, op_kind, command_hash,
		       COALESCE(path, ''), COALESCE(sandbox, ''),
		       decision, COALESCE(reason, ''), COALESCE(risk_band, ''),
		       decided_at, hitl_response_id
		FROM host_gate_decisions
		ORDER BY decided_at DESC
		LIMIT $1
	`
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("gate_decisions: list: %w", err)
	}
	defer rows.Close()
	var out []*persist.GateDecisionRecord
	for rows.Next() {
		rec := &persist.GateDecisionRecord{}
		var hitl *string
		if err := rows.Scan(
			&rec.ID, &rec.SessionID, &rec.OpKind, &rec.CommandHash,
			&rec.Path, &rec.Sandbox,
			&rec.Decision, &rec.Reason, &rec.RiskBand,
			&rec.DecidedAt, &hitl,
		); err != nil {
			return nil, err
		}
		rec.HITLResponseID = hitl
		out = append(out, rec)
	}
	return out, rows.Err()
}
