// Package redis provides Redis-backed caching and HITL persistence.
package redis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// NewRedisPool creates a Redis client from a URL string.
// Supports redis:// and rediss:// (TLS) schemes.
func NewRedisPool(url string) (*redis.Client, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("redis: parse URL: %w", err)
	}

	client := redis.NewClient(opts)
	if err := client.Ping(context.Background()).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis: ping: %w", err)
	}

	return client, nil
}
