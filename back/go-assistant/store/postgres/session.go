package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// SessionRepository implements persist.SessionRepository using PostgreSQL.
// It receives pre-populated SessionRecord DTOs — the integration layer
// is responsible for combining cpn.Session + sessionState into a SessionRecord.
type SessionRepository struct {
	pool *pgxpool.Pool
}

// Compile-time interface assertion.
var _ persist.SessionRepository = (*SessionRepository)(nil)

// NewSessionRepository creates a new Postgres-backed session repository.
func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository {
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
		`SELECT id, session_id, role, content, cpn_id, cpn_role, cpn_depth, timestamp
		 FROM messages WHERE session_id = $1 ORDER BY timestamp`, sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres session get messages: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		m := &persist.MessageRecord{}
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.CPNID, &m.CPNRole, &m.CPNDepth, &m.Timestamp); err != nil {
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
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO messages (id, session_id, role, content, cpn_id, cpn_role, cpn_depth, timestamp)
		 SELECT $1, $2, $3, $4, $5, $6, $7, $8
		 FROM sessions WHERE id = $2 AND state NOT IN ('closed', 'expired')`,
		msg.ID, sessionID, msg.Role, msg.Content, msg.CPNID, msg.CPNRole, msg.CPNDepth, msg.Timestamp,
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
