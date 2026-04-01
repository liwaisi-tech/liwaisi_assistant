---
title: "Block P3 — Postgres Connection Pool + Migrations (Updated)"
version: 2.0
date_created: 2026-03-27
last_updated: 2026-04-01
owner: Agentic CPN Team
tags: golang, persistence, postgres, migrations, infrastructure, spec-driven, block-P3
---

# Introduction

Block P3 establishes the **Postgres connection pool**, **5 SQL migrations**, and **Docker Compose infrastructure** for the Agentic CPN persistence layer (v1.3). It is the foundational infrastructure block that all Postgres-backed repositories (P4, P5, P7, P8) and the Redis layer (P6) depend on.

This is **version 2.0** of the P3 specification, updated after an expert panel audit (2026-04-01) that identified 3 discrepancies between the original spec and the current codebase. All changes are annotated with `[UPDATED v2.0]` tags.

**Spec reference:** `.docs/specs/agentic-cpn-v1.3.md` Sections 7, 10, 11, 13, 14

**Specialist Team:**
- **Database Architect**: Schema design, partitioning strategy, migration ordering, index selection
- **Senior Golang Engineer**: pgx/v5 pool configuration, fail-fast validation, graceful shutdown, testcontainers-go
- **DevSecOps Engineer**: Docker Compose services, health checks, credential redaction, DSN security, .env management

**Depends on:** P1 (#55) — schema matches DTOs

---

## 1. Purpose & Scope

### Purpose
Provide a production-ready Postgres connection pool with health checking, 5 SQL migrations that create the full persistence schema, and the Docker Compose infrastructure needed to run Postgres (and Redis for P6) in development.

### Scope
- **In scope**: `PoolConfig`, `NewPool`, `Health`, `Close`, 5 migration files (up + down), `RunMigrations`, Docker Compose Postgres + Redis services, `.env.example` updates, testcontainers-go integration tests
- **Out of scope**: Repository implementations (P4, P5, P7, P8), Redis client code (P6), event batching logic (P4), Store facade (P8)

### Audience
Implementers of Block P3 and all downstream blocks (P4-P8). Also serves as the infrastructure reference for local development setup.

---

## 2. Definitions

| Term | Definition |
|------|-----------|
| **Connection Pool** | `*pgxpool.Pool` from `github.com/jackc/pgx/v5/pgxpool` — manages a pool of Postgres connections with configurable limits |
| **DSN** | Data Source Name — Postgres connection string (e.g., `postgres://user:pass@host:5432/db?sslmode=disable`) |
| **Migration** | Versioned SQL file applied in order to evolve the database schema. Each migration has an `up` (apply) and `down` (rollback) file |
| **golang-migrate** | `github.com/golang-migrate/migrate/v4` — SQL migration runner used for schema versioning |
| **testcontainers-go** | `github.com/testcontainers/testcontainers-go` — spins up real Docker containers for integration tests |
| **Fail-fast** | `NewPool` validates the connection immediately; if Postgres is unreachable, the server does not start |
| **Partitioned table** | Postgres range-partitioned table where rows are distributed across child tables by a key (month for events) |
| **Health check** | `SELECT 1` probe to verify the Postgres connection is alive |
| **`[UPDATED v2.0]` pgxpool.Pool** | The ONLY Postgres driver type used in this project. The v1.3 spec Section 10 incorrectly referenced `*sql.DB` for EventBatcher. ALL code (pool, repositories, batcher) MUST use `*pgxpool.Pool` from pgx/v5 |
| **`[UPDATED v2.0]` Wire point** | The composition root where dependencies are constructed and injected. For this project it is `cmd/server/main.go`, NOT `cmd/cli/main.go` as stated in the original spec |

---

## 3. Requirements, Constraints & Guidelines

### Requirements

#### Package Structure
- **REQ-01**: All pool/migration files MUST be in `store/postgres/` sub-package with `package postgres`
- **REQ-02**: Module path: `github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/store/postgres`

#### PoolConfig
- **REQ-03**: `PoolConfig` struct MUST include fields: `DSN string`, `MaxConns int32`, `MinConns int32`, `MaxConnLifetime time.Duration`, `HealthCheckInterval time.Duration`, `SSLMode string`
- **REQ-04**: `PoolConfig.String()` MUST redact credentials from the DSN (username/password replaced with `***`) while preserving the host, port, database, and parameters. This prevents accidental credential leakage in logs.
- **REQ-05**: `PoolConfig` MUST have sensible defaults: `MaxConns=10`, `MinConns=2`, `MaxConnLifetime=30m`, `HealthCheckInterval=15s`, `SSLMode="disable"` (overridden in production)

#### NewPool (Fail-Fast)
- **REQ-06**: `NewPool(ctx context.Context, cfg PoolConfig) (*pgxpool.Pool, error)` MUST validate the connection by executing a `SELECT 1` query. If Postgres is unreachable, return an error immediately — the server MUST NOT start with a broken database connection.
- **REQ-07**: `[UPDATED v2.0]` `NewPool` MUST return `*pgxpool.Pool` from `github.com/jackc/pgx/v5/pgxpool`. It MUST NOT use `*sql.DB` from `database/sql`. All downstream consumers (repositories, EventBatcher) receive `*pgxpool.Pool`.

#### Health & Close
- **REQ-08**: `Health(ctx context.Context, pool *pgxpool.Pool) error` MUST execute `SELECT 1` and return nil on success or a wrapped error on failure.
- **REQ-09**: `Close(pool *pgxpool.Pool)` MUST call `pool.Close()` to drain connections gracefully. This is called during server shutdown.

#### Migrations (5 total)
- **REQ-10**: Migration `001_sessions.up.sql` MUST create:
  - `sessions` table: `id TEXT PRIMARY KEY`, `user_id TEXT NOT NULL`, `channel TEXT NOT NULL`, `state TEXT NOT NULL DEFAULT 'active'`, `metadata JSONB`, `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`, `last_activity_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`, `closed_at TIMESTAMPTZ`
  - `messages` table: `id TEXT PRIMARY KEY`, `session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE`, `role TEXT NOT NULL`, `content TEXT NOT NULL`, `cpn_id TEXT`, `cpn_role TEXT`, `cpn_depth INT DEFAULT 0`, `timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()`
  - Index: `idx_sessions_user_id ON sessions(user_id)`
  - Index: `idx_sessions_state ON sessions(state)`
  - Index: `idx_messages_session_id ON messages(session_id)`

- **REQ-11**: Migration `002_events.up.sql` MUST create:
  - `events` table (range-partitioned by month on `timestamp`): `id TEXT NOT NULL`, `type TEXT NOT NULL`, `session_id TEXT NOT NULL`, `cpn_id TEXT NOT NULL`, `cpn_role TEXT`, `cpn_depth INT DEFAULT 0`, `transition_id TEXT`, `transition_kind TEXT`, `token_snapshot JSONB`, `payload JSONB`, `timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()`, `PRIMARY KEY (id, timestamp)`
  - Initial partition: `events_default` as DEFAULT partition
  - Index: `idx_events_session_id ON events(session_id, timestamp)`
  - Index: `idx_events_cpn_id ON events(cpn_id, timestamp)`
  - Index: `idx_events_type ON events(type, timestamp)`

- **REQ-12**: Migration `003_token_ledger.up.sql` MUST create:
  - `token_ledger` table: `session_id TEXT PRIMARY KEY`, `input_tokens BIGINT NOT NULL DEFAULT 0`, `output_tokens BIGINT NOT NULL DEFAULT 0`, `calls BIGINT NOT NULL DEFAULT 0`, `total_cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0`, `daily_total_usd DOUBLE PRECISION NOT NULL DEFAULT 0`, `last_updated TIMESTAMPTZ NOT NULL DEFAULT NOW()`
  - Index: `idx_token_ledger_last_updated ON token_ledger(last_updated)`

- **REQ-13**: Migration `004_flows.up.sql` MUST create:
  - `flows` table: `hash TEXT PRIMARY KEY`, `role TEXT NOT NULL`, `topology_json JSONB NOT NULL`, `function_mapping JSONB NOT NULL`, `execution_count BIGINT NOT NULL DEFAULT 0`, `success_rate DOUBLE PRECISION NOT NULL DEFAULT 0`, `avg_cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0`, `avg_duration_ms BIGINT NOT NULL DEFAULT 0`, `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`, `updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`, `deleted_at TIMESTAMPTZ`
  - Index: `idx_flows_role ON flows(role) WHERE deleted_at IS NULL`

- **REQ-14**: Migration `005_execution_records.up.sql` MUST create:
  - `execution_records` table: `id TEXT PRIMARY KEY`, `cpn_id TEXT NOT NULL`, `cpn_role TEXT NOT NULL`, `cpn_depth INT NOT NULL DEFAULT 0`, `session_id TEXT NOT NULL`, `transitions_fired INT NOT NULL DEFAULT 0`, `llm_calls INT NOT NULL DEFAULT 0`, `tool_calls INT NOT NULL DEFAULT 0`, `tokens_produced INT NOT NULL DEFAULT 0`, `total_cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0`, `duration_ms BIGINT NOT NULL DEFAULT 0`, `success BOOLEAN NOT NULL DEFAULT false`, `started_at TIMESTAMPTZ NOT NULL`, `completed_at TIMESTAMPTZ NOT NULL`
  - Index: `idx_execution_records_cpn_role ON execution_records(cpn_role, completed_at)`
  - Index: `idx_execution_records_session_id ON execution_records(session_id)`

- **REQ-15**: Each migration MUST have a corresponding `.down.sql` file that reverses the changes (DROP TABLE, DROP INDEX)

#### RunMigrations
- **REQ-16**: `RunMigrations(dsn string, migrationsPath string) error` MUST apply all pending migrations using `golang-migrate/v4`. It MUST use the `pgx5` driver source for golang-migrate (not `database/sql`).
- **REQ-17**: `RunMigrations` MUST be idempotent — running it multiple times on an already-migrated database MUST NOT error.

#### Timestamp Policy
- **REQ-18**: ALL timestamp columns across ALL migrations MUST use `TIMESTAMPTZ` (not `TIMESTAMP`). This ensures correct timezone handling regardless of Postgres server timezone setting.

#### Environment Variable
- **REQ-19**: The DSN MUST be read from the `LIWAISI_DB_DSN` environment variable. The pool MUST NOT start if this variable is empty.

#### External Dependencies
- **REQ-20**: `go.mod` MUST add: `github.com/jackc/pgx/v5`, `github.com/golang-migrate/migrate/v4`, `github.com/testcontainers/testcontainers-go`

#### `[UPDATED v2.0]` Docker Compose Infrastructure
- **REQ-21**: `[UPDATED v2.0]` `docker-compose.yml` MUST be updated to include a `postgres` service:
  - Image: `postgres:16-alpine`
  - Container name: `liwaisi-postgres`
  - Environment: `POSTGRES_DB=liwaisi`, `POSTGRES_USER=liwaisi`, `POSTGRES_PASSWORD=${POSTGRES_PASSWORD:-liwaisi_dev}`
  - Port mapping: `5432:5432`
  - Volume: `pgdata:/var/lib/postgresql/data`
  - Health check: `pg_isready -U liwaisi -d liwaisi` with `interval: 5s`, `timeout: 3s`, `retries: 5`
  - The `backend` service MUST add `depends_on: postgres: condition: service_healthy`

- **REQ-22**: `[UPDATED v2.0]` `docker-compose.yml` MUST be updated to include a `redis` service (infrastructure for P6):
  - Image: `redis:7-alpine`
  - Container name: `liwaisi-redis`
  - Command: `redis-server --appendonly yes`
  - Port mapping: `6379:6379`
  - Volume: `redisdata:/var/lib/redis/data`
  - Health check: `redis-cli ping` with `interval: 5s`, `timeout: 3s`, `retries: 5`

- **REQ-23**: `[UPDATED v2.0]` `docker-compose.yml` MUST declare named volumes: `pgdata`, `redisdata`

- **REQ-24**: `[UPDATED v2.0]` `.env.example` MUST be updated with:
  ```
  # ── Persistence (P3+) ──────────────────────────────────────
  # Postgres DSN (required for persistence layer)
  # LIWAISI_DB_DSN=postgres://liwaisi:liwaisi_dev@localhost:5432/liwaisi?sslmode=disable

  # Postgres password for Docker Compose
  # POSTGRES_PASSWORD=liwaisi_dev

  # Redis URL (required for P6 session cache + HITL)
  # LIWAISI_REDIS_URL=redis://localhost:6379/0
  ```

#### `[UPDATED v2.0]` Driver Consistency (EventBatcher)
- **REQ-25**: `[UPDATED v2.0]` The EventBatcher (P4) MUST receive `*pgxpool.Pool`, NOT `*sql.DB`. The v1.3 spec Section 10 incorrectly shows `db *sql.DB` in the `EventBatcher` struct. This is corrected here: the field MUST be `pool *pgxpool.Pool`. P3 documents this for P4 implementers. The `COPY` protocol for bulk inserts is available via `pgxpool.Pool.CopyFrom()` — no `database/sql` needed.

#### `[UPDATED v2.0]` Wire Point
- **REQ-26**: `[UPDATED v2.0]` The pool MUST be wired in `cmd/server/main.go` (the composition root), NOT `cmd/cli/main.go`. The server reads `LIWAISI_DB_DSN`, calls `NewPool`, runs `RunMigrations`, and passes `*pgxpool.Pool` to repository constructors. On shutdown, it calls `Close`.

### Security Requirements
- **SEC-01**: DSN MUST never appear in logs. All logging MUST use `PoolConfig.String()` which redacts credentials.
- **SEC-02**: Migration files MUST NOT contain secrets, seed data, or user data.
- **SEC-03**: `SSLMode` MUST default to `"disable"` for local development but MUST be configurable for production (`verify-full`).

### Constraints
- **CON-01**: Pool uses `pgxpool.Pool` exclusively — no `database/sql` wrapper
- **CON-02**: Migrations use `golang-migrate/v4` with file source — no embedded migrations in v1
- **CON-03**: Events table partitioning uses Postgres native RANGE partitioning — no third-party partitioning extensions
- **CON-04**: Integration tests require Docker (testcontainers-go)
- **CON-05**: Tests use `package postgres_test` (black-box, testing exported API only)

### Guidelines
- **GUD-01**: Pool configuration should be tunable via environment variables for production, but `PoolConfig` struct allows programmatic override
- **GUD-02**: Migration files should be self-contained — each migration can be run independently
- **GUD-03**: DOWN migrations should be safe to run even if UP was never applied (use `IF EXISTS`)
- **GUD-04**: Index names follow convention: `idx_{table}_{column(s)}`
- **GUD-05**: Partitioned events table uses a DEFAULT partition to catch rows that do not match any monthly partition — prevents insert failures

---

## 4. Interfaces & Data Contracts

### 4.1 Package Layout

```
store/postgres/
├── pool.go              — PoolConfig, NewPool, Health, Close
├── migrate.go           — RunMigrations
├── migrations/
│   ├── 001_sessions.up.sql
│   ├── 001_sessions.down.sql
│   ├── 002_events.up.sql
│   ├── 002_events.down.sql
│   ├── 003_token_ledger.up.sql
│   ├── 003_token_ledger.down.sql
│   ├── 004_flows.up.sql
│   ├── 004_flows.down.sql
│   ├── 005_execution_records.up.sql
│   └── 005_execution_records.down.sql
├── pool_test.go         — Integration tests (testcontainers-go)
└── migrate_test.go      — Migration up/down tests (testcontainers-go)
```

### 4.2 PoolConfig

```go
// PoolConfig holds Postgres connection pool configuration.
// String() redacts credentials for safe logging.
type PoolConfig struct {
    DSN                 string        // Postgres connection string (from LIWAISI_DB_DSN)
    MaxConns            int32         // Maximum connections in pool (default: 10)
    MinConns            int32         // Minimum idle connections (default: 2)
    MaxConnLifetime     time.Duration // Max lifetime of a connection (default: 30m)
    HealthCheckInterval time.Duration // Interval between health probes (default: 15s)
    SSLMode             string        // TLS mode: disable, require, verify-full (default: disable)
}

// String returns a log-safe representation with credentials redacted.
func (c PoolConfig) String() string {
    // Parse DSN, replace user:password with ***:***, reassemble
}
```

### 4.3 Pool Functions

```go
// NewPool creates a *pgxpool.Pool and validates the connection (fail-fast).
// [UPDATED v2.0] Returns *pgxpool.Pool (pgx/v5), NOT *sql.DB.
func NewPool(ctx context.Context, cfg PoolConfig) (*pgxpool.Pool, error)

// Health checks database connectivity by executing SELECT 1.
func Health(ctx context.Context, pool *pgxpool.Pool) error

// Close drains the connection pool gracefully.
func Close(pool *pgxpool.Pool)
```

### 4.4 Migration Function

```go
// RunMigrations applies all pending SQL migrations from the given path.
// Uses golang-migrate/v4 with the pgx5 driver. Idempotent.
func RunMigrations(dsn string, migrationsPath string) error
```

### 4.5 `[UPDATED v2.0]` Wire Point — cmd/server/main.go

```go
// In cmd/server/main.go (composition root):
func main() {
    // ... existing setup ...

    // ── Persistence (P3) ────────────────────────────────────
    dbDSN := os.Getenv("LIWAISI_DB_DSN")
    if dbDSN == "" {
        logger.Error("LIWAISI_DB_DSN not set; persistence layer disabled")
        // Graceful degradation: continue with in-memory only
        // OR fail-fast depending on deployment mode
    }

    poolCfg := postgres.PoolConfig{DSN: dbDSN}
    pool, err := postgres.NewPool(ctx, poolCfg)
    if err != nil {
        logger.Error("failed to connect to Postgres", "error", err)
        os.Exit(1)
    }
    defer postgres.Close(pool)

    if err := postgres.RunMigrations(dbDSN, "store/postgres/migrations"); err != nil {
        logger.Error("failed to run migrations", "error", err)
        os.Exit(1)
    }

    // Pass pool to repository constructors (P4-P8)
    // e.g., eventRepo := postgres.NewEventRepository(pool)
}
```

**`[UPDATED v2.0]` Note:** The original spec referenced `cmd/cli/main.go` as the wire point. The actual composition root is `cmd/server/main.go` — this is where all driven adapters (LLM client, billing, and now Postgres pool) are constructed and injected.

### 4.6 `[UPDATED v2.0]` EventBatcher Driver Contract (for P4)

```go
// [UPDATED v2.0] EventBatcher in P4 MUST use *pgxpool.Pool, NOT *sql.DB.
// The v1.3 spec Section 10 incorrectly shows:
//   db *sql.DB    <-- WRONG
// The correct field is:
//   pool *pgxpool.Pool   <-- CORRECT
//
// pgxpool.Pool provides CopyFrom() for Postgres COPY protocol,
// which is the bulk insert mechanism EventBatcher uses.
// This is documented in P3 because P4 depends on P3's pool.
type EventBatcher struct {
    ch            chan *persist.EventRecord
    pool          *pgxpool.Pool   // [UPDATED v2.0] was *sql.DB in v1.3 spec
    batchSize     int             // default: 100
    flushInterval time.Duration   // default: 500ms
}
```

---

## 5. Acceptance Criteria

- **AC-01**: `NewPool` connects to Postgres and returns `*pgxpool.Pool` (fail-fast on connection failure)
- **AC-02**: `NewPool` returns error when DSN is empty
- **AC-03**: `PoolConfig.String()` redacts username and password from DSN
- **AC-04**: `Health(ctx, pool)` returns nil when Postgres is reachable
- **AC-05**: `Health(ctx, pool)` returns error when Postgres is unreachable
- **AC-06**: `Close(pool)` drains connections without panic
- **AC-07**: `RunMigrations` applies all 5 migrations (10 files) in order
- **AC-08**: `RunMigrations` is idempotent — second call is a no-op
- **AC-09**: DOWN migrations successfully reverse all UP migrations
- **AC-10**: `sessions` table exists with correct columns and indexes after migration 001
- **AC-11**: `messages` table exists with FK to `sessions` after migration 001
- **AC-12**: `events` table is range-partitioned by `timestamp` after migration 002
- **AC-13**: `token_ledger` table exists after migration 003
- **AC-14**: `flows` table exists with partial index (WHERE deleted_at IS NULL) after migration 004
- **AC-15**: `execution_records` table exists after migration 005
- **AC-16**: ALL timestamp columns use `TIMESTAMPTZ`
- **AC-17**: Integration tests use testcontainers-go with Postgres 16
- **AC-18**: `go build ./store/postgres/` succeeds
- **AC-19**: `go vet ./store/postgres/` passes
- **AC-20**: `golangci-lint run ./store/postgres/` passes
- **AC-21**: `go test -race -count=1 ./store/postgres/` — all pass (requires Docker)
- **AC-22**: `[UPDATED v2.0]` `docker-compose.yml` includes `postgres` service with health check
- **AC-23**: `[UPDATED v2.0]` `docker-compose.yml` includes `redis` service with health check
- **AC-24**: `[UPDATED v2.0]` `.env.example` includes `LIWAISI_DB_DSN` and `LIWAISI_REDIS_URL`
- **AC-25**: `[UPDATED v2.0]` No reference to `*sql.DB` or `database/sql` in any `store/postgres/` file
- **AC-26**: `[UPDATED v2.0]` Wire point documentation references `cmd/server/main.go`, not `cmd/cli/main.go`

---

## 6. Test Automation Strategy

### Test Files

| File | Tests | Framework |
|------|-------|-----------|
| `store/postgres/pool_test.go` | Pool lifecycle, health, close, config redaction | testcontainers-go + Go testing |
| `store/postgres/migrate_test.go` | Migration up, down, idempotency, schema verification | testcontainers-go + Go testing |

### Test Structure

| Test Function | Subtests | Count |
|---------------|----------|-------|
| `TestPoolConfig_String` | Redacts_credentials, Preserves_host_port_db | 2 |
| `TestNewPool` | Success, Empty_DSN, Unreachable_host, Cancelled_context | 4 |
| `TestHealth` | Healthy, Unhealthy_after_close | 2 |
| `TestClose` | Drains_gracefully, Double_close_no_panic | 2 |
| `TestRunMigrations` | All_up, Idempotent, Down_then_up | 3 |
| `TestMigration_001_Sessions` | Tables_exist, Columns_correct, Indexes_exist, FK_constraint | 4 |
| `TestMigration_002_Events` | Table_partitioned, Default_partition_exists, Indexes_exist | 3 |
| `TestMigration_003_TokenLedger` | Table_exists, Columns_correct, Index_exists | 3 |
| `TestMigration_004_Flows` | Table_exists, Partial_index_exists, Soft_delete_column | 3 |
| `TestMigration_005_ExecutionRecords` | Table_exists, Columns_correct, Indexes_exist | 3 |
| `TestMigration_Down` | Each_migration_down_reverses_up | 5 |
| **Total** | | **~34** |

### testcontainers-go Setup

```go
func setupPostgres(t *testing.T) (*pgxpool.Pool, string) {
    t.Helper()
    ctx := context.Background()

    container, err := postgres.RunContainer(ctx,
        testcontainers.WithImage("postgres:16-alpine"),
        postgres.WithDatabase("liwaisi_test"),
        postgres.WithUsername("test"),
        postgres.WithPassword("test"),
        testcontainers.WithWaitStrategy(
            wait.ForLog("database system is ready to accept connections").
                WithOccurrence(2).
                WithStartupTimeout(30*time.Second),
        ),
    )
    require.NoError(t, err)
    t.Cleanup(func() { container.Terminate(ctx) })

    dsn, err := container.ConnectionString(ctx, "sslmode=disable")
    require.NoError(t, err)

    pool, err := pgxpool.New(ctx, dsn)
    require.NoError(t, err)
    t.Cleanup(pool.Close)

    return pool, dsn
}
```

### Schema Verification Pattern

```go
func assertTableExists(t *testing.T, pool *pgxpool.Pool, tableName string) {
    t.Helper()
    var exists bool
    err := pool.QueryRow(context.Background(),
        "SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = $1)",
        tableName,
    ).Scan(&exists)
    require.NoError(t, err)
    require.True(t, exists, "table %s should exist", tableName)
}

func assertColumnType(t *testing.T, pool *pgxpool.Pool, table, column, expectedType string) {
    t.Helper()
    var dataType string
    err := pool.QueryRow(context.Background(),
        `SELECT data_type FROM information_schema.columns
         WHERE table_name = $1 AND column_name = $2`,
        table, column,
    ).Scan(&dataType)
    require.NoError(t, err)
    require.Equal(t, expectedType, dataType)
}
```

---

## 7. Rationale & Context

### Why pgxpool.Pool (Not database/sql)
`pgx/v5` is the only Postgres driver in this project's dependency tree (v1.3 spec Section 13). It provides: (a) native Postgres types without `Scan` boilerplate, (b) `CopyFrom` for bulk inserts (EventBatcher), (c) `LISTEN/NOTIFY` support, (d) connection pool with health checks built in. Using `database/sql` would require a separate driver wrapper and lose access to pgx-specific features.

### `[UPDATED v2.0]` Why EventBatcher Uses pgxpool.Pool
The v1.3 spec Section 10 incorrectly shows `*sql.DB` in the `EventBatcher` struct. This was an oversight — the rest of the spec exclusively uses pgx/v5. The `COPY` protocol for bulk inserts is accessed via `pgxpool.Pool.CopyFrom()`, which is a pgx-native feature not available through `database/sql`. Correcting this ensures driver consistency across the entire persistence layer.

### Why Fail-Fast NewPool
If Postgres is unreachable at startup, all persistence operations will fail. Detecting this immediately via `SELECT 1` prevents the server from accepting requests it cannot fulfill. This follows the "crash early" principle — a clear startup error is better than degraded runtime behavior.

### Why 5 Separate Migrations (Not 1)
Each migration is independently reversible and maps to a single domain concern. This allows: (a) partial rollback without losing all schema, (b) clear ownership — each migration corresponds to specific P-blocks, (c) easier code review — each file is focused.

### Why Monthly Partitioning for Events
Events are the highest-volume table (every CPN transition fires events). Monthly partitioning enables: (a) efficient time-range queries (partition pruning), (b) simple retention — DROP an entire partition instead of DELETE with WHERE, (c) vacuum runs on smaller tables.

### `[UPDATED v2.0]` Why Docker Compose Includes Redis in P3
Although Redis client code is part of P6, the infrastructure (Docker container) is added in P3 because: (a) developers need both services running for local development from P3 onward, (b) adding infrastructure incrementally across blocks creates setup friction, (c) the Redis service has no code dependency — it is just a container.

### `[UPDATED v2.0]` Why cmd/server/main.go (Not cmd/cli/main.go)
The project has a single entry point: `cmd/server/main.go`. This is the HTTP server composition root where all driven adapters are wired. There is no `cmd/cli/main.go` in the codebase. The original spec referenced the wrong path.

---

## 8. Dependencies & External Integrations

### Block Dependencies
- **DEP-01**: Block P1 (#55) — Schema matches DTOs (SessionRecord → sessions table, EventRecord → events table, etc.)
- **DEP-02**: Block 0 (#23) — Project skeleton (go.mod, Makefile, .golangci.yml, docker-compose.yml)

### Technology Dependencies
- **PLT-01**: Go 1.25
- **PLT-02**: `github.com/jackc/pgx/v5` — Postgres driver with native pooling
- **PLT-03**: `github.com/golang-migrate/migrate/v4` — SQL migration runner
- **PLT-04**: `github.com/testcontainers/testcontainers-go` — Integration test containers
- **PLT-05**: Docker — Required for testcontainers-go and docker-compose

### Infrastructure Dependencies
- **INF-01**: Postgres 16 — Target database version (matches Docker Compose service)
- **INF-02**: Redis 7 — `[UPDATED v2.0]` Infrastructure added in P3, client code in P6

### Depended On By
- **P4** — Postgres Event Repository + Batcher (uses pool + events migration)
- **P5** — Postgres Session + Message Repository (uses pool + sessions migration)
- **P6** — Redis Session Cache + HITL Repository (uses Redis infrastructure from P3)
- **P7** — Postgres Ledger + Intelligence Repositories (uses pool + ledger/execution_records migrations)
- **P8** — Postgres Flow Repository + Store Facade (uses pool + flows migration)

---

## 9. Examples & Edge Cases

### 9.1 Pool Lifecycle

```go
cfg := postgres.PoolConfig{
    DSN:     os.Getenv("LIWAISI_DB_DSN"),
    MaxConns: 20,
    MinConns: 5,
}
fmt.Println(cfg) // "postgres://***:***@localhost:5432/liwaisi?sslmode=disable"

pool, err := postgres.NewPool(ctx, cfg)
if err != nil {
    log.Fatal("postgres unreachable:", err) // fail-fast
}
defer postgres.Close(pool)

// Health probe (used by /api/v1/health endpoint)
if err := postgres.Health(ctx, pool); err != nil {
    log.Warn("postgres unhealthy", "error", err)
}
```

### 9.2 Running Migrations

```go
dsn := os.Getenv("LIWAISI_DB_DSN")
if err := postgres.RunMigrations(dsn, "store/postgres/migrations"); err != nil {
    log.Fatal("migration failed:", err)
}
// Safe to call again — idempotent
if err := postgres.RunMigrations(dsn, "store/postgres/migrations"); err != nil {
    log.Fatal("unexpected:", err) // should NOT happen
}
```

### 9.3 `[UPDATED v2.0]` Docker Compose Usage

```bash
# Start all infrastructure
docker compose up -d postgres redis

# Wait for health checks
docker compose ps  # both should show "healthy"

# Run backend with persistence
LIWAISI_DB_DSN=postgres://liwaisi:liwaisi_dev@localhost:5432/liwaisi?sslmode=disable \
  go run ./cmd/server/
```

### 9.4 Edge Cases

| Edge Case | Expected Behavior |
|-----------|-------------------|
| Empty DSN | `NewPool` returns error immediately |
| Invalid DSN format | `NewPool` returns parse error |
| Postgres unreachable | `NewPool` returns connection error (fail-fast) |
| DSN with special characters in password | `PoolConfig.String()` still redacts correctly |
| `RunMigrations` on empty database | Applies all 5 migrations in order |
| `RunMigrations` on fully migrated database | No-op, returns nil |
| `RunMigrations` with partial migrations | Applies only pending migrations |
| DOWN migration on empty database | No error (IF EXISTS guards) |
| `Health` after `Close` | Returns error (pool is closed) |
| `Close` called twice | No panic (pgxpool.Close is idempotent) |
| Concurrent `Health` calls | Safe — `pgxpool.Pool` is goroutine-safe |
| Events insert without monthly partition | Lands in `events_default` partition |
| `sessions` FK violation (message with invalid session_id) | Postgres returns FK error |
| `flows` partial index query | Only non-deleted flows returned by index scan |

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

# 4. Tests pass with race detector (requires Docker)
go test -race -count=1 -v ./store/postgres/...

# 5. Coverage check
go test -race -coverprofile=postgres_coverage.out ./store/postgres/...
go tool cover -func=postgres_coverage.out | grep -E "(pool|migrate)\.go"

# 6. [v2.0] No database/sql imports
! grep -rn "database/sql" store/postgres/

# 7. Migration files exist (10 files: 5 up + 5 down)
ls store/postgres/migrations/*.sql | wc -l  # should be 10

# 8. All timestamps use TIMESTAMPTZ
! grep -i "TIMESTAMP[^T]" store/postgres/migrations/*.up.sql  # no bare TIMESTAMP

# 9. [v2.0] Docker Compose has postgres and redis services
grep -c "postgres:16" docker-compose.yml   # should be 1
grep -c "redis:7" docker-compose.yml       # should be 1

# 10. [v2.0] .env.example has new vars
grep "LIWAISI_DB_DSN" .env.example
grep "LIWAISI_REDIS_URL" .env.example

# 11. [v2.0] Wire point is cmd/server/main.go
grep "LIWAISI_DB_DSN" cmd/server/main.go

# 12. Events table is partitioned
grep -i "PARTITION BY RANGE" store/postgres/migrations/002_events.up.sql
```

---

## 11. Related Specifications / Further Reading

- [Agentic CPN v1.3 — Persistence Layer](../agentic-cpn-v1.3.md) — Sections 7, 10, 11, 13, 14
- [Block P1: Repository Interfaces](block-p1-repository-interfaces.md) — DTOs that map to schema
- [Block P2: CPN Topology Serialization](block-p2-cpn-topology-serialization.md) — FuncRegistry (stored in flows.function_mapping)
- [Block P4: Event Repository + Batcher](block-p4-event-repository-batcher.md) — Consumer of pool + events migration + EventBatcher (uses pgxpool.Pool per v2.0)
- [Block P5: Session + Message Repository](block-p5-postgres-session-message.md) — Consumer of pool + sessions migration
- [Block P6: Redis Session Cache + HITL](block-p6-redis-session-cache-hitl.md) — Consumer of Redis infrastructure added in P3
- [Block P7: Ledger + Intelligence Repositories](block-p7-postgres-ledger-intelligence.md) — Consumer of pool + ledger/execution_records migrations
- [Block P8: Flow Repository + Store Facade](block-p8-flow-repository-store-facade.md) — Consumer of pool + flows migration

---

## Appendix A: Change Log from v1.0

| Change | Reason | Impact |
|--------|--------|--------|
| `[UPDATED v2.0]` EventBatcher uses `*pgxpool.Pool`, not `*sql.DB` | v1.3 spec Section 10 inconsistency — all other code uses pgx/v5 | P3 (documentation), P4 (implementation) |
| `[UPDATED v2.0]` Docker Compose adds Postgres 16 + Redis 7 services | Current docker-compose.yml has no database services; developers need them for local dev | P3 (infrastructure), all downstream blocks |
| `[UPDATED v2.0]` Wire point corrected to `cmd/server/main.go` | Original spec said `cmd/cli/main.go` which does not exist; actual composition root is `cmd/server/main.go` | P3 (documentation), wire code |
| `[UPDATED v2.0]` `.env.example` updated with `LIWAISI_DB_DSN`, `LIWAISI_REDIS_URL` | New environment variables needed for persistence layer | P3 (infrastructure), developer onboarding |
| `[UPDATED v2.0]` Added REQ-25 documenting pgxpool.Pool for EventBatcher | Cross-block concern: P3 documents the contract P4 must follow | P4 |

---

## Implementation File Checklist

- [ ] `back/go-assistant/store/postgres/pool.go` — PoolConfig, NewPool, Health, Close
- [ ] `back/go-assistant/store/postgres/migrate.go` — RunMigrations
- [ ] `back/go-assistant/store/postgres/migrations/001_sessions.up.sql` — sessions + messages tables
- [ ] `back/go-assistant/store/postgres/migrations/001_sessions.down.sql` — DROP sessions + messages
- [ ] `back/go-assistant/store/postgres/migrations/002_events.up.sql` — events partitioned table
- [ ] `back/go-assistant/store/postgres/migrations/002_events.down.sql` — DROP events
- [ ] `back/go-assistant/store/postgres/migrations/003_token_ledger.up.sql` — token_ledger table
- [ ] `back/go-assistant/store/postgres/migrations/003_token_ledger.down.sql` — DROP token_ledger
- [ ] `back/go-assistant/store/postgres/migrations/004_flows.up.sql` — flows table
- [ ] `back/go-assistant/store/postgres/migrations/004_flows.down.sql` — DROP flows
- [ ] `back/go-assistant/store/postgres/migrations/005_execution_records.up.sql` — execution_records table
- [ ] `back/go-assistant/store/postgres/migrations/005_execution_records.down.sql` — DROP execution_records
- [ ] `back/go-assistant/store/postgres/pool_test.go` — Pool integration tests (testcontainers-go)
- [ ] `back/go-assistant/store/postgres/migrate_test.go` — Migration integration tests (testcontainers-go)
- [ ] `docker-compose.yml` — `[UPDATED v2.0]` Add postgres + redis services, named volumes
- [ ] `.env.example` — `[UPDATED v2.0]` Add LIWAISI_DB_DSN, POSTGRES_PASSWORD, LIWAISI_REDIS_URL
- [ ] `back/go-assistant/cmd/server/main.go` — `[UPDATED v2.0]` Wire pool + migrations (composition root)
- [ ] `back/go-assistant/go.mod` — Add pgx/v5, golang-migrate/v4, testcontainers-go
- [ ] Run `go build ./store/postgres/` — compiles
- [ ] Run `go vet ./store/postgres/` — passes
- [ ] Run `golangci-lint run ./store/postgres/` — zero findings
- [ ] Run `go test -race -count=1 ./store/postgres/` — all pass (requires Docker)
- [ ] Verify: No `database/sql` imports in `store/postgres/`
- [ ] Verify: All timestamps use TIMESTAMPTZ
- [ ] Verify: Events table is partitioned
- [ ] Verify: PoolConfig.String() redacts credentials
- [ ] Verify: RunMigrations is idempotent
- [ ] Verify: DOWN migrations reverse UP migrations
- [ ] Verify: Docker Compose postgres + redis services start and pass health checks
- [ ] Verify: `.env.example` has LIWAISI_DB_DSN and LIWAISI_REDIS_URL
