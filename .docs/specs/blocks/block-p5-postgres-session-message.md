---
title: "Block P5 — Postgres Session + Message Repository (Updated)"
version: 2.0
date_created: 2026-03-27
last_updated: 2026-04-01
owner: Agentic CPN Team
tags: golang, persistence, postgres, session, message, spec-driven, block-P5
---

# Introduction

Block P5 implements `PostgresSessionRepository` — the durable Postgres backend for the `persist.SessionRepository` interface defined in P1. It covers session CRUD, message storage with FK cascading, state lifecycle, activity tracking, and expiry queries. All SQL is parameterized. Integration tests use `testcontainers-go`.

This is **version 2.0** of the P5 specification, updated after an expert panel audit (2026-04-01) that identified a critical gap: `cpn.Session` does NOT carry `State`, `LastActivityAt`, or `ClosedAt` fields. These are managed by `internal/app.sessionState` (a separate struct). The `persist.SessionRecord` DTO has all these fields. P5 must correctly receive pre-populated DTOs from the integration layer.

**Spec reference:** `.docs/specs/agentic-cpn-v1.3.md` Sections 4.1, 5, 7, 9

**GitHub Issue:** #59

**Specialist Team:**
- **Database Architect**: Schema design, FK cascades, indexing, parameterized SQL, partition strategy
- **Senior Golang Engineer**: Repository implementation, pgx usage, context propagation, error mapping
- **DevSecOps Engineer**: Connection security, encryption at rest, testcontainers isolation

**Depends on:** P1 (#55), P3 (#57)

---

## 1. Purpose & Scope

### Purpose
Provide a durable Postgres-backed implementation of `persist.SessionRepository` (9 methods) with full ACID guarantees, FK-cascaded message storage, and efficient expiry queries.

### Scope
- **In scope**: `PostgresSessionRepository` struct, 9 interface methods, `sessions` table schema, `messages` table schema, FK cascade on delete, parameterized SQL queries, sentinel error mapping, `testcontainers-go` integration tests
- **Out of scope**: Redis session cache (P6), session state management logic (integration layer / P8), CPN topology (P2/P8), event storage (P4), token ledger (P7)

### Audience
Implementers of Block P5 and downstream blocks P6 (Redis cache wrapping Postgres) and P8 (Store facade bridging session state).

---

## 2. Definitions

| Term | Definition |
|------|-----------|
| **PostgresSessionRepository** | Concrete implementation of `persist.SessionRepository` backed by PostgreSQL via `pgx/v5` |
| **SessionRecord** | Persistence DTO defined in P1 — combines data from `cpn.Session` + `internal/app.sessionState` |
| **MessageRecord** | Persistence DTO for conversation messages, FK-linked to sessions |
| **Session state bridge** | `[v2.0]` Pattern where `SessionRecord.State` and `SessionRecord.LastActivityAt` are populated by the integration layer (P8 Store facade or SessionService), NOT by `cpn.Session` directly |
| **FK cascade** | Foreign key constraint with `ON DELETE CASCADE` — deleting a session automatically deletes its messages |
| **Parameterized SQL** | All SQL uses `$1`, `$2`, ... placeholders — never string interpolation |
| **Sentinel error mapping** | Postgres errors (e.g., unique violation) are mapped to `persist.ErrXxx` sentinels |
| **Touch** | Update `last_activity_at` to current time without modifying other fields |
| **Testcontainers** | `testcontainers-go` library for spinning up ephemeral Postgres containers in integration tests |

---

## 3. Requirements, Constraints & Guidelines

### Requirements

#### Package Structure
- **REQ-001**: Implementation file: `store/postgres/session.go` with `package postgres`
- **REQ-002**: Test file: `store/postgres/session_test.go` with `package postgres_test`

#### PostgresSessionRepository (9 methods)
- **REQ-003**: `Create(ctx, *SessionRecord) error` — INSERT into `sessions` table. Returns `persist.ErrSessionExists` on unique violation (session ID already exists). Returns `persist.ErrInvalidInput` if `SessionRecord.ID` is empty.
- **REQ-004**: `Get(ctx, sessionID) (*SessionRecord, error)` — SELECT from `sessions` by ID. Returns `persist.ErrSessionNotFound` if no row found.
- **REQ-005**: `GetByUserID(ctx, userID) ([]*SessionRecord, error)` — SELECT from `sessions` WHERE `user_id = $1` ORDER BY `created_at DESC`. Returns empty slice (not nil) if no sessions found.
- **REQ-006**: `AppendMessage(ctx, sessionID, *MessageRecord) error` — INSERT into `messages` table. Returns `persist.ErrSessionNotFound` if FK constraint fails (session does not exist). Returns `persist.ErrSessionClosed` if session state is `closed` or `expired`.
- **REQ-007**: `UpdateState(ctx, sessionID, SessionState) error` — UPDATE `sessions` SET `state = $2` WHERE `id = $1`. When state is `SessionClosed`, also sets `closed_at = NOW()`. Returns `persist.ErrSessionNotFound` if no row updated.
- **REQ-008**: `Touch(ctx, sessionID) error` — UPDATE `sessions` SET `last_activity_at = NOW()` WHERE `id = $1`. Returns `persist.ErrSessionNotFound` if no row updated.
- **REQ-009**: `Close(ctx, sessionID) error` — UPDATE `sessions` SET `state = 'closed', closed_at = NOW()` WHERE `id = $1`. Returns `persist.ErrSessionNotFound` if no row updated. Returns `persist.ErrSessionClosed` if already closed.
- **REQ-010**: `ListExpired(ctx, before time.Time) ([]string, error)` — SELECT `id` FROM `sessions` WHERE `state = 'active' AND last_activity_at < $1`. Returns empty slice if none found.
- **REQ-011**: `Delete(ctx, sessionID) error` — DELETE FROM `sessions` WHERE `id = $1`. Messages are cascade-deleted by FK constraint. Returns `persist.ErrSessionNotFound` if no row deleted.
- **REQ-012**: Every method's first parameter MUST be `context.Context`. All pgx calls use the provided context for cancellation/timeout propagation.

#### `[UPDATED v2.0]` Session State Bridge Pattern
- **REQ-013**: `PostgresSessionRepository` receives **pre-populated** `SessionRecord` DTOs from the integration layer (P8 Store facade or SessionService wrapper). The repository does NOT combine `cpn.Session` + `sessionState` — that is the integration layer's responsibility.
- **REQ-014**: `Create` receives a full `SessionRecord` with `State = SessionActive`, `CreatedAt = time.Now()`, `LastActivityAt = time.Now()`. The caller (integration layer) is responsible for populating these fields before calling `Create`.
- **REQ-015**: `UpdateState` updates the `state` column and optionally sets `closed_at` (when transitioning to `SessionClosed`). The integration layer decides WHEN to call `UpdateState` based on CPN execution lifecycle.
- **REQ-016**: `Touch` updates ONLY `last_activity_at` to `NOW()`. The integration layer calls `Touch` on each user interaction (e.g., `SendMessage`).
- **REQ-017**: The repository has NO knowledge of `cpn.Session`, `cpn.State`, or `internal/app.sessionState`. It operates purely on `persist.SessionRecord` and `persist.SessionState` types.

#### SQL Schema
- **REQ-018**: `sessions` table:
  ```sql
  CREATE TABLE IF NOT EXISTS sessions (
      id              TEXT PRIMARY KEY,
      user_id         TEXT NOT NULL,
      channel         TEXT NOT NULL,
      state           TEXT NOT NULL DEFAULT 'active',
      created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      last_activity_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
      closed_at       TIMESTAMPTZ,
      metadata        JSONB
  );
  CREATE INDEX idx_sessions_user_id ON sessions (user_id);
  CREATE INDEX idx_sessions_state_activity ON sessions (state, last_activity_at);
  ```
- **REQ-019**: `messages` table:
  ```sql
  CREATE TABLE IF NOT EXISTS messages (
      id          TEXT PRIMARY KEY,
      session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
      role        TEXT NOT NULL,
      content     TEXT NOT NULL,
      cpn_id      TEXT NOT NULL DEFAULT '',
      cpn_role    TEXT NOT NULL DEFAULT '',
      cpn_depth   INT NOT NULL DEFAULT 0,
      timestamp   TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );
  CREATE INDEX idx_messages_session_id ON messages (session_id);
  CREATE INDEX idx_messages_session_timestamp ON messages (session_id, timestamp);
  ```
- **REQ-020**: These tables are created in migration `001_sessions.up.sql` (as referenced in v1.3 Section 7).

#### Error Mapping
- **REQ-021**: Postgres unique violation (`23505`) on `sessions.id` → `persist.ErrSessionExists`
- **REQ-022**: Postgres FK violation (`23503`) on `messages.session_id` → `persist.ErrSessionNotFound`
- **REQ-023**: Zero rows affected on UPDATE/DELETE → appropriate `persist.ErrSessionNotFound` or `persist.ErrSessionClosed`
- **REQ-024**: All other Postgres errors → wrap with `fmt.Errorf("postgres session: %w", err)`

#### Constructor
- **REQ-025**: `NewPostgresSessionRepository(pool *pgxpool.Pool) *PostgresSessionRepository` — accepts a `pgxpool.Pool` from P3.

### Security Requirements
- **SEC-001**: All SQL uses parameterized queries (`$1`, `$2`, ...) — no string interpolation
- **SEC-002**: Message content column uses `pgcrypto` AES-256-GCM encryption at rest (as per v1.3 Section 11)
- **SEC-003**: Connection pool from P3 uses `sslmode=verify-full`

### Constraints
- **CON-001**: Depends on `pgx/v5` and `pgxpool` (introduced by P3)
- **CON-002**: Does NOT import `cpn/` package — operates purely on `persist` types
- **CON-003**: Does NOT implement caching — that is P6's responsibility
- **CON-004**: Migration files are managed by P3 (`golang-migrate/migrate`)

### Guidelines
- **GUD-001**: Use `pgx.RowToStructByName` or manual `Scan` for row mapping — avoid ORMs
- **GUD-002**: Use `pool.QueryRow` for single-row results, `pool.Query` for multi-row
- **GUD-003**: Wrap all pgx errors with descriptive context: `fmt.Errorf("postgres session get %s: %w", id, err)`
- **GUD-004**: `GetByUserID` returns a plain slice (not paginated) — user sessions are bounded (per P1 GUD-004)
- **GUD-005**: `[v2.0]` Document that `State`, `LastActivityAt`, and `ClosedAt` in `SessionRecord` arrive pre-populated from the integration layer. PostgresSessionRepository is a pure data access layer.

---

## 4. Interfaces & Data Contracts

### 4.1 Package Layout

```
store/postgres/
├── pool.go          — Connection pool (P3)
├── session.go       — PostgresSessionRepository (this block)
├── session_test.go  — Integration tests with testcontainers
└── migrations/
    └── 001_sessions.up.sql   — sessions + messages tables
    └── 001_sessions.down.sql — DROP sessions, messages
```

### 4.2 PostgresSessionRepository

```go
// PostgresSessionRepository implements persist.SessionRepository using PostgreSQL.
// It receives pre-populated SessionRecord DTOs — the integration layer (P8 Store
// facade or SessionService) is responsible for combining cpn.Session + sessionState
// into a SessionRecord before calling Create.
type PostgresSessionRepository struct {
    pool *pgxpool.Pool
}

// Compile-time interface assertion.
var _ persist.SessionRepository = (*PostgresSessionRepository)(nil)

func NewPostgresSessionRepository(pool *pgxpool.Pool) *PostgresSessionRepository
```

### 4.3 `[v2.0]` Session State Bridge — Data Flow

```
                     Integration Layer (P8 / SessionService)
                     ┌──────────────────────────────────────┐
                     │                                      │
  cpn.Session        │  Combine:                            │  persist.SessionRecord
  ┌──────────┐       │    ID       ← cpn.Session.ID        │  ┌──────────────────┐
  │ ID       │──────►│    UserID   ← cpn.Session.UserID    │─►│ ID               │
  │ UserID   │       │    Channel  ← cpn.Session.Channel   │  │ UserID           │
  │ Channel  │       │    CreatedAt← cpn.Session.CreatedAt │  │ Channel          │
  │ CreatedAt│       │                                      │  │ State = "active" │
  │ Root     │       │    State    ← sessionState.state     │  │ CreatedAt        │
  │ Stream   │       │    ClosedAt ← set on Close()         │  │ LastActivityAt   │
  └──────────┘       │    LastActivityAt ← time.Now()       │  │ ClosedAt         │
                     │                                      │  │ Metadata         │
  sessionState       │                                      │  └──────────────────┘
  ┌──────────┐       │                                      │         │
  │ state    │──────►│                                      │         ▼
  │ cancel   │       └──────────────────────────────────────┘  PostgresSessionRepository
  └──────────┘                                                    .Create(ctx, rec)
```

**Key invariant**: `PostgresSessionRepository` never reads `cpn.Session` or `sessionState` directly. It only receives and persists `SessionRecord` DTOs.

### 4.4 SQL Queries (Reference)

| Method | SQL | Notes |
|--------|-----|-------|
| Create | `INSERT INTO sessions (id, user_id, channel, state, created_at, last_activity_at, closed_at, metadata) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)` | All fields from SessionRecord |
| Get | `SELECT id, user_id, channel, state, created_at, last_activity_at, closed_at, metadata FROM sessions WHERE id = $1` | Single row |
| GetByUserID | `SELECT ... FROM sessions WHERE user_id = $1 ORDER BY created_at DESC` | Multi-row, not paginated |
| AppendMessage | Check session state first, then `INSERT INTO messages (id, session_id, role, content, cpn_id, cpn_role, cpn_depth, timestamp) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)` | Two queries in sequence |
| UpdateState | `UPDATE sessions SET state = $2, closed_at = $3 WHERE id = $1` | `closed_at` is NULL unless state=closed |
| Touch | `UPDATE sessions SET last_activity_at = NOW() WHERE id = $1` | Single column update |
| Close | `UPDATE sessions SET state = 'closed', closed_at = NOW() WHERE id = $1 AND state != 'closed'` | Idempotency guard |
| ListExpired | `SELECT id FROM sessions WHERE state = 'active' AND last_activity_at < $1` | Uses composite index |
| Delete | `DELETE FROM sessions WHERE id = $1` | FK cascade removes messages |

---

## 5. Acceptance Criteria

- **AC-001**: `go build ./store/postgres/` succeeds
- **AC-002**: `go vet ./store/postgres/` passes
- **AC-003**: `golangci-lint run ./store/postgres/` passes
- **AC-004**: `go test -race -count=1 ./store/postgres/` — all pass (requires Docker for testcontainers)
- **AC-005**: `var _ persist.SessionRepository = (*PostgresSessionRepository)(nil)` compiles
- **AC-006**: `Create` returns `persist.ErrSessionExists` on duplicate ID
- **AC-007**: `Get` returns `persist.ErrSessionNotFound` for nonexistent session
- **AC-008**: `AppendMessage` returns `persist.ErrSessionClosed` for closed sessions
- **AC-009**: `Delete` cascade-deletes all associated messages
- **AC-010**: `ListExpired` returns only active sessions with `last_activity_at` before the threshold
- **AC-011**: `Touch` updates only `last_activity_at` without modifying state
- **AC-012**: `Close` sets `state = 'closed'` and `closed_at = NOW()`
- **AC-013**: All SQL is parameterized — no string interpolation in any query
- **AC-014**: Messages are ordered by `timestamp` within a session
- **AC-015**: `[v2.0]` `Create` accepts a pre-populated `SessionRecord` with State/LastActivityAt already set by the caller
- **AC-016**: `[v2.0]` Repository has NO imports from `cpn/` package (only `persist` types)

---

## 6. Test Automation Strategy

### Test File: `store/postgres/session_test.go`

### Framework
- `testcontainers-go` for ephemeral Postgres container per test suite
- Standard Go `testing` package
- `pgx/v5` for direct DB verification queries
- `golang-migrate/migrate` for schema setup in `TestMain`

### Test Structure

| Test Function | Subtests | Count |
|---------------|----------|-------|
| `TestPostgresSessionRepository_Create` | Success, DuplicateID, EmptyID, AllFieldsPersisted | 4 |
| `TestPostgresSessionRepository_Get` | Success, NotFound, AllFieldsMapped | 3 |
| `TestPostgresSessionRepository_GetByUserID` | Success, NoSessions, MultipleSessions_OrderedByCreatedAt | 3 |
| `TestPostgresSessionRepository_AppendMessage` | Success, SessionNotFound, SessionClosed, MessageFieldsPersisted | 4 |
| `TestPostgresSessionRepository_UpdateState` | ToActive, ToClosed_SetsClosedAt, ToExpired, NotFound | 4 |
| `TestPostgresSessionRepository_Touch` | Success_UpdatesLastActivityAt, NotFound | 2 |
| `TestPostgresSessionRepository_Close` | Success, AlreadyClosed, NotFound | 3 |
| `TestPostgresSessionRepository_ListExpired` | HasExpired, NoneExpired, IgnoresClosed | 3 |
| `TestPostgresSessionRepository_Delete` | Success_CascadesMessages, NotFound | 2 |
| `TestPostgresSessionRepository_ContextCancellation` | CancelledContext | 1 |
| `TestPostgresSessionRepository_StateBridge` | `[v2.0]` PrePopulatedRecord, StateFromCaller | 2 |
| **Total** | | **~31** |

### TestMain Setup

```go
func TestMain(m *testing.M) {
    // 1. Start Postgres testcontainer
    // 2. Run migrations (001_sessions.up.sql)
    // 3. Create pgxpool.Pool
    // 4. Run tests
    // 5. Teardown container
}
```

---

## 7. Rationale & Context

### Why Postgres for Sessions
Sessions are the durable record of user interactions. Postgres provides ACID guarantees, FK constraints for message integrity, and efficient indexing for expiry queries. Redis (P6) provides the hot cache layer on top.

### Why FK Cascade
When a session is deleted, its messages become orphaned data. FK CASCADE ensures referential integrity without requiring application-level cleanup. This follows the v1.3 spec Section 7 design.

### Why Parameterized SQL (No ORM)
ORMs add complexity and hide query behavior. The 9 repository methods have simple, predictable SQL patterns that are clearer as parameterized queries. This is consistent with the project's "no magic" philosophy.

### `[v2.0]` Why Session State Bridge
The `cpn.Session` struct is a runtime binding (user to CPN). It holds the `Root *CPN` pointer and HITL channels. It does NOT track lifecycle state (idle, running, waiting). State is managed by `internal/app.sessionState` which wraps `cpn.State`. The `persist.SessionRecord` DTO correctly unifies both into a single persistence-friendly structure. The integration layer (P8 Store facade or an enhanced SessionService) combines them before calling `PostgresSessionRepository.Create`.

### `[v2.0]` Why Repository Has No Domain Knowledge
`PostgresSessionRepository` is a pure data access layer. It does not know about `cpn.Session`, `cpn.State`, or state machine transitions. This keeps the persistence layer decoupled from the CPN engine (Axiom A13) and makes it independently testable with plain `SessionRecord` values.

---

## 8. Dependencies & External Integrations

### Block Dependencies
- **DEP-001**: P1 (#55) — `persist.SessionRepository` interface, `persist.SessionRecord`, `persist.MessageRecord`, `persist.SessionState`, sentinel errors
- **DEP-002**: P3 (#57) — `pgxpool.Pool`, migration infrastructure, `001_sessions` migration file

### Technology Dependencies
- **PLT-001**: `github.com/jackc/pgx/v5` — Postgres driver
- **PLT-002**: `github.com/jackc/pgx/v5/pgxpool` — Connection pooling
- **PLT-003**: `github.com/testcontainers/testcontainers-go` — Integration test containers
- **PLT-004**: `github.com/golang-migrate/migrate/v4` — Schema migrations (via P3)

### Depended On By
- **P6** (#60) — Redis session cache wraps `PostgresSessionRepository` for cache-miss fallthrough
- **P8** (#62) — Store facade uses `PostgresSessionRepository` as the durable backend, bridges session state

---

## 9. Examples & Edge Cases

### 9.1 Session Lifecycle (Integration Layer Perspective)

```go
// Integration layer creates a pre-populated SessionRecord
rec := &persist.SessionRecord{
    ID:             "sess-abc123",
    UserID:         "user-1",
    Channel:        "web",
    State:          persist.SessionActive,
    CreatedAt:      time.Now(),
    LastActivityAt:  time.Now(),
}
err := pgRepo.Create(ctx, rec) // P5 just persists it

// On each user message, integration layer calls Touch
err = pgRepo.Touch(ctx, "sess-abc123")

// On CPN completion, integration layer decides to update state
err = pgRepo.UpdateState(ctx, "sess-abc123", persist.SessionClosed)

// Expiry sweep (cron job)
expired, _ := pgRepo.ListExpired(ctx, time.Now().Add(-30*time.Minute))
for _, id := range expired {
    pgRepo.UpdateState(ctx, id, persist.SessionExpired)
}
```

### 9.2 Message Append with Session State Check

```go
// AppendMessage checks session state before INSERT
msg := &persist.MessageRecord{
    ID:        "msg-1",
    SessionID: "sess-abc123",
    Role:      "user",
    Content:   "Hello",
    Timestamp: time.Now(),
}
err := pgRepo.AppendMessage(ctx, "sess-abc123", msg)
// If session is closed: errors.Is(err, persist.ErrSessionClosed) == true
```

### 9.3 Edge Cases

| Edge Case | Expected Behavior |
|-----------|-------------------|
| Create with duplicate session ID | `persist.ErrSessionExists` |
| Create with empty ID | `persist.ErrInvalidInput` |
| Get nonexistent session | `persist.ErrSessionNotFound` |
| AppendMessage to closed session | `persist.ErrSessionClosed` |
| AppendMessage to nonexistent session | `persist.ErrSessionNotFound` |
| UpdateState on nonexistent session | `persist.ErrSessionNotFound` |
| Touch on nonexistent session | `persist.ErrSessionNotFound` |
| Close already-closed session | `persist.ErrSessionClosed` |
| Delete nonexistent session | `persist.ErrSessionNotFound` |
| Delete session with messages | Messages cascade-deleted, no error |
| ListExpired with no expired sessions | Empty slice, nil error |
| ListExpired ignores closed/expired sessions | Only state='active' returned |
| Cancelled context on any method | Returns `context.Canceled` |
| `[v2.0]` Create with State already set to "closed" | Persists as-is (repository does not validate state transitions) |
| `[v2.0]` Create with nil Metadata | Persists NULL in JSONB column |
| `[v2.0]` GetByUserID returns sessions ordered by created_at DESC | Most recent first |

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
go test -race -count=1 -v ./store/postgres/... -run TestPostgresSession

# 5. Interface compliance
grep "var _ persist.SessionRepository = " store/postgres/session.go

# 6. No cpn/ imports
! grep -rn '"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"' store/postgres/session.go

# 7. Parameterized SQL only
! grep -n 'fmt.Sprintf.*SELECT\|fmt.Sprintf.*INSERT\|fmt.Sprintf.*UPDATE\|fmt.Sprintf.*DELETE' store/postgres/session.go

# 8. FK cascade in migration
grep -n "ON DELETE CASCADE" store/postgres/migrations/001_sessions.up.sql

# 9. [v2.0] Verify no cpn.Session references
! grep -n "cpn.Session\|cpn.State\|sessionState" store/postgres/session.go
```

---

## 11. Related Specifications / Further Reading

- [Agentic CPN v1.3 — Persistence Layer](../agentic-cpn-v1.3.md) — Sections 4.1, 5, 7, 9, 11
- [Block P1: Repository Interfaces](block-p1-repository-interfaces.md) — SessionRepository interface, SessionRecord DTO, sentinel errors
- [Block P3: Postgres Connection Pool + Migrations](#57) — pgxpool.Pool, migration infrastructure
- [Block P6: Redis Session Cache](block-p6-redis-session-cache-hitl.md) — Cache layer wrapping this repository
- [Block P8: Flow Repository + Store Facade](#62) — Integration layer bridging cpn.Session + sessionState → SessionRecord

---

## Appendix A: Change Log

| Version | Date | Change | Reason | Impact |
|---------|------|--------|--------|--------|
| 1.0 | 2026-03-27 | Initial specification from Issue #59 | Block definition | P5 |
| 2.0 | 2026-04-01 | Added Session State Bridge pattern (REQ-013 to REQ-017) | `cpn.Session` has no State/LastActivityAt/ClosedAt — integration layer bridges the gap | P5, P6, P8 |
| 2.0 | 2026-04-01 | Documented data flow diagram for state bridge | Clarify responsibility boundaries | P5 |
| 2.0 | 2026-04-01 | Added AC-015, AC-016 for v2.0 validation | Verify bridge pattern compliance | P5 |
| 2.0 | 2026-04-01 | Added `TestPostgresSessionRepository_StateBridge` tests | Validate pre-populated DTOs | P5 |

---

## Implementation File Checklist

- [ ] `back/go-assistant/store/postgres/session.go` — `PostgresSessionRepository` with 9 methods
- [ ] `back/go-assistant/store/postgres/session_test.go` — ~31 integration tests with testcontainers
- [ ] `back/go-assistant/store/postgres/migrations/001_sessions.up.sql` — sessions + messages tables
- [ ] `back/go-assistant/store/postgres/migrations/001_sessions.down.sql` — DROP tables
- [ ] Run `go build ./store/postgres/` — compiles
- [ ] Run `go vet ./store/postgres/` — passes
- [ ] Run `golangci-lint run ./store/postgres/` — zero findings
- [ ] Run `go test -race -count=1 ./store/postgres/ -run TestPostgresSession` — all pass
- [ ] Verify: `var _ persist.SessionRepository = (*PostgresSessionRepository)(nil)` compiles
- [ ] Verify: All SQL is parameterized
- [ ] Verify: FK CASCADE on messages table
- [ ] Verify: No `cpn/` package imports in session.go
- [ ] Verify: Sentinel error mapping for unique/FK violations
- [ ] Verify: `[v2.0]` Create accepts pre-populated SessionRecord
- [ ] Verify: `[v2.0]` Touch updates only last_activity_at
- [ ] Verify: `[v2.0]` UpdateState sets closed_at when state=closed
- [ ] Verify: `[v2.0]` No references to cpn.Session or sessionState
