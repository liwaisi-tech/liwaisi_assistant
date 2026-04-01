package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// LedgerRepository implements persist.LedgerRepository using PostgreSQL.
type LedgerRepository struct {
	pool *pgxpool.Pool
}

// Compile-time interface assertion.
var _ persist.LedgerRepository = (*LedgerRepository)(nil)

// NewLedgerRepository creates a new Postgres-backed ledger repository.
func NewLedgerRepository(pool *pgxpool.Pool) *LedgerRepository {
	return &LedgerRepository{pool: pool}
}

// Record upserts a ledger entry: creates if new, increments if existing.
func (r *LedgerRepository) Record(ctx context.Context, rec *persist.LedgerRecord) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO token_ledger (session_id, input_tokens, output_tokens, calls, total_cost_usd, last_updated)
		 VALUES ($1, $2, $3, $4, $5, NOW())
		 ON CONFLICT (session_id) DO UPDATE SET
		   input_tokens = token_ledger.input_tokens + EXCLUDED.input_tokens,
		   output_tokens = token_ledger.output_tokens + EXCLUDED.output_tokens,
		   calls = token_ledger.calls + EXCLUDED.calls,
		   total_cost_usd = token_ledger.total_cost_usd + EXCLUDED.total_cost_usd,
		   last_updated = NOW()`,
		rec.SessionID, rec.InputTokens, rec.OutputTokens, rec.Calls, rec.TotalCostUSD,
	)
	if err != nil {
		return fmt.Errorf("postgres ledger record: %w", err)
	}
	return nil
}

// GetBySession retrieves the ledger for a session.
func (r *LedgerRepository) GetBySession(ctx context.Context, sessionID string) (*persist.LedgerRecord, error) {
	l := &persist.LedgerRecord{}
	err := r.pool.QueryRow(ctx,
		`SELECT session_id, input_tokens, output_tokens, calls, total_cost_usd, daily_total_usd, last_updated
		 FROM token_ledger WHERE session_id = $1`, sessionID,
	).Scan(&l.SessionID, &l.InputTokens, &l.OutputTokens, &l.Calls, &l.TotalCostUSD, &l.DailyTotalUSD, &l.LastUpdated)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, persist.ErrLedgerNotFound
		}
		return nil, fmt.Errorf("postgres ledger get %s: %w", sessionID, err)
	}
	return l, nil
}

// QueryByDate returns all ledger records updated on the given date.
func (r *LedgerRepository) QueryByDate(ctx context.Context, date time.Time) ([]*persist.LedgerRecord, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT session_id, input_tokens, output_tokens, calls, total_cost_usd, daily_total_usd, last_updated
		 FROM token_ledger WHERE last_updated::date = $1::date`, date,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres ledger queryByDate: %w", err)
	}
	defer rows.Close()

	var result []*persist.LedgerRecord
	for rows.Next() {
		l := &persist.LedgerRecord{}
		if err := rows.Scan(&l.SessionID, &l.InputTokens, &l.OutputTokens, &l.Calls, &l.TotalCostUSD, &l.DailyTotalUSD, &l.LastUpdated); err != nil {
			return nil, fmt.Errorf("postgres ledger scan: %w", err)
		}
		result = append(result, l)
	}
	return result, rows.Err()
}

// SetDailyTotal sets the daily total cost for a session.
func (r *LedgerRepository) SetDailyTotal(ctx context.Context, sessionID string, _ time.Time, totalUSD float64) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE token_ledger SET daily_total_usd = $2 WHERE session_id = $1",
		sessionID, totalUSD,
	)
	if err != nil {
		return fmt.Errorf("postgres ledger setDailyTotal: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return persist.ErrLedgerNotFound
	}
	return nil
}

// AggregateByDateRange returns aggregated metrics over a date range.
func (r *LedgerRepository) AggregateByDateRange(ctx context.Context, from, to time.Time) (*persist.LedgerAggregate, error) {
	agg := &persist.LedgerAggregate{}
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(input_tokens),0), COALESCE(SUM(output_tokens),0),
		        COALESCE(SUM(calls),0), COALESCE(SUM(total_cost_usd),0)
		 FROM token_ledger WHERE last_updated >= $1 AND last_updated <= $2`,
		from, to,
	).Scan(&agg.TotalInputTokens, &agg.TotalOutputTokens, &agg.TotalCalls, &agg.TotalCostUSD)
	if err != nil {
		return nil, fmt.Errorf("postgres ledger aggregate: %w", err)
	}
	return agg, nil
}
