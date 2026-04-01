---
title: "Block P6 — Redis Session Cache + HITL Repository (Updated)"
version: 2.0
date_created: 2026-03-27
last_updated: 2026-04-01
owner: Agentic CPN Team
tags: golang, persistence, redis, session-cache, hitl, spec-driven, block-P6
---

# Introduction

Block P6 implements two Redis-backed repositories: `RedisSessionRepository` (hot cache wrapping `PostgresSessionRepository`) and `RedisHITLRepository` (pending HITL request queue with TTL). Redis provides low-latency reads for active sessions and ephemeral storage for time-bounded HITL approval requests.

This is **version 2.0** of the P6 specification, updated after an expert panel audit (2026-04-01) that identified two gaps: (1) the HITL flow in `cpn/hitl.go` blocks on a channel without capturing the proposal content for persistence, and (2) the Redis session cache layer must receive pre-populated `SessionRecord` DTOs (same session state bridge as P5).

**Spec reference:** `.docs/specs/agentic-cpn-v1.3.md` Sections 4.1, 4.6, 8, 9, 14

**GitHub Issue:** #60

**Specialist Team:**
- **Database Architect**: Redis key design, TTL strategy, cache invalidation, data model for HITL queue
- **Senior Golang Engineer**: Cache-aside pattern, go-redis usage, context propagation, error handling
- **DevSecOps Engineer**: Redis TLS, key expiration, testcontainers isolation

**Depends on:** P1 (#55), P3 (#57), P5 (#59)

---

## 1. Purpose & Scope

### Purpose
Provide a Redis-backed hot cache for active sessions (avoiding Postgres round-trips on every read) and a Redis-backed HITL pending request queue with automatic TTL expiry.

### Scope
- **In scope**: `RedisSessionRepository` (cache-aside wrapping P5), `RedisHITLRepository` (4 methods), Redis key design, TTL management, HITL proposal capture integration pattern, `testcontainers-go` integration tests
- **Out of scope**: Postgres session storage (P5), CPN execution (engine), event streaming via Redis pub/sub (future), session state management logic (P8 integration layer)

### Audience
Implementers of Block P6 and downstream block P8 (Store facade wiring).

---

## 2. Definitions

| Term | Definition |
|------|-----------|
| **RedisSessionRepository** | Cache-aside implementation of `persist.SessionRepository` backed by Redis, with fallthrough to `PostgresSessionRepository` on cache miss |
| **RedisHITLRepository** | Implementation of `persist.HITLRepository` using Redis keys with TTL for pending HITL approval requests |
| **Cache-aside pattern** | Read: check cache, on miss read from Postgres and populate cache. Write: write to Postgres first, then update/invalidate cache. |
| **TTL** | Time-To-Live — Redis key expiration. Sessions: 30 min. HITL requests: 1 hour. |
| **HITL proposal capture** | `[v2.0]` Integration pattern where the `EventHITLRequested` event payload is captured by the event callback and serialized into `HITLPendingRequest.Proposal` before enqueuing |
| **Session state bridge** | `[v2.0]` Same pattern as P5 — Redis cache receives pre-populated `SessionRecord` DTOs from the integration layer, does NOT combine `cpn.Session` + `sessionState` |
| **EventSink callback** | `CPN.EventSink` function set during session creation — receives every event the CPN emits |
| **Testcontainers** | `testcontainers-go` for ephemeral Redis containers in integration tests |

---

## 3. Requirements, Constraints & Guidelines

### Requirements

#### Package Structure
- **REQ-001**: Implementation files: `store/redis/session.go`, `store/redis/hitl.go`, `store/redis/pool.go` with `package redis`
- **REQ-002**: Test files: `store/redis/session_test.go`, `store/redis/hitl_test.go` with `package redis_test`

#### RedisSessionRepository (9 methods — cache-aside)
- **REQ-003**: `Create(ctx, *SessionRecord) error` — Write to Postgres (delegate to wrapped repo) FIRST, then SET in Redis with 30 min TTL. If Postgres write fails, do NOT cache.
- **REQ-004**: `Get(ctx, sessionID) (*SessionRecord, error)` — GET from Redis. On hit: return cached record. On miss: delegate to Postgres, cache result with 30 min TTL, return.
- **REQ-005**: `GetByUserID(ctx, userID) ([]*SessionRecord, error)` — Delegate directly to Postgres (not cached — user session lists change too frequently for reliable cache invalidation).
- **REQ-006**: `AppendMessage(ctx, sessionID, *MessageRecord) error` — Delegate to Postgres. On success, RPUSH message JSON to `session:{id}:messages` list in Redis. Reset TTL.
- **REQ-007**: `UpdateState(ctx, sessionID, SessionState) error` — Delegate to Postgres. On success, update cached SessionRecord in Redis (GET, modify state, SET). If state is closed/expired, DELETE Redis key (evict from cache).
- **REQ-008**: `Touch(ctx, sessionID) error` — Delegate to Postgres. On success, EXPIRE Redis key to reset TTL to 30 min (refresh without re-fetching).
- **REQ-009**: `Close(ctx, sessionID) error` — Delegate to Postgres. On success, DELETE Redis key (closed sessions evicted from hot cache).
- **REQ-010**: `ListExpired(ctx, before time.Time) ([]string, error)` — Delegate directly to Postgres (no Redis involvement — expiry scan is a batch operation).
- **REQ-011**: `Delete(ctx, sessionID) error` — Delegate to Postgres. On success, DELETE Redis key and `session:{id}:messages` key.
- **REQ-012**: Cache errors are logged but NOT propagated — Postgres is the source of truth. If Redis is down, the repository degrades gracefully to Postgres-only.

#### RedisHITLRepository (4 methods)
- **REQ-013**: `Enqueue(ctx, *HITLPendingRequest) error` — SET key `hitl:{session_id}:{transition_id}` with JSON-serialized `HITLPendingRequest`. TTL = `ExpiresAt - time.Now()` (from the request's ExpiresAt field). Returns `persist.ErrInvalidInput` if TTL is non-positive.
- **REQ-014**: `Dequeue(ctx, sessionID, transitionID) (*HITLPendingRequest, error)` — GET + DEL key `hitl:{session_id}:{transition_id}`. Returns `persist.ErrHITLNotFound` if key does not exist. Returns `persist.ErrHITLExpired` if key existed but TTL has already elapsed (race condition — treat as not found).
- **REQ-015**: `ListPending(ctx, sessionID) ([]*HITLPendingRequest, error)` — SCAN keys matching `hitl:{session_id}:*`, GET each, return list. Returns empty slice if none found.
- **REQ-016**: `Expire(ctx, olderThan time.Duration) (int64, error)` — Redis handles TTL natively. This method SCANs all `hitl:*` keys and counts/returns how many have already expired (informational). In practice, Redis auto-expires keys, so this is mainly for metrics/reporting.

#### Redis Key Design (from v1.3 Section 8)
- **REQ-017**: Key patterns:
  ```
  session:{id}                      → JSON(SessionRecord)          TTL: 30m
  session:user:{user_id}            → SET of session IDs           TTL: 24h
  hitl:{session_id}:{transition_id} → JSON(HITLPendingRequest)     TTL: 1h
  session:{id}:messages             → LIST of JSON(MessageRecord)  TTL: 30m
  ```
- **REQ-018**: All keys MUST use the prefix patterns above (no bare keys).

#### `[UPDATED v2.0]` HITL Proposal Capture — Integration Pattern
- **REQ-019**: When a HITL transition fires (`fireHITL` or `fireHITLWithRevision` in `cpn/hitl.go`), it emits `EventHITLRequested` with the proposal content in `Event.Payload`:
  - Single-turn: `Event.Payload = cfg.Prompt` (string)
  - Revision loop: `Event.Payload = map[string]any{"prompt": prompt, "round": rounds}`
- **REQ-020**: The integration layer (event callback wired via `CPN.EventSink` in `SessionService.CreateSession`) captures this event and creates a `HITLPendingRequest`:
  ```go
  // In the EventSink callback (SessionService or P8 integration):
  if evt.Type == cpn.EventHITLRequested {
      proposal, _ := json.Marshal(evt.Payload)
      req := &persist.HITLPendingRequest{
          SessionID:    sessionID,
          TransitionID: evt.TransitionID,
          CPNID:        evt.CPNID,
          CPNRole:      evt.CPNRole,
          Proposal:     proposal,
          CreatedAt:    time.Now(),
          ExpiresAt:    time.Now().Add(1 * time.Hour),
      }
      hitlRepo.Enqueue(ctx, req)
  }
  ```
- **REQ-021**: This is an INTEGRATION pattern, NOT a Redis-specific concern. P6 implements `Enqueue`/`Dequeue` correctly; the integration layer (P8 or SessionService) is responsible for wiring the `EventSink` callback that calls `Enqueue`.
- **REQ-022**: `HITLPendingRequest.Proposal` is a `json.RawMessage` containing the serialized event payload. Consumers (e.g., HTTP API `GET /sessions/{id}/hitl`) deserialize it to display the proposal to the human.

#### `[UPDATED v2.0]` Session State Bridge (Same as P5)
- **REQ-023**: `RedisSessionRepository` receives pre-populated `SessionRecord` DTOs from the integration layer. The Redis cache layer wraps `PostgresSessionRepository` and does NOT need to understand the session state bridge.
- **REQ-024**: The cache layer's responsibility is purely caching — it caches whatever `SessionRecord` is returned by the Postgres layer, which already has State/LastActivityAt/ClosedAt populated.
- **REQ-025**: `RedisSessionRepository` has NO knowledge of `cpn.Session`, `cpn.State`, or `internal/app.sessionState`.

#### Constructor
- **REQ-026**: `NewRedisSessionRepository(client *redis.Client, fallback persist.SessionRepository) *RedisSessionRepository`
- **REQ-027**: `NewRedisHITLRepository(client *redis.Client) *RedisHITLRepository`
- **REQ-028**: `NewRedisPool(url string, opts ...RedisOption) (*redis.Client, error)` — Connection pool with TLS support.

### Security Requirements
- **SEC-001**: Redis connection uses TLS (`redis.Options{TLSConfig: ...}`) as per v1.3 Section 11
- **SEC-002**: Redis URL from environment variable `LIWAISI_REDIS_URL` — never hardcoded
- **SEC-003**: HITL keys have mandatory TTL — no unbounded growth

### Constraints
- **CON-001**: Depends on `github.com/redis/go-redis/v9`
- **CON-002**: Does NOT import `cpn/` package — operates purely on `persist` types
- **CON-003**: Cache failures are non-fatal — Postgres is always the source of truth
- **CON-004**: `GetByUserID` and `ListExpired` are NOT cached (passed through to Postgres)

### Guidelines
- **GUD-001**: Use `go-redis` pipeline for multi-key operations where possible
- **GUD-002**: JSON serialize/deserialize `SessionRecord` and `HITLPendingRequest` for Redis storage
- **GUD-003**: Log cache errors at WARN level — do not propagate to caller
- **GUD-004**: TTL values should be configurable via constructor options (with defaults: session=30m, HITL=1h)
- **GUD-005**: `[v2.0]` Document that HITL proposal capture happens in the integration layer, not in Redis repository code

---

## 4. Interfaces & Data Contracts

### 4.1 Package Layout

```
store/redis/
├── pool.go         — Redis connection pool + TLS
├── session.go      — RedisSessionRepository (cache-aside)
├── session_test.go — Integration tests with testcontainers
├── hitl.go         — RedisHITLRepository
├── hitl_test.go    — Integration tests with testcontainers
└── store.go        — RedisStore factory (future, P8)
```

### 4.2 RedisSessionRepository

```go
// RedisSessionRepository implements persist.SessionRepository as a cache-aside
// layer over a fallback repository (typically PostgresSessionRepository).
// Cache failures are logged but never propagated — Postgres is the source of truth.
// Receives pre-populated SessionRecord DTOs — does NOT manage session state.
type RedisSessionRepository struct {
    client   *redis.Client
    fallback persist.SessionRepository
    ttl      time.Duration // default: 30m
    logger   *slog.Logger
}

var _ persist.SessionRepository = (*RedisSessionRepository)(nil)

func NewRedisSessionRepository(client *redis.Client, fallback persist.SessionRepository) *RedisSessionRepository
```

### 4.3 RedisHITLRepository

```go
// RedisHITLRepository implements persist.HITLRepository using Redis keys with TTL.
// HITL requests are ephemeral — they expire after 1 hour if unanswered.
// Proposal content is populated by the integration layer's EventSink callback.
type RedisHITLRepository struct {
    client *redis.Client
    logger *slog.Logger
}

var _ persist.HITLRepository = (*RedisHITLRepository)(nil)

func NewRedisHITLRepository(client *redis.Client) *RedisHITLRepository
```

### 4.4 `[v2.0]` HITL Proposal Capture — Integration Flow

```
  CPN Engine (cpn/hitl.go)
  ┌──────────────────────────┐
  │ fireHITL()               │
  │  1. c.emit(Event{        │
  │       Type: HITLRequested,│
  │       Payload: cfg.Prompt │
  │     })                    │
  │  2. Block on channel...   │
  └──────────┬───────────────┘
             │ Event emitted via EventSink
             ▼
  Integration Layer (SessionService / P8)
  ┌──────────────────────────────────────┐
  │ EventSink callback:                  │
  │  if evt.Type == EventHITLRequested { │
  │    proposal := json.Marshal(         │
  │      evt.Payload)                    │
  │    req := HITLPendingRequest{        │
  │      SessionID:    sessionID,        │
  │      TransitionID: evt.TransitionID, │
  │      CPNID:        evt.CPNID,       │
  │      CPNRole:      evt.CPNRole,      │
  │      Proposal:     proposal,         │
  │      CreatedAt:    time.Now(),       │
  │      ExpiresAt:    +1h,              │
  │    }                                 │
  │    hitlRepo.Enqueue(ctx, &req)       │
  │  }                                   │
  └──────────┬───────────────────────────┘
             │
             ▼
  Redis (P6)
  ┌──────────────────────────────┐
  │ SET hitl:{sid}:{tid}         │
  │     JSON(HITLPendingRequest) │
  │     EX 3600                  │
  └──────────────────────────────┘
```

**Key invariant**: `RedisHITLRepository.Enqueue` receives a fully populated `HITLPendingRequest` including `Proposal`. It does NOT interact with the CPN engine or event system. The integration layer is the bridge.

### 4.5 `[v2.0]` Session State Bridge — Cache Layer

```
  Integration Layer
  ┌─────────────────────────────┐
  │ Combine cpn.Session +       │
  │ sessionState → SessionRecord│
  └──────────┬──────────────────┘
             │ Pre-populated SessionRecord
             ▼
  RedisSessionRepository (P6)
  ┌─────────────────────────────┐
  │ Cache-aside:                │
  │  Create → Postgres, cache   │
  │  Get → Redis hit OR         │
  │         Postgres + cache    │
  │  Touch → Postgres, reset TTL│
  │  Close → Postgres, evict    │
  └──────────┬──────────────────┘
             │ Delegates to
             ▼
  PostgresSessionRepository (P5)
  ┌─────────────────────────────┐
  │ Pure data access            │
  └─────────────────────────────┘
```

**Key invariant**: The Redis cache layer caches whatever `SessionRecord` the Postgres layer returns. It does not need to understand where `State`, `LastActivityAt`, or `ClosedAt` came from.

---

## 5. Acceptance Criteria

- **AC-001**: `go build ./store/redis/` succeeds
- **AC-002**: `go vet ./store/redis/` passes
- **AC-003**: `golangci-lint run ./store/redis/` passes
- **AC-004**: `go test -race -count=1 ./store/redis/` — all pass (requires Docker for testcontainers)
- **AC-005**: `var _ persist.SessionRepository = (*RedisSessionRepository)(nil)` compiles
- **AC-006**: `var _ persist.HITLRepository = (*RedisHITLRepository)(nil)` compiles
- **AC-007**: Cache hit returns same `SessionRecord` as Postgres
- **AC-008**: Cache miss falls through to Postgres and populates cache
- **AC-009**: Redis failure degrades gracefully (Postgres-only, no error to caller)
- **AC-010**: HITL Enqueue creates key with correct TTL
- **AC-011**: HITL Dequeue returns and deletes the request
- **AC-012**: HITL keys auto-expire after TTL
- **AC-013**: `Close` and `Delete` evict session from Redis cache
- **AC-014**: `Touch` resets Redis TTL without re-fetching from Postgres
- **AC-015**: `[v2.0]` HITL `Enqueue` accepts `HITLPendingRequest` with populated `Proposal` field
- **AC-016**: `[v2.0]` Redis session cache has NO imports from `cpn/` package
- **AC-017**: `[v2.0]` HITL proposal capture is documented as an integration pattern, NOT implemented in P6

---

## 6. Test Automation Strategy

### Test Files: `store/redis/session_test.go`, `store/redis/hitl_test.go`

### Framework
- `testcontainers-go` for ephemeral Redis container per test suite
- Standard Go `testing` package
- `go-redis/v9` for direct Redis verification commands
- Mock `persist.SessionRepository` for fallback in session cache tests

### Test Structure

| Test Function | Subtests | Count |
|---------------|----------|-------|
| **Session Cache Tests** | | |
| `TestRedisSessionRepository_Create` | Success_CachesAfterPostgres, PostgresFails_NoCacheWrite | 2 |
| `TestRedisSessionRepository_Get` | CacheHit, CacheMiss_FetchFromPostgres, BothMiss_NotFound | 3 |
| `TestRedisSessionRepository_GetByUserID` | DelegatesToPostgres | 1 |
| `TestRedisSessionRepository_AppendMessage` | Success_UpdatesCache, PostgresFails | 2 |
| `TestRedisSessionRepository_UpdateState` | ToClosed_EvictsCache, ToActive_UpdatesCache | 2 |
| `TestRedisSessionRepository_Touch` | ResetsTTL | 1 |
| `TestRedisSessionRepository_Close` | EvictsFromCache | 1 |
| `TestRedisSessionRepository_Delete` | EvictsFromCache | 1 |
| `TestRedisSessionRepository_GracefulDegradation` | RedisDown_FallsToPostgres | 1 |
| **HITL Repository Tests** | | |
| `TestRedisHITLRepository_Enqueue` | Success, InvalidTTL, WithProposal | 3 |
| `TestRedisHITLRepository_Dequeue` | Success, NotFound, Expired | 3 |
| `TestRedisHITLRepository_ListPending` | HasPending, NoPending | 2 |
| `TestRedisHITLRepository_Expire` | ReportsExpiredCount | 1 |
| `TestRedisHITLRepository_TTL` | KeyExpiresAfterTTL | 1 |
| `TestRedisHITLRepository_ProposalRoundTrip` | `[v2.0]` EnqueueDequeue_PreservesProposal | 1 |
| **Total** | | **~25** |

### TestMain Setup

```go
func TestMain(m *testing.M) {
    // 1. Start Redis testcontainer
    // 2. Create go-redis client
    // 3. Run tests
    // 4. Teardown container
}
```

---

## 7. Rationale & Context

### Why Redis for Session Cache
Active sessions are read on every HTTP request (GET /sessions/{id}). Redis provides sub-millisecond reads, avoiding a Postgres round-trip. The 30-minute TTL ensures stale sessions naturally evict.

### Why Redis for HITL
HITL pending requests are ephemeral (1-hour TTL). Redis provides atomic GET+DEL and built-in TTL — simpler than a Postgres row with a cron-based expiry sweep.

### Why Cache-Aside (Not Write-Through)
Cache-aside gives the application control over when to populate the cache. Write-through would require updating Redis on every Postgres write, including bulk operations (ListExpired, batch updates) where caching is wasteful.

### Why Cache Failures Are Non-Fatal
Redis is a performance optimization, not a correctness requirement. If Redis is down, the system degrades to Postgres-only with higher latency but no data loss (Axiom A14: "Redis loss is recoverable from Postgres").

### `[v2.0]` Why HITL Proposal Capture Is an Integration Pattern
The CPN engine (`cpn/hitl.go`) emits `EventHITLRequested` with the proposal as `Event.Payload`. The engine does NOT know about persistence. The integration layer (which wires `CPN.EventSink`) is the natural place to capture this event, serialize the proposal, and call `HITLRepository.Enqueue`. This preserves Axiom A13 (persistence is transparent to the engine).

### `[v2.0]` Why Redis Cache Does Not Manage Session State
The Redis cache layer's job is caching `SessionRecord` values. It does not need to know that `State` comes from `sessionState` or that `LastActivityAt` is managed by the integration layer. It simply caches what Postgres returns and evicts on state changes.

---

## 8. Dependencies & External Integrations

### Block Dependencies
- **DEP-001**: P1 (#55) — `persist.SessionRepository`, `persist.HITLRepository` interfaces, DTOs, sentinel errors
- **DEP-002**: P3 (#57) — Migration infrastructure (for Postgres fallback in integration tests)
- **DEP-003**: P5 (#59) — `PostgresSessionRepository` as the fallback for cache misses

### Technology Dependencies
- **PLT-001**: `github.com/redis/go-redis/v9` — Redis client with pooling, TLS, pub/sub
- **PLT-002**: `github.com/testcontainers/testcontainers-go` — Integration test containers
- **PLT-003**: Go `encoding/json` — SessionRecord and HITLPendingRequest serialization

### Depended On By
- **P8** (#62) — Store facade wires `RedisSessionRepository` and `RedisHITLRepository` into the application

---

## 9. Examples & Edge Cases

### 9.1 Cache-Aside Session Read

```go
// Get: cache hit
rec, err := redisRepo.Get(ctx, "sess-123")
// Redis has the key → returns immediately (sub-ms)

// Get: cache miss
rec, err := redisRepo.Get(ctx, "sess-456")
// Redis miss → falls through to PostgresSessionRepository
// Postgres returns → cached in Redis with 30m TTL → returned
```

### 9.2 HITL Proposal Capture (Integration Layer)

```go
// In SessionService.CreateSession:
root.EventSink = func(e *cpn.Event) {
    if e.Type == cpn.EventHITLRequested {
        proposal, _ := json.Marshal(e.Payload)
        req := &persist.HITLPendingRequest{
            SessionID:    sessionID,
            TransitionID: e.TransitionID,
            CPNID:        e.CPNID,
            CPNRole:      e.CPNRole,
            Proposal:     proposal,
            CreatedAt:    time.Now(),
            ExpiresAt:    time.Now().Add(1 * time.Hour),
        }
        _ = hitlRepo.Enqueue(context.Background(), req)
    }
}
```

### 9.3 HITL Dequeue for HTTP API

```go
// HTTP handler: GET /sessions/{id}/hitl
pending, err := hitlRepo.ListPending(ctx, sessionID)
// Returns list of HITLPendingRequest with Proposal field populated
// Frontend displays proposal to human for approve/reject/revise
```

### 9.4 Edge Cases

| Edge Case | Expected Behavior |
|-----------|-------------------|
| Redis down during Get | Falls through to Postgres, logs WARN |
| Redis down during Create | Postgres write succeeds, cache not populated, logs WARN |
| Redis down during Touch | Postgres Touch succeeds, TTL not reset, logs WARN |
| Get session that exists in Redis but not Postgres | Returns cached record (cache is trusted during TTL window) |
| HITL Enqueue with ExpiresAt in the past | `persist.ErrInvalidInput` (non-positive TTL) |
| HITL Dequeue race: two consumers | First consumer gets the request, second gets `persist.ErrHITLNotFound` |
| HITL key expired by Redis TTL | Dequeue returns `persist.ErrHITLNotFound` |
| ListPending with thousands of keys | SCAN-based iteration with COUNT hint (no KEYS blocking) |
| Close session | Redis key deleted (evicted), Postgres updated |
| Delete session | Redis key + messages list deleted, Postgres cascades |
| `[v2.0]` HITL Enqueue with empty Proposal | Persists with null/empty Proposal — no validation (integration layer responsibility) |
| `[v2.0]` HITL Enqueue with large Proposal | Redis SET succeeds up to 512MB value limit — no application-level cap |
| `[v2.0]` Cache stores pre-populated SessionRecord | State/LastActivityAt/ClosedAt are in the cached JSON — no special handling needed |

---

## 10. Validation Criteria

```bash
cd back/go-assistant

# 1. Package compiles
go build ./store/redis/

# 2. Vet passes
go vet ./store/redis/

# 3. Lint passes
golangci-lint run ./store/redis/

# 4. Integration tests pass (requires Docker for Redis + Postgres)
go test -race -count=1 -v ./store/redis/...

# 5. Interface compliance
grep "var _ persist.SessionRepository = " store/redis/session.go
grep "var _ persist.HITLRepository = " store/redis/hitl.go

# 6. No cpn/ imports
! grep -rn '"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"' store/redis/

# 7. Redis key patterns
grep -n "session:" store/redis/session.go
grep -n "hitl:" store/redis/hitl.go

# 8. TTL configuration
grep -n "TTL\|Expire\|time.Duration" store/redis/session.go store/redis/hitl.go

# 9. [v2.0] Verify HITL proposal round-trip test exists
grep -n "Proposal" store/redis/hitl_test.go

# 10. [v2.0] Verify graceful degradation test exists
grep -n "GracefulDegradation\|RedisDown" store/redis/session_test.go
```

---

## 11. Related Specifications / Further Reading

- [Agentic CPN v1.3 — Persistence Layer](../agentic-cpn-v1.3.md) — Sections 4.1, 4.6, 8, 9, 14
- [Block P1: Repository Interfaces](block-p1-repository-interfaces.md) — SessionRepository, HITLRepository interfaces, DTOs, sentinel errors
- [Block P5: Postgres Session + Message Repository](block-p5-postgres-session-message.md) — Postgres fallback for cache misses, session state bridge pattern
- [Block P8: Flow Repository + Store Facade](#62) — Wires Redis + Postgres repositories, HITL proposal capture integration

---

## Appendix A: Change Log

| Version | Date | Change | Reason | Impact |
|---------|------|--------|--------|--------|
| 1.0 | 2026-03-27 | Initial specification from Issue #60 | Block definition | P6 |
| 2.0 | 2026-04-01 | Added HITL Proposal Capture integration pattern (REQ-019 to REQ-022) | `fireHITL` emits proposal in `EventHITLRequested` but HITL channel blocks without capturing it for persistence | P6, P8 |
| 2.0 | 2026-04-01 | Added Session State Bridge notes (REQ-023 to REQ-025) | Redis cache layer must not assume session state comes from `cpn.Session` | P6 |
| 2.0 | 2026-04-01 | Added data flow diagrams for HITL proposal capture and cache layer | Clarify integration responsibility boundaries | P6, P8 |
| 2.0 | 2026-04-01 | Added AC-015 to AC-017 for v2.0 validation | Verify proposal capture and state bridge compliance | P6 |
| 2.0 | 2026-04-01 | Added `TestRedisHITLRepository_ProposalRoundTrip` test | Validate Proposal field persistence | P6 |

---

## Implementation File Checklist

- [ ] `back/go-assistant/store/redis/pool.go` — Redis connection pool with TLS support
- [ ] `back/go-assistant/store/redis/session.go` — `RedisSessionRepository` with 9 cache-aside methods
- [ ] `back/go-assistant/store/redis/hitl.go` — `RedisHITLRepository` with 4 methods
- [ ] `back/go-assistant/store/redis/session_test.go` — ~14 integration tests for session cache
- [ ] `back/go-assistant/store/redis/hitl_test.go` — ~11 integration tests for HITL repository
- [ ] Run `go build ./store/redis/` — compiles
- [ ] Run `go vet ./store/redis/` — passes
- [ ] Run `golangci-lint run ./store/redis/` — zero findings
- [ ] Run `go test -race -count=1 ./store/redis/` — all pass
- [ ] Verify: `var _ persist.SessionRepository = (*RedisSessionRepository)(nil)` compiles
- [ ] Verify: `var _ persist.HITLRepository = (*RedisHITLRepository)(nil)` compiles
- [ ] Verify: Cache-aside pattern (Postgres first, then cache)
- [ ] Verify: Cache failures are logged, not propagated
- [ ] Verify: HITL keys have TTL set from ExpiresAt
- [ ] Verify: No `cpn/` package imports
- [ ] Verify: `[v2.0]` HITL Enqueue accepts populated Proposal field
- [ ] Verify: `[v2.0]` Proposal round-trips through Enqueue → Dequeue
- [ ] Verify: `[v2.0]` Session cache does not reference cpn.Session or sessionState
