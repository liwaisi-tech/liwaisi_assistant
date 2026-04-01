---
title: "Block P1 — Repository Interfaces + In-Memory Implementations (Updated)"
version: 2.0
date_created: 2026-03-27
last_updated: 2026-04-01
owner: Agentic CPN Team
tags: golang, persistence, interfaces, repository-pattern, testing, spec-driven, block-P1
---

# Introduction

Block P1 defines the **6 persistence repository interfaces**, all DTOs, sentinel errors, and in-memory implementations for the Agentic CPN persistence layer (v1.3). This is the foundation for all subsequent persistence blocks (P2-P8). It introduces **zero external dependencies** and follows **Axiom A13**: the `cpn/` package never imports persistence drivers.

This is **version 2.0** of the P1 specification, updated after an expert panel audit (2026-04-01) that identified 8 discrepancies between the original spec and the current codebase. All changes are annotated with `[UPDATED v2.0]` tags.

**Spec reference:** `.docs/specs/agentic-cpn-v1.3.md` Sections 4, 5, 14

**Specialist Team:**
- **Database Architect**: Schema-driven interface design, query patterns, cursor pagination, upsert semantics
- **Senior Golang Engineer**: Small focused interfaces, `Page[T]` generic for pagination, table-driven tests, compile-time interface assertions
- **DevSecOps Engineer**: No driver imports in `cpn/`, context propagation on every method, error wrapping with sentinels

**Depends on:** Blocks 0-7 (project skeleton + CPN engine core)

---

## 1. Purpose & Scope

### Purpose
Define all persistence contracts as Go interfaces in `cpn/persist/`. Provide in-memory implementations that pass a comprehensive test suite — serving as the reference implementation and test doubles for all subsequent blocks.

### Scope
- **In scope**: 6 repository interfaces, 17 types (16 DTOs + `Page[T]` generic), 9 sentinel errors, 6 in-memory implementations, ~60 test cases, DTO conversion helpers
- **Out of scope**: Postgres implementations (P3-P5, P7-P8), Redis implementations (P6), CPN serialization (P2), external dependencies

### Audience
Implementers of Block P1 and all downstream blocks (P2-P8). Also serves as the canonical reference for DTO ↔ domain type mappings.

---

## 2. Definitions

| Term | Definition |
|------|-----------|
| **Repository** | Go interface abstracting persistence for one domain concern |
| **DTO** | Data Transfer Object — persistence-specific record type decoupled from domain types |
| **Sentinel error** | Package-level `var` created with `errors.New()`, compared via `errors.Is()` |
| **Upsert** | Create if not exists, update (increment) if exists — used by LedgerRepository.Record |
| **Soft delete** | Set `DeletedAt` timestamp instead of removing data — used by FlowRepository.Delete |
| **Cursor pagination** | Opaque string cursor for stateless page traversal — used by EventRepository, FlowRepository |
| **`Page[T]`** | Generic paginated result: `{Items []T, NextCursor string, HasMore bool}` |
| **Axiom A9** | Event Store is append-only — EventRepository has NO Update or Delete methods |
| **Axiom A13** | Persistence is transparent — `cpn/` never imports drivers; all behind interfaces |
| **Domain type** | Types defined in `cpn/` package (e.g., `cpn.Event`, `cpn.Message`, `cpn.ExecutionRecord`) |
| **Persist DTO** | Types defined in `cpn/persist/` package (e.g., `persist.EventRecord`, `persist.MessageRecord`) |
| **Session state bridge** | `[UPDATED v2.0]` Pattern where `SessionRecord.State` and `SessionRecord.LastActivityAt` are populated by the integration layer (SessionService), not directly from `cpn.Session`, because `cpn.Session` does not carry state or activity timestamps |

---

## 3. Requirements, Constraints & Guidelines

### Requirements

#### Package Structure
- **REQ-001**: All files MUST be in `cpn/persist/` sub-package with `package persist`
- **REQ-002**: Module path: `github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist`

#### Interfaces (6 total)
- **REQ-003**: `SessionRepository` MUST define: Create, Get, GetByUserID, AppendMessage, UpdateState, Touch, Close, ListExpired, Delete
- **REQ-004**: `EventRepository` MUST define: Append (variadic), QueryBySession, QueryByCPN, QueryByType, Count — NO Update or Delete (Axiom A9). `[UPDATED v2.0]` QueryBySession, QueryByCPN, QueryByType MUST return `*Page[*EventRecord]` (not plain slices). Count returns `(int64, error)`.
- **REQ-005**: `LedgerRepository` MUST define: Record (upsert), GetBySession, QueryByDate, SetDailyTotal, AggregateByDateRange
- **REQ-006**: `FlowRepository` MUST define: Save, GetByHash, List, UpdateStats, Delete (soft)
- **REQ-007**: `IntelligenceRepository` MUST define: RecordExecution, QueryByRole, Aggregate, TopFlows
- **REQ-008**: `HITLRepository` MUST define: Enqueue, Dequeue, ListPending, Expire
- **REQ-009**: Every method's first parameter MUST be `context.Context`

#### Types (17 total) `[UPDATED v2.0]`
- **REQ-010**: Define: SessionState (3 constants), SessionRecord, MessageRecord, EventRecord, EventQueryOpts, LedgerRecord, LedgerAggregate, FlowRecord, FlowStats, FlowListOpts, ExecutionRecord, RankingMetrics, RankedFlow, HITLPendingRequest, Converter (helper type)
- **REQ-011**: `Page[T any]` generic struct MUST have: `Items []T`, `NextCursor string`, `HasMore bool`
- **REQ-012**: Paginated query methods MUST return `*Page[*RecordType]`

#### `[UPDATED v2.0]` ExecutionRecord DTO Alignment
- **REQ-024**: `persist.ExecutionRecord` MUST include `CPNDepth int` (present in `cpn.ExecutionRecord`, needed for hierarchy queries)
- **REQ-025**: `persist.ExecutionRecord` MUST include `StartedAt time.Time` and `CompletedAt time.Time` (present in `cpn.ExecutionRecord`, replacing the original single `Timestamp` field)
- **REQ-026**: `persist.ExecutionRecord` MUST include `TokensProduced int` (present in `cpn.ExecutionRecord`)
- **REQ-027**: `persist.ExecutionRecord` MUST include `ToolCalls int` (NOT yet tracked in `cpn.executionTracker` — see REQ-030 for prerequisite code change)
- **REQ-028**: `persist.ExecutionRecord` MUST use `DurationMs int64` (converted from `cpn.ExecutionRecord.Duration time.Duration` via `.Milliseconds()`)
- **REQ-029**: `persist.ExecutionRecord` MUST include `ID string` (generated at persistence layer, not from domain)

#### `[UPDATED v2.0]` Prerequisite Code Change
- **REQ-030**: Before P1 can be fully implemented, `cpn.executionTracker` MUST be extended with a `toolCallCount atomic.Int64` field and `RecordTransitionFired` MUST increment it when `kind == NodeKindTool`. This is a **code change in `cpn/flow_metrics.go`**, not in `persist/`.

#### `[UPDATED v2.0]` MessageRecord SessionID
- **REQ-031**: `persist.MessageRecord` MUST include `SessionID string`. The `cpn.Message` type does NOT carry `SessionID`. The integration layer (P8 Store facade or SessionService wrapper) MUST inject `SessionID` when converting `cpn.Message` to `persist.MessageRecord`.

#### `[UPDATED v2.0]` DTO Conversion Helpers
- **REQ-032**: `cpn/persist/` MUST include conversion helper functions: `EventToRecord(sessionID string, e *cpn.Event) (*EventRecord, error)` and `MessageToRecord(sessionID string, m *cpn.Message) *MessageRecord`. These handle: `cpn.Token` serialization to `json.RawMessage`, `any` Payload serialization to `json.RawMessage`, `cpn.NodeKind` to `string` cast, and `SessionID` injection.

#### Errors (9 sentinels)
- **REQ-013**: Define: ErrSessionNotFound, ErrSessionExists, ErrSessionClosed, ErrFlowNotFound, ErrEventAppendFailed, ErrHITLNotFound, ErrHITLExpired, ErrLedgerNotFound, ErrInvalidInput
- **REQ-033**: `[UPDATED v2.0]` `persist.ErrSessionClosed` coexists with `cpn.ErrSessionClosed` (different packages). Document that `cpn.ErrSessionClosed` is for runtime HITL/session operations while `persist.ErrSessionClosed` is for persistence operations on closed/expired sessions. Both use distinct `errors.New()` calls and are NOT equal via `errors.Is()`.

#### In-Memory Implementations
- **REQ-014**: 6 exported types: `MemorySessionRepository`, `MemoryEventRepository`, `MemoryLedgerRepository`, `MemoryFlowRepository`, `MemoryIntelligenceRepository`, `MemoryHITLRepository`
- **REQ-015**: Each with `New` constructor returning concrete type
- **REQ-016**: Each with `Reset()` and `Len() int` test helpers
- **REQ-017**: Compile-time interface assertions: `var _ XxxRepository = (*MemoryXxxRepository)(nil)`
- **REQ-018**: `LedgerRepository.Record` MUST perform upsert: incoming fields are additive to existing (InputTokens += rec.InputTokens, etc.)
- **REQ-019**: `FlowRepository.Delete` MUST be soft delete (set DeletedAt, not remove)
- **REQ-020**: `FlowRepository.GetByHash` and `List` MUST skip soft-deleted records
- **REQ-021**: Cursor pagination: empty cursor starts from beginning, `Limit <= 0` defaults to 100, empty `NextCursor` means no more results
- **REQ-022**: `HITLRepository.Expire` removes entries where `ExpiresAt.Before(time.Now())`
- **REQ-023**: All methods MUST check `ctx.Err()` first and return early if cancelled

### Security Requirements
- **SEC-001**: No imports of `database/sql`, `pgx`, `go-redis`, or any persistence driver
- **SEC-002**: No `panic()`, `log.Fatal()`, or `os.Exit()`
- **SEC-003**: All data structures protected by `sync.RWMutex` for concurrent access

### Constraints
- **CON-001**: Only stdlib imports: `context`, `encoding/json`, `errors`, `fmt`, `sort`, `strconv`, `sync`, `time`
- **CON-002**: No generics for CRUD — interfaces are distinct enough that a generic `Repository[T]` adds complexity without reducing duplication
- **CON-003**: In-memory cursor pagination uses `strconv.Itoa(offset)` — an implementation detail, not a contract
- **CON-004**: Tests use `package persist` (white-box, matching project convention)
- **CON-005**: `[UPDATED v2.0]` Conversion helpers (`EventToRecord`, `MessageToRecord`) MUST import `cpn` package types. This is acceptable because `persist` is a sub-package of `cpn/` and the dependency direction is `cpn/persist → cpn` (never the reverse).

### Guidelines
- **GUD-001**: Each sentinel error doc comment explains when it is returned
- **GUD-002**: In-memory implementations use `map` for keyed lookups, `[]` for ordered collections
- **GUD-003**: Paginated results: use `Page[T]` consistently across EventRepository and FlowRepository
- **GUD-004**: `GetByUserID` returns a plain slice (not paginated) — user sessions are bounded
- **GUD-005**: `[UPDATED v2.0]` The `persist.SessionRecord` DTO combines data from multiple code sources: `cpn.Session` provides ID, UserID, Channel, CreatedAt, Messages; `internal/app.sessionState` provides State; the integration layer manages LastActivityAt and ClosedAt. This is by design — DTOs are persistence-centric, not domain-centric.

---

## 4. Interfaces & Data Contracts

### 4.1 Package Layout

```
cpn/persist/
├── doc.go             — Package documentation (Axiom A13)
├── errors.go          — 9 sentinel errors
├── types.go           — 17 types (16 DTOs + Page[T])
├── interfaces.go      — 6 repository interfaces
├── convert.go         — [UPDATED v2.0] DTO conversion helpers (EventToRecord, MessageToRecord)
├── memory.go          — 6 in-memory implementations + compile-time checks
└── memory_test.go     — ~60 test cases
```

### 4.2 Interfaces

#### SessionRepository (9 methods)
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

#### EventRepository (5 methods) `[UPDATED v2.0]`
```go
// Append-only (Axiom A9). No Update or Delete methods.
type EventRepository interface {
    Append(ctx context.Context, events ...*EventRecord) error
    QueryBySession(ctx context.Context, sessionID string, opts *EventQueryOpts) (*Page[*EventRecord], error)
    QueryByCPN(ctx context.Context, cpnID string, opts *EventQueryOpts) (*Page[*EventRecord], error)
    QueryByType(ctx context.Context, eventType string, from, to time.Time, opts *EventQueryOpts) (*Page[*EventRecord], error)
    Count(ctx context.Context, opts *EventQueryOpts) (int64, error)
}
```

**Change rationale**: The v1.3 spec Section 4.2 returned plain slices. This contradicted the P1 requirement for `Page[T]` pagination. The panel decided `Page[T]` is correct for all query methods because event volumes can be unbounded. `Count` remains non-paginated (single scalar).

#### LedgerRepository (5 methods)
```go
type LedgerRepository interface {
    Record(ctx context.Context, rec *LedgerRecord) error
    GetBySession(ctx context.Context, sessionID string) (*LedgerRecord, error)
    QueryByDate(ctx context.Context, date time.Time) ([]*LedgerRecord, error)
    SetDailyTotal(ctx context.Context, sessionID string, date time.Time, totalUSD float64) error
    AggregateByDateRange(ctx context.Context, from, to time.Time) (*LedgerAggregate, error)
}
```

#### FlowRepository (5 methods)
```go
type FlowRepository interface {
    Save(ctx context.Context, flow *FlowRecord) error
    GetByHash(ctx context.Context, hash string) (*FlowRecord, error)
    List(ctx context.Context, opts *FlowListOpts) (*Page[*FlowRecord], error)
    UpdateStats(ctx context.Context, hash string, stats *FlowStats) error
    Delete(ctx context.Context, hash string) error
}
```

#### IntelligenceRepository (4 methods)
```go
type IntelligenceRepository interface {
    RecordExecution(ctx context.Context, rec *ExecutionRecord) error
    QueryByRole(ctx context.Context, role string, from, to time.Time) ([]*ExecutionRecord, error)
    Aggregate(ctx context.Context, role string, from, to time.Time) (*RankingMetrics, error)
    TopFlows(ctx context.Context, n int) ([]*RankedFlow, error)
}
```

#### HITLRepository (4 methods)
```go
type HITLRepository interface {
    Enqueue(ctx context.Context, req *HITLPendingRequest) error
    Dequeue(ctx context.Context, sessionID, transitionID string) (*HITLPendingRequest, error)
    ListPending(ctx context.Context, sessionID string) ([]*HITLPendingRequest, error)
    Expire(ctx context.Context, olderThan time.Duration) (int64, error)
}
```

### 4.3 Page[T] Generic

```go
// Page holds a paginated result set with cursor-based navigation.
type Page[T any] struct {
    Items      []T
    NextCursor string // empty when no more results
    HasMore    bool
}
```

### 4.4 Sentinel Errors

```go
var (
    ErrSessionNotFound   = errors.New("persist: session not found")
    ErrSessionExists     = errors.New("persist: session already exists")
    ErrSessionClosed     = errors.New("persist: session is closed or expired")
    ErrFlowNotFound      = errors.New("persist: flow not found")
    ErrEventAppendFailed = errors.New("persist: event append failed")
    ErrHITLNotFound      = errors.New("persist: HITL request not found")
    ErrHITLExpired       = errors.New("persist: HITL request expired")
    ErrLedgerNotFound    = errors.New("persist: ledger record not found")
    ErrInvalidInput      = errors.New("persist: invalid input")
)
```

**`[UPDATED v2.0]` Note on ErrSessionClosed**: `persist.ErrSessionClosed` is a distinct error from `cpn.ErrSessionClosed`. The `cpn` version (`"session is closed"`) is returned by runtime operations (e.g., HITL resolve on a closed session). The `persist` version (`"persist: session is closed or expired"`) is returned by persistence operations (e.g., AppendMessage on a closed/expired SessionRecord). They are NOT equal via `errors.Is()` because they are separate `errors.New()` calls in separate packages.

### 4.5 Persistence DTOs `[UPDATED v2.0]`

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
// NOTE [v2.0]: cpn.Session does NOT have State, LastActivityAt, or ClosedAt.
// State comes from internal/app.sessionState. LastActivityAt is tracked by
// the persistence integration layer (Touch updates it). ClosedAt is set
// by the Close method. The SessionRecord is a persistence-centric DTO that
// combines data from multiple sources.

type MessageRecord struct {
    ID, SessionID, Role, Content, CPNID, CPNRole string
    CPNDepth  int
    Timestamp time.Time
}
// NOTE [v2.0]: cpn.Message does NOT have SessionID. The integration layer
// (P8 Store facade or SessionService wrapper) MUST inject SessionID when
// converting cpn.Message to MessageRecord. Use MessageToRecord helper.

type EventRecord struct {
    ID, Type, SessionID, CPNID, CPNRole, TransitionID, TransitionKind string
    CPNDepth      int
    TokenSnapshot json.RawMessage
    Payload       json.RawMessage
    Timestamp     time.Time
}
// NOTE [v2.0]: cpn.Event has TransitionKind as NodeKind (typed string),
// Token as *Token (pointer), and Payload as any (interface{}).
// Conversion requires:
//   - TransitionKind: string(event.TransitionKind)
//   - TokenSnapshot: json.Marshal(event.Token) — nil Token becomes null
//   - Payload: json.Marshal(event.Payload) — nil Payload becomes null
// Use EventToRecord helper.

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

type FlowListOpts struct {
    Role   string
    Limit  int
    Cursor string
}

// [UPDATED v2.0] ExecutionRecord DTO — aligned with cpn.ExecutionRecord fields
type ExecutionRecord struct {
    ID               string    // Generated at persistence layer (UUID)
    CPNID            string    // From cpn.ExecutionRecord.CPNID
    CPNRole          string    // From cpn.ExecutionRecord.CPNRole
    CPNDepth         int       // [NEW v2.0] From cpn.ExecutionRecord.CPNDepth
    SessionID        string    // From cpn.ExecutionRecord.SessionID
    TransitionsFired int       // From cpn.ExecutionRecord.TransitionsFired
    LLMCalls         int       // From cpn.ExecutionRecord.LLMCallCount
    ToolCalls        int       // [REQUIRES REQ-030] From cpn.executionTracker.toolCallCount
    TokensProduced   int       // [NEW v2.0] From cpn.ExecutionRecord.TokensProduced
    TotalCostUSD     float64   // From cpn.ExecutionRecord.TotalCostUSD
    DurationMs       int64     // Converted: cpn.ExecutionRecord.Duration.Milliseconds()
    Success          bool      // From cpn.ExecutionRecord.Success
    StartedAt        time.Time // [NEW v2.0] From cpn.ExecutionRecord.StartedAt
    CompletedAt      time.Time // [NEW v2.0] From cpn.ExecutionRecord.CompletedAt
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
// NOTE [v2.0]: HITLPendingRequest.Proposal is populated by the integration
// layer (P6/P8). The current HITL flow in cpn/hitl.go blocks on a channel
// without explicitly capturing the proposal. The integration layer must
// capture the output tokens from the HITL transition's input places (the
// proposed action) and serialize them to json.RawMessage before enqueuing.
// This is an integration concern, not a P1 concern — P1 only defines the DTO.
```

### 4.6 DTO Conversion Helpers `[NEW v2.0]`

```go
// EventToRecord converts a domain cpn.Event to a persistence EventRecord.
// Requires sessionID because cpn.Event may not always carry it (defensive).
// Returns error if Token or Payload serialization fails.
func EventToRecord(sessionID string, e *cpn.Event) (*EventRecord, error) {
    var tokenSnap json.RawMessage
    if e.Token != nil {
        b, err := json.Marshal(e.Token)
        if err != nil {
            return nil, fmt.Errorf("marshal token: %w", err)
        }
        tokenSnap = b
    }
    var payload json.RawMessage
    if e.Payload != nil {
        b, err := json.Marshal(e.Payload)
        if err != nil {
            return nil, fmt.Errorf("marshal payload: %w", err)
        }
        payload = b
    }
    sid := sessionID
    if e.SessionID != "" {
        sid = e.SessionID
    }
    return &EventRecord{
        ID:             e.ID,
        Type:           string(e.Type),
        SessionID:      sid,
        CPNID:          e.CPNID,
        CPNRole:        e.CPNRole,
        CPNDepth:       e.CPNDepth,
        TransitionID:   e.TransitionID,
        TransitionKind: string(e.TransitionKind),
        TokenSnapshot:  tokenSnap,
        Payload:        payload,
        Timestamp:      e.Timestamp,
    }, nil
}

// MessageToRecord converts a domain cpn.Message to a persistence MessageRecord.
// Injects sessionID because cpn.Message does not carry it.
func MessageToRecord(sessionID string, m *cpn.Message) *MessageRecord {
    return &MessageRecord{
        ID:        m.ID,
        SessionID: sessionID,
        Role:      string(m.Role),
        Content:   m.Content,
        CPNID:     m.CPNID,
        CPNRole:   m.CPNRole,
        CPNDepth:  m.CPNDepth,
        Timestamp: m.Timestamp,
    }
}
```

---

## 5. Acceptance Criteria

- **AC-001**: `go build ./cpn/persist/` succeeds with zero external deps
- **AC-002**: `go vet ./cpn/persist/` passes
- **AC-003**: `golangci-lint run ./cpn/persist/` passes (project `.golangci.yml`)
- **AC-004**: `go test -race -count=1 ./cpn/persist/` — all pass, no races
- **AC-005**: Test coverage >= 90% for `memory.go` and `convert.go`
- **AC-006**: All 6 compile-time interface assertions pass
- **AC-007**: No driver imports in any file
- **AC-008**: Every exported symbol has a doc comment
- **AC-009**: Every method's first param is `context.Context`
- **AC-010**: `EventRepository` has no Update/Delete methods (Axiom A9)
- **AC-011**: `FlowRepository.Delete` is soft-delete only
- **AC-012**: `LedgerRepository.Record` upserts (increments on existing)
- **AC-013**: Pagination returns empty `NextCursor` when no more results
- **AC-014**: 9 sentinel errors are distinct (`errors.Is` pairwise check)
- **AC-015**: Cancelled context returns `context.Canceled` from all methods
- **AC-016**: `[UPDATED v2.0]` `EventRepository.QueryBySession` returns `*Page[*EventRecord]`, not `[]*EventRecord`
- **AC-017**: `[UPDATED v2.0]` `persist.ExecutionRecord` includes `CPNDepth`, `StartedAt`, `CompletedAt`, `TokensProduced` fields
- **AC-018**: `[UPDATED v2.0]` `persist.MessageRecord` includes `SessionID` field
- **AC-019**: `[UPDATED v2.0]` `EventToRecord` correctly serializes `cpn.Token` to `json.RawMessage`
- **AC-020**: `[UPDATED v2.0]` `MessageToRecord` correctly injects `SessionID`
- **AC-021**: `[UPDATED v2.0]` `persist.ErrSessionClosed` is NOT equal to `cpn.ErrSessionClosed` via `errors.Is()`

---

## 6. Test Automation Strategy

### Test File: `cpn/persist/memory_test.go`

### Test Structure (grouped by interface, subtests per method) `[UPDATED v2.0]`

| Test Function | Subtests | Count |
|---------------|----------|-------|
| `TestSentinelErrors` | Distinct, WrapCorrectly, NotEqualToCPNErrors | 3 |
| `TestMemorySessionRepository` | Create, Create_duplicate, Get, Get_not_found, GetByUserID, AppendMessage, AppendMessage_closed, UpdateState, Touch, Close, ListExpired, Delete | 12 |
| `TestMemoryEventRepository` | Append_single, Append_batch, Append_nil, QueryBySession, QueryBySession_pagination, QueryByCPN, QueryByType, Count, Count_filtered | 9 |
| `TestMemoryLedgerRepository` | Record_create, Record_upsert, GetBySession, GetBySession_not_found, QueryByDate, SetDailyTotal, Aggregate | 7 |
| `TestMemoryFlowRepository` | Save, GetByHash, GetByHash_not_found, GetByHash_soft_deleted, List, List_pagination, List_by_role, UpdateStats, Delete_soft | 9 |
| `TestMemoryIntelligenceRepository` | RecordExecution, RecordExecution_with_depth, QueryByRole, Aggregate, Aggregate_empty, TopFlows | 6 |
| `TestMemoryHITLRepository` | Enqueue, Dequeue, Dequeue_not_found, ListPending, Expire | 5 |
| `TestConcurrent_AllRepositories` | Concurrent read/write on each repo | 6 |
| `TestEventToRecord` | WithToken, WithPayload, NilToken, NilPayload, SessionIDFallback | 5 |
| `TestMessageToRecord` | Basic, SessionIDInjection | 2 |
| **Total** | | **~64** |

### Framework
- Go standard `testing` package only (no testify — zero external deps)
- Table-driven where multiple input/output cases exist
- `errors.Is()` for sentinel comparison
- `-race` flag via Makefile

---

## 7. Rationale & Context

### Why In-Memory First
In-memory implementations serve as: (a) reference implementation defining correct behavior, (b) test doubles for all CPN engine tests, (c) development mode (no external services needed). The full test suite on in-memory validates the interface contracts before any database code is written.

### Why No Generic Repository
The 6 interfaces have fundamentally different semantics: append-only (Event), upsert (Ledger), queue (HITL), soft-delete (Flow), CRUD (Session), analytics (Intelligence). A generic `Repository[T]` would need to abstract over these differences, adding complexity without reducing code.

### Why `Page[T]` Generic
Pagination is the cross-cutting concern shared by Event and Flow queries. A single generic type avoids duplicating `EventPage`, `FlowPage`, etc. The type parameter is always a pointer to a record struct.

### `[UPDATED v2.0]` Why Page[T] for EventRepository (resolving v1.3 contradiction)
The v1.3 spec Section 4.2 showed plain slice returns, but event volumes are unbounded (a single session can generate thousands of events). Forcing callers to paginate prevents memory issues. The P1 issue's original `Page[T]` design was correct.

### Why Sentinel Errors (Not Error Types)
Follows the project convention from `cpn/errors.go`. Sentinels with `errors.Is()` are simpler than typed errors for the 9 expected failure modes. Context is added via `fmt.Errorf("%w: detail", ErrXxx)`.

### `[UPDATED v2.0]` Why DTO Conversion Helpers
Domain types (`cpn.Event`, `cpn.Message`) use Go-idiomatic types: `any` for Payload, `*Token` for snapshots, `NodeKind` for transition kinds. Persistence DTOs use serializable types: `json.RawMessage`, `string`. This conversion is NOT automatic and requires explicit helper functions. Placing them in `cpn/persist/` keeps the conversion logic close to the DTOs and avoids leaking persistence concerns into the `cpn/` package.

### `[UPDATED v2.0]` Why Session State Lives Outside cpn.Session
The `cpn.Session` struct is a runtime binding (user ↔ CPN). It holds the `Root *CPN` pointer and HITL channels — runtime-only concerns. State management (idle, running, waiting, completed, failed) is handled by `SessionService` because it depends on CPN execution lifecycle, not session creation. The `persist.SessionRecord` DTO correctly unifies both into a single persistence-friendly structure.

---

## 8. Dependencies & External Integrations

### Block Dependencies
- **DEP-001**: Block 0 (#23) — Project skeleton (go.mod, Makefile, .golangci.yml)
- **DEP-002**: Block 1 (#24) — Reference for sentinel error pattern and test style
- **DEP-003**: `[UPDATED v2.0]` CPN engine types — `cpn.Event`, `cpn.Message`, `cpn.Token`, `cpn.NodeKind`, `cpn.EventType`, `cpn.MessageRole` are imported by conversion helpers

### Technology Dependencies
- **PLT-001**: Go 1.25 — Generics for `Page[T]`, multi-error unwrap
- **PLT-002**: Go stdlib only — zero external deps

### `[UPDATED v2.0]` Prerequisite Code Change
- **PRE-001**: `cpn/flow_metrics.go` must add `toolCallCount atomic.Int64` to `executionTracker` and increment it in `RecordTransitionFired` when `kind == NodeKindTool`. The `Finalize` method must include the count in `ExecutionRecord`. This change is needed before `persist.ExecutionRecord.ToolCalls` can be populated.

### Depended On By
- **All persistence blocks P2-P8** — implement these interfaces
- **Future CPN engine integration** — uses in-memory implementations as defaults

---

## 9. Examples & Edge Cases

### 9.1 Session Lifecycle
```go
repo := NewMemorySessionRepository()
ctx := context.Background()

repo.Create(ctx, &SessionRecord{ID: "s1", UserID: "u1", Channel: "web", State: SessionActive})
repo.AppendMessage(ctx, "s1", &MessageRecord{ID: "m1", SessionID: "s1", Role: "user", Content: "hello"})
repo.Touch(ctx, "s1") // updates LastActivityAt
repo.Close(ctx, "s1") // sets State=SessionClosed, ClosedAt=now

// After close, append returns ErrSessionClosed
err := repo.AppendMessage(ctx, "s1", &MessageRecord{...})
// errors.Is(err, ErrSessionClosed) == true
```

### 9.2 Event Pagination `[UPDATED v2.0]`
```go
repo := NewMemoryEventRepository()
// Append 250 events...
page1, _ := repo.QueryBySession(ctx, "s1", &EventQueryOpts{Limit: 100})
// page1.Items has 100 events, page1.HasMore == true, page1.NextCursor != ""
page2, _ := repo.QueryBySession(ctx, "s1", &EventQueryOpts{Limit: 100, Cursor: page1.NextCursor})
// page2.Items has 100 events, page2.HasMore == true
page3, _ := repo.QueryBySession(ctx, "s1", &EventQueryOpts{Limit: 100, Cursor: page2.NextCursor})
// page3.Items has 50 events, page3.HasMore == false, page3.NextCursor == ""
```

### 9.3 DTO Conversion `[NEW v2.0]`
```go
// Convert domain Event to persistence EventRecord
event := &cpn.Event{
    ID:             "evt-1",
    Type:           cpn.EventTransitionFired,
    SessionID:      "s1",
    CPNID:          "cpn-root",
    CPNDepth:       0,
    CPNRole:        "coordinator",
    TransitionID:   "t-llm",
    TransitionKind: cpn.NodeKindLLM,
    Token:          &cpn.Token{Color: cpn.ColorString, Payload: "hello"},
    Payload:        map[string]any{"model": "gpt-4"},
    Timestamp:      time.Now(),
}
rec, err := EventToRecord("s1", event)
// rec.Type == "transition_fired"
// rec.TransitionKind == "llm"
// rec.TokenSnapshot == json encoded Token
// rec.Payload == json encoded map

// Convert domain Message to persistence MessageRecord
msg := &cpn.Message{
    ID:       "m1",
    Role:     cpn.RoleUser,
    Content:  "hello",
    CPNID:    "cpn-root",
    CPNRole:  "coordinator",
    CPNDepth: 0,
}
msgRec := MessageToRecord("s1", msg)
// msgRec.SessionID == "s1" (injected)
// msgRec.Role == "user"
```

### 9.4 Edge Cases

| Edge Case | Expected Behavior |
|-----------|-------------------|
| Create session with empty ID | ErrInvalidInput |
| Append event with nil EventRecord | ErrInvalidInput |
| GetByUserID for user with no sessions | Empty slice, nil error |
| Pagination with Limit=0 | Defaults to 100 |
| Pagination with invalid cursor | Starts from beginning (cursor=0) |
| FlowRepository.GetByHash after soft delete | ErrFlowNotFound |
| LedgerRepository.Record twice for same session | Second call increments tokens/cost |
| HITLRepository.Expire with no expired requests | Returns 0, nil |
| Cancelled context on any method | Returns context.Canceled |
| `[v2.0]` EventToRecord with nil Token | TokenSnapshot is nil (not json null) |
| `[v2.0]` EventToRecord with non-serializable Payload | Returns error |
| `[v2.0]` MessageToRecord with empty SessionID param | MessageRecord.SessionID is empty (caller error) |
| `[v2.0]` ExecutionRecord with zero Duration | DurationMs is 0 |

---

## 10. Validation Criteria

```bash
cd back/go-assistant

# 1. Package compiles
go build ./cpn/persist/

# 2. Vet passes
go vet ./cpn/persist/

# 3. Lint passes
golangci-lint run ./cpn/persist/

# 4. Tests pass with race detector
go test -race -count=1 -v ./cpn/persist/...

# 5. Coverage check
go test -race -coverprofile=persist_coverage.out ./cpn/persist/...
go tool cover -func=persist_coverage.out | grep -E "(memory|convert)\.go"

# 6. No driver imports
! grep -rn "database/sql\|pgx\|go-redis" cpn/persist/

# 7. Interface compliance
grep "var _ .*Repository = " cpn/persist/memory.go

# 8. [v2.0] Verify Page[T] return types
grep -n "Page\[" cpn/persist/interfaces.go

# 9. [v2.0] Verify conversion helpers exist
grep -n "func EventToRecord\|func MessageToRecord" cpn/persist/convert.go

# 10. [v2.0] Verify ExecutionRecord has new fields
grep -n "CPNDepth\|StartedAt\|CompletedAt\|TokensProduced" cpn/persist/types.go
```

---

## 11. Related Specifications / Further Reading

- [Agentic CPN v1.3 — Persistence Layer](../agentic-cpn-v1.3.md) — Sections 4, 5, 14
- [Block 1: Base Types](#24) — Sentinel error pattern reference
- [Block P2: CPN Topology Serialization](block-p2-cpn-topology-serialization.md) — Next Block
- [Block P5: Postgres Session + Message Repository](block-p5-postgres-session-message.md) — Session state bridge consumer
- [Block P8: Flow Repository + Store Facade](block-p8-flow-repository-store-facade.md) — Integration layer that bridges Session + sessionState → SessionRecord

---

## Appendix A: Change Log from v1.0

| Change | Reason | Impact |
|--------|--------|--------|
| ExecutionRecord DTO: added CPNDepth, StartedAt, CompletedAt, TokensProduced | Fields exist in `cpn.ExecutionRecord` but were missing from DTO | P1, P7 |
| EventRepository returns `*Page[*EventRecord]` | Resolved contradiction between v1.3 Section 4.2 and P1 issue | P1, P4 |
| MessageRecord: added SessionID | `cpn.Message` lacks SessionID; needed for FK in Postgres | P1, P5 |
| Added convert.go with EventToRecord, MessageToRecord | Domain → DTO conversion is non-trivial (Token serialization, type casts) | P1 |
| Documented Session state bridge pattern | `cpn.Session` has no State/LastActivityAt/ClosedAt fields | P1, P5, P6 |
| Documented ErrSessionClosed coexistence | `cpn.ErrSessionClosed` and `persist.ErrSessionClosed` are distinct | P1 |
| Documented HITLPendingRequest.Proposal source | HITL channel-based flow doesn't capture proposals — integration layer concern | P1, P6 |
| Added REQ-030 prerequisite: toolCallCount in executionTracker | ToolCalls field in DTO requires code change in CPN engine | P1 |
| Test count increased from ~55 to ~64 | New conversion helper tests + additional edge cases | P1 |

---

## Implementation File Checklist

- [ ] `back/go-assistant/cpn/persist/doc.go` — Package documentation
- [ ] `back/go-assistant/cpn/persist/errors.go` — 9 sentinel errors with doc comments
- [ ] `back/go-assistant/cpn/persist/types.go` — 17 types: 16 DTOs + Page[T] generic
- [ ] `back/go-assistant/cpn/persist/interfaces.go` — 6 repository interfaces
- [ ] `back/go-assistant/cpn/persist/convert.go` — `[NEW v2.0]` EventToRecord, MessageToRecord helpers
- [ ] `back/go-assistant/cpn/persist/memory.go` — 6 in-memory implementations + Reset/Len helpers + compile-time checks
- [ ] `back/go-assistant/cpn/persist/memory_test.go` — ~64 test cases across all repositories + conversion helpers
- [ ] `back/go-assistant/cpn/flow_metrics.go` — `[PREREQUISITE v2.0]` Add toolCallCount to executionTracker
- [ ] Run `go build ./cpn/persist/` — compiles
- [ ] Run `go vet ./cpn/persist/` — passes
- [ ] Run `golangci-lint run ./cpn/persist/` — zero findings
- [ ] Run `go test -race -count=1 ./cpn/persist/` — all pass
- [ ] Verify: No driver imports
- [ ] Verify: All interface assertions compile
- [ ] Verify: EventRepository has no Update/Delete
- [ ] Verify: EventRepository returns Page[T]
- [ ] Verify: FlowRepository.Delete is soft-delete
- [ ] Verify: LedgerRepository.Record upserts
- [ ] Verify: Pagination with Page[T] works
- [ ] Verify: Sentinel errors are distinct
- [ ] Verify: Cancelled context returns context.Canceled
- [ ] Verify: EventToRecord serializes Token correctly
- [ ] Verify: MessageToRecord injects SessionID
- [ ] Verify: ExecutionRecord has CPNDepth, StartedAt, CompletedAt, TokensProduced
