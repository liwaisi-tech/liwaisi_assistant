// Package postgres provides the Postgres connection pool, health check,
// and SQL migration runner for the Agentic CPN persistence layer.
package postgres

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolConfig holds Postgres connection pool settings.
type PoolConfig struct {
	DSN                 string        // Postgres connection string (from LIWAISI_DB_DSN)
	MaxConns            int32         // Maximum connections (default: 10)
	MinConns            int32         // Minimum idle connections (default: 2)
	MaxConnLifetime     time.Duration // Max connection lifetime (default: 30m)
	HealthCheckInterval time.Duration // Health check interval (default: 15s)
	SSLMode             string        // TLS mode (default: disable)
}

// String returns a log-safe representation with credentials redacted.
func (c PoolConfig) String() string {
	u, err := url.Parse(c.DSN)
	if err != nil {
		return "PoolConfig{DSN: <invalid>}"
	}
	if u.User != nil {
		u.User = url.UserPassword("***", "***")
	}
	return fmt.Sprintf("PoolConfig{DSN: %s, MaxConns: %d, MinConns: %d}", u.String(), c.maxConns(), c.minConns())
}

func (c PoolConfig) maxConns() int32 {
	if c.MaxConns <= 0 {
		return 10
	}
	return c.MaxConns
}

func (c PoolConfig) minConns() int32 {
	if c.MinConns <= 0 {
		return 2
	}
	return c.MinConns
}

func (c PoolConfig) maxConnLifetime() time.Duration {
	if c.MaxConnLifetime <= 0 {
		return 30 * time.Minute
	}
	return c.MaxConnLifetime
}

func (c PoolConfig) healthCheckInterval() time.Duration {
	if c.HealthCheckInterval <= 0 {
		return 15 * time.Second
	}
	return c.HealthCheckInterval
}

// NewPool creates a *pgxpool.Pool and validates the connection with SELECT 1 (fail-fast).
// Returns error immediately if the DSN is empty or Postgres is unreachable.
func NewPool(ctx context.Context, cfg PoolConfig) (*pgxpool.Pool, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("postgres: DSN is empty")
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse DSN: %w", err)
	}

	poolCfg.MaxConns = cfg.maxConns()
	poolCfg.MinConns = cfg.minConns()
	poolCfg.MaxConnLifetime = cfg.maxConnLifetime()
	poolCfg.HealthCheckPeriod = cfg.healthCheckInterval()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: create pool: %w", err)
	}

	if err := Health(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: health check: %w", err)
	}

	return pool, nil
}

// Health checks database connectivity by executing SELECT 1.
func Health(ctx context.Context, pool *pgxpool.Pool) error {
	var n int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&n); err != nil {
		return fmt.Errorf("postgres: health: %w", err)
	}
	return nil
}

// Close drains the connection pool gracefully.
func Close(pool *pgxpool.Pool) {
	pool.Close()
}
