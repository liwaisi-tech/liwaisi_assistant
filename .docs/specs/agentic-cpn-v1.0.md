# Unified Agentic CPN — Spec Driven Design
> Version 1.0 — Supersedes: Agentic OS v0.1 · CPN Tool Engine v0.1  
> Theoretical foundation: Borghoff, Bottoni & Pareschi (2025) — *Human-Artificial Interaction in the Age of Agentic AI*

---

## 0. Related Documents

| Document | Description | Status |
|---|---|---|
| **Unified Agentic CPN — Spec Driven Design** (this document) | Unified architecture: CPN as the single substrate for tools, agents, and coordination | v1.0 |
| Agentic OS — Spec Driven Development | Superseded. Concepts absorbed and redesigned below | archived |
| CPN Tool Engine — Spec Driven Design | Superseded. Engine extended and unified below | archived |

---

## 1. Design Axioms

These axioms are the non-negotiable constraints that every implementation decision must respect.
They derive directly from three sources: the paper's formal framework, the previous specs,
and the three architectural decisions made before writing this document.

| # | Axiom | Source |
|---|---|---|
| A1 | **Everything is a CPN.** A tool, an agent, a director, and the orchestrator are all instances of the same `CPN` type. Their identity emerges from topology (depth) and role label, not from distinct types. | Decision Q2 |
| A2 | **Sub-CPNs are isolated.** A child CPN has its own place namespace. It cannot read or write parent places directly. | Decision Q1 |
| A3 | **Sub-CPNs are observable.** A child CPN emits events to a shared bus. The parent observes via `NodeKindObserver` transitions. This is the Observation Space made concrete. | Decision Q1 + Paper §4.2 |
| A4 | **HITL is a first-class node kind.** A `NodeKindHITL` transition blocks its branch until a human-provided token arrives on a channel. The CPN executor understands this contract natively. | Decision Q3 |
| A5 | **Three communication spaces are structural.** Every Place belongs to one of: Surface, Observation, or Computation space. The executor uses this to enforce data pipeline integrity. | Paper §4.2 |
| A6 | **Tokens carry origin.** Every token knows which CPN produced it and its depth. This enables Centaurian guards (require human-origin AND AI-origin tokens) and full traceability. | Paper §4.4 |
| A7 | **MAS and Centaurian are runtime modes.** A CPN can switch between `ModeMAS` (independent firing) and `ModeCentaurian` (co-trigger guards) at any iteration of the executor loop. | Paper §2.4 |
| A8 | **Parallelism is topological.** Concurrency is never declared explicitly. It emerges from the firing rule: any transition whose input places are satisfied fires in its own goroutine. | Paper §3.1 |
| A9 | **Event Store is append-only.** No event is ever modified. The Group-Agent protocol (register/deliver/deregister/switchCMP) operates over this store. | Spec v0.1 + Paper §4.2 |
| A10 | **The human channel is the only interface.** The user never sees topology, depth, or mode. They see only the stream from the surface space of the root CPN. | Spec v0.1 §2 |

---

## 2. Glossary

Terms mapped to their formal names and their location in the new model.

| Term in system | Formal name | Where it lives now |
|---|---|---|
| Orchestrator | Root CPN | `CPN` at `Depth=0` |
| Director | Domain CPN | `CPN` at `Depth=1`, spawned as `NodeKindSubNet` |
| Agent / Worker | Worker CPN | `CPN` at `Depth=2+`, spawned as `NodeKindSubNet` |
| Tool call | Tool transition | `Transition` with `NodeKindTool` |
| Nested agent / SubNet | Sub-CPN | `Transition` with `NodeKindSubNet` holding a `*CPN` |
| Human gate | HITL transition | `Transition` with `NodeKindHITL` |
| LLM reasoning step | LLM transition | `Transition` with `NodeKindLLM` |
| Observation bridge | Observer transition | `Transition` with `NodeKindObserver` |
| Communication space | SpaceKind on Place | `Place.Space` field (`Surface`/`Observation`/`Computation`) |
| Token type | Color set | `Token.Color` (`STRING`, `JSON`, `ARTIFACT`, `SCORE`, `EVENT`, `HUMAN`, `CPN`) |
| Token origin | Origin fields | `Token.OriginID`, `Token.OriginDepth` |
| Centaurian synergy | Co-trigger guard | `Transition.Guard` checking both `ColorHuman` and non-human origin tokens |
| Group-agent | GroupAgent struct | `CPN.Group` — manages active/non-active sub-CPNs |
| Ephemeral team | Active sub-CPN group | Sub-CPNs registered in `GroupAgent.Active` |
| Thinking block | Observer token | `TOKEN{Color: ColorEvent}` carrying sub-CPN summary |
| Flow crystallization | Reusable SubNet | Saved `*CPN` topology in `FlowLibrary` |
| Data flywheel | Flow Intelligence Engine | Consumes Event Store asynchronously |
| HITL | NodeKindHITL | Blocks branch on `Transition.HITLChannel` |
| Prototype + fork() | SubNetFactory | `Transition.SubNetFactory func() *CPN` |

---

## 3. Core Types

### 3.1 SpaceKind

Maps directly to the paper's three communication layers.
Every Place must declare which space it belongs to.
The executor uses this to enforce that raw tokens never skip the Observation space.

```go
type SpaceKind string

const (
    // SpaceSurface — mediates contact with the outside world.
    // User input, sensor data, external API responses land here.
    // In MAS: message-passing protocols and event listeners.
    // In Centaurian: merged human sensory input and AI-driven data capture.
    SpaceSurface SpaceKind = "surface"

    // SpaceObservation — bridges surface and computation.
    // Handles message transformation, routing, light coordination.
    // Observer transitions live here. Sub-CPN events are received here.
    // No raw token from SpaceSurface may appear directly in SpaceComputation —
    // it must pass through an Observation transition first.
    SpaceObservation SpaceKind = "observation"

    // SpaceComputation — the system's core.
    // Decision-making, resource allocation, final outputs.
    // In MAS: independent modules with partial state.
    // In Centaurian: human+AI co-trigger transitions.
    SpaceComputation SpaceKind = "computation"
)
```

### 3.2 ColorSet

Extended from the previous spec. New colors support the observer pattern,
HITL protocol, and nested CPN token passing.

```go
type ColorSet string

const (
    ColorString   ColorSet = "STRING"    // raw text, queries, prompts
    ColorJSON     ColorSet = "JSON"      // structured data between transitions
    ColorArtifact ColorSet = "ARTIFACT"  // final deliverables: reports, code, specs
    ColorScore    ColorSet = "SCORE"     // numeric analysis results

    // New in v1.0

    // ColorEvent — carries an Event emitted by a sub-CPN.
    // Only valid in SpaceObservation places.
    // Observer transitions consume this color.
    ColorEvent ColorSet = "EVENT"

    // ColorHuman — carries input explicitly provided by a human.
    // Only produced by NodeKindHITL transitions.
    // Centaurian guard functions require at least one token of this color
    // to co-trigger computation transitions.
    ColorHuman ColorSet = "HUMAN"

    // ColorCPN — carries a *CPN reference as a token payload.
    // Used when a CPN instance is passed as data (e.g., a reusable
    // sub-topology injected at runtime by the Flow Intelligence Engine).
    ColorCPN ColorSet = "CPN"
)
```

### 3.3 NodeKind

Every Transition has a Kind. This replaces the implicit distinction between
"tools", "agents", and "coordination steps" from the previous specs.

```go
type NodeKind string

const (
    // NodeKindTool — calls a synchronous or async Executor function.
    // The original CPN Tool Engine transition. Unchanged semantics.
    NodeKindTool NodeKind = "tool"

    // NodeKindSubNet — spawns a child CPN as a unit of work.
    // Input tokens become the child's initial marking.
    // Child runs in its own goroutine with isolated place namespace.
    // Child emits events via EventEmitter; parent observes via NodeKindObserver.
    // Output tokens from the child's terminal places are deposited
    // into this transition's OutputPlaces when the child reaches COMPLETED.
    NodeKindSubNet NodeKind = "subnet"

    // NodeKindObserver — listens to the EventBus for events from sub-CPNs.
    // When a matching event arrives, it is materialized as a token
    // of ColorEvent and deposited in OutputPlaces.
    // This is the concrete implementation of the Observation Space bridge.
    NodeKindObserver NodeKind = "observer"

    // NodeKindHITL — blocks its branch until a human token arrives.
    // The executor transitions the CPN to StateWaiting.
    // The Session layer injects the human token via HITLChannel.
    // Once received, the token (ColorHuman) flows to OutputPlaces
    // and execution resumes normally.
    NodeKindHITL NodeKind = "hitl"

    // NodeKindLLM — calls an LLM with the token payload as context.
    // The system prompt, available tools, and conversation history
    // are assembled from the token and the CPN's session context.
    // Output is a token of ColorJSON or ColorArtifact.
    NodeKindLLM NodeKind = "llm"
)
```

### 3.4 CPNMode

```go
// CPNMode — controls the firing semantics of the executor.
type CPNMode string

const (
    // ModeMAS — Multi-Agent System mode.
    // Transitions fire independently whenever their input places are satisfied.
    // No cross-agent token coordination required.
    // Default mode. Used for routine, well-defined tasks.
    ModeMAS CPNMode = "mas"

    // ModeCentaurian — deep human-AI integration mode.
    // Computation-space transitions require co-trigger: their guard function
    // must find at least one token of ColorHuman AND at least one token
    // of non-human origin before firing is allowed.
    // Activated automatically when a NodeKindHITL transition deposits
    // a ColorHuman token into a shared Computation place.
    ModeCentaurian CPNMode = "centaurian"
)
```

### 3.5 CPNState

```go
type CPNState string

const (
    StateIdle      CPNState = "idle"
    StateRunning   CPNState = "running"
    StateWaiting   CPNState = "waiting"   // blocked on NodeKindHITL
    StateCompleted CPNState = "completed"
    StateFailed    CPNState = "failed"
)
```

### 3.6 Token

Tokens are enriched with origin metadata. This is the key addition
that enables Centaurian guards and full traceability.

```go
// Token — the fundamental unit of data in the CPN.
// Immutable once created. Never modified in place.
type Token struct {
    // Core data
    Color   ColorSet
    Payload any

    // Origin — who produced this token
    OriginID    string    // ID of the CPN that produced this token
    OriginDepth int       // depth of the producing CPN (0=root, 1=domain, 2+=worker)
    OriginKind  NodeKind  // kind of transition that produced this token

    // Spatial metadata
    Space SpaceKind // which space this token currently occupies

    // Traceability
    SessionID string
    Timestamp time.Time
}

// IsHumanOrigin returns true if this token was produced by a HITL transition.
// Used by Centaurian guard functions.
func (t Token) IsHumanOrigin() bool {
    return t.Color == ColorHuman || t.OriginKind == NodeKindHITL
}
```

### 3.7 Place

Extended with SpaceKind. The executor validates that tokens deposited
in a place match both the Color and Space constraints.

```go
// Place — typed buffer with spatial identity.
// Thread-safe: all mutations go through Deposit/Consume.
type Place struct {
    ID    string
    Color ColorSet
    Space SpaceKind

    Tokens []Token
    mu     sync.Mutex
}

func (p *Place) Deposit(t Token) error {
    if t.Color != p.Color {
        return ErrColorMismatch
    }
    if t.Space != p.Space {
        return ErrSpaceMismatch
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

### 3.8 Transition

The central type. NodeKind determines which fields are active.
Fields for inactive kinds are zero-valued and ignored by the executor.

```go
// Transition — a unit of computation with a specific kind.
// Only the fields relevant to the transition's Kind are populated.
type Transition struct {
    ID           string
    Kind         NodeKind
    InputPlaces  []string
    OutputPlaces []string

    // Guard — optional. If non-nil, must return true for CanFire() to proceed.
    // In ModeCentaurian, computation-space transitions automatically get
    // an injected Centaurian guard if none is provided explicitly.
    Guard func(tokens []Token) bool

    // ── NodeKindTool ────────────────────────────────────────────────────────
    ToolName string
    Executor func(ctx context.Context, in Token) (Token, error)

    // ── NodeKindSubNet ───────────────────────────────────────────────────────
    // SubNetFactory is preferred over SubNet for dynamic/forked sub-CPNs.
    // If SubNet is set, it is used directly (for crystallized/reusable flows).
    // If SubNetFactory is set, it is called once per firing (prototype + fork).
    SubNet        *CPN
    SubNetFactory func() *CPN

    // ── NodeKindObserver ─────────────────────────────────────────────────────
    // ObservedCPNID — if set, only events from this sub-CPN are consumed.
    // Empty string means observe all sub-CPNs in the parent's Group.
    ObservedCPNID string
    EventFilter   func(e Event) bool // nil = accept all

    // ── NodeKindHITL ─────────────────────────────────────────────────────────
    // HITLChannel — the executor blocks on receive from this channel.
    // The Session layer injects the human token by sending to this channel.
    HITLChannel chan Token
    HITLPrompt  string // shown to the human via the surface stream

    // ── NodeKindLLM ──────────────────────────────────────────────────────────
    SystemPrompt string
    LLMClient    LLMClient
    // Tools available to the LLM call. Each tool is itself a Transition
    // with NodeKindTool, allowing the LLM to trigger CPN sub-executions.
    LLMTools []string // Transition IDs in the same CPN that the LLM may invoke
}

// CanFire returns true if all InputPlaces have at least one token
// and the Guard (if set) passes on the current token set.
func (t *Transition) CanFire(places map[string]*Place) bool {
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

### 3.9 GroupAgent

Direct implementation of the paper's group-agent protocol (§4.2).
Manages the lifecycle of sub-CPNs (the former "ephemeral teams").

```go
// GroupAgent — manages active/non-active sub-CPNs for a parent CPN.
// Implements the register/deliver/deregister/switchCMP protocol from the paper.
type GroupAgent struct {
    ID    string
    Topic string // semantic label for this group's concern

    active    []*CPN
    nonActive []*CPN
    mu        sync.RWMutex
}

// Register adds a sub-CPN to the group.
// If the sub-CPN is in StateRunning or StateIdle, it goes to active.
// Otherwise it goes to non-active.
func (g *GroupAgent) Register(c *CPN) {
    g.mu.Lock()
    defer g.mu.Unlock()
    if c.State == StateRunning || c.State == StateIdle {
        g.active = append(g.active, c)
    } else {
        g.nonActive = append(g.nonActive, c)
    }
}

// Deliver sends an event to all active sub-CPNs whose EventEmitter is set.
// This is the fan-out mechanism for group communication.
func (g *GroupAgent) Deliver(e Event) {
    g.mu.RLock()
    defer g.mu.RUnlock()
    for _, c := range g.active {
        if c.EventEmitter != nil {
            select {
            case c.EventEmitter <- e:
            default: // non-blocking; sub-CPN's bus is full, skip
            }
        }
    }
}

// Deregister removes an active sub-CPN from the group.
// Only active sub-CPNs may be deregistered (paper constraint).
func (g *GroupAgent) Deregister(id string) {
    g.mu.Lock()
    defer g.mu.Unlock()
    g.active = filter(g.active, func(c *CPN) bool { return c.ID != id })
}

// SwitchCMP moves a sub-CPN between active and non-active lists
// based on its current state.
func (g *GroupAgent) SwitchCMP(id string) {
    g.mu.Lock()
    defer g.mu.Unlock()
    // Move from active → non-active
    for i, c := range g.active {
        if c.ID == id && (c.State == StateCompleted || c.State == StateFailed || c.State == StateWaiting) {
            g.active = append(g.active[:i], g.active[i+1:]...)
            g.nonActive = append(g.nonActive, c)
            return
        }
    }
    // Move from non-active → active
    for i, c := range g.nonActive {
        if c.ID == id && (c.State == StateRunning || c.State == StateIdle) {
            g.nonActive = append(g.nonActive[:i], g.nonActive[i+1:]...)
            g.active = append(g.active, c)
            return
        }
    }
}

func filter(cpns []*CPN, f func(*CPN) bool) []*CPN {
    out := cpns[:0]
    for _, c := range cpns {
        if f(c) {
            out = append(out, c)
        }
    }
    return out
}
```

### 3.10 CPN — The Unified Type

This is the single type that was previously three types
(Orchestrator, Director, AgentInstance). Identity comes from `Depth` and `Role`.

```go
// CPN — the universal agent type.
// At Depth=0: root coordinator (was Orchestrator).
// At Depth=1: domain coordinator (was Director).
// At Depth=2+: worker (was AgentInstance).
// A CPN may contain other CPNs as NodeKindSubNet transitions.
type CPN struct {
    ID   string
    Role string   // semantic label: "root", "engineering", "data-analyst", etc.
    Depth int     // 0 = root, 1 = domain, 2+ = worker

    Mode  CPNMode
    State CPNState
    Error error

    // The net structure
    Places      map[string]*Place
    Transitions map[string]*Transition

    // Sub-CPN management (group-agent protocol)
    Group *GroupAgent

    // Event bus for hybrid scoping
    // EventEmitter: this CPN sends events here (read by parent's observer transitions)
    // EventBus: this CPN receives events here (from its own sub-CPNs)
    EventEmitter chan<- Event
    EventBus     <-chan Event

    // Session context (carried for traceability and LLM history)
    SessionID string
    History   []Message // conversation history for LLM transitions

    mu sync.RWMutex
}

// TerminalPlaces returns all places that have no outgoing transitions.
// A CPN is COMPLETED when all terminal places have tokens.
func (c *CPN) TerminalPlaces() []*Place {
    // find places not referenced in any InputPlaces
    referenced := map[string]bool{}
    for _, t := range c.Transitions {
        for _, pid := range t.InputPlaces {
            referenced[pid] = true
        }
    }
    var out []*Place
    for _, p := range c.Places {
        if !referenced[p.ID] {
            out = append(out, p)
        }
    }
    return out
}

// IsComplete returns true when all terminal places hold at least one token.
func (c *CPN) IsComplete() bool {
    for _, p := range c.TerminalPlaces() {
        ts, ok := p.Peek()
        if !ok || len(ts) == 0 {
            return false
        }
    }
    return true
}
```

### 3.11 Event

The append-only record emitted by sub-CPNs and consumed by observer transitions.
This is the bridge of the Observation Space.

```go
type EventType string

const (
    EventTransitionFired    EventType = "transition_fired"
    EventSubNetStarted      EventType = "subnet_started"
    EventSubNetCompleted    EventType = "subnet_completed"
    EventSubNetFailed       EventType = "subnet_failed"
    EventHITLRequested      EventType = "hitl_requested"
    EventHITLResolved       EventType = "hitl_resolved"
    EventTokenDeposited     EventType = "token_deposited"
    EventModeSwitch         EventType = "mode_switch"
    EventStreamChunk        EventType = "stream_chunk"
)

type Event struct {
    ID         string
    Type       EventType
    SessionID  string

    // Origin
    CPNID    string // which CPN emitted this event
    CPNDepth int
    CPNRole  string

    // Transition context (if applicable)
    TransitionID   string
    TransitionKind NodeKind

    // Token snapshot (if applicable)
    Token *Token

    Payload   map[string]any
    Timestamp time.Time
}
```

### 3.12 Session

Simplified from the previous spec. A session is now just the root CPN
plus the channel for streaming output and the HITL injection point.

```go
type ChannelType string

const (
    ChannelWeb      ChannelType = "web"
    ChannelWhatsApp ChannelType = "whatsapp"
    ChannelTelegram ChannelType = "telegram"
)

// Session — binds a user to their root CPN and streaming channel.
// No Orchestrator/Director knowledge here. The root CPN IS the session's brain.
type Session struct {
    ID      string      // opaque UUID v4
    UserID  string
    Channel ChannelType

    // Root CPN — depth=0. Handles intent classification, routing,
    // spawning domain sub-CPNs, and streaming output.
    Root *CPN

    // Stream — the root CPN writes StreamChunks here.
    // The Session layer routes them to the correct channel adapter.
    Stream chan StreamChunk

    // HITLInject — when a HITL transition is waiting, the Session layer
    // sends the human's token here. The executor's HITL handler reads it.
    // Keyed by transition ID so multiple HITL blocks can coexist.
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
    // RoleObserver — thinking blocks: summaries from completed sub-CPNs.
    // These are observer tokens materialized into the conversation history.
    RoleObserver MessageRole = "observer"
)

type Message struct {
    ID          string
    Role        MessageRole
    Content     string
    CPNID       string
    CPNRole     string
    CPNDepth    int
    Timestamp   time.Time
}
```

---

## 4. Communication Spaces — Structural Rules

The three spaces are not just labels. The executor enforces them.

### 4.1 Surface Space

**Purpose:** All contact with the outside world.
User messages, sensor data, API responses, tool inputs provided by external systems.

**Rules:**
- Input places of the root CPN (depth=0) are always `SpaceSurface`.
- A `NodeKindHITL` transition deposits `ColorHuman` tokens with `Space: SpaceSurface`.
- A `NodeKindObserver` transition that materializes a sub-CPN event deposits with `Space: SpaceObservation`, not Surface.
- The executor **rejects** any attempt to place a Surface token directly into a Computation place (enforces `ErrSpaceViolation`).

### 4.2 Observation Space

**Purpose:** Transformation, routing, and monitoring.
Observer transitions live here. Sub-CPN event tokens land here.
The summary (thinking block) of a completed sub-CPN is an Observation-space token.

**Rules:**
- `NodeKindObserver` transitions consume from `SpaceObservation` input places and produce into `SpaceObservation` or `SpaceComputation` output places.
- A Surface token must pass through at least one Observation transition before reaching Computation (the "parsed command" rule from the paper).
- Observer transitions in the parent are how the parent knows what sub-CPNs are doing — without breaking their isolation.

**Observer node spec:**
```
GIVEN a NodeKindObserver transition T
  AND a sub-CPN S has emitted Event E
  AND T.EventFilter(E) == true
WHEN the executor polls the EventBus
THEN T fires:
  - consumes no tokens from InputPlaces (it has none; it is triggered by events)
  - deposits Token{Color: ColorEvent, Payload: E, Space: SpaceObservation}
    into each of T.OutputPlaces
```

### 4.3 Computation Space

**Purpose:** Core decision-making and work execution.

**Rules:**
- All `NodeKindTool`, `NodeKindSubNet`, and `NodeKindLLM` transitions operate primarily on Computation-space places.
- Terminal output places are always `SpaceComputation` (they hold the final `ColorArtifact` tokens).
- In `ModeCentaurian`, computation transitions get an auto-injected guard:
  ```go
  func centaurianGuard(tokens []Token) bool {
      hasHuman := false
      hasAI    := false
      for _, t := range tokens {
          if t.IsHumanOrigin() { hasHuman = true }
          if !t.IsHumanOrigin() { hasAI = true }
      }
      return hasHuman && hasAI
  }
  ```
  This enforces the paper's core Centaurian property: neither human nor AI can trigger computation alone.

---

## 5. SubNet Protocol

### 5.1 Isolation Contract

```
GIVEN a NodeKindSubNet transition T in parent CPN P
  AND T fires consuming token(s) from P.InputPlaces
THEN:
  1. A child CPN C is created via T.SubNetFactory() or T.SubNet (cloned)
  2. C.ID is a new UUID — C is a distinct CPN, not a reference to a prototype
  3. C.Depth = P.Depth + 1
  4. C.SessionID = P.SessionID
  5. C's initial marking is set from the consumed tokens:
       C.Places["P:INPUT"].Tokens = consumedTokens (re-typed to C's place color)
  6. C.EventEmitter is set to the shared eventBus channel
  7. P.Group.Register(C) is called
  8. C.Run(ctx) is launched in a new goroutine
  9. The parent does NOT block. It continues its executor loop.
 10. When C reaches StateCompleted:
       - C.TerminalPlaces() tokens are copied (with OriginID=C.ID, OriginDepth=C.Depth)
       - Those tokens are deposited into T.OutputPlaces in the parent
       - P.Group.Deregister(C.ID)
       - C emits EventSubNetCompleted
 11. If C reaches StateFailed:
       - P transitions to StateFailed with ErrSubNetFailed
       - C emits EventSubNetFailed
```

### 5.2 Event Emission

Sub-CPNs emit events at every meaningful state change.
This is the hybrid scoping contract: isolation in place namespace,
transparency in behavior.

```go
// emit is called by the executor on every significant transition.
// It is a non-blocking send — the parent's observer picks it up asynchronously.
func (c *CPN) emit(e Event) {
    if c.EventEmitter == nil {
        return
    }
    e.CPNID    = c.ID
    e.CPNDepth = c.Depth
    e.CPNRole  = c.Role
    select {
    case c.EventEmitter <- e:
    default:
        // bus full — event dropped. Observer transitions are best-effort.
        // Critical state (completion/failure) is communicated via token deposit,
        // not via events, so dropping is safe.
    }
}
```

Events emitted by every sub-CPN:
- `EventSubNetStarted` — when `Run()` begins
- `EventTransitionFired` — on every transition fire (includes token snapshot)
- `EventTokenDeposited` — when a terminal place receives a token
- `EventSubNetCompleted` / `EventSubNetFailed`
- `EventHITLRequested` / `EventHITLResolved` — for HITL transitions

### 5.3 Observer Transitions in Practice

Observer transitions have **no InputPlaces**. They are triggered by the EventBus,
not by token availability. The executor treats them separately:

```
Main loop iteration:
  1. Check all Transitions with len(InputPlaces) > 0 for CanFire() — fire in goroutines
  2. Check all NodeKindObserver Transitions — drain EventBus, fire for matching events
  3. Check completion / deadlock
  4. Repeat
```

This gives the parent CPN real-time visibility into sub-CPN behavior
without coupling place namespaces.

---

## 6. Group-Agent Protocol

The full implementation of the paper's §4.2 group-agent behavior.
The `GroupAgent` in `CPN.Group` manages all sub-CPNs spawned by that CPN.

### Lifecycle

```
SubNetFactory() called
    → child CPN created
    → P.Group.Register(child)          ← paper: register()
    → child.Run() launched
    → child emits events via EventEmitter
    → parent observer transitions materialize events as tokens
    → child reaches StateCompleted
    → P.Group.Deregister(child.ID)     ← paper: deregister()
    → output tokens deposited in parent

If child pauses (StateWaiting / HITL):
    → P.Group.SwitchCMP(child.ID)      ← paper: switchCMP() → non-active
    → HITL resolved
    → child returns to StateRunning
    → P.Group.SwitchCMP(child.ID)      ← paper: switchCMP() → active

Deliver is used for broadcast from parent to all active sub-CPNs:
    → P.Group.Deliver(event)           ← paper: deliver()
    → all active sub-CPNs receive via their EventBus
```

### Usage: lateral communication between domain CPNs

When two domain CPNs (depth=1) need to coordinate (the "lateral channel"
from the previous spec), they do so via the parent CPN's GroupAgent:

```
Parent CPN (depth=0) has Group managing:
    C1: CPN{Role: "engineering", Depth: 1}
    C2: CPN{Role: "data", Depth: 1}

C1 emits Event{Type: EventStreamChunk, Payload: {"needs": "schema-info"}}
Parent's NodeKindObserver materializes it as a ColorEvent token
Observer's OutputPlace feeds into a NodeKindSubNet that spawns C2
  (or if C2 is already active, the parent's Group.Deliver routes to it)
```

The parent CPN (depth=0) always observes lateral communication.
It does not intervene — it only logs and routes. This matches the paper's
"Orchestrator observes EventLateralComm but does not decide" contract.

---

## 7. HITL Protocol

### Full Spec

```
GIVEN a NodeKindHITL transition T
  AND T.HITLChannel is initialized (buffered, cap=1)
  AND all T.InputPlaces have tokens (CanFire returns true)

WHEN the executor fires T:
  1. Consume input tokens from T.InputPlaces (atomic, before blocking)
  2. Emit Event{Type: EventHITLRequested, Payload: {"prompt": T.HITLPrompt}}
     via C.emit() so the parent's observer can surface this to the user
  3. Set C.State = StateWaiting
  4. C.Group.SwitchCMP(C.ID) — moves this CPN to non-active in parent's group
  5. Block on: humanToken := <-T.HITLChannel
  6. Validate humanToken.Color == ColorHuman
  7. Set C.State = StateRunning
  8. C.Group.SwitchCMP(C.ID) — moves back to active
  9. Deposit humanToken into each place in T.OutputPlaces
 10. Emit Event{Type: EventHITLResolved}
 11. If C.Mode == ModeMAS and at least one OutputPlace is SpaceComputation:
       Switch C.Mode = ModeCentaurian
       Emit Event{Type: EventModeSwitch, Payload: {"from": "mas", "to": "centaurian"}}
```

### Session Layer Responsibility

The Session layer is the ONLY code that writes to `HITLChannel`.
It receives the human message, wraps it as a `ColorHuman` token,
and sends it to the correct transition by ID:

```go
func (s *Session) ResolveHITL(transitionID string, humanInput string) error {
    ch, ok := s.HITLInject[transitionID]
    if !ok {
        return ErrNoHITLWaiting
    }
    ch <- Token{
        Color:       ColorHuman,
        Payload:     humanInput,
        OriginID:    "human",
        OriginDepth: -1, // sentinel: human is not a CPN
        OriginKind:  NodeKindHITL,
        Space:       SpaceSurface,
        SessionID:   s.ID,
        Timestamp:   time.Now(),
    }
    return nil
}
```

### Mode switch implication

When a HITL token enters a Computation-space place, the CPN automatically
switches to ModeCentaurian. This means all subsequent computation-space
transitions require co-trigger (human + AI tokens). This is the formal
implementation of the paper's §4.3 Centaurian architecture:

> "A shared observation layer acts as a bridge between human and AI representations,
> facilitating smooth cognitive fusion."

---

## 8. MAS / Centaurian Mode Switching

### Rules

| Condition | Mode transition |
|---|---|
| CPN created | `ModeMAS` |
| First `ColorHuman` token deposited in any `SpaceComputation` place | `ModeMAS → ModeCentaurian` |
| All `ColorHuman` tokens consumed and no HITL transitions pending | `ModeCentaurian → ModeMAS` |
| Explicit call to `CPN.SetMode(mode)` by parent | Override |

### Centaurian guard injection

The executor checks mode on every firing decision.
In ModeCentaurian, if a Computation-space transition has no Guard set,
the centaurianGuard function is injected automatically:

```go
func (exec *Executor) effectiveGuard(t *Transition, places map[string]*Place) func([]Token) bool {
    if t.Guard != nil {
        return t.Guard
    }
    // Only inject for computation-space transitions in Centaurian mode
    if exec.cPN.Mode == ModeCentaurian && exec.isComputationTransition(t, places) {
        return centaurianGuard
    }
    return nil // no guard — fire freely
}
```

---

## 9. Executor

### 9.1 Main Loop

```
GIVEN a CPN C with valid initial marking and validated arcs
WHEN C.Run(ctx) is called
THEN the executor:

  loop:
    1. Collect firable transitions:
         regular  = [T for T in Transitions where T.Kind != NodeKindObserver AND T.CanFire(places)]
         observer = [T for T in Transitions where T.Kind == NodeKindObserver]

    2. For each T in regular: launch goroutine → T.fire(ctx, C)

    3. For each T in observer: drain C.EventBus → if EventFilter matches → T.fire(ctx, C)

    4. wg.Wait() — all goroutines from step 2 complete

    5. if C.IsComplete() → C.State = StateCompleted → return nil

    6. if len(regular) == 0 AND no events in EventBus:
         if C.hasWaitingHITL() → C.State = StateWaiting → block until HITL resolves
         else → C.State = StateFailed → return ErrDeadlock

    7. if ctx.Done() → C.State = StateFailed → return ErrTimeout

    8. goto loop
```

### 9.2 Parallelism (unchanged from v0.1, now extended)

```
GIVEN transitions A and B where CanFire() == true simultaneously
WHEN the executor runs step 2
THEN A and B execute in separate goroutines concurrently
```

This applies to all NodeKinds including SubNet — two sub-CPNs may run
fully concurrently, each in their own goroutine, without synchronization
beyond their output token deposits.

### 9.3 Thread Safety

All token operations go through `Place.Deposit` and `Place.Consume`,
which hold `sync.Mutex`. Tokens are consumed **before** the Executor
function is called (atomic consumption rule from v0.1 §6.4, unchanged).

`CPN.State` is protected by `CPN.mu sync.RWMutex`.
`GroupAgent` lists are protected by `GroupAgent.mu sync.RWMutex`.

### 9.4 SubNet Firing

```go
func fireSubNet(ctx context.Context, t *Transition, parent *CPN) error {
    // 1. Consume input tokens atomically
    consumed := consumeAll(t.InputPlaces, parent.Places)

    // 2. Create child CPN
    var child *CPN
    if t.SubNetFactory != nil {
        child = t.SubNetFactory()
    } else {
        child = cloneCPN(t.SubNet)
    }
    child.ID        = newUUID()
    child.Depth     = parent.Depth + 1
    child.SessionID = parent.SessionID

    // 3. Set up event bus
    bus := make(chan Event, 64)
    child.EventEmitter = bus
    // Parent's EventBus is separate; we wire this bus to the parent's observer loop
    parent.registerSubNetBus(child.ID, bus)

    // 4. Inject input tokens as initial marking
    injectTokens(child, consumed)

    // 5. Register in group
    parent.Group.Register(child)

    // 6. Run child concurrently
    go func() {
        err := child.Run(ctx)
        if err != nil {
            parent.emit(Event{Type: EventSubNetFailed, CPNID: child.ID})
            parent.setFailed(ErrSubNetFailed)
            return
        }
        // 7. Deposit output tokens into parent
        for _, terminal := range child.TerminalPlaces() {
            for _, tok := range terminal.Tokens {
                enriched := tok
                enriched.OriginID    = child.ID
                enriched.OriginDepth = child.Depth
                for _, pid := range t.OutputPlaces {
                    parent.Places[pid].Deposit(enriched)
                }
            }
        }
        parent.Group.Deregister(child.ID)
        parent.emit(Event{Type: EventSubNetCompleted, CPNID: child.ID})
    }()

    return nil
}
```

### 9.5 Observer Firing

Observer transitions have no InputPlaces. They are triggered by the EventBus.

```go
func drainObservers(ctx context.Context, parent *CPN) {
    for _, t := range parent.Transitions {
        if t.Kind != NodeKindObserver {
            continue
        }
        // Non-blocking drain of all buses registered for this CPN's sub-CPNs
        for _, bus := range parent.subNetBuses {
            select {
            case e := <-bus:
                if t.EventFilter == nil || t.EventFilter(e) {
                    tok := Token{
                        Color:     ColorEvent,
                        Payload:   e,
                        OriginID:  e.CPNID,
                        Space:     SpaceObservation,
                        SessionID: parent.SessionID,
                        Timestamp: time.Now(),
                    }
                    for _, pid := range t.OutputPlaces {
                        parent.Places[pid].Deposit(tok)
                    }
                }
            default:
                // nothing to drain
            }
        }
    }
}
```

### 9.6 HITL Firing

The HITL branch blocks, but the executor runs it in a goroutine like any other
transition, so other branches continue running in parallel.

```go
func fireHITL(ctx context.Context, t *Transition, c *CPN) error {
    _ = consumeAll(t.InputPlaces, c.Places) // consume atomically
    c.emit(Event{Type: EventHITLRequested, Payload: map[string]any{"prompt": t.HITLPrompt}})
    c.setState(StateWaiting)
    c.Group.SwitchCMP(c.ID)

    select {
    case tok := <-t.HITLChannel:
        if tok.Color != ColorHuman {
            return ErrColorMismatch
        }
        c.setState(StateRunning)
        c.Group.SwitchCMP(c.ID)
        c.emit(Event{Type: EventHITLResolved})
        for _, pid := range t.OutputPlaces {
            c.Places[pid].Deposit(tok)
        }
        // Check for automatic Centaurian mode switch
        for _, pid := range t.OutputPlaces {
            if c.Places[pid].Space == SpaceComputation {
                c.switchModeToCentaurian()
                break
            }
        }
        return nil
    case <-ctx.Done():
        c.setState(StateFailed)
        return ErrTimeout
    }
}
```

### 9.7 LLM Firing

```go
func fireLLM(ctx context.Context, t *Transition, c *CPN) error {
    consumed := consumeAll(t.InputPlaces, c.Places)
    // Build context from token payload + session history
    messages := buildLLMContext(consumed, c.History, t.SystemPrompt)
    // Available tools: transitions in LLMTools list
    tools := resolveLLMTools(t.LLMTools, c.Transitions)

    resp, err := t.LLMClient.Complete(ctx, messages, tools)
    if err != nil {
        return err
    }
    // LLM may invoke tools — each tool call fires its CPN transition
    // and the result re-enters the LLM context (agentic loop)
    result, err := handleLLMResponse(ctx, resp, tools, c)
    if err != nil {
        return err
    }
    outTok := Token{
        Color:     inferOutputColor(result),
        Payload:   result,
        OriginID:  c.ID,
        OriginDepth: c.Depth,
        OriginKind: NodeKindLLM,
        Space:     SpaceComputation,
        SessionID: c.SessionID,
        Timestamp: time.Now(),
    }
    for _, pid := range t.OutputPlaces {
        c.Places[pid].Deposit(outTok)
    }
    return nil
}
```

### 9.8 Timeout

```
GIVEN a CPN in execution
WHEN ctx.Done() fires (timeout or explicit cancellation)
THEN:
  - C.State = StateFailed
  - C.Error = ErrTimeout
  - All running sub-CPN goroutines receive ctx.Done() and terminate
  - GroupAgent is cleared
  - Return ErrTimeout
```

---

## 10. Topology-Based Hierarchy

There are **no** Orchestrator, Director, or Agent types.
The hierarchy is entirely determined by `CPN.Depth` and `CPN.Role`.

### 10.1 Depth Semantics

| Depth | Former name | Responsibility | Typical Transitions |
|---|---|---|---|
| 0 | Orchestrator | Intent classification, session routing, streaming to user | NodeKindLLM (classifier), NodeKindSubNet (domain CPNs), NodeKindHITL (structural changes), NodeKindObserver |
| 1 | Director | Domain coordination, ephemeral team assembly | NodeKindSubNet (worker CPNs), NodeKindObserver, NodeKindLLM |
| 2+ | Agent / Worker | Specialized task execution | NodeKindTool, NodeKindLLM, NodeKindSubNet (for deeply nested tasks) |

### 10.2 Role Labels

`Role` is a free string. Convention:

| Role | Depth | Description |
|---|---|---|
| `"root"` | 0 | Session root CPN |
| `"engineering"` | 1 | Software engineering domain |
| `"data"` | 1 | Data analysis domain |
| `"marketing"` | 1 | Marketing domain |
| `"repo-analyzer"` | 2 | Analyzes a codebase |
| `"spec-builder"` | 2 | Builds a specification document |
| `"requirement-reader"` | 2 | Parses a requirements document |
| `"impl-planner"` | 2 | Produces an implementation plan |

### 10.3 Depth-Aware Logging and Routing

The Event Store records `CPNDepth` on every event.
The `buildContext` function for LLM transitions uses depth to filter history:

```go
func buildContext(c *CPN) []Message {
    var out []Message
    for _, m := range c.History {
        // Include all observer (thinking-block) messages regardless of depth
        if m.Role == RoleObserver {
            out = append(out, m)
            continue
        }
        // Include raw messages only from this CPN and parent
        if m.CPNDepth <= c.Depth {
            out = append(out, m)
        }
    }
    return last(out, N) // last N messages
}
```

---

## 11. Spec: Three Operation Types

The root CPN (depth=0) implements intent classification as its first transition.

```
Spec OP.1 — Simple operation
  GIVEN user input token in P:INPUT (SpaceSurface)
  WHEN NodeKindLLM classifier fires
  THEN IntentResult.Type == "simple"
  AND a NodeKindLLM transition resolves the request directly
  AND output streams to P:SURFACE_OUT
  AND no sub-CPNs are spawned

Spec OP.2 — Business operation
  GIVEN IntentResult.Type == "business_operation"
  WHEN the root CPN fires a NodeKindSubNet transition
  THEN a domain CPN (depth=1) is spawned with appropriate Role
  AND domain CPN may spawn worker CPNs (depth=2)
  AND HITL is optional per configured threshold

Spec OP.3 — Structural change
  GIVEN IntentResult.Type == "structural_change"
  WHEN the root CPN reaches the structural-change branch
  THEN a NodeKindHITL transition MUST fire before any computation continues
  AND CPN.Mode switches to ModeCentaurian after HITL resolves
  AND no structural output is deposited without the co-trigger guard passing
```

---

## 12. Error Types

Extended from v0.1.

```go
var (
    // From v0.1 (unchanged)
    ErrColorMismatch = errors.New("token color does not match place color set")
    ErrEmptyPlace    = errors.New("no tokens available in place")
    ErrInvalidArc    = errors.New("transition references non-existent place")
    ErrDeadlock      = errors.New("no transitions can fire but CPN is not complete")
    ErrTimeout       = errors.New("CPN execution exceeded timeout")

    // New in v1.0
    ErrSpaceMismatch   = errors.New("token space does not match place space kind")
    ErrSpaceViolation  = errors.New("token cannot bypass observation space")
    ErrSubNetFailed    = errors.New("sub-CPN reached failed state")
    ErrNoHITLWaiting   = errors.New("no HITL transition is currently waiting")
    ErrInvalidNodeKind = errors.New("transition kind is not recognized")
    ErrCentaurianGuard = errors.New("centaurian guard failed: missing human or AI token")
)
```

---

## 13. Flow Intelligence Engine

Unchanged in purpose. Extended to operate on CPN-level topology metrics.

### Phase 1 (MVP)
- Record every CPN execution: depth, role, transitions fired, tokens produced, duration, success/failure.
- Ranking: fewer transitions + same output quality = higher score.

### Phase 2 — Pattern Mining
- Detect repeated sub-CPN topologies across sessions (not just tool sequences).
- A topology is a candidate for crystallization when it appears ≥ N times with success rate ≥ threshold.
- HITL gate required before any topology is promoted to the `FlowLibrary`.

### Phase 3 — Flow Crystallization
- Crystallized flows are stored as `*CPN` in `FlowLibrary` (keyed by topology hash).
- `NodeKindSubNet` transitions can reference `FlowLibrary` entries:
  ```go
  t.SubNet = flowLibrary.Get("repo-analysis-v3")
  ```
- Cross-client replication: a crystallized topology is proposed to clients with similar domains.

### FlowLibrary

```go
type FlowLibrary struct {
    flows map[string]*CPN  // topology hash → CPN
    mu    sync.RWMutex
}

func (fl *FlowLibrary) Register(c *CPN, hash string) { ... }
func (fl *FlowLibrary) Get(hash string) *CPN         { ... }
func (fl *FlowLibrary) Propose(clientID string) []*CPN { ... }
```

---

## 14. Linux Sandbox

Unchanged from previous spec.

- Dedicated system user (non-sudoer) per session or per sub-CPN (configurable).
- NodeKindTool executors that need filesystem access run commands under this user.
- Available: `ls`, `cd`, `cat`, `grep`, `find`, `echo`, `pwd`.
- The sandbox is the security boundary — a compromised tool cannot escalate.

```go
type SandboxedTool struct {
    User    string // OS user name
    WorkDir string // working directory
}

func (s *SandboxedTool) Exec(ctx context.Context, in Token) (Token, error) {
    cmd := exec.CommandContext(ctx, "su", "-s", "/bin/sh", s.User, "-c", in.Payload.(string))
    cmd.Dir = s.WorkDir
    out, err := cmd.Output()
    // ...
}
```

---

## 15. Reference Networks

### 15.1 Financial Analysis (updated from v0.1)

The original reference network is now expressed with the new types.
Key changes: Space fields on Places, Token origin, Observer transition added.

```
Surface: P:INPUT (STRING)
    ↓
Observation: [llm:planner] → P:TICKER (JSON) + P:QUERY (JSON)
    ↓                        ↓
Computation: [tool:get_fin]  [tool:web_search]
    ↓                            ↓         ↓
P:FIN_DATA (JSON)      P:WEB_SENT (JSON)  P:WEB_SUMM (JSON)
    ↓                        ↓                 ↓
[tool:chart_gen]      [tool:sentiment]   [tool:summarizer]
    ↓                        ↓                 ↓
P:CHART (ARTIFACT)  P:SENTIMENT (SCORE)  P:SUMMARY (ARTIFACT)
    └──────────────────────────┴──────────────┘
                               ↓
                         [tool:report]
                               ↓
                         P:OUTPUT (ARTIFACT)
```

```go
net := &CPN{
    ID: newUUID(), Role: "financial-analysis", Depth: 2,
    Mode: ModeMAS,
    Places: map[string]*Place{
        "P:INPUT":     {ID: "P:INPUT",     Color: ColorString,   Space: SpaceSurface},
        "P:TICKER":    {ID: "P:TICKER",    Color: ColorJSON,     Space: SpaceObservation},
        "P:QUERY":     {ID: "P:QUERY",     Color: ColorJSON,     Space: SpaceObservation},
        "P:FIN_DATA":  {ID: "P:FIN_DATA",  Color: ColorJSON,     Space: SpaceComputation},
        "P:WEB_SENT":  {ID: "P:WEB_SENT",  Color: ColorJSON,     Space: SpaceComputation},
        "P:WEB_SUMM":  {ID: "P:WEB_SUMM",  Color: ColorJSON,     Space: SpaceComputation},
        "P:CHART":     {ID: "P:CHART",     Color: ColorArtifact, Space: SpaceComputation},
        "P:SENTIMENT": {ID: "P:SENTIMENT", Color: ColorScore,    Space: SpaceComputation},
        "P:SUMMARY":   {ID: "P:SUMMARY",   Color: ColorArtifact, Space: SpaceComputation},
        "P:OUTPUT":    {ID: "P:OUTPUT",    Color: ColorArtifact, Space: SpaceComputation},
    },
    Transitions: map[string]*Transition{
        "planner": {
            ID: "planner", Kind: NodeKindLLM,
            InputPlaces:  []string{"P:INPUT"},
            OutputPlaces: []string{"P:TICKER", "P:QUERY"},
            SystemPrompt: "Extract ticker symbol and analysis query from input.",
        },
        "get_fin":    {Kind: NodeKindTool, InputPlaces: []string{"P:TICKER"},   OutputPlaces: []string{"P:FIN_DATA"},  Executor: getFinTool},
        "web_search": {Kind: NodeKindTool, InputPlaces: []string{"P:QUERY"},    OutputPlaces: []string{"P:WEB_SENT","P:WEB_SUMM"}, Executor: webSearchTool},
        "chart_gen":  {Kind: NodeKindTool, InputPlaces: []string{"P:FIN_DATA"}, OutputPlaces: []string{"P:CHART"},     Executor: chartGenTool},
        "sentiment":  {Kind: NodeKindTool, InputPlaces: []string{"P:WEB_SENT"}, OutputPlaces: []string{"P:SENTIMENT"}, Executor: sentimentTool},
        "summarizer": {Kind: NodeKindTool, InputPlaces: []string{"P:WEB_SUMM"}, OutputPlaces: []string{"P:SUMMARY"},   Executor: summarizerTool},
        "report": {
            Kind: NodeKindTool,
            InputPlaces:  []string{"P:CHART", "P:SENTIMENT", "P:SUMMARY"},
            OutputPlaces: []string{"P:OUTPUT"},
            Executor:     reportTool,
        },
    },
}
```

---

### 15.2 Spec Builder with Nested Repo Analyzer

This is the user's described scenario: a spec-building CPN (depth=1) has a branch
that IS a repo-analysis CPN (depth=2 SubNet) running in parallel with a
requirement-reader branch, both converging at an implementation planner.

The parent observes the repo-analysis sub-CPN via an Observer node —
the parent can surface progress updates without breaking isolation.

```
Parent CPN (depth=1, Role: "spec-builder")

P:TASK (JSON, Surface)
    ↓
[llm:task-splitter] (Observation)
    ├──► P:REPO_URL (JSON, Observation)
    └──► P:REQ_DOC  (JSON, Observation)
         │                │
         ▼                ▼
[subnet:repo-analyzer]  [tool:read-requirement]    ← parallel
         │                │
         ▼                ▼
P:CODE_CONTEXT          P:REQ_CONTEXT              ← both Computation
(ARTIFACT)              (JSON)
         │                │
         └────────┬────────┘
                  ▼
          [llm:impl-planner]                        ← Computation
                  ↓
          P:SPEC (ARTIFACT, Computation)            ← terminal place

Observer transition (no InputPlaces, SpaceObservation):
[observer:repo-progress]
    listens for EventTransitionFired from repo-analyzer sub-CPN
    deposits Token{ColorEvent} → P:PROGRESS_LOG (EVENT, Observation)
    → can be streamed to user as live progress
```

```go
// The repo-analyzer sub-CPN (depth=2) — full CPN, returned by factory
func repoAnalyzerFactory() *CPN {
    return &CPN{
        Role: "repo-analyzer",
        Places: map[string]*Place{
            "P:REPO_URL":      {Color: ColorJSON,     Space: SpaceSurface},
            "P:FILE_LIST":     {Color: ColorJSON,     Space: SpaceObservation},
            "P:KEY_FILES":     {Color: ColorJSON,     Space: SpaceComputation},
            "P:GO_SUMMARY":    {Color: ColorArtifact, Space: SpaceComputation},
            "P:DEP_GRAPH":     {Color: ColorJSON,     Space: SpaceComputation},
            "P:CODE_CONTEXT":  {Color: ColorArtifact, Space: SpaceComputation},
        },
        Transitions: map[string]*Transition{
            "list_files":   {Kind: NodeKindTool, InputPlaces: []string{"P:REPO_URL"},   OutputPlaces: []string{"P:FILE_LIST"},                 Executor: listFilesTool},
            "filter_files": {Kind: NodeKindLLM,  InputPlaces: []string{"P:FILE_LIST"},  OutputPlaces: []string{"P:KEY_FILES"},                 SystemPrompt: "Select the most relevant files for understanding this codebase."},
            "read_go":      {Kind: NodeKindTool, InputPlaces: []string{"P:KEY_FILES"},  OutputPlaces: []string{"P:GO_SUMMARY", "P:DEP_GRAPH"}, Executor: readGoFilesTool},
            "dep_analysis": {Kind: NodeKindTool, InputPlaces: []string{"P:DEP_GRAPH"},  OutputPlaces: []string{"P:CODE_CONTEXT"},              Executor: depAnalysisTool},
            "summarize":    {Kind: NodeKindLLM,  InputPlaces: []string{"P:GO_SUMMARY"}, OutputPlaces: []string{"P:CODE_CONTEXT"},              SystemPrompt: "Summarize the codebase architecture concisely."},
        },
    }
}

// The spec-builder parent CPN (depth=1)
specBuilder := &CPN{
    Role: "spec-builder", Depth: 1, Mode: ModeMAS,
    Group: &GroupAgent{Topic: "spec-building"},
    Places: map[string]*Place{
        "P:TASK":          {Color: ColorJSON,     Space: SpaceSurface},
        "P:REPO_URL":      {Color: ColorJSON,     Space: SpaceObservation},
        "P:REQ_DOC":       {Color: ColorJSON,     Space: SpaceObservation},
        "P:CODE_CONTEXT":  {Color: ColorArtifact, Space: SpaceComputation},
        "P:REQ_CONTEXT":   {Color: ColorJSON,     Space: SpaceComputation},
        "P:PROGRESS_LOG":  {Color: ColorEvent,    Space: SpaceObservation},
        "P:SPEC":          {Color: ColorArtifact, Space: SpaceComputation},
    },
    Transitions: map[string]*Transition{
        "task-splitter": {
            Kind: NodeKindLLM,
            InputPlaces:  []string{"P:TASK"},
            OutputPlaces: []string{"P:REPO_URL", "P:REQ_DOC"},
            SystemPrompt: "Split the task into a repo URL and a requirements document reference.",
        },
        // The repo analyzer IS a full CPN — spawned as a sub-CPN
        "repo-analyzer": {
            Kind:           NodeKindSubNet,
            InputPlaces:    []string{"P:REPO_URL"},
            OutputPlaces:   []string{"P:CODE_CONTEXT"},
            SubNetFactory:  repoAnalyzerFactory,
        },
        // Runs in parallel with repo-analyzer
        "read-requirement": {
            Kind:         NodeKindTool,
            InputPlaces:  []string{"P:REQ_DOC"},
            OutputPlaces: []string{"P:REQ_CONTEXT"},
            Executor:     readRequirementTool,
        },
        // Observer: materializes repo-analyzer progress as stream tokens
        "repo-progress": {
            Kind:        NodeKindObserver,
            InputPlaces: []string{}, // no input places — driven by EventBus
            OutputPlaces: []string{"P:PROGRESS_LOG"},
            EventFilter: func(e Event) bool {
                return e.CPNRole == "repo-analyzer" && e.Type == EventTransitionFired
            },
        },
        // Fires only when BOTH P:CODE_CONTEXT and P:REQ_CONTEXT have tokens
        "impl-planner": {
            Kind:         NodeKindLLM,
            InputPlaces:  []string{"P:CODE_CONTEXT", "P:REQ_CONTEXT"},
            OutputPlaces: []string{"P:SPEC"},
            SystemPrompt: "You have a codebase analysis and a requirements document. Produce a detailed implementation specification.",
        },
    },
}
```

---

### 15.3 Centaurian Approval Pattern

A structural-change request. The HITL gate blocks the spec from executing
until a human approves. After HITL resolves, the CPN enters ModeCentaurian:
the final output transition requires both the human approval token AND the
AI-generated plan token.

```
P:CHANGE_REQUEST (JSON, Surface)
    ↓
[llm:impact-analyzer] (Observation)
    ├──► P:RISK_ASSESSMENT (JSON, Observation)
    └──► P:PROPOSED_PLAN  (JSON, Observation)
              │                    │
              ▼                    ▼
[hitl:approve-change]         [llm:refine-plan]   ← parallel
(blocks branch)                (computation)
              │                    │
              ▼                    ▼
P:HUMAN_APPROVAL (HUMAN, Surface) P:REFINED_PLAN (ARTIFACT, Computation)
              │                    │
              └──────────┬─────────┘
                         ▼
              [llm:execute-change]                ← ModeCentaurian auto-guard:
                         ↓                          requires ColorHuman AND ColorArtifact
              P:CHANGE_RESULT (ARTIFACT, Computation)
```

```go
approvalCPN := &CPN{
    Role: "structural-change", Depth: 1, Mode: ModeMAS,
    Places: map[string]*Place{
        "P:CHANGE_REQUEST":  {Color: ColorJSON,     Space: SpaceSurface},
        "P:RISK_ASSESSMENT": {Color: ColorJSON,     Space: SpaceObservation},
        "P:PROPOSED_PLAN":   {Color: ColorJSON,     Space: SpaceObservation},
        "P:HUMAN_APPROVAL":  {Color: ColorHuman,    Space: SpaceSurface},
        "P:REFINED_PLAN":    {Color: ColorArtifact, Space: SpaceComputation},
        "P:CHANGE_RESULT":   {Color: ColorArtifact, Space: SpaceComputation},
    },
    Transitions: map[string]*Transition{
        "impact-analyzer": {
            Kind: NodeKindLLM,
            InputPlaces:  []string{"P:CHANGE_REQUEST"},
            OutputPlaces: []string{"P:RISK_ASSESSMENT", "P:PROPOSED_PLAN"},
            SystemPrompt: "Analyze the structural change request. Output risk assessment and proposed plan.",
        },
        // HITL gate — blocks until human approves
        "approve-change": {
            Kind:         NodeKindHITL,
            InputPlaces:  []string{"P:RISK_ASSESSMENT"},
            OutputPlaces: []string{"P:HUMAN_APPROVAL"},
            HITLChannel:  make(chan Token, 1),
            HITLPrompt:   "Risk assessment complete. Please review and approve or reject the proposed structural change.",
        },
        // Runs in parallel with HITL
        "refine-plan": {
            Kind:         NodeKindLLM,
            InputPlaces:  []string{"P:PROPOSED_PLAN"},
            OutputPlaces: []string{"P:REFINED_PLAN"},
            SystemPrompt: "Refine the proposed plan into a detailed implementation spec.",
        },
        // After HITL resolves, CPN switches to ModeCentaurian automatically.
        // This transition gets centaurianGuard injected: requires both
        // P:HUMAN_APPROVAL (ColorHuman) and P:REFINED_PLAN (ColorArtifact).
        "execute-change": {
            Kind:         NodeKindLLM,
            InputPlaces:  []string{"P:HUMAN_APPROVAL", "P:REFINED_PLAN"},
            OutputPlaces: []string{"P:CHANGE_RESULT"},
            SystemPrompt: "Human has approved. Execute the structural change plan.",
            // No Guard set — centaurianGuard is injected automatically
        },
    },
}
```

---

## 16. Package Structure

```
cpn/
├── colors.go         // ColorSet constants
├── space.go          // SpaceKind constants
├── kinds.go          // NodeKind constants
├── mode.go           // CPNMode, CPNState constants
├── token.go          // Token struct, IsHumanOrigin()
├── place.go          // Place, Deposit, Consume, Peek
├── transition.go     // Transition, CanFire
├── group_agent.go    // GroupAgent, Register, Deliver, Deregister, SwitchCMP
├── cpn.go            // CPN struct, TerminalPlaces, IsComplete, emit
├── executor.go       // Run loop, fireSubNet, fireHITL, fireLLM, drainObservers
├── validator.go      // Validate (arc refs, space constraints, kind fields)
├── errors.go         // all typed errors
├── session.go        // Session, StreamChunk, Message, ResolveHITL
├── flow_library.go   // FlowLibrary, Register, Get, Propose
├── sandbox.go        // SandboxedTool
└── cpn_test.go       // one test function per spec statement
```

---

## 17. Implementation Order

```
Step 1 — Base types (no logic)
         colors.go · space.go · kinds.go · mode.go · errors.go
         Token · Place (struct only) · Transition (struct only) · CPN (struct only)
         Goal: go build with zero errors.

Step 2 — Place operations
         Deposit, Consume, Peek with sync.Mutex
         Tests: Spec 5.1–5.4 from v0.1 + new Spec SpaceMismatch

Step 3 — Transition CanFire
         CanFire with Guard support
         Tests: Spec 6.1–6.3 from v0.1

Step 4 — Validator
         Validate: arc references, SpaceViolation rule, HITL channel init
         Tests: Spec 7.2 + new space violation cases

Step 5 — Executor (MAS mode only, NodeKindTool only)
         Run loop, goroutines, WaitGroup, deadlock detection, timeout
         Tests: Spec 8.1–8.4 from v0.1 (all passing)
         Reference net 15.1 passing end-to-end.

Step 6 — GroupAgent
         Register, Deliver, Deregister, SwitchCMP
         Tests: one per method, paper pseudocode verified

Step 7 — NodeKindSubNet
         fireSubNet, event bus wiring, output token deposit
         Tests: nested CPN completes and deposits tokens in parent.
         Reference net 15.2 (spec builder) passing.

Step 8 — NodeKindObserver
         drainObservers in executor loop
         Tests: observer transition fires on matching event.

Step 9 — NodeKindHITL
         fireHITL, StateWaiting, channel inject
         Session.ResolveHITL
         Tests: CPN waits, inject human token, resumes.

Step 10 — ModeCentaurian
          centaurianGuard injection, mode switch on HITL resolve
          Tests: reference net 15.3 (approval pattern) passing.

Step 11 — NodeKindLLM
          fireLLM, buildLLMContext, tool invocation loop
          Tests: LLM transition produces correct output color.

Step 12 — Session and Channel layer
          Session struct, StreamChunk routing, multi-channel adapters.

Step 13 — Flow Intelligence Engine Phase 1
          Metrics recording, ranking by transition count.

Step 14 — FlowLibrary
          Register crystallized CPNs, SubNetFactory integration.
```

---

## 18. Design Decisions Log

| Decision | Value | Reasoning |
|---|---|---|
| Single CPN type for all agents | `CPN` with `Depth` + `Role` | Topology defines hierarchy — no parallel type hierarchies to maintain |
| SubNet isolation | Own place namespace | Prevents accidental coupling; forces explicit token contracts at boundaries |
| Sub-CPN observability | Event emission + Observer nodes | Parent needs visibility without breaking isolation — matches paper's Observation Space |
| HITL as NodeKind | `NodeKindHITL` transition | HITL is a first-class computational step, not an external interrupt; integrates naturally into firing semantics |
| Mode switch on HITL | Automatic on first ColorHuman in Computation space | Centaurian mode is earned by human participation, not configured — matches paper §2.2 |
| Centaurian guard injection | Automatic for Computation transitions without explicit Guard | Eliminates boilerplate; makes Centaurian the default protection, not opt-in |
| Observer has no InputPlaces | Driven by EventBus, not token availability | Observation is continuous, not discrete — matches paper's "continuous feedback loops" |
| Token carries OriginDepth | Always | Enables depth-aware LLM context filtering; enables Centaurian guard without extra infrastructure |
| GroupAgent per CPN | Each CPN manages its own sub-CPNs | Mirrors paper's group-agent protocol; sub-CPNs are the new "ephemeral teams" |
| Session owns HITLInject map | By transition ID | Multiple HITL branches can coexist in the same CPN; human resolves the right one |
| FlowLibrary by topology hash | `map[string]*CPN` | Crystallized flows are identified by structure, not by name — enables cross-client reuse |
| Lateral channels via GroupAgent.Deliver | Parent fans out to active sub-CPNs | Parent always observes; sub-CPNs don't reference each other directly |
| SpaceViolation error | Enforced in validator | Ensures the pipeline integrity rule from the paper: raw → observation → computation |

---

## 19. What This Document Does Not Include (Future Work)

- Persistence: Session Store and Event Store in memory. Redis/Postgres next phase.
- Multi-tenancy and authentication.
- Rate limiting per channel.
- Billing / token metering.
- UI for HITL review and flow approval.
- CPN visual editor (topology serialization to/from JSON is a prerequisite).
- Distributed executor: running sub-CPNs on separate machines (requires serializable tokens).
- CPN topology serialization: `encoding/json` marshaling of `*CPN` for persistence and cross-process passing of `ColorCPN` tokens.
- Formal verification: the paper references reachability and liveness analysis — CPN Tools integration is a future research direction.
