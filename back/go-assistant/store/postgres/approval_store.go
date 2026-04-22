package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/toolapproval"
)

// ApprovalRepository is the Postgres-backed implementation of
// toolapproval.ApprovalStore. Approvals are keyed on the composite
// (session_id, tool_name, provenance_sha256); drift in any component misses
// the lookup and therefore re-triggers HITL (REQ-1303). Denials are never
// persisted — absence of a row is the fail-closed "unknown" state.
type ApprovalRepository struct {
	pool pgxPool
}

var _ toolapproval.ApprovalStore = (*ApprovalRepository)(nil)

// NewApprovalRepository constructs an ApprovalRepository bound to pool.
func NewApprovalRepository(pool pgxPool) *ApprovalRepository {
	return &ApprovalRepository{pool: pool}
}

// Record upserts an Approval. Arrival with the same composite key overwrites
// approved_at so the latest decision timestamp wins.
func (r *ApprovalRepository) Record(ctx context.Context, a toolapproval.Approval) error {
	if a.SessionID == "" || a.ToolName == "" || a.ProvenanceSHA256 == "" {
		return toolapproval.ErrInvalidKey
	}
	approvedAt := a.ApprovedAt
	if approvedAt.IsZero() {
		approvedAt = time.Now().UTC()
	} else {
		approvedAt = approvedAt.UTC()
	}

	const q = `
		INSERT INTO tool_approvals
			(session_id, tool_name, provenance_sha256, approved_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (session_id, tool_name, provenance_sha256) DO UPDATE
		  SET approved_at = EXCLUDED.approved_at
	`
	if _, err := r.pool.Exec(ctx, q,
		a.SessionID, a.ToolName, a.ProvenanceSHA256, approvedAt,
	); err != nil {
		return fmt.Errorf("postgres-tool-approval: record: %w", err)
	}
	return nil
}

// Lookup returns DecisionApproved when a row exists for the composite key,
// DecisionUnknown otherwise. Denials are never persisted (SEC-003
// fail-closed).
func (r *ApprovalRepository) Lookup(ctx context.Context, sessionID, toolName, provenance string) (toolapproval.Decision, error) {
	if sessionID == "" || toolName == "" || provenance == "" {
		return toolapproval.DecisionUnknown, toolapproval.ErrInvalidKey
	}
	const q = `
		SELECT 1
		FROM tool_approvals
		WHERE session_id = $1 AND tool_name = $2 AND provenance_sha256 = $3
		LIMIT 1
	`
	var one int
	err := r.pool.QueryRow(ctx, q, sessionID, toolName, provenance).Scan(&one)
	if err != nil {
		if err == pgx.ErrNoRows {
			return toolapproval.DecisionUnknown, nil
		}
		return toolapproval.DecisionUnknown, fmt.Errorf("postgres-tool-approval: lookup: %w", err)
	}
	return toolapproval.DecisionApproved, nil
}

// ListForSession returns every Approval for sessionID, deterministically
// ordered by tool_name then provenance_sha256.
func (r *ApprovalRepository) ListForSession(ctx context.Context, sessionID string) ([]toolapproval.Approval, error) {
	if sessionID == "" {
		return nil, toolapproval.ErrInvalidKey
	}
	const q = `
		SELECT session_id, tool_name, provenance_sha256, approved_at
		FROM tool_approvals
		WHERE session_id = $1
		ORDER BY tool_name ASC, provenance_sha256 ASC
	`
	rows, err := r.pool.Query(ctx, q, sessionID)
	if err != nil {
		return nil, fmt.Errorf("postgres-tool-approval: list: %w", err)
	}
	defer rows.Close()

	out := make([]toolapproval.Approval, 0)
	for rows.Next() {
		var (
			a          toolapproval.Approval
			approvedAt time.Time
		)
		if err := rows.Scan(&a.SessionID, &a.ToolName, &a.ProvenanceSHA256, &approvedAt); err != nil {
			return nil, fmt.Errorf("postgres-tool-approval: scan: %w", err)
		}
		a.ApprovedAt = approvedAt
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres-tool-approval: rows: %w", err)
	}
	return out, nil
}
