package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// SessionRepository implements persist.SessionRepository using PostgreSQL.
// It receives pre-populated SessionRecord DTOs — the integration layer
// is responsible for combining cpn.Session + sessionState into a SessionRecord.
type SessionRepository struct {
	pool pgxPool
}

// Compile-time interface assertion.
var _ persist.SessionRepository = (*SessionRepository)(nil)

// NewSessionRepository creates a new Postgres-backed session repository.
func NewSessionRepository(pool pgxPool) *SessionRepository {
	return &SessionRepository{pool: pool}
}

func (r *SessionRepository) Create(ctx context.Context, session *persist.SessionRecord) error {
	if session.ID == "" {
		return persist.ErrInvalidInput
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO sessions (id, user_id, channel, state, created_at, last_activity_at, closed_at, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		session.ID, session.UserID, session.Channel, string(session.State),
		session.CreatedAt, session.LastActivityAt, session.ClosedAt, session.Metadata,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return persist.ErrSessionExists
		}
		return fmt.Errorf("postgres session create: %w", err)
	}
	return nil
}

func (r *SessionRepository) Get(ctx context.Context, sessionID string) (*persist.SessionRecord, error) {
	s := &persist.SessionRecord{}
	var state string
	err := r.pool.QueryRow(ctx,
		`SELECT id, user_id, channel, state, created_at, last_activity_at, closed_at, metadata
		 FROM sessions WHERE id = $1`, sessionID,
	).Scan(&s.ID, &s.UserID, &s.Channel, &state, &s.CreatedAt, &s.LastActivityAt, &s.ClosedAt, &s.Metadata)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, persist.ErrSessionNotFound
		}
		return nil, fmt.Errorf("postgres session get %s: %w", sessionID, err)
	}
	s.State = persist.SessionState(state)

	// Load messages
	rows, err := r.pool.Query(ctx,
		`SELECT id, session_id, role, content, cpn_id, cpn_role, cpn_depth, timestamp, COALESCE(parent_message_id, '')
		 FROM messages WHERE session_id = $1 ORDER BY timestamp`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres session get messages: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		m := &persist.MessageRecord{}
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.CPNID, &m.CPNRole, &m.CPNDepth, &m.Timestamp, &m.ParentMessageID); err != nil {
			return nil, fmt.Errorf("postgres session scan message: %w", err)
		}
		s.Messages = append(s.Messages, m)
	}
	return s, rows.Err()
}

func (r *SessionRepository) GetByUserID(ctx context.Context, userID string) ([]*persist.SessionRecord, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, channel, state, created_at, last_activity_at, closed_at, metadata
		 FROM sessions WHERE user_id = $1 ORDER BY created_at DESC`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres session getByUserID: %w", err)
	}
	defer rows.Close()

	var result []*persist.SessionRecord
	for rows.Next() {
		s := &persist.SessionRecord{}
		var state string
		if err := rows.Scan(&s.ID, &s.UserID, &s.Channel, &state, &s.CreatedAt, &s.LastActivityAt, &s.ClosedAt, &s.Metadata); err != nil {
			return nil, fmt.Errorf("postgres session scan: %w", err)
		}
		s.State = persist.SessionState(state)
		result = append(result, s)
	}
	if result == nil {
		result = []*persist.SessionRecord{}
	}
	return result, rows.Err()
}

func (r *SessionRepository) AppendMessage(ctx context.Context, sessionID string, msg *persist.MessageRecord) error {
	// Atomic check-and-insert: verify session is active in the same statement
	// to avoid TOCTOU race between separate SELECT and INSERT.
	// parent_message_id is NULL when ParentMessageID is empty so legacy rows
	// remain distinguishable from explicitly-linked rows.
	var parentID any
	if msg.ParentMessageID != "" {
		parentID = msg.ParentMessageID
	}
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO messages (id, session_id, role, content, cpn_id, cpn_role, cpn_depth, timestamp, parent_message_id)
		 SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9
		 FROM sessions WHERE id = $2 AND state NOT IN ('closed', 'expired')`,
		msg.ID, sessionID, msg.Role, msg.Content, msg.CPNID, msg.CPNRole, msg.CPNDepth, msg.Timestamp, parentID,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return persist.ErrSessionNotFound
		}
		return fmt.Errorf("postgres session append message: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Distinguish between not found and closed
		var state string
		checkErr := r.pool.QueryRow(ctx, "SELECT state FROM sessions WHERE id = $1", sessionID).Scan(&state)
		if checkErr != nil {
			return persist.ErrSessionNotFound
		}
		return persist.ErrSessionClosed
	}
	return nil
}

func (r *SessionRepository) UpdateState(ctx context.Context, sessionID string, state persist.SessionState) error {
	var closedAt *time.Time
	if state == persist.SessionClosed {
		now := time.Now()
		closedAt = &now
	}

	tag, err := r.pool.Exec(ctx,
		"UPDATE sessions SET state = $2, closed_at = COALESCE($3, closed_at) WHERE id = $1",
		sessionID, string(state), closedAt,
	)
	if err != nil {
		return fmt.Errorf("postgres session updateState: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return persist.ErrSessionNotFound
	}
	return nil
}

func (r *SessionRepository) Touch(ctx context.Context, sessionID string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE sessions SET last_activity_at = NOW() WHERE id = $1", sessionID,
	)
	if err != nil {
		return fmt.Errorf("postgres session touch: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return persist.ErrSessionNotFound
	}
	return nil
}

func (r *SessionRepository) Close(ctx context.Context, sessionID string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE sessions SET state = 'closed', closed_at = NOW() WHERE id = $1 AND state != 'closed'",
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("postgres session close: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Check if it exists at all
		var exists bool
		_ = r.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM sessions WHERE id = $1)", sessionID).Scan(&exists)
		if !exists {
			return persist.ErrSessionNotFound
		}
		return persist.ErrSessionClosed
	}
	return nil
}

func (r *SessionRepository) ListExpired(ctx context.Context, before time.Time) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		"SELECT id FROM sessions WHERE state = 'active' AND last_activity_at < $1", before,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres session listExpired: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("postgres session scan expired: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *SessionRepository) Delete(ctx context.Context, sessionID string) error {
	tag, err := r.pool.Exec(ctx, "DELETE FROM sessions WHERE id = $1", sessionID)
	if err != nil {
		return fmt.Errorf("postgres session delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return persist.ErrSessionNotFound
	}
	return nil
}

func (r *SessionRepository) ListByUserID(ctx context.Context, userID string, opts *persist.SessionListOpts) (*persist.Page[*persist.SessionListItem], error) {
	limit := 50
	if opts != nil && opts.Limit > 0 {
		limit = opts.Limit
	}
	if limit > 100 {
		limit = 100
	}

	// Fetch the last 10 message contents (newest-first) per session so the
	// shared persist.RenderablePreview helper can skip raw-routing-JSON or
	// unparseable A2UI rows and pick the first user-renderable summary
	// (REQ-202, INV-302, spec-process-bugfix-a2ui-rehydration-completion.md).
	const previewLookback = 10
	query := `
		SELECT s.id, COALESCE(s.title, ''), s.state, s.last_activity_at, s.created_at,
		       COALESCE(s.forked_from_session_id, ''),
		       COALESCE(tl.total_cost_usd, 0),
		       (SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id),
		       COALESCE(
		         (SELECT array_agg(content ORDER BY ts DESC)
		            FROM (SELECT content, timestamp AS ts
		                    FROM messages m2
		                   WHERE m2.session_id = s.id
		                ORDER BY m2.timestamp DESC
		                   LIMIT $3) recent),
		         ARRAY[]::text[]
		       )
		FROM sessions s
		LEFT JOIN token_ledger tl ON tl.session_id = s.id
		WHERE s.user_id = $1 AND s.deleted_at IS NULL AND s.state != 'expired'
		ORDER BY s.last_activity_at DESC
		LIMIT $2`

	rows, err := r.pool.Query(ctx, query, userID, limit+1, previewLookback)
	if err != nil {
		return nil, fmt.Errorf("postgres session listByUserID: %w", err)
	}
	defer rows.Close()

	var items []*persist.SessionListItem
	for rows.Next() {
		item := &persist.SessionListItem{}
		var state string
		var recent []string
		if err := rows.Scan(
			&item.ID, &item.Title, &state, &item.LastActivityAt, &item.CreatedAt,
			&item.ForkedFromSessionID, &item.TotalCostUSD,
			&item.MessageCount, &recent,
		); err != nil {
			return nil, fmt.Errorf("postgres session scan list item: %w", err)
		}
		item.State = persist.SessionState(state)
		item.LastMessagePreview = persist.RenderablePreview(recent)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres session listByUserID rows: %w", err)
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	page := &persist.Page[*persist.SessionListItem]{
		Items:   items,
		HasMore: hasMore,
	}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		page.NextCursor = fmt.Sprintf("%d|%s", last.LastActivityAt.UnixNano(), last.ID)
	}
	return page, nil
}

func (r *SessionRepository) UpdateTitle(ctx context.Context, sessionID, title string) error {
	tag, err := r.pool.Exec(ctx, "UPDATE sessions SET title = $2 WHERE id = $1", sessionID, title)
	if err != nil {
		return fmt.Errorf("postgres session updateTitle: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return persist.ErrSessionNotFound
	}
	return nil
}

func (r *SessionRepository) SoftDelete(ctx context.Context, sessionID string) error {
	tag, err := r.pool.Exec(ctx,
		"UPDATE sessions SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL",
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("postgres session softDelete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return persist.ErrSessionNotFound
	}
	return nil
}

func (r *SessionRepository) ForkSession(ctx context.Context, newSessionID, sourceSessionID string, messageIndex int, userID, channel string) (*persist.SessionRecord, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("postgres session fork begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // rollback best-effort on deferred cleanup

	// Get source session
	var srcTitle string
	var srcFlowHash *string
	err = tx.QueryRow(ctx,
		"SELECT COALESCE(title, ''), flow_hash FROM sessions WHERE id = $1",
		sourceSessionID,
	).Scan(&srcTitle, &srcFlowHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, persist.ErrSessionNotFound
		}
		return nil, fmt.Errorf("postgres session fork get source: %w", err)
	}

	// Count source messages for validation
	var msgCount int
	err = tx.QueryRow(ctx,
		"SELECT COUNT(*) FROM messages WHERE session_id = $1",
		sourceSessionID,
	).Scan(&msgCount)
	if err != nil {
		return nil, fmt.Errorf("postgres session fork count messages: %w", err)
	}
	if messageIndex < 0 || messageIndex >= msgCount {
		return nil, persist.ErrInvalidInput
	}

	// Create new session
	now := time.Now()
	forkTitle := "Fork of: " + srcTitle
	if srcTitle == "" {
		forkTitle = "Forked conversation"
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO sessions (id, user_id, channel, state, created_at, last_activity_at, title, forked_from_session_id, fork_message_count, flow_hash)
		 VALUES ($1, $2, $3, 'active', $4, $4, $5, $6, $7, $8)`,
		newSessionID, userID, channel, now, forkTitle, sourceSessionID, messageIndex+1, srcFlowHash,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres session fork create: %w", err)
	}

	// Copy messages from source up to messageIndex (inclusive), ordered by timestamp
	_, err = tx.Exec(ctx,
		`INSERT INTO messages (id, session_id, role, content, cpn_id, cpn_role, cpn_depth, timestamp)
		 SELECT
		   $2 || '-' || row_number() OVER (ORDER BY timestamp),
		   $2, role, content, cpn_id, cpn_role, cpn_depth, timestamp
		 FROM (
		   SELECT *, row_number() OVER (ORDER BY timestamp) - 1 AS rn
		   FROM messages WHERE session_id = $1
		 ) sub
		 WHERE sub.rn <= $3
		 ORDER BY sub.timestamp`,
		sourceSessionID, newSessionID, messageIndex,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres session fork copy messages: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("postgres session fork commit: %w", err)
	}

	return &persist.SessionRecord{
		ID:                  newSessionID,
		UserID:              userID,
		Channel:             channel,
		State:               persist.SessionActive,
		CreatedAt:           now,
		LastActivityAt:      now,
		Title:               forkTitle,
		ForkedFromSessionID: sourceSessionID,
		ForkMessageCount:    messageIndex + 1,
	}, nil
}
