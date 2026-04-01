package postgres

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// EventRepository implements persist.EventRepository using pgxpool.
// Append-only: no Update or Delete methods (Axiom A9).
type EventRepository struct {
	pool pgxPool
}

// Compile-time interface assertion.
var _ persist.EventRepository = (*EventRepository)(nil)

// NewEventRepository creates a new Postgres-backed event repository.
func NewEventRepository(pool pgxPool) *EventRepository {
	return &EventRepository{pool: pool}
}

// Append persists one or more events via batch insert.
func (r *EventRepository) Append(ctx context.Context, events ...*persist.EventRecord) error {
	if len(events) == 0 {
		return nil
	}
	for _, e := range events {
		if e == nil {
			return persist.ErrEventAppendFailed
		}
	}

	rows := make([][]any, 0, len(events))
	for _, e := range events {
		rows = append(rows, []any{
			e.ID, e.Type, e.SessionID, e.CPNID, e.CPNRole, e.CPNDepth,
			e.TransitionID, e.TransitionKind, e.TokenSnapshot, e.Payload, e.Timestamp,
		})
	}

	_, err := r.pool.CopyFrom(ctx,
		pgx.Identifier{"events"},
		[]string{"id", "type", "session_id", "cpn_id", "cpn_role", "cpn_depth",
			"transition_id", "transition_kind", "token_snapshot", "payload", "timestamp"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return fmt.Errorf("%w: %v", persist.ErrEventAppendFailed, err)
	}
	return nil
}

// QueryBySession returns events for a session with cursor pagination.
func (r *EventRepository) QueryBySession(ctx context.Context, sessionID string, opts *persist.EventQueryOpts) (*persist.Page[*persist.EventRecord], error) {
	limit, cursor := parseOpts(opts)

	var rows pgx.Rows
	var err error
	if cursor == "" {
		rows, err = r.pool.Query(ctx,
			`SELECT id, type, session_id, cpn_id, cpn_role, cpn_depth,
				transition_id, transition_kind, token_snapshot, payload, timestamp
			 FROM events WHERE session_id = $1
			 ORDER BY timestamp, id LIMIT $2`,
			sessionID, limit+1)
	} else {
		ts, eid := parseCursor(cursor)
		rows, err = r.pool.Query(ctx,
			`SELECT id, type, session_id, cpn_id, cpn_role, cpn_depth,
				transition_id, transition_kind, token_snapshot, payload, timestamp
			 FROM events WHERE session_id = $1 AND (timestamp, id) > ($2, $3)
			 ORDER BY timestamp, id LIMIT $4`,
			sessionID, ts, eid, limit+1)
	}
	if err != nil {
		return nil, fmt.Errorf("query events by session: %w", err)
	}
	return scanEventPage(rows, limit)
}

// QueryByCPN returns events for a CPN instance with cursor pagination.
func (r *EventRepository) QueryByCPN(ctx context.Context, cpnID string, opts *persist.EventQueryOpts) (*persist.Page[*persist.EventRecord], error) {
	limit, cursor := parseOpts(opts)

	var rows pgx.Rows
	var err error
	if cursor == "" {
		rows, err = r.pool.Query(ctx,
			`SELECT id, type, session_id, cpn_id, cpn_role, cpn_depth,
				transition_id, transition_kind, token_snapshot, payload, timestamp
			 FROM events WHERE cpn_id = $1
			 ORDER BY timestamp, id LIMIT $2`,
			cpnID, limit+1)
	} else {
		ts, eid := parseCursor(cursor)
		rows, err = r.pool.Query(ctx,
			`SELECT id, type, session_id, cpn_id, cpn_role, cpn_depth,
				transition_id, transition_kind, token_snapshot, payload, timestamp
			 FROM events WHERE cpn_id = $1 AND (timestamp, id) > ($2, $3)
			 ORDER BY timestamp, id LIMIT $4`,
			cpnID, ts, eid, limit+1)
	}
	if err != nil {
		return nil, fmt.Errorf("query events by cpn: %w", err)
	}
	return scanEventPage(rows, limit)
}

// QueryByType returns events of a type within a time range with cursor pagination.
func (r *EventRepository) QueryByType(ctx context.Context, eventType string, from, to time.Time, opts *persist.EventQueryOpts) (*persist.Page[*persist.EventRecord], error) {
	limit, cursor := parseOpts(opts)

	var rows pgx.Rows
	var err error
	if cursor == "" {
		rows, err = r.pool.Query(ctx,
			`SELECT id, type, session_id, cpn_id, cpn_role, cpn_depth,
				transition_id, transition_kind, token_snapshot, payload, timestamp
			 FROM events WHERE type = $1 AND timestamp >= $2 AND timestamp <= $3
			 ORDER BY timestamp, id LIMIT $4`,
			eventType, from, to, limit+1)
	} else {
		ts, eid := parseCursor(cursor)
		rows, err = r.pool.Query(ctx,
			`SELECT id, type, session_id, cpn_id, cpn_role, cpn_depth,
				transition_id, transition_kind, token_snapshot, payload, timestamp
			 FROM events WHERE type = $1 AND timestamp >= $2 AND timestamp <= $3
			   AND (timestamp, id) > ($4, $5)
			 ORDER BY timestamp, id LIMIT $6`,
			eventType, from, to, ts, eid, limit+1)
	}
	if err != nil {
		return nil, fmt.Errorf("query events by type: %w", err)
	}
	return scanEventPage(rows, limit)
}

// Count returns the number of events matching the query options.
func (r *EventRepository) Count(ctx context.Context, opts *persist.EventQueryOpts) (int64, error) {
	query := "SELECT count(*) FROM events WHERE 1=1"
	args := []any{}
	argN := 1

	if opts != nil {
		if opts.SessionID != "" {
			query += fmt.Sprintf(" AND session_id = $%d", argN)
			args = append(args, opts.SessionID)
			argN++
		}
		if opts.CPNID != "" {
			query += fmt.Sprintf(" AND cpn_id = $%d", argN)
			args = append(args, opts.CPNID)
			argN++
		}
		if opts.EventType != "" {
			query += fmt.Sprintf(" AND type = $%d", argN)
			args = append(args, opts.EventType)
			argN++
		}
		if !opts.From.IsZero() {
			query += fmt.Sprintf(" AND timestamp >= $%d", argN)
			args = append(args, opts.From)
			argN++
		}
		if !opts.To.IsZero() {
			query += fmt.Sprintf(" AND timestamp <= $%d", argN)
			args = append(args, opts.To)
		}
	}

	var count int64
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count events: %w", err)
	}
	return count, nil
}

func parseOpts(opts *persist.EventQueryOpts) (limit int, cursor string) {
	limit = 100
	if opts != nil {
		if opts.Limit > 0 {
			limit = opts.Limit
		}
		cursor = opts.Cursor
	}
	return limit, cursor
}

func parseCursor(cursor string) (ts time.Time, eid string) {
	// Cursor format: "timestamp_unix_nano|event_id"
	for i := range cursor {
		if cursor[i] == '|' {
			nanos, err := strconv.ParseInt(cursor[:i], 10, 64)
			if err == nil {
				ts = time.Unix(0, nanos)
			}
			eid = cursor[i+1:]
			return ts, eid
		}
	}
	return ts, cursor
}

func buildCursor(e *persist.EventRecord) string {
	return strconv.FormatInt(e.Timestamp.UnixNano(), 10) + "|" + e.ID
}

func scanEventPage(rows pgx.Rows, limit int) (*persist.Page[*persist.EventRecord], error) {
	defer rows.Close()

	var items []*persist.EventRecord
	for rows.Next() {
		e := &persist.EventRecord{}
		if err := rows.Scan(
			&e.ID, &e.Type, &e.SessionID, &e.CPNID, &e.CPNRole, &e.CPNDepth,
			&e.TransitionID, &e.TransitionKind, &e.TokenSnapshot, &e.Payload, &e.Timestamp,
		); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	page := &persist.Page[*persist.EventRecord]{}
	if len(items) > limit {
		page.Items = items[:limit]
		page.HasMore = true
		page.NextCursor = buildCursor(items[limit-1])
	} else {
		page.Items = items
	}
	return page, nil
}
