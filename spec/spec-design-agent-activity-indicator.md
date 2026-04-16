---
title: "Agent Activity Indicator — Verb-Driven Live Status for CPN Executions"
version: 1.0
date_created: 2026-04-16
owner: liwaisi-tech
tags: [design, ux, cpn, a2ui, sse, frontend, backend, accessibility, observability]
---

# Introduction

This specification defines the **Agent Activity Indicator**, a fast-changing, accessible verb-bubble rendered in the React chat surface that surfaces what the Coloured Petri Net (CPN) engine is doing internally while the agent is running. It replaces the static three-dot "thinking" spinner (`StreamingIndicator.tsx`) with a live textual signal — e.g. "Thinking", "Reading files", "Calling tool: web_search", "Waiting for you", "Summarizing" — that updates as transitions fire, and resolves into a compact receipt ("Thought for 2.3s · $0.004") when the run completes.

The backend already emits the necessary raw signals (`transition_started`, `transition_completed`, `subnet_started/completed/failed`, `hitl_requested`) over the existing per-session SSE stream; the frontend parses them but currently drops them. This spec closes the gap by adding a backend-owned display-label vocabulary, a frontend reducer slice, a flicker-resistant rendering state machine, and HITL/sub-net handoff rules.

## 1. Purpose & Scope

**Purpose**: Convert the perceived-latency "black box" between user prompt and agent response into a narrated sequence. Give users high-frequency, trustworthy evidence that the agent is making progress, what kind of work it is doing, and what it cost when it finishes. This is the highest-leverage conversational-UX improvement available without changing the CPN engine's semantics.

**Scope**: Full-stack, vertical slice.
- **Backend (Go, `back/go-assistant`)**: Enrich `TransitionStartedPayload` / `TransitionCompletedPayload` with a `display_label` field derived from a versioned verb catalog. Add a `Silent bool` flag on transitions so internal/observer kinds can opt out of UI surfacing. No changes to CPN firing semantics.
- **Frontend (React 19, `front/react-assistant`)**: New `ActivityBubble` component, new `currentActivity` reducer slice in `useChat`, wiring of existing (currently-unused) `onTransitionStarted` / `onTransitionCompleted` / `onSubNet*` callbacks, deletion or replacement of the dots-only `StreamingIndicator`, and a post-completion receipt pill.
- **Out of scope for v1**: nested/indented activity for sub-net hierarchies, per-verb telemetry dashboards, custom verb catalogs per workspace, internationalization of the verb catalog (English only in v1 — i18n wiring is left as a v1.1 follow-up but keys MUST be catalog-addressable).

**Audience**: Go backend engineers (CPN domain), React frontend engineers (chat surface), UX designers (accessibility review), AI Product Manager (verb catalog ownership).

**Assumptions**:
- The per-session SSE broker at `handler_sse.go:14` is the only transport; no new endpoint is introduced.
- `useSSE.ts:187-234` already parses `transition_started` / `transition_completed` / `subnet_*` as named SSE events — the wire format is stable.
- The deep-space terminal theme (dark, cyan accent, JetBrains Mono) and the existing `RoundBadge` pill (`A2UIMessageRenderer.tsx:390-437`) define the visual vocabulary to copy.
- The reducer at `useChat.ts:124-356` is the single source of truth for chat state; new slices MUST be additive and MUST NOT alter existing `STREAM_CHUNK` handling.
- `MessageBubble.tsx` A2UI marker rules (REQ-008/009/010, as referenced in existing tests) continue to hold — the activity indicator is *not* an A2UI payload and MUST NOT participate in the A2UI boundary protocol.

## 2. Definitions

| Term | Definition |
|------|-----------|
| **Activity** | A user-visible statement of what the agent is presently doing, represented as `{ verb, detail?, startedAt }`. |
| **Verb** | A short, present-continuous phrase from the Verb Catalog (e.g., "Thinking", "Reading"). Never ends with a period or ellipsis in v1. |
| **Detail** | Optional, verb-specific modifier (e.g., for Tool calls: the tool name; for SubNet: the child CPN role). Truncated to 40 characters with trailing `…` if longer. |
| **Verb Catalog** | The versioned, PM-owned mapping from `(TransitionKind, Role, ToolName?)` to `{ verb, detail }`. Lives in the backend (see §4). |
| **DisplayLabel** | The serialized `{ verb, detail }` pair attached to transition events by the backend. |
| **Silent Transition** | A transition whose `Silent` flag is `true`. Its events do not update the Activity Indicator. Observer-kind transitions default to `Silent: true`. |
| **Min-Display-Time (MDT)** | The minimum duration (400 ms) that any verb must remain visible before being replaced, to prevent flicker under fast-firing transition bursts. |
| **Coalescing Queue** | A single-slot "latest-wins" buffer holding the next verb to display once MDT elapses. |
| **Receipt** | The post-completion pill replacing the Activity Bubble for ~4 seconds, showing aggregate duration and cost (e.g., "Thought for 2.3s · $0.004"). |
| **HITL** | Human-in-the-Loop — a CPN transition kind that pauses the run and requests user input via an A2UI questionnaire or card. |
| **CPN / A2UI / SSE** | See `spec-design-cpn-execution-monitor.md` §2. Definitions reused verbatim. |

## 3. Requirements, Constraints & Guidelines

### 3.1 Backend — Verb Catalog & Payload Enrichment

- **REQ-001**: Introduce a `DisplayLabel` struct in the CPN domain:
  ```go
  type DisplayLabel struct {
      Verb   string `json:"verb"`
      Detail string `json:"detail,omitempty"`
  }
  ```
- **REQ-002**: Extend `TransitionStartedPayload` with `DisplayLabel *DisplayLabel `json:"display_label,omitempty"``. The pointer is nil for silent transitions.
- **REQ-003**: Extend `TransitionCompletedPayload` with the same optional `DisplayLabel` field, echoing the label emitted at start so the frontend can match start/complete even if the start event was coalesced away.
- **REQ-004**: Introduce a package-level `VerbCatalog` resolver with the signature:
  ```go
  func ResolveDisplayLabel(kind NodeKind, role string, meta TransitionMeta) *DisplayLabel
  ```
  It MUST be pure (no I/O, no randomness) and MUST return `nil` for silent transitions.
- **REQ-005**: The catalog MUST cover, at minimum, the verbs in §4.2 (Verb Catalog v1) and MUST fall back to the generic verb `"Working"` for any `(kind, role)` tuple not explicitly mapped.
- **REQ-006**: Add a `Silent bool` field to `Transition`. When `true`, the engine MUST NOT attach a `DisplayLabel` to its start/complete payloads. The engine MUST still emit the events for monitoring/metrics purposes — only the label is suppressed.
- **REQ-007**: All transitions of `NodeKindObserver` MUST be constructed with `Silent: true` by default. Library constructors that build observer transitions MUST set this flag; this is the allowlist strategy that keeps the bus hygienic.
- **REQ-008**: The catalog resolver and `Silent` handling MUST be applied inside the existing `emit()` path (near `executor.go:147` and `executor.go:182`) so that both the `EventSink` callback and the SSE broker see the enriched payload. No new event types are introduced.
- **REQ-009**: For `NodeKindSubNet`, the `DisplayLabel` MUST derive `detail` from the child CPN's `Role` field, not the parent's. Catalog default verb: `"Delegating"`.
- **REQ-010**: For `NodeKindTool`, if `TransitionMeta` carries a `tool_name` hint, `detail` MUST contain it. Catalog default verb: `"Calling tool"`.
- **REQ-011**: For `NodeKindHITL`, the `DisplayLabel` MUST be `{ verb: "Waiting for you" }` with no detail. This label MUST be emitted at the moment the `EventHITLRequested` event is published, BEFORE the A2UI questionnaire/card payload, so the UI can swap state before rendering the interactive card.

### 3.2 Backend — Wire & Backpressure

- **CON-001**: The existing session SSE channel (buffered at 64, `session.go:119`) is the ONLY transport. No new SSE event type is added in v1.
- **CON-002**: No changes to the CPN firing semantics, token colors, or guard behavior. This feature is purely a metadata enrichment.
- **REQ-012**: Serialization of `display_label` MUST be stable JSON (no field-order dependency), and MUST use `omitempty` so legacy clients see no unknown fields when the label is absent.
- **REQ-013**: A unit test MUST verify that for a transition with `Silent: true`, the emitted `TransitionStartedPayload.DisplayLabel` JSON field is omitted entirely (not `null`).

### 3.3 Frontend — Reducer & State

- **REQ-014**: Add a `currentActivity` slice to the chat reducer state:
  ```ts
  type CurrentActivity = {
    verb: string;
    detail?: string;
    startedAt: number;   // ms epoch, from client clock
    transitionId: string;
    cpnId: string;
  } | null;
  ```
- **REQ-015**: Add a `recentReceipt` slice for the post-completion pill:
  ```ts
  type RecentReceipt = {
    durationMs: number;
    costUsd: number;
    shownAt: number;
  } | null;
  ```
- **REQ-016**: New reducer actions: `ACTIVITY_START`, `ACTIVITY_END`, `ACTIVITY_RECEIPT_SHOW`, `ACTIVITY_RECEIPT_DISMISS`. Each MUST be idempotent — re-dispatching with the same `transitionId` is a no-op.
- **REQ-017**: Wire the existing, currently-unused `onTransitionStarted`, `onTransitionCompleted`, `onSubNetStarted`, `onSubNetCompleted`, `onSubNetFailed` callbacks exposed by `useChat.ts:366-378` into `ChatContainer.tsx` so the reducer actions actually fire. This wiring is the single largest behavioral change on the frontend.
- **REQ-018**: If a `transition_started` event arrives with `display_label == null` (i.e., silent), the reducer MUST NOT dispatch `ACTIVITY_START`. The event is still observable for future telemetry.
- **REQ-019**: When `sessionState` transitions to `completed`, `failed`, or `hitl_pending`, any active `currentActivity` MUST be cleared at the next animation frame (not synchronously — see MDT rule in §3.4).
- **REQ-020**: Session-id race guard (parity with REQ-302 for stream chunks): activity events from a different `SessionID` than the current active session MUST be dropped silently.

### 3.4 Frontend — Rendering State Machine (Flicker Control)

- **REQ-021**: Implement a two-state display controller inside `ActivityBubble`:
  - `stable(verb)` — verb is currently visible.
  - `coalescing(nextVerb, readyAt)` — a new verb has arrived but MDT has not elapsed; latest-wins replaces any prior `nextVerb` in the single-slot buffer.
- **REQ-022**: Min-Display-Time (MDT) MUST be **400 ms** in v1. After MDT elapses, the controller transitions to `stable(nextVerb)` with a **150 ms CSS opacity crossfade**.
- **REQ-023**: If an activity ends (transition_completed or session terminal) while in `coalescing`, the buffered `nextVerb` MUST be discarded without ever being shown.
- **REQ-024**: If two consecutive verbs are identical (same `verb` AND same `detail`), the transition MUST NOT trigger a crossfade — only the underlying `transitionId`/`startedAt` is updated silently.
- **REQ-025**: Under sustained bursts (>10 transitions/second), the user MUST observe AT MOST `1000 / MDT = 2.5` verb swaps per second. The Coalescing Queue guarantees this ceiling.

### 3.5 Frontend — Component & Visual Design

- **REQ-026**: Create `src/features/chat/ActivityBubble.tsx`. Delete `src/features/chat/StreamingIndicator.tsx` and remove its `showThinking` gate in `MessageList.tsx:37-40`; `ActivityBubble` is the sole replacement.
- **REQ-027**: Visual style MUST mirror `RoundBadge` (`A2UIMessageRenderer.tsx:390-437`): pill shape, JetBrains Mono, 8 % accent-color tint background, 1 px accent-color border, uppercase tracking on the verb, mixed-case on the detail.
- **REQ-028**: The bubble MUST render between the last message and the composer input, in the same vertical slot where `StreamingIndicator` currently renders. It MUST NOT be a chat message (i.e., MUST NOT live in the `messages[]` array; it lives in its own reducer slice).
- **REQ-029**: When `currentActivity` is non-null, render `{verb}{detail ? " · " + detail : ""}` with a small leading pulse glyph (single animated dot, 700 ms ease-in-out). Detail MUST be truncated to 40 characters with a trailing `…` (CSS `text-overflow: ellipsis`, single line, max-width 60 % of chat width).
- **REQ-030**: When `currentActivity` is null AND `recentReceipt` is non-null AND `now - shownAt < 4000 ms`, render the Receipt pill with the same chrome but muted color (50 % accent tint). Text format: `"Thought for {durationSeconds}s · ${costUsd.toFixed(4)}"`. Durations <1 s display as `"<1s"`; costs of 0 omit the `· $0.0000` clause entirely.

### 3.6 Accessibility

- **SEC-001**: The `ActivityBubble` root MUST carry `role="status"` and `aria-live="polite"`. It MUST NOT carry `aria-live="assertive"` (avoids interrupting screen-reader announcements mid-sentence).
- **SEC-002**: Verb changes MUST be debounced for screen-reader announcement at **1200 ms** (stricter than the visual MDT of 400 ms) — visual flicker control is about motion; SR announcement flicker is about cognitive load, and requires a larger window. Implementation: a second ref-held timer that only commits the verb to a visually-hidden `aria-live` slot when it has been stable for 1200 ms.
- **SEC-003**: The pulse glyph animation MUST respect `prefers-reduced-motion: reduce` and fall back to a static dot.
- **SEC-004**: The bubble MUST NOT steal focus. It is presentational-plus-status; it is not interactive.
- **SEC-005**: Color contrast of verb text against the tinted background MUST meet WCAG AA (≥ 4.5:1) in both the deep-space dark theme and any future light theme.

### 3.7 HITL & Terminal-State Handoff

- **REQ-031**: On receipt of `hitl_requested`, the reducer MUST immediately dispatch `ACTIVITY_START({ verb: "Waiting for you" })` BEFORE the A2UI card renders. This provides the "swap-before-jump" affordance Product requires.
- **REQ-032**: When the A2UI questionnaire/card is actually mounted in the chat message stream, the reducer MUST dispatch `ACTIVITY_END` to remove the "Waiting for you" bubble (the card itself is now the affordance — a duplicate bubble would be redundant).
- **REQ-033**: On `session_completed`, the reducer MUST dispatch `ACTIVITY_END` and, if an accumulated duration/cost is available, `ACTIVITY_RECEIPT_SHOW` with the totals. If cost is unavailable or zero, the receipt still shows the duration.
- **REQ-034**: On `session_failed` or a terminal SSE error, the reducer MUST dispatch `ACTIVITY_END` with no receipt. The failure is surfaced by the existing error UI; the activity bubble must not linger.

### 3.8 Guidelines & Patterns

- **GUD-001**: Prefer backend-owned verbs (catalog) over frontend mapping tables. Rationale: the verb vocabulary is product copy, not UI logic; putting it in Go keeps copy review centralized and enables future i18n by catalog swap.
- **GUD-002**: Keep the reducer slice additive. Do not mutate or reorder existing action handlers in `useChat.ts`.
- **PAT-001**: Follow the `RoundBadge` rendering pattern — same DOM shape, same CSS module conventions, same `aria-live` approach. Copy, then diverge only where necessary.
- **PAT-002**: Model the display controller (stable/coalescing) as a plain reducer-driven state, not a custom hook with its own `useEffect` timers scattered across the component. Centralize timer management in one `useEffect` keyed on `currentActivity?.transitionId`.

## 4. Interfaces & Data Contracts

### 4.1 Backend — Enriched Payload JSON (on the wire)

```json
// event: transition_started
{
  "type": "transition_started",
  "transition_id": "t-llm-reason",
  "transition_kind": "LLM",
  "cpn_id": "root",
  "cpn_role": "planner",
  "session_id": "sess_abc123",
  "timestamp": "2026-04-16T14:22:05.123Z",
  "payload": {
    "input_tokens": [/* unchanged */],
    "display_label": { "verb": "Thinking" }
  }
}

// event: transition_started (Tool with detail)
{
  "type": "transition_started",
  "transition_id": "t-tool-search",
  "transition_kind": "Tool",
  "payload": {
    "input_tokens": [/* unchanged */],
    "display_label": { "verb": "Calling tool", "detail": "web_search" }
  }
}

// event: transition_started (Silent — field omitted)
{
  "type": "transition_started",
  "transition_id": "t-observer-log",
  "transition_kind": "Observer",
  "payload": {
    "input_tokens": [/* unchanged */]
    // display_label MUST be absent, not null
  }
}

// event: transition_completed
{
  "type": "transition_completed",
  "transition_id": "t-llm-reason",
  "payload": {
    "output_tokens": [/* unchanged */],
    "cost_usd": 0.0041,
    "duration_ms": 2310,
    "executed_model": "claude-opus-4-6",
    "display_label": { "verb": "Thinking" }
  }
}
```

### 4.2 Verb Catalog v1 (Backend-Owned)

| TransitionKind | Role / Hint | Verb | Detail Source | Silent? |
|----------------|-------------|------|---------------|---------|
| `LLM` | `planner`, `reasoner`, any | `"Thinking"` | — | no |
| `LLM` | `summarizer` | `"Summarizing"` | — | no |
| `LLM` | `classifier`, `router` | `"Deciding"` | — | no |
| `Tool` | `tool_name = fs_read` | `"Reading"` | file path (≤40 chars) | no |
| `Tool` | `tool_name = fs_write` | `"Writing"` | file path (≤40 chars) | no |
| `Tool` | `tool_name = web_search` | `"Searching"` | query (≤40 chars) | no |
| `Tool` | any other | `"Calling tool"` | `tool_name` | no |
| `Validate` | — | `"Validating"` | — | no |
| `SubNet` | — | `"Delegating"` | child CPN `Role` | no |
| `HITL` | — | `"Waiting for you"` | — | no |
| `Observer` | — | *(none)* | — | **yes** |
| *(fallback)* | — | `"Working"` | — | no |

### 4.3 Frontend — Reducer Action Shapes (TypeScript)

```ts
type ChatAction =
  // ... existing actions ...
  | { type: "ACTIVITY_START"; payload: {
        transitionId: string;
        cpnId: string;
        sessionId: string;
        verb: string;
        detail?: string;
    }}
  | { type: "ACTIVITY_END"; payload: {
        transitionId: string;
        sessionId: string;
        durationMs?: number;
        costUsd?: number;
    }}
  | { type: "ACTIVITY_RECEIPT_SHOW"; payload: {
        durationMs: number;
        costUsd: number;
    }}
  | { type: "ACTIVITY_RECEIPT_DISMISS" };
```

### 4.4 Frontend — `ActivityBubble` Component Contract

```tsx
// src/features/chat/ActivityBubble.tsx
interface ActivityBubbleProps {
  activity: CurrentActivity;       // from reducer
  receipt: RecentReceipt;          // from reducer
  prefersReducedMotion?: boolean;  // default: from media query
}

// Renders exactly one of:
//   - null (no activity, no receipt within 4s)
//   - activity pill (animated dot + verb + optional detail)
//   - receipt pill (muted, static, auto-dismissing after 4s)
```

## 5. Acceptance Criteria

- **AC-001**: Given the session is running and an `LLM` transition fires, when `transition_started` arrives, then the `ActivityBubble` displays `"Thinking"` within 150 ms of receipt (one animation frame of crossfade).
- **AC-002**: Given a `Tool` transition fires with `tool_name = "web_search"` and `query = "CPN event semantics"`, when the event is received, then the bubble renders `"Searching · CPN event semantics"`.
- **AC-003**: Given 12 transitions fire in 1 second, when MDT is 400 ms, then the user sees AT MOST 3 distinct verb states (ceiling = ⌈1000/400⌉ = 3), with intermediate verbs coalesced away.
- **AC-004**: Given an `Observer`-kind transition fires, when the event is received, then the `ActivityBubble` state does NOT change (silent transition).
- **AC-005**: Given a transition emits `transition_started` with `display_label` absent from the JSON, when the reducer receives it, then `ACTIVITY_START` is NOT dispatched.
- **AC-006**: Given `hitl_requested` arrives, when the event is processed, then `"Waiting for you"` is shown BEFORE the A2UI card mounts in the message stream, and is cleared exactly when the card becomes visible.
- **AC-007**: Given `session_completed` fires with total duration 2310 ms and cost $0.0041, when the terminal event is received, then the receipt pill displays `"Thought for 2.3s · $0.0041"` for 4 s and then dismisses.
- **AC-008**: Given `session_completed` fires with cost 0, when the receipt is shown, then only `"Thought for 2.3s"` is displayed — the cost clause is omitted.
- **AC-009**: Given the user has `prefers-reduced-motion: reduce`, when the bubble is displayed, then the pulse glyph is a static dot and the crossfade is replaced by an instantaneous swap.
- **AC-010**: Given a screen reader is active, when three verbs change within 800 ms, then ONLY the verb stable at the 1200 ms mark is announced (SR debounce window, SEC-002).
- **AC-011**: Given two activity events arrive carrying different `sessionId`s than the active session, when the reducer processes them, then they are dropped and no UI change occurs (session-id race guard, REQ-020).
- **AC-012**: Given a `transition_completed` event has `display_label` matching the currently-displayed activity, when processed, then `ACTIVITY_END` is dispatched and the bubble clears (or, on terminal state, the receipt replaces it).
- **AC-013**: Given the backend emits a payload whose `(kind, role, tool)` tuple is not in the catalog, when the resolver runs, then the fallback verb `"Working"` is emitted and the UI renders it without error.
- **AC-014**: Given `StreamingIndicator.tsx` previously rendered three dots while `sessionState === 'running'` with no streaming message, when the new feature ships, then that component is DELETED and no dots-only indicator appears anywhere in the chat surface.

## 6. Test Automation Strategy

- **Test Levels**: Unit (Go + Vitest), integration (Go HTTP handler + Vitest with SSE fixtures), end-to-end (Playwright, existing harness).
- **Frameworks**:
  - Go: standard `testing` package, table-driven tests, `httptest` for SSE handler assertions.
  - Frontend: Vitest + React Testing Library (consistent with existing `A2UIMessageRenderer.test.tsx` and `useChat.test.ts`).
  - E2E: Playwright scripts that replay a captured SSE trace (new fixture under `front/react-assistant/src/__fixtures__/activity/`).
- **Test Data Management**:
  - A new Go fixture file `cpn/testdata/verb_catalog_cases.json` enumerates (kind, role, tool) → expected `DisplayLabel` for table-driven coverage.
  - A new frontend fixture `activity-burst.sse.txt` captures a 12-event burst for flicker-ceiling verification (AC-003).
- **CI/CD Integration**: All new tests run in the existing GitHub Actions pipeline. The backend package gains a `TestResolveDisplayLabel` suite and a `TestSilentObserverOmitsLabel` unit test. The frontend gains `ActivityBubble.test.tsx` and new cases under `useChat.test.ts`.
- **Coverage Requirements**: ≥ 85 % for new files (`ActivityBubble.tsx`, verb-catalog resolver), consistent with the Dev Workflow rule. Existing file coverage MUST NOT regress.
- **Performance Testing**: A single Vitest benchmark asserts that 100 `ACTIVITY_START`/`ACTIVITY_END` pairs per second produce no more than 2.5 DOM updates per second (coalescing-ceiling proof, AC-003 in bulk form).

## 7. Rationale & Context

**Why this feature, why now.** The Expert Team Review (prior conversation turn) identified this as the single highest-leverage conversational-UX investment available without re-architecting the engine. Competitor tools (Claude.ai, Cursor, Perplexity) all ship a live activity narration; its absence in liwaisi is increasingly noticeable and erodes trust during multi-second LLM or sub-net runs.

**Why the verb catalog lives in the backend.** The verbs are product copy, not UI layout. Putting them in Go keeps product/PM review in one place, enables future i18n by swapping a catalog file, and decouples the frontend from CPN internals (`NodeKind` enums never leak into TSX). This was the architect's and PM's unanimous recommendation.

**Why no new SSE event type.** The existing `transition_started` / `transition_completed` events already carry the right lifecycle boundaries. Adding a dedicated `activity` event would double the bus traffic without adding information, and would introduce a second source of truth that could drift from transition events. Enriching the existing payloads is the minimum-intrusion, maximum-fidelity path.

**Why a single-slot coalescing buffer (and not a full queue).** Users cannot read six verbs in one second. The visual value of showing every intermediate verb is zero; the cognitive cost is positive. Latest-wins matches how humans track "what is the system doing right now." This is the UX designer's (Lena) validated recommendation.

**Why 400 ms MDT and 1200 ms SR debounce.** 400 ms is the documented perceptual threshold for "a new thing appeared" — below it, the eye registers flicker rather than change. 1200 ms for screen readers is triple that, because audio announcements cannot be glanced past; they must be complete sentences, and they must not pile up.

**Why HITL swaps first and the card mounts second.** Without the early swap, users see the thinking indicator jump directly to a fully-rendered interactive card, which feels abrupt. The "Waiting for you" intermediate frame gives the UI a narrative beat.

**Why delete `StreamingIndicator.tsx` rather than keep both.** Two coexisting "the agent is doing something" affordances is strictly worse than one — they'd compete for the same vertical slot and confuse users about which is authoritative. The `ActivityBubble` strictly subsumes the dots (it renders the same *kind* of signal, just more informatively).

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: None. This is a UI-plus-enrichment feature internal to the liwaisi stack.

### Third-Party Services
- **SVC-001**: None.

### Infrastructure Dependencies
- **INF-001**: The existing SSE broker (`handler_sse.go`, `sse_broker.go`) must continue to deliver events in-order per session. This is an existing guarantee; no new requirement.

### Data Dependencies
- **DAT-001**: The verb catalog data lives in-repo as Go source. No external data source.

### Technology Platform Dependencies
- **PLT-001**: Go 1.22+ (matches existing `back/go-assistant` minimum).
- **PLT-002**: React 19 + Vitest + TypeScript 5.x (matches existing `front/react-assistant` setup).

### Compliance Dependencies
- **COM-001**: WCAG 2.1 AA — applies to color contrast (SEC-005) and `aria-live` usage (SEC-001, SEC-002, SEC-003). No new regulatory scope beyond what the existing chat surface already satisfies.

## 9. Examples & Edge Cases

### 9.1 Happy path — single LLM call

```
User sends prompt
↓ SSE: transition_started { kind=LLM, display_label={verb:"Thinking"} }     t+0ms
UI renders "Thinking"                                                         t+20ms
↓ SSE: stream_chunk ("Hello, ...")                                            t+800ms
  (stream_chunk does NOT clear activity; ACTIVITY_END waits on completed)
↓ SSE: transition_completed { duration_ms=2310, cost_usd=0.0041 }             t+2310ms
↓ SSE: session_completed                                                       t+2340ms
UI hides bubble, shows receipt "Thought for 2.3s · $0.0041" for 4s           t+2340ms
```

### 9.2 Rapid transition burst (flicker control)

```
t+0ms:   transition_started verb="Thinking"       → stable("Thinking")
t+50ms:  transition_started verb="Reading"        → coalescing("Reading", readyAt=400)
t+100ms: transition_started verb="Summarizing"    → coalescing("Summarizing", readyAt=400)  [latest-wins]
t+200ms: transition_started verb="Calling tool"   → coalescing("Calling tool", readyAt=400) [latest-wins]
t+400ms: timer fires                              → stable("Calling tool") with 150ms crossfade
```
Result: user sees exactly two verbs — `"Thinking"` then `"Calling tool"` — not four.

### 9.3 Silent observer is invisible

```
SSE: transition_started { kind=Observer }      ← no display_label field
Reducer: sees payload.display_label === undefined → dispatch NO-OP
ActivityBubble: unchanged
```

### 9.4 HITL handoff

```
t+0ms:    currentActivity = { verb: "Thinking" }
t+1500ms: SSE: hitl_requested
          Reducer: ACTIVITY_START { verb: "Waiting for you" }
          UI: bubble crossfades to "Waiting for you"
t+1650ms: A2UI card payload mounts in message stream
          Reducer: ACTIVITY_END (bubble clears; card is now the affordance)
```

### 9.5 Cost-zero completion

```
session_completed { total_duration_ms: 800, total_cost_usd: 0 }
→ Receipt: "Thought for <1s"   (no cost clause, <1s formatting)
```

### 9.6 Unknown tool fallback

```
Backend:  kind=Tool, tool_name="some_unregistered_mcp_tool"
Catalog:  no (kind, tool) match → falls through to (Tool, *) row
Result:   display_label = { verb: "Calling tool", detail: "some_unregistered_mcp_tool" }
UI:       "Calling tool · some_unregistered_mcp_tool"
```

### 9.7 Cross-session leak prevention

```
Active session: sess_A
SSE delivers (racily) transition_started with session_id=sess_B
Reducer REQ-020 guard: sessionId mismatch → drop silently
ActivityBubble: unchanged
```

### 9.8 Reduced-motion accessibility

```
prefers-reduced-motion: reduce
→ pulse glyph: static ●
→ crossfade: replaced by instant swap
→ SR debounce: unchanged at 1200ms (motion pref ≠ cognitive pref)
```

## 10. Validation Criteria

A PR implementing this spec is considered compliant when ALL of the following are demonstrably true:

1. **All acceptance criteria AC-001 through AC-014 pass** in automated tests (Vitest for AC-001..012,014; Playwright replay for AC-003 burst; axe-core assertion for AC-009..010).
2. **Backend unit tests** cover every row of the Verb Catalog v1 table in §4.2, plus the silent-omission test (REQ-013).
3. **Frontend unit tests** cover: reducer actions (REQ-016 idempotence), coalescing controller (REQ-021..025), receipt auto-dismiss (REQ-030 + 4 s timeout).
4. **No regression in existing tests**: `MessageBubble.test.tsx`, `useChat.test.ts`, `A2UIMessageRenderer.test.tsx` remain green.
5. **`StreamingIndicator.tsx` is deleted** from the repo, and no other file imports it (verified by `rg "StreamingIndicator"` returning zero matches).
6. **Coverage on new files ≥ 85 %** (Dev Workflow gate).
7. **Manual smoke test**: running a multi-transition prompt (e.g., "search the web for X, read file Y, summarize") in the dev environment shows at least three distinct verbs over the run and a receipt at the end.
8. **Accessibility review**: axe-core reports zero critical or serious violations on the chat surface with the bubble visible; manual VoiceOver/NVDA pass confirms SR debounce behavior (SEC-002).

## 11. Related Specifications / Further Reading

- [spec-design-cpn-execution-monitor.md](./spec-design-cpn-execution-monitor.md) — the real-time CPN observability dock, which also consumes `transition_started`/`transition_completed` events. The activity indicator is a lighter-weight, conversational-surface counterpart to the full monitor. Event payloads MUST remain compatible.
- [spec-architecture-a2a-a2ui-protocol-integration.md](./spec-architecture-a2a-a2ui-protocol-integration.md) — A2UI message contract. The activity bubble is explicitly NOT an A2UI payload; this spec inherits the A2UI boundary rules only insofar as it must not interfere with them.
- [spec-architecture-block20-llm-streaming.md](./spec-architecture-block20-llm-streaming.md) — LLM streaming semantics. The activity bubble complements token streaming; it does not suppress or replace it.
- [spec-architecture-cpn-iterative-clarification-loop.md](./spec-architecture-cpn-iterative-clarification-loop.md) — clarification-loop context; the HITL handoff rules (REQ-031..032) intersect with the clarify loop's A2UI card lifecycle.
- [spec-design-sse-streaming-bugfixes.md](./spec-design-sse-streaming-bugfixes.md) — SSE reconnection and session-id race guards. REQ-020 in this spec reuses the same guard pattern.
