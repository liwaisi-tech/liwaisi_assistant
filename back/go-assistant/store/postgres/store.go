package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"golang.org/x/sync/errgroup"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	storeredis "github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/store/redis"
)

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	URL string // redis:// or rediss:// URL
}

// Store is the composition root for all persistence backends.
// It creates all backends, runs migrations, and exposes repository accessors.
type Store struct {
	pool    *pgxpool.Pool
	rdb     *goredis.Client
	batcher *EventBatcher

	sessions     persist.SessionRepository
	events       persist.EventRepository
	ledger       persist.LedgerRepository
	flows        persist.FlowRepository
	intelligence persist.IntelligenceRepository
	hitl         persist.HITLRepository
}

// NewStore creates all persistence backends, runs migrations, and returns the facade.
// Returns descriptive error if any backend is unreachable.
func NewStore(ctx context.Context, pgCfg PoolConfig, redisCfg RedisConfig, migrationsPath string) (*Store, error) {
	// 1. Create Postgres pool
	pool, err := NewPool(ctx, pgCfg)
	if err != nil {
		return nil, fmt.Errorf("store: postgres unreachable: %w", err)
	}

	// 2. Run migrations
	if migrationsPath != "" {
		if err := RunMigrations(pgCfg.DSN, migrationsPath); err != nil {
			pool.Close()
			return nil, fmt.Errorf("store: migrations failed: %w", err)
		}
	}

	// 3. Create Redis client
	rdb, err := storeredis.NewRedisPool(redisCfg.URL)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: redis unreachable: %w", err)
	}

	// 4. Create repositories
	pgSessionRepo := NewSessionRepository(pool)
	pgEventRepo := NewEventRepository(pool)
	batcher := NewEventBatcher(pgEventRepo, 100, 500*time.Millisecond)

	s := &Store{
		pool:         pool,
		rdb:          rdb,
		batcher:      batcher,
		sessions:     storeredis.NewSessionRepository(rdb, pgSessionRepo),
		events:       pgEventRepo,
		ledger:       NewLedgerRepository(pool),
		flows:        NewFlowRepository(pool),
		intelligence: NewIntelligenceRepository(pool),
		hitl:         storeredis.NewHITLRepository(rdb),
	}

	return s, nil
}

// Health checks Postgres and Redis concurrently.
func (s *Store) Health(ctx context.Context) error {
	g, gctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return Health(gctx, s.pool)
	})

	g.Go(func() error {
		return s.rdb.Ping(gctx).Err()
	})

	return g.Wait()
}

// Close shuts down all backends in correct order:
// 1. Flush EventBatcher, 2. Close Redis, 3. Close Postgres pool.
func (s *Store) Close() error {
	var firstErr error

	if err := s.batcher.Close(context.Background()); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("store: batcher close: %w", err)
	}

	if err := s.rdb.Close(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("store: redis close: %w", err)
	}

	s.pool.Close()
	return firstErr
}

// Sessions returns the session repository (Redis cache + Postgres fallback).
func (s *Store) Sessions() persist.SessionRepository { return s.sessions }

// Events returns the event repository (Postgres, append-only).
func (s *Store) Events() persist.EventRepository { return s.events }

// EventBatcher returns the event batcher for high-throughput ingestion.
func (s *Store) EventBatcher() *EventBatcher { return s.batcher }

// Ledger returns the token ledger repository (Postgres).
func (s *Store) Ledger() persist.LedgerRepository { return s.ledger }

// Flows returns the flow repository (Postgres, soft-delete).
func (s *Store) Flows() persist.FlowRepository { return s.flows }

// Intelligence returns the intelligence repository (Postgres).
func (s *Store) Intelligence() persist.IntelligenceRepository { return s.intelligence }

// HITL returns the HITL repository (Redis).
func (s *Store) HITL() persist.HITLRepository { return s.hitl }
