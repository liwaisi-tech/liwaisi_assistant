package redis

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

const defaultSessionTTL = 30 * time.Minute

// SessionRepository implements persist.SessionRepository with Redis cache-aside.
// Postgres (fallback) is the source of truth; cache errors are logged, not propagated.
type SessionRepository struct {
	client   *redis.Client
	fallback persist.SessionRepository
	ttl      time.Duration
	logger   *slog.Logger
}

// Compile-time interface assertion.
var _ persist.SessionRepository = (*SessionRepository)(nil)

// NewSessionRepository creates a cache-aside session repository.
func NewSessionRepository(client *redis.Client, fallback persist.SessionRepository) *SessionRepository {
	return &SessionRepository{
		client:   client,
		fallback: fallback,
		ttl:      defaultSessionTTL,
		logger:   slog.Default(),
	}
}

func sessionKey(id string) string  { return "session:" + id }
func messagesKey(id string) string { return "session:" + id + ":messages" }

func (r *SessionRepository) Create(ctx context.Context, session *persist.SessionRecord) error {
	if err := r.fallback.Create(ctx, session); err != nil {
		return err
	}
	r.cacheSession(ctx, session)
	return nil
}

func (r *SessionRepository) Get(ctx context.Context, sessionID string) (*persist.SessionRecord, error) {
	// Try cache first
	data, err := r.client.Get(ctx, sessionKey(sessionID)).Bytes()
	if err == nil {
		var s persist.SessionRecord
		if json.Unmarshal(data, &s) == nil {
			return &s, nil
		}
	}

	// Cache miss — fetch from Postgres
	s, err := r.fallback.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	r.cacheSession(ctx, s)
	return s, nil
}

func (r *SessionRepository) GetByUserID(ctx context.Context, userID string) ([]*persist.SessionRecord, error) {
	return r.fallback.GetByUserID(ctx, userID)
}

func (r *SessionRepository) AppendMessage(ctx context.Context, sessionID string, msg *persist.MessageRecord) error {
	if err := r.fallback.AppendMessage(ctx, sessionID, msg); err != nil {
		return err
	}
	// Append to Redis message list and reset TTL
	data, err := json.Marshal(msg)
	if err == nil {
		pipe := r.client.Pipeline()
		pipe.RPush(ctx, messagesKey(sessionID), data)
		pipe.Expire(ctx, messagesKey(sessionID), r.ttl)
		pipe.Expire(ctx, sessionKey(sessionID), r.ttl)
		if _, err := pipe.Exec(ctx); err != nil {
			r.logger.Warn("redis: append message cache", "error", err)
		}
	}
	return nil
}

func (r *SessionRepository) UpdateState(ctx context.Context, sessionID string, state persist.SessionState) error {
	if err := r.fallback.UpdateState(ctx, sessionID, state); err != nil {
		return err
	}
	if state == persist.SessionClosed || state == persist.SessionExpired {
		r.evict(ctx, sessionID)
	} else {
		r.invalidate(ctx, sessionID)
	}
	return nil
}

func (r *SessionRepository) Touch(ctx context.Context, sessionID string) error {
	if err := r.fallback.Touch(ctx, sessionID); err != nil {
		return err
	}
	// Reset TTL without re-fetching
	if err := r.client.Expire(ctx, sessionKey(sessionID), r.ttl).Err(); err != nil {
		r.logger.Warn("redis: touch TTL reset", "error", err)
	}
	return nil
}

func (r *SessionRepository) Close(ctx context.Context, sessionID string) error {
	if err := r.fallback.Close(ctx, sessionID); err != nil {
		return err
	}
	r.evict(ctx, sessionID)
	return nil
}

func (r *SessionRepository) ListExpired(ctx context.Context, before time.Time) ([]string, error) {
	return r.fallback.ListExpired(ctx, before)
}

func (r *SessionRepository) Delete(ctx context.Context, sessionID string) error {
	if err := r.fallback.Delete(ctx, sessionID); err != nil {
		return err
	}
	r.evict(ctx, sessionID)
	return nil
}

func (r *SessionRepository) ListByUserID(ctx context.Context, userID string, opts *persist.SessionListOpts) (*persist.Page[*persist.SessionListItem], error) {
	return r.fallback.ListByUserID(ctx, userID, opts)
}

func (r *SessionRepository) UpdateTitle(ctx context.Context, sessionID string, title string) error {
	if err := r.fallback.UpdateTitle(ctx, sessionID, title); err != nil {
		return err
	}
	r.invalidate(ctx, sessionID)
	return nil
}

func (r *SessionRepository) SoftDelete(ctx context.Context, sessionID string) error {
	if err := r.fallback.SoftDelete(ctx, sessionID); err != nil {
		return err
	}
	r.evict(ctx, sessionID)
	return nil
}

func (r *SessionRepository) ForkSession(ctx context.Context, newSessionID string, sourceSessionID string, messageIndex int, userID string, channel string) (*persist.SessionRecord, error) {
	rec, err := r.fallback.ForkSession(ctx, newSessionID, sourceSessionID, messageIndex, userID, channel)
	if err != nil {
		return nil, err
	}
	r.cacheSession(ctx, rec)
	return rec, nil
}

func (r *SessionRepository) cacheSession(ctx context.Context, s *persist.SessionRecord) {
	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	if err := r.client.Set(ctx, sessionKey(s.ID), data, r.ttl).Err(); err != nil {
		r.logger.Warn("redis: cache session", "error", err)
	}
}

func (r *SessionRepository) evict(ctx context.Context, sessionID string) {
	pipe := r.client.Pipeline()
	pipe.Del(ctx, sessionKey(sessionID))
	pipe.Del(ctx, messagesKey(sessionID))
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		r.logger.Warn("redis: evict session", "error", err)
	}
}

func (r *SessionRepository) invalidate(ctx context.Context, sessionID string) {
	if err := r.client.Del(ctx, sessionKey(sessionID)).Err(); err != nil && !errors.Is(err, redis.Nil) {
		r.logger.Warn("redis: invalidate session", "error", err)
	}
}
