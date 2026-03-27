# Unified Agentic CPN — Spec Driven Design
> Version 1.3 — Supersedes: nothing (extends v1.2)
> Focus: Persistence Layer — Session Store, Event Store, CPN Serialization, Flow Intelligence Persistence

---

## 0. Related Documents

| Document | Description | Status |
|---|---|---|
| **Unified Agentic CPN — Spec Driven Design v1.3** (this document) | Persistence layer: repositories, serialization, Redis/Postgres backends | current |
| Unified Agentic CPN v1.2 | Full OpenRouter API, streaming, guardrails, activity billing | current (engine spec) |
| Unified Agentic CPN v1.1 | 7 building blocks, basic OpenRouter, 18-step implementation | archived |
| Unified Agentic CPN v1.0 | First unified spec | archived |

---

## 1. What This Document Covers

| Area | Description |
|---|---|
| Repository Interfaces | 6 Go interfaces abstracting all persistence: Session, Event, Ledger, Flow, Intelligence, HITL |
| Persistence DTOs | Decoupled storage types (SessionRecord, EventRecord, etc.) separate from domain types |
| CPN Topology Serialization | Function Registry pattern for marshaling CPNs with func fields |
| Postgres Backend | Schema, migrations, connection pooling, event batching, monthly partitioning |
| Redis Backend | Hot session cache, HITL pending requests, pub/sub for streaming, event write-behind |
| Integration Strategy | How persistence wraps the existing in-memory CPN engine without changing signatures |
| Incremental Build Blocks | 8 blocks (P1–P8) for testable, valuable delivery |

---

## 2. Design Axioms (Persistence Extensions)

These extend the 12 axioms from v1.2 with persistence-specific constraints.

| # | Axiom | Source |
|---|---|---|
| A9 | **Event Store is append-only.** No event is ever modified. The persistence layer enforces this structurally: EventRepository has no Update or Delete methods. | v1.0 A9 + persistence enforcement |
| A13 | **Persistence is transparent.** The CPN engine (`cpn/` package) never imports persistence drivers. All persistence is behind interfaces. In-memory implementations enable zero-dependency testing. | New |
| A14 | **Hot/cold separation.** Ephemeral data (active sessions, pending HITL) lives in Redis. Durable data (events, metrics, flows, billing) lives in Postgres. Redis loss is recoverable from Postgres. | New |
| A15 | **Func fields are registered, not serialized.** Go functions cannot be marshaled. The Function Registry pattern maps string names to func values at startup. Serialization stores names; deserialization looks up funcs. | New |

---

## 3. Architecture Overview

### 3.1 Package Layout

```
back/go-assistant/
├── cpn/                    # Core engine (existing) — NO persistence imports
│   ├── persist/            # Repository interfaces + in-memory implementations
│   │   ├── interfaces.go   # All 6 repository interfaces
│   │   ├── types.go        # Persistence DTOs (SessionRecord, EventRecord, etc.)
│   │   ├── memory.go       # In-memory implementations (test/dev)
│   │   ├── registry.go     # FuncRegistry for CPN serialization
│   │   └── topology.go     # MarshalCPN, UnmarshalCPN, TopologyHash
│   └── ...existing files...
├── store/                  # Concrete backend implementations
│   ├── postgres/
│   │   ├── pool.go         # Connection pool + health checks
│   │   ├── session.go      # PostgresSessionRepository
│   │   ├── event.go        # PostgresEventRepository
│   │   ├── event_batcher.go # Batch writer for high-volume events
│   │   ├── ledger.go       # PostgresLedgerRepository
│   │   ├── intelligence.go # PostgresIntelligenceRepository
│   │   ├── flow.go         # PostgresFlowRepository
│   │   ├── store.go        # PostgresStore factory
│   │   └── migrations/     # SQL migration files (NNN_name.up/down.sql)
│   └── redis/
│       ├── pool.go         # Connection pool + TLS
│       ├── session.go      # RedisSessionRepository (cache layer)
│       ├── hitl.go         # RedisHITLRepository
│       └── store.go        # RedisStore factory
└── cmd/cli/main.go         # Wire everything together
```

### 3.2 Data Flow

```
CPN Engine (cpn/)
    │
    ├─ Event emission ──► EventRepository.Append() ──► Postgres (batched)
    │                                                    ↑
    │                                              Redis write-behind buffer
    │
    ├─ Session state ──► SessionRepository ──► Redis (hot) + Postgres (durable)
    │
    ├─ HITL pending ──► HITLRepository ──► Redis (TTL: 1h)
    │
    ├─ Token/cost ──► LedgerRepository ──► Postgres
    │
    ├─ Execution metrics ──► IntelligenceRepository ──► Postgres
    │
    └─ Crystallized flows ──► FlowRepository ──► Postgres (topology JSON)
                                                    ↑
                                              FuncRegistry (name→func mapping)
```

---

## 4. Repository Interfaces

### 4.1 SessionRepository

```go
type SessionRepository interface {
    Create(ctx context.Context, session *SessionRecord) error
    Get(ctx context.Context, sessionID string) (*SessionRecord, error)
    GetByUserID(ctx context.Context, userID string) ([]*SessionRecord, error)
    AppendMessage(ctx context.Context, sessionID string, msg *MessageRecord) error
    UpdateState(ctx context.Context, sessionID string, state SessionState) error
    Touch(ctx context.Context, sessionID string) error
    Close(ctx context.Context, sessionID string) error
    ListExpired(ctx context.Context, before time.Time) ([]string, error)
    Delete(ctx context.Context, sessionID string) error
}
```

### 4.2 EventRepository

```go
// Append-only (Axiom A9). No Update or Delete methods.
type EventRepository interface {
    Append(ctx context.Context, events ...*EventRecord) error
    QueryBySession(ctx context.Context, sessionID string, opts *EventQueryOpts) ([]*EventRecord, error)
    QueryByCPN(ctx context.Context, cpnID string, opts *EventQueryOpts) ([]*EventRecord, error)
    QueryByType(ctx context.Context, eventType string, from, to time.Time, opts *EventQueryOpts) ([]*EventRecord, error)
    Count(ctx context.Context, opts *EventQueryOpts) (int64, error)
}
```

### 4.3 LedgerRepository

```go
type LedgerRepository interface {
    Record(ctx context.Context, rec *LedgerRecord) error
    GetBySession(ctx context.Context, sessionID string) (*LedgerRecord, error)
    QueryByDate(ctx context.Context, date time.Time) ([]*LedgerRecord, error)
    SetDailyTotal(ctx context.Context, sessionID string, date time.Time, totalUSD float64) error
    AggregateByDateRange(ctx context.Context, from, to time.Time) (*LedgerAggregate, error)
}
```

### 4.4 FlowRepository

```go
type FlowRepository interface {
    Save(ctx context.Context, flow *FlowRecord) error
    GetByHash(ctx context.Context, hash string) (*FlowRecord, error)
    List(ctx context.Context, opts *FlowListOpts) ([]*FlowRecord, error)
    UpdateStats(ctx context.Context, hash string, stats *FlowStats) error
    Delete(ctx context.Context, hash string) error
}
```

### 4.5 IntelligenceRepository

```go
type IntelligenceRepository interface {
    RecordExecution(ctx context.Context, rec *ExecutionRecord) error
    QueryByRole(ctx context.Context, role string, from, to time.Time) ([]*ExecutionRecord, error)
    Aggregate(ctx context.Context, role string, from, to time.Time) (*RankingMetrics, error)
    TopFlows(ctx context.Context, n int) ([]*RankedFlow, error)
}
```

### 4.6 HITLRepository

```go
type HITLRepository interface {
    Enqueue(ctx context.Context, req *HITLPendingRequest) error
    Dequeue(ctx context.Context, sessionID, transitionID string) (*HITLPendingRequest, error)
    ListPending(ctx context.Context, sessionID string) ([]*HITLPendingRequest, error)
    Expire(ctx context.Context, olderThan time.Duration) (int64, error)
}
```

---

## 5. Persistence DTOs

```go
type SessionState string
const (
    SessionActive  SessionState = "active"
    SessionClosed  SessionState = "closed"
    SessionExpired SessionState = "expired"
)

type SessionRecord struct {
    ID, UserID, Channel string
    State               SessionState
    CreatedAt, LastActivityAt time.Time
    ClosedAt            *time.Time
    Metadata            json.RawMessage
}

type MessageRecord struct {
    ID, SessionID, Role, Content, CPNID, CPNRole string
    CPNDepth  int
    Timestamp time.Time
}

type EventRecord struct {
    ID, Type, SessionID, CPNID, CPNRole, TransitionID, TransitionKind string
    CPNDepth      int
    TokenSnapshot json.RawMessage
    Payload       json.RawMessage
    Timestamp     time.Time
}

type EventQueryOpts struct {
    SessionID, CPNID, EventType string
    From, To                    time.Time
    Limit                       int
    Cursor                      string
}

type LedgerRecord struct {
    SessionID     string
    InputTokens, OutputTokens, Calls int64
    TotalCostUSD, DailyTotalUSD      float64
    LastUpdated   time.Time
}

type LedgerAggregate struct {
    TotalInputTokens, TotalOutputTokens, TotalCalls int64
    TotalCostUSD                                     float64
}

type FlowRecord struct {
    Hash, Role      string
    TopologyJSON    json.RawMessage
    FunctionMapping json.RawMessage
    Stats           FlowStats
    CreatedAt, UpdatedAt time.Time
    DeletedAt       *time.Time
}

type FlowStats struct {
    ExecutionCount          int64
    SuccessRate, AvgCostUSD float64
    AvgDurationMs           int64
}

type ExecutionRecord struct {
    ID, CPNID, CPNRole, SessionID string
    TransitionsFired, LLMCalls, ToolCalls int
    TotalCostUSD float64
    DurationMs   int64
    Success      bool
    Timestamp    time.Time
}

type RankingMetrics struct {
    Role string
    AvgLLMCalls, AvgToolCalls, AvgCostUSD, AvgDurationMs, SuccessRate float64
    ExecutionCount int64
}

type RankedFlow struct {
    Hash, Role string
    Score      float64
    Stats      FlowStats
}

type HITLPendingRequest struct {
    SessionID, TransitionID, CPNID, CPNRole string
    Proposal  json.RawMessage
    CreatedAt, ExpiresAt time.Time
}
```

---

## 6. CPN Topology Serialization

### 6.1 Function Registry

```go
type FuncRegistry struct {
    guards    map[string]func([]*Token) bool
    executors map[string]func(context.Context, Token) (Token, error)
    retryOns  map[string]func(error, int) bool
    factories map[string]func() *CPN
    mu        sync.RWMutex
}

var DefaultRegistry = NewFuncRegistry()

func (r *FuncRegistry) RegisterGuard(name string, fn func([]*Token) bool)
func (r *FuncRegistry) RegisterExecutor(name string, fn func(context.Context, Token) (Token, error))
func (r *FuncRegistry) RegisterRetryOn(name string, fn func(error, int) bool)
func (r *FuncRegistry) RegisterSubNetFactory(name string, fn func() *CPN)
```

### 6.2 Serializable Topology

```go
type CPNTopology struct {
    ID, Role string
    Depth    int
    Mode     string
    Places      map[string]PlaceTopology
    Transitions map[string]TransitionTopology
}

type PlaceTopology struct {
    ID, Color, Space string
}

type TransitionTopology struct {
    ID, Kind        string
    InputPlaces, OutputPlaces []string
    ErrorPlace, ToolName      string
    GuardFunc, ExecutorFunc, RetryOnFunc, FactoryFunc string
    Retry *RetryPolicyTopology
}
```

### 6.3 Marshal / Unmarshal

```go
func MarshalCPN(c *CPN, registry *FuncRegistry) (*CPNTopology, error)
func UnmarshalCPN(t *CPNTopology, registry *FuncRegistry) (*CPN, error)
func TopologyHash(t *CPNTopology) string // SHA-256 of structural topology
```

---

## 7. Postgres Schema

See migrations in `store/postgres/migrations/`:

| Migration | Tables | Purpose |
|---|---|---|
| 001_sessions | `sessions`, `messages` | Session lifecycle + conversation history |
| 002_events | `events` (partitioned by month) | Append-only event store (Axiom A9) |
| 003_ledger | `token_ledger` | Per-session token/cost tracking |
| 004_flows | `flows` | Crystallized CPN topologies |
| 005_execution_records | `execution_records` | Flow Intelligence metrics |

Events table is **range-partitioned by month** for high-volume handling. Old partitions are detached and archived after retention period.

---

## 8. Redis Key Design

```
session:{id}                     → JSON(SessionRecord)         TTL: 30m
session:user:{user_id}           → SET of session IDs          TTL: 24h
hitl:{session_id}:{transition_id} → JSON(HITLPendingRequest)   TTL: 1h
session:{id}:messages            → LIST of JSON(MessageRecord) TTL: 30m
stream:{session_id}              → PUB/SUB (no persistence)
events:buffer                    → STREAM (write-behind, no TTL)
```

---

## 9. Retention Policies

| Store | Retention | Rationale |
|---|---|---|
| Sessions (Redis) | 30 min inactivity TTL | Hot cache; promotes to Postgres |
| Sessions (Postgres) | 90 days | Audit trail |
| Messages | Cascade with session | |
| Events | 30 days (configurable) | Monthly partition drop |
| TokenLedger | 90 days | Billing reconciliation |
| Flows | Indefinite (soft delete) | Crystallized knowledge |
| Execution Records | 90 days | Trailing window for ranking |
| HITL Pending (Redis) | 1 hour TTL | Unanswered approvals expire |

---

## 10. Event Batching

```go
type EventBatcher struct {
    ch            chan *EventRecord
    db            *sql.DB
    batchSize     int           // default: 100
    flushInterval time.Duration // default: 500ms
}

func (b *EventBatcher) Submit(rec *EventRecord) // blocks if buffer full (backpressure)
func (b *EventBatcher) Flush(ctx context.Context) error
func (b *EventBatcher) Close() error // flush + stop goroutine
```

Uses Postgres `COPY` for bulk inserts (10-50x faster than individual INSERTs).

---

## 11. Security

| Concern | Approach |
|---|---|
| Encryption at rest | `pgcrypto` AES-256-GCM for sensitive columns (message content, event payload) |
| Encryption in transit | Postgres `sslmode=verify-full`, Redis TLS |
| Secrets management | Environment variables (`LIWAISI_DB_DSN`, `LIWAISI_REDIS_URL`, `LIWAISI_DB_ENCRYPTION_KEY`) |
| Connection strings | Never logged; `String()` methods redact credentials |
| Backup | Postgres WAL archiving + daily pg_dump; Redis AOF + RDB snapshots |

---

## 12. Integration with CPN Engine

The `cpn/` package is NOT modified to import persistence drivers. Integration uses:

1. **EventSink callback** on CPN: `WithEventSink(func(Event))` option — persistence layer provides the callback
2. **OnMessage callback** on Session: `OnMessage func(Message)` — persistence writes through
3. **PersistentTokenLedger wrapper**: wraps in-memory TokenLedger + writes through to Postgres
4. **FuncRegistry at startup**: all tool/guard/factory functions registered before CPN construction

---

## 13. External Dependencies (first additions to go.mod)

| Dependency | Purpose |
|---|---|
| `github.com/jackc/pgx/v5` | Postgres driver with connection pooling, COPY, LISTEN/NOTIFY |
| `github.com/redis/go-redis/v9` | Redis client with pooling, TLS, pub/sub |
| `github.com/golang-migrate/migrate/v4` | SQL migration runner |

---

## 14. Implementation Order

```
Block P1 — Repository Interfaces + In-Memory Implementations
           persist/interfaces.go, persist/types.go, persist/memory.go
           Tests: full interface test suite using in-memory impl
           Dependencies: none

Block P2 — CPN Topology Serialization
           persist/registry.go, persist/topology.go
           Tests: round-trip marshal/unmarshal, TopologyHash stability
           Dependencies: P1

Block P3 — Postgres Connection Pool + Migrations
           store/postgres/pool.go, store/postgres/migrations/*.sql
           Tests: testcontainers-go integration, migrate up/down
           Dependencies: P1 (schema matches DTOs)

Block P4 — Postgres Event Repository + Batcher
           store/postgres/event.go, store/postgres/event_batcher.go
           Tests: append, query, batch flush, backpressure
           Dependencies: P3

Block P5 — Postgres Session + Message Repository
           store/postgres/session.go
           Tests: CRUD lifecycle, message ordering, expiry
           Dependencies: P3

Block P6 — Redis Session Cache + HITL Repository
           store/redis/session.go, store/redis/hitl.go, store/redis/pool.go
           Tests: cache hit/miss, TTL, HITL enqueue/dequeue
           Dependencies: P3, P5

Block P7 — Postgres Ledger + Intelligence Repositories
           store/postgres/ledger.go, store/postgres/intelligence.go
           Tests: upsert, aggregation, TopFlows ranking
           Dependencies: P3

Block P8 — Postgres Flow Repository + Store Facade
           store/postgres/flow.go, store/store.go
           Tests: topology persistence, graceful shutdown, Health()
           Dependencies: P2, P3-P7
```

---

## 15. What This Document Does Not Include (Future Work)

- Multi-tenancy data isolation (row-level security in Postgres)
- Authentication and authorization layer
- Horizontal scaling (multiple executor processes sharing Redis/Postgres)
- PgBouncer configuration for connection pooling at scale
- Real-time event streaming to external consumers (Kafka/NATS)
- CPN visual editor persistence (topology CRUD API)
- Data export/import for CPN topologies
- Cross-region replication for disaster recovery
