---
title: "Block P7 — Postgres Ledger + Intelligence Repositories (Updated)"
version: 2.0
date_created: 2026-03-27
last_updated: 2026-04-01
owner: Agentic CPN Team
tags: golang, persistence, postgres, ledger, intelligence, analytics, spec-driven, block-P7
---

# Introduction

Block P7 implements two Postgres-backed repositories: `PostgresLedgerRepository` (token/cost tracking with upsert semantics) and `PostgresIntelligenceRepository` (execution metrics for Flow Intelligence ranking). Together they provide the durable analytics backend for billing reconciliation and CPN optimization.

This is **version 2.0** of the P7 specification, updated after an expert panel audit (2026-04-01) that identified three gaps: (1) the `persist.ExecutionRecord` DTO (updated in P1 v2.0) has additional fields (`CPNDepth`, `StartedAt`, `CompletedAt`, `TokensProduced`, `ToolCalls`) that must be reflected in the SQL schema, (2) the connection between `MetricsRecorder` (in-memory) and `IntelligenceRepository` (Postgres) was undocumented, and (3) the `execution_records` table was missing a `tool_calls` column.

**Spec reference:** `.docs/specs/agentic-cpn-v1.3.md` Sections 4.3, 4.5, 5, 7, 9, 12

**GitHub Issue:** #61

**Specialist Team:**
- **Database Architect**: Schema design, upsert SQL, aggregation queries, indexing for TopFlows ranking
- **Senior Golang Engineer**: Repository implementation, pgx usage, context propagation, DTO alignment
- **Software Architect**: MetricsRecorder-to-IntelligenceRepository bridge pattern, batch persistence strategy

**Depends on:** P1 (#55), P3 (#57)

---

## 1. Purpose & Scope

### Purpose
Provide durable Postgres-backed implementations of `persist.LedgerRepository` (5 methods) and `persist.IntelligenceRepository` (4 methods) with ACID guarantees, efficient upsert for token accounting, and aggregation queries for Flow Intelligence ranking.

### Scope
- **In scope**: `PostgresLedgerRepository`, `PostgresIntelligenceRepository`, `token_ledger` table schema, `execution_records` table schema, upsert SQL for ledger, aggregation queries for intelligence, MetricsRecorder bridge pattern documentation, `testcontainers-go` integration tests
- **Out of scope**: In-memory implementations (P1), Flow Repository (P8), Redis cache (P6), session storage (P5), event storage (P4)

### Audience
Implementers of Block P7 and downstream block P8 (Store facade wiring MetricsRecorder to IntelligenceRepository).

---

## 2. Definitions

| Term | Definition |
|------|-----------|
| **PostgresLedgerRepository** | Concrete implementation of `persist.LedgerRepository` backed by PostgreSQL — tracks tokens, costs, and daily totals per session |
| **PostgresIntelligenceRepository** | Concrete implementation of `persist.IntelligenceRepository` backed by PostgreSQL — stores execution metrics and computes Flow Intelligence rankings |
| **Upsert** | INSERT on first call, UPDATE (increment) on subsequent calls for the same session — used by `LedgerRepository.Record` |
| **ExecutionRecord** | Persistence DTO for a single CPN execution's metrics — `[v2.0]` includes CPNDepth, StartedAt, CompletedAt, TokensProduced, ToolCalls |
| **MetricsRecorder** | In-memory append-only store of `cpn.ExecutionRecord` values, populated by `executionTracker.Finalize()` at the end of each `CPN.Run()` |
| **MetricsRecorder bridge** | `[v2.0]` Integration pattern connecting `MetricsRecorder.Append()` (in-memory) to `IntelligenceRepository.RecordExecution()` (Postgres) |
| **Flow Intelligence** | Analytics system that ranks CPN topologies by performance (success rate, cost, duration) to recommend optimal flows |
| **TopFlows** | Query returning the top-N CPN flows ranked by a composite score |
| **Testcontainers** | `testcontainers-go` for ephemeral Postgres containers in integration tests |

---

## 3. Requirements, Constraints & Guidelines

### Requirements

#### Package Structure
- **REQ-001**: Implementation files: `store/postgres/ledger.go`, `store/postgres/intelligence.go` with `package postgres`
- **REQ-002**: Test files: `store/postgres/ledger_test.go`, `store/postgres/intelligence_test.go` with `package postgres_test`

#### PostgresLedgerRepository (5 methods)
- **REQ-003**: `Record(ctx, *LedgerRecord) error` — Upsert into `token_ledger`. First call for a session inserts. Subsequent calls increment `input_tokens`, `output_tokens`, `calls`, and `total_cost_usd`. Updates `last_updated = NOW()`. Returns `persist.ErrInvalidInput` if `SessionID` is empty.
- **REQ-004**: `GetBySession(ctx, sessionID) (*LedgerRecord, error)` — SELECT from `token_ledger` WHERE `session_id = $1`. Returns `persist.ErrLedgerNotFound` if no row found.
- **REQ-005**: `QueryByDate(ctx, date time.Time) ([]*LedgerRecord, error)` — SELECT from `token_ledger` WHERE `last_updated::date = $1::date`. Returns empty slice if none found.
- **REQ-006**: `SetDailyTotal(ctx, sessionID, date, totalUSD) error` — UPDATE `token_ledger` SET `daily_total_usd = $3` WHERE `session_id = $1`. Returns `persist.ErrLedgerNotFound` if no row found.
- **REQ-007**: `AggregateByDateRange(ctx, from, to) (*LedgerAggregate, error)` — SELECT SUM(input_tokens), SUM(output_tokens), SUM(calls), SUM(total_cost_usd) FROM `token_ledger` WHERE `last_updated BETWEEN $1 AND $2`. Returns zero-valued aggregate if no rows match.

#### PostgresIntelligenceRepository (4 methods)
- **REQ-008**: `RecordExecution(ctx, *ExecutionRecord) error` — INSERT into `execution_records`. The `ID` field is generated at the persistence layer if empty (UUID v4). Returns `persist.ErrInvalidInput` if `CPNID` is empty.
- **REQ-009**: `QueryByRole(ctx, role, from, to) ([]*ExecutionRecord, error)` — SELECT from `execution_records` WHERE `cpn_role = $1 AND started_at BETWEEN $2 AND $3` ORDER BY `started_at DESC`. Returns empty slice if none found.
- **REQ-010**: `Aggregate(ctx, role, from, to) (*RankingMetrics, error)` — SELECT aggregations (AVG, COUNT, SUM) from `execution_records` WHERE `cpn_role = $1 AND started_at BETWEEN $2 AND $3`. Returns zero-valued metrics if no rows match.
- **REQ-011**: `TopFlows(ctx, n) ([]*RankedFlow, error)` — JOIN `execution_records` with `flows` (P8) to compute a composite ranking score. Returns top-N flows ordered by score descending. Returns empty slice if no flows exist.

#### `[UPDATED v2.0]` ExecutionRecord DTO Alignment — SQL Schema
- **REQ-012**: The `execution_records` table MUST include ALL fields from `persist.ExecutionRecord` (as updated in P1 v2.0):
  ```sql
  CREATE TABLE IF NOT EXISTS execution_records (
      id                TEXT PRIMARY KEY,
      cpn_id            TEXT NOT NULL,
      cpn_role          TEXT NOT NULL,
      cpn_depth         INT NOT NULL DEFAULT 0,
      session_id        TEXT NOT NULL,
      transitions_fired INT NOT NULL DEFAULT 0,
      llm_calls         INT NOT NULL DEFAULT 0,
      tool_calls        INT NOT NULL DEFAULT 0,
      tokens_produced   INT NOT NULL DEFAULT 0,
      total_cost_usd    DOUBLE PRECISION NOT NULL DEFAULT 0,
      duration_ms       BIGINT NOT NULL DEFAULT 0,
      success           BOOLEAN NOT NULL DEFAULT false,
      started_at        TIMESTAMPTZ NOT NULL,
      completed_at      TIMESTAMPTZ NOT NULL
  );
  CREATE INDEX idx_execution_records_cpn_role ON execution_records (cpn_role);
  CREATE INDEX idx_execution_records_session ON execution_records (session_id);
  CREATE INDEX idx_execution_records_started ON execution_records (started_at);
  CREATE INDEX idx_execution_records_role_time ON execution_records (cpn_role, started_at);
  ```
- **REQ-013**: `[v2.0]` The `tool_calls` column (INT) stores the count of tool transitions fired during execution. This value comes from `persist.ExecutionRecord.ToolCalls`, which is populated by the integration layer from `cpn.ExecutionRecord` (requires P1 REQ-030 prerequisite: `toolCallCount` in `executionTracker`).
- **REQ-014**: `[v2.0]` The `cpn_depth` column (INT) stores the CPN hierarchy depth (0=root, 1=domain, 2+=worker). Used for hierarchy-aware analytics queries.
- **REQ-015**: `[v2.0]` The `tokens_produced` column (INT) stores the number of tokens produced by the CPN during execution. Used for throughput analytics.
- **REQ-016**: `[v2.0]` `started_at` and `completed_at` replace the original single `timestamp` column from the v1.3 spec. Both are `TIMESTAMPTZ NOT NULL`. `duration_ms` is a derived field (computed from `CompletedAt - StartedAt` at the application layer, stored for query efficiency).

#### Token Ledger Schema
- **REQ-017**: `token_ledger` table:
  ```sql
  CREATE TABLE IF NOT EXISTS token_ledger (
      session_id      TEXT PRIMARY KEY,
      input_tokens    BIGINT NOT NULL DEFAULT 0,
      output_tokens   BIGINT NOT NULL DEFAULT 0,
      calls           BIGINT NOT NULL DEFAULT 0,
      total_cost_usd  DOUBLE PRECISION NOT NULL DEFAULT 0,
      daily_total_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
      last_updated    TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );
  CREATE INDEX idx_token_ledger_last_updated ON token_ledger (last_updated);
  ```

#### Upsert SQL for Ledger.Record
- **REQ-018**: Use Postgres `INSERT ... ON CONFLICT (session_id) DO UPDATE SET`:
  ```sql
  INSERT INTO token_ledger (session_id, input_tokens, output_tokens, calls, total_cost_usd, last_updated)
  VALUES ($1, $2, $3, $4, $5, NOW())
  ON CONFLICT (session_id) DO UPDATE SET
      input_tokens   = token_ledger.input_tokens   + EXCLUDED.input_tokens,
      output_tokens  = token_ledger.output_tokens  + EXCLUDED.output_tokens,
      calls          = token_ledger.calls           + EXCLUDED.calls,
      total_cost_usd = token_ledger.total_cost_usd + EXCLUDED.total_cost_usd,
      last_updated   = NOW();
  ```

#### `[UPDATED v2.0]` MetricsRecorder to IntelligenceRepository Bridge
- **REQ-019**: The `MetricsRecorder` (defined in `cpn/flow_metrics.go`) is the in-memory recording point for execution metrics. It receives `cpn.ExecutionRecord` values via `Append(rec)` at the end of each `CPN.Run()`.
- **REQ-020**: The integration layer MUST bridge `MetricsRecorder` to `IntelligenceRepository` for durable persistence. Two options were evaluated:
  - **Option 1 (Wrapper)**: Wrap `MetricsRecorder` to also call `IntelligenceRepository.RecordExecution` on each `Append`. Pros: real-time persistence. Cons: adds latency to the hot path, requires error handling on write.
  - **Option 2 (Batch after Run)**: The integration layer (SessionService or P8 Store facade) reads `MetricsRecorder.Records()` after each `CPN.Run()` completes and persists them to `IntelligenceRepository`. Pros: decoupled, batch-friendly, no hot-path latency. Cons: metrics lost if process crashes between Run completion and persistence.
- **REQ-021**: **Recommended: Option 2 (Batch after Run)** — The integration layer calls `MetricsRecorder.Records()` after `CPN.Run()` returns, converts each `cpn.ExecutionRecord` to `persist.ExecutionRecord`, and calls `IntelligenceRepository.RecordExecution()` for each. This is simpler and avoids coupling the CPN hot path to Postgres latency.
- **REQ-022**: The conversion from `cpn.ExecutionRecord` to `persist.ExecutionRecord` includes:
  ```go
  func executionToRecord(rec *cpn.ExecutionRecord) *persist.ExecutionRecord {
      return &persist.ExecutionRecord{
          ID:               uuid.NewString(), // generated at persistence layer
          CPNID:            rec.CPNID,
          CPNRole:          rec.CPNRole,
          CPNDepth:         rec.CPNDepth,
          SessionID:        rec.SessionID,
          TransitionsFired: rec.TransitionsFired,
          LLMCalls:         rec.LLMCallCount,
          ToolCalls:        0, // TODO: requires P1 REQ-030 prerequisite
          TokensProduced:   rec.TokensProduced,
          TotalCostUSD:     rec.TotalCostUSD,
          DurationMs:       rec.Duration.Milliseconds(),
          Success:          rec.Success,
          StartedAt:        rec.StartedAt,
          CompletedAt:      rec.CompletedAt,
      }
  }
  ```
- **REQ-023**: This bridge logic lives in the integration layer (P8 Store facade or SessionService), NOT in P7. P7 implements the raw `IntelligenceRepository` methods.

#### Migrations
- **REQ-024**: Migration `003_ledger.up.sql` creates `token_ledger` table (as per v1.3 Section 7).
- **REQ-025**: Migration `005_execution_records.up.sql` creates `execution_records` table (as per v1.3 Section 7), `[v2.0]` with the updated column set.

#### Constructor
- **REQ-026**: `NewPostgresLedgerRepository(pool *pgxpool.Pool) *PostgresLedgerRepository`
- **REQ-027**: `NewPostgresIntelligenceRepository(pool *pgxpool.Pool) *PostgresIntelligenceRepository`

### Security Requirements
- **SEC-001**: All SQL uses parameterized queries — no string interpolation
- **SEC-002**: Connection pool from P3 uses `sslmode=verify-full`

### Constraints
- **CON-001**: Depends on `pgx/v5` and `pgxpool` (introduced by P3)
- **CON-002**: Does NOT import `cpn/` package — operates purely on `persist` types
- **CON-003**: `TopFlows` query may JOIN with `flows` table (P8) — if `flows` table does not yet exist, the query should return an empty result, not an error
- **CON-004**: Migration files are managed by P3 (`golang-migrate/migrate`)

### Guidelines
- **GUD-001**: Use `pgx.RowToStructByName` or manual `Scan` for row mapping — avoid ORMs
- **GUD-002**: `Record` upsert MUST use `ON CONFLICT DO UPDATE` — not a read-modify-write cycle (race-safe)
- **GUD-003**: Aggregation queries use `COALESCE` for null handling (e.g., `COALESCE(SUM(input_tokens), 0)`)
- **GUD-004**: `TopFlows` ranking score: `score = success_rate * 0.4 + (1 / avg_cost_usd) * 0.3 + (1 / avg_duration_ms) * 0.3` — configurable weights in future
- **GUD-005**: `[v2.0]` Document that `ToolCalls` will be zero until P1 REQ-030 (toolCallCount in executionTracker) is implemented

---

## 4. Interfaces & Data Contracts

### 4.1 Package Layout

```
store/postgres/
├── pool.go            — Connection pool (P3)
├── ledger.go          — PostgresLedgerRepository (this block)
├── ledger_test.go     — Integration tests
├── intelligence.go    — PostgresIntelligenceRepository (this block)
├── intelligence_test.go — Integration tests
└── migrations/
    ├── 003_ledger.up.sql          — token_ledger table
    ├── 003_ledger.down.sql        — DROP token_ledger
    ├── 005_execution_records.up.sql — execution_records table [UPDATED v2.0]
    └── 005_execution_records.down.sql — DROP execution_records
```

### 4.2 PostgresLedgerRepository

```go
// PostgresLedgerRepository implements persist.LedgerRepository using PostgreSQL.
// Uses ON CONFLICT DO UPDATE for atomic upsert semantics on Record().
type PostgresLedgerRepository struct {
    pool *pgxpool.Pool
}

var _ persist.LedgerRepository = (*PostgresLedgerRepository)(nil)

func NewPostgresLedgerRepository(pool *pgxpool.Pool) *PostgresLedgerRepository
```

### 4.3 PostgresIntelligenceRepository

```go
// PostgresIntelligenceRepository implements persist.IntelligenceRepository
// using PostgreSQL. Stores per-execution metrics and computes aggregate
// rankings for Flow Intelligence.
//
// [v2.0] The execution_records table includes: cpn_depth, started_at,
// completed_at, tokens_produced, tool_calls — aligned with persist.ExecutionRecord.
type PostgresIntelligenceRepository struct {
    pool *pgxpool.Pool
}

var _ persist.IntelligenceRepository = (*PostgresIntelligenceRepository)(nil)

func NewPostgresIntelligenceRepository(pool *pgxpool.Pool) *PostgresIntelligenceRepository
```

### 4.4 `[v2.0]` MetricsRecorder Bridge — Data Flow

```
  CPN Engine (cpn/)
  ┌─────────────────────────────────────┐
  │ CPN.Run() completes                 │
  │  └─ executionTracker.Finalize()     │
  │      └─ cpn.ExecutionRecord {       │
  │           CPNID, CPNRole, CPNDepth, │
  │           SessionID,                │
  │           TransitionsFired,         │
  │           LLMCallCount,             │
  │           TokensProduced,           │
  │           TotalCostUSD,             │
  │           Duration,                 │
  │           Success,                  │
  │           StartedAt, CompletedAt    │
  │         }                           │
  │  └─ MetricsRecorder.Append(&rec)   │
  └──────────┬──────────────────────────┘
             │ Run() returns
             ▼
  Integration Layer (P8 / SessionService)
  ┌─────────────────────────────────────┐
  │ After CPN.Run() completes:          │
  │  records := metricsRecorder.Records │
  │  for _, rec := range records {      │
  │    pRec := executionToRecord(&rec)  │
  │    intellRepo.RecordExecution(      │
  │      ctx, pRec)                     │
  │  }                                  │
  └──────────┬──────────────────────────┘
             │
             ▼
  PostgresIntelligenceRepository (P7)
  ┌─────────────────────────────────────┐
  │ INSERT INTO execution_records ...   │
  └─────────────────────────────────────┘
```

**Key invariant**: P7 implements the raw SQL operations. The conversion from `cpn.ExecutionRecord` to `persist.ExecutionRecord` and the batch persistence logic live in the integration layer (P8 or SessionService).

### 4.5 `[v2.0]` Updated execution_records Columns

| Column | Type | Source Field | Notes |
|--------|------|-------------|-------|
| `id` | TEXT PK | Generated (UUID) | At persistence layer |
| `cpn_id` | TEXT NOT NULL | `CPNID` | |
| `cpn_role` | TEXT NOT NULL | `CPNRole` | |
| `cpn_depth` | INT NOT NULL | `CPNDepth` | `[NEW v2.0]` Hierarchy depth |
| `session_id` | TEXT NOT NULL | `SessionID` | |
| `transitions_fired` | INT NOT NULL | `TransitionsFired` | |
| `llm_calls` | INT NOT NULL | `LLMCalls` | |
| `tool_calls` | INT NOT NULL | `ToolCalls` | `[NEW v2.0]` Requires P1 REQ-030 |
| `tokens_produced` | INT NOT NULL | `TokensProduced` | `[NEW v2.0]` |
| `total_cost_usd` | DOUBLE PRECISION | `TotalCostUSD` | |
| `duration_ms` | BIGINT NOT NULL | `DurationMs` | Converted from Duration |
| `success` | BOOLEAN NOT NULL | `Success` | |
| `started_at` | TIMESTAMPTZ NOT NULL | `StartedAt` | `[NEW v2.0]` Replaces `timestamp` |
| `completed_at` | TIMESTAMPTZ NOT NULL | `CompletedAt` | `[NEW v2.0]` Replaces `timestamp` |

### 4.6 SQL Queries (Reference)

| Method | SQL | Notes |
|--------|-----|-------|
| Ledger.Record | `INSERT ... ON CONFLICT DO UPDATE SET` (see REQ-018) | Atomic upsert |
| Ledger.GetBySession | `SELECT ... FROM token_ledger WHERE session_id = $1` | Single row |
| Ledger.QueryByDate | `SELECT ... FROM token_ledger WHERE last_updated::date = $1::date` | Multi-row |
| Ledger.SetDailyTotal | `UPDATE token_ledger SET daily_total_usd = $2 WHERE session_id = $1` | Single column |
| Ledger.AggregateByDateRange | `SELECT COALESCE(SUM(...), 0) FROM token_ledger WHERE last_updated BETWEEN $1 AND $2` | Aggregation |
| Intelligence.RecordExecution | `INSERT INTO execution_records (id, cpn_id, cpn_role, cpn_depth, session_id, transitions_fired, llm_calls, tool_calls, tokens_produced, total_cost_usd, duration_ms, success, started_at, completed_at) VALUES ($1..$14)` | `[v2.0]` 14 params |
| Intelligence.QueryByRole | `SELECT ... FROM execution_records WHERE cpn_role = $1 AND started_at BETWEEN $2 AND $3 ORDER BY started_at DESC` | Multi-row |
| Intelligence.Aggregate | `SELECT cpn_role, AVG(llm_calls), AVG(tool_calls), AVG(total_cost_usd), AVG(duration_ms), AVG(CASE WHEN success THEN 1.0 ELSE 0.0 END), COUNT(*) FROM execution_records WHERE cpn_role = $1 AND started_at BETWEEN $2 AND $3 GROUP BY cpn_role` | Aggregation |
| Intelligence.TopFlows | `SELECT f.hash, f.role, ... JOIN execution_records e ON ... GROUP BY f.hash ORDER BY score DESC LIMIT $1` | JOIN with flows table |

---

## 5. Acceptance Criteria

- **AC-001**: `go build ./store/postgres/` succeeds
- **AC-002**: `go vet ./store/postgres/` passes
- **AC-003**: `golangci-lint run ./store/postgres/` passes
- **AC-004**: `go test -race -count=1 ./store/postgres/ -run TestPostgresLedger` — all pass
- **AC-005**: `go test -race -count=1 ./store/postgres/ -run TestPostgresIntelligence` — all pass
- **AC-006**: `var _ persist.LedgerRepository = (*PostgresLedgerRepository)(nil)` compiles
- **AC-007**: `var _ persist.IntelligenceRepository = (*PostgresIntelligenceRepository)(nil)` compiles
- **AC-008**: `Ledger.Record` upserts (increments on second call for same session)
- **AC-009**: `Ledger.AggregateByDateRange` returns zero-valued aggregate for empty range
- **AC-010**: `Intelligence.RecordExecution` generates UUID if ID is empty
- **AC-011**: `Intelligence.TopFlows` returns empty slice if no flows exist
- **AC-012**: `Intelligence.Aggregate` returns zero-valued metrics for empty range
- **AC-013**: All SQL is parameterized
- **AC-014**: `[v2.0]` `execution_records` table has `cpn_depth`, `tool_calls`, `tokens_produced`, `started_at`, `completed_at` columns
- **AC-015**: `[v2.0]` `RecordExecution` correctly persists all 14 fields including new v2.0 columns
- **AC-016**: `[v2.0]` `QueryByRole` returns records with `StartedAt` and `CompletedAt` populated
- **AC-017**: `[v2.0]` Repository has NO imports from `cpn/` package (only `persist` types)

---

## 6. Test Automation Strategy

### Test Files: `store/postgres/ledger_test.go`, `store/postgres/intelligence_test.go`

### Framework
- `testcontainers-go` for ephemeral Postgres container per test suite
- Standard Go `testing` package
- `pgx/v5` for direct DB verification queries
- `golang-migrate/migrate` for schema setup in `TestMain`

### Test Structure

| Test Function | Subtests | Count |
|---------------|----------|-------|
| **Ledger Tests** | | |
| `TestPostgresLedgerRepository_Record` | FirstInsert, UpsertIncrements, EmptySessionID | 3 |
| `TestPostgresLedgerRepository_GetBySession` | Success, NotFound | 2 |
| `TestPostgresLedgerRepository_QueryByDate` | HasResults, NoResults | 2 |
| `TestPostgresLedgerRepository_SetDailyTotal` | Success, NotFound | 2 |
| `TestPostgresLedgerRepository_AggregateByDateRange` | HasData, EmptyRange | 2 |
| **Intelligence Tests** | | |
| `TestPostgresIntelligenceRepository_RecordExecution` | Success, EmptyCPNID, GeneratesID, AllFieldsPersisted | 4 |
| `TestPostgresIntelligenceRepository_QueryByRole` | HasResults, NoResults, OrderedByStartedAt | 3 |
| `TestPostgresIntelligenceRepository_Aggregate` | HasData, EmptyRange | 2 |
| `TestPostgresIntelligenceRepository_TopFlows` | HasFlows, NoFlows, RankingOrder | 3 |
| `TestPostgresIntelligenceRepository_V2Fields` | `[v2.0]` CPNDepth, ToolCalls, TokensProduced, StartedAt, CompletedAt | 5 |
| `TestPostgresLedgerRepository_ContextCancellation` | CancelledContext | 1 |
| `TestPostgresIntelligenceRepository_ContextCancellation` | CancelledContext | 1 |
| **Total** | | **~30** |

### TestMain Setup

```go
func TestMain(m *testing.M) {
    // 1. Start Postgres testcontainer
    // 2. Run migrations (003_ledger.up.sql, 005_execution_records.up.sql)
    // 3. Create pgxpool.Pool
    // 4. Run tests
    // 5. Teardown container
}
```

---

## 7. Rationale & Context

### Why Postgres for Ledger
Token ledger data is financial in nature (billing reconciliation). Postgres provides ACID guarantees, efficient upsert with `ON CONFLICT`, and date-range aggregation queries. The data has a 90-day retention policy.

### Why Upsert for Ledger.Record
A CPN execution may produce multiple token counts (LLM calls, tool calls). Rather than require the caller to check-then-insert, the upsert atomically handles both first-write and subsequent-increment cases. This is race-safe under concurrent access.

### Why Postgres for Intelligence
Execution records power Flow Intelligence ranking — aggregate queries (AVG, COUNT, SUM with GROUP BY) that Postgres handles efficiently with proper indexing. The 90-day trailing window bounds data volume.

### `[v2.0]` Why Additional ExecutionRecord Columns
The `cpn.ExecutionRecord` (in `cpn/flow_metrics.go`) has always had `CPNDepth`, `StartedAt`, `CompletedAt`, and `TokensProduced` fields. The original P7 v1.0 spec did not include them in the SQL schema, creating a lossy persistence layer. The v2.0 update ensures 1:1 field coverage between the DTO and the table.

### `[v2.0]` Why ToolCalls Column
Tool call frequency is a key metric for CPN optimization — high tool-call CPNs may benefit from caching or parallelization. The `tool_calls` column stores this count. Note: the prerequisite (P1 REQ-030: `toolCallCount` in `executionTracker`) must be implemented first. Until then, `ToolCalls` will be 0 in stored records.

### `[v2.0]` Why Batch After Run (MetricsRecorder Bridge)
Option 2 (batch after Run) was chosen over Option 1 (wrapper) because:
1. **No hot-path latency**: Postgres writes during `CPN.Run()` would add latency to the firing loop
2. **Simpler error handling**: A failed Postgres write during Run would need to be non-fatal, complicating the wrapper
3. **Natural batch point**: `Run()` returns → `MetricsRecorder.Records()` gives all records for the execution → persist them in a single transaction
4. **Acceptable loss window**: If the process crashes between Run completion and persistence, the in-memory records are lost. This is acceptable for analytics data (not transactional).

---

## 8. Dependencies & External Integrations

### Block Dependencies
- **DEP-001**: P1 (#55) — `persist.LedgerRepository`, `persist.IntelligenceRepository` interfaces, `persist.LedgerRecord`, `persist.ExecutionRecord`, `persist.RankingMetrics`, `persist.RankedFlow`, sentinel errors
- **DEP-002**: P3 (#57) — `pgxpool.Pool`, migration infrastructure
- **DEP-003**: P8 (#62) — `TopFlows` query JOINs with `flows` table (created in P8). If `flows` table does not exist, `TopFlows` returns empty result.

### Technology Dependencies
- **PLT-001**: `github.com/jackc/pgx/v5` — Postgres driver
- **PLT-002**: `github.com/jackc/pgx/v5/pgxpool` — Connection pooling
- **PLT-003**: `github.com/testcontainers/testcontainers-go` — Integration test containers
- **PLT-004**: `github.com/golang-migrate/migrate/v4` — Schema migrations (via P3)

### `[v2.0]` Prerequisite
- **PRE-001**: P1 REQ-030 — `cpn.executionTracker` must add `toolCallCount atomic.Int64` before `persist.ExecutionRecord.ToolCalls` can be populated with real data. Until then, the column stores 0.

### Depended On By
- **P8** (#62) — Store facade wires `PostgresLedgerRepository` and `PostgresIntelligenceRepository`, implements MetricsRecorder bridge

---

## 9. Examples & Edge Cases

### 9.1 Ledger Upsert

```go
ledgerRepo := postgres.NewPostgresLedgerRepository(pool)

// First call: INSERT
err := ledgerRepo.Record(ctx, &persist.LedgerRecord{
    SessionID:    "sess-1",
    InputTokens:  500,
    OutputTokens: 200,
    Calls:        1,
    TotalCostUSD: 0.05,
})

// Second call: INCREMENT
err = ledgerRepo.Record(ctx, &persist.LedgerRecord{
    SessionID:    "sess-1",
    InputTokens:  300,
    OutputTokens: 150,
    Calls:        1,
    TotalCostUSD: 0.03,
})

// Result: InputTokens=800, OutputTokens=350, Calls=2, TotalCostUSD=0.08
rec, _ := ledgerRepo.GetBySession(ctx, "sess-1")
```

### 9.2 MetricsRecorder Bridge (Integration Layer)

```go
// After CPN.Run() completes in SessionService.SendMessage goroutine:
records := metricsRecorder.Records()
for i := range records {
    pRec := &persist.ExecutionRecord{
        ID:               uuid.NewString(),
        CPNID:            records[i].CPNID,
        CPNRole:          records[i].CPNRole,
        CPNDepth:         records[i].CPNDepth,
        SessionID:        records[i].SessionID,
        TransitionsFired: records[i].TransitionsFired,
        LLMCalls:         records[i].LLMCallCount,
        ToolCalls:        0, // TODO: after P1 REQ-030
        TokensProduced:   records[i].TokensProduced,
        TotalCostUSD:     records[i].TotalCostUSD,
        DurationMs:       records[i].Duration.Milliseconds(),
        Success:          records[i].Success,
        StartedAt:        records[i].StartedAt,
        CompletedAt:      records[i].CompletedAt,
    }
    _ = intellRepo.RecordExecution(ctx, pRec)
}
```

### 9.3 TopFlows Ranking Query

```go
// Top 5 flows by composite score
top, _ := intellRepo.TopFlows(ctx, 5)
for _, f := range top {
    fmt.Printf("Hash=%s Role=%s Score=%.2f\n", f.Hash, f.Role, f.Score)
}
```

### 9.4 Edge Cases

| Edge Case | Expected Behavior |
|-----------|-------------------|
| Ledger.Record with empty SessionID | `persist.ErrInvalidInput` |
| Ledger.Record twice for same session | Second call increments all counters |
| Ledger.GetBySession for nonexistent session | `persist.ErrLedgerNotFound` |
| Ledger.QueryByDate with no data for that date | Empty slice, nil error |
| Ledger.AggregateByDateRange with empty range | Zero-valued `LedgerAggregate`, nil error |
| Ledger.SetDailyTotal for nonexistent session | `persist.ErrLedgerNotFound` |
| Intelligence.RecordExecution with empty ID | UUID generated at persistence layer |
| Intelligence.RecordExecution with empty CPNID | `persist.ErrInvalidInput` |
| Intelligence.QueryByRole with no matching records | Empty slice, nil error |
| Intelligence.Aggregate with no matching records | Zero-valued `RankingMetrics`, nil error |
| Intelligence.TopFlows with no flows table | Empty slice, nil error |
| Intelligence.TopFlows with n=0 | Empty slice, nil error |
| Cancelled context on any method | Returns `context.Canceled` |
| `[v2.0]` RecordExecution with ToolCalls=0 | Persists normally (0 is valid until prerequisite implemented) |
| `[v2.0]` RecordExecution with CPNDepth > 0 | Persists correctly, used in hierarchy queries |
| `[v2.0]` QueryByRole uses started_at for time range | Not completed_at (started_at is the execution origin time) |

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

# 4. Integration tests pass (requires Docker)
go test -race -count=1 -v ./store/postgres/... -run "TestPostgresLedger|TestPostgresIntelligence"

# 5. Interface compliance
grep "var _ persist.LedgerRepository = " store/postgres/ledger.go
grep "var _ persist.IntelligenceRepository = " store/postgres/intelligence.go

# 6. No cpn/ imports
! grep -rn '"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"' store/postgres/ledger.go store/postgres/intelligence.go

# 7. Parameterized SQL only
! grep -n 'fmt.Sprintf.*SELECT\|fmt.Sprintf.*INSERT\|fmt.Sprintf.*UPDATE' store/postgres/ledger.go store/postgres/intelligence.go

# 8. Upsert SQL
grep -n "ON CONFLICT" store/postgres/ledger.go

# 9. [v2.0] Verify new columns in migration
grep -n "cpn_depth\|tool_calls\|tokens_produced\|started_at\|completed_at" store/postgres/migrations/005_execution_records.up.sql

# 10. [v2.0] Verify all 14 fields in INSERT
grep -c '\$' store/postgres/intelligence.go | head -5
```

---

## 11. Related Specifications / Further Reading

- [Agentic CPN v1.3 — Persistence Layer](../agentic-cpn-v1.3.md) — Sections 4.3, 4.5, 5, 7, 9, 12
- [Block P1: Repository Interfaces](block-p1-repository-interfaces.md) — LedgerRepository, IntelligenceRepository interfaces, DTOs, ExecutionRecord v2.0 updates
- [Block P3: Postgres Connection Pool + Migrations](#57) — pgxpool.Pool, migration infrastructure
- [Block P5: Postgres Session + Message Repository](block-p5-postgres-session-message.md) — Session table (for session_id FK references)
- [Block P8: Flow Repository + Store Facade](#62) — Flows table for TopFlows JOIN, MetricsRecorder bridge implementation

---

## Appendix A: Change Log

| Version | Date | Change | Reason | Impact |
|---------|------|--------|--------|--------|
| 1.0 | 2026-03-27 | Initial specification from Issue #61 | Block definition | P7 |
| 2.0 | 2026-04-01 | Updated execution_records schema: added cpn_depth, tool_calls, tokens_produced, started_at, completed_at columns (REQ-012 to REQ-016) | `persist.ExecutionRecord` DTO updated in P1 v2.0 — SQL schema must match | P7, P8 |
| 2.0 | 2026-04-01 | Documented MetricsRecorder-to-IntelligenceRepository bridge pattern (REQ-019 to REQ-023) | Connection between in-memory MetricsRecorder and Postgres persistence was undocumented | P7, P8 |
| 2.0 | 2026-04-01 | Recommended Option 2 (batch after Run) for bridge pattern | Simpler, no hot-path latency, acceptable loss window for analytics | P7, P8 |
| 2.0 | 2026-04-01 | Added tool_calls column to execution_records | ToolCalls field in persist.ExecutionRecord requires storage | P7 |
| 2.0 | 2026-04-01 | Added AC-014 to AC-017, test group `TestPostgresIntelligenceRepository_V2Fields` | Verify new column storage and retrieval | P7 |
| 2.0 | 2026-04-01 | RecordExecution INSERT expanded from 10 to 14 parameters | New columns require additional bind parameters | P7 |

---

## Implementation File Checklist

- [ ] `back/go-assistant/store/postgres/ledger.go` — `PostgresLedgerRepository` with 5 methods
- [ ] `back/go-assistant/store/postgres/intelligence.go` — `PostgresIntelligenceRepository` with 4 methods
- [ ] `back/go-assistant/store/postgres/ledger_test.go` — ~11 integration tests with testcontainers
- [ ] `back/go-assistant/store/postgres/intelligence_test.go` — ~19 integration tests with testcontainers
- [ ] `back/go-assistant/store/postgres/migrations/003_ledger.up.sql` — token_ledger table
- [ ] `back/go-assistant/store/postgres/migrations/003_ledger.down.sql` — DROP token_ledger
- [ ] `back/go-assistant/store/postgres/migrations/005_execution_records.up.sql` — execution_records table (v2.0 columns)
- [ ] `back/go-assistant/store/postgres/migrations/005_execution_records.down.sql` — DROP execution_records
- [ ] Run `go build ./store/postgres/` — compiles
- [ ] Run `go vet ./store/postgres/` — passes
- [ ] Run `golangci-lint run ./store/postgres/` — zero findings
- [ ] Run `go test -race -count=1 ./store/postgres/ -run TestPostgresLedger` — all pass
- [ ] Run `go test -race -count=1 ./store/postgres/ -run TestPostgresIntelligence` — all pass
- [ ] Verify: `var _ persist.LedgerRepository = (*PostgresLedgerRepository)(nil)` compiles
- [ ] Verify: `var _ persist.IntelligenceRepository = (*PostgresIntelligenceRepository)(nil)` compiles
- [ ] Verify: Ledger.Record uses ON CONFLICT DO UPDATE
- [ ] Verify: All SQL is parameterized
- [ ] Verify: No `cpn/` package imports
- [ ] Verify: `[v2.0]` execution_records has cpn_depth, tool_calls, tokens_produced, started_at, completed_at
- [ ] Verify: `[v2.0]` RecordExecution INSERT has 14 parameters
- [ ] Verify: `[v2.0]` MetricsRecorder bridge pattern is documented (not implemented in P7)
- [ ] Verify: `[v2.0]` TopFlows handles missing flows table gracefully
