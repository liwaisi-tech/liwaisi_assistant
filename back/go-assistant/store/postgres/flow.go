package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// FlowRepository implements persist.FlowRepository using PostgreSQL.
type FlowRepository struct {
	pool *pgxpool.Pool
}

// Compile-time interface assertion.
var _ persist.FlowRepository = (*FlowRepository)(nil)

// NewFlowRepository creates a new Postgres-backed flow repository.
func NewFlowRepository(pool *pgxpool.Pool) *FlowRepository {
	return &FlowRepository{pool: pool}
}

// Save persists a flow record (upsert by hash).
func (r *FlowRepository) Save(ctx context.Context, flow *persist.FlowRecord) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO flows (hash, role, topology_json, function_mapping,
		  execution_count, success_rate, avg_cost_usd, avg_duration_ms, created_at, updated_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		 ON CONFLICT (hash) DO UPDATE SET
		   topology_json = EXCLUDED.topology_json,
		   function_mapping = EXCLUDED.function_mapping,
		   updated_at = EXCLUDED.updated_at`,
		flow.Hash, flow.Role, flow.TopologyJSON, flow.FunctionMapping,
		flow.Stats.ExecutionCount, flow.Stats.SuccessRate, flow.Stats.AvgCostUSD,
		flow.Stats.AvgDurationMs, flow.CreatedAt, flow.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres flow save: %w", err)
	}
	return nil
}

// GetByHash retrieves a flow by hash. Returns ErrFlowNotFound if absent or soft-deleted.
func (r *FlowRepository) GetByHash(ctx context.Context, hash string) (*persist.FlowRecord, error) {
	f := &persist.FlowRecord{}
	err := r.pool.QueryRow(ctx,
		`SELECT hash, role, topology_json, function_mapping,
		  execution_count, success_rate, avg_cost_usd, avg_duration_ms,
		  created_at, updated_at, deleted_at
		 FROM flows WHERE hash = $1 AND deleted_at IS NULL`, hash,
	).Scan(&f.Hash, &f.Role, &f.TopologyJSON, &f.FunctionMapping,
		&f.Stats.ExecutionCount, &f.Stats.SuccessRate, &f.Stats.AvgCostUSD,
		&f.Stats.AvgDurationMs, &f.CreatedAt, &f.UpdatedAt, &f.DeletedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, persist.ErrFlowNotFound
		}
		return nil, fmt.Errorf("postgres flow get %s: %w", hash, err)
	}
	return f, nil
}

// List returns flows with optional role filter and cursor pagination.
func (r *FlowRepository) List(ctx context.Context, opts *persist.FlowListOpts) (*persist.Page[*persist.FlowRecord], error) {
	limit := 100
	offset := 0
	role := ""

	if opts != nil {
		if opts.Limit > 0 {
			limit = opts.Limit
		}
		if opts.Cursor != "" {
			if o, err := strconv.Atoi(opts.Cursor); err == nil {
				offset = o
			}
		}
		role = opts.Role
	}

	var rows pgx.Rows
	var err error
	if role != "" {
		rows, err = r.pool.Query(ctx,
			`SELECT hash, role, topology_json, function_mapping,
			  execution_count, success_rate, avg_cost_usd, avg_duration_ms,
			  created_at, updated_at, deleted_at
			 FROM flows WHERE deleted_at IS NULL AND role = $1
			 ORDER BY created_at LIMIT $2 OFFSET $3`,
			role, limit+1, offset)
	} else {
		rows, err = r.pool.Query(ctx,
			`SELECT hash, role, topology_json, function_mapping,
			  execution_count, success_rate, avg_cost_usd, avg_duration_ms,
			  created_at, updated_at, deleted_at
			 FROM flows WHERE deleted_at IS NULL
			 ORDER BY created_at LIMIT $1 OFFSET $2`,
			limit+1, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("postgres flow list: %w", err)
	}
	defer rows.Close()

	var items []*persist.FlowRecord
	for rows.Next() {
		f := &persist.FlowRecord{}
		if err := rows.Scan(&f.Hash, &f.Role, &f.TopologyJSON, &f.FunctionMapping,
			&f.Stats.ExecutionCount, &f.Stats.SuccessRate, &f.Stats.AvgCostUSD,
			&f.Stats.AvgDurationMs, &f.CreatedAt, &f.UpdatedAt, &f.DeletedAt); err != nil {
			return nil, fmt.Errorf("postgres flow scan: %w", err)
		}
		items = append(items, f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	page := &persist.Page[*persist.FlowRecord]{}
	if len(items) > limit {
		page.Items = items[:limit]
		page.HasMore = true
		page.NextCursor = strconv.Itoa(offset + limit)
	} else {
		page.Items = items
	}
	return page, nil
}

// UpdateStats updates the execution statistics for a flow.
func (r *FlowRepository) UpdateStats(ctx context.Context, hash string, stats *persist.FlowStats) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE flows SET execution_count = $2, success_rate = $3, avg_cost_usd = $4,
		  avg_duration_ms = $5, updated_at = NOW()
		 WHERE hash = $1 AND deleted_at IS NULL`,
		hash, stats.ExecutionCount, stats.SuccessRate, stats.AvgCostUSD, stats.AvgDurationMs,
	)
	if err != nil {
		return fmt.Errorf("postgres flow updateStats: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return persist.ErrFlowNotFound
	}
	return nil
}

// Delete soft-deletes a flow by setting deleted_at.
func (r *FlowRepository) Delete(ctx context.Context, hash string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE flows SET deleted_at = $2 WHERE hash = $1 AND deleted_at IS NULL",
		hash, time.Now(),
	)
	if err != nil {
		return fmt.Errorf("postgres flow delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return persist.ErrFlowNotFound
	}
	return nil
}
