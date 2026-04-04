---
title: "CPN Execution Visualizer & Persistence"
version: 1.0
date_created: 2026-04-04
owner: liwaisi-tech
tags: [design, cpn, visualization, persistence, react, fullstack, flows, execution-records]
---

# Introduction

This specification defines a full-stack feature to **persist executed CPN topologies** to the database and **visualize them interactively** from the React frontend. Users will be able to browse saved flows, inspect topology graphs (places, transitions, arcs), replay execution traces (which transitions fired, token flow, timing, costs), and evaluate CPN performance across sessions.

The backend infrastructure (FuncRegistry, MarshalCPN, FlowRepository, IntelligenceRepository, MetricsRecorder) is fully implemented but not wired. The frontend has no CPN visualization. This spec closes both gaps.

## 1. Purpose & Scope

**Purpose**: Enable users to visually inspect, evaluate, and replay any CPN that was dynamically built and executed by the system. Persist every CPN execution as a recoverable topology with execution metrics, and render it as an interactive directed graph in the browser.

**Scope**: Full-stack — backend (Go), frontend (React/TypeScript), database (PostgreSQL), and infrastructure (Docker). Spans four backend layers (domain, application, driving, composition root) and two frontend feature modules (flow-browser, cpn-visualizer).

**Audience**: Go backend engineers, React frontend engineers, UX/UI designers, QA automation engineers.

**Assumptions**:
- CPN topologies are small directed graphs (3-15 nodes in practice)
- The unified topology factory is the primary topology in production
- The existing deep-space terminal theme (dark, cyan accent, JetBrains Mono) must be preserved
- The dock-based desktop metaphor uses conditional rendering, not URL routing
- The events table already contains execution trace data (stream_chunk, transition_fired events)

## 2. Definitions

| Term | Definition |
|------|-----------|
| **CPN** | Coloured Petri Net — the execution engine for agent workflows |
| **Flow** | A crystallized CPN topology stored in the `flows` table, identified by a deterministic SHA-256 hash |
| **Topology** | The structural blueprint of a CPN: places, transitions, arcs, configs. Serialized as `CPNTopology` JSON |
| **Execution Record** | Metrics from a single CPN run: transitions fired, LLM calls, cost, duration, success |
| **FuncRegistry** | Bidirectional name↔closure mapping enabling topology serialization of guards/executors |
| **MarshalCPN** | Serializes a live CPN into a `CPNTopology` struct for JSON storage |
| **UnmarshalCPN** | Reconstructs a live CPN from a `CPNTopology` struct |
| **TopologyHash** | Deterministic SHA-256 hash of a topology for deduplication |
| **MetricsRecorder** | In-memory collector for execution metrics (transitions fired, LLM calls, etc.) |
| **React Flow** | `@xyflow/react` — React library for interactive node-based graph UIs |
| **Execution Replay** | Overlaying execution data (fired transitions, timing, tokens) on a topology graph |
| **Dock** | The bottom navigation bar in the desktop layout with app icons |

## 3. Requirements, Constraints & Guidelines

### Phase 1: Backend Persistence Wiring

- **REQ-001**: All guard and executor functions in topology factories (`cmd/server/topologies.go`) MUST be registered in a `FuncRegistry` by name, extracting anonymous closures into named package-level functions
- **REQ-002**: `SessionService.CreateSession` MUST set `root.Metrics = cpn.NewMetricsRecorder()` on every CPN to enable execution tracking
- **REQ-003**: `persistFlow` in `session_service.go` MUST receive the wired `FuncRegistry` via `PersistDeps.FuncRegistry` and successfully call `MarshalCPN` → `TopologyHash` → `FlowRepository.Save`
- **REQ-004**: After each CPN run, `persistAfterRun` MUST persist `ExecutionRecord`s from `MetricsRecorder.Records()` to `IntelligenceRepository.RecordExecution`
- **REQ-005**: The session's `flow_hash` (which topology was used) MUST be tracked. Add a `flow_hash TEXT` column to the sessions table via migration 006
- **REQ-006**: `FlowRepository.Save` uses upsert semantics — duplicate hashes update `topology_json` and `updated_at` but preserve `created_at` and accumulate stats

### Phase 2: Backend API Endpoints

- **REQ-007**: `GET /api/v1/flows` MUST return a paginated list of saved flows with stats (hash, role, execution_count, success_rate, avg_cost_usd, avg_duration_ms, created_at)
- **REQ-008**: `GET /api/v1/flows/{hash}` MUST return the full flow record including `topology_json` as parsed JSON (not raw string)
- **REQ-009**: `GET /api/v1/flows/{hash}/executions` MUST return execution records for a flow, joined via `cpn_role` matching
- **REQ-010**: `GET /api/v1/sessions/{id}/topology` MUST return the CPN topology used by a specific session (derived from `flow_hash` on the session record)
- **REQ-011**: `GET /api/v1/sessions/{id}/execution` MUST return the execution trace: events ordered by timestamp, with transition_id and payload
- **REQ-012**: All new endpoints MUST follow existing patterns: `writeJSON` for responses, `writeError` for errors, standard error codes (400, 404, 500)

### Phase 3: Frontend Flow Browser

- **REQ-013**: A "Flows" icon in the Dock MUST be activatable, switching the main content area from Chat to the Flow Browser view via an `activeApp` state in `DesktopLayout`
- **REQ-014**: The Flow Browser MUST display a table/list of saved flows with columns: Role, Executions, Success Rate, Avg Cost, Last Updated
- **REQ-015**: Clicking a flow MUST navigate to the Flow Detail view showing the interactive topology graph
- **REQ-016**: The Flow Browser MUST fetch data from `GET /api/v1/flows` with loading and empty states

### Phase 4: Frontend CPN Topology Visualizer

- **REQ-017**: The topology graph MUST render Places as circles and Transitions as rounded rectangles, connected by directed arcs (edges)
- **REQ-018**: Place nodes MUST display: ID, ColorSet (as fill color), SpaceKind (as border style)
- **REQ-019**: Transition nodes MUST display: ID, NodeKind (as icon), model name (if LLM), guard name (if guarded), system prompt preview (truncated)
- **REQ-020**: The graph MUST be zoomable (mouse wheel), pannable (drag), and auto-layout using dagre (top-to-bottom direction)
- **REQ-021**: Hovering a transition node MUST show a detail tooltip with full LLMConfig, guard function name, retry policy, and HITL config if applicable
- **REQ-022**: The visualizer MUST use React Flow (`@xyflow/react`) for graph rendering with custom node components

### Phase 4b: Execution Replay Overlay

- **REQ-023**: When viewing a flow with execution data, the user MUST be able to select an execution (session) from a dropdown
- **REQ-024**: Fired transitions MUST be highlighted (glow border, color change) with execution order numbers
- **REQ-025**: An execution timeline MUST show events chronologically with transition labels, durations, and costs
- **REQ-026**: Clicking a transition in the graph MUST show the event details: payload, token snapshot, LLM content (truncated), cost, duration
- **REQ-027**: Non-fired transitions (e.g., `t-plan` when classifier routed to `t-direct`) MUST be visually dimmed
- **REQ-028**: The total execution summary (duration, cost, tokens, LLM calls) MUST be displayed in a stats bar above the graph

### Security Requirements

- **SEC-001**: New API endpoints MUST respect the same CORS policy as existing endpoints
- **SEC-002**: `topology_json` MUST NOT expose system prompts in full to unauthenticated users (truncate to 100 chars in list view)
- **SEC-003**: Flow deletion (soft-delete) MUST only be accessible to authenticated users

### Constraints

- **CON-001**: The frontend MUST NOT use a routing library — navigation between Chat and Flows uses conditional rendering via `activeApp` state in `DesktopLayout`
- **CON-002**: The deep-space terminal theme (dark bg, cyan accent, JetBrains Mono) MUST be preserved for all new components
- **CON-003**: CPN topologies in production are small (3-15 nodes) — optimize for readability, not for rendering thousands of nodes
- **CON-004**: The graph rendering library MUST be React Flow (`@xyflow/react`) for consistency with the React ecosystem and built-in zoom/pan/minimap
- **CON-005**: All backend persistence is best-effort (fire-and-forget goroutines) — flow save failures MUST NOT break the chat experience

### Guidelines

- **GUD-001**: Use the existing `Handlers` struct pattern for new HTTP handlers
- **GUD-002**: Follow the existing `spyRepo` pattern in `persist_test.go` for unit testing new persistence calls
- **GUD-003**: Frontend components should follow the existing feature-module pattern: `src/features/cpn-visualizer/`
- **GUD-004**: Use `useDeferredValue` for topology JSON parsing to avoid blocking the UI thread
- **GUD-005**: Custom React Flow nodes should use Tailwind classes consistent with the existing theme

### Patterns

- **PAT-001**: Backend API responses follow `writeJSON(w, status, data)` / `writeError(w, status, msg)` pattern from `httpapi/response.go`
- **PAT-002**: Frontend data fetching follows the `services/api.ts` pattern with `ApiError` class
- **PAT-003**: SSE event types follow `EventType` constants from `cpn/event.go`
- **PAT-004**: Persistence follows the `PersistDeps` optional injection pattern with nil-safe checks

## 4. Interfaces & Data Contracts

### 4.1 New Database Migration (006_session_flow_hash)

```sql
-- 006_session_flow_hash.up.sql
ALTER TABLE sessions ADD COLUMN flow_hash TEXT;
CREATE INDEX idx_sessions_flow_hash ON sessions (flow_hash) WHERE flow_hash IS NOT NULL;

-- 006_session_flow_hash.down.sql
DROP INDEX IF EXISTS idx_sessions_flow_hash;
ALTER TABLE sessions DROP COLUMN IF EXISTS flow_hash;
```

### 4.2 New Backend API Endpoints

#### GET /api/v1/flows

**Response** (200):
```json
{
  "items": [
    {
      "hash": "a1b2c3d4...",
      "role": "assistant",
      "execution_count": 42,
      "success_rate": 0.95,
      "avg_cost_usd": 0.003,
      "avg_duration_ms": 2500,
      "created_at": "2026-04-04T14:00:00Z",
      "updated_at": "2026-04-04T15:30:00Z"
    }
  ],
  "has_more": false,
  "next_cursor": ""
}
```

#### GET /api/v1/flows/{hash}

**Response** (200):
```json
{
  "hash": "a1b2c3d4...",
  "role": "assistant",
  "topology": {
    "id": "cpn-...",
    "role": "assistant",
    "depth": 0,
    "mode": "mas",
    "places": {
      "p-input": { "id": "p-input", "color": "STRING", "space": "surface" },
      "p-classified": { "id": "p-classified", "color": "JSON", "space": "surface" },
      "p-output": { "id": "p-output", "color": "ARTIFACT", "space": "surface" }
    },
    "transitions": {
      "t-classify": {
        "id": "t-classify", "kind": "llm",
        "inputPlaces": ["p-input"], "outputPlaces": ["p-classified"],
        "llmConfig": { "model": "classifier", "maxTokens": 64, "temperature": 0 }
      },
      "t-direct": {
        "id": "t-direct", "kind": "llm",
        "inputPlaces": ["p-classified"], "outputPlaces": ["p-output"],
        "guardFunc": "guard-direct-conversation",
        "llmConfig": { "maxTokens": 4096, "temperature": 0.7, "streamOutput": true }
      }
    }
  },
  "stats": {
    "execution_count": 42,
    "success_rate": 0.95,
    "avg_cost_usd": 0.003,
    "avg_duration_ms": 2500
  },
  "created_at": "2026-04-04T14:00:00Z",
  "updated_at": "2026-04-04T15:30:00Z"
}
```

#### GET /api/v1/flows/{hash}/executions

**Response** (200):
```json
{
  "items": [
    {
      "id": "exec-...",
      "cpn_id": "cpn-...",
      "session_id": "sess-...",
      "transitions_fired": 3,
      "llm_calls": 2,
      "tool_calls": 0,
      "tokens_produced": 150,
      "total_cost_usd": 0.004,
      "duration_ms": 3200,
      "success": true,
      "started_at": "2026-04-04T14:40:57Z",
      "completed_at": "2026-04-04T14:41:00Z"
    }
  ]
}
```

#### GET /api/v1/sessions/{id}/topology

**Response** (200): Same shape as `GET /api/v1/flows/{hash}` topology field.

#### GET /api/v1/sessions/{id}/execution

**Response** (200):
```json
{
  "session_id": "sess-...",
  "flow_hash": "a1b2c3d4...",
  "events": [
    {
      "id": "evt-...",
      "type": "stream_chunk",
      "transition_id": "t-classify",
      "transition_kind": "llm",
      "timestamp": "2026-04-04T14:40:57.066Z",
      "payload": { "Content": "...", "Done": false }
    }
  ],
  "execution": {
    "transitions_fired": 3,
    "llm_calls": 2,
    "total_cost_usd": 0.004,
    "duration_ms": 3200,
    "success": true
  }
}
```

### 4.3 FuncRegistry Wiring (cmd/server/topologies.go)

Extract anonymous guards to named functions:

```go
// Package-level named guard functions
func guardDirectConversation(tokens []*cpn.Token) bool { ... }
func guardPlanTask(tokens []*cpn.Token) bool { ... }

// Factory creates a FuncRegistry with all topology functions registered
func newServerFuncRegistry() *persist.FuncRegistry {
    r := persist.NewFuncRegistry()
    r.RegisterGuard("guard-direct-conversation", guardDirectConversation)
    r.RegisterGuard("guard-plan-task", guardPlanTask)
    return r
}
```

### 4.4 Frontend Component Architecture

```
src/features/
├── cpn-visualizer/
│   ├── FlowBrowser.tsx          # Flow list with table/cards
│   ├── FlowDetail.tsx           # Topology graph + execution selector
│   ├── TopologyGraph.tsx        # React Flow graph rendering
│   ├── nodes/
│   │   ├── PlaceNode.tsx        # Custom React Flow node for CPN Places
│   │   └── TransitionNode.tsx   # Custom React Flow node for CPN Transitions
│   ├── ExecutionTimeline.tsx    # Chronological event timeline
│   ├── ExecutionStats.tsx       # Summary bar (cost, duration, tokens, LLM calls)
│   ├── TransitionDetail.tsx     # Slide-over panel with transition details
│   └── useFlows.ts             # Data fetching hook for flows API
├── desktop/
│   └── DesktopLayout.tsx        # Modified: activeApp state, conditional rendering
```

### 4.5 Frontend TypeScript Types

```typescript
// src/types/flow.ts
interface FlowSummary {
  hash: string;
  role: string;
  execution_count: number;
  success_rate: number;
  avg_cost_usd: number;
  avg_duration_ms: number;
  created_at: string;
  updated_at: string;
}

interface FlowDetail extends FlowSummary {
  topology: CPNTopology;
}

interface CPNTopology {
  id: string;
  role: string;
  depth: number;
  mode: string;
  places: Record<string, PlaceTopology>;
  transitions: Record<string, TransitionTopology>;
}

interface PlaceTopology {
  id: string;
  color: string;
  space: string;
}

interface TransitionTopology {
  id: string;
  kind: string;
  inputPlaces: string[];
  outputPlaces: string[];
  errorPlace?: string;
  guardFunc?: string;
  executorFunc?: string;
  systemPrompt?: string;
  llmConfig?: LLMConfigTopology;
  hitlConfig?: HITLConfigTopology;
  subNetTopology?: CPNTopology;
}

interface ExecutionRecord {
  id: string;
  cpn_id: string;
  session_id: string;
  transitions_fired: number;
  llm_calls: number;
  tool_calls: number;
  tokens_produced: number;
  total_cost_usd: number;
  duration_ms: number;
  success: boolean;
  started_at: string;
  completed_at: string;
}

interface ExecutionEvent {
  id: string;
  type: string;
  transition_id: string;
  transition_kind: string;
  timestamp: string;
  payload: unknown;
}
```

### 4.6 React Flow Graph Mapping

```
CPNTopology → React Flow Nodes + Edges:

For each place in topology.places:
  → Node { id: place.id, type: 'place', data: { place }, position: auto-layout }

For each transition in topology.transitions:
  → Node { id: transition.id, type: 'transition', data: { transition }, position: auto-layout }
  → For each inputPlace:
      Edge { source: inputPlace, target: transition.id, animated: true }
  → For each outputPlace:
      Edge { source: transition.id, target: outputPlace }

Layout: dagre (direction: TB, nodeSpacing: 80, rankSpacing: 120)
```

## 5. Acceptance Criteria

### Phase 1: Backend Persistence

- **AC-001**: Given the unified topology factory, when a session sends a message and the CPN completes, then the `flows` table contains a row with the topology's SHA-256 hash and valid `topology_json` JSONB
- **AC-002**: Given a completed CPN run, when querying `execution_records`, then at least one record exists with `transitions_fired > 0` and `success = true`
- **AC-003**: Given a completed session, when querying `sessions`, then the `flow_hash` column is non-null and matches the flow in the `flows` table
- **AC-004**: Given multiple sessions using the same topology, when querying `flows`, then only one flow record exists (deduplication by hash) with `execution_count` matching the session count

### Phase 2: Backend API

- **AC-005**: Given saved flows in the DB, when calling `GET /api/v1/flows`, then the response contains all non-deleted flows with correct stats
- **AC-006**: Given a flow hash, when calling `GET /api/v1/flows/{hash}`, then the response contains the parsed `topology` object (not a JSON string)
- **AC-007**: Given a session ID, when calling `GET /api/v1/sessions/{id}/topology`, then the response contains the CPN topology that was used for that session
- **AC-008**: Given a session ID, when calling `GET /api/v1/sessions/{id}/execution`, then the response contains all events ordered by timestamp

### Phase 3: Frontend Flow Browser

- **AC-009**: Given the Dock, when clicking the "Flows" icon, then the main content area switches from Chat to the Flow Browser
- **AC-010**: Given saved flows, when the Flow Browser loads, then a table displays all flows with role, execution count, success rate, and cost
- **AC-011**: Given a flow in the list, when clicking it, then the Flow Detail view opens showing the topology graph

### Phase 4: Frontend Visualizer

- **AC-012**: Given a flow topology, when the graph renders, then all places appear as circles and transitions as rectangles with correct labels
- **AC-013**: Given a flow with execution data, when selecting an execution, then fired transitions are highlighted and non-fired transitions are dimmed
- **AC-014**: Given a highlighted transition, when hovering, then a tooltip shows LLMConfig details, guard name, cost, and duration
- **AC-015**: Given an execution timeline, when viewing events, then they appear in chronological order with transition labels and durations

## 6. Test Automation Strategy

### Test Levels

- **Unit Tests**: FuncRegistry wiring, guard function extraction, topology-to-graph-node mapping
- **Integration Tests**: Flow persistence round-trip (save → query → verify), execution record persistence
- **UAT Tests**: Full E2E with testcontainers — session → CPN run → flow saved → API returns topology → events queryable
- **Frontend Tests**: React Testing Library for FlowBrowser, TopologyGraph with mock data
- **Visual Tests**: Storybook stories for PlaceNode, TransitionNode custom components

### Frameworks

- **Backend**: Go `testing` + `pgxmock` for unit, testcontainers for integration
- **Frontend**: Vitest + React Testing Library
- **E2E**: Extend existing `integration/persistence_uat_test.go` with flow/execution assertions

### Coverage Requirements

- Backend: >85% on new code (persistence methods, handlers, registry wiring)
- Frontend: >70% on new components (FlowBrowser, TopologyGraph logic)

### Key Test Scenarios

1. `TestUAT_FlowPersistedAfterExecution` — verify flows table has topology after CPN run
2. `TestUAT_ExecutionRecordSaved` — verify execution_records table after CPN run
3. `TestUAT_SessionFlowHashLinked` — verify session.flow_hash is set
4. `TestUAT_FlowAPIReturnsTopology` — verify GET /api/v1/flows/{hash} returns parsed JSON
5. `TestUAT_SessionExecutionTraceQueryable` — verify GET /api/v1/sessions/{id}/execution returns events

## 7. Rationale & Context

### Why React Flow?

CPN topologies are small directed graphs (3-15 nodes). React Flow provides:
- Built-in zoom, pan, minimap, fit-to-view
- Custom node components (perfect for place/transition distinction)
- Edge animations (for token flow visualization)
- First-class React integration with hooks API
- Active maintenance, large community, MIT license

Alternatives considered:
- **D3.js**: Maximum flexibility but requires significant boilerplate for interactivity
- **Cytoscape.js**: Better for graph algorithms than visualization
- **vis.js**: Older, less React-friendly

### Why Dock State Instead of Router?

The existing app uses a desktop metaphor with a dock. Adding React Router would:
- Break the single-page desktop illusion
- Require URL state management not needed for this use case
- Add a dependency when conditional rendering is sufficient

Instead, `DesktopLayout` gets an `activeApp` state ('chat' | 'flows') that switches the main content area.

### Why Background Goroutines for Persistence?

Flow crystallization and execution record persistence must not block the chat response. Following the existing pattern in `persistAfterRun` (which already runs in a goroutine), all new persistence calls are fire-and-forget with logged errors.

### Why Track flow_hash on Sessions?

Without this link, recovering which topology a session used requires re-computing the hash from the in-memory CPN (which doesn't survive restart). Storing it in the sessions table enables:
- `GET /sessions/{id}/topology` without re-computation
- Analytics: which flows are most used
- Debugging: correlate session behavior with topology structure

## 8. Dependencies & External Integrations

### Existing Systems (Already Implemented)

- **EXT-001**: PostgreSQL 16 — flows, execution_records, events tables already exist
- **EXT-002**: Redis 7 — session cache (no changes needed)
- **EXT-003**: `cpn/persist/topology.go` — MarshalCPN, UnmarshalCPN, TopologyHash
- **EXT-004**: `cpn/persist/registry.go` — FuncRegistry with Register/Lookup/ReverseLookup
- **EXT-005**: `store/postgres/flow.go` — FlowRepository (Save, GetByHash, List, UpdateStats, Delete)
- **EXT-006**: `store/postgres/intelligence.go` — IntelligenceRepository (RecordExecution, QueryByRole, Aggregate, TopFlows)
- **EXT-007**: `cpn/flow_metrics.go` — MetricsRecorder, executionTracker, ExecutionRecord

### New Frontend Dependencies

- **PLT-001**: `@xyflow/react` (React Flow v12+) — Interactive graph rendering
- **PLT-002**: `@dagrejs/dagre` — Automatic graph layout (top-to-bottom directed graph)

### Infrastructure Dependencies

- **INF-001**: Migration 006 — `ALTER TABLE sessions ADD COLUMN flow_hash TEXT`

## 9. Examples & Edge Cases

### Example: Unified Topology Graph

```
[p-input]──→[t-classify]──→[p-classified]──→[t-direct]──→[p-output]
    (STRING)     (LLM)          (JSON)          (LLM)      (ARTIFACT)
                                   │
                                   └──→[t-plan]──→[p-plan]──→[t-review]──→[p-reviewed]──→[t-execute]──→[p-output]
                                        (LLM)     (ARTIFACT)    (HITL)       (HUMAN)        (LLM)       (ARTIFACT)
```

### Edge Cases

1. **Topology with no guards**: All transitions fire — graph renders without guard labels
2. **Failed CPN run**: Execution record saved with `success=false`, timeline shows where failure occurred
3. **HITL topology**: Graph shows HITL transitions with "gate" icon, execution replay shows pause duration
4. **SubNet topology**: Nested CPNs render as collapsed groups (expand on click)
5. **Empty flows table**: Flow Browser shows "No flows recorded yet" empty state
6. **Large payload in events**: Truncate payload preview to 200 chars, show full on click
7. **Concurrent CPN runs**: Multiple execution records for same flow hash — each shown independently
8. **FuncRegistry missing function**: MarshalCPN returns error, logged as warning, flow not saved (graceful degradation)

## 10. Validation Criteria

1. After `docker compose up -d --build`, sending a message through the web app results in a non-empty `flows` table with valid `topology_json`
2. The `execution_records` table contains records with `transitions_fired > 0` after any chat interaction
3. `GET /api/v1/flows` returns the saved flows with correct statistics
4. The Flow Browser in the frontend displays the flows and allows clicking into the detail view
5. The topology graph renders places and transitions with correct connectivity
6. Execution replay highlights fired transitions and shows the timeline
7. All existing tests continue to pass (backward compatibility)
8. `make test && golangci-lint run && make test-uat` all pass

## 11. Related Specifications / Further Reading

- [spec-architecture-block20-llm-streaming.md](/spec/spec-architecture-block20-llm-streaming.md) — LLM streaming through CPN engine
- [spec-architecture-clear-conversation.md](/spec/spec-architecture-clear-conversation.md) — Session lifecycle management
- [spec-design-sse-streaming-bugfixes.md](/spec/spec-design-sse-streaming-bugfixes.md) — SSE pipeline fixes
- [React Flow Documentation](https://reactflow.dev/docs) — Graph rendering library
- [dagre Documentation](https://github.com/dagrejs/dagre) — Directed graph layout
