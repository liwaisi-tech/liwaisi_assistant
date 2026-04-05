---
title: "CPN Execution Monitor — Real-Time Observability for Petri Net Agent Actions"
version: 1.0
date_created: 2026-04-04
owner: liwaisi-tech
tags: [design, cpn, monitoring, observability, real-time, visualization, react, fullstack, sse, cost-tracking]
---

# Introduction

This specification defines a full-stack **real-time CPN Execution Monitor** that enables users to observe every agent action as it happens through live Petri net visualization. The monitor provides token-level observability: users see tokens flowing through places, transitions firing with animated state changes, input/output inspection per transition, per-transition cost attribution, and hierarchical sub-CPN navigation via breadcrumbs.

The system builds on the existing static CPN Execution Visualizer (spec-design-cpn-execution-visualizer.md) by adding **live execution state** streamed via SSE events, **enriched event payloads** with input/output token snapshots, and **per-transition cost attribution**. Chat messages that result from CPN execution are tagged with clickable badges for drill-down into the execution view.

## 1. Purpose & Scope

**Purpose**: Give users an "x-ray view" into the AI agent's reasoning process in real-time. Every CPN execution step — token consumption, transition firing, token production, cost incurred — is visible and inspectable. This is the foundation for building dynamic CPN topologies and monitoring agent behavior.

**Scope**: Full-stack — backend (Go CPN engine, HTTP API, SSE), frontend (React 19, @xyflow/react). Spans:
- CPN domain core: event enrichment, cost attribution, token snapshots
- Application layer: session service event wiring
- Driving adapter: SSE event types, execution trace API enrichment
- Frontend: new Execution Monitor dock app, extended SSE hooks, live graph visualization, chat integration

**Audience**: Go backend engineers, React frontend engineers, CPN theory experts, UX/UI designers.

**Assumptions**:
- CPN topologies are small directed graphs (3-15 nodes in practice)
- SSE connection is already established per session; new event types flow automatically via `SSEBroker.PublishEvent`
- The existing deep-space terminal theme (dark, cyan accent, JetBrains Mono) must be preserved
- The dock-based desktop metaphor uses conditional rendering via `activeApp` state
- `EventRecord.Payload` and `EventRecord.TokenSnapshot` columns (JSONB) already exist in the events table but are underutilized
- Per-transition cost is only non-zero for LLM and Validate (with correction LLM) transitions

## 2. Definitions

| Term | Definition |
|------|-----------|
| **CPN** | Coloured Petri Net — the execution engine for agent workflows |
| **Token** | The fundamental data unit flowing through the CPN. Carries Color, Payload, OriginID, Space, and trace metadata |
| **TokenSnapshot** | A serializable, truncated representation of a Token for event payloads (500-char payload preview) |
| **Transition Firing** | The atomic consume-then-produce operation: input tokens consumed from input places, computation performed, output tokens deposited in output places |
| **In-Flight Token** | A token that has been consumed from its input place but whose transition has not yet completed. Exists in the temporal gap between `consumeAll` and `dispatch` completion |
| **SSE** | Server-Sent Events — unidirectional server-to-client event stream over HTTP |
| **SSE Broker** | Per-session event distributor (`SSEBroker`) that fans out CPN events to all connected clients |
| **EventSink** | Synchronous callback on `CPN.EventSink` invoked by `emit()` — the hook that routes CPN events to the SSE broker |
| **Cost Attribution** | Assigning the `CostUSD` from `LLMResponse` to the specific transition that made the LLM call |
| **Execution Monitor** | The new dock application providing real-time CPN execution visualization |
| **LiveGraph** | The animated topology graph component that reflects real-time execution state |
| **Transition Inspector** | Side panel showing detailed input/output/cost/config for a selected transition |
| **CPN Breadcrumb** | Hierarchical navigation path for nested sub-CPN executions |

## 3. Requirements, Constraints & Guidelines

### Phase 1: Backend Event Enrichment

#### New Event Types

- **REQ-001**: Add `EventTransitionStarted` event type (value: `"transition_started"`) emitted AFTER token consumption and BEFORE dispatch goroutine launch, carrying a `TransitionStartedPayload` with input token snapshots
- **REQ-002**: Add `EventTransitionCompleted` event type (value: `"transition_completed"`) emitted AFTER dispatch returns (success or failure), carrying a `TransitionCompletedPayload` with output token snapshots, cost, duration, and optional error string
- **REQ-003**: The existing `EventTransitionFired` MUST remain for backward compatibility. The new started/completed pair provides richer semantics but does not replace the existing event

#### Token Snapshot

- **REQ-004**: Add a `Snapshot() TokenSnapshot` method to `Token` that produces a serializable representation with `Color` (string), `PayloadPreview` (string, truncated to 500 characters with `"..."` suffix if truncated), `Space` (string), `OriginID` (string), and `OriginKind` (string)
- **REQ-005**: `PayloadPreview` MUST handle all payload types: string (direct truncation), `[]byte` (convert to string), JSON-serializable (marshal then truncate), nil (empty string)

#### Payload Structs

- **REQ-006**: `TransitionStartedPayload` struct:
  ```go
  type TransitionStartedPayload struct {
      InputTokens []TokenSnapshot `json:"input_tokens"`
  }
  ```
- **REQ-007**: `TransitionCompletedPayload` struct:
  ```go
  type TransitionCompletedPayload struct {
      OutputTokens []TokenSnapshot `json:"output_tokens"`
      CostUSD      float64         `json:"cost_usd"`
      DurationMs   int64           `json:"duration_ms"`
      Error        string          `json:"error,omitempty"`
  }
  ```
- **REQ-008**: `TokenSnapshot` struct:
  ```go
  type TokenSnapshot struct {
      Color          string `json:"color"`
      PayloadPreview string `json:"payload_preview"`
      Space          string `json:"space"`
      OriginID       string `json:"origin_id"`
      OriginKind     string `json:"origin_kind"`
  }
  ```

#### Executor Event Emission

- **REQ-009**: In `executor.go`, after `consumeAll` creates the `firing` struct (line ~111), emit `EventTransitionStarted` with:
  - `TransitionID`: transition ID
  - `TransitionKind`: transition Kind
  - `Payload`: `TransitionStartedPayload` with snapshots of consumed tokens
- **REQ-010**: In `executor.go`, inside the fire goroutine (line ~129-138), wrap the `fireWithRetry` + `dispatch` call with timing. After it returns, emit `EventTransitionCompleted` with:
  - `TransitionID`: transition ID
  - `TransitionKind`: transition Kind
  - `Payload`: `TransitionCompletedPayload` with output token snapshots (snapshot output places after dispatch), cost from dispatch result, duration, and error string if dispatch failed
- **CON-001**: `EventTransitionStarted` MUST be emitted on the **main goroutine** (same goroutine that calls consumeAll), NOT inside the fire goroutine. This ensures the event is emitted before any concurrent state changes from the dispatch
- **CON-002**: `EventTransitionCompleted` MUST be emitted inside the **fire goroutine**, after dispatch returns. This captures the actual execution duration and result

#### Dispatch Cost Threading

- **REQ-011**: Modify `dispatch` function signature from `func dispatch(...) error` to `func dispatch(...) (float64, error)` where `float64` is the `CostUSD` incurred by this specific transition
- **REQ-012**: `fireLLM` MUST return `LLMResponse.CostUSD` from the final LLM call (or sum of all calls in tool-call loop)
- **REQ-013**: `fireTool` MUST return `0.0` (tool transitions have no LLM cost)
- **REQ-014**: `fireHITL` and `fireHITLWithRevision` MUST return `0.0` (human actions have no LLM cost, but if revision loop calls `fireLLMDirect`, that cost MUST be included)
- **REQ-015**: `fireSubNet` MUST return `0.0` for the transition itself (child CPN costs are tracked separately via child events)
- **REQ-016**: `fireValidate` MUST return the correction LLM cost if `callCorrectionLLM` was invoked, else `0.0`
- **REQ-017**: Update `fireWithRetry` to propagate the cost from the inner `fire` function: change signature to accept `func() (float64, error)` and return `(float64, error)`

#### Output Token Snapshot Capture

- **REQ-018**: To capture output tokens after dispatch, snapshot each output place's token count BEFORE dispatch (on main goroutine) and AFTER dispatch (in fire goroutine). The delta tokens are the outputs. Alternative: dispatch returns the produced tokens directly
- **PAT-001**: Preferred pattern — each `fire*` function returns the output tokens it produced (via a slice or the deposit operations). This avoids race conditions from snapshotting shared places

### Phase 2: Backend API Enrichment

- **REQ-019**: Enrich `EventResponse` in `response.go` with:
  ```go
  type EventResponse struct {
      ID             string          `json:"id"`
      Type           string          `json:"type"`
      TransitionID   string          `json:"transition_id"`
      TransitionKind string          `json:"transition_kind"`
      CPNID          string          `json:"cpn_id"`
      CPNDepth       int             `json:"cpn_depth"`
      CPNRole        string          `json:"cpn_role"`
      Payload        json.RawMessage `json:"payload,omitempty"`
      TokenSnapshot  json.RawMessage `json:"token_snapshot,omitempty"`
      Timestamp      string          `json:"timestamp"`
  }
  ```
- **REQ-020**: `HandleGetSessionExecution` MUST map `EventRecord.Payload` and `EventRecord.TokenSnapshot` to the enriched `EventResponse` fields
- **CON-003**: No new database tables or migrations are required. The `events` table already stores `token_snapshot` and `payload` as JSONB columns

### Phase 3: Frontend SSE & State Management

#### New SSE Event Types

- **REQ-021**: Add to `types/sse.ts`:
  ```typescript
  interface TokenSnapshotData {
    color: string;
    payload_preview: string;
    space: string;
    origin_id: string;
    origin_kind: string;
  }

  interface TransitionStartedPayload {
    input_tokens: TokenSnapshotData[];
  }

  interface TransitionCompletedPayload {
    output_tokens: TokenSnapshotData[];
    cost_usd: number;
    duration_ms: number;
    error?: string;
  }
  ```
- **REQ-022**: Extend `SSEEventType` union with `'transition_started'` and `'transition_completed'`

#### Extend useSSE Hook

- **REQ-023**: Add new callback options to `UseSSEOptions`:
  ```typescript
  onTransitionStarted?: (data: CPNEventData) => void;
  onTransitionCompleted?: (data: CPNEventData) => void;
  onSubNetStarted?: (data: CPNEventData) => void;
  onSubNetCompleted?: (data: CPNEventData) => void;
  onSubNetFailed?: (data: CPNEventData) => void;
  ```
- **REQ-024**: Register corresponding `es.addEventListener(...)` calls for each new event type in the `connect` function, following the existing pattern (ref-based callbacks, try/catch JSON parsing)

#### Execution Monitor State Hook

- **REQ-025**: Create `useExecutionMonitor.ts` hook with reducer-based state:
  ```typescript
  interface MonitorEvent {
    id: string;
    type: string;
    transitionId: string;
    transitionKind: string;
    cpnId: string;
    cpnDepth: number;
    cpnRole: string;
    payload: unknown;
    timestamp: string;
  }

  interface ExecutionState {
    events: MonitorEvent[];
    activeTransitions: Map<string, TransitionStartedPayload>;
    completedTransitions: Map<string, TransitionCompletedPayload>;
    placeTokenCounts: Record<string, number>;
    totalCostUSD: number;
    cpnHierarchy: CPNHierarchyNode[];
    activeCPNId: string | null;
    topology: CPNTopology | null;
  }
  ```
- **REQ-026**: The hook MUST reduce `transition_started` events by adding the transition to `activeTransitions` (Map key = transitionId)
- **REQ-027**: The hook MUST reduce `transition_completed` events by moving the transition from `activeTransitions` to `completedTransitions`, incrementing `totalCostUSD` by `payload.cost_usd`
- **REQ-028**: The hook MUST reduce `subnet_started` events by appending to `cpnHierarchy` tree
- **REQ-029**: The hook MUST expose a `loadExecutionTrace(sessionId: string)` function that fetches `GET /sessions/{sessionId}/execution` and replays events into state for historical viewing
- **GUD-001**: Use `useReducer` with discriminated union actions for predictable state transitions. Follow the pattern established by `useChat.ts`

### Phase 4: Live Graph Visualization

#### Execution Monitor Component

- **REQ-030**: Create `ExecutionMonitor.tsx` as a new dock application. Add `'monitor'` to the `ActiveApp` type union in `DesktopLayout.tsx`
- **REQ-031**: Add a Monitor icon to `Dock.tsx` (activity/pulse SVG icon) with label "Monitor"
- **REQ-032**: `ExecutionMonitor` layout:
  - **Top bar**: CPNBreadcrumb (left), MetricsSummary (right)
  - **Main area**: LiveGraph (full width/height)
  - **Right panel**: TransitionInspector (collapsible, 320px width)
  - **Bottom bar**: TimelineScrubber (full width, 48px height)
  - **Floating**: CostAccumulator (top-right overlay, always visible)

#### LiveGraph Component

- **REQ-033**: `LiveGraph.tsx` MUST wrap the existing `TopologyGraph` component, injecting real-time state into node data props
- **REQ-034**: `PlaceNode` MUST display a token count badge (top-right corner, rounded circle, accent color) when `tokenCount > 0`. Badge MUST pulse briefly on increment (CSS animation, 0.3s)
- **REQ-035**: `TransitionNode` MUST display three visual states:
  - **Idle**: Default styling (existing)
  - **Firing**: Pulsing glow border animation (cyan for LLM, green for Tool, amber for HITL, purple for Validate). Animation CSS: `@keyframes firing-pulse { 0%, 100% { box-shadow: 0 0 4px color; } 50% { box-shadow: 0 0 16px color; } }` with 0.8s duration
  - **Completed**: Brief green flash (0.5s), then return to idle with execution order badge
  - **Failed**: Red glow (persistent until next execution)
- **REQ-036**: `TransitionNode` MUST display a cost micro-badge (bottom-right, `$0.003` format) when `costUSD > 0`
- **REQ-037**: Edges from input places to a firing transition MUST animate with `animated: true` (existing @xyflow prop) during the firing window (between `transition_started` and `transition_completed`)
- **GUD-002**: Batch SSE event updates with `requestAnimationFrame` or a 50ms debounce to prevent render thrashing from concurrent transition firings

#### TransitionInspector Component

- **REQ-038**: Clicking a transition node in LiveGraph MUST open the TransitionInspector side panel
- **REQ-039**: The inspector MUST display:
  - **Header**: Transition ID, Kind badge (color-coded), execution status
  - **Input Tokens section**: List of `TokenSnapshotData` from `transition_started` event — each showing color badge, payload preview (scrollable, monospace), space label, origin
  - **Output Tokens section**: List of `TokenSnapshotData` from `transition_completed` event — same format
  - **Cost & Duration**: `$0.0034` and `245ms` with visual bar
  - **Config section**: LLM config (model, temperature, max tokens), guard function name, system prompt (expandable), HITL config if applicable
  - **Error section**: Red-highlighted error message if `transition_completed.error` is non-empty
- **REQ-040**: The inspector MUST show a loading/pending state while the transition is firing (between started and completed events)

#### TimelineScrubber Component

- **REQ-041**: Horizontal bar showing events as colored dots (color by event type: cyan for started, green for completed, red for failed, amber for HITL)
- **REQ-042**: Events are positioned proportionally by timestamp along the timeline
- **REQ-043**: Hovering an event dot MUST show a tooltip with event type, transition ID, and timestamp
- **REQ-044**: For historical replay (loaded via `loadExecutionTrace`), the scrubber MUST support dragging to any point, replaying events up to that timestamp
- **GUD-003**: Timeline should auto-scroll to keep the latest event visible during live execution

#### CostAccumulator Component

- **REQ-045**: Floating overlay (top-right of LiveGraph area) displaying:
  - Total execution cost (`$0.0142`)
  - Number of LLM calls
  - Number of transitions fired
- **REQ-046**: Cost MUST increment in real-time as `transition_completed` events arrive with non-zero cost
- **REQ-047**: Visual format: monospace font, compact layout, semi-transparent background (`rgba(10,10,15,0.85)`)

#### CPNBreadcrumb Component

- **REQ-048**: Render breadcrumb path: `Root CPN > SubNet: planner > SubNet: worker`
- **REQ-049**: Each breadcrumb segment MUST be clickable, switching `activeCPNId` to navigate the CPN hierarchy
- **REQ-050**: Active (current) segment MUST be highlighted (accent color), parent segments styled as links
- **REQ-051**: Breadcrumb tree MUST be built from `subnet_started` events (parent CPNID → child CPNID/Role mapping)

#### MetricsSummary Component

- **REQ-052**: Compact stats row showing: Transitions Fired (count), LLM Calls (count), Total Cost (`$X.XXXX`), Duration (`Xs`)
- **REQ-053**: Values MUST update in real-time from `useExecutionMonitor` state

### Phase 5: Chat Integration

- **REQ-054**: Assistant messages in `MessageBubble.tsx` that have a non-empty `cpnId` MUST display a clickable "CPN" badge
- **REQ-055**: The badge MUST show the CPN role label (e.g., "classifier", "planner") from `cpnRole`
- **REQ-056**: Clicking the badge MUST switch `activeApp` to `'monitor'` and load the execution trace for the current session
- **REQ-057**: The badge styling: monospace font, 9px, border with kind-color, rounded-full padding, hover glow effect
- **GUD-004**: Do not show the CPN badge on messages during active streaming (wait for `Done` sentinel)

### Phase 6: Sub-CPN Navigation

- **REQ-058**: Clicking a `SubNet` transition node in LiveGraph MUST navigate into the child CPN's topology, updating the breadcrumb
- **REQ-059**: The child CPN's topology MUST be available from either:
  - (a) The `subnet_started` event payload containing the child topology, OR
  - (b) A `GET /flows/{hash}` API call if the child topology was crystallized
- **REQ-060**: Navigating into a child CPN MUST filter events in `useExecutionMonitor` to show only events where `cpnId` matches the child CPN ID
- **REQ-061**: The back button (or breadcrumb parent click) MUST restore the parent CPN view with its full event set

### Security & Performance Constraints

- **SEC-001**: Token payload previews MUST NOT exceed 500 characters. Full payloads are never sent via SSE — only via the execution trace API on-demand
- **SEC-002**: The `TokenSnapshot.PayloadPreview` field MUST sanitize content to prevent XSS (the existing `rehype-sanitize` in MarkdownContent handles rendering, but raw previews in the inspector MUST use text-only rendering, no HTML interpretation)
- **CON-004**: The SSE broker drops events if the client buffer (128 capacity) is full. The monitor MUST handle gaps gracefully — a missing `transition_started` for a completed transition should show "completed" state without the input token data
- **CON-005**: `EventTransitionStarted` emission on the main goroutine MUST NOT block or slow the firing loop. The `emit()` function is already non-blocking on the EventEmitter channel; the EventSink callback must remain fast
- **CON-006**: Animation frame rate for the LiveGraph MUST stay above 30fps even with 10+ concurrent transition firings. Use CSS animations (GPU-composited) rather than JavaScript-driven animation
- **CON-007**: The `dispatch` signature change (`error` → `(float64, error)`) is a breaking change within the `cpn` package. All callers of `dispatch` and all `fire*` functions MUST be updated atomically

## 4. Interfaces & Data Contracts

### Backend: New Event Payloads (Go)

```go
// cpn/event.go — New event types
const (
    EventTransitionStarted   EventType = "transition_started"
    EventTransitionCompleted EventType = "transition_completed"
)

// cpn/event.go — New payload structs
type TransitionStartedPayload struct {
    InputTokens []TokenSnapshot `json:"input_tokens"`
}

type TransitionCompletedPayload struct {
    OutputTokens []TokenSnapshot `json:"output_tokens"`
    CostUSD      float64         `json:"cost_usd"`
    DurationMs   int64           `json:"duration_ms"`
    Error        string          `json:"error,omitempty"`
}

type TokenSnapshot struct {
    Color          string `json:"color"`
    PayloadPreview string `json:"payload_preview"`
    Space          string `json:"space"`
    OriginID       string `json:"origin_id"`
    OriginKind     string `json:"origin_kind"`
}
```

### Backend: Modified Dispatch Signature (Go)

```go
// cpn/executor.go — Before
func dispatch(ctx context.Context, t *Transition, c *CPN, consumed []Token) error

// cpn/executor.go — After
func dispatch(ctx context.Context, t *Transition, c *CPN, consumed []Token) (float64, error)

// Each fire* function changes accordingly:
func fireLLM(ctx context.Context, t *Transition, c *CPN, consumed []Token) (float64, error)
func fireTool(ctx context.Context, t *Transition, c *CPN, consumed []Token) (float64, error)
func fireSubNet(ctx context.Context, t *Transition, parent *CPN, consumed []Token) (float64, error)
func fireHITL(ctx context.Context, t *Transition, c *CPN, _ []Token) (float64, error)
func fireHITLWithRevision(ctx context.Context, t *Transition, c *CPN, _ []Token) (float64, error)
func fireValidate(ctx context.Context, t *Transition, c *CPN, consumed []Token) (float64, error)

// fireWithRetry signature change:
func fireWithRetry(ctx context.Context, retry *RetryPolicy, cb *CircuitBreakerState,
    fire func() (float64, error)) (float64, error)
```

### Backend: Enriched EventResponse (HTTP API)

```go
// internal/driving/httpapi/response.go
type EventResponse struct {
    ID             string          `json:"id"`
    Type           string          `json:"type"`
    TransitionID   string          `json:"transition_id"`
    TransitionKind string          `json:"transition_kind"`
    CPNID          string          `json:"cpn_id"`
    CPNDepth       int             `json:"cpn_depth"`
    CPNRole        string          `json:"cpn_role"`
    Payload        json.RawMessage `json:"payload,omitempty"`
    TokenSnapshot  json.RawMessage `json:"token_snapshot,omitempty"`
    Timestamp      string          `json:"timestamp"`
}
```

### Frontend: SSE Event Types (TypeScript)

```typescript
// types/sse.ts
interface TokenSnapshotData {
  color: string;
  payload_preview: string;
  space: string;
  origin_id: string;
  origin_kind: string;
}

interface TransitionStartedPayload {
  input_tokens: TokenSnapshotData[];
}

interface TransitionCompletedPayload {
  output_tokens: TokenSnapshotData[];
  cost_usd: number;
  duration_ms: number;
  error?: string;
}

// Extended SSEEventType
type SSEEventType =
  | 'stream_chunk'
  | 'transition_fired'
  | 'transition_started'    // NEW
  | 'transition_completed'  // NEW
  | 'subnet_started'
  | 'subnet_completed'
  | 'subnet_failed'
  | 'hitl_requested'
  | 'hitl_resolved'
  | 'mode_switch'
  | 'session_completed'
  | 'session_failed';
```

### Frontend: Execution Monitor State (TypeScript)

```typescript
// hooks/useExecutionMonitor.ts
interface CPNHierarchyNode {
  cpnId: string;
  cpnRole: string;
  cpnDepth: number;
  parentCPNId: string | null;
  children: CPNHierarchyNode[];
}

interface ExecutionState {
  events: MonitorEvent[];
  activeTransitions: Map<string, TransitionStartedPayload>;
  completedTransitions: Map<string, TransitionCompletedPayload>;
  placeTokenCounts: Record<string, number>;
  totalCostUSD: number;
  totalTransitionsFired: number;
  totalLLMCalls: number;
  cpnHierarchy: CPNHierarchyNode[];
  activeCPNId: string | null;
  topology: CPNTopology | null;
  selectedTransitionId: string | null;
}

type MonitorAction =
  | { type: 'TRANSITION_STARTED'; event: MonitorEvent }
  | { type: 'TRANSITION_COMPLETED'; event: MonitorEvent }
  | { type: 'SUBNET_STARTED'; event: MonitorEvent }
  | { type: 'SUBNET_COMPLETED'; event: MonitorEvent }
  | { type: 'LOAD_TOPOLOGY'; topology: CPNTopology }
  | { type: 'LOAD_TRACE'; events: MonitorEvent[] }
  | { type: 'SELECT_TRANSITION'; transitionId: string | null }
  | { type: 'NAVIGATE_CPN'; cpnId: string }
  | { type: 'RESET' };
```

### Frontend: Component Props (TypeScript)

```typescript
// features/execution-monitor/ExecutionMonitor.tsx
interface ExecutionMonitorProps {
  sessionId: string | null;
  onClose?: () => void;
}

// features/execution-monitor/LiveGraph.tsx
interface LiveGraphProps {
  topology: CPNTopology;
  activeTransitions: Map<string, TransitionStartedPayload>;
  completedTransitions: Map<string, TransitionCompletedPayload>;
  placeTokenCounts: Record<string, number>;
  onSelectTransition: (transitionId: string) => void;
}

// features/execution-monitor/TransitionInspector.tsx
interface TransitionInspectorProps {
  transitionId: string;
  topology: CPNTopology;
  startedPayload?: TransitionStartedPayload;
  completedPayload?: TransitionCompletedPayload;
  onClose: () => void;
}

// features/execution-monitor/TimelineScrubber.tsx
interface TimelineScrubberProps {
  events: MonitorEvent[];
  currentTime?: string;
  onScrub?: (timestamp: string) => void;
}

// features/execution-monitor/CostAccumulator.tsx
interface CostAccumulatorProps {
  totalCostUSD: number;
  llmCalls: number;
  transitionsFired: number;
}

// features/execution-monitor/CPNBreadcrumb.tsx
interface CPNBreadcrumbProps {
  hierarchy: CPNHierarchyNode[];
  activeCPNId: string | null;
  onNavigate: (cpnId: string) => void;
}

// features/execution-monitor/MetricsSummary.tsx
interface MetricsSummaryProps {
  transitionsFired: number;
  llmCalls: number;
  totalCostUSD: number;
  elapsedMs: number;
}
```

## 5. Acceptance Criteria

### Backend Event Enrichment

- **AC-001**: Given a CPN with 1 LLM transition (p-input → t-llm → p-output), When a token is deposited in p-input and Run() executes, Then the EventSink receives (in order): `transition_started` (with 1 input token snapshot), then `transition_completed` (with 1 output token snapshot, non-zero cost, non-zero duration)
- **AC-002**: Given a CPN with 2 concurrent transitions, When both fire in the same round, Then each transition gets its own `transition_started` and `transition_completed` events with correct transition IDs
- **AC-003**: Given a `fireLLM` call that returns `LLMResponse.CostUSD = 0.0034`, When `transition_completed` is emitted, Then `Payload.CostUSD == 0.0034`
- **AC-004**: Given a token with payload longer than 500 characters, When `Snapshot()` is called, Then `PayloadPreview` is exactly 500 chars + `"..."`
- **AC-005**: Given a `fireTool` execution, When `transition_completed` is emitted, Then `Payload.CostUSD == 0.0`

### Backend API

- **AC-006**: Given a session with enriched events persisted, When `GET /sessions/{id}/execution` is called, Then the response includes `payload` and `token_snapshot` fields as JSON objects (not null)

### Frontend SSE

- **AC-007**: Given the SSE connection is active, When the backend emits `transition_started`, Then `useSSE.onTransitionStarted` callback fires with parsed `CPNEventData`
- **AC-008**: Given the SSE connection is active, When the backend emits `transition_completed`, Then `useSSE.onTransitionCompleted` callback fires with parsed `CPNEventData`

### Frontend LiveGraph

- **AC-009**: Given the monitor is open during a CPN execution, When `transition_started` arrives for transition T, Then T's node displays the firing animation (pulsing glow)
- **AC-010**: Given T is firing, When `transition_completed` arrives for T, Then T's node shows a brief green flash and displays the cost badge
- **AC-011**: Given place P has 3 tokens, Then P's node displays a badge with "3"
- **AC-012**: Given a `transition_completed` with `error` field non-empty, Then T's node displays persistent red glow

### Frontend TransitionInspector

- **AC-013**: Given the user clicks transition T in the LiveGraph, Then the TransitionInspector opens showing T's config, and after T completes, shows input tokens, output tokens, cost, and duration
- **AC-014**: Given T has `transition_started` but not yet `transition_completed`, Then the inspector shows input tokens and a "Firing..." loading indicator

### Frontend Chat Integration

- **AC-015**: Given an assistant message with `cpnId = "abc-123"` and `cpnRole = "planner"`, Then a "CPN planner" badge appears on the message
- **AC-016**: Given the user clicks the CPN badge, Then the dock switches to the Monitor app and loads the execution trace for that session

### Frontend Sub-CPN Navigation

- **AC-017**: Given a CPN with a SubNet transition that spawned child CPN "child-1", When the user clicks the SubNet node, Then the monitor navigates to "child-1" and the breadcrumb shows "Root > child-1"
- **AC-018**: Given the breadcrumb shows "Root > child-1", When the user clicks "Root", Then the monitor navigates back to the root CPN view

## 6. Test Automation Strategy

### Test Levels

| Level | Scope | Framework |
|-------|-------|-----------|
| Unit (Go) | Token.Snapshot(), payload structs, dispatch cost return | `go test` with table-driven tests |
| Unit (TS) | useExecutionMonitor reducer, MonitorAction dispatch | Vitest with React Testing Library |
| Integration (Go) | Executor emits started/completed events with correct payloads | `go test` with in-memory CPN |
| Integration (Go) | EventResponse includes enriched fields | `go test` with HTTP test server |
| Component (TS) | PlaceNode badge, TransitionNode states, TransitionInspector | Vitest + React Testing Library |
| E2E | Full flow: send message → monitor shows live firing → inspect transition | Manual / Playwright |

### Backend Test Cases

```go
// cpn/token_test.go
func TestToken_Snapshot_TruncatesLongPayload(t *testing.T)
func TestToken_Snapshot_HandlesNilPayload(t *testing.T)
func TestToken_Snapshot_HandlesJSONPayload(t *testing.T)

// cpn/executor_test.go
func TestRun_EmitsTransitionStartedBeforeDispatch(t *testing.T)
func TestRun_EmitsTransitionCompletedAfterDispatch(t *testing.T)
func TestRun_TransitionCompletedContainsCost(t *testing.T)
func TestRun_ConcurrentFiringsEmitSeparateEvents(t *testing.T)

// cpn/fire_llm_test.go
func TestFireLLM_ReturnsCostFromResponse(t *testing.T)

// cpn/fire_tool_test.go
func TestFireTool_ReturnsZeroCost(t *testing.T)
```

### Frontend Test Cases

```typescript
// hooks/useExecutionMonitor.test.ts
describe('monitorReducer', () => {
  it('adds transition to activeTransitions on TRANSITION_STARTED')
  it('moves transition to completedTransitions on TRANSITION_COMPLETED')
  it('increments totalCostUSD on TRANSITION_COMPLETED')
  it('builds cpnHierarchy from SUBNET_STARTED events')
  it('replays historical events via LOAD_TRACE')
})

// features/cpn-visualizer/nodes/PlaceNode.test.tsx
describe('PlaceNode', () => {
  it('renders token count badge when tokenCount > 0')
  it('does not render badge when tokenCount is 0 or undefined')
})

// features/cpn-visualizer/nodes/TransitionNode.test.tsx
describe('TransitionNode', () => {
  it('applies firing-pulse class when isFiring is true')
  it('renders cost badge when costUSD > 0')
})
```

### Coverage Requirements

- Backend (Go): 80%+ coverage on modified files (event.go, executor.go, fire_*.go, token.go)
- Frontend (TS): 70%+ coverage on new hooks and reducer logic

## 7. Rationale & Context

### Why enriched events instead of polling?

The CPN engine already has an event-driven architecture with `emit()` and `EventSink`. Adding payload data to events is the natural extension — it avoids polling overhead, maintains the append-only event store contract, and leverages the existing SSE broker for delivery. The alternative (polling CPN state) would require exposing mutable CPN internals and introduce race conditions with the firing loop.

### Why a transition_started/completed pair?

A single `transition_fired` event cannot capture the temporal extent of a transition firing. LLM transitions may take 2-10 seconds. The monitor needs to show a "firing" state during this window. The started/completed pair provides:
1. A clear "in-progress" signal for the UI
2. Accurate duration measurement (completed.timestamp - started.timestamp)
3. Separation of input data (at start) and output data + cost (at completion)

### Why truncate token payloads to 500 chars?

LLM responses can be thousands of characters. Sending full payloads over SSE for every transition would:
1. Overwhelm the SSE broker buffer (128 events)
2. Bloat the events table JSONB columns
3. Slow down frontend rendering
The 500-char preview gives enough context for monitoring; full payloads are available on-demand via the execution trace API.

### Why modify dispatch signature instead of using a side channel?

Threading cost through the return value is explicit, type-safe, and testable. Alternatives considered:
- **Context value**: Hidden coupling, easy to forget to read
- **CPN field mutation**: Race condition with concurrent firings
- **Channel**: Overly complex for a simple float64 value
The `(float64, error)` return is Go-idiomatic and makes the cost flow visible in the code.

### Virtual Expert Team Rationale

| Expert | Why This Role |
|--------|--------------|
| **CPN Theorist** (Kurt Jensen) | The monitor must respect CPN firing semantics — the temporal gap between consumeAll (tokens removed from places) and dispatch completion (tokens deposited in output places) creates "in-flight" tokens. The monitor must distinguish these three states: in-place, in-flight, deposited |
| **Distributed Systems Engineer** (Cindy Sridharan) | SSE event ordering, cost aggregation across sub-CPN boundaries, append-only event store integrity, and handling event drops from broker buffer overflow |
| **Visualization Specialist** (Mike Bostock) | Real-time graph animation on @xyflow/react — handling bursts of concurrent events, GPU-composited CSS animations, debouncing renders |
| **UX Designer** (Bret Victor) | Non-intrusive monitoring — the chat remains primary, the monitor is a secondary "x-ray" view. Drill-down from chat badges, breadcrumb navigation for hierarchy |

## 8. Dependencies & External Integrations

### Internal Dependencies

- **INT-001**: CPN Engine (`cpn/`) — Event emission via `emit()`, `EventSink` callback, `Token`, `Transition`, `Place` types
- **INT-002**: SSE Broker (`httpapi/sse_broker.go`) — `PublishEvent()` already serializes any `cpn.Event` to JSON and fans out to clients. No changes needed
- **INT-003**: Event Repository (`persist.EventRepository`) — `Append()` stores events with `Payload` and `TokenSnapshot` as `json.RawMessage`. Already supports the enriched data
- **INT-004**: Existing CPN Visualizer (`cpn-visualizer/`) — `TopologyGraph`, `PlaceNode`, `TransitionNode` components are reused and extended

### Technology Platform Dependencies

- **PLT-001**: Go 1.25+ — Required for existing codebase compatibility
- **PLT-002**: React 19 — Required for `useDeferredValue`, concurrent features
- **PLT-003**: @xyflow/react 12.10.2 — Graph visualization library (already installed)
- **PLT-004**: @dagrejs/dagre 3.0.0 — Graph layout algorithm (already installed)
- **PLT-005**: Tailwind CSS 4.0 — Styling framework (already installed)

### No New External Dependencies

This feature requires no new npm packages or Go modules. All visualization, animation, and state management is built with existing dependencies.

## 9. Examples & Edge Cases

### Example: SSE Event Sequence for a Simple LLM Execution

```
p-input → t-llm → p-output
```

1. User sends message → token deposited in p-input
2. Executor main loop collects firable: [t-llm]
3. consumeAll removes token from p-input
4. **SSE: transition_started**
   ```json
   {
     "event": "transition_started",
     "data": {
       "ID": "evt-001",
       "Type": "transition_started",
       "TransitionID": "t-llm",
       "TransitionKind": "llm",
       "CPNID": "cpn-root",
       "CPNDepth": 0,
       "CPNRole": "assistant",
       "Payload": {
         "input_tokens": [{
           "color": "STRING",
           "payload_preview": "What is the capital of France?",
           "space": "surface",
           "origin_id": "",
           "origin_kind": ""
         }]
       },
       "Timestamp": "2026-04-04T14:30:00.123Z"
     }
   }
   ```
5. LLM call executes (2.3 seconds, $0.0034)
6. Result deposited in p-output
7. **SSE: transition_completed**
   ```json
   {
     "event": "transition_completed",
     "data": {
       "ID": "evt-002",
       "Type": "transition_completed",
       "TransitionID": "t-llm",
       "TransitionKind": "llm",
       "CPNID": "cpn-root",
       "CPNDepth": 0,
       "CPNRole": "assistant",
       "Payload": {
         "output_tokens": [{
           "color": "STRING",
           "payload_preview": "The capital of France is Paris. Paris is the largest city...",
           "space": "surface",
           "origin_id": "cpn-root",
           "origin_kind": "llm"
         }],
         "cost_usd": 0.0034,
         "duration_ms": 2300
       },
       "Timestamp": "2026-04-04T14:30:02.423Z"
     }
   }
   ```

### Edge Case: Concurrent Firings

Given unified topology with t-classify producing a token that enables both t-direct (guard: conversation) and t-plan (guard: task):
- Only ONE guard evaluates to true, so only one fires
- If both guards returned true (hypothetical), both would get separate started/completed event pairs
- The monitor handles this by mapping events by transitionId (unique key)

### Edge Case: Transition Failure with ErrorPlace

```
t-llm fails → error token deposited in p-error → t-retry fires
```

1. **SSE: transition_started** for t-llm (input tokens)
2. **SSE: transition_completed** for t-llm (error field set, cost incurred, no output tokens)
3. Error token deposited in p-error
4. **SSE: transition_started** for t-retry
5. **SSE: transition_completed** for t-retry (success)

The monitor shows t-llm with red glow and error badge, t-retry with green flash.

### Edge Case: SSE Buffer Overflow

If 128+ events queue before the client reads them:
- Broker drops events with warning log
- Monitor may receive `transition_completed` without prior `transition_started`
- Monitor MUST handle this: show completed state without input token data
- The `activeTransitions` map won't have the entry; the reducer adds directly to `completedTransitions`

### Edge Case: Sub-CPN Cost Attribution

```
Root: p-input → t-subnet → p-output
Child: p-child-in → t-child-llm ($0.005) → p-child-out
```

- t-subnet's `transition_completed.cost_usd = 0.0` (subnet transition itself is free)
- t-child-llm's `transition_completed.cost_usd = 0.005` (emitted by child CPN)
- The monitor's `totalCostUSD` sums ALL completed events across ALL CPNs in the hierarchy

### Edge Case: Token Payload Types

```go
// String payload
token := &Token{Color: ColorString, Payload: "Hello world"}
snap := token.Snapshot() // PayloadPreview: "Hello world"

// JSON payload
token := &Token{Color: ColorJSON, Payload: map[string]any{"intent": "greet"}}
snap := token.Snapshot() // PayloadPreview: `{"intent":"greet"}`

// Nil payload
token := &Token{Color: ColorEvent, Payload: nil}
snap := token.Snapshot() // PayloadPreview: ""

// Long payload (600 chars)
token := &Token{Color: ColorString, Payload: strings.Repeat("x", 600)}
snap := token.Snapshot() // PayloadPreview: "xxx...xxx..." (500 chars + "...")
```

## 10. Validation Criteria

| # | Criterion | How to Validate |
|---|-----------|----------------|
| V-001 | `EventTransitionStarted` emitted before dispatch | Unit test with event collector: started timestamp < completed timestamp |
| V-002 | `EventTransitionCompleted` contains accurate cost | Mock LLMClient returning known CostUSD, assert in completed payload |
| V-003 | `TokenSnapshot.PayloadPreview` never exceeds 503 chars (500 + "...") | Property test with random payload sizes |
| V-004 | All `fire*` functions return `(float64, error)` | Compile-time check (Go type system) |
| V-005 | `EventResponse` includes `Payload` field | Integration test: POST message → GET execution → assert payload present |
| V-006 | LiveGraph shows firing animation | Manual test: open monitor, send message, observe transition glow |
| V-007 | TransitionInspector shows input/output tokens | Manual test: click transition after completion, verify token list |
| V-008 | CPN badge appears on assistant messages | Manual test: send message in chat, verify badge on response |
| V-009 | Cost accumulator updates in real-time | Manual test: observe cost incrementing during LLM execution |
| V-010 | Breadcrumb navigation works for sub-CPNs | Manual test with topology containing SubNet transition |

## 11. Related Specifications / Further Reading

- [spec-design-cpn-execution-visualizer.md](spec-design-cpn-execution-visualizer.md) — Static CPN visualization (predecessor to this spec)
- [spec-architecture-block20-llm-streaming.md](spec-architecture-block20-llm-streaming.md) — SSE streaming architecture
- [.docs/specs/agentic-cpn-v1.3.md](../.docs/specs/agentic-cpn-v1.3.md) — Persistence layer specification
- [.docs/specs/agentic-cpn-v1.2.md](../.docs/specs/agentic-cpn-v1.2.md) — CPN engine specification
- Jensen, K. (1997). *Coloured Petri Nets: Basic Concepts, Analysis Methods and Practical Use*. Springer-Verlag
