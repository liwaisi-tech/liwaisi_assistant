package postgres

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// IntelligenceRepository implements persist.IntelligenceRepository using PostgreSQL.
type IntelligenceRepository struct {
	pool *pgxpool.Pool
}

// Compile-time interface assertion.
var _ persist.IntelligenceRepository = (*IntelligenceRepository)(nil)

// NewIntelligenceRepository creates a new Postgres-backed intelligence repository.
func NewIntelligenceRepository(pool *pgxpool.Pool) *IntelligenceRepository {
	return &IntelligenceRepository{pool: pool}
}

// RecordExecution persists an execution record.
func (r *IntelligenceRepository) RecordExecution(ctx context.Context, rec *persist.ExecutionRecord) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO execution_records (id, cpn_id, cpn_role, cpn_depth, session_id,
		  transitions_fired, llm_calls, tool_calls, tokens_produced,
		  total_cost_usd, duration_ms, success, started_at, completed_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		rec.ID, rec.CPNID, rec.CPNRole, rec.CPNDepth, rec.SessionID,
		rec.TransitionsFired, rec.LLMCalls, rec.ToolCalls, rec.TokensProduced,
		rec.TotalCostUSD, rec.DurationMs, rec.Success, rec.StartedAt, rec.CompletedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres intelligence record: %w", err)
	}
	return nil
}

// QueryByRole returns executions for a role within a time range.
func (r *IntelligenceRepository) QueryByRole(ctx context.Context, role string, from, to time.Time) ([]*persist.ExecutionRecord, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, cpn_id, cpn_role, cpn_depth, session_id,
		  transitions_fired, llm_calls, tool_calls, tokens_produced,
		  total_cost_usd, duration_ms, success, started_at, completed_at
		 FROM execution_records WHERE cpn_role = $1 AND started_at >= $2 AND started_at <= $3
		 ORDER BY started_at`, role, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres intelligence queryByRole: %w", err)
	}
	defer rows.Close()

	var result []*persist.ExecutionRecord
	for rows.Next() {
		rec := &persist.ExecutionRecord{}
		if err := rows.Scan(
			&rec.ID, &rec.CPNID, &rec.CPNRole, &rec.CPNDepth, &rec.SessionID,
			&rec.TransitionsFired, &rec.LLMCalls, &rec.ToolCalls, &rec.TokensProduced,
			&rec.TotalCostUSD, &rec.DurationMs, &rec.Success, &rec.StartedAt, &rec.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("postgres intelligence scan: %w", err)
		}
		result = append(result, rec)
	}
	return result, rows.Err()
}

// Aggregate computes ranking metrics for a role within a time range.
func (r *IntelligenceRepository) Aggregate(ctx context.Context, role string, from, to time.Time) (*persist.RankingMetrics, error) {
	m := &persist.RankingMetrics{Role: role}
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*),
		  COALESCE(AVG(llm_calls),0), COALESCE(AVG(tool_calls),0),
		  COALESCE(AVG(total_cost_usd),0), COALESCE(AVG(duration_ms),0),
		  COALESCE(AVG(CASE WHEN success THEN 1.0 ELSE 0.0 END),0)
		 FROM execution_records WHERE cpn_role = $1 AND started_at >= $2 AND started_at <= $3`,
		role, from, to,
	).Scan(&m.ExecutionCount, &m.AvgLLMCalls, &m.AvgToolCalls, &m.AvgCostUSD, &m.AvgDurationMs, &m.SuccessRate)
	if err != nil {
		return nil, fmt.Errorf("postgres intelligence aggregate: %w", err)
	}
	return m, nil
}

// TopFlows returns the top n flows by ranking score.
func (r *IntelligenceRepository) TopFlows(ctx context.Context, n int) ([]*persist.RankedFlow, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT cpn_id, cpn_role,
		  COUNT(*) as exec_count,
		  AVG(CASE WHEN success THEN 1.0 ELSE 0.0 END) as success_rate,
		  AVG(total_cost_usd) as avg_cost,
		  AVG(duration_ms) as avg_dur
		 FROM execution_records
		 GROUP BY cpn_id, cpn_role
		 ORDER BY (AVG(CASE WHEN success THEN 1.0 ELSE 0.0 END) * 100 - AVG(total_cost_usd) * 10) DESC
		 LIMIT $1`, n,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres intelligence topFlows: %w", err)
	}
	defer rows.Close()

	var result []*persist.RankedFlow
	for rows.Next() {
		rf := &persist.RankedFlow{}
		if err := rows.Scan(
			&rf.Hash, &rf.Role,
			&rf.Stats.ExecutionCount, &rf.Stats.SuccessRate,
			&rf.Stats.AvgCostUSD, &rf.Stats.AvgDurationMs,
		); err != nil {
			return nil, fmt.Errorf("postgres intelligence scan topFlow: %w", err)
		}
		rf.Score = rf.Stats.SuccessRate*100 - rf.Stats.AvgCostUSD*10
		result = append(result, rf)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})

	return result, nil
}
