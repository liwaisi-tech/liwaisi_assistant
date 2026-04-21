package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// AuthoredFlowRepository implements cpn.AuthoredFlowRepository on top of
// the flows table, using the columns added by migration 027. Every row
// written here carries origin='agent-authored' so the admin list can
// filter cleanly.
type AuthoredFlowRepository struct {
	pool pgxPool
}

// Compile-time assertion.
var _ cpn.AuthoredFlowRepository = (*AuthoredFlowRepository)(nil)

// NewAuthoredFlowRepository builds the repo.
func NewAuthoredFlowRepository(pool pgxPool) *AuthoredFlowRepository {
	return &AuthoredFlowRepository{pool: pool}
}

// SaveAuthored canonicalises → sha256 → upsert (idempotent).
func (r *AuthoredFlowRepository) SaveAuthored(
	ctx context.Context,
	canonicalJSON json.RawMessage,
	summary string,
	sizePlaces, sizeTransitions int,
	referenced []string,
	prov cpn.AuthoredFlowProvenance,
) (string, bool, error) {
	if len(canonicalJSON) == 0 {
		return "", false, errors.New("postgres: canonical topology JSON is empty")
	}
	sum := sha256.Sum256(canonicalJSON)
	flowID := hex.EncodeToString(sum[:])

	refs := referenced
	if refs == nil {
		refs = []string{}
	}
	refsJSON, err := json.Marshal(refs)
	if err != nil {
		return "", false, fmt.Errorf("marshal referenced primitives: %w", err)
	}

	now := time.Now().UTC()
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO flows (
			hash, role, topology_json, function_mapping,
			execution_count, success_rate, avg_cost_usd, avg_duration_ms,
			created_at, updated_at,
			authored_by_cpn_id, authored_from_prompt_digest, safe_lint_passed,
			size_places, size_transitions, rejected, rejected_reason,
			origin, summary, referenced_primitives, session_id
		) VALUES ($1,$2,$3,$4,0,0,0,0,$5,$5,$6,$7,TRUE,$8,$9,FALSE,NULL,'agent-authored',$10,$11,$12)
		ON CONFLICT (hash) DO NOTHING`,
		flowID,
		summary,
		canonicalJSON,
		[]byte("{}"),
		now,
		nullIfEmpty(prov.AuthoredByCPNID),
		nullIfEmpty(prov.AuthoredFromPromptHash),
		sizePlaces,
		sizeTransitions,
		summary,
		refsJSON,
		nullIfEmpty(prov.SessionID),
	)
	if err != nil {
		return "", false, fmt.Errorf("postgres: insert authored flow: %w", err)
	}
	return flowID, tag.RowsAffected() == 1, nil
}

// GetByID returns the authored record for a flow_id.
func (r *AuthoredFlowRepository) GetByID(ctx context.Context, flowID string) (*cpn.AuthoredFlowRecord, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT hash, topology_json, COALESCE(summary, '') AS summary,
		        COALESCE(rejected, FALSE) AS rejected,
		        COALESCE(rejected_reason, '') AS rejected_reason,
		        COALESCE(safe_lint_passed, FALSE) AS safe_lint_passed,
		        COALESCE(size_places, 0) AS size_places,
		        COALESCE(size_transitions, 0) AS size_transitions,
		        COALESCE(authored_by_cpn_id, '') AS authored_by_cpn_id,
		        COALESCE(authored_from_prompt_digest, '') AS authored_from_prompt_digest,
		        COALESCE(session_id, '') AS session_id
		 FROM flows WHERE hash = $1 AND deleted_at IS NULL`, flowID)
	var rec cpn.AuthoredFlowRecord
	var topology []byte
	if err := row.Scan(&rec.FlowID, &topology, &rec.Summary,
		&rec.Rejected, &rec.RejectedReason,
		&rec.SafeLintPassed, &rec.SizePlaces, &rec.SizeTransitions,
		&rec.Provenance.AuthoredByCPNID, &rec.Provenance.AuthoredFromPromptHash,
		&rec.Provenance.SessionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("authored flow %q not found", flowID)
		}
		return nil, fmt.Errorf("postgres: get authored flow: %w", err)
	}
	rec.TopologyJSON = json.RawMessage(topology)
	return &rec, nil
}

// Reject flips the rejected flag and stores the reason.
func (r *AuthoredFlowRepository) Reject(ctx context.Context, flowID, reason string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE flows SET rejected = TRUE, rejected_reason = $2, updated_at = NOW() WHERE hash = $1`,
		flowID, reason)
	if err != nil {
		return fmt.Errorf("postgres: reject flow: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("authored flow %q not found", flowID)
	}
	return nil
}

// ListAuthored returns agent-authored flows (most recent first).
func (r *AuthoredFlowRepository) ListAuthored(ctx context.Context, limit int) ([]*cpn.AuthoredFlowRecord, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx,
		`SELECT hash, topology_json, COALESCE(summary, '') AS summary,
		        COALESCE(rejected, FALSE) AS rejected,
		        COALESCE(rejected_reason, '') AS rejected_reason,
		        COALESCE(safe_lint_passed, FALSE) AS safe_lint_passed,
		        COALESCE(size_places, 0) AS size_places,
		        COALESCE(size_transitions, 0) AS size_transitions,
		        COALESCE(authored_by_cpn_id, '') AS authored_by_cpn_id,
		        COALESCE(authored_from_prompt_digest, '') AS authored_from_prompt_digest,
		        COALESCE(session_id, '') AS session_id
		 FROM flows WHERE origin = 'agent-authored' AND deleted_at IS NULL
		 ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("postgres: list authored flows: %w", err)
	}
	defer rows.Close()

	var out []*cpn.AuthoredFlowRecord
	for rows.Next() {
		var rec cpn.AuthoredFlowRecord
		var topology []byte
		if err := rows.Scan(&rec.FlowID, &topology, &rec.Summary,
			&rec.Rejected, &rec.RejectedReason,
			&rec.SafeLintPassed, &rec.SizePlaces, &rec.SizeTransitions,
			&rec.Provenance.AuthoredByCPNID, &rec.Provenance.AuthoredFromPromptHash,
			&rec.Provenance.SessionID); err != nil {
			return nil, fmt.Errorf("postgres: scan authored flow: %w", err)
		}
		rec.TopologyJSON = json.RawMessage(topology)
		out = append(out, &rec)
	}
	return out, rows.Err()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
