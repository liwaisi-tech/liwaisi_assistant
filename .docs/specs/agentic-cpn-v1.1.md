# Unified Agentic CPN — Spec Driven Design
> Version 1.1 — Supersedes v1.0  
> Theoretical foundation: Borghoff, Bottoni & Pareschi (2025) — *Human-Artificial Interaction in the Age of Agentic AI*  
> Engineering foundation: *The 7 Foundational Building Blocks of AI Agents*

---

## 0. Related Documents

| Document | Description | Status |
|---|---|---|
| **Unified Agentic CPN — Spec Driven Design v1.1** (this document) | Unified architecture: CPN as the single substrate for tools, agents, coordination, and the 7 building blocks | current |
| Unified Agentic CPN v1.0 | Previous version. 7 building blocks not yet integrated. OpenRouter not yet specified. | archived |
| Agentic OS — Spec Driven Development v0.1 | Superseded by v1.0 | archived |
| CPN Tool Engine — Spec Driven Design v0.1 | Superseded by v1.0 | archived |

---

## 1. What Changed in v1.1

| Area | Change |
|---|---|
| Design Axioms | Added A11 (LLM is last resort), A12 (context engineering is the core skill) |
| NodeKind | Added `NodeKindValidate` — new transition kind for schema enforcement and retry |
| Transition | Added `RetryPolicy` struct — per-transition retry, backoff, and circuit breaker |
| LLMClient | Specified `OpenRouterClient` as the concrete implementation |
| OpenRouter | New §6 — model registry, per-transition model selection, pricing awareness, token budgets |
| Memory | New §7 — formal context window spec: what goes in, what stays out, compression strategy |
| Building Blocks | New §8 — maps all 7 blocks to CPN primitives; names "Control" as topology |
| Recovery | New `RetryPolicy` spec; circuit breaker state machine; executor retry loop |
| Validation | New `NodeKindValidate` spec; schema enforcement; feedback to LLM on failure |
| Feedback (HITL) | Added revision-loop pattern; extended HITL to support multi-round approval |
| Reference Networks | Updated net 15.1 with validation and retry; added net 15.4 (revision loop) |
| Package Structure | Added `openrouter.go`, `validate.go`, `retry.go`, `memory.go` |
| Implementation Order | Extended to Step 18 |

---

## 2. Design Axioms

These constraints are non-negotiable. Every implementation decision must respect all of them.

| # | Axiom | Source |
|---|---|---|
| A1 | **Everything is a CPN.** A tool, an agent, a director, and the orchestrator are all instances of the same `CPN` type. Their identity emerges from topology (depth) and role label. | Decision Q2 |
| A2 | **Sub-CPNs are isolated.** A child CPN has its own place namespace. It cannot read or write parent places directly. | Decision Q1 |
| A3 | **Sub-CPNs are observable.** A child CPN emits events to a shared bus. The parent observes via `NodeKindObserver` transitions. | Decision Q1 + Paper §4.2 |
| A4 | **HITL is a first-class node kind.** A `NodeKindHITL` transition blocks its branch until a human-provided token arrives on a channel. | Decision Q3 |
| A5 | **Three communication spaces are structural.** Every Place belongs to Surface, Observation, or Computation space. The executor enforces this. | Paper §4.2 |
| A6 | **Tokens carry origin.** Every token knows which CPN produced it and its depth. | Paper §4.4 |
| A7 | **MAS and Centaurian are runtime modes.** A CPN can switch between `ModeMAS` and `ModeCentaurian` at any executor iteration. | Paper §2.4 |
| A8 | **Parallelism is topological.** Concurrency emerges from the firing rule, never declared explicitly. | Paper §3.1 |
| A9 | **Event Store is append-only.** No event is ever modified. | Spec v0.1 + Paper §4.2 |
| A10 | **The human channel is the only interface.** The user never sees topology, depth, or mode. | Spec v0.1 §2 |
| A11 | **LLM is the last resort.** An LLM call is the most expensive and most dangerous operation in the system. Every transition that can be implemented as `NodeKindTool` (deterministic code) must be. `NodeKindLLM` is used only when reasoning with context is genuinely required and cannot be replaced by deterministic logic. | Building Blocks §1 |
| A12 | **Context engineering is the core skill.** When an LLM call is unavoidable, its quality depends entirely on sending the right context at the right time to the right model. Every `NodeKindLLM` transition is responsible for assembling that context before firing. | Building Blocks §1 |

---

## 3. Glossary

| Term in system | Formal name | Where it lives |
|---|---|---|
| Orchestrator | Root CPN | `CPN` at `Depth=0` |
| Director | Domain CPN | `CPN` at `Depth=1`, spawned as `NodeKindSubNet` |
| Agent / Worker | Worker CPN | `CPN` at `Depth=2+`, spawned as `NodeKindSubNet` |
| Tool call | Tool transition | `Transition` with `NodeKindTool` |
| LLM reasoning step | LLM transition | `Transition` with `NodeKindLLM` |
| Schema enforcement | Validate transition | `Transition` with `NodeKindValidate` |
| Nested agent | Sub-CPN | `Transition` with `NodeKindSubNet` holding a `*CPN` |
| Human gate / approval | HITL transition | `Transition` with `NodeKindHITL` |
| Observation bridge | Observer transition | `Transition` with `NodeKindObserver` |
| Communication space | SpaceKind on Place | `Place.Space` (`Surface`/`Observation`/`Computation`) |
| Token type | Color set | `Token.Color` |
| Context window | Memory buffer | `ContextWindow` built by `buildContext()` |
| Compressed memory | Observer token | `Token{Color: ColorEvent}` carrying sub-CPN summary |
| Retry behavior | RetryPolicy | `Transition.Retry *RetryPolicy` |
| Model selection | LLM config | `Transition.LLMConfig LLMConfig` |
| OpenRouter | LLM proxy | `OpenRouterClient` implements `LLMClient` |
| Centaurian synergy | Co-trigger guard | `Transition.Guard` checking both `ColorHuman` and AI-origin tokens |
| Deterministic routing | Control (block 5) | CPN topology + guards — no LLM involved |

---

## 4. Core Types

### 4.1 SpaceKind

```go
type SpaceKind string

const (
    // SpaceSurface — mediates contact with the outside world.
    // User input, sensor data, external API responses land here.
    SpaceSurface SpaceKind = "surface"

    // SpaceObservation — bridges surface and computation.
    // Observer transitions live here. Sub-CPN events are received here.
    // No raw token from SpaceSurface may appear directly in SpaceComputation.
    SpaceObservation SpaceKind = "observation"

    // SpaceComputation — the system's core.
    // Decision-making, resource allocation, final outputs.
    // In ModeCentaurian: human+AI co-trigger transitions.
    SpaceComputation SpaceKind = "computation"
)
```

### 4.2 ColorSet

```go
type ColorSet string

const (
    ColorString   ColorSet = "STRING"    // raw text, queries, prompts
    ColorJSON     ColorSet = "JSON"      // structured data between transitions
    ColorArtifact ColorSet = "ARTIFACT"  // final deliverables: reports, code, specs
    ColorScore    ColorSet = "SCORE"     // numeric analysis results
    ColorEvent    ColorSet = "EVENT"     // sub-CPN events, only valid in SpaceObservation
    ColorHuman    ColorSet = "HUMAN"     // explicit human input, only from NodeKindHITL
    ColorCPN      ColorSet = "CPN"       // a *CPN reference passed as a token payload
    ColorSchema   ColorSet = "SCHEMA"    // JSON schema used by NodeKindValidate
    ColorError    ColorSet = "ERROR"     // error payload for recovery transitions
)
```

### 4.3 NodeKind

```go
type NodeKind string

const (
    // NodeKindTool — deterministic code execution.
    // The first choice for any transition. No LLM. No network (unless the tool
    // itself calls an API, but that call is not the LLM).
    NodeKindTool NodeKind = "tool"

    // NodeKindLLM — LLM call via OpenRouter.
    // Use ONLY when deterministic code cannot solve the problem.
    // Requires LLMConfig for model selection.
    NodeKindLLM NodeKind = "llm"

    // NodeKindValidate — schema validation with optional LLM feedback loop.
    // Validates the payload of input tokens against a JSON schema.
    // On failure: if RetryWithLLM is set, sends the token back to an LLM
    // transition for correction. Otherwise emits ColorError token.
    NodeKindValidate NodeKind = "validate"

    // NodeKindSubNet — spawns a child CPN.
    // Input tokens become the child's initial marking.
    // Child runs isolated with its own place namespace.
    // Parent observes via NodeKindObserver.
    NodeKindSubNet NodeKind = "subnet"

    // NodeKindObserver — listens to the EventBus for events from sub-CPNs.
    // No InputPlaces. Triggered by events, not token availability.
    NodeKindObserver NodeKind = "observer"

    // NodeKindHITL — blocks its branch until a human token arrives.
    // Supports single-turn approval and multi-round revision loops.
    NodeKindHITL NodeKind = "hitl"
)
```

### 4.4 CPNMode and CPNState

```go
type CPNMode string

const (
    ModeMAS        CPNMode = "mas"        // independent firing, default
    ModeCentaurian CPNMode = "centaurian" // co-trigger required for computation transitions
)

type CPNState string

const (
    StateIdle      CPNState = "idle"
    StateRunning   CPNState = "running"
    StateWaiting   CPNState = "waiting"   // blocked on NodeKindHITL
    StateCompleted CPNState = "completed"
    StateFailed    CPNState = "failed"
)
```

### 4.5 Token

```go
// Token — the fundamental unit of data. Immutable once created.
type Token struct {
    Color   ColorSet
    Payload any

    // Origin metadata
    OriginID    string   // CPN ID that produced this token
    OriginDepth int      // depth of producing CPN (-1 = human)
    OriginKind  NodeKind // kind of transition that produced this token

    // Spatial metadata
    Space SpaceKind

    // Traceability
    SessionID  string
    TraceID    string    // propagated across sub-CPN boundaries for distributed tracing
    Timestamp  time.Time
}

func (t Token) IsHumanOrigin() bool {
    return t.Color == ColorHuman || t.OriginKind == NodeKindHITL
}
```

### 4.6 Place

```go
// Place — typed buffer with spatial identity. Thread-safe.
type Place struct {
    ID    string
    Color ColorSet
    Space SpaceKind

    Tokens []Token
    mu     sync.Mutex
}

func (p *Place) Deposit(t Token) error {
    if t.Color != p.Color {
        return fmt.Errorf("%w: place=%s expected=%s got=%s", ErrColorMismatch, p.ID, p.Color, t.Color)
    }
    if t.Space != p.Space {
        return fmt.Errorf("%w: place=%s expected=%s got=%s", ErrSpaceMismatch, p.ID, p.Space, t.Space)
    }
    p.mu.Lock()
    defer p.mu.Unlock()
    p.Tokens = append(p.Tokens, t)
    return nil
}

func (p *Place) Consume() (Token, error) {
    p.mu.Lock()
    defer p.mu.Unlock()
    if len(p.Tokens) == 0 {
        return Token{}, ErrEmptyPlace
    }
    t := p.Tokens[0]
    p.Tokens = p.Tokens[1:]
    return t, nil
}

func (p *Place) Peek() ([]Token, bool) {
    p.mu.Lock()
    defer p.mu.Unlock()
    if len(p.Tokens) == 0 {
        return nil, false
    }
    cp := make([]Token, len(p.Tokens))
    copy(cp, p.Tokens)
    return cp, true
}
```

### 4.7 RetryPolicy

New in v1.1. Attached to any Transition regardless of kind.
Implements Building Block 6 (Recovery) at the transition level.

```go
// RetryPolicy — per-transition retry and backoff configuration.
// Applies to NodeKindTool, NodeKindLLM, and NodeKindSubNet.
// NodeKindValidate has its own retry semantics (see §10).
type RetryPolicy struct {
    MaxAttempts int           // total attempts including first (default: 1 = no retry)
    InitialWait time.Duration // wait before first retry (default: 500ms)
    MaxWait     time.Duration // cap for exponential backoff (default: 30s)
    Multiplier  float64       // backoff multiplier (default: 2.0)

    // RetryOn — function that decides whether an error is retryable.
    // If nil, all errors are retried up to MaxAttempts.
    RetryOn func(err error, attempt int) bool

    // CircuitBreaker — if set, tracks failure rate across all firings of
    // this transition in the current CPN run and trips after threshold.
    CircuitBreaker *CircuitBreakerConfig
}

// CircuitBreakerConfig — prevents hammering a failing dependency.
type CircuitBreakerConfig struct {
    FailureThreshold int           // consecutive failures before tripping
    OpenDuration     time.Duration // how long the circuit stays open before half-open
}

// CircuitBreakerState — runtime state, stored on the Transition.
type CircuitBreakerState struct {
    Config      *CircuitBreakerConfig
    Failures    int
    TrippedAt   *time.Time
    mu          sync.Mutex
}

func (cb *CircuitBreakerState) Allow() bool {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    if cb.TrippedAt == nil {
        return true // closed
    }
    if time.Since(*cb.TrippedAt) > cb.Config.OpenDuration {
        cb.TrippedAt = nil // half-open: allow one probe
        return true
    }
    return false // open: reject
}

func (cb *CircuitBreakerState) RecordFailure() {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    cb.Failures++
    if cb.Failures >= cb.Config.FailureThreshold {
        now := time.Now()
        cb.TrippedAt = &now
    }
}

func (cb *CircuitBreakerState) RecordSuccess() {
    cb.mu.Lock()
    defer cb.mu.Unlock()
    cb.Failures = 0
    cb.TrippedAt = nil
}

// DefaultRetryPolicy returns a sensible default for external API calls.
func DefaultRetryPolicy() *RetryPolicy {
    return &RetryPolicy{
        MaxAttempts: 3,
        InitialWait: 500 * time.Millisecond,
        MaxWait:     30 * time.Second,
        Multiplier:  2.0,
        RetryOn: func(err error, _ int) bool {
            // Retry on transient errors; do not retry on context cancellation
            return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
        },
    }
}
```

### 4.8 LLMConfig

New in v1.1. Per-transition model selection and budget constraints.
Works in conjunction with the OpenRouterClient.

```go
// LLMConfig — model selection and budget for a single NodeKindLLM or
// NodeKindValidate (when RetryWithLLM is set) transition.
type LLMConfig struct {
    // Model — OpenRouter model string.
    // Examples: "anthropic/claude-sonnet-4-6", "google/gemini-2.0-flash-001"
    // If empty, falls back to OpenRouterClient.DefaultModel.
    Model string

    // MaxTokens — hard cap for the completion. Required.
    // Set this as low as the task allows. Every token costs money.
    MaxTokens int

    // Temperature — 0.0 to 1.0. Default 0.0 for deterministic outputs
    // (classification, JSON generation). Higher for creative tasks.
    Temperature float64

    // StreamOutput — if true, the executor streams response chunks to
    // the session's Stream channel as they arrive. Use for surface-facing
    // LLM transitions (the ones the user sees in real time).
    StreamOutput bool

    // RequireJSON — if true, OpenRouter is told to return JSON only.
    // Use with NodeKindValidate's RetryWithLLM. Sets response_format.
    RequireJSON bool

    // Budget — optional per-call cost ceiling in USD.
    // If the estimated cost exceeds this, the transition emits ErrBudgetExceeded
    // instead of firing. Set to 0 to disable.
    Budget float64
}
```

### 4.9 ValidateConfig

New in v1.1. Attached to `NodeKindValidate` transitions.

```go
// ValidateConfig — schema validation and LLM correction loop.
// Implements Building Block 4 (Validation).
type ValidateConfig struct {
    // Schema — the JSON schema to validate against.
    // Can be a Go struct (marshaled to JSON schema via reflection)
    // or a raw json.RawMessage.
    Schema any

    // MaxCorrections — how many times to send back to LLM for correction
    // before giving up and emitting a ColorError token. Default: 2.
    MaxCorrections int

    // CorrectionLLMID — Transition ID in the same CPN to use for correction.
    // That transition must be NodeKindLLM with RequireJSON: true.
    // If empty, validation failure emits ColorError and does not retry.
    CorrectionLLMID string

    // OnSuccess — optional transform applied to the validated payload
    // before depositing into OutputPlaces.
    OnSuccess func(validated any) any
}
```

### 4.10 HITLConfig

Extended in v1.1 to support multi-round revision loops.

```go
// HITLConfig — human-in-the-loop configuration.
// Implements Building Block 7 (Feedback).
type HITLConfig struct {
    Channel    chan Token
    Prompt     string

    // RevisionLoop — if true, after human provides input, the CPN
    // does NOT automatically deposit the token and continue.
    // Instead, the human's input goes back through a CorrectionLLMID
    // transition for revision, and the revised output is presented again.
    // This repeats until the human approves with HITLAction=Approve.
    RevisionLoop    bool
    CorrectionLLMID string // Transition ID to use for revisions
    MaxRevisions    int    // safety cap on revision rounds (default: 5)
}

// HITLAction — the human's decision on a HITL gate.
type HITLAction string

const (
    HITLApprove HITLAction = "approve"
    HITLReject  HITLAction = "reject"
    HITLRevise  HITLAction = "revise" // provide feedback for another round
)

// HITLResponse — what the human sends to the HITL channel.
type HITLResponse struct {
    Action   HITLAction
    Content  string // free-text approval note, rejection reason, or revision feedback
}
```

### 4.11 Transition

Full definition incorporating all v1.1 additions.

```go
// Transition — a unit of computation with a specific kind.
// NodeKind determines which fields are active. Unused fields are zero-valued.
type Transition struct {
    ID           string
    Kind         NodeKind
    InputPlaces  []string
    OutputPlaces []string

    // Guard — optional. Must return true for CanFire() to proceed.
    // In ModeCentaurian, computation transitions get centaurianGuard injected.
    Guard func(tokens []Token) bool

    // Retry — optional. If nil, no retry on failure.
    // Applies to NodeKindTool, NodeKindLLM, NodeKindSubNet.
    Retry *RetryPolicy

    // cbState — runtime circuit breaker state. Initialized from Retry.CircuitBreaker.
    cbState *CircuitBreakerState

    // ── NodeKindTool ────────────────────────────────────────────────────────
    ToolName string
    Executor func(ctx context.Context, in Token) (Token, error)

    // ── NodeKindLLM ──────────────────────────────────────────────────────────
    SystemPrompt string
    LLMConfig    LLMConfig
    // LLMTools — Transition IDs the LLM may invoke as tool calls.
    // Each referenced transition must be NodeKindTool.
    LLMTools []string

    // ── NodeKindValidate ─────────────────────────────────────────────────────
    ValidateConfig ValidateConfig

    // ── NodeKindSubNet ───────────────────────────────────────────────────────
    SubNet        *CPN
    SubNetFactory func() *CPN

    // ── NodeKindObserver ─────────────────────────────────────────────────────
    ObservedCPNID string
    EventFilter   func(e Event) bool

    // ── NodeKindHITL ─────────────────────────────────────────────────────────
    HITLConfig HITLConfig
}

func (t *Transition) CanFire(places map[string]*Place, mode CPNMode) bool {
    // Circuit breaker check
    if t.cbState != nil && !t.cbState.Allow() {
        return false
    }
    var tokens []Token
    for _, pid := range t.InputPlaces {
        p, ok := places[pid]
        if !ok {
            return false
        }
        ts, ok := p.Peek()
        if !ok {
            return false
        }
        tokens = append(tokens, ts...)
    }
    if t.Guard != nil {
        return t.Guard(tokens)
    }
    return true
}
```

### 4.12 GroupAgent

Unchanged from v1.0. Implements the paper's §4.2 group-agent protocol.

```go
type GroupAgent struct {
    ID    string
    Topic string
    active    []*CPN
    nonActive []*CPN
    mu        sync.RWMutex
}

func (g *GroupAgent) Register(c *CPN)         { /* ... */ }
func (g *GroupAgent) Deliver(e Event)          { /* ... */ }
func (g *GroupAgent) Deregister(id string)     { /* ... */ }
func (g *GroupAgent) SwitchCMP(id string)      { /* ... */ }
```

### 4.13 CPN

```go
// CPN — the universal agent type.
// Depth=0: root (was Orchestrator). Depth=1: domain (was Director). Depth=2+: worker (was Agent).
type CPN struct {
    ID    string
    Role  string
    Depth int

    Mode  CPNMode
    State CPNState
    Error error

    Places      map[string]*Place
    Transitions map[string]*Transition

    Group *GroupAgent

    EventEmitter chan<- Event
    EventBus     <-chan Event

    SessionID string
    History   []Message // conversation history for LLM transitions

    mu sync.RWMutex
}
```

---

## 5. Communication Spaces — Structural Rules

### 5.1 The Space Pipeline

Tokens must flow Surface → Observation → Computation. No skipping.

```
Rule: a token with Space=SpaceSurface cannot be deposited
      directly into a Place with Space=SpaceComputation.
      It must pass through at least one NodeKindObserver or
      NodeKindLLM/NodeKindTool transition in SpaceObservation first.

Enforcement: Place.Deposit() returns ErrSpaceViolation if
      token.Space == SpaceSurface AND place.Space == SpaceComputation.
```

### 5.2 Surface Space

- Input places of depth=0 CPN are always SpaceSurface.
- NodeKindHITL produces ColorHuman tokens with Space: SpaceSurface.
- NodeKindObserver that materializes a sub-CPN event produces Space: SpaceObservation.

### 5.3 Observation Space

- NodeKindObserver transitions have no InputPlaces. Driven by EventBus.
- Transformation, routing, and intent classification happen here.
- The "parsed command" rule: raw user input must pass through an Observation transition before reaching Computation. This can be a deterministic parser (`NodeKindTool`) or an LLM classifier (`NodeKindLLM`), but the step must exist.

### 5.4 Computation Space

- NodeKindTool, NodeKindSubNet, NodeKindLLM, and NodeKindValidate transitions operate here.
- Terminal output places are always SpaceComputation holding ColorArtifact tokens.
- In ModeCentaurian, centaurianGuard is auto-injected on computation transitions without explicit Guard.

---

## 6. OpenRouter Integration

### 6.1 Why OpenRouter

OpenRouter is the LLM proxy. It provides:
- A single API endpoint to access any model (Anthropic, Google, Meta, Mistral, etc.)
- Unified billing and per-user token accounting
- Model fallback and routing by capability/price
- No direct API key management per provider in application code

### 6.2 LLMClient Interface

```go
// LLMClient — the interface all LLM transitions call.
// OpenRouterClient is the production implementation.
// MockLLMClient is used in tests.
type LLMClient interface {
    Complete(ctx context.Context, req LLMRequest) (LLMResponse, error)
    EstimateCost(req LLMRequest) (float64, error) // USD estimate before calling
}
```

### 6.3 LLMRequest and LLMResponse

```go
type LLMRequest struct {
    Model       string          // OpenRouter model string
    Messages    []LLMMessage    // conversation history + system prompt
    MaxTokens   int
    Temperature float64
    Tools       []LLMTool       // available tool schemas (from NodeKindTool transitions)
    ResponseFmt string          // "json_object" if RequireJSON is set
    SessionID   string          // passed to OpenRouter for per-user token tracking
}

type LLMMessage struct {
    Role    string // "system", "user", "assistant", "tool"
    Content string
    // ToolCall — populated when Role == "assistant" and LLM invoked a tool
    ToolCall *LLMToolCall
    // ToolResult — populated when Role == "tool"
    ToolResult *LLMToolResult
}

type LLMResponse struct {
    Content      string
    ToolCalls    []LLMToolCall
    InputTokens  int
    OutputTokens int
    Model        string  // actual model used (may differ from requested if fallback occurred)
    CostUSD      float64 // actual cost reported by OpenRouter
}

type LLMToolCall struct {
    ID        string
    ToolName  string
    Arguments json.RawMessage
}

type LLMToolResult struct {
    ToolCallID string
    Content    string
}

type LLMTool struct {
    Name        string
    Description string
    Parameters  json.RawMessage // JSON schema of the tool's arguments
}
```

### 6.4 OpenRouterClient

```go
// OpenRouterClient — production LLMClient backed by api.openrouter.ai.
type OpenRouterClient struct {
    APIKey       string
    DefaultModel string          // fallback when Transition.LLMConfig.Model is empty
    BaseURL      string          // default: "https://openrouter.ai/api/v1"
    HTTPClient   *http.Client
    // TokenLedger — records token usage per session for billing.
    // Keyed by SessionID. Thread-safe.
    TokenLedger  *TokenLedger
}

func NewOpenRouterClient(apiKey string, defaultModel string) *OpenRouterClient {
    return &OpenRouterClient{
        APIKey:       apiKey,
        DefaultModel: defaultModel,
        BaseURL:      "https://openrouter.ai/api/v1",
        HTTPClient: &http.Client{
            Timeout: 120 * time.Second,
        },
        TokenLedger: NewTokenLedger(),
    }
}

func (c *OpenRouterClient) Complete(ctx context.Context, req LLMRequest) (LLMResponse, error) {
    if req.Model == "" {
        req.Model = c.DefaultModel
    }
    // Estimate cost before calling if Budget is set (checked by executor)
    body := c.buildRequestBody(req)
    httpReq, _ := http.NewRequestWithContext(ctx, "POST",
        c.BaseURL+"/chat/completions", bytes.NewReader(body))
    httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
    httpReq.Header.Set("Content-Type", "application/json")
    // OpenRouter per-user tracking
    httpReq.Header.Set("X-Session-Id", req.SessionID)

    resp, err := c.HTTPClient.Do(httpReq)
    if err != nil {
        return LLMResponse{}, err
    }
    defer resp.Body.Close()

    if resp.StatusCode == http.StatusTooManyRequests {
        return LLMResponse{}, ErrRateLimited
    }
    if resp.StatusCode >= 500 {
        return LLMResponse{}, ErrProviderUnavailable
    }

    var out LLMResponse
    // parse response...
    c.TokenLedger.Record(req.SessionID, out.InputTokens, out.OutputTokens, out.CostUSD)
    return out, nil
}

func (c *OpenRouterClient) EstimateCost(req LLMRequest) (float64, error) {
    // Use OpenRouter's /models endpoint to fetch pricing,
    // then estimate: (input_tokens * input_price) + (output_tokens * output_price)
    // This is an estimate — actual cost may vary.
    return 0, nil // implementation detail
}
```

### 6.5 Model Registry

A curated set of model strings for common use cases.
Using the right model for each transition is part of Axiom A11 (LLM is last resort —
and when you do use it, use the cheapest model that can do the job).

```go
// ModelRegistry — suggested model assignments by task type.
// These are defaults; any OpenRouter model string is valid.
var ModelRegistry = map[string]string{
    // Classification, routing, intent detection — fast and cheap
    "classifier":   "google/gemini-2.0-flash-001",

    // JSON generation, structured extraction — reliable JSON mode
    "structured":   "anthropic/claude-haiku-4-5-20251001",

    // Code generation, complex reasoning — higher capability
    "reasoning":    "anthropic/claude-sonnet-4-6",

    // Long document analysis, large context — large context window
    "long-context": "google/gemini-2.0-pro-001",

    // Cheap summarization and compression — small outputs
    "summarize":    "meta-llama/llama-3.3-8b-instruct",
}
```

### 6.6 TokenLedger

Per-session token and cost tracking. Used by the Flow Intelligence Engine
for efficiency ranking and by the billing layer.

```go
type TokenLedger struct {
    records map[string]*SessionTokenRecord // keyed by SessionID
    mu      sync.RWMutex
}

type SessionTokenRecord struct {
    SessionID    string
    InputTokens  int
    OutputTokens int
    TotalCostUSD float64
    Calls        int
    LastUpdated  time.Time
}

func (l *TokenLedger) Record(sessionID string, in, out int, costUSD float64) {
    l.mu.Lock()
    defer l.mu.Unlock()
    r := l.records[sessionID]
    if r == nil {
        r = &SessionTokenRecord{SessionID: sessionID}
        l.records[sessionID] = r
    }
    r.InputTokens  += in
    r.OutputTokens += out
    r.TotalCostUSD += costUSD
    r.Calls++
    r.LastUpdated = time.Now()
}

func (l *TokenLedger) Get(sessionID string) *SessionTokenRecord {
    l.mu.RLock()
    defer l.mu.RUnlock()
    return l.records[sessionID]
}
```

---

## 7. Memory — Context Window Management

> Building Block 2 (Memory) formally specified.

### 7.1 The Problem

LLMs are stateless. Every NodeKindLLM transition call is independent — the model
remembers nothing. Memory is entirely the system's responsibility.
The context window is finite and expensive. Sending everything always is
the most common mistake in production LLM systems.

### 7.2 Memory Strategy

The system uses three tiers of memory, assembled by `buildContext()` before every LLM call:

| Tier | Content | Retention | Token cost |
|---|---|---|---|
| T1: System context | System prompt, role, available tools | Always included | Fixed per transition |
| T2: Compressed memory | Observer tokens (thinking blocks) from completed sub-CPNs | Always included (all of them) | Low — they are summaries |
| T3: Recent raw messages | Last N raw user/assistant messages | Sliding window | Moderate |

Observer tokens are the key insight: when a sub-CPN completes, it emits a summary
as a ColorEvent token. The parent materializes this as a `RoleObserver` message in
the history. Future LLM calls read the summary instead of replaying the entire
sub-CPN's execution history. This is compressed memory.

### 7.3 ContextWindow

```go
// ContextWindow — the assembled input for a single LLM call.
// Built by buildContext() and passed to OpenRouterClient.Complete().
type ContextWindow struct {
    SystemPrompt string
    Messages     []LLMMessage
    // Stats — for logging and cost estimation
    InputTokenEstimate int
}

// buildContext assembles the context window for a NodeKindLLM transition.
// It applies the three-tier memory strategy.
func buildContext(c *CPN, t *Transition) ContextWindow {
    var messages []LLMMessage

    // T2: All observer (thinking block) messages — compressed memory of past operations.
    // These are small and carry high information density. Always include all of them.
    for _, m := range c.History {
        if m.Role == RoleObserver {
            messages = append(messages, LLMMessage{
                Role:    "assistant",
                Content: "[Summary from " + m.CPNRole + "]: " + m.Content,
            })
        }
    }

    // T3: Recent raw messages — sliding window.
    // Only include the last N user/assistant exchanges.
    // N is configured per CPN; default is 10 (5 exchanges).
    rawMessages := filterRaw(c.History)
    if len(rawMessages) > c.ContextWindowSize*2 {
        rawMessages = rawMessages[len(rawMessages)-c.ContextWindowSize*2:]
    }
    for _, m := range rawMessages {
        messages = append(messages, LLMMessage{
            Role:    string(m.Role),
            Content: m.Content,
        })
    }

    return ContextWindow{
        SystemPrompt:       t.SystemPrompt,
        Messages:           messages,
        InputTokenEstimate: estimateTokens(t.SystemPrompt, messages),
    }
}

// compressSubNetSummary is called when a sub-CPN completes.
// It generates a RoleObserver message and appends it to the parent's History.
// The sub-CPN's raw execution history is NOT appended — only this summary.
func compressSubNetSummary(parent *CPN, child *CPN, outputTokens []Token) {
    summary := formatSummary(child, outputTokens)
    parent.History = append(parent.History, Message{
        ID:       newUUID(),
        Role:     RoleObserver,
        Content:  summary,
        CPNID:    child.ID,
        CPNRole:  child.Role,
        CPNDepth: child.Depth,
        Timestamp: time.Now(),
    })
}
```

### 7.4 Context Engineering Rules

These rules apply to every `NodeKindLLM` transition. Violating them is a build-time
concern, not a runtime one — they should be verified in code review.

| Rule | Rationale |
|---|---|
| System prompt is static per transition | Dynamic system prompts that change per call make behavior unpredictable |
| Token budget must be set on every LLMConfig | Unbounded completions are a billing risk |
| Context window must be assembled before the call — never inside the LLM call | Axiom A12: context engineering is a distinct step from inference |
| Use the cheapest model that can solve the task | Axiom A11 extended: if you must call an LLM, don't overspend on it |
| JSON output must use RequireJSON: true and be followed by NodeKindValidate | Never parse LLM JSON without schema validation downstream |
| Surface-facing LLM transitions must use StreamOutput: true | Users see responses in real time; non-streaming is only for background transitions |

### 7.5 Memory Across CPN Depths

```
Depth-0 (root) history:
  ├─ T2: all observer summaries from depth-1 sub-CPNs
  └─ T3: last N raw user/assistant messages

Depth-1 (domain) history:
  ├─ T2: all observer summaries from its own depth-2 sub-CPNs
  ├─ T3: last N raw messages from its own coordination
  └─ does NOT include root's history (sub-CPNs are isolated)

LLM transitions at depth-1 receive context from depth-1's history only.
If cross-depth context is needed, it must be passed explicitly as a token payload.
```

---

## 8. The 7 Building Blocks Mapped to CPN Primitives

This section is the authoritative cross-reference between the engineering
guidelines and the CPN architecture.

| # | Block | Core principle | CPN primitive | NodeKind | Notes |
|---|---|---|---|---|---|
| 1 | Intelligence | LLM is last resort | `NodeKindLLM` + `LLMConfig` | `NodeKindLLM` | Only when deterministic code cannot solve it (Axiom A11) |
| 2 | Memory | Pass state; LLM is stateless | `ContextWindow` + `buildContext()` + observer tokens | — | Built before every LLM call; compressed via sub-CPN summaries |
| 3 | Tools | External system integration | `NodeKindTool` + `Executor` func | `NodeKindTool` | First choice for any transition. Zero LLM cost. |
| 4 | Validation | Enforce structured output | `NodeKindValidate` + `ValidateConfig` | `NodeKindValidate` | Validates ColorJSON tokens against schema; retry loop back to LLM |
| 5 | Control | Deterministic routing | CPN topology + guards + `CanFire()` | — | The topology IS the control flow. No LLM involved. |
| 6 | Recovery | Graceful failure | `RetryPolicy` + `CircuitBreakerState` | — | Per-transition. Backoff, circuit breaker, fallback tokens. |
| 7 | Feedback | Human oversight | `NodeKindHITL` + `HITLConfig` | `NodeKindHITL` | Single-turn approval or multi-round revision loop. |

### 8.1 Block 5 — Control is Topology

This block has no dedicated NodeKind because it IS the CPN itself.
The directed graph of places and transitions, combined with guards and
`CanFire()` rules, implements all deterministic routing:

- **If/else routing** → two transitions sharing an InputPlace with mutually exclusive guards
- **Switch/case routing** → N transitions sharing an InputPlace, each guard matching one case
- **Intent classification output routing** → one NodeKindLLM classifier produces a ColorJSON token with `{"intent": "..."}`, followed by N NodeKindTool routing transitions with deterministic guards that read the intent field
- **Parallel execution** → two transitions with independent InputPlaces fire concurrently (Axiom A8)
- **Synchronization / join** → one transition with multiple InputPlaces fires only when all are satisfied

```go
// Example: intent-based routing without LLM in the routing step
// The LLM classifies once; deterministic code routes.
Guard: func(tokens []Token) bool {
    for _, t := range tokens {
        if m, ok := t.Payload.(map[string]any); ok {
            return m["intent"] == "business_operation"
        }
    }
    return false
},
```

### 8.2 Block 6 — Recovery in the Executor

The retry loop is inside the executor's `fireWithRetry()` function.
It applies to any transition that has a non-nil `Retry` field.

```go
func fireWithRetry(ctx context.Context, t *Transition, c *CPN, fire func() error) error {
    if t.Retry == nil {
        return fire()
    }
    if t.cbState != nil && !t.cbState.Allow() {
        return ErrCircuitOpen
    }
    var lastErr error
    wait := t.Retry.InitialWait
    for attempt := 1; attempt <= t.Retry.MaxAttempts; attempt++ {
        err := fire()
        if err == nil {
            if t.cbState != nil {
                t.cbState.RecordSuccess()
            }
            return nil
        }
        lastErr = err
        if t.cbState != nil {
            t.cbState.RecordFailure()
        }
        if t.Retry.RetryOn != nil && !t.Retry.RetryOn(err, attempt) {
            break // non-retryable error
        }
        if attempt == t.Retry.MaxAttempts {
            break
        }
        // Exponential backoff with cap
        select {
        case <-time.After(wait):
        case <-ctx.Done():
            return ctx.Err()
        }
        wait = time.Duration(float64(wait) * t.Retry.Multiplier)
        if wait > t.Retry.MaxWait {
            wait = t.Retry.MaxWait
        }
    }
    return lastErr
}
```

When `fireWithRetry` exhausts all attempts, the transition deposits a `ColorError`
token into a designated error output place (if configured) instead of failing the
entire CPN. This enables fallback branches:

```go
// A transition with both normal and error output places.
// On retry exhaustion, executor deposits ColorError into "P:ERROR"
// instead of setting CPN.State = StateFailed.
// A downstream transition can consume P:ERROR and execute a fallback.
Transition{
    ID:           "fetch-data",
    Kind:         NodeKindTool,
    InputPlaces:  []string{"P:QUERY"},
    OutputPlaces: []string{"P:DATA"},           // normal path
    ErrorPlace:   "P:ERROR",                    // fallback path (v1.1 addition)
    Executor:     fetchDataTool,
    Retry:        DefaultRetryPolicy(),
}
```

### 8.3 Block 4 — Validation Flow

`NodeKindValidate` implements the schema-enforcement + LLM correction loop.

```
P:LLM_OUTPUT (JSON, Computation)
    ↓
[validate:check-schema]
    ├─ valid → P:VALIDATED (JSON, Computation)
    └─ invalid, attempt < MaxCorrections →
           P:CORRECTION_INPUT (JSON, Observation)
               ↓
         [llm:correct-json]
               ↓
         P:LLM_OUTPUT (JSON, Computation)    ← loops back
    └─ invalid, attempts exhausted →
           P:VALIDATION_ERROR (ERROR, Computation)
```

```go
func fireValidate(ctx context.Context, t *Transition, c *CPN) error {
    tok, _ := consumeAll(t.InputPlaces, c.Places)[0], nil
    schema := t.ValidateConfig.Schema
    corrections := 0

    for {
        err := validateAgainstSchema(tok.Payload, schema)
        if err == nil {
            // Valid — apply OnSuccess transform and deposit
            payload := tok.Payload
            if t.ValidateConfig.OnSuccess != nil {
                payload = t.ValidateConfig.OnSuccess(payload)
            }
            outTok := tok
            outTok.Payload = payload
            for _, pid := range t.OutputPlaces {
                c.Places[pid].Deposit(outTok)
            }
            return nil
        }

        corrections++
        if corrections > t.ValidateConfig.MaxCorrections || t.ValidateConfig.CorrectionLLMID == "" {
            // Give up — emit error token
            errTok := Token{
                Color:   ColorError,
                Payload: map[string]any{"error": err.Error(), "token": tok},
                Space:   SpaceComputation,
            }
            if t.ErrorPlace != "" {
                c.Places[t.ErrorPlace].Deposit(errTok)
                return nil // CPN continues on fallback branch
            }
            return ErrValidationFailed
        }

        // Send back to LLM for correction
        correctionTok := Token{
            Color: ColorJSON,
            Payload: map[string]any{
                "invalid_output": tok.Payload,
                "schema_error":   err.Error(),
                "instruction":    "Fix the JSON to match the schema. Return only valid JSON.",
            },
            Space: SpaceObservation,
        }
        corrLLMTransition := c.Transitions[t.ValidateConfig.CorrectionLLMID]
        corrLLMTransition.InputPlaces = []string{} // inject directly
        corrResult, corrErr := fireLLMDirect(ctx, corrLLMTransition, c, correctionTok)
        if corrErr != nil {
            return corrErr
        }
        tok = corrResult
    }
}
```

### 8.4 Block 7 — Revision Loop Pattern

When `HITLConfig.RevisionLoop == true`, the HITL transition supports multiple rounds:

```
[llm:draft-response] → P:DRAFT (ARTIFACT)
    ↓
[hitl:review] — presents draft to human
    human: HITLRevise → P:REVISION_FEEDBACK (HUMAN)
        ↓
    [llm:revise] — applies feedback
        ↓
    P:DRAFT (ARTIFACT) ← loops back to [hitl:review]

    human: HITLApprove → P:APPROVED (HUMAN)
        ↓
    [tool:execute] — executes the approved plan
```

```go
func fireHITLWithRevision(ctx context.Context, t *Transition, c *CPN) error {
    _ = consumeAll(t.InputPlaces, c.Places)
    cfg := t.HITLConfig
    rounds := 0

    for {
        c.emit(Event{Type: EventHITLRequested, Payload: map[string]any{
            "prompt": cfg.Prompt, "round": rounds,
        }})
        c.setState(StateWaiting)
        c.Group.SwitchCMP(c.ID)

        select {
        case tok := <-cfg.Channel:
            c.setState(StateRunning)
            c.Group.SwitchCMP(c.ID)

            resp, ok := tok.Payload.(HITLResponse)
            if !ok {
                resp = HITLResponse{Action: HITLApprove, Content: tok.Payload.(string)}
            }

            switch resp.Action {
            case HITLApprove:
                humanTok := Token{
                    Color: ColorHuman, Payload: resp.Content,
                    OriginKind: NodeKindHITL, Space: SpaceSurface,
                }
                for _, pid := range t.OutputPlaces {
                    c.Places[pid].Deposit(humanTok)
                }
                c.emit(Event{Type: EventHITLResolved})
                c.switchModeToCentaurian()
                return nil

            case HITLReject:
                return ErrHITLRejected

            case HITLRevise:
                if rounds >= cfg.MaxRevisions {
                    return ErrHITLMaxRevisions
                }
                rounds++
                // Fire correction LLM with the feedback
                revTok := Token{
                    Color: ColorJSON,
                    Payload: map[string]any{
                        "feedback": resp.Content,
                    },
                    Space: SpaceObservation,
                }
                revised, err := fireLLMDirect(ctx, c.Transitions[cfg.CorrectionLLMID], c, revTok)
                if err != nil {
                    return err
                }
                // Update the draft in place — deposit revised token back
                // to the surface place so it appears in the next HITL prompt
                cfg.Prompt = formatRevisionPrompt(revised)
                continue
            }

        case <-ctx.Done():
            c.setState(StateFailed)
            return ErrTimeout
        }
    }
}
```

---

## 9. Executor

### 9.1 Main Loop

Unchanged from v1.0 in structure. Extended to call `fireWithRetry` and
to handle `NodeKindValidate`.

```
GIVEN a valid CPN with initial marking
WHEN Run(ctx) is called:

  loop:
    1. Collect firable transitions:
         regular  = [T | Kind != NodeKindObserver AND CanFire(places, mode)]
         observer = [T | Kind == NodeKindObserver]

    2. For each T in regular:
         go fireWithRetry(ctx, T, CPN, func() error { return dispatch(ctx, T, CPN) })

    3. For each T in observer:
         drain EventBus → if EventFilter matches → T.fire(ctx, CPN)

    4. wg.Wait()

    5. if CPN.IsComplete() → CPN.State = StateCompleted → return nil

    6. if len(regular)==0 AND EventBus empty:
         if hasWaitingHITL() → CPN.State = StateWaiting → block
         else → CPN.State = StateFailed → return ErrDeadlock

    7. if ctx.Done() → CPN.State = StateFailed → return ErrTimeout

    8. goto loop
```

### 9.2 dispatch

Routes a transition to the correct fire function based on its NodeKind.

```go
func dispatch(ctx context.Context, t *Transition, c *CPN) error {
    switch t.Kind {
    case NodeKindTool:
        return fireTool(ctx, t, c)
    case NodeKindLLM:
        return fireLLM(ctx, t, c)
    case NodeKindValidate:
        return fireValidate(ctx, t, c)
    case NodeKindSubNet:
        return fireSubNet(ctx, t, c)
    case NodeKindHITL:
        if t.HITLConfig.RevisionLoop {
            return fireHITLWithRevision(ctx, t, c)
        }
        return fireHITL(ctx, t, c)
    default:
        return fmt.Errorf("%w: %s", ErrInvalidNodeKind, t.Kind)
    }
}
```

### 9.3 fireLLM — with context engineering

```go
func fireLLM(ctx context.Context, t *Transition, c *CPN) error {
    consumed := consumeAll(t.InputPlaces, c.Places)

    // Budget check before making the call (Axiom A11)
    if t.LLMConfig.Budget > 0 {
        req := buildLLMRequest(t, c, consumed)
        est, _ := c.LLMClient.EstimateCost(req)
        if est > t.LLMConfig.Budget {
            return fmt.Errorf("%w: estimated=%.4f budget=%.4f",
                ErrBudgetExceeded, est, t.LLMConfig.Budget)
        }
    }

    // Assemble context window (Axiom A12)
    cw := buildContext(c, t)
    // Append current input token as final user message
    cw.Messages = append(cw.Messages, LLMMessage{
        Role:    "user",
        Content: formatTokenPayload(consumed),
    })

    req := LLMRequest{
        Model:       t.LLMConfig.Model,
        Messages:    cw.Messages,
        MaxTokens:   t.LLMConfig.MaxTokens,
        Temperature: t.LLMConfig.Temperature,
        SessionID:   c.SessionID,
    }
    if t.LLMConfig.RequireJSON {
        req.ResponseFmt = "json_object"
    }
    // Resolve tool schemas for LLM-callable tools
    for _, toolID := range t.LLMTools {
        if tool, ok := c.Transitions[toolID]; ok {
            req.Tools = append(req.Tools, buildToolSchema(tool))
        }
    }

    resp, err := c.LLMClient.Complete(ctx, req)
    if err != nil {
        return err
    }

    // Handle tool calls (agentic loop — LLM invoked a tool)
    finalContent, err := handleToolCalls(ctx, resp, t.LLMTools, c)
    if err != nil {
        return err
    }

    // If streaming, chunks were already sent; finalContent has the full text
    outTok := Token{
        Color:       inferOutputColor(finalContent, t.LLMConfig.RequireJSON),
        Payload:     finalContent,
        OriginID:    c.ID,
        OriginDepth: c.Depth,
        OriginKind:  NodeKindLLM,
        Space:       SpaceComputation,
        SessionID:   c.SessionID,
    }
    for _, pid := range t.OutputPlaces {
        c.Places[pid].Deposit(outTok)
    }
    return nil
}
```

---

## 10. Topology-Based Hierarchy

No Orchestrator, Director, or Agent types. Identity comes from `Depth` and `Role`.

| Depth | Former name | Typical transitions |
|---|---|---|
| 0 | Orchestrator (root) | NodeKindLLM (classifier), NodeKindSubNet (domain CPNs), NodeKindHITL, NodeKindObserver |
| 1 | Director (domain) | NodeKindSubNet (workers), NodeKindObserver, NodeKindLLM |
| 2+ | Agent / Worker | NodeKindTool, NodeKindLLM, NodeKindValidate, NodeKindSubNet (deep nesting) |

---

## 11. SubNet Protocol

Unchanged from v1.0. Summary:

- Child CPN has its own place namespace (isolation)
- Child emits events via EventEmitter (observability)
- When child completes, output tokens are deposited into parent's OutputPlaces with OriginID/OriginDepth set
- Parent's GroupAgent manages lifecycle (Register → Deliver → Deregister)
- `compressSubNetSummary()` is called on completion to write a RoleObserver message into parent's History

---

## 12. HITL Protocol

Extends v1.0 with revision loop support. See §8.4 for the revision loop pattern.

Single-turn approval (unchanged from v1.0):
1. Consume input tokens atomically
2. Emit EventHITLRequested
3. Block on HITLConfig.Channel
4. Validate ColorHuman
5. Deposit into OutputPlaces
6. Switch CPN to ModeCentaurian if OutputPlace is SpaceComputation

Multi-round revision (new in v1.1):
1–3. Same as above
4. Parse HITLResponse.Action
5. If HITLApprove: deposit and exit loop
6. If HITLReject: return ErrHITLRejected
7. If HITLRevise: fire CorrectionLLM, update draft, loop back to step 2

---

## 13. MAS / Centaurian Mode Switching

Unchanged from v1.0. Summary:

| Condition | Mode |
|---|---|
| CPN created | ModeMAS |
| First ColorHuman token in SpaceComputation | ModeMAS → ModeCentaurian |
| All ColorHuman consumed, no HITL pending | ModeCentaurian → ModeMAS |
| Explicit `CPN.SetMode()` | Override |

In ModeCentaurian, `centaurianGuard` is auto-injected on computation transitions
without explicit Guard:
```go
func centaurianGuard(tokens []Token) bool {
    hasHuman, hasAI := false, false
    for _, t := range tokens {
        if t.IsHumanOrigin() { hasHuman = true } else { hasAI = true }
    }
    return hasHuman && hasAI
}
```

---

## 14. Session

```go
type ChannelType string

const (
    ChannelWeb      ChannelType = "web"
    ChannelWhatsApp ChannelType = "whatsapp"
    ChannelTelegram ChannelType = "telegram"
)

type Session struct {
    ID      string      // opaque UUID v4
    UserID  string
    Channel ChannelType

    Root   *CPN         // depth=0 CPN
    Stream chan StreamChunk

    // HITLInject — keyed by transition ID. Session layer injects human tokens here.
    HITLInject map[string]chan Token

    History   []Message
    CreatedAt time.Time
}

type StreamChunk struct {
    SessionID string
    CPNID     string
    CPNRole   string
    Content   string
    Done      bool
}

type MessageRole string

const (
    RoleUser      MessageRole = "user"
    RoleAssistant MessageRole = "assistant"
    RoleObserver  MessageRole = "observer" // compressed sub-CPN summary
)

type Message struct {
    ID        string
    Role      MessageRole
    Content   string
    CPNID     string
    CPNRole   string
    CPNDepth  int
    Timestamp time.Time
}

func (s *Session) ResolveHITL(transitionID string, resp HITLResponse) error {
    ch, ok := s.HITLInject[transitionID]
    if !ok {
        return ErrNoHITLWaiting
    }
    ch <- Token{
        Color:       ColorHuman,
        Payload:     resp,
        OriginID:    "human",
        OriginDepth: -1,
        OriginKind:  NodeKindHITL,
        Space:       SpaceSurface,
        SessionID:   s.ID,
        Timestamp:   time.Now(),
    }
    return nil
}
```

---

## 15. Error Types

```go
var (
    // v1.0 errors (unchanged)
    ErrColorMismatch    = errors.New("token color does not match place color set")
    ErrEmptyPlace       = errors.New("no tokens available in place")
    ErrInvalidArc       = errors.New("transition references non-existent place")
    ErrDeadlock         = errors.New("no transitions can fire but CPN is not complete")
    ErrTimeout          = errors.New("CPN execution exceeded timeout")
    ErrSpaceMismatch    = errors.New("token space does not match place space kind")
    ErrSpaceViolation   = errors.New("token cannot bypass observation space")
    ErrSubNetFailed     = errors.New("sub-CPN reached failed state")
    ErrNoHITLWaiting    = errors.New("no HITL transition is currently waiting")
    ErrInvalidNodeKind  = errors.New("transition kind is not recognized")
    ErrCentaurianGuard  = errors.New("centaurian guard failed: missing human or AI token")

    // v1.1 errors (new)
    ErrValidationFailed  = errors.New("token failed schema validation after max corrections")
    ErrCircuitOpen       = errors.New("circuit breaker is open: transition blocked")
    ErrRateLimited       = errors.New("LLM provider returned 429: rate limited")
    ErrProviderUnavailable = errors.New("LLM provider returned 5xx: unavailable")
    ErrBudgetExceeded    = errors.New("estimated LLM cost exceeds transition budget")
    ErrHITLRejected      = errors.New("human rejected the HITL gate")
    ErrHITLMaxRevisions  = errors.New("HITL revision loop exceeded max rounds")
)
```

---

## 16. Reference Networks

### 16.1 Financial Analysis (updated — validation and retry added)

```
P:INPUT (STRING, Surface)
    ↓
[llm:planner — cheapest model, JSON output] (Observation)
    ↓
[validate:plan-schema] (Observation) — validates planner JSON
    ├─ valid → P:TICKER + P:QUERY (JSON, Observation)
    └─ invalid → [llm:correct-plan] → retry
         ↓                              ↓
[tool:get_fin, retry=3]         [tool:web_search, retry=3]    ← parallel
         ↓                              ↓
   P:FIN_DATA                    P:WEB_SENT + P:WEB_SUMM
         ↓                          ↓           ↓
[tool:chart_gen]          [tool:sentiment]  [tool:summarizer]
         ↓                          ↓           ↓
    P:CHART                  P:SENTIMENT   P:SUMMARY
         └────────────────────────────────────┘
                              ↓
                       [llm:report — reasoning model]
                              ↓
                         P:OUTPUT (ARTIFACT)
```

Key model assignments for this network:
- `planner`: `ModelRegistry["classifier"]` — cheap, fast JSON extraction
- `correct-plan`: `ModelRegistry["structured"]` — JSON correction
- `report`: `ModelRegistry["reasoning"]` — complex synthesis

### 16.2 Spec Builder with Nested Repo Analyzer

Unchanged from v1.0. See previous spec for full code.
Key additions in v1.1: `repoAnalyzerFactory()` returns a CPN with
`Retry: DefaultRetryPolicy()` on all NodeKindTool transitions.

### 16.3 Centaurian Approval Pattern

Unchanged from v1.0. The `execute-change` transition gets centaurianGuard auto-injected.

### 16.4 Full-Stack Request with all 7 Blocks

This network demonstrates all 7 building blocks in a single CPN.
Scenario: user asks to "draft a marketing email and send it to the list."

```
Block 1 — Intelligence
Block 2 — Memory (via buildContext in all LLM transitions)
Block 3 — Tools (email send, list fetch)
Block 4 — Validation (email schema check)
Block 5 — Control (routing by intent, join gate)
Block 6 — Recovery (retry on email send failure)
Block 7 — Feedback (human reviews before send)

P:REQUEST (STRING, Surface)
    ↓
[tool:classify-intent] (Observation)        ← Block 5: deterministic routing
    ↓
P:CLASSIFIED (JSON, Observation)
    ↓  guard: intent=="marketing_email"
[tool:fetch-contact-list] (Computation)     ← Block 3
    ↓
P:CONTACT_LIST (JSON, Computation)
    ↓
[llm:draft-email — reasoning model]         ← Block 1
(buildContext includes past campaigns        ← Block 2
 from observer tokens)
    ↓
P:DRAFT_EMAIL (JSON, Computation)
    ↓
[validate:email-schema]                     ← Block 4
    ├─ valid → P:VALID_DRAFT
    └─ invalid → [llm:fix-email] → retry
    ↓
P:VALID_DRAFT (JSON, Computation)
    ↓
[hitl:review-email — revision loop]         ← Block 7
    ├─ Revise → [llm:revise-email] → loop
    └─ Approve → P:APPROVED_EMAIL (HUMAN)
    ↓
[tool:send-email, retry=DefaultRetryPolicy] ← Block 3 + Block 6
    ↓
P:SEND_RESULT (ARTIFACT, Computation)
```

```go
emailCPN := &CPN{
    Role: "marketing-email", Depth: 1, Mode: ModeMAS,
    Places: map[string]*Place{
        "P:REQUEST":      {Color: ColorString,   Space: SpaceSurface},
        "P:CLASSIFIED":   {Color: ColorJSON,     Space: SpaceObservation},
        "P:CONTACT_LIST": {Color: ColorJSON,     Space: SpaceComputation},
        "P:DRAFT_EMAIL":  {Color: ColorJSON,     Space: SpaceComputation},
        "P:VALID_DRAFT":  {Color: ColorJSON,     Space: SpaceComputation},
        "P:APPROVED":     {Color: ColorHuman,    Space: SpaceSurface},
        "P:SEND_RESULT":  {Color: ColorArtifact, Space: SpaceComputation},
        "P:EMAIL_ERROR":  {Color: ColorError,    Space: SpaceComputation},
    },
    Transitions: map[string]*Transition{
        // Block 5: deterministic classification — no LLM
        "classify-intent": {
            Kind:         NodeKindTool,
            InputPlaces:  []string{"P:REQUEST"},
            OutputPlaces: []string{"P:CLASSIFIED"},
            Executor:     classifyEmailIntentTool, // regex + keyword matching
        },
        // Block 3: tool call — no LLM
        "fetch-contacts": {
            Kind:         NodeKindTool,
            InputPlaces:  []string{"P:CLASSIFIED"},
            OutputPlaces: []string{"P:CONTACT_LIST"},
            Executor:     fetchContactListTool,
            Guard: func(tokens []Token) bool {
                m, _ := tokens[0].Payload.(map[string]any)
                return m["intent"] == "marketing_email"
            },
        },
        // Block 1: LLM — justified because drafting requires reasoning
        "draft-email": {
            Kind:         NodeKindLLM,
            InputPlaces:  []string{"P:CONTACT_LIST"},
            OutputPlaces: []string{"P:DRAFT_EMAIL"},
            SystemPrompt: "You are a marketing copywriter. Draft a personalized email for this contact list segment.",
            LLMConfig: LLMConfig{
                Model:       ModelRegistry["reasoning"],
                MaxTokens:   800,
                Temperature: 0.7,
                RequireJSON: true,
            },
        },
        // Block 4: validation — schema enforces subject/body/cta fields
        "validate-email": {
            Kind:         NodeKindValidate,
            InputPlaces:  []string{"P:DRAFT_EMAIL"},
            OutputPlaces: []string{"P:VALID_DRAFT"},
            ErrorPlace:   "P:EMAIL_ERROR",
            ValidateConfig: ValidateConfig{
                Schema:          emailSchema,
                MaxCorrections:  2,
                CorrectionLLMID: "fix-email",
            },
        },
        "fix-email": {
            Kind:         NodeKindLLM,
            InputPlaces:  []string{},
            OutputPlaces: []string{},
            SystemPrompt: "Fix the JSON to match the email schema exactly. Return only valid JSON.",
            LLMConfig:    LLMConfig{Model: ModelRegistry["structured"], MaxTokens: 400, RequireJSON: true},
        },
        // Block 7: HITL with revision loop
        "review-email": {
            Kind:         NodeKindHITL,
            InputPlaces:  []string{"P:VALID_DRAFT"},
            OutputPlaces: []string{"P:APPROVED"},
            HITLConfig: HITLConfig{
                Channel:         make(chan Token, 1),
                Prompt:          "Please review the email draft. Approve to send, or provide revision feedback.",
                RevisionLoop:    true,
                CorrectionLLMID: "revise-email",
                MaxRevisions:    3,
            },
        },
        "revise-email": {
            Kind:         NodeKindLLM,
            InputPlaces:  []string{},
            OutputPlaces: []string{},
            SystemPrompt: "Revise the email draft based on the feedback. Return updated JSON.",
            LLMConfig:    LLMConfig{Model: ModelRegistry["reasoning"], MaxTokens: 800, RequireJSON: true},
        },
        // Block 3 + Block 6: tool call with retry
        "send-email": {
            Kind:         NodeKindTool,
            InputPlaces:  []string{"P:APPROVED", "P:CONTACT_LIST"},
            OutputPlaces: []string{"P:SEND_RESULT"},
            ErrorPlace:   "P:EMAIL_ERROR",
            Executor:     sendEmailTool,
            Retry:        DefaultRetryPolicy(),
        },
    },
}
```

---

## 17. Flow Intelligence Engine

Extended in v1.1 to track OpenRouter costs per crystallized flow.

### Phase 1 (MVP)
- Record per-CPN execution: transitions fired, tokens produced, duration, success/failure.
- Record per-session token usage from TokenLedger.
- Ranking: fewer LLM calls + lower cost + same output quality = higher score.
- This directly operationalizes Axiom A11: flows with more `NodeKindTool` transitions
  and fewer `NodeKindLLM` transitions rank higher for the same output quality.

### Phase 2 — Pattern Mining
- Detect repeated sub-CPN topologies across sessions.
- Candidate for crystallization: ≥ N occurrences, success rate ≥ threshold.
- HITL gate required before promotion.

### Phase 3 — Flow Crystallization
- Crystallized flows stored as `*CPN` in FlowLibrary (keyed by topology hash).
- Cost profile attached to each crystallized flow (average USD per execution).
- Cross-client replication with cost transparency.

---

## 18. Linux Sandbox

Unchanged. Dedicated non-sudoer user per session. NodeKindTool executors
that need filesystem access run under this user. Available: `ls`, `cd`, `cat`,
`grep`, `find`, `echo`, `pwd`.

---

## 19. Package Structure

```
cpn/
├── colors.go         // ColorSet constants (updated: ColorSchema, ColorError)
├── space.go          // SpaceKind constants
├── kinds.go          // NodeKind constants (updated: NodeKindValidate)
├── mode.go           // CPNMode, CPNState constants
├── token.go          // Token struct, IsHumanOrigin()
├── place.go          // Place, Deposit, Consume, Peek
├── retry.go          // RetryPolicy, CircuitBreakerConfig, CircuitBreakerState, fireWithRetry
├── validate.go       // ValidateConfig, fireValidate, validateAgainstSchema
├── transition.go     // Transition, CanFire, LLMConfig, HITLConfig, ValidateConfig
├── group_agent.go    // GroupAgent protocol
├── cpn.go            // CPN struct, TerminalPlaces, IsComplete, emit
├── executor.go       // Run loop, dispatch, fireWithRetry, fireTool, fireLLM, fireSubNet, fireHITL, fireHITLWithRevision, fireValidate, drainObservers
├── memory.go         // ContextWindow, buildContext, compressSubNetSummary, estimateTokens
├── openrouter.go     // OpenRouterClient, LLMRequest, LLMResponse, LLMMessage, LLMTool
├── token_ledger.go   // TokenLedger, SessionTokenRecord
├── model_registry.go // ModelRegistry map
├── validator.go      // CPN-level Validate (arc refs, space constraints, HITL channel init)
├── errors.go         // all typed errors
├── session.go        // Session, StreamChunk, Message, ResolveHITL
├── flow_library.go   // FlowLibrary
├── sandbox.go        // SandboxedTool
└── cpn_test.go       // one test function per spec statement
```

---

## 20. Implementation Order

```
Step 1  — Base types (no logic)
          colors.go · space.go · kinds.go · mode.go · errors.go · token.go
          Goal: go build with zero errors.

Step 2  — Place operations
          Deposit, Consume, Peek with sync.Mutex
          Tests: Spec 5.1–5.4 (original) + SpaceMismatch + SpaceViolation

Step 3  — Transition CanFire
          Tests: Spec 6.1–6.3 (original) + Guard cases

Step 4  — RetryPolicy and CircuitBreaker
          retry.go: fireWithRetry, backoff math, circuit breaker state machine
          Tests: exhausted retries, circuit trips after N failures, backoff timing

Step 5  — Validator
          arc references, SpaceViolation rule, HITL channel init
          Tests: ErrInvalidArc, ErrSpaceViolation

Step 6  — Executor (MAS mode, NodeKindTool only, with retry)
          Run loop + goroutines + WaitGroup + deadlock + timeout + fireWithRetry
          Tests: Spec 8.1–8.4 (original) + retry integration

Step 7  — OpenRouter client
          openrouter.go: Complete(), EstimateCost(), TokenLedger
          Tests: mock HTTP server, rate limit handling, budget check

Step 8  — Memory
          memory.go: buildContext(), compressSubNetSummary(), estimateTokens()
          Tests: T2 observer messages always included, T3 sliding window

Step 9  — NodeKindLLM
          fireLLM: context assembly, budget check, tool call loop, streaming
          Tests: correct context window sent, budget exceeded error, tool call dispatched

Step 10 — NodeKindValidate
          validate.go: fireValidate, schema validation, correction loop
          Tests: valid pass-through, 1 correction success, max corrections → ErrorPlace

Step 11 — GroupAgent
          group_agent.go: Register, Deliver, Deregister, SwitchCMP
          Tests: paper pseudocode verified

Step 12 — NodeKindSubNet
          fireSubNet, event bus wiring, output token deposit, compressSubNetSummary
          Tests: nested CPN completes, tokens in parent, summary in parent history

Step 13 — NodeKindObserver
          drainObservers in executor loop
          Tests: observer fires on matching event, filtered events ignored

Step 14 — NodeKindHITL (single-turn)
          fireHITL, StateWaiting, channel inject, Centaurian mode switch
          Tests: CPN waits, inject ColorHuman, resumes, mode switches

Step 15 — NodeKindHITL (revision loop)
          fireHITLWithRevision: Approve/Reject/Revise actions, max revisions cap
          Tests: approve exits, revise loops, reject returns error, max revisions

Step 16 — ModeCentaurian
          centaurianGuard injection, mode switch trigger
          Tests: reference net 16.3 (approval pattern) passing end-to-end

Step 17 — Session and channel layer
          Session, StreamChunk routing, multi-channel adapters, ResolveHITL
          Tests: HITL inject via Session, stream chunks routed by channel type

Step 18 — Flow Intelligence Engine Phase 1
          Metrics: transition count, LLM call count, total cost from TokenLedger
          Ranking: fewer LLM calls + lower cost = higher score
          Tests: ranking correctly prefers lower-LLM-count flows
```

---

## 21. Design Decisions Log

| Decision | Value | Reasoning |
|---|---|---|
| Single CPN type | `CPN` with `Depth` + `Role` | Topology defines hierarchy — no parallel type hierarchies |
| SubNet isolation | Own place namespace | Forces explicit token contracts at boundaries |
| Sub-CPN observability | Event emission + Observer nodes | Parent visibility without breaking isolation |
| HITL as NodeKind | `NodeKindHITL` transition | First-class computational step, integrates into firing semantics |
| Mode switch on HITL | Automatic on first ColorHuman in Computation | Centaurian mode is earned by human participation |
| Centaurian guard injection | Automatic for Computation transitions without Guard | Protection is default, not opt-in |
| Observer has no InputPlaces | Driven by EventBus | Observation is continuous, not discrete |
| Token carries OriginDepth | Always | Centaurian guard + depth-aware context filtering |
| GroupAgent per CPN | Each CPN manages its sub-CPNs | Paper §4.2 protocol; sub-CPNs are the new ephemeral teams |
| Session owns HITLInject | By transition ID | Multiple HITL branches can coexist |
| OpenRouter as LLM proxy | Single implementation of LLMClient | Unified billing, model switching, per-user tracking |
| Model assigned per transition | `LLMConfig.Model` | Axiom A11 extension: use cheapest model that works |
| Budget per transition | `LLMConfig.Budget` | Prevents runaway costs in production |
| RetryPolicy per transition | `Transition.Retry` | Recovery is granular; not all transitions need the same policy |
| Circuit breaker per transition | `RetryPolicy.CircuitBreaker` | Prevents hammering a failing dependency |
| NodeKindValidate as first-class | Separate NodeKind, not a guard | Validation + correction loop is a distinct computational pattern |
| ErrorPlace on Transition | `Transition.ErrorPlace string` | Enables fallback branches without failing the CPN |
| buildContext as explicit step | `ContextWindow` + `buildContext()` | Axiom A12: context assembly is separate from inference |
| Observer tokens as compressed memory | `RoleObserver` in History | Sub-CPN summaries replace raw execution history — context window stays bounded |
| TokenLedger in OpenRouterClient | Thread-safe per-session records | Billing + Flow Intelligence can rank by actual cost |
| LLM is last resort (Axiom A11) | NodeKindTool is first choice | LLM calls are expensive and slow; deterministic code is free |
| Context engineering is core skill (A12) | buildContext() is required before every LLM call | Output quality depends on context quality, not just model choice |

---

## 22. What This Document Does Not Include (Future Work)

- Persistence: Session Store and Event Store in memory. Redis/Postgres next phase.
- Multi-tenancy and authentication.
- Rate limiting per channel.
- Billing UI and per-client cost dashboards.
- HITL review UI (web interface for approval workflows).
- CPN topology serialization (JSON marshaling of `*CPN`) for persistence and `ColorCPN` tokens.
- Distributed executor: sub-CPNs on separate machines (requires serializable tokens + network transport for EventBus).
- Formal verification: reachability, liveness, boundedness analysis via CPN Tools integration.
- CPN visual editor.
- OpenRouter streaming (server-sent events) integration for `StreamOutput: true` transitions.
- Model fallback configuration: automatic retry with cheaper model on `ErrBudgetExceeded`.
