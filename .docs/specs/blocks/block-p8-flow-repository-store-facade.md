---
title: "Block P8 — Flow Repository + Store Facade (Updated)"
version: 2.0
date_created: 2026-03-27
last_updated: 2026-04-01
owner: Agentic CPN Team
tags: golang, persistence, postgres, flow, store-facade, spec-driven, block-P8
---

# Introduction

Block P8 implements the **Postgres FlowRepository** and the **Store facade** — the final persistence block that ties together all six repositories, the EventBatcher, and the connection pools (Postgres + Redis) behind a single entry point. The FlowRepository persists crystallized CPN topologies as JSONB, enabling Flow Intelligence queries. The Store facade is the **composition root for persistence**: it creates all backends, runs migrations, and exposes repository accessors.

This is **version 2.0** of the P8 specification, updated after an expert panel audit (2026-04-01) that identified two discrepancies with the current codebase:
1. The v1.3 spec listed `cmd/cli/main.go` as the integration wire point; the **actual** wire point is `cmd/server/main.go`.
2. The Store facade must bridge `cpn.Session` + `internal/app.sessionState` into `persist.SessionRecord` — this integration concern was undocumented.

All changes are annotated with `[UPDATED v2.0]` tags.

**Spec reference:** `.docs/specs/agentic-cpn-v1.3.md` Sections 3.1, 4.4, 7, 14

**Specialist Team:**
- **Database Architect**: JSONB schema for topology/function mapping, soft-delete indexing, migration design
- **Senior Golang Engineer**: Store facade composition, graceful shutdown ordering, health check concurrency, accessor pattern
- **Software Architect**: Integration wire point in `cmd/server/main.go`, session state bridging between `cpn.Session` + `app.sessionState` + `persist.SessionRecord`

**Depends on:** P1 (#55), P2 (#56), P3 (#57), P4 (#58), P5 (#59), P6 (#60), P7 (#61)

---

## 1. Purpose & Scope

### Purpose
Provide the Postgres-backed `FlowRepository` for crystallized CPN topology persistence, and the `Store` facade that unifies all persistence concerns into a single injectable dependency for the application layer.

### Scope
- **In scope**: `PostgresFlowRepository` (5 methods), `Store` struct (constructor, Health, Close, 6 accessors), integration with `cmd/server/main.go`, session state bridging documentation
- **Out of scope**: In-memory implementations (P1), CPN serialization (P2), connection pools (P3), other Postgres repositories (P4, P5, P7), Redis repositories (P6), CPN engine runtime

### Audience
Implementers of Block P8, maintainers of `cmd/server/main.go`, and anyone integrating persistence into the application layer (`internal/app/session_service.go`).

---

## 2. Definitions

| Term | Definition |
|------|-----------|
| **FlowRepository** | Persistence interface for crystallized CPN topologies (defined in P1) |
| **Store facade** | Struct that holds all 6 repositories + EventBatcher + connection pools, providing a single dependency for the application layer |
| **Crystallized flow** | A CPN topology that has been serialized (P2), hashed, and persisted with execution statistics |
| **Soft delete** | Set `deleted_at` timestamp instead of removing data — FlowRepository.Delete semantics |
| **JSONB** | PostgreSQL binary JSON type used for `topology_json` and `function_mapping` columns |
| **TopologyHash** | Deterministic SHA-256 hex string from P2, used as the primary lookup key for flows |
| **Wire point** | `[UPDATED v2.0]` The location in the codebase where the Store is instantiated and injected — `cmd/server/main.go` |
| **Session state bridge** | `[UPDATED v2.0]` Pattern where `cpn.Session` (runtime) + `app.sessionState` (state tracking) are combined into `persist.SessionRecord` (persistence DTO) by the integration layer |
| **PoolConfig** | Configuration struct for the Postgres connection pool (from P3) |
| **RedisConfig** | Configuration struct for the Redis connection (from P6) |
| **EventBatcher** | Batched event writer from P4 that buffers events before flushing to Postgres |
| **Axiom A13** | Persistence is transparent — `cpn/` never imports drivers; all behind interfaces |
| **Axiom A14** | Hot/cold separation — ephemeral data in Redis, durable data in Postgres |

---

## 3. Requirements, Constraints & Guidelines

### Requirements

#### Package Structure
- **REQ-01**: `PostgresFlowRepository` MUST be in `store/postgres/flow.go` with `package postgres`
- **REQ-01a**: `Store` facade MUST be in `store/postgres/store.go` with `package postgres`

#### FlowRepository: 5 Methods
- **REQ-01**: `FlowRepository.Save` MUST store `TopologyJSON` and `FunctionMapping` as JSONB columns in the `flows` table
- **REQ-02**: `FlowRepository.Save` MUST store `topology_json` + `function_mapping` as JSONB columns. These come from `FlowRecord.TopologyJSON` and `FlowRecord.FunctionMapping` (both `json.RawMessage` in the DTO)
- **REQ-03**: `FlowRepository.GetByHash` MUST exclude soft-deleted records (`WHERE deleted_at IS NULL`)
- **REQ-04**: `FlowRepository.List` MUST support `FlowListOpts` with optional `Role` filter and cursor-based pagination. When `Role` is non-empty, filter by `role = $1`. Pagination uses `LIMIT` + `OFFSET` cursor pattern (cursor is `strconv.Itoa(offset)`)
- **REQ-05**: `FlowRepository.Delete` MUST perform soft delete: `UPDATE flows SET deleted_at = NOW() WHERE hash = $1`
- **REQ-01b**: `FlowRepository.UpdateStats` MUST update `execution_count`, `success_rate`, `avg_cost_usd`, `avg_duration_ms`, and `updated_at` for the given hash

#### Store Facade
- **REQ-06**: `Store` struct MUST hold all 6 repositories + `EventBatcher` + Postgres pool + Redis client references
- **REQ-07**: `NewStore(ctx context.Context, pgCfg PoolConfig, redisCfg RedisConfig)` MUST: (a) create Postgres connection pool, (b) run migrations, (c) create Redis client, (d) instantiate all 6 Postgres repositories, (e) instantiate Redis repositories (session cache, HITL), (f) instantiate EventBatcher, (g) return `(*Store, error)`
- **REQ-08**: `Store.Health(ctx context.Context)` MUST check Postgres and Redis health **concurrently** using `errgroup` or goroutines, returning a combined error if either backend is unreachable
- **REQ-09**: `Store.Close()` MUST follow this shutdown order: (1) flush EventBatcher (ensure all buffered events are written), (2) close Redis connection, (3) close Postgres connection pool. Violations of this order risk data loss (unflushed events) or connection leaks
- **REQ-10**: `Store` MUST expose repositories via accessor methods: `Sessions() SessionRepository`, `Events() EventRepository`, `Ledger() LedgerRepository`, `Flows() FlowRepository`, `Intelligence() IntelligenceRepository`, `HITL() HITLRepository`
- **REQ-11**: `NewStore` MUST return a descriptive error if any backend is unreachable at startup. The error MUST indicate which backend failed (Postgres or Redis) and include the underlying connection error via `fmt.Errorf("store: postgres unreachable: %w", err)`

#### `[NEW v2.0]` Integration Wire Point
- **REQ-12**: The Store MUST be instantiated in `cmd/server/main.go` (NOT `cmd/cli/main.go` as stated in the v1.3 spec Section 3.1). The current `cmd/server/main.go` is the composition root that wires driven adapters (LLM client, billing) into the application layer (`app.NewSessionService`). The Store is a driven adapter and follows the same pattern. The wire point code MUST:
  - Read `DATABASE_URL` and `REDIS_URL` from environment variables
  - Call `NewStore(ctx, pgCfg, redisCfg)` before creating `SessionService`
  - Pass the Store (or its repository accessors) into `SessionService`
  - Call `Store.Close()` during graceful shutdown (after HTTP server shutdown, before process exit)

#### `[NEW v2.0]` Session State Bridging
- **REQ-13**: The integration layer MUST document and implement how `persist.SessionRecord` is populated from two distinct runtime sources:
  - **`cpn.Session`** provides: `ID`, `UserID`, `Channel`, `CreatedAt`, and `Messages()` (conversation history)
  - **`internal/app.sessionState`** provides: `State` (idle, running, waiting, completed, failed — mapped to `persist.SessionState`)
  - **Integration layer** manages: `LastActivityAt` (updated on every `Touch`), `ClosedAt` (set when session is closed)

  The bridging can be implemented via one of two approaches (implementation choice):
  - **Option A**: `SessionService` populates `SessionRecord` fields before calling `SessionRepository.Create`, `UpdateState`, etc. This keeps the Store as a pure data gateway.
  - **Option B**: Store provides helper methods like `Store.SaveSession(ctx, cpnSession, appState)` that internally build the `SessionRecord`. This centralizes the mapping but couples Store to application types.

  **Recommended**: Option A — `SessionService` is the natural place because it already holds both `sessions map[string]*cpn.Session` and `states map[string]*sessionState`.

### Security Requirements
- **SEC-001**: No raw SQL string concatenation — all queries MUST use parameterized statements (`$1`, `$2`, etc.)
- **SEC-002**: Database credentials MUST come from environment variables, never hardcoded
- **SEC-003**: `Store.Close()` MUST NOT panic — all close errors are logged, not propagated

### Constraints
- **CON-001**: Depends on `github.com/jackc/pgx/v5` for Postgres and `github.com/redis/go-redis/v9` for Redis (introduced in P3/P6)
- **CON-002**: Migration `004_flows` must create the `flows` table with JSONB columns for `topology_json` and `function_mapping`
- **CON-003**: `PostgresFlowRepository` MUST implement `persist.FlowRepository` — compile-time assertion required
- **CON-004**: Tests use `testcontainers-go` for integration testing against real Postgres/Redis instances
- **CON-005**: `[UPDATED v2.0]` The `cmd/cli/main.go` wire point referenced in v1.3 spec Section 3.1 does NOT exist in the current codebase. The actual server entry point is `cmd/server/main.go`

### Guidelines
- **GUD-001**: `FlowRepository` queries should use partial indexes on `deleted_at IS NULL` for performance
- **GUD-002**: `topology_json` and `function_mapping` columns should have `CHECK` constraints ensuring valid JSON (Postgres validates JSONB on insert)
- **GUD-003**: `Store` accessor methods return interfaces (not concrete types) — callers depend on abstractions
- **GUD-004**: `Store.Health` timeout should respect the parent context deadline
- **GUD-005**: `[UPDATED v2.0]` When adding Store to `cmd/server/main.go`, follow the existing pattern: environment config at top, driven adapters in the middle, application layer construction, driving adapter last

---

## 4. Interfaces & Data Contracts

### 4.1 Package Layout

```
store/postgres/
├── pool.go             — Connection pool (P3)
├── session.go          — PostgresSessionRepository (P5)
├── event.go            — PostgresEventRepository (P4)
├── event_batcher.go    — Batch writer (P4)
├── ledger.go           — PostgresLedgerRepository (P7)
├── intelligence.go     — PostgresIntelligenceRepository (P7)
├── flow.go             — PostgresFlowRepository (P8, this block)
├── store.go            — Store facade (P8, this block)
└── migrations/
    ├── ...
    └── 004_flows.up.sql   — flows table
    └── 004_flows.down.sql — drop flows table
```

### 4.2 PostgresFlowRepository

```go
// PostgresFlowRepository implements persist.FlowRepository backed by Postgres.
type PostgresFlowRepository struct {
    pool *pgxpool.Pool
}

// Compile-time interface assertion.
var _ persist.FlowRepository = (*PostgresFlowRepository)(nil)

func NewPostgresFlowRepository(pool *pgxpool.Pool) *PostgresFlowRepository {
    return &PostgresFlowRepository{pool: pool}
}
```

#### Method Signatures

```go
func (r *PostgresFlowRepository) Save(ctx context.Context, flow *persist.FlowRecord) error
func (r *PostgresFlowRepository) GetByHash(ctx context.Context, hash string) (*persist.FlowRecord, error)
func (r *PostgresFlowRepository) List(ctx context.Context, opts *persist.FlowListOpts) (*persist.Page[*persist.FlowRecord], error)
func (r *PostgresFlowRepository) UpdateStats(ctx context.Context, hash string, stats *persist.FlowStats) error
func (r *PostgresFlowRepository) Delete(ctx context.Context, hash string) error
```

#### SQL Patterns

**Save** (upsert by hash):
```sql
INSERT INTO flows (hash, role, topology_json, function_mapping, execution_count, success_rate, avg_cost_usd, avg_duration_ms, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (hash) DO UPDATE SET
    topology_json = EXCLUDED.topology_json,
    function_mapping = EXCLUDED.function_mapping,
    updated_at = EXCLUDED.updated_at
```

**GetByHash** (exclude soft-deleted):
```sql
SELECT hash, role, topology_json, function_mapping,
       execution_count, success_rate, avg_cost_usd, avg_duration_ms,
       created_at, updated_at, deleted_at
FROM flows
WHERE hash = $1 AND deleted_at IS NULL
```

**List** (with optional role filter + pagination):
```sql
SELECT hash, role, topology_json, function_mapping,
       execution_count, success_rate, avg_cost_usd, avg_duration_ms,
       created_at, updated_at, deleted_at
FROM flows
WHERE deleted_at IS NULL
  AND ($1 = '' OR role = $1)
ORDER BY created_at DESC
LIMIT $2 OFFSET $3
```

**UpdateStats**:
```sql
UPDATE flows
SET execution_count = $2, success_rate = $3, avg_cost_usd = $4, avg_duration_ms = $5, updated_at = NOW()
WHERE hash = $1 AND deleted_at IS NULL
```

**Delete** (soft):
```sql
UPDATE flows SET deleted_at = NOW() WHERE hash = $1 AND deleted_at IS NULL
```

### 4.3 Migration: 004_flows

**004_flows.up.sql**:
```sql
CREATE TABLE IF NOT EXISTS flows (
    hash            TEXT        PRIMARY KEY,
    role            TEXT        NOT NULL DEFAULT '',
    topology_json   JSONB       NOT NULL,
    function_mapping JSONB      NOT NULL DEFAULT '{}',
    execution_count BIGINT      NOT NULL DEFAULT 0,
    success_rate    DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    avg_cost_usd    DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    avg_duration_ms BIGINT      NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at      TIMESTAMPTZ
);

-- Partial index for active flows (soft-delete pattern)
CREATE INDEX IF NOT EXISTS idx_flows_active ON flows (role, created_at DESC)
    WHERE deleted_at IS NULL;

-- Index for looking up active flows by hash (covers GetByHash WHERE clause)
CREATE INDEX IF NOT EXISTS idx_flows_hash_active ON flows (hash)
    WHERE deleted_at IS NULL;
```

**004_flows.down.sql**:
```sql
DROP TABLE IF EXISTS flows;
```

### 4.4 Store Facade

```go
// Store is the persistence facade that holds all repositories and backends.
// It is the single dependency injected into the application layer.
type Store struct {
    pool    *pgxpool.Pool
    redis   *redis.Client
    batcher *EventBatcher

    sessions     persist.SessionRepository
    events       persist.EventRepository
    ledger       persist.LedgerRepository
    flows        persist.FlowRepository
    intelligence persist.IntelligenceRepository
    hitl         persist.HITLRepository
}
```

#### Constructor

```go
// NewStore creates a Store by connecting to Postgres and Redis, running
// migrations, and instantiating all repositories. Returns a descriptive
// error if any backend is unreachable.
func NewStore(ctx context.Context, pgCfg PoolConfig, redisCfg RedisConfig) (*Store, error) {
    // 1. Create Postgres pool
    pool, err := NewPool(ctx, pgCfg)
    if err != nil {
        return nil, fmt.Errorf("store: postgres unreachable: %w", err)
    }

    // 2. Run migrations
    if err := RunMigrations(pool); err != nil {
        pool.Close()
        return nil, fmt.Errorf("store: migration failed: %w", err)
    }

    // 3. Create Redis client
    rdb, err := NewRedisClient(ctx, redisCfg)
    if err != nil {
        pool.Close()
        return nil, fmt.Errorf("store: redis unreachable: %w", err)
    }

    // 4. Instantiate repositories
    sessions := NewPostgresSessionRepository(pool)
    events := NewPostgresEventRepository(pool)
    batcher := NewEventBatcher(events, BatcherConfig{...})
    ledger := NewPostgresLedgerRepository(pool)
    flows := NewPostgresFlowRepository(pool)
    intelligence := NewPostgresIntelligenceRepository(pool)

    // 5. Redis-backed repositories
    hitl := NewRedisHITLRepository(rdb)
    // Session cache wraps Postgres session repo with Redis cache
    cachedSessions := NewRedisSessionRepository(rdb, sessions)

    return &Store{
        pool:         pool,
        redis:        rdb,
        batcher:      batcher,
        sessions:     cachedSessions,
        events:       batcher, // EventBatcher implements EventRepository
        ledger:       ledger,
        flows:        flows,
        intelligence: intelligence,
        hitl:         hitl,
    }, nil
}
```

#### Accessors

```go
func (s *Store) Sessions() persist.SessionRepository     { return s.sessions }
func (s *Store) Events() persist.EventRepository         { return s.events }
func (s *Store) Ledger() persist.LedgerRepository        { return s.ledger }
func (s *Store) Flows() persist.FlowRepository           { return s.flows }
func (s *Store) Intelligence() persist.IntelligenceRepository { return s.intelligence }
func (s *Store) HITL() persist.HITLRepository            { return s.hitl }
```

#### Health Check

```go
// Health checks Postgres and Redis concurrently.
// Returns a combined error if either backend is unreachable.
func (s *Store) Health(ctx context.Context) error {
    g, ctx := errgroup.WithContext(ctx)

    g.Go(func() error {
        if err := s.pool.Ping(ctx); err != nil {
            return fmt.Errorf("postgres: %w", err)
        }
        return nil
    })

    g.Go(func() error {
        if err := s.redis.Ping(ctx).Err(); err != nil {
            return fmt.Errorf("redis: %w", err)
        }
        return nil
    })

    return g.Wait()
}
```

#### Graceful Shutdown

```go
// Close shuts down the Store in the correct order:
// 1. Flush EventBatcher (ensure all buffered events are written)
// 2. Close Redis connection
// 3. Close Postgres connection pool
// This order prevents data loss (unflushed events) and connection leaks.
func (s *Store) Close() error {
    var errs []error

    // 1. Flush event batcher first
    if err := s.batcher.Flush(); err != nil {
        errs = append(errs, fmt.Errorf("flush batcher: %w", err))
    }

    // 2. Close Redis
    if err := s.redis.Close(); err != nil {
        errs = append(errs, fmt.Errorf("close redis: %w", err))
    }

    // 3. Close Postgres pool
    s.pool.Close()

    return errors.Join(errs...)
}
```

### 4.5 `[UPDATED v2.0]` Integration in cmd/server/main.go

The following shows how the Store integrates into the existing composition root:

```go
// cmd/server/main.go — additions for persistence (P8)

func main() {
    // ... existing config ...

    // ── Persistence config (NEW) ───────────────────────────────────
    dbURL := os.Getenv("DATABASE_URL")
    redisURL := os.Getenv("REDIS_URL")

    // ── Driven adapters ─────────────────────────────────────────────
    llmClient := openrouter.NewClient(apiKey, defaultModel)
    costProvider := &ledgerCostAdapter{ledger: llmClient.TokenLedger}

    // ── Persistence (NEW) ──────────────────────────────────────────
    var store *postgres.Store
    if dbURL != "" {
        var err error
        store, err = postgres.NewStore(ctx, postgres.PoolConfig{URL: dbURL}, postgres.RedisConfig{URL: redisURL})
        if err != nil {
            logger.Error("store initialization failed", slog.Any("error", err))
            os.Exit(1)
        }
        defer store.Close()
    }

    // ── Application layer ───────────────────────────────────────────
    // SessionService receives Store (or nil for in-memory mode)
    appService := app.NewSessionService(llmClient, costProvider, logger, topologyFactory)
    // When Store is available, wire persistence:
    // if store != nil {
    //     appService.SetStore(store)
    // }

    // ... rest of existing code ...

    // Graceful shutdown — Store.Close() is called via defer above,
    // which runs after HTTP server shutdown.
}
```

### 4.6 `[UPDATED v2.0]` Session State Bridge Pattern

The `persist.SessionRecord` DTO (defined in P1) combines fields from multiple runtime sources:

| SessionRecord Field | Source | Notes |
|---|---|---|
| `ID` | `cpn.Session.ID` | Direct |
| `UserID` | `cpn.Session.UserID` | Direct |
| `Channel` | `cpn.Session.Channel` | `string(session.Channel)` |
| `State` | `app.sessionState.get()` | Mapped: `cpn.StateIdle` -> `persist.SessionActive`, `cpn.StateRunning` -> `persist.SessionActive`, `cpn.StateCompleted` -> `persist.SessionClosed` |
| `CreatedAt` | `cpn.Session.CreatedAt` | Direct |
| `LastActivityAt` | Integration layer | Updated by `SessionRepository.Touch()` on every `SendMessage` |
| `ClosedAt` | Integration layer | Set by `SessionRepository.Close()` when session is explicitly closed or expired |
| `Metadata` | Integration layer | Optional JSON metadata (e.g., topology hash, channel config) |

**State mapping** (from `cpn.State` to `persist.SessionState`):

```go
func mapState(s cpn.State) persist.SessionState {
    switch s {
    case cpn.StateIdle, cpn.StateRunning, cpn.StateWaiting:
        return persist.SessionActive
    case cpn.StateCompleted, cpn.StateFailed:
        return persist.SessionClosed
    default:
        return persist.SessionActive
    }
}
```

**SessionService integration** (Option A — recommended):

```go
// In SessionService.CreateSession, after creating cpn.Session:
func (s *SessionService) persistSession(session *cpn.Session, state *sessionState) error {
    if s.store == nil {
        return nil // in-memory mode
    }
    rec := &persist.SessionRecord{
        ID:             session.ID,
        UserID:         session.UserID,
        Channel:        string(session.Channel),
        State:          mapState(state.get()),
        CreatedAt:      session.CreatedAt,
        LastActivityAt: time.Now(),
    }
    return s.store.Sessions().Create(context.Background(), rec)
}
```

---

## 5. Acceptance Criteria

### FlowRepository
- **AC-001**: `Save` persists a `FlowRecord` and `GetByHash` retrieves it with matching `TopologyJSON` and `FunctionMapping`
- **AC-002**: `Save` with an existing hash updates `topology_json`, `function_mapping`, and `updated_at` (upsert)
- **AC-003**: `GetByHash` returns `persist.ErrFlowNotFound` for non-existent hash
- **AC-004**: `GetByHash` returns `persist.ErrFlowNotFound` for soft-deleted hash
- **AC-005**: `List` returns `*Page[*FlowRecord]` with correct pagination (HasMore, NextCursor)
- **AC-006**: `List` with `Role` filter returns only matching flows
- **AC-007**: `List` excludes soft-deleted flows
- **AC-008**: `UpdateStats` modifies `execution_count`, `success_rate`, `avg_cost_usd`, `avg_duration_ms`
- **AC-009**: `Delete` sets `deleted_at` (does NOT remove the row)
- **AC-010**: `Delete` on already-deleted flow is a no-op (idempotent)
- **AC-011**: All methods return `context.Canceled` when context is cancelled

### Store Facade
- **AC-012**: `NewStore` with valid config connects and returns `(*Store, nil)`
- **AC-013**: `NewStore` with unreachable Postgres returns error containing `"postgres unreachable"`
- **AC-014**: `NewStore` with unreachable Redis returns error containing `"redis unreachable"`
- **AC-015**: `Health` returns nil when both backends are healthy
- **AC-016**: `Health` returns error when Postgres is down
- **AC-017**: `Health` returns error when Redis is down
- **AC-018**: `Health` checks Postgres and Redis concurrently (not sequentially)
- **AC-019**: `Close` flushes EventBatcher before closing connections
- **AC-020**: `Close` does not panic on double-close
- **AC-021**: All 6 accessor methods return non-nil repositories
- **AC-022**: Compile-time assertion: `var _ persist.FlowRepository = (*PostgresFlowRepository)(nil)`

### `[UPDATED v2.0]` Integration
- **AC-023**: Store is instantiated in `cmd/server/main.go`, not `cmd/cli/main.go`
- **AC-024**: `DATABASE_URL` and `REDIS_URL` environment variables are read for Store configuration
- **AC-025**: `Store.Close()` is called during graceful shutdown after HTTP server shutdown
- **AC-026**: `SessionService` can operate without a Store (in-memory fallback for development)
- **AC-027**: When Store is present, `SessionRecord` is correctly populated from both `cpn.Session` and `app.sessionState`

---

## 6. Test Automation Strategy

### Test Files
- `store/postgres/flow_test.go` — FlowRepository integration tests
- `store/postgres/store_test.go` — Store facade integration tests

### Test Structure

| Test Function | Subtests | Count |
|---------------|----------|-------|
| `TestPostgresFlowRepository_Save` | Insert, Upsert_update, Invalid_json | 3 |
| `TestPostgresFlowRepository_GetByHash` | Found, Not_found, Soft_deleted | 3 |
| `TestPostgresFlowRepository_List` | All, By_role, Pagination, Empty, Excludes_deleted | 5 |
| `TestPostgresFlowRepository_UpdateStats` | Update, Not_found, Deleted_noop | 3 |
| `TestPostgresFlowRepository_Delete` | Soft_delete, Idempotent, Not_found | 3 |
| `TestPostgresFlowRepository_Context` | Cancelled | 1 |
| `TestNewStore` | Success, Postgres_unreachable, Redis_unreachable | 3 |
| `TestStore_Health` | Healthy, Postgres_down, Redis_down, Concurrent | 4 |
| `TestStore_Close` | Order, Double_close, Flush_error | 3 |
| `TestStore_Accessors` | All_non_nil | 1 |
| **Total** | | **~29** |

### Framework
- Go standard `testing` package + `testcontainers-go` for real Postgres/Redis instances
- Each test function starts a fresh container (or uses shared container with transaction rollback)
- `-race` flag via Makefile
- Tests tagged with `//go:build integration` to separate from unit tests

---

## 7. Rationale & Context

### Why JSONB for Topology and Function Mapping
PostgreSQL's JSONB type provides: (a) validation on insert (rejects malformed JSON), (b) indexing support (GIN indexes for future queries), (c) partial updates without full-document replacement, (d) compact binary storage. The topology JSON (from P2's `MarshalCPN`) and function mapping (registry name-to-kind map) are both variable-structure documents that fit JSONB naturally.

### Why Soft Delete for Flows
Crystallized flows may be referenced by execution records (P7) and Flow Intelligence queries. Hard deletion would create dangling references. Soft delete preserves referential integrity while hiding "deleted" flows from normal queries. The partial index on `deleted_at IS NULL` ensures soft-delete does not degrade query performance.

### Why Store Facade (Not Individual Repository Injection)
Injecting 6 repositories + EventBatcher + health check individually into `SessionService` would require 8+ constructor parameters. The Store facade provides:
- Single dependency injection point
- Coordinated lifecycle (startup migrations, shutdown ordering)
- Health check aggregation
- Clean accessor pattern

### `[UPDATED v2.0]` Why cmd/server/main.go (Not cmd/cli/main.go)
The v1.3 spec Section 3.1 listed `cmd/cli/main.go` as the wire point. This path does not exist in the current codebase. The actual composition root is `cmd/server/main.go`, which already wires:
- `openrouter.NewClient` (LLM driven adapter)
- `billing.NewClient` (billing driven adapter)
- `app.NewSessionService` (application layer)
- `httpapi.NewServer` (driving adapter)

The Store facade follows the same hexagonal architecture pattern: it is a driven adapter instantiated in the composition root and injected into the application layer.

### `[UPDATED v2.0]` Why SessionService Owns the State Bridge
The `cpn.Session` struct (in `cpn/` package) cannot carry persistence state fields (`persist.SessionState`, `LastActivityAt`, `ClosedAt`) because:
1. **Axiom A13**: `cpn/` must not import persistence types
2. **Separation of concerns**: `cpn.Session` is a runtime binding (user-to-CPN), not a persistence entity
3. **State tracking**: `sessionState` already lives in `internal/app/` because CPN execution lifecycle (idle/running/waiting) is an application concern

Therefore, `SessionService` is the natural integration point. It holds both maps (`sessions` and `states`) and can construct `SessionRecord` DTOs before calling repository methods.

---

## 8. Dependencies & External Integrations

### Block Dependencies
- **DEP-001**: P1 (#55) — `persist.FlowRepository` interface, `persist.FlowRecord`, `persist.FlowStats`, `persist.FlowListOpts`, `persist.Page[T]`, sentinel errors
- **DEP-002**: P2 (#56) — `persist.MarshalCPN`, `persist.TopologyHash`, `persist.FuncRegistry` — used to populate `FlowRecord.TopologyJSON` and `FlowRecord.FunctionMapping`
- **DEP-003**: P3 (#57) — `postgres.NewPool`, `postgres.PoolConfig`, `postgres.RunMigrations` — connection pool and migration runner
- **DEP-004**: P4 (#58) — `postgres.NewPostgresEventRepository`, `postgres.NewEventBatcher` — event persistence + batching
- **DEP-005**: P5 (#59) — `postgres.NewPostgresSessionRepository` — session persistence
- **DEP-006**: P6 (#60) — `redis.NewRedisClient`, `redis.RedisConfig`, `redis.NewRedisSessionRepository`, `redis.NewRedisHITLRepository` — Redis cache layer
- **DEP-007**: P7 (#61) — `postgres.NewPostgresLedgerRepository`, `postgres.NewPostgresIntelligenceRepository` — ledger + intelligence persistence

### Technology Dependencies
- **PLT-001**: Go 1.25
- **PLT-002**: `github.com/jackc/pgx/v5` — Postgres driver
- **PLT-003**: `github.com/redis/go-redis/v9` — Redis client
- **PLT-004**: `github.com/golang-migrate/migrate/v4` — Migration runner
- **PLT-005**: `golang.org/x/sync/errgroup` — Concurrent health checks

### Depended On By
- `cmd/server/main.go` — Composition root that instantiates and injects the Store
- `internal/app/session_service.go` — Application layer that uses Store repositories
- Future: HTTP health endpoint (`GET /health`) calling `Store.Health()`

---

## 9. Examples & Edge Cases

### 9.1 Flow Lifecycle

```go
store, _ := postgres.NewStore(ctx, pgCfg, redisCfg)
defer store.Close()

flows := store.Flows()

// Save a crystallized topology
topology, _ := persist.MarshalCPN(myCPN)
hash := persist.TopologyHash(topology)
funcMap, _ := json.Marshal(registry.Snapshot())

err := flows.Save(ctx, &persist.FlowRecord{
    Hash:            hash,
    Role:            "coordinator",
    TopologyJSON:    topologyJSON,
    FunctionMapping: funcMap,
    Stats:           persist.FlowStats{},
    CreatedAt:       time.Now(),
    UpdatedAt:       time.Now(),
})

// Retrieve by hash
flow, err := flows.GetByHash(ctx, hash)
// flow.TopologyJSON contains JSONB topology

// Update stats after execution
flows.UpdateStats(ctx, hash, &persist.FlowStats{
    ExecutionCount: 42,
    SuccessRate:    0.95,
    AvgCostUSD:     0.003,
    AvgDurationMs:  250,
})

// Soft delete
flows.Delete(ctx, hash)
_, err = flows.GetByHash(ctx, hash) // persist.ErrFlowNotFound
```

### 9.2 Store Lifecycle

```go
store, err := postgres.NewStore(ctx,
    postgres.PoolConfig{URL: "postgres://..."},
    postgres.RedisConfig{URL: "redis://..."},
)
if err != nil {
    log.Fatalf("store: %v", err) // "store: postgres unreachable: ..."
}
defer store.Close()

// Health check
if err := store.Health(ctx); err != nil {
    log.Printf("unhealthy: %v", err)
}

// Use repositories
store.Sessions().Create(ctx, &persist.SessionRecord{...})
store.Events().Append(ctx, &persist.EventRecord{...})
store.Flows().Save(ctx, &persist.FlowRecord{...})
```

### 9.3 `[UPDATED v2.0]` Session State Bridge Example

```go
// In SessionService.CreateSession:
session := cpn.NewSession(id, userID, channel, root)
st := &sessionState{state: cpn.StateIdle}

// Persist the session (bridging two sources)
if s.store != nil {
    rec := &persist.SessionRecord{
        ID:             session.ID,
        UserID:         session.UserID,
        Channel:        string(session.Channel),
        State:          persist.SessionActive, // from sessionState
        CreatedAt:      session.CreatedAt,
        LastActivityAt: time.Now(),
    }
    if err := s.store.Sessions().Create(ctx, rec); err != nil {
        return nil, fmt.Errorf("persist session: %w", err)
    }
}

// In SessionService.SendMessage:
if s.store != nil {
    _ = s.store.Sessions().Touch(ctx, sessionID) // updates LastActivityAt
}

// After CPN run completes, persist messages:
if s.store != nil {
    for _, m := range newMessages {
        msgRec := persist.MessageToRecord(sessionID, &m)
        _ = s.store.Sessions().AppendMessage(ctx, sessionID, msgRec)
    }
}
```

### 9.4 Edge Cases

| Edge Case | Expected Behavior |
|-----------|-------------------|
| `Save` with empty hash | `persist.ErrInvalidInput` |
| `Save` with invalid JSON in TopologyJSON | Postgres rejects JSONB insert — return wrapped error |
| `GetByHash` for non-existent hash | `persist.ErrFlowNotFound` |
| `GetByHash` for soft-deleted hash | `persist.ErrFlowNotFound` |
| `List` with empty FlowListOpts | Returns all active flows, default limit 100 |
| `List` with Limit=0 | Defaults to 100 |
| `List` with invalid cursor | Starts from beginning (offset=0) |
| `UpdateStats` for non-existent hash | Return error (no rows affected) |
| `Delete` on already-deleted flow | No-op (idempotent), no error |
| `NewStore` with empty DATABASE_URL | Return descriptive error |
| `Health` with 5s context timeout | Both Postgres and Redis checks respect timeout |
| `Close` called twice | Second call is no-op, no panic |
| `Close` when EventBatcher has buffered events | Events are flushed before pool closes |
| `[v2.0]` Store is nil in SessionService | All persistence calls are skipped (in-memory mode) |
| `[v2.0]` sessionState is StateRunning during persist | Maps to `persist.SessionActive` (active means in-use) |

---

## 10. Validation Criteria

```bash
cd back/go-assistant

# 1. Package compiles
go build ./store/postgres/

# 2. Vet passes
go vet ./store/postgres/

# 3. Lint passes
golangci-lint run ./store/postgres/

# 4. Integration tests pass with race detector
go test -race -count=1 -tags=integration -v ./store/postgres/...

# 5. Coverage check (flow.go + store.go)
go test -race -tags=integration -coverprofile=p8_coverage.out ./store/postgres/...
go tool cover -func=p8_coverage.out | grep -E "(flow|store)\.go"

# 6. Compile-time interface assertion
grep "var _ persist.FlowRepository" store/postgres/flow.go

# 7. Verify migration exists
ls store/postgres/migrations/004_flows.up.sql
ls store/postgres/migrations/004_flows.down.sql

# 8. Verify Store accessors
grep -n "func (s \*Store)" store/postgres/store.go

# 9. Verify no hardcoded credentials
! grep -rn "password\|secret" store/postgres/

# 10. [v2.0] Verify wire point is cmd/server/main.go
grep -n "NewStore\|Store" cmd/server/main.go

# 11. [v2.0] Verify parameterized queries (no string concat)
! grep -n "fmt.Sprintf.*SELECT\|fmt.Sprintf.*INSERT\|fmt.Sprintf.*UPDATE\|fmt.Sprintf.*DELETE" store/postgres/flow.go
```

---

## 11. Related Specifications / Further Reading

- [Agentic CPN v1.3 — Persistence Layer](../agentic-cpn-v1.3.md) — Sections 3.1, 4.4, 7, 14
- [Block P1: Repository Interfaces](block-p1-repository-interfaces.md) — FlowRepository interface, FlowRecord DTO, sentinel errors
- [Block P2: CPN Topology Serialization](block-p2-cpn-topology-serialization.md) — MarshalCPN, TopologyHash, FuncRegistry
- [Block P3: Postgres Connection Pool + Migrations](block-p3-postgres-connection-pool-migrations.md) — PoolConfig, RunMigrations
- [Block P4: Postgres Event Repository + Batcher](block-p4-postgres-event-repository-batcher.md) — EventBatcher
- [Block P5: Postgres Session + Message Repository](block-p5-postgres-session-message.md) — SessionRepository Postgres impl
- [Block P6: Redis Session Cache + HITL Repository](block-p6-redis-session-cache-hitl.md) — Redis config, HITL repository
- [Block P7: Postgres Ledger + Intelligence Repositories](block-p7-postgres-ledger-intelligence.md) — Ledger + Intelligence Postgres impl

---

## Appendix A: Change Log from v1.0

| Change | Reason | Impact |
|--------|--------|--------|
| Wire point corrected: `cmd/server/main.go` (not `cmd/cli/main.go`) | `cmd/cli/main.go` does not exist in the current codebase. The actual composition root is `cmd/server/main.go`, which already wires all driven adapters | P8, all integration code |
| Added REQ-12: Store integration with `cmd/server/main.go` | Documents the actual wire point with environment variables, constructor call, and shutdown ordering | P8 |
| Added REQ-13: Session state bridge documentation | `persist.SessionRecord` combines data from `cpn.Session` and `app.sessionState` — this bridging was undocumented | P8, P5, P1 |
| Added Section 4.5: Integration in cmd/server/main.go | Shows concrete code for wiring Store into existing composition root | P8 |
| Added Section 4.6: Session state bridge pattern | Documents the state mapping and recommended integration approach (Option A: SessionService populates records) | P8, P5 |
| Added AC-023 through AC-027 | Integration acceptance criteria for wire point, env vars, shutdown, fallback, and state bridge | P8 |

---

## Implementation File Checklist

- [ ] `back/go-assistant/store/postgres/flow.go` — `PostgresFlowRepository` (5 methods) + compile-time assertion
- [ ] `back/go-assistant/store/postgres/store.go` — `Store` facade (NewStore, Health, Close, 6 accessors)
- [ ] `back/go-assistant/store/postgres/migrations/004_flows.up.sql` — `flows` table with JSONB columns + partial indexes
- [ ] `back/go-assistant/store/postgres/migrations/004_flows.down.sql` — Drop `flows` table
- [ ] `back/go-assistant/store/postgres/flow_test.go` — FlowRepository integration tests (~18 subtests)
- [ ] `back/go-assistant/store/postgres/store_test.go` — Store facade integration tests (~11 subtests)
- [ ] `back/go-assistant/cmd/server/main.go` — `[UPDATED v2.0]` Wire Store: read `DATABASE_URL`/`REDIS_URL`, call `NewStore`, inject into `SessionService`, defer `Store.Close()`
- [ ] `back/go-assistant/internal/app/session_service.go` — `[UPDATED v2.0]` Add optional Store field, `SetStore` method, and session state bridge helpers (`mapState`, `persistSession`)
- [ ] Run `go build ./store/postgres/` — compiles
- [ ] Run `go vet ./store/postgres/` — passes
- [ ] Run `golangci-lint run ./store/postgres/` — zero findings
- [ ] Run `go test -race -tags=integration -count=1 ./store/postgres/` — all pass
- [ ] Verify: `var _ persist.FlowRepository = (*PostgresFlowRepository)(nil)` compiles
- [ ] Verify: All SQL uses parameterized queries (`$1`, `$2`, etc.)
- [ ] Verify: `Store.Close()` order: flush batcher, close Redis, close Postgres
- [ ] Verify: `Store.Health()` checks backends concurrently
- [ ] Verify: `NewStore` returns descriptive error on backend failure
- [ ] Verify: `[v2.0]` Store wired in `cmd/server/main.go`
- [ ] Verify: `[v2.0]` SessionRecord populated from both `cpn.Session` and `sessionState`
