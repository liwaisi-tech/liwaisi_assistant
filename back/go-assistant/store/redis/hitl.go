package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// HITLRepository implements persist.HITLRepository using Redis.
// Keys: hitl:{sessionID}:{transitionID} with TTL from ExpiresAt.
type HITLRepository struct {
	client *redis.Client
	logger *slog.Logger
}

// Compile-time interface assertion.
var _ persist.HITLRepository = (*HITLRepository)(nil)

// NewHITLRepository creates a Redis-backed HITL repository.
func NewHITLRepository(client *redis.Client) *HITLRepository {
	return &HITLRepository{
		client: client,
		logger: slog.Default(),
	}
}

func hitlKey(sessionID, transitionID string) string {
	return "hitl:" + sessionID + ":" + transitionID
}

func (r *HITLRepository) Enqueue(ctx context.Context, req *persist.HITLPendingRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("redis hitl enqueue marshal: %w", err)
	}

	ttl := time.Until(req.ExpiresAt)
	if ttl <= 0 {
		return persist.ErrHITLExpired
	}

	key := hitlKey(req.SessionID, req.TransitionID)
	if err := r.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("redis hitl enqueue: %w", err)
	}
	return nil
}

func (r *HITLRepository) Dequeue(ctx context.Context, sessionID, transitionID string) (*persist.HITLPendingRequest, error) {
	key := hitlKey(sessionID, transitionID)

	data, err := r.client.GetDel(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, persist.ErrHITLNotFound
		}
		return nil, fmt.Errorf("redis hitl dequeue: %w", err)
	}

	var req persist.HITLPendingRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("redis hitl dequeue unmarshal: %w", err)
	}

	if time.Now().After(req.ExpiresAt) {
		return nil, persist.ErrHITLExpired
	}

	return &req, nil
}

func (r *HITLRepository) ListPending(ctx context.Context, sessionID string) ([]*persist.HITLPendingRequest, error) {
	pattern := "hitl:" + sessionID + ":*"
	var result []*persist.HITLPendingRequest

	iter := r.client.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		data, err := r.client.Get(ctx, iter.Val()).Bytes()
		if err != nil {
			continue
		}
		var req persist.HITLPendingRequest
		if json.Unmarshal(data, &req) == nil {
			result = append(result, &req)
		}
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("redis hitl list pending: %w", err)
	}
	return result, nil
}

func (r *HITLRepository) Expire(ctx context.Context, _ time.Duration) (int64, error) {
	// Redis handles TTL-based expiry natively.
	// This method scans for any hitl:* keys that might have expired
	// (defensive cleanup for metrics). Redis auto-expires keys with TTL.
	var expired int64
	iter := r.client.Scan(ctx, 0, "hitl:*", 100).Iterator()
	for iter.Next(ctx) {
		ttl, err := r.client.TTL(ctx, iter.Val()).Result()
		if err != nil {
			continue
		}
		// TTL == -2 means key doesn't exist (already expired)
		// TTL == -1 means no expiry set (shouldn't happen for HITL)
		if ttl == -2*time.Second || ttl == -1*time.Second {
			r.client.Del(ctx, iter.Val())
			expired++
		}
	}
	return expired, iter.Err()
}
