---
title: "Block P2 — CPN Topology Serialization (Updated)"
version: 2.0
date_created: 2026-03-27
last_updated: 2026-04-01
owner: Agentic CPN Team
tags: golang, persistence, serialization, function-registry, topology, spec-driven, block-P2
---

# Introduction

Block P2 implements the Function Registry pattern (Axiom A15) for CPN topology serialization. Go functions (`Guard`, `Executor`, `RetryOn`, `SubNetFactory`, `EventFilter`, `ValidateFunc`, `OnSuccess`) cannot be marshaled; the registry maps string names to func values at startup. Topologies are stored as JSON with func fields replaced by registry key strings.

This is **version 2.0** of the P2 specification, updated after an expert panel audit (2026-04-01) that found the original `TransitionTopology` captured only ~30% of the actual `Transition` fields, making round-trip serialization impossible. All changes are annotated with `[UPDATED v2.0]` tags.

**Spec reference:** `.docs/specs/agentic-cpn-v1.3.md` Sections 6.1-6.3

**Specialist Team:**
- **Database Architect**: Topology JSON schema, hash stability, JSONB storage design
- **Senior Golang Engineer**: FuncRegistry concurrency, reverse lookup, type-safe registration, compile-time safety
- **Software Architect**: Complete Transition field coverage, LLMConfig/HITLConfig/ValidateConfig serialization

**Depends on:** P1 (#55)

---

## 1. Purpose & Scope

### Purpose
Enable CPN topologies to be serialized to JSON and deserialized back to live `*CPN` instances. This is required for: (a) persisting crystallized flows in Postgres (P8), (b) computing deterministic topology hashes for Flow Intelligence, (c) detecting topology changes across deployments.

### Scope
- **In scope**: FuncRegistry with 7 func kinds, topology structs for CPN/Place/Transition, MarshalCPN/UnmarshalCPN, TopologyHash, LLMConfig/HITLConfig/ValidateConfig serialization
- **Out of scope**: Postgres storage (P8), Flow Intelligence queries (P7), runtime state serialization (tokens, circuit breaker state)

### Audience
Implementers of Block P2 and downstream blocks P4 (event batcher topology tagging), P7 (execution record topology linking), P8 (flow repository).

---

## 2. Definitions

| Term | Definition |
|------|-----------|
| **FuncRegistry** | Thread-safe map of string names to Go function values, used for (de)serialization |
| **Topology** | Serializable snapshot of a CPN's structural definition (places, transitions, arcs) — excludes runtime state |
| **TopologyHash** | Deterministic SHA-256 hex string computed from a topology's structural fields |
| **Round-trip** | MarshalCPN → JSON → UnmarshalCPN produces a structurally equivalent `*CPN` |
| **Reverse lookup** | Given a func value, find its registered name — required by MarshalCPN |
| **Axiom A15** | Func fields are registered, not serialized. String names replace func values in JSON. |
| **Runtime-only field** | Field that exists only during execution and is NOT serialized (e.g., `CircuitBreakerState`, `HITLConfig.Channel`) |

---

## 3. Requirements, Constraints & Guidelines

### Requirements

#### Package Structure
- **REQ-001**: All files MUST be in `cpn/persist/` sub-package with `package persist`
- **REQ-002**: Module path: `github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist`

#### FuncRegistry `[UPDATED v2.0]` — 7 Func Kinds
- **REQ-003**: FuncRegistry MUST store 7 maps (one per func kind), all protected by `sync.RWMutex`:
  1. `guards` — `map[string]func([]*cpn.Token) bool`
  2. `executors` — `map[string]func(context.Context, cpn.Token) (cpn.Token, error)`
  3. `retryOns` — `map[string]func(error, int) bool`
  4. `factories` — `map[string]func() *cpn.CPN`
  5. `eventFilters` — `[NEW v2.0]` `map[string]func(cpn.Event) bool`
  6. `validateFuncs` — `[NEW v2.0]` `map[string]func(any) error`
  7. `onSuccessFuncs` — `[NEW v2.0]` `map[string]func(any) any`
- **REQ-004**: `RegisterXxx(name, fn)` panics on duplicate name (fail-fast at startup)
- **REQ-005**: `LookupXxx(name)` returns `(fn, bool)` — no panic on miss
- **REQ-006**: `ReverseLookupXxx(fn)` returns `(name, bool)` — required by MarshalCPN to resolve func → name
- **REQ-007**: `DefaultRegistry` is a package-level `var` initialized with `NewFuncRegistry()`

#### `[UPDATED v2.0]` TransitionTopology — Complete Field Coverage
- **REQ-008**: `TransitionTopology` MUST capture ALL serializable fields from `cpn.Transition`:

| Transition Field | TransitionTopology Field | Type | Notes |
|---|---|---|---|
| ID | ID | string | Direct |
| Kind | Kind | string | `string(t.Kind)` |
| InputPlaces | InputPlaces | []string | Direct |
| OutputPlaces | OutputPlaces | []string | Direct |
| ErrorPlace | ErrorPlace | string | Direct |
| Guard | GuardFunc | string | Registry name |
| SystemPrompt | SystemPrompt | string | `[NEW v2.0]` Direct |
| ToolName | ToolName | string | `[NEW v2.0]` Direct |
| Executor | ExecutorFunc | string | Registry name |
| LLMConfig | LLMConfig | *LLMConfigTopology | `[NEW v2.0]` Nested struct |
| LLMTools | LLMTools | []string | `[NEW v2.0]` Direct |
| ValidateConfig | ValidateConfig | *ValidateConfigTopology | `[NEW v2.0]` Nested struct |
| SubNet | SubNetTopology | *CPNTopology | Recursive (nil if factory used) |
| SubNetFactory | FactoryFunc | string | Registry name |
| HITLConfig | HITLConfig | *HITLConfigTopology | `[NEW v2.0]` Nested struct |
| ObservedCPNID | ObservedCPNID | string | `[NEW v2.0]` Direct |
| EventFilter | EventFilterFunc | string | `[NEW v2.0]` Registry name |
| Retry | Retry | *RetryPolicyTopology | With CB config |

- **REQ-009**: Fields that are runtime-only MUST NOT be serialized:
  - `Transition.cbState` (CircuitBreakerState) — runtime circuit breaker state, reconstructed from config
  - `HITLConfig.Channel` (chan Token) — runtime channel, rewired by SessionService
  - `ValidateConfig.Schema` (any) — type reference, handled via registry as `schemaFunc`

#### `[UPDATED v2.0]` LLMConfigTopology
- **REQ-010**: `LLMConfigTopology` MUST capture ALL `cpn.LLMConfig` fields. Since ALL LLMConfig fields are primitive types or serializable structs, this is a direct 1:1 mapping:

```go
type LLMConfigTopology struct {
    Model          string
    FallbackModels []string
    Endpoint       string // string(LLMEndpoint)
    MaxTokens      int
    Temperature    float64
    StreamOutput   bool
    RequireJSON    bool
    JSONSchema     *JSONSchemaConfigTopology // nullable
    Budget         float64
    Provider       *ProviderConfigTopology   // nullable
    Trace          *TraceConfigTopology      // nullable
    Plugins        []string
    Reasoning      *ReasoningConfigTopology  // nullable
    CacheControl   *CacheControlConfigTopology // nullable
    SkipHistory    bool
}
```

- **REQ-011**: Sub-types (`JSONSchemaConfigTopology`, `ProviderConfigTopology`, `TraceConfigTopology`, `ReasoningConfigTopology`, `CacheControlConfigTopology`) mirror their `cpn` counterparts field-for-field. `json.RawMessage` fields (e.g., `JSONSchemaConfig.Schema`) are stored as `json.RawMessage` in topology (already serializable).

#### `[NEW v2.0]` ValidateConfigTopology
- **REQ-012**: `ValidateConfigTopology` MUST separate serializable and func fields:

```go
type ValidateConfigTopology struct {
    // Func fields — stored as registry names
    SchemaFunc      string // [NEW v2.0] Registry name for Schema (type reference)
    ValidateFunc    string // [NEW v2.0] Registry name for ValidateFunc
    OnSuccessFunc   string // [NEW v2.0] Registry name for OnSuccess
    // Serializable fields
    MaxCorrections  int
    CorrectionLLMID string
}
```

- **REQ-013**: `ValidateConfig.Schema` is an `any` used as a type reference for JSON round-trip validation. For registry purposes, it is wrapped: the register function stores a factory `func() any` that returns a zero-value of the target type. The topology stores the registry name. On unmarshal, the factory is called to reconstruct the Schema value.

#### `[NEW v2.0]` HITLConfigTopology
- **REQ-014**: `HITLConfigTopology` MUST capture serializable fields only:

```go
type HITLConfigTopology struct {
    Prompt          string
    RevisionLoop    bool
    CorrectionLLMID string
    MaxRevisions    int
    // Channel is runtime-only — NOT serialized
}
```

#### `[UPDATED v2.0]` RetryPolicyTopology
- **REQ-015**: `RetryPolicyTopology` MUST include CircuitBreaker config:

```go
type RetryPolicyTopology struct {
    MaxAttempts    int
    InitialWaitMs  int64 // Duration.Milliseconds()
    MaxWaitMs      int64 // Duration.Milliseconds()
    Multiplier     float64
    RetryOnFunc    string // Registry name
    CircuitBreaker *CircuitBreakerConfigTopology // [NEW v2.0] nullable
}

type CircuitBreakerConfigTopology struct {
    FailureThreshold int
    OpenDurationMs   int64 // Duration.Milliseconds()
}
```

#### Marshal / Unmarshal
- **REQ-016**: `MarshalCPN(c *cpn.CPN, registry *FuncRegistry) (*CPNTopology, error)` resolves each func to its registered name via reverse lookup
- **REQ-017**: `UnmarshalCPN(t *CPNTopology, registry *FuncRegistry) (*cpn.CPN, error)` reconstructs live `*CPN` from topology + registry
- **REQ-018**: `MarshalCPN` returns error if any func is set (non-nil) but not found in registry
- **REQ-019**: `MarshalCPN` silently skips nil funcs (empty string in topology)
- **REQ-020**: `UnmarshalCPN` returns error if a topology func name is non-empty but not found in registry
- **REQ-021**: `UnmarshalCPN` sets runtime-only fields to zero values: `cbState = nil` (reconstructed when CPN starts), `HITLConfig.Channel = nil` (rewired by SessionService)

#### TopologyHash
- **REQ-022**: `TopologyHash(t *CPNTopology) string` produces deterministic SHA-256 hex
- **REQ-023**: TopologyHash ignores runtime state (token counts, CPN State, Mode)
- **REQ-024**: TopologyHash is stable across Go process restarts (deterministic JSON serialization with sorted keys)
- **REQ-025**: TopologyHash includes: place IDs/colors/spaces, transition IDs/kinds/arcs/func names, LLMConfig fields, ValidateConfig fields, HITLConfig fields, RetryPolicy fields

#### Zero External Dependencies
- **REQ-026**: No imports of `store/` or external drivers
- **REQ-027**: Only stdlib imports + `cpn` package types

### Security Requirements
- **SEC-001**: `panic` on duplicate registration is acceptable (startup-only, fail-fast)
- **SEC-002**: No reflection-based func comparison — use explicit reverse maps
- **SEC-003**: TopologyHash uses `crypto/sha256` (stdlib)

### Constraints
- **CON-001**: Func comparison in Go is unreliable (`reflect.DeepEqual` on funcs is always false). Reverse lookup MUST use a parallel `map[uintptr]string` where the key is `reflect.ValueOf(fn).Pointer()`.
- **CON-002**: `ValidateConfig.Schema` (any type) cannot be directly serialized. It is registered as a "schema factory" — `func() any` that returns a zero-value of the target type.
- **CON-003**: `json.RawMessage` fields in LLMConfig sub-types (e.g., `JSONSchemaConfig.Schema`) are already serializable and pass through unchanged.
- **CON-004**: Tests use `package persist` (white-box, matching project convention)

### Guidelines
- **GUD-001**: Register all funcs in `cmd/server/main.go` at startup, before any CPN construction
- **GUD-002**: Use `Must` prefix for registration helpers that panic: `MustRegisterGuard`, etc.
- **GUD-003**: Topology struct field names match their source struct field names where possible
- **GUD-004**: `[NEW v2.0]` When adding new func fields to Transition in the future, add a corresponding registry kind and topology field — this is the extension pattern

---

## 4. Interfaces & Data Contracts

### 4.1 Package Layout

```
cpn/persist/
├── ... (P1 files)
├── registry.go          — FuncRegistry, NewFuncRegistry, DefaultRegistry, 7 register/lookup pairs
├── topology.go          — CPNTopology, PlaceTopology, TransitionTopology, config topologies,
│                          MarshalCPN, UnmarshalCPN, TopologyHash
├── registry_test.go     — Registry CRUD + concurrency
└── topology_test.go     — Round-trip + hash stability
```

### 4.2 FuncRegistry `[UPDATED v2.0]`

```go
type FuncRegistry struct {
    // 7 forward maps: name → func
    guards       map[string]func([]*cpn.Token) bool
    executors    map[string]func(context.Context, cpn.Token) (cpn.Token, error)
    retryOns     map[string]func(error, int) bool
    factories    map[string]func() *cpn.CPN
    eventFilters map[string]func(cpn.Event) bool         // [NEW v2.0]
    validateFns  map[string]func(any) error               // [NEW v2.0]
    onSuccessFns map[string]func(any) any                 // [NEW v2.0]
    schemaFns    map[string]func() any                    // [NEW v2.0] Schema factory

    // 7 reverse maps: func pointer → name (for MarshalCPN)
    guardRev       map[uintptr]string
    executorRev    map[uintptr]string
    retryOnRev     map[uintptr]string
    factoryRev     map[uintptr]string
    eventFilterRev map[uintptr]string   // [NEW v2.0]
    validateFnRev  map[uintptr]string   // [NEW v2.0]
    onSuccessFnRev map[uintptr]string   // [NEW v2.0]
    // schemaFns has no reverse map — Schema is any, not a func

    mu sync.RWMutex
}

var DefaultRegistry = NewFuncRegistry()

// Registration methods (panic on duplicate name)
func (r *FuncRegistry) RegisterGuard(name string, fn func([]*cpn.Token) bool)
func (r *FuncRegistry) RegisterExecutor(name string, fn func(context.Context, cpn.Token) (cpn.Token, error))
func (r *FuncRegistry) RegisterRetryOn(name string, fn func(error, int) bool)
func (r *FuncRegistry) RegisterSubNetFactory(name string, fn func() *cpn.CPN)
func (r *FuncRegistry) RegisterEventFilter(name string, fn func(cpn.Event) bool)       // [NEW v2.0]
func (r *FuncRegistry) RegisterValidateFunc(name string, fn func(any) error)            // [NEW v2.0]
func (r *FuncRegistry) RegisterOnSuccess(name string, fn func(any) any)                 // [NEW v2.0]
func (r *FuncRegistry) RegisterSchema(name string, factory func() any)                  // [NEW v2.0]

// Lookup methods (return fn, bool — no panic on miss)
func (r *FuncRegistry) LookupGuard(name string) (func([]*cpn.Token) bool, bool)
func (r *FuncRegistry) LookupExecutor(name string) (func(context.Context, cpn.Token) (cpn.Token, error), bool)
func (r *FuncRegistry) LookupRetryOn(name string) (func(error, int) bool, bool)
func (r *FuncRegistry) LookupSubNetFactory(name string) (func() *cpn.CPN, bool)
func (r *FuncRegistry) LookupEventFilter(name string) (func(cpn.Event) bool, bool)      // [NEW v2.0]
func (r *FuncRegistry) LookupValidateFunc(name string) (func(any) error, bool)           // [NEW v2.0]
func (r *FuncRegistry) LookupOnSuccess(name string) (func(any) any, bool)                // [NEW v2.0]
func (r *FuncRegistry) LookupSchema(name string) (func() any, bool)                      // [NEW v2.0]

// Reverse lookup methods (for MarshalCPN — func pointer → name)
func (r *FuncRegistry) ReverseLookupGuard(fn func([]*cpn.Token) bool) (string, bool)
func (r *FuncRegistry) ReverseLookupExecutor(fn func(context.Context, cpn.Token) (cpn.Token, error)) (string, bool)
func (r *FuncRegistry) ReverseLookupRetryOn(fn func(error, int) bool) (string, bool)
func (r *FuncRegistry) ReverseLookupSubNetFactory(fn func() *cpn.CPN) (string, bool)
func (r *FuncRegistry) ReverseLookupEventFilter(fn func(cpn.Event) bool) (string, bool)  // [NEW v2.0]
func (r *FuncRegistry) ReverseLookupValidateFunc(fn func(any) error) (string, bool)      // [NEW v2.0]
func (r *FuncRegistry) ReverseLookupOnSuccess(fn func(any) any) (string, bool)           // [NEW v2.0]
// No ReverseLookupSchema — Schema is any, reverse lookup uses validateFn name convention
```

### 4.3 Topology Structs `[UPDATED v2.0]`

```go
type CPNTopology struct {
    ID, Role    string
    Depth       int
    Mode        string // string(cpn.Mode)
    Places      map[string]PlaceTopology
    Transitions map[string]TransitionTopology
}

type PlaceTopology struct {
    ID, Color, Space string
}

// [UPDATED v2.0] — Complete field coverage
type TransitionTopology struct {
    // Identity & arcs (unchanged from v1.0)
    ID           string
    Kind         string
    InputPlaces  []string
    OutputPlaces []string
    ErrorPlace   string

    // Func fields — stored as registry names (unchanged from v1.0)
    GuardFunc    string
    ExecutorFunc string
    FactoryFunc  string

    // [NEW v2.0] Additional func fields
    EventFilterFunc string // Observer transitions
    RetryOnFunc     string // Moved from RetryPolicyTopology for clarity

    // [NEW v2.0] Serializable config fields
    SystemPrompt  string                   // LLM system prompt
    ToolName      string                   // Tool transition name
    LLMConfig     *LLMConfigTopology       // Full LLM configuration (nullable)
    LLMTools      []string                 // Tool transition IDs for agentic loop
    ValidateConfig *ValidateConfigTopology  // Validation configuration (nullable)
    HITLConfig    *HITLConfigTopology       // HITL configuration (nullable)
    ObservedCPNID string                   // Observer target CPN ID

    // Retry & resilience
    Retry *RetryPolicyTopology
    // SubNet stored as nested topology (when SubNet != nil and Factory is empty)
    SubNetTopology *CPNTopology
}

// [NEW v2.0] Full LLMConfig serialization
type LLMConfigTopology struct {
    Model          string                          `json:"model,omitempty"`
    FallbackModels []string                        `json:"fallbackModels,omitempty"`
    Endpoint       string                          `json:"endpoint,omitempty"`
    MaxTokens      int                             `json:"maxTokens,omitempty"`
    Temperature    float64                         `json:"temperature,omitempty"`
    StreamOutput   bool                            `json:"streamOutput,omitempty"`
    RequireJSON    bool                            `json:"requireJSON,omitempty"`
    JSONSchema     *JSONSchemaConfigTopology        `json:"jsonSchema,omitempty"`
    Budget         float64                         `json:"budget,omitempty"`
    Provider       *ProviderConfigTopology          `json:"provider,omitempty"`
    Trace          *TraceConfigTopology             `json:"trace,omitempty"`
    Plugins        []string                        `json:"plugins,omitempty"`
    Reasoning      *ReasoningConfigTopology         `json:"reasoning,omitempty"`
    CacheControl   *CacheControlConfigTopology      `json:"cacheControl,omitempty"`
    SkipHistory    bool                            `json:"skipHistory,omitempty"`
}

// Mirror types — 1:1 with cpn counterparts
type JSONSchemaConfigTopology struct {
    Name        string          `json:"name,omitempty"`
    Description string          `json:"description,omitempty"`
    Schema      json.RawMessage `json:"schema,omitempty"`
    Strict      *bool           `json:"strict,omitempty"`
}

type ProviderConfigTopology struct {
    Order             []string               `json:"order,omitempty"`
    Only              []string               `json:"only,omitempty"`
    Ignore            []string               `json:"ignore,omitempty"`
    AllowFallbacks    *bool                  `json:"allowFallbacks,omitempty"`
    Sort              string                 `json:"sort,omitempty"`
    MaxPrice          *ProviderMaxPriceTopology `json:"maxPrice,omitempty"`
    DataCollection    string                 `json:"dataCollection,omitempty"`
    ZDR               bool                   `json:"zdr,omitempty"`
    RequireParameters bool                   `json:"requireParameters,omitempty"`
}

type ProviderMaxPriceTopology struct {
    Prompt, Completion, Image, Audio, Request string
}

type TraceConfigTopology struct {
    TraceID, TraceName, SpanName, GenerationName, ParentSpanID string
}

type ReasoningConfigTopology struct {
    Effort       string `json:"effort,omitempty"`
    Summary      string `json:"summary,omitempty"`
    MaxTokens    int    `json:"maxTokens,omitempty"`
    Enabled      *bool  `json:"enabled,omitempty"`
    Exclude      *bool  `json:"exclude,omitempty"`
    BudgetTokens int    `json:"budgetTokens,omitempty"`
}

type CacheControlConfigTopology struct {
    TTL string `json:"ttl,omitempty"`
}

// [NEW v2.0] ValidateConfig — func fields as registry names
type ValidateConfigTopology struct {
    SchemaFunc      string `json:"schemaFunc,omitempty"`      // Registry name
    ValidateFunc    string `json:"validateFunc,omitempty"`    // Registry name
    OnSuccessFunc   string `json:"onSuccessFunc,omitempty"`   // Registry name
    MaxCorrections  int    `json:"maxCorrections,omitempty"`
    CorrectionLLMID string `json:"correctionLLMID,omitempty"`
}

// [NEW v2.0] HITLConfig — serializable fields only
type HITLConfigTopology struct {
    Prompt          string `json:"prompt,omitempty"`
    RevisionLoop    bool   `json:"revisionLoop,omitempty"`
    CorrectionLLMID string `json:"correctionLLMID,omitempty"`
    MaxRevisions    int    `json:"maxRevisions,omitempty"`
}

// [UPDATED v2.0] RetryPolicy — with CircuitBreaker config
type RetryPolicyTopology struct {
    MaxAttempts    int                           `json:"maxAttempts"`
    InitialWaitMs  int64                         `json:"initialWaitMs"`
    MaxWaitMs      int64                         `json:"maxWaitMs"`
    Multiplier     float64                       `json:"multiplier"`
    RetryOnFunc    string                        `json:"retryOnFunc,omitempty"`
    CircuitBreaker *CircuitBreakerConfigTopology  `json:"circuitBreaker,omitempty"`
}

type CircuitBreakerConfigTopology struct {
    FailureThreshold int   `json:"failureThreshold"`
    OpenDurationMs   int64 `json:"openDurationMs"`
}
```

### 4.4 Marshal / Unmarshal / Hash

```go
func MarshalCPN(c *cpn.CPN, registry *FuncRegistry) (*CPNTopology, error)
func UnmarshalCPN(t *CPNTopology, registry *FuncRegistry) (*cpn.CPN, error)
func TopologyHash(t *CPNTopology) string // SHA-256 of structural topology
```

### 4.5 `[UPDATED v2.0]` Field Mapping: Transition → TransitionTopology

| Transition Field | Serialized? | Topology Field | Mechanism |
|---|---|---|---|
| `ID` | Yes | `ID` | Direct copy |
| `Kind` | Yes | `Kind` | `string(t.Kind)` |
| `InputPlaces` | Yes | `InputPlaces` | Direct copy |
| `OutputPlaces` | Yes | `OutputPlaces` | Direct copy |
| `ErrorPlace` | Yes | `ErrorPlace` | Direct copy |
| `Guard` | Yes | `GuardFunc` | Registry reverse lookup |
| `Retry` | Yes | `Retry` | Nested struct (Durations → Ms) |
| `Retry.RetryOn` | Yes | `RetryOnFunc` | Registry reverse lookup |
| `Retry.CircuitBreaker` | Yes | `Retry.CircuitBreaker` | Nested struct |
| `cbState` | **NO** | — | Runtime-only, reconstructed |
| `SystemPrompt` | Yes | `SystemPrompt` | Direct copy |
| `ToolName` | Yes | `ToolName` | Direct copy |
| `Executor` | Yes | `ExecutorFunc` | Registry reverse lookup |
| `LLMConfig` | Yes | `LLMConfig` | Nested struct (all fields serializable) |
| `LLMTools` | Yes | `LLMTools` | Direct copy |
| `ValidateConfig.Schema` | Yes | `ValidateConfig.SchemaFunc` | Schema factory registry |
| `ValidateConfig.ValidateFunc` | Yes | `ValidateConfig.ValidateFunc` | Registry reverse lookup |
| `ValidateConfig.OnSuccess` | Yes | `ValidateConfig.OnSuccessFunc` | Registry reverse lookup |
| `ValidateConfig.MaxCorrections` | Yes | `ValidateConfig.MaxCorrections` | Direct copy |
| `ValidateConfig.CorrectionLLMID` | Yes | `ValidateConfig.CorrectionLLMID` | Direct copy |
| `SubNet` | Yes | `SubNetTopology` | Recursive MarshalCPN |
| `SubNetFactory` | Yes | `FactoryFunc` | Registry reverse lookup |
| `HITLConfig.Channel` | **NO** | — | Runtime-only, rewired by SessionService |
| `HITLConfig.Prompt` | Yes | `HITLConfig.Prompt` | Direct copy |
| `HITLConfig.RevisionLoop` | Yes | `HITLConfig.RevisionLoop` | Direct copy |
| `HITLConfig.CorrectionLLMID` | Yes | `HITLConfig.CorrectionLLMID` | Direct copy |
| `HITLConfig.MaxRevisions` | Yes | `HITLConfig.MaxRevisions` | Direct copy |
| `ObservedCPNID` | Yes | `ObservedCPNID` | Direct copy |
| `EventFilter` | Yes | `EventFilterFunc` | Registry reverse lookup |

---

## 5. Acceptance Criteria

- **AC-001**: Register + Lookup round-trips for all 7 func kinds
- **AC-002**: Duplicate registration panics
- **AC-003**: Lookup miss returns `(nil, false)`
- **AC-004**: ReverseLookup resolves registered func → name
- **AC-005**: ReverseLookup miss returns `("", false)`
- **AC-006**: MarshalCPN → UnmarshalCPN round-trip produces structurally equivalent `*CPN`
- **AC-007**: MarshalCPN errors when a non-nil func is not in registry
- **AC-008**: MarshalCPN succeeds when func is nil (empty string in topology)
- **AC-009**: TopologyHash is stable across process restarts (same input → same hash)
- **AC-010**: TopologyHash changes when any structural field changes
- **AC-011**: `[UPDATED v2.0]` Round-trip preserves LLMConfig fields (Model, MaxTokens, Budget, etc.)
- **AC-012**: `[UPDATED v2.0]` Round-trip preserves HITLConfig fields (Prompt, RevisionLoop, MaxRevisions)
- **AC-013**: `[UPDATED v2.0]` Round-trip preserves ValidateConfig func names and serializable fields
- **AC-014**: `[UPDATED v2.0]` Round-trip preserves RetryPolicy with CircuitBreaker config
- **AC-015**: `[UPDATED v2.0]` Round-trip preserves SystemPrompt, ToolName, LLMTools, ObservedCPNID
- **AC-016**: `[UPDATED v2.0]` EventFilter func is registered, serialized, and restored via round-trip
- **AC-017**: `[UPDATED v2.0]` UnmarshalCPN sets `cbState = nil` and `HITLConfig.Channel = nil`
- **AC-018**: Concurrent Register/Lookup from multiple goroutines — no races

---

## 6. Test Automation Strategy

### Test Files

| File | Contents | Count |
|---|---|---|
| `registry_test.go` | Registry CRUD, duplicate panic, reverse lookup, concurrency | ~20 |
| `topology_test.go` | Round-trip, hash stability, error cases, config serialization | ~25 |
| **Total** | | **~45** |

### Test Structure

| Test Function | Subtests |
|---|---|
| `TestFuncRegistry_Guards` | Register, Lookup, LookupMiss, ReverseLookup, DuplicatePanic |
| `TestFuncRegistry_Executors` | Register, Lookup, LookupMiss, ReverseLookup, DuplicatePanic |
| `TestFuncRegistry_RetryOns` | Register, Lookup, LookupMiss, ReverseLookup, DuplicatePanic |
| `TestFuncRegistry_Factories` | Register, Lookup, LookupMiss, ReverseLookup, DuplicatePanic |
| `TestFuncRegistry_EventFilters` | `[NEW v2.0]` Register, Lookup, ReverseLookup, DuplicatePanic |
| `TestFuncRegistry_ValidateFuncs` | `[NEW v2.0]` Register, Lookup, ReverseLookup, DuplicatePanic |
| `TestFuncRegistry_OnSuccessFuncs` | `[NEW v2.0]` Register, Lookup, ReverseLookup, DuplicatePanic |
| `TestFuncRegistry_Schemas` | `[NEW v2.0]` Register, Lookup, DuplicatePanic |
| `TestFuncRegistry_Concurrent` | Concurrent register + lookup |
| `TestMarshalCPN_Simple` | Minimal CPN round-trip |
| `TestMarshalCPN_FullTopology` | `[NEW v2.0]` CPN with all transition kinds + all config fields |
| `TestMarshalCPN_LLMConfig` | `[NEW v2.0]` Full LLMConfig serialization |
| `TestMarshalCPN_ValidateConfig` | `[NEW v2.0]` ValidateConfig with func registry |
| `TestMarshalCPN_HITLConfig` | `[NEW v2.0]` HITLConfig serializable fields only |
| `TestMarshalCPN_RetryWithCB` | `[NEW v2.0]` RetryPolicy + CircuitBreaker config |
| `TestMarshalCPN_UnregisteredFunc` | Error on unregistered func |
| `TestMarshalCPN_NilFunc` | Success with nil funcs |
| `TestUnmarshalCPN_MissingFunc` | Error on missing registry name |
| `TestUnmarshalCPN_RuntimeFields` | `[NEW v2.0]` cbState=nil, HITLConfig.Channel=nil |
| `TestTopologyHash_Stable` | Same input → same hash across calls |
| `TestTopologyHash_Changes` | Different topology → different hash |
| `TestTopologyHash_IgnoresRuntime` | Mode/State changes don't affect hash |
| `TestTopologyHash_IncludesConfigs` | `[NEW v2.0]` LLMConfig/HITLConfig affect hash |

### Framework
- Go standard `testing` package only
- `-race` flag
- Table-driven for registry CRUD
- `recover()` for panic tests

---

## 7. Rationale & Context

### Why 7 Func Kinds (Not 4)
The original spec assumed only 4 func fields on Transition: Guard, Executor, RetryOn, SubNetFactory. The actual Transition struct has 7 func-typed or non-serializable fields: Guard, Executor, RetryOn, SubNetFactory, EventFilter, ValidateFunc, OnSuccess. Each requires its own registry map because the function signatures differ.

### Why Schema Factory Pattern
`ValidateConfig.Schema` is typed as `any` and used as a reflection target (`reflect.TypeOf(schema).Elem()`). It cannot be serialized as JSON. The registry stores a "schema factory" — `func() any` — that returns a zero-value of the target type. This preserves the runtime behavior without serializing Go types.

### Why Separate Topology Types (Not json tags on CPN)
CPN domain types serve the execution engine. Topology types serve serialization. Mixing both concerns (json tags on CPN, Place, Transition) would: (a) pollute domain types with persistence details, (b) create accidental coupling, (c) make it impossible to exclude runtime-only fields cleanly.

### `[UPDATED v2.0]` Why Full LLMConfig in Topology
LLM configuration (model, budget, temperature, plugins, reasoning) is structural — it defines the CPN's behavior. Two topologies with different LLM models are different flows. TopologyHash must include these fields. Without them, Flow Intelligence would incorrectly merge flows that use different models.

---

## 8. Dependencies & External Integrations

### Block Dependencies
- **DEP-001**: P1 (#55) — Repository interfaces, types, in-memory implementations
- **DEP-002**: CPN engine types — `cpn.CPN`, `cpn.Place`, `cpn.Transition`, `cpn.Token`, `cpn.Event`, `cpn.LLMConfig`, `cpn.ValidateConfig`, `cpn.HITLConfig`, `cpn.RetryPolicy`, `cpn.NodeKind`, `cpn.Mode`

### Technology Dependencies
- **PLT-001**: Go 1.25 — stdlib only
- **PLT-002**: `crypto/sha256` — for TopologyHash
- **PLT-003**: `reflect` — for func pointer reverse maps (`reflect.ValueOf(fn).Pointer()`)
- **PLT-004**: `encoding/json` — for topology serialization and hash computation

### Depended On By
- **P8** (#62) — FlowRepository stores `TopologyJSON` and `FunctionMapping` from MarshalCPN
- **P7** (#61) — IntelligenceRepository links ExecutionRecords to flows via TopologyHash

---

## 9. Examples & Edge Cases

### 9.1 Registration at Startup
```go
// cmd/server/main.go — register all funcs before CPN construction
reg := persist.DefaultRegistry

reg.RegisterGuard("always-true", func(tokens []*cpn.Token) bool { return true })
reg.RegisterExecutor("search-tool", searchToolExecutor)
reg.RegisterSubNetFactory("research-team", buildResearchTeam)
reg.RegisterEventFilter("llm-events-only", func(e cpn.Event) bool {
    return e.TransitionKind == cpn.NodeKindLLM
})
reg.RegisterValidateFunc("json-schema-v1", jsonSchemaValidator)
reg.RegisterOnSuccess("extract-content", extractContentTransform)
reg.RegisterSchema("user-profile", func() any { return &UserProfile{} })
reg.RegisterRetryOn("skip-context-canceled", func(err error, _ int) bool {
    return !errors.Is(err, context.Canceled)
})
```

### 9.2 Marshal / Unmarshal Round-Trip
```go
cpnInstance := buildTopology("session-1") // constructs a *cpn.CPN
topology, err := persist.MarshalCPN(cpnInstance, reg)
// topology.Transitions["t-llm"].LLMConfig.Model == "anthropic/claude-sonnet-4-5-20250514"
// topology.Transitions["t-llm"].SystemPrompt == "You are a helpful assistant."
// topology.Transitions["t-review"].HITLConfig.Prompt == "Please review this plan."

hash := persist.TopologyHash(topology)
// hash == "a3f2b8c1..." (deterministic)

restored, err := persist.UnmarshalCPN(topology, reg)
// restored is a live *cpn.CPN with all funcs wired from registry
// restored.Transitions["t-review"].HITLConfig.Channel == nil (runtime-only)
```

### 9.3 Edge Cases

| Edge Case | Expected Behavior |
|-----------|-------------------|
| Transition with nil Guard, nil Executor | GuardFunc="" and ExecutorFunc="" in topology |
| Transition with Guard set but not in registry | MarshalCPN returns error |
| Topology with unknown func name | UnmarshalCPN returns error |
| CPN with nested SubNet (depth > 0) | Recursive MarshalCPN on SubNet |
| Transition with both SubNet and SubNetFactory | FactoryFunc takes precedence; SubNetTopology is nil |
| LLMConfig with all fields set | All fields serialized in LLMConfigTopology |
| LLMConfig with nil sub-structs | Null fields omitted (omitempty) |
| HITLConfig.Channel in topology | NOT present — runtime-only |
| CircuitBreakerState in topology | NOT present — runtime-only |
| ValidateConfig.Schema as *MyStruct | Stored as SchemaFunc name → factory returns `&MyStruct{}` |
| Two CPNs differing only in LLMConfig.Model | Different TopologyHash |
| Same CPN marshaled twice | Identical TopologyHash |

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
go tool cover -func=persist_coverage.out | grep -E "(registry|topology)\.go"

# 6. No driver imports
! grep -rn "database/sql\|pgx\|go-redis" cpn/persist/

# 7. All 7 func kinds registered in tests
grep -c "Register" cpn/persist/registry_test.go

# 8. [v2.0] Verify LLMConfigTopology exists
grep "LLMConfigTopology" cpn/persist/topology.go

# 9. [v2.0] Verify 7 func maps
grep -c "map\[string\]func" cpn/persist/registry.go
```

---

## 11. Related Specifications / Further Reading

- [Agentic CPN v1.3 — Persistence Layer](../agentic-cpn-v1.3.md) — Sections 6.1-6.3
- [Block P1: Repository Interfaces](block-p1-repository-interfaces.md) — Foundation types and interfaces
- [Block P8: Flow Repository + Store Facade](block-p8-flow-repository-store-facade.md) — Consumes topology JSON
- **Next Block**: Block P3 — Postgres Connection Pool + Migrations

---

## Appendix A: Change Log from v1.0

| Change | Reason | Impact |
|--------|--------|--------|
| FuncRegistry: 4 → 7+1 func kinds | Code has EventFilter, ValidateFunc, OnSuccess, Schema | P2 |
| TransitionTopology: added 8 fields | SystemPrompt, ToolName, LLMConfig, LLMTools, ValidateConfig, HITLConfig, ObservedCPNID, EventFilterFunc | P2, P8 |
| Added LLMConfigTopology + 5 sub-types | Full LLMConfig serialization for round-trip and hashing | P2 |
| Added ValidateConfigTopology | 3 func fields need registry, 2 data fields are direct | P2 |
| Added HITLConfigTopology | Channel is runtime-only; Prompt/RevisionLoop/MaxRevisions serializable | P2 |
| RetryPolicyTopology: added CircuitBreaker | CircuitBreakerConfig serializable; CircuitBreakerState runtime-only | P2 |
| Schema factory pattern for ValidateConfig.Schema | `any` type reference cannot be JSON serialized | P2 |
| TopologyHash includes all config fields | LLMConfig/HITLConfig/ValidateConfig changes must produce different hashes | P2, P7 |
| Test count: ~15 → ~45 | Comprehensive config serialization tests needed | P2 |

---

## Implementation File Checklist

- [ ] `back/go-assistant/cpn/persist/registry.go` — FuncRegistry with 7+1 func kinds, reverse maps
- [ ] `back/go-assistant/cpn/persist/topology.go` — All topology structs, MarshalCPN, UnmarshalCPN, TopologyHash
- [ ] `back/go-assistant/cpn/persist/registry_test.go` — ~20 tests: CRUD, duplicate panic, reverse lookup, concurrency
- [ ] `back/go-assistant/cpn/persist/topology_test.go` — ~25 tests: round-trip, hash, config serialization, error cases
- [ ] Verify: All 7 func kinds register + lookup + reverse lookup
- [ ] Verify: MarshalCPN captures ALL Transition fields (see Section 4.5 mapping table)
- [ ] Verify: UnmarshalCPN restores all config structs
- [ ] Verify: Runtime-only fields (cbState, Channel) are nil after unmarshal
- [ ] Verify: TopologyHash is deterministic and includes config fields
- [ ] Verify: No driver imports
