# CPN Agentic Engine

A pattern for building AI agent orchestration engines using Coloured Petri Nets.

This document is designed to give any engineer — from startup founder to systems programmer — a deep, first-principles understanding of how to build a production-grade agentic AI engine on top of Coloured Petri Nets (CPNs). It is language-agnostic, with working examples in Go, Rust, and Elixir. The goal is not to sell you a framework but to teach you the mathematics, the architecture, and the engineering so you can build your own.

Most agentic AI systems today are imperative pipelines dressed up as "agents." They call an LLM, parse the output, call another LLM, maybe use a tool. This works for demos. It falls apart when you need concurrency, human-in-the-loop approval gates, dynamic team composition, retry with circuit breakers, streaming, cost tracking, and the ability to formally verify that your agent can't deadlock. Coloured Petri Nets give you all of that, and the theory has been battle-tested for 60 years in fields where correctness matters: nuclear power plants, avionics, and telecommunications protocols.

The key insight is this: **every agent, tool, coordinator, and human interaction is the same thing — a CPN instance whose identity emerges from its topology and depth in the hierarchy.** There is no `Agent` class, no `Tool` class, no `Coordinator` class. There is one universal type. This radical simplification is what makes the system tractable.

---

## Table of Contents

1. [Why Petri Nets for Agents](#1-why-petri-nets-for-agents)
2. [Mathematical Foundations](#2-mathematical-foundations)
3. [From Math to Code: Core Types](#3-from-math-to-code-core-types)
4. [The Five Primitives](#4-the-five-primitives)
5. [Communication Spaces](#5-communication-spaces)
6. [The Executor Loop](#6-the-executor-loop)
7. [Transition Kinds: The Six Node Types](#7-transition-kinds-the-six-node-types)
8. [The Dual-Mode System: MAS and Centaurian](#8-the-dual-mode-system-mas-and-centaurian)
9. [Human-in-the-Loop (HITL)](#9-human-in-the-loop-hitl)
10. [Sub-CPNs: Hierarchical Agent Teams](#10-sub-cpns-hierarchical-agent-teams)
11. [Group Agents and Event Observation](#11-group-agents-and-event-observation)
12. [Memory and Context Assembly](#12-memory-and-context-assembly)
13. [Retry, Circuit Breakers, and Resilience](#13-retry-circuit-breakers-and-resilience)
14. [Validation and Self-Correction](#14-validation-and-self-correction)
15. [Sessions: The User's Only Interface](#15-sessions-the-users-only-interface)
16. [Topology Validation](#16-topology-validation)
17. [Cost Tracking and Flow Ranking](#17-cost-tracking-and-flow-ranking)
18. [Hexagonal Architecture: Ports and Adapters](#18-hexagonal-architecture-ports-and-adapters)
19. [Building Your First CPN Agent (Step by Step)](#19-building-your-first-cpn-agent-step-by-step)
20. [Complete Working Examples](#20-complete-working-examples)
21. [Learning Path and Resources](#21-learning-path-and-resources)

---

## 1. Why Petri Nets for Agents

The agentic AI space has a tooling problem. Most frameworks give you one of two things:

1. **DAGs** (Directed Acyclic Graphs) — good for static pipelines, bad for loops, human interaction, and dynamic behavior.
2. **Imperative code with LLM calls** — flexible but impossible to reason about formally. You can't prove absence of deadlocks. You can't visualize the state. You can't replay.

Petri nets solve both problems because they are:

- **Inherently concurrent.** Multiple transitions can fire simultaneously. You don't need to think about thread pools or async/await — concurrency is a mathematical property of the net.
- **Formally analyzable.** You can prove properties like reachability (can the system reach state X?), liveness (will transition T eventually fire?), and boundedness (will memory grow without bound?).
- **Visual.** A Petri net is a diagram. You can draw it on a whiteboard. Non-engineers can understand it. This matters when you're debugging why your agent did something unexpected.
- **State-explicit.** The state of the entire system is the distribution of tokens across places. You can snapshot it, serialize it, restore it, diff it.

The **Coloured** extension adds types to tokens. Instead of anonymous black dots, tokens carry structured data — a user message, a JSON plan, a classification result, a human approval. Places become typed containers. Transitions become conditional on token types. This is what makes CPNs powerful enough to model real agent workflows.

### The academic foundation

This work is grounded in the paper ["Human-Artificial Interaction in the Age of Agentic AI: A System-Theoretical Approach"](https://arxiv.org/abs/2502.14000) (Borghoff, Bottoni, Pareschi — 2025), which formalizes the use of Coloured Petri Nets for modeling both multi-agent systems (MAS) and deeply integrated human-AI systems ("Centaurian" architectures). The key contributions from that paper that we implement here:

- **Communication Spaces** — partitioning places into Surface, Observation, and Computation layers
- **Dual-mode execution** — MAS (autonomous) and Centaurian (human co-trigger) modes in the same net
- **Group agents** — dynamic team management with register/deliver/deregister protocols
- **Living systems theory** — clear boundaries, regulated interactions, adaptive feedback loops

---

## 2. Mathematical Foundations

> *"If you want to build a ship, don't drum up people to collect wood and don't assign them tasks and work, but rather teach them to long for the endless immensity of the sea." — Antoine de Saint-Exupery*

Before writing a single line of code, you need to understand the mathematics. This section is written for engineers, not mathematicians — we'll use precise definitions but explain every symbol.

### 2.1 Classical Petri Net

A Petri net is a tuple **(P, T, F, M₀)** where:

- **P** = a finite set of *places* (drawn as circles)
- **T** = a finite set of *transitions* (drawn as rectangles)
- **F** ⊆ (P × T) ∪ (T × P) = a set of *arcs* connecting places to transitions and transitions to places
- **M₀**: P → N = the *initial marking* — how many tokens each place starts with

The net is **bipartite**: arcs only connect places to transitions or transitions to places, never place-to-place or transition-to-transition.

**Firing rule:** A transition *t* is *enabled* (can fire) when every input place has at least one token. When *t* fires:
1. One token is removed from each input place
2. One token is added to each output place

That's it. Everything else — concurrency, synchronization, mutual exclusion, producer-consumer — emerges from this simple rule applied to different topologies.

```
  Before firing:         After firing:

  [P1: ●●] → T1 → [P2: ]     [P1: ●] → T1 → [P2: ●]

  Token consumed from P1, deposited into P2
```

### 2.2 Coloured Petri Net (CPN)

A CPN extends the classical net with types. Formally, a CPN is a tuple **(P, T, A, Σ, C, G, E, M₀)** where:

- **Σ** = a finite set of *color sets* (types). Think of these as your type system: `STRING`, `JSON`, `ARTIFACT`, `HUMAN`, `ERROR`, etc.
- **C**: P → Σ = the *color function* that assigns a type to each place. Place P1 might accept only `STRING` tokens, P2 only `JSON` tokens.
- **G** = *guard functions* on transitions. A guard is a predicate over the input tokens. The transition only fires if the guard returns true.
- **E** = *arc expressions* that determine which tokens flow along which arcs.

In practice, this means:

```
Place "user_input" (Color: STRING, Space: Surface)
  contains: Token{Color: STRING, Payload: "What's the weather?", Origin: human}

Place "classification" (Color: JSON, Space: Computation)
  contains: Token{Color: JSON, Payload: {"intent": "weather_query"}, Origin: llm}
```

A transition with a guard might look like:

```
Transition "route_to_weather" {
  InputPlaces:  ["classification"]
  OutputPlaces: ["weather_agent_input"]
  Guard: func(tokens) -> tokens[0].Payload.intent == "weather_query"
}
```

### 2.3 Key Properties You Get for Free

Once your agent is modeled as a CPN, you can formally verify:

| Property | Question it answers | Why it matters for agents |
|----------|-------------------|--------------------------|
| **Reachability** | Can the system reach state X? | "Can my agent ever reach a state where it sends an email without approval?" |
| **Liveness** | Will transition T eventually fire? | "Will my HITL approval gate ever unblock?" |
| **Boundedness** | Can place P accumulate unbounded tokens? | "Will my message queue grow forever if the LLM is slow?" |
| **Deadlock freedom** | Can the system reach a state where nothing can fire? | "Can my agent get stuck with no progress possible?" |
| **Fairness** | Will every enabled transition eventually fire? | "Will the retry path ever starve the happy path?" |

In most agent frameworks, you discover deadlocks in production at 3 AM. With CPNs, you discover them at compile time.

### 2.4 The Bipartite Graph Intuition

Think of a CPN as a **bipartite directed graph**:

```
         ┌──────────┐        ┌──────────┐        ┌──────────┐
         │  Place    │───────▶│Transition│───────▶│  Place    │
         │  (data)   │        │ (compute)│        │  (data)   │
         └──────────┘        └──────────┘        └──────────┘
              ○                    ■                    ○
           "buffer"             "action"            "buffer"
```

- **Places** are passive. They hold data. They are buffers, queues, registers.
- **Transitions** are active. They transform data. They are functions, LLM calls, tool invocations, human approval gates.
- **Tokens** are the data itself. They flow through the net, carrying payloads, origin metadata, and type information.

---

## 3. From Math to Code: Core Types

Here's how the mathematical primitives map to code. We show all three languages side by side.

### 3.1 Token — The Unit of Data

A token is the fundamental unit of data flowing through the net. It carries a typed payload, its origin (who created it), and its spatial identity (which communication layer it belongs to).

**Go:**
```go
type Token struct {
    Color       ColorSet   // Type classification: STRING, JSON, ARTIFACT, HUMAN, ERROR
    Payload     any        // The actual data. Consumers type-assert.
    OriginID    string     // ID of the CPN that produced this token
    OriginDepth int        // Depth of producer (0=root, 1=domain, 2+=worker, -1=human)
    OriginKind  NodeKind   // Kind of transition that produced this (llm, tool, hitl...)
    Space       SpaceKind  // Communication layer: surface, observation, computation
    SessionID   string     // Links to user session
    TraceID     string     // Distributed tracing
    Timestamp   time.Time  // Creation time
}

// IsHumanOrigin returns true if this token was produced by a human.
// Used by Centaurian guard functions.
func (t *Token) IsHumanOrigin() bool {
    return t.Color == ColorHuman || t.OriginKind == NodeKindHITL
}
```

**Rust:**
```rust
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::any::Any;

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub enum ColorSet {
    String,
    Json,
    Artifact,
    Score,
    Event,
    Human,
    Error,
    Schema,
    Identity,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub enum SpaceKind {
    Surface,
    Observation,
    Computation,
}

#[derive(Debug, Clone)]
pub struct Token {
    pub color: ColorSet,
    pub payload: Box<dyn Any + Send + Sync>,
    pub origin_id: String,
    pub origin_depth: i32,
    pub origin_kind: NodeKind,
    pub space: SpaceKind,
    pub session_id: String,
    pub trace_id: String,
    pub timestamp: DateTime<Utc>,
}

impl Token {
    pub fn is_human_origin(&self) -> bool {
        self.color == ColorSet::Human || self.origin_kind == NodeKind::HITL
    }
}
```

**Elixir:**
```elixir
defmodule CPN.Token do
  @moduledoc """
  The fundamental unit of data in the CPN.
  Immutable — created once, never modified in place.
  """

  @type color_set :: :string | :json | :artifact | :score | :event
                   | :human | :error | :schema | :identity

  @type space_kind :: :surface | :observation | :computation

  @type t :: %__MODULE__{
    color: color_set(),
    payload: any(),
    origin_id: String.t(),
    origin_depth: integer(),
    origin_kind: CPN.NodeKind.t(),
    space: space_kind(),
    session_id: String.t(),
    trace_id: String.t(),
    timestamp: DateTime.t()
  }

  defstruct [
    :color, :payload, :origin_id, :origin_depth, :origin_kind,
    :space, :session_id, :trace_id, :timestamp
  ]

  @spec human_origin?(t()) :: boolean()
  def human_origin?(%__MODULE__{color: :human}), do: true
  def human_origin?(%__MODULE__{origin_kind: :hitl}), do: true
  def human_origin?(_), do: false
end
```

### 3.2 Place — The Typed Buffer

A place is a thread-safe, typed container for tokens. It enforces color constraints (type safety) and space constraints (layer isolation).

**Go:**
```go
type Place struct {
    ID     string
    Color  ColorSet    // Only tokens of this color can be deposited
    Space  SpaceKind   // Communication layer this place belongs to
    Tokens []*Token    // The token buffer (mutex-protected)
    mu     sync.Mutex
}

func (p *Place) Deposit(t *Token) error {
    // Enforce space isolation: surface tokens cannot skip to computation
    if t.Space == SpaceSurface && p.Space == SpaceComputation {
        return ErrSpaceViolation
    }
    // Enforce type safety
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

func (p *Place) Consume() (*Token, error) {
    p.mu.Lock()
    defer p.mu.Unlock()
    if len(p.Tokens) == 0 {
        return nil, ErrEmptyPlace
    }
    t := p.Tokens[0]
    p.Tokens = p.Tokens[1:]
    return t, nil
}
```

**Rust:**
```rust
use std::sync::Mutex;
use std::collections::VecDeque;

pub struct Place {
    pub id: String,
    pub color: ColorSet,
    pub space: SpaceKind,
    tokens: Mutex<VecDeque<Token>>,
}

impl Place {
    pub fn new(id: String, color: ColorSet, space: SpaceKind) -> Self {
        Place {
            id,
            color,
            space,
            tokens: Mutex::new(VecDeque::new()),
        }
    }

    pub fn deposit(&self, token: Token) -> Result<(), CPNError> {
        if token.space == SpaceKind::Surface && self.space == SpaceKind::Computation {
            return Err(CPNError::SpaceViolation);
        }
        if token.color != self.color {
            return Err(CPNError::ColorMismatch);
        }
        let mut tokens = self.tokens.lock().unwrap();
        tokens.push_back(token);
        Ok(())
    }

    pub fn consume(&self) -> Result<Token, CPNError> {
        let mut tokens = self.tokens.lock().unwrap();
        tokens.pop_front().ok_or(CPNError::EmptyPlace)
    }

    pub fn peek(&self) -> Vec<Token> {
        let tokens = self.tokens.lock().unwrap();
        tokens.iter().cloned().collect()
    }

    pub fn len(&self) -> usize {
        self.tokens.lock().unwrap().len()
    }
}
```

**Elixir:**
```elixir
defmodule CPN.Place do
  @moduledoc """
  A typed, concurrent buffer for tokens.
  Implemented as a GenServer for thread-safe access via the BEAM.
  """
  use GenServer

  defstruct [:id, :color, :space, tokens: :queue.new()]

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: via(opts[:id]))
  end

  def deposit(place_id, %CPN.Token{} = token) do
    GenServer.call(via(place_id), {:deposit, token})
  end

  def consume(place_id) do
    GenServer.call(via(place_id), :consume)
  end

  # --- Callbacks ---

  @impl true
  def init(opts) do
    {:ok, %__MODULE__{
      id: opts[:id],
      color: opts[:color],
      space: opts[:space]
    }}
  end

  @impl true
  def handle_call({:deposit, token}, _from, state) do
    with :ok <- validate_space(token.space, state.space),
         :ok <- validate_color(token.color, state.color) do
      {:reply, :ok, %{state | tokens: :queue.in(token, state.tokens)}}
    else
      {:error, _} = err -> {:reply, err, state}
    end
  end

  @impl true
  def handle_call(:consume, _from, state) do
    case :queue.out(state.tokens) do
      {{:value, token}, rest} -> {:reply, {:ok, token}, %{state | tokens: rest}}
      {:empty, _} -> {:reply, {:error, :empty_place}, state}
    end
  end

  defp validate_space(:surface, :computation), do: {:error, :space_violation}
  defp validate_space(s, s), do: :ok
  defp validate_space(_, _), do: {:error, :space_mismatch}

  defp validate_color(c, c), do: :ok
  defp validate_color(_, _), do: {:error, :color_mismatch}

  defp via(id), do: {:via, Registry, {CPN.Registry, id}}
end
```

### 3.3 Transition — The Unit of Computation

A transition is a conditional action that fires when its input places have tokens and its guard is satisfied.

**Go:**
```go
type Transition struct {
    ID           string
    Kind         NodeKind   // tool, llm, validate, subnet, observer, hitl
    InputPlaces  []string   // IDs of input places
    OutputPlaces []string   // IDs of output places
    ErrorPlace   string     // Fallback for error tokens
    Guard        func(tokens []*Token) bool  // Optional firing condition
    Retry        *RetryPolicy
    // Kind-specific fields (LLMConfig, SubNet, HITLConfig, etc.)
}

func (t *Transition) CanFire(places map[string]*Place) bool {
    if len(t.InputPlaces) == 0 {
        return false
    }
    var tokens []*Token
    for _, pid := range t.InputPlaces {
        p, ok := places[pid]
        if !ok { return false }
        ts, ok := p.Peek()
        if !ok { return false }
        tokens = append(tokens, ts...)
    }
    if t.Guard != nil {
        return t.Guard(tokens)
    }
    return true
}
```

**Rust:**
```rust
pub struct Transition {
    pub id: String,
    pub kind: NodeKind,
    pub input_places: Vec<String>,
    pub output_places: Vec<String>,
    pub error_place: Option<String>,
    pub guard: Option<Box<dyn Fn(&[Token]) -> bool + Send + Sync>>,
    pub retry: Option<RetryPolicy>,
    // Kind-specific configuration via enum
    pub config: TransitionConfig,
}

pub enum TransitionConfig {
    Tool { executor: Box<dyn Fn(Token) -> Result<Token, CPNError> + Send + Sync> },
    LLM(LLMConfig),
    Validate(ValidateConfig),
    SubNet { factory: Box<dyn Fn() -> CPN + Send + Sync> },
    Observer { observed_cpn_id: Option<String> },
    HITL(HITLConfig),
}

impl Transition {
    pub fn can_fire(&self, places: &HashMap<String, Place>) -> bool {
        if self.input_places.is_empty() { return false; }

        let mut tokens = Vec::new();
        for pid in &self.input_places {
            match places.get(pid) {
                Some(place) => {
                    let peeked = place.peek();
                    if peeked.is_empty() { return false; }
                    tokens.extend(peeked);
                }
                None => return false,
            }
        }

        match &self.guard {
            Some(guard) => guard(&tokens),
            None => true,
        }
    }
}
```

**Elixir:**
```elixir
defmodule CPN.Transition do
  @type node_kind :: :tool | :llm | :validate | :subnet | :observer | :hitl

  @type t :: %__MODULE__{
    id: String.t(),
    kind: node_kind(),
    input_places: [String.t()],
    output_places: [String.t()],
    error_place: String.t() | nil,
    guard: (list(CPN.Token.t()) -> boolean()) | nil,
    config: map()
  }

  defstruct [:id, :kind, :input_places, :output_places, :error_place, :guard, :config]

  @spec can_fire?(t(), map()) :: boolean()
  def can_fire?(%__MODULE__{input_places: []}, _places), do: false
  def can_fire?(%__MODULE__{} = t, places) do
    tokens =
      Enum.flat_map(t.input_places, fn pid ->
        case Map.get(places, pid) do
          nil -> throw(:missing_place)
          place ->
            case CPN.Place.peek(place.id) do
              {:ok, tokens} when tokens != [] -> tokens
              _ -> throw(:empty_place)
            end
        end
      end)

    case t.guard do
      nil -> true
      guard_fn -> guard_fn.(tokens)
    end
  catch
    :missing_place -> false
    :empty_place -> false
  end
end
```

### 3.4 The CPN — The Universal Agent Type

This is the critical insight: **a CPN is the only agent type you need.** A CPN at depth 0 is a root coordinator. At depth 1, a domain specialist. At depth 2+, a worker. The behavior emerges from topology, not from class hierarchy.

**Go:**
```go
type CPN struct {
    ID          string
    Role        string             // "coordinator", "researcher", "coder", etc.
    Depth       int                // 0=root, 1=domain, 2+=worker
    Mode        Mode               // "mas" (autonomous) or "centaurian" (human co-trigger)
    State       State              // idle, running, waiting, completed, failed
    Places      map[string]*Place
    Transitions map[string]*Transition
    SessionID   string
    LLMClient   LLMClient          // Port interface for LLM calls
    History     []*Message         // Conversation context
    Group       *GroupAgent        // Manages sub-CPN team
    EventSink   func(*Event)       // Event callback
}
```

---

## 4. The Five Primitives

Every CPN operation reduces to five primitive operations. Master these and you understand the entire engine.

| # | Primitive | Description | Thread Safety |
|---|-----------|-------------|---------------|
| 1 | **Deposit** | Add a token to a place | Mutex on place |
| 2 | **Consume** | Remove oldest token from a place (FIFO) | Mutex on place |
| 3 | **Peek** | Read tokens without removing (snapshot) | Mutex on place |
| 4 | **CanFire** | Check if a transition's inputs are satisfied + guard passes | Read-only peek |
| 5 | **Fire** | Consume inputs, execute computation, deposit outputs | Consume on main goroutine, compute in parallel |

The critical engineering insight is the **consume-before-launch** pattern:

```
Main thread:
  1. Collect all firable transitions
  2. For each: re-check CanFire (it may have changed)
  3. CONSUME tokens on the main thread
  4. Launch fire goroutines with consumed tokens (by value, not reference)
  5. Wait for all goroutines to complete

This prevents two transitions from consuming the same token.
```

---

## 5. Communication Spaces

Communication spaces are the conceptual layers that partition a CPN's places into distinct zones with different roles. This comes directly from the academic paper's framework (Section 4) and maps precisely to the MVC pattern:

```
┌─────────────────────────────────────────────────┐
│                 SURFACE SPACE                    │
│  User-facing: prompts, responses, UI artifacts   │
│  Token types: STRING, ARTIFACT, HUMAN            │
│  Analogy: View layer                             │
├─────────────────────────────────────────────────┤
│               OBSERVATION SPACE                  │
│  Events from sub-CPNs, routing, coordination     │
│  Token types: EVENT                              │
│  Analogy: Controller layer                       │
├─────────────────────────────────────────────────┤
│              COMPUTATION SPACE                   │
│  Internal processing: classification, planning   │
│  Token types: JSON, SCORE, SCHEMA                │
│  Analogy: Model layer                            │
└─────────────────────────────────────────────────┘
```

### The Space Isolation Rule

**A surface token cannot skip directly to a computation place.** This is enforced at deposit time and validated at topology construction. The rationale: raw user input must pass through at least one transition (classification, parsing, sanitization) before entering the computation layer.

The **only exception** is HITL transitions. A HITL transition is the sanctioned bridge between surface and computation — it's how human approval tokens enter the computation layer. This is by design: human judgment is the one thing that should be able to bypass the normal processing pipeline.

```
ALLOWED:
  Surface → [Transition] → Observation → [Transition] → Computation
  Surface → [HITL Transition] → Computation   (sanctioned bridge)

FORBIDDEN:
  Surface → [Regular Transition] → Computation  (space violation!)
```

---

## 6. The Executor Loop

The executor is the heart of the engine. It's a single loop that runs until the CPN completes, deadlocks, times out, or fails. Here's the algorithm:

```
func (c *CPN) Run(ctx context.Context) error {
    // 1. Validate topology before starting
    if err := Validate(c.Places, c.Transitions); err != nil {
        return err
    }
    c.State = Running

    for {
        // 2. Check context cancellation (timeout, parent cancel)
        if ctx.Err() != nil {
            return ErrTimeout
        }

        // 3. Drain observer events from sub-CPN buses
        drainObservers(ctx, c)

        // 4. Collect firable transitions (mode-aware)
        firable := c.collectFirable()

        // 5. Nothing firable? Check completion or deadlock
        if len(firable) == 0 {
            if c.IsComplete() {
                c.State = Completed
                return nil
            }
            if c.activeChildren > 0 {
                // Wait for children — they may deposit tokens
                c.childWg.Wait()
                continue  // Re-evaluate
            }
            return ErrDeadlock
        }

        // 6. Sort for determinism, consume tokens on main thread
        sort.Slice(firable, func(i, j int) bool {
            return firable[i].ID < firable[j].ID
        })

        var firings []firing
        for _, t := range firable {
            if !c.effectiveCanFire(t) { continue }  // Re-check after prior consume
            consumed := consumeAll(t.InputPlaces, c.Places)
            firings = append(firings, firing{t, consumed})
        }

        // 7. Launch fire goroutines in parallel
        var wg sync.WaitGroup
        errCh := make(chan fireResult, len(firings))
        for _, f := range firings {
            wg.Add(1)
            go func(t *Transition, consumed []Token) {
                defer wg.Done()
                _, _, err := dispatch(ctx, t, c, consumed)
                if err != nil {
                    errCh <- fireResult{t.ID, err}
                }
            }(f.transition, f.consumed)
        }
        wg.Wait()
        close(errCh)

        // 8. Process errors: route to ErrorPlace or fail the CPN
        for fr := range errCh {
            t := c.Transitions[fr.transitionID]
            if t.ErrorPlace != "" {
                // Route error token to ErrorPlace for recovery
                depositErrorToken(c, t, fr.err)
                continue
            }
            return fr.err  // No recovery — CPN fails
        }

        // 9. Check mode transitions (MAS ↔ Centaurian)
        c.checkModeSwitch()
    }
}
```

### Completion Detection

A CPN is complete when **all terminal places have at least one token.** A terminal place is one that is not an input to any transition — it's a "sink" in the graph.

```go
func (c *CPN) IsComplete() bool {
    terminals := c.TerminalPlaces()
    if len(terminals) == 0 { return false }
    for _, p := range terminals {
        if p.Len() == 0 { return false }
    }
    return true
}

func (c *CPN) TerminalPlaces() []*Place {
    inputRefs := make(map[string]bool)
    for _, t := range c.Transitions {
        for _, pid := range t.InputPlaces {
            inputRefs[pid] = true
        }
    }
    var terminals []*Place
    for id, p := range c.Places {
        if !inputRefs[id] {
            terminals = append(terminals, p)
        }
    }
    return terminals
}
```

---

## 7. Transition Kinds: The Six Node Types

Every transition has a `Kind` that determines its firing behavior. There are exactly six kinds:

### 7.1 Tool (`tool`)

Calls an external function or API. Deterministic, no LLM involved.

```go
// A tool transition has an Executor function
t := &Transition{
    ID:   "t-fetch-weather",
    Kind: NodeKindTool,
    InputPlaces:  []string{"weather-query"},
    OutputPlaces: []string{"weather-result"},
    Executor: func(ctx context.Context, in Token) (Token, error) {
        query := in.Payload.(string)
        result, err := weatherAPI.Fetch(ctx, query)
        if err != nil { return Token{}, err }
        return Token{Color: ColorJSON, Payload: result}, nil
    },
}
```

### 7.2 LLM (`llm`)

Calls a language model. Supports streaming, tool use (agentic loop), budget limits, and context assembly.

```go
t := &Transition{
    ID:   "t-respond",
    Kind: NodeKindLLM,
    InputPlaces:  []string{"classified-input"},
    OutputPlaces: []string{"response-output"},
    SystemPrompt: "You are a helpful assistant.",
    LLMConfig: &LLMConfig{
        Model:        "anthropic/claude-sonnet-4-20250514",
        MaxTokens:    4096,
        Temperature:  0.7,
        StreamOutput: true,   // Real-time streaming to user
        Budget:       0.10,   // Max $0.10 per call
    },
    LLMTools: []string{"t-fetch-weather", "t-search-web"},  // Tools the LLM can invoke
}
```

The LLM transition implements a full **agentic tool-call loop**:
1. Call LLM with tools available
2. If LLM requests a tool → execute it → feed result back to LLM
3. Repeat until LLM returns final content (capped at 10 iterations)

### 7.3 Validate (`validate`)

Schema validation with self-correction. If the payload doesn't match the expected schema, an LLM can attempt to fix it.

```go
t := &Transition{
    ID:   "t-validate-plan",
    Kind: NodeKindValidate,
    InputPlaces:  []string{"raw-plan"},
    OutputPlaces: []string{"valid-plan"},
    ErrorPlace:   "plan-errors",
    ValidateConfig: &ValidateConfig{
        Schema:          &PlanSchema{},    // Go struct for round-trip validation
        MaxCorrections:  3,                // Up to 3 LLM correction attempts
        CorrectionLLMID: "t-fix-json",    // Which LLM transition to use for fixes
    },
}
```

### 7.4 SubNet (`subnet`)

Spawns a child CPN in a new goroutine. The parent continues — it does not block. When the child completes, its output tokens are deposited back into the parent.

```go
t := &Transition{
    ID:   "t-research",
    Kind: NodeKindSubNet,
    InputPlaces:  []string{"research-task"},
    OutputPlaces: []string{"research-result"},
    SubNetFactory: func() *CPN {
        return buildResearcherCPN()  // Returns a fresh CPN topology
    },
}
```

### 7.5 Observer (`observer`)

Watches events from sub-CPNs. Deposits event tokens into observation-space places for downstream processing.

```go
t := &Transition{
    ID:   "t-watch-research",
    Kind: NodeKindObserver,
    OutputPlaces:  []string{"research-events"},
    ObservedCPNID: "",  // Empty = observe all children
    EventFilter: func(e Event) bool {
        return e.Type == EventSubNetCompleted || e.Type == EventSubNetFailed
    },
}
```

### 7.6 HITL (`hitl`)

Human-in-the-loop. Blocks the CPN until a human provides input (approve, reject, or revise).

```go
hitlCh := make(chan Token, 1)

t := &Transition{
    ID:   "t-approve-plan",
    Kind: NodeKindHITL,
    InputPlaces:  []string{"proposed-plan"},
    OutputPlaces: []string{"approved-plan"},
    HITLConfig: &HITLConfig{
        Channel:      hitlCh,
        Prompt:       "Please review this plan and approve, reject, or revise.",
        RevisionLoop: true,          // Multi-round revision support
        MaxRevisions: 5,
        CorrectionLLMID: "t-revise", // LLM for applying revision feedback
    },
}
```

---

## 8. The Dual-Mode System: MAS and Centaurian

This is one of the most novel aspects of the engine. A single CPN can operate in two modes:

### 8.1 MAS Mode (Multi-Agent System)

In MAS mode, transitions fire autonomously. The LLM classifies, plans, and executes without human intervention. This is the default for most interactions.

```
User: "What's the weather in Tokyo?"
→ classify → route → fetch_weather_tool → respond
  (All automatic, no human needed)
```

### 8.2 Centaurian Mode

In Centaurian mode, computation-space transitions require BOTH a human-origin token AND an AI token to fire. Neither human nor AI can trigger computation alone.

```go
// The Centaurian guard — injected automatically in Centaurian mode
func centaurianGuard(tokens []*Token) bool {
    var hasHuman, hasAI bool
    for _, tok := range tokens {
        if tok.IsHumanOrigin() {
            hasHuman = true
        } else {
            hasAI = true
        }
        if hasHuman && hasAI {
            return true
        }
    }
    return false
}
```

### 8.3 Automatic Mode Switching

The mode switches dynamically based on token state:

- **MAS → Centaurian:** When a human-origin token enters a computation-space place (via HITL approval)
- **Centaurian → MAS:** When no computation-space place contains human-origin tokens AND no HITL transition is firable

This means the system operates autonomously by default but automatically shifts to collaborative mode when a human provides input that reaches the computation layer.

```
                    ┌─────────────┐
                    │   MAS Mode  │ (autonomous)
                    └──────┬──────┘
                           │ human token enters computation space
                           ▼
                    ┌──────────────┐
                    │  Centaurian  │ (co-trigger required)
                    │    Mode      │
                    └──────┬───────┘
                           │ no human tokens in computation + no HITL firable
                           ▼
                    ┌─────────────┐
                    │   MAS Mode  │ (back to autonomous)
                    └─────────────┘
```

---

## 9. Human-in-the-Loop (HITL)

HITL is implemented as a transition kind, not as special middleware. This means it participates in the same firing rules, guard conditions, and error handling as every other transition.

### 9.1 Single-Turn HITL

```
1. Emit EventHITLRequested with prompt
2. Set CPN state to "waiting"
3. Block on the HITL channel (or context timeout)
4. Receive human response token (ColorHuman)
5. If action == "reject" → return ErrHITLRejected
6. If action == "approve" → deposit token to output places
7. Space bridge: adjust token space to match output place
8. If output is computation space → trigger Centaurian mode
```

### 9.2 Multi-Round Revision Loop

```
Round 0:
  AI produces draft → HITL asks human to review
  Human: "revise — make it more concise"

Round 1:
  Revision LLM rewrites based on feedback → HITL asks again
  Human: "revise — add an example"

Round 2:
  Revision LLM rewrites again → HITL asks again
  Human: "approve"
  → Approved token deposited to output

(Capped at MaxRevisions, default 5)
```

### 9.3 The Space Bridge

HITL is the **only** transition type that can bridge the surface-to-computation boundary. When a human approves something, the token's space is adjusted to match the output place. This is by design: it makes the human the explicit gatekeeper between the user-facing layer and the internal computation layer.

---

## 10. Sub-CPNs: Hierarchical Agent Teams

A CPN can spawn child CPNs as sub-agents. This creates a hierarchy:

```
Depth 0: Root Coordinator (orchestrates the team)
├── Depth 1: Researcher CPN (gathers information)
│   ├── Depth 2: Web Search Worker
│   └── Depth 2: Document Reader Worker
├── Depth 1: Analyst CPN (processes findings)
└── Depth 1: Writer CPN (produces final output)
```

### Lifecycle

1. Parent's SubNet transition fires
2. A fresh child CPN is created (via factory or clone)
3. Child gets `depth = parent.depth + 1`, inherits `sessionID`
4. Consumed tokens are injected into child's source places
5. Child runs in its own goroutine — **parent does NOT block**
6. Child's events are sent to parent via a buffered channel (event bus)
7. When child completes, its terminal-place tokens are deposited into parent's output places
8. A compressed summary is added to parent's conversation history

### Non-Blocking Execution

This is critical: the parent continues its executor loop while children run. This means:

- Multiple children can run concurrently
- The parent can fire other transitions while waiting
- Observer transitions can monitor child progress in real-time
- The parent only blocks when it has no firable transitions AND children are still active

```go
// The parent tracks active children with atomic counter + WaitGroup
parent.activeChildren.Add(1)
parent.childWg.Go(func() {
    defer parent.activeChildren.Add(-1)
    defer close(bus)  // Close event bus when child finishes

    err := child.Run(ctx)
    if err != nil {
        // Route error to parent's ErrorPlace or fail parent
    } else {
        // Deposit child's output tokens into parent's output places
    }
})
```

---

## 11. Group Agents and Event Observation

### Group Agent Protocol

From the academic paper (Section 4.2), a group agent manages a team of sub-CPNs:

```
register(agent)   — Add to active or non-active set based on state
deliver(event)    — Fan-out event to all active members (non-blocking)
deregister(agent) — Remove from active set
switchCMP(agent)  — Move between active/non-active based on state change
```

**Go implementation:**
```go
type GroupAgent struct {
    ID        string
    Topic     string
    active    []*CPN
    nonActive []*CPN
    mu        sync.RWMutex
}

func (g *GroupAgent) Deliver(e *Event) {
    g.mu.RLock()
    defer g.mu.RUnlock()
    for _, c := range g.active {
        if c.EventEmitter == nil { continue }
        select {
        case c.EventEmitter <- *e:
        default: // Non-blocking: skip if buffer full
        }
    }
}
```

### Observer Pattern

Observer transitions drain events from child event buses and deposit them as tokens in observation-space places:

```
Phase 1: Non-blocking drain of ALL events from ALL child buses
Phase 2: For each event × each observer:
  - Check ObservedCPNID filter
  - Check EventFilter predicate
  - Create ColorEvent token
  - Deposit into observer's output places
```

This runs on the main goroutine, never blocks, never spawns goroutines.

---

## 12. Memory and Context Assembly

### Three-Tier Memory Strategy

When an LLM transition fires, its context window is assembled in three tiers:

```
┌────────────────────────────────────────────┐
│ T1: System Prompt (static, always included) │
├────────────────────────────────────────────┤
│ T2: Observer Summaries (compressed sub-CPN  │
│     results, always included)               │
├────────────────────────────────────────────┤
│ T3: Sliding Window (last N conversation     │
│     turns, user + assistant only)           │
└────────────────────────────────────────────┘
```

- **T1** is the transition's system prompt — static, deterministic
- **T2** carries compressed summaries from sub-CPNs. When a child completes, its output is compressed into a RoleObserver message and added to the parent's history.
- **T3** is a sliding window of raw conversation (default: last 10 turns = 20 messages)

This gives you:
- **Bounded context** — you never exceed the LLM's context window
- **Sub-CPN awareness** — the parent LLM knows what its children produced
- **Recency bias** — recent messages are always included; old ones slide out

```go
func BuildContext(systemPrompt string, history []*Message, windowSize int) ContextWindow {
    // T2: All observer messages (never evicted)
    var observers []*LLMMessage
    for _, m := range history {
        if m.Role == RoleObserver {
            observers = append(observers, &LLMMessage{
                Role:    "assistant",
                Content: fmt.Sprintf("[Summary from %s]: %s", m.CPNRole, m.Content),
            })
        }
    }

    // T3: Sliding window of user/assistant messages
    raw := filterUserAndAssistant(history)
    limit := windowSize * 2
    if len(raw) > limit {
        raw = raw[len(raw)-limit:]
    }

    messages := append(observers, toMessages(raw)...)
    return ContextWindow{SystemPrompt: systemPrompt, Messages: messages}
}
```

---

## 13. Retry, Circuit Breakers, and Resilience

### Retry Policy

Every transition can have a retry policy with exponential backoff:

```go
type RetryPolicy struct {
    MaxAttempts int            // Total attempts (default: 1 = no retry)
    InitialWait time.Duration  // Wait before first retry
    MaxWait     time.Duration  // Cap on exponential growth
    Multiplier  float64        // Backoff multiplier (default: 2.0)
    RetryOn     func(err error, attempt int) bool  // Which errors to retry
}

// Standard policy: 3 attempts, 500ms → 1s → 2s, skip context errors
func DefaultRetryPolicy() *RetryPolicy {
    return &RetryPolicy{
        MaxAttempts: 3,
        InitialWait: 500 * time.Millisecond,
        MaxWait:     30 * time.Second,
        Multiplier:  2.0,
        RetryOn: func(err error, _ int) bool {
            return !errors.Is(err, context.Canceled) &&
                   !errors.Is(err, context.DeadlineExceeded)
        },
    }
}
```

### Circuit Breaker

A circuit breaker tracks failures across all firings of a transition:

```
State Machine:
  CLOSED → (failures ≥ threshold) → OPEN
  OPEN   → (time elapsed > duration) → HALF-OPEN
  HALF-OPEN → (success) → CLOSED
  HALF-OPEN → (failure) → OPEN
```

When the circuit is OPEN, `CanFire` returns false — the transition is temporarily disabled. This prevents a failing LLM provider from consuming all your budget on retries.

**Rust implementation:**
```rust
use std::time::{Duration, Instant};
use std::sync::Mutex;

pub struct CircuitBreaker {
    config: CircuitBreakerConfig,
    state: Mutex<CBState>,
}

struct CBState {
    failures: u32,
    tripped_at: Option<Instant>,
}

pub struct CircuitBreakerConfig {
    pub failure_threshold: u32,
    pub open_duration: Duration,
}

impl CircuitBreaker {
    pub fn allow(&self) -> bool {
        let mut state = self.state.lock().unwrap();
        match state.tripped_at {
            None => true,
            Some(tripped) => {
                if tripped.elapsed() > self.config.open_duration {
                    state.tripped_at = None; // half-open
                    true
                } else {
                    false // still open
                }
            }
        }
    }

    pub fn record_failure(&self) {
        let mut state = self.state.lock().unwrap();
        state.failures += 1;
        if state.failures >= self.config.failure_threshold {
            state.tripped_at = Some(Instant::now());
        }
    }

    pub fn record_success(&self) {
        let mut state = self.state.lock().unwrap();
        state.failures = 0;
        state.tripped_at = None;
    }
}
```

---

## 14. Validation and Self-Correction

The validate transition implements a pattern critical for production agents: **structured output with automatic repair.**

```
1. Consume token from input place
2. Validate payload against schema (JSON round-trip or custom function)
3. If valid → apply OnSuccess transform → deposit to output
4. If invalid + corrections available:
   a. Call correction LLM with error message and invalid payload
   b. Re-validate corrected output
   c. Repeat up to MaxCorrections (capped at 10)
5. If all corrections exhausted → route to ErrorPlace
```

This closes the gap between "LLMs produce unstructured text" and "my downstream code needs typed data."

---

## 15. Sessions: The User's Only Interface

The Session is the boundary between the CPN engine and the outside world. Users never see topologies, transitions, or tokens. They see a stream.

```go
type Session struct {
    ID        string
    UserID    string
    Channel   ChannelType    // web, whatsapp, telegram
    Root      *CPN           // The depth-0 CPN for this session
    Stream    chan StreamChunk // Real-time LLM output to the user
    hitlInject map[string]chan Token  // HITL response channels
}

// The user sends a message → it becomes a token deposited into the root CPN
// The root CPN processes it → StreamChunks flow back to the user via SSE
```

### StreamChunk

```go
type StreamChunk struct {
    SessionID string
    CPNID     string  // Which CPN produced this chunk
    CPNRole   string  // "coordinator", "researcher", etc.
    Content   string  // The text delta
    Done      bool    // True for the final chunk
}
```

---

## 16. Topology Validation

Before any CPN runs, its topology is validated. This catches bugs at startup, not at 3 AM.

Checks performed:

1. **Arc reference integrity** — every place ID referenced by a transition must exist in the places map
2. **Space violation detection** — no transition may wire surface inputs directly to computation outputs (except HITL)
3. **HITL configuration** — HITL transitions must have non-nil HITLConfig with non-nil Channel
4. **ErrorPlace existence** — if a transition references an ErrorPlace, it must exist

```go
func Validate(places map[string]*Place, transitions map[string]*Transition) error {
    var errs []error
    for _, t := range transitions {
        errs = append(errs, checkArcReferences(t, places)...)
        if t.Kind != NodeKindHITL {
            errs = append(errs, checkSpaceViolations(t, places)...)
        }
        errs = append(errs, checkHITLConfig(t)...)
    }
    if len(errs) > 0 {
        return &ValidationErrors{Errors: errs}
    }
    return nil
}
```

---

## 17. Cost Tracking and Flow Ranking

### Execution Metrics

Every CPN run records metrics: transition count by kind, tokens produced, total cost, duration, success/failure.

### Flow Ranking

The ranking formula implements the principle: **LLM is the last resort.**

```
Score = LLMCallCount × LLMCallWeight + TotalCostUSD × CostWeight
```

Lower score = better flow. A flow that uses 2 tool calls and 1 LLM call ranks better than a flow using 5 LLM calls. This incentivizes topologies that use tools and validation before resorting to expensive LLM calls.

```go
func RankScore(rec *ExecutionRecord, w RankingWeights) float64 {
    return float64(rec.LLMCallCount)*w.LLMCallWeight + rec.TotalCostUSD*w.CostWeight
}
```

---

## 18. Hexagonal Architecture: Ports and Adapters

The CPN engine follows hexagonal architecture strictly. The domain (`cpn/`) defines port interfaces. Infrastructure implements them.

### Port Interfaces

```go
// The CPN domain defines what it needs — not how it's provided

type LLMClient interface {
    Complete(ctx context.Context, req *LLMRequest) (LLMResponse, error)
    CompleteStream(ctx context.Context, req *LLMRequest, onChunk func(string)) (LLMResponse, error)
    EstimateCost(req *LLMRequest) (float64, error)
}

type ChannelAdapter interface {
    Send(ctx context.Context, chunk StreamChunk) error
    Receive(ctx context.Context) (Message, error)
    Channel() ChannelType
}

type CostProvider interface {
    SessionCostUSD(sessionID string) float64
}
```

### Adapter Examples

```
Domain (cpn/)             Driven Adapters (infra/)       Driving Adapters (internal/driving/)
────────────────          ─────────────────────         ───────────────────────────────────
LLMClient        ←──── OpenRouterClient               HTTP API handlers ────→ SessionService
ChannelAdapter   ←──── HTTPChannelAdapter              SSE Broker             (Application Layer)
CostProvider     ←──── TokenLedger
```

The domain NEVER imports infrastructure. Infrastructure implements domain interfaces via dependency inversion. This means you can swap OpenRouter for Anthropic direct, or swap HTTP/SSE for WebSocket, without touching a single line of domain code.

---

## 19. Building Your First CPN Agent (Step by Step)

Let's build a simple agent that classifies user intent and routes to the appropriate handler.

### Step 1: Define Places

```go
places := map[string]*Place{
    // Surface space — user-facing
    "p-input":    NewPlace("p-input",    ColorString,   SpaceSurface),
    "p-response": NewPlace("p-response", ColorArtifact, SpaceSurface),

    // Computation space — internal processing
    "p-classified": NewPlace("p-classified", ColorJSON,     SpaceComputation),
    "p-greeting":   NewPlace("p-greeting",   ColorString,   SpaceComputation),
    "p-question":   NewPlace("p-question",   ColorString,   SpaceComputation),
}
```

### Step 2: Define Transitions

```go
transitions := map[string]*Transition{
    // Classify: surface → computation (via LLM)
    "t-classify": {
        ID: "t-classify", Kind: NodeKindLLM,
        InputPlaces:  []string{"p-input"},
        OutputPlaces: []string{"p-classified"},
        SystemPrompt: `Classify the user message. Return JSON: {"intent": "greeting"|"question"}`,
        LLMConfig: &LLMConfig{
            Model: "anthropic/claude-haiku-4-5-20251001", MaxTokens: 100,
            RequireJSON: true, SkipHistory: true,
        },
    },

    // Route greetings
    "t-route-greeting": {
        ID: "t-route-greeting", Kind: NodeKindLLM,
        InputPlaces:  []string{"p-greeting"},
        OutputPlaces: []string{"p-response"},
        SystemPrompt: "Respond warmly to the greeting.",
        LLMConfig: &LLMConfig{
            Model: "anthropic/claude-haiku-4-5-20251001", MaxTokens: 200,
            StreamOutput: true,
        },
    },

    // Route questions
    "t-route-question": {
        ID: "t-route-question", Kind: NodeKindLLM,
        InputPlaces:  []string{"p-question"},
        OutputPlaces: []string{"p-response"},
        SystemPrompt: "Answer the question helpfully.",
        LLMConfig: &LLMConfig{
            Model: "anthropic/claude-sonnet-4-20250514", MaxTokens: 2048,
            StreamOutput: true,
        },
    },

    // Validate classification and route
    "t-validate-route": {
        ID: "t-validate-route", Kind: NodeKindValidate,
        InputPlaces:  []string{"p-classified"},
        OutputPlaces: []string{"p-greeting"},  // Default route
        ValidateConfig: &ValidateConfig{
            ValidateFunc: func(payload any) error {
                // Custom routing logic here
                return nil
            },
        },
    },
}
```

### Step 3: Create and Run the CPN

```go
cpn := NewCPN("root", "classifier", 0, ModeMAS, sessionID, places, transitions)
cpn.LLMClient = myLLMClient

// Deposit the user's message as the initial token
inputToken := &Token{
    Color: ColorString, Payload: "Hello, how are you?",
    Space: SpaceSurface, SessionID: sessionID,
    Timestamp: time.Now(),
}
places["p-input"].Deposit(inputToken)

// Run the CPN
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

err := cpn.Run(ctx)
// Result is in p-response
```

### Step 4: Add HITL (Optional)

```go
// Add an approval gate before the response
"t-approve": {
    ID: "t-approve", Kind: NodeKindHITL,
    InputPlaces:  []string{"p-draft-response"},
    OutputPlaces: []string{"p-response"},
    HITLConfig: &HITLConfig{
        Channel:      hitlCh,
        Prompt:       "Review this response before sending.",
        RevisionLoop: true,
        MaxRevisions: 3,
    },
}
```

---

## 20. Complete Working Examples

### Example 1: Minimal CPN in Rust

```rust
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::Mutex;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // Create places
    let mut places = HashMap::new();
    places.insert("input".into(), Arc::new(Place::new(
        "input".into(), ColorSet::String, SpaceKind::Surface
    )));
    places.insert("output".into(), Arc::new(Place::new(
        "output".into(), ColorSet::Artifact, SpaceKind::Surface
    )));

    // Create a tool transition
    let transition = Transition {
        id: "t-echo".into(),
        kind: NodeKind::Tool,
        input_places: vec!["input".into()],
        output_places: vec!["output".into()],
        error_place: None,
        guard: None,
        retry: None,
        config: TransitionConfig::Tool {
            executor: Box::new(|token: Token| {
                let payload = token.payload.downcast_ref::<String>()
                    .unwrap().clone();
                Ok(Token {
                    color: ColorSet::Artifact,
                    payload: Box::new(format!("Echo: {}", payload)),
                    origin_kind: NodeKind::Tool,
                    space: SpaceKind::Surface,
                    ..Default::default()
                })
            }),
        },
    };

    let mut transitions = HashMap::new();
    transitions.insert("t-echo".into(), transition);

    // Deposit initial token
    places.get("input").unwrap().deposit(Token {
        color: ColorSet::String,
        payload: Box::new("Hello, CPN!".to_string()),
        space: SpaceKind::Surface,
        ..Default::default()
    })?;

    // Create and run CPN
    let mut cpn = CPN::new("root", "echo", 0, Mode::MAS, places, transitions);
    cpn.run().await?;

    // Read result
    let result = places.get("output").unwrap().consume()?;
    println!("Result: {:?}", result.payload.downcast_ref::<String>());

    Ok(())
}
```

### Example 2: Concurrent Agent Team in Elixir

Elixir's BEAM VM is a natural fit for CPNs — each place is a GenServer, each transition firing is a Task, and the executor loop is a recursive function in a GenServer.

```elixir
defmodule CPN.Engine do
  @moduledoc """
  The CPN executor loop, implemented as a GenServer.
  Each CPN instance is a process — the BEAM gives us free concurrency.
  """
  use GenServer

  defstruct [:id, :role, :depth, :mode, :state, :places, :transitions,
             :session_id, :llm_client]

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts)
  end

  def run(pid) do
    GenServer.call(pid, :run, :infinity)
  end

  @impl true
  def init(opts) do
    {:ok, struct!(__MODULE__, opts)}
  end

  @impl true
  def handle_call(:run, _from, state) do
    case validate_topology(state) do
      :ok ->
        result = executor_loop(%{state | state: :running})
        {:reply, result, state}
      {:error, _} = err ->
        {:reply, err, %{state | state: :failed}}
    end
  end

  defp executor_loop(%{state: :running} = cpn) do
    firable = collect_firable(cpn)

    case firable do
      [] ->
        if complete?(cpn), do: {:ok, :completed}, else: {:error, :deadlock}

      transitions ->
        # Consume tokens on the caller process (sequential)
        firings = consume_and_prepare(transitions, cpn)

        # Launch each firing as a Task (concurrent, supervised)
        tasks = Enum.map(firings, fn {transition, consumed} ->
          Task.async(fn -> dispatch(transition, cpn, consumed) end)
        end)

        # Await all tasks
        results = Task.await_many(tasks, 30_000)

        # Process results, deposit outputs
        case process_results(results, cpn) do
          {:ok, updated_cpn} -> executor_loop(updated_cpn)
          {:error, _} = err -> err
        end
    end
  end

  defp collect_firable(%{transitions: transitions, places: places}) do
    transitions
    |> Map.values()
    |> Enum.filter(&CPN.Transition.can_fire?(&1, places))
    |> Enum.sort_by(& &1.id)
  end

  defp complete?(%{places: places, transitions: transitions}) do
    terminal_ids = find_terminal_places(places, transitions)
    Enum.all?(terminal_ids, fn id ->
      case CPN.Place.len(id) do
        n when n > 0 -> true
        _ -> false
      end
    end)
  end
end
```

### Example 3: HITL Approval Flow in Go

```go
func buildApprovalCPN(sessionID string, llmClient LLMClient) (*CPN, chan Token) {
    hitlCh := make(chan Token, 1)

    places := map[string]*Place{
        "p-input":    NewPlace("p-input",    ColorString,   SpaceSurface),
        "p-draft":    NewPlace("p-draft",    ColorArtifact, SpaceComputation),
        "p-approved": NewPlace("p-approved", ColorArtifact, SpaceComputation),
        "p-output":   NewPlace("p-output",   ColorArtifact, SpaceSurface),
    }

    transitions := map[string]*Transition{
        "t-draft": {
            ID: "t-draft", Kind: NodeKindLLM,
            InputPlaces:  []string{"p-input"},
            OutputPlaces: []string{"p-draft"},
            SystemPrompt: "Draft a professional email based on the user's request.",
            LLMConfig: &LLMConfig{
                Model: "anthropic/claude-sonnet-4-20250514",
                MaxTokens: 2048, StreamOutput: true,
            },
        },
        "t-approve": {
            ID: "t-approve", Kind: NodeKindHITL,
            InputPlaces:  []string{"p-draft"},
            OutputPlaces: []string{"p-approved"},
            HITLConfig: &HITLConfig{
                Channel:         hitlCh,
                Prompt:          "Review the email draft. Approve, reject, or request revisions.",
                RevisionLoop:    true,
                MaxRevisions:    3,
                CorrectionLLMID: "t-revise",
            },
        },
        "t-revise": {
            ID: "t-revise", Kind: NodeKindLLM,
            InputPlaces:  []string{},  // Used inline by HITL revision loop
            OutputPlaces: []string{},
            SystemPrompt: "Revise the email based on the feedback provided.",
            LLMConfig: &LLMConfig{
                Model: "anthropic/claude-sonnet-4-20250514", MaxTokens: 2048,
            },
        },
        "t-format": {
            ID: "t-format", Kind: NodeKindTool,
            InputPlaces:  []string{"p-approved"},
            OutputPlaces: []string{"p-output"},
            Executor: func(ctx context.Context, in Token) (Token, error) {
                return Token{
                    Color: ColorArtifact,
                    Payload: fmt.Sprintf("✉️ Final Email:\n\n%s", in.Payload),
                }, nil
            },
        },
    }

    cpn := NewCPN("email-agent", "drafter", 0, ModeMAS, sessionID, places, transitions)
    cpn.LLMClient = llmClient

    return cpn, hitlCh
}

// Usage:
// cpn, hitlCh := buildApprovalCPN(sessionID, llmClient)
// go cpn.Run(ctx)
// ... later, when user responds:
// hitlCh <- Token{Color: ColorHuman, Payload: HITLResponse{Action: HITLApprove}}
```

---

## 21. Learning Path and Resources

### For Engineers New to Petri Nets

1. **Start with the math** (Section 2). Draw a few nets on paper. Trace token flow manually.
2. **Implement the five primitives** in your language of choice. Get Deposit, Consume, Peek, CanFire, and the executor loop working with just Tool transitions.
3. **Add LLM transitions.** Wire up an LLM client. Make a classify-then-respond pipeline.
4. **Add HITL.** Build an approval gate. Feel the power of blocking a CPN on human input.
5. **Add Sub-CPNs.** Build a parent that spawns two children and merges their results.
6. **Add Communication Spaces.** Partition your places into surface/observation/computation. Add space validation.
7. **Add the dual-mode system.** Implement centaurianGuard and automatic mode switching.

### Key Academic References

- **Borghoff, Bottoni, Pareschi (2025)** — "Human-Artificial Interaction in the Age of Agentic AI: A System-Theoretical Approach" ([arXiv:2502.14000](https://arxiv.org/abs/2502.14000)). The foundational paper for this architecture.
- **Jensen, K. (1995)** — "Coloured Petri Nets: Basic Concepts, Analysis Methods and Practical Use." The definitive CPN reference.
- **Murata, T. (1989)** — "Petri nets: Properties, analysis and applications." Classic survey paper.
- **Petri, C.A. (1962)** — "Kommunikation mit Automaten." The original dissertation that started it all.
- **Pareschi, R. (2024)** — "Beyond human and machine: An architecture and methodology guideline for centaurian design."
- **Simon, H.A. (1996)** — "The Sciences of the Artificial." The tripartite model (external interface, coding mechanism, internal processing) that maps to our three communication spaces.

### Language-Specific Concurrency Notes

| Language | Best Concurrency Primitive for CPN | Why |
|----------|-----------------------------------|-----|
| **Go** | Goroutines + channels + sync.Mutex | Native concurrency. Each transition fires in a goroutine. Places use mutexes. Event buses are channels. Zero-dependency. |
| **Rust** | Tokio tasks + Arc<Mutex<T>> | Ownership system prevents data races at compile time. `Arc<Mutex<VecDeque<Token>>>` for places. Tokio tasks for transition firing. |
| **Elixir** | GenServer + Task.async | The BEAM VM is a natural CPN executor. Each place is a process. Each firing is a supervised task. OTP gives you fault tolerance for free. |
| **Kotlin** | Coroutines + StateFlow | Structured concurrency maps well to the CPN hierarchy. Flows for event streaming. |
| **Zig** | async/await + Thread pool | Manual memory management gives you ultimate control over token lifecycle. Ideal for embedded CPN engines. |

### Design Principles

1. **One type to rule them all.** The CPN is the only agent type. Identity emerges from topology and depth. Don't create `Agent`, `Tool`, `Coordinator` classes — they're all CPNs.

2. **Consume before launch.** Always consume tokens on the main thread before launching fire goroutines. This prevents double-consumption races.

3. **Space isolation is a feature, not a bug.** The surface→computation barrier prevents raw user input from reaching internal processing without transformation. HITL is the only sanctioned bridge.

4. **LLM is the last resort.** Prefer tools, validation, and routing over LLM calls. The flow ranking formula penalizes LLM-heavy paths.

5. **Events are append-only.** The event log is the source of truth for what happened. Events are never modified or deleted.

6. **The session is the user's only interface.** Users never see CPNs, tokens, or transitions. They see a stream of text chunks and HITL prompts. Everything else is invisible.

7. **Validate at construction, not at runtime.** Topology validation catches arc errors, space violations, and misconfigured HITL transitions before the first token flows.

8. **Non-blocking by default.** Sub-CPNs run asynchronously. Observer draining is non-blocking. SSE fan-out uses non-blocking sends. Slow clients are dropped, never blocking the engine.

---

## Note

This document describes a pattern, not a product. The exact implementation will depend on your language, your LLM provider, your deployment environment, and your specific agent use case. The mathematical foundations are universal. The communication spaces and dual-mode system apply regardless of implementation language. The hexagonal architecture ensures you can swap any adapter without touching the domain core.

The best way to use this document is to pick a language from the examples above, implement Section 19 step by step, and iterate. Start with a single CPN that does classify → respond. Add complexity only when the simple version works. The theory will be there when you need it.

Build small, verify formally, scale horizontally.

---

*This document is based on the Coloured Petri Net engine powering [liwaisi](https://github.com/liwaisi-tech), grounded in the academic framework from [Borghoff, Bottoni & Pareschi (2025)](https://arxiv.org/abs/2502.14000).*
