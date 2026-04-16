---
title: "Bug Fix — A2UI Chunk Boundary & HITL Surface Persistence"
version: 1.0
date_created: 2026-04-13
last_updated: 2026-04-13
owner: liwaisi-tech
tags: [process, bugfix, a2ui, hitl, streaming, sse, cpn, frontend, backend]
---

# Introduction

The A2UI questionnaire emitted by the `t-clarify` HITL transition currently renders
as garbled character-per-line text in the React chat instead of as an interactive
wizard. The root cause is a collision between two independent subsystems:

1. The frontend chat reducer concatenates every incoming `stream_chunk` into the
   still-streaming assistant bubble that shares the same `CPNID`. When an
   `$$a2ui:` chunk lands on a bubble that already contains prior text, the merged
   content no longer starts with `$$a2ui:` and
   [`MessageBubble.parsePayload`](../front/react-assistant/src/features/chat/MessageBubble.tsx)
   falls through to the markdown renderer.
2. The markdown renderer loads `remark-math` + `rehype-katex`, which treat `$$`
   as a LaTeX display-math delimiter and `{ }` as grouping markers. When an A2UI
   payload reaches the markdown path, KaTeX strips the `$$`, `{`, and `}`, and
   renders the remaining JSON tokens as failed math — producing the observed
   character-by-character output.

A secondary defect: `fireHITL` in `back/go-assistant/cpn/hitl.go` publishes the
A2UI chunk via `EventStreamChunk` but never appends it to `c.History`, so
`persistAfterRun` in `internal/app/session_service.go` does not persist the
surface. The questionnaire disappears on session reload, leaving the user with
no way to answer after a page refresh — yet the CPN is still blocked on HITL.

This specification defines the narrow, additive fix. It introduces **no new
protocol surface**. The `$$a2ui:` marker, the HITL resolve wire contract, the
catalog, and every A2UI component schema remain unchanged. The fix is a pure
correctness patch at the two integration seams (reducer boundary, persistence).

## 1. Purpose & Scope

**Purpose**: Restore correct rendering of A2UI HITL surfaces (questionnaire +
any future A2UI-driven HITL component) during live streaming and after session
rehydration, by (a) making `$$a2ui:` a hard message boundary in the frontend
reducer and (b) persisting the surface into session history on the backend.

**In scope**:
- Frontend chat reducer (`useChat.ts`) chunk-boundary behaviour for `$$a2ui:`.
- Frontend A2UI payload detection (`MessageBubble.parsePayload`) defensiveness
  against leading whitespace and embedded markers.
- Backend HITL A2UI chunk persistence (`fireHITL` → `c.History` →
  `persistAfterRun` path).
- Session rehydration so persisted `$$a2ui:` messages re-render as A2UI
  surfaces, not as text.
- Regression tests that cover the chunk-boundary and persistence invariants.

**Out of scope**:
- Any change to the `$$a2ui:` marker string or to the A2UI component schemas.
- Any change to the HITL resolve HTTP contract (`action`, `content` fields).
- Any change to the A2A driving adapter (`internal/driving/a2a/`).
- Removal, replacement, or reconfiguration of `remark-math` / `rehype-katex`.
  These are treated as an upstream constraint the A2UI path must coexist with.
- A2UI buffering of intentionally split chunks (§12.6 open item in the parent
  spec); see REQ-003 below for the narrower invariant adopted here.
- Changes to non-HITL A2UI surfaces emitted from other code paths (there are
  none today; any future emitter MUST follow §4.3 of this document).

**Audience**: Backend Go engineers modifying `cpn/hitl.go` and
`internal/app/session_service.go`; frontend React engineers modifying
`useChat.ts`, `MessageBubble.tsx`, and related tests.

**Assumptions**:
- The A2UI payload is emitted atomically in a single `StreamChunk.Content` by
  the backend today (`back/go-assistant/cpn/hitl.go:96-107`). The fix preserves
  that invariant — split A2UI payloads remain out of scope until §12.6 of the
  parent spec is resolved.
- The frontend continues to use `remark-math` + `rehype-katex` in
  `MarkdownContent.tsx`. No math-plugin changes are required.
- The existing persistence stack (`persist.MessageToRecord` →
  `Sessions.AppendMessage` → Postgres `messages.content TEXT`) preserves the
  `$$a2ui:` prefix verbatim (verified: no escaping, no trimming, no collation
  coercion in the code path between `Content` and the DB column).

## 2. Definitions

| Term | Definition |
|------|-----------|
| **A2UI marker** | The literal prefix `$$a2ui:` that precedes a JSON payload in a `StreamChunk.Content`, as defined in spec-architecture-a2a-a2ui-protocol-integration.md §4.6 |
| **A2UI surface** | A rendered A2UI component tree (currently: `questionnaire` + `choice` children from §12 of the parent spec) |
| **HITL surface chunk** | The specific `StreamChunk` emitted by `fireHITL` whose `Content` begins with `$$a2ui:`, carrying the interactive surface for a HITL transition |
| **Chunk boundary** | A point in the SSE stream at which the frontend reducer MUST start a fresh assistant bubble instead of appending to an existing streaming bubble |
| **Rehydration** | The process (in `SessionService.Rehydrate…` / `GetSession`) by which an in-memory CPN session is reconstructed from persistence after backend restart or page reload |
| **KaTeX collision** | The garbled rendering produced when a `$$a2ui:…` string reaches `MarkdownContent` and is parsed by `rehype-katex` as LaTeX display-math |
| **`c.History`** | The in-memory `[]*Message` slice on `cpn.CPN` that `session_service.go` syncs into `session.Messages()` and then persists via `persistAfterRun` |

## 3. Requirements, Constraints & Guidelines

### Frontend requirements

- **REQ-001**: The chat reducer (`useChat.ts`, `STREAM_CHUNK` case) MUST treat
  any incoming chunk whose `Content` starts with the A2UI marker (`$$a2ui:`)
  as a **new assistant bubble**. The reducer MUST NOT append such a chunk to
  any existing streaming bubble, regardless of matching `CPNID`.
- **REQ-002**: When the reducer creates the new bubble for a marker-prefixed
  chunk, it MUST first mark every currently streaming assistant message as
  `isStreaming: false` to close out any in-flight bubble that belongs to an
  earlier transition. This mirrors the behaviour of the existing `Done && !Content`
  sentinel.
- **REQ-003**: The reducer MUST NOT attempt to buffer partial A2UI JSON across
  multiple chunks in this fix. If a `$$a2ui:` chunk is followed by a
  non-marker chunk on the same `CPNID` before a `Done` sentinel, the reducer
  MUST treat the follow-on chunk as a new assistant bubble as well (no
  concatenation onto the A2UI bubble). Buffering across split A2UI payloads
  is deferred to the §12.6 open item in the parent spec.
- **REQ-004**: `MessageBubble.parsePayload` MUST detect the A2UI marker after
  trimming leading ASCII whitespace (`\u0020`, `\t`, `\r`, `\n`). Content with
  leading whitespace followed by `$$a2ui:` MUST route to the A2UI renderer on
  successful JSON parse.
- **REQ-005**: `MessageBubble.parsePayload` MUST continue to fall through to
  the markdown text component when (a) the trimmed content does not start with
  `$$a2ui:`, or (b) the JSON after the marker fails to parse. This preserves
  the §9 "partial A2UI payloads fall back to markdown rendering until complete"
  invariant from the parent spec.
- **REQ-006**: Session rehydration (the path in `useChat.ts` that maps
  `session.messages` into `ChatMessage` objects on `SESSION_LOADED`) MUST
  preserve `content` verbatim. A persisted message whose `content` starts with
  `$$a2ui:` MUST render through the A2UI renderer on reload, not through
  markdown.

### Backend requirements

- **REQ-007**: `fireHITL` in `back/go-assistant/cpn/hitl.go` MUST, after
  successfully emitting an A2UI `EventStreamChunk`, append a
  `RoleAssistant` `Message` to `c.History` whose `Content` is the full
  `"$$a2ui:" + json` payload and whose `CPNID`, `CPNRole`, and `CPNDepth` match
  the emitting CPN. This ensures the surface is picked up by the existing
  history-sync path in `session_service.go:293-307` and persisted by
  `persistAfterRun`.
- **REQ-008**: The append in REQ-007 MUST occur only on the success branch
  where `payload != nil`, `json.Marshal` succeeded, and the `EventStreamChunk`
  was emitted. Failures MUST continue to be non-fatal (the HITL request
  proceeds without an A2UI surface, matching the existing comment at
  `hitl.go:55-57`).
- **REQ-009**: The Done-sentinel emission currently at `hitl.go:113-124` MUST
  remain unchanged. It continues to act as an SSE-level boundary for live
  clients that connected before REQ-001 landed (defence in depth).
- **REQ-010**: The A2UI surface message appended in REQ-007 MUST NOT be
  duplicated by downstream paths. Specifically:
  - `session_service.go:293-307` (history sync after run) already copies
    `c.History` into `session.Messages`; no additional path is required.
  - `session_service.go:349-358` (terminal-places fallback when streaming was
    not active) MUST NOT emit the A2UI message again — this branch runs only
    when `streamingActive == false`, and `fireHITL` sets
    `c.StreamedOutput = true` (`hitl.go:95`), so the guard already holds.
  - Tests in AC-006 MUST assert no duplicate message appears in
    `session.Messages()` or in the persisted `messages` table.

### Constraints

- **CON-001**: The `$$a2ui:` marker string MUST NOT change. It is the protocol
  contract in spec-architecture-a2a-a2ui-protocol-integration.md §4.6
  (`A2UI_MARKER`) and is referenced by both sides of the adapter.
- **CON-002**: The HITL resolve wire shape (`POST /api/v1/sessions/{id}/hitl/{transitionId}`
  with `{action, content}`, where `action ∈ {approve, reject, revise, submit}`)
  MUST NOT change. See parent spec §12.2.
- **CON-003**: `remark-math` and `rehype-katex` in `MarkdownContent.tsx` MUST
  remain configured as they are today. The fix MUST work without removing or
  reconfiguring them.
- **CON-004**: The backend MUST continue to emit the A2UI payload atomically in
  a single `StreamChunk.Content`. The fix does NOT introduce split-chunk
  buffering; REQ-003 explicitly defers that to the §12.6 open item.
- **CON-005**: No new HTTP endpoints, SSE event types, or `ResolveHITLRequest`
  fields are introduced.
- **CON-006**: The CPN domain layer (`cpn/`) MUST NOT import anything from
  `internal/app/` or `internal/driving/`. REQ-007 is satisfied purely by
  mutating `c.History` — the existing pre-established channel.

### Guidelines

- **GUD-001**: Prefer a single-line helper `isA2UIMarker(content string) bool`
  in the frontend reducer so the marker literal appears exactly once. The
  literal SHOULD be imported from the same module that exports `A2UI_MARKER`
  in `MessageBubble.tsx` (extract to a small shared module if needed to avoid
  a circular import).
- **GUD-002**: On the backend, extract the `$$a2ui:` prefix into a package-level
  constant in `cpn` (e.g. `const A2UIMarker = "$$a2ui:"`) so `hitl.go` emits and
  persists using the same literal. This also hardens future test assertions
  (`hitl_test.go:554`) against drift.
- **GUD-003**: Keep the reducer change surgical. A full redesign (buffering,
  out-of-order chunk handling, reassembly) is out of scope; the narrow
  invariant in REQ-001/REQ-002/REQ-003 is sufficient to eliminate the bug.
- **GUD-004**: The frontend should not attempt to detect A2UI content inside a
  mid-string slice. The marker is authoritative only at the start of a chunk
  (after trimming leading whitespace per REQ-004). Mid-string detection would
  complicate REQ-005 fallback semantics.

### Patterns

- **PAT-001**: **Marker as boundary** — `$$a2ui:` is treated as an implicit
  message terminator for the preceding bubble and an implicit message opener
  for the new bubble. This is the minimum semantics that prevents
  concatenation without introducing a new protocol field.
- **PAT-002**: **Single-site persistence** — the A2UI surface is persisted by
  appending to `c.History` at the emission site (`fireHITL`), re-using the
  existing history-sync pipeline. No new persistence code path is added.

## 4. Interfaces & Data Contracts

### 4.1 Frontend reducer — STREAM_CHUNK decision table

The reducer (`front/react-assistant/src/hooks/useChat.ts`) MUST implement the
following branching when handling a `STREAM_CHUNK` action. `C` denotes
`data.Content`, `M` denotes the A2UI marker `$$a2ui:`.

| Condition | Action |
|-----------|--------|
| `data.Done && C === ""` | **Sentinel**: mark every streaming assistant message `isStreaming: false`; set `sessionState` to `idle`. (Existing behaviour — unchanged.) |
| `C.startsWith(M)` | **A2UI boundary** (REQ-001/REQ-002): mark every streaming assistant message `isStreaming: false`; append a NEW assistant bubble with `content = C`, `isStreaming = !data.Done`, `cpnId = data.CPNID`, `cpnRole = data.CPNRole`. Do NOT look up an existing bubble. |
| `!C.startsWith(M)` AND a streaming bubble exists with matching `CPNID` whose content ALREADY starts with `M` | **Post-A2UI chunk** (REQ-003): mark that bubble `isStreaming: false`; append a NEW assistant bubble with `content = C`. This prevents a trailing non-marker chunk from polluting a complete A2UI payload. |
| Otherwise | **Default append** (existing behaviour — unchanged): find streaming assistant bubble with matching `CPNID`; concatenate `C` to its `content`; set `isStreaming: !data.Done`. If none found, create a new bubble. |

Reference pseudocode:

```ts
const A2UI_MARKER = '$$a2ui:';

function isA2UIChunk(content: string): boolean {
  return content.startsWith(A2UI_MARKER);
}

function chatReducer(state, action) {
  if (action.type !== 'STREAM_CHUNK') return /* existing branches */;
  const { data } = action;

  // 1. Done sentinel (unchanged)
  if (data.Done && !data.Content) {
    return {
      ...state,
      sessionState: 'idle',
      messages: state.messages.map(m => m.isStreaming ? { ...m, isStreaming: false } : m),
    };
  }

  // 2. A2UI boundary (NEW — REQ-001/REQ-002)
  if (isA2UIChunk(data.Content)) {
    return {
      ...state,
      sessionState: data.Done ? 'idle' : 'running',
      messages: [
        ...state.messages.map(m => m.isStreaming ? { ...m, isStreaming: false } : m),
        newAssistantBubble(data),
      ],
    };
  }

  // 3. Post-A2UI follow-on (NEW — REQ-003)
  const a2uiIdx = state.messages.findIndex(
    m => m.role === 'assistant' && m.isStreaming &&
         m.cpnId === data.CPNID && isA2UIChunk(m.content)
  );
  if (a2uiIdx >= 0) {
    const updated = [...state.messages];
    updated[a2uiIdx] = { ...updated[a2uiIdx], isStreaming: false };
    updated.push(newAssistantBubble(data));
    return { ...state, messages: updated, sessionState: data.Done ? 'idle' : 'running' };
  }

  // 4. Default append (unchanged)
  // ... existing behaviour
}
```

### 4.2 Frontend — `MessageBubble.parsePayload` contract

```ts
// Leading-whitespace-tolerant marker detection (REQ-004).
// Fall-through to text renderer on missing marker OR JSON parse failure (REQ-005).
function parsePayload(content: string, isStreaming: boolean): A2UIPayload {
  const trimmed = content.replace(/^[\s]+/, '');
  if (trimmed.startsWith(A2UI_MARKER)) {
    try {
      return JSON.parse(trimmed.slice(A2UI_MARKER.length));
    } catch {
      // partial or malformed — fall through
    }
  }
  return {
    components: content ? [{ type: 'text', props: { content, isStreaming } }] : [],
  };
}
```

### 4.3 Backend — `fireHITL` persistence append

```go
// back/go-assistant/cpn/hitl.go (inside fireHITL, replacing lines 92-128)

const A2UIMarker = "$$a2ui:" // NEW package-level constant (GUD-002)

if cfg.A2UIPayloadBuilder != nil {
    if payload, err := cfg.A2UIPayloadBuilder(consumed); err == nil && payload != nil {
        if encoded, mErr := json.Marshal(payload); mErr == nil {
            content := A2UIMarker + string(encoded)

            c.StreamedOutput = true
            c.emit(&Event{
                Type:           EventStreamChunk,
                TransitionID:   t.ID,
                TransitionKind: NodeKindHITL,
                Payload: StreamChunk{
                    SessionID: c.SessionID,
                    CPNID:     c.ID,
                    CPNRole:   c.Role,
                    Content:   content,
                    Done:      false,
                },
            })

            // Done sentinel (REQ-009, unchanged)
            c.emit(&Event{
                Type:           EventStreamChunk,
                TransitionID:   t.ID,
                TransitionKind: NodeKindHITL,
                Payload: StreamChunk{
                    SessionID: c.SessionID,
                    CPNID:     c.ID,
                    CPNRole:   c.Role,
                    Content:   "",
                    Done:      true,
                },
            })

            // NEW (REQ-007): persist the surface via c.History so the existing
            // history-sync path in session_service.go:293-307 promotes it into
            // session.Messages and persistAfterRun writes it to the DB.
            c.mu.Lock()
            c.History = append(c.History, &Message{
                Role:      RoleAssistant,
                Content:   content,
                CPNID:     c.ID,
                CPNRole:   c.Role,
                CPNDepth:  c.Depth,
                Timestamp: time.Now(),
            })
            c.mu.Unlock()

            customSurface = true
        }
    }
}
```

### 4.4 Persisted `messages.content` — wire shape

No schema change. The `messages.content` column continues to store the full
`StreamChunk.Content` verbatim. After this fix, persisted rows for HITL
surfaces will contain strings of the form:

```
$$a2ui:{"components":[{"type":"questionnaire","props":{...},"children":[...]}]}
```

This is the identical byte sequence that live clients receive over SSE, so the
frontend path (REQ-006) does not need to distinguish live vs rehydrated
rendering.

### 4.5 Session rehydration path

`GET /api/v1/sessions/{id}` continues to return each message's `content` field
as-is (see `handler_session.go`, `MessageResponse.Content = m.Content`).
`useChat.ts` `SESSION_LOADED` maps `m.content → ChatMessage.content` unchanged.
`MessageBubble.parsePayload` then handles routing per §4.2. No new interface.

## 5. Acceptance Criteria

### Live streaming (no prior text on bubble)

- **AC-001**: Given a fresh session with no prior assistant messages,
  When the backend emits an A2UI `StreamChunk` with
  `Content = "$$a2ui:{...valid JSON...}"` and `Done = false` followed by a
  Done sentinel,
  Then the frontend MUST render a single interactive A2UI surface
  (e.g. questionnaire) and MUST NOT render any markdown text bubble for that
  chunk.

### Live streaming (prior streaming text on the same CPN)

- **AC-002**: Given a CPN that has streamed N text chunks to an assistant
  bubble (`isStreaming = true`, `cpnId = X`) without yet emitting a Done
  sentinel,
  When a chunk with `Content = "$$a2ui:{...}"` and `cpnId = X` arrives,
  Then the frontend MUST mark the existing bubble `isStreaming = false` (close
  it) AND MUST render the A2UI payload as a new, separate assistant bubble
  containing the interactive surface.
- **AC-003**: Given the state in AC-002 after the A2UI bubble is created,
  When a further non-marker chunk with `cpnId = X` arrives before the Done
  sentinel,
  Then the frontend MUST NOT append that chunk to the A2UI bubble's content;
  it MUST create yet another new assistant bubble.

### Rendering & KaTeX coexistence

- **AC-004**: Given a persisted or streaming message whose `content` starts
  with `$$a2ui:` and parses as valid JSON,
  When rendered,
  Then `MessageBubble` MUST route it to the A2UI renderer and the markdown
  pipeline (including `remark-math` / `rehype-katex`) MUST NOT receive the
  `$$a2ui:` payload.
- **AC-005**: Given content that contains the substring `$$a2ui:` NOT at the
  start (after whitespace trim) — e.g. an LLM response that literally mentions
  the marker —
  When rendered,
  Then `MessageBubble` MUST route it to the markdown renderer (existing
  fallback behaviour preserved).

### Backend persistence

- **AC-006**: Given `fireHITL` successfully emits an A2UI `StreamChunk`,
  When `persistAfterRun` runs for the enclosing execution,
  Then exactly ONE row MUST exist in the `messages` table for that surface,
  with `role = 'assistant'`, `content` starting with `$$a2ui:`, `cpn_id = c.ID`,
  and no duplicate row emitted by the terminal-places fallback path.
- **AC-007**: Given `fireHITL` where `A2UIPayloadBuilder` returns
  `(nil, nil)` or `(_, err != nil)` or where `json.Marshal` fails,
  When the HITL flow proceeds,
  Then no `$$a2ui:` row MUST be appended to `c.History` and the transition
  MUST still block on the channel for human input (existing non-fatal
  behaviour preserved).

### Session reload

- **AC-008**: Given a session in which `t-clarify` persisted an A2UI
  questionnaire and is still waiting on HITL,
  When the user reloads the page and `GET /api/v1/sessions/{id}` returns the
  message history,
  Then the A2UI questionnaire MUST render as an interactive surface (not as
  text), with the same `componentId` (the HITL transition id), so that
  submitting answers resolves the still-blocked HITL via the existing
  `POST /sessions/{id}/hitl/{transitionId}` endpoint.

### Regression guard

- **AC-009**: Existing non-A2UI assistant messages (pure markdown, including
  LaTeX math in `$$...$$` delimiters) MUST continue to render through
  `MarkdownContent` with KaTeX unchanged. The frontend test suite's existing
  `MessageBubble.test.tsx` math-rendering snapshots MUST pass without
  modification.
- **AC-010**: The legacy review-card A2UI emission path in
  `cmd/server/main.go:267-292` (triggered when `transitionOwnsSurface == false`)
  MUST continue to work. Its A2UI chunk also starts with `$$a2ui:`, so
  REQ-001/REQ-002 apply uniformly and the review card MUST render correctly.

## 6. Test Automation Strategy

### Test levels & locations

| Level | Backend | Frontend |
|-------|---------|----------|
| Unit | `cpn/hitl_test.go` — assert `c.History` append after A2UI emit; assert single append across success/failure branches | `src/hooks/useChat.test.ts` — new tests for reducer branches in §4.1 (AC-001/002/003) |
| Unit | `cpn/hitl_test.go` — assert Done-sentinel unchanged (REQ-009) | `src/features/chat/MessageBubble.test.tsx` — extend with whitespace-tolerant marker (REQ-004), non-leading marker fallback (AC-005) |
| Integration | `internal/app/session_service_test.go` (or existing rehydrate test) — persist + reload asserting the `$$a2ui:` row survives and appears in `GetSession` response | `src/features/chat/ChatContainer.test.tsx` or similar — simulate SSE flow producing AC-002 scenario; assert exactly two bubbles (closed text + A2UI surface) |
| Integration | HTTP handler test for `GET /sessions/{id}` returning messages whose `content` starts with `$$a2ui:` verbatim | Session rehydration test (mock `getSession`) asserting an `$$a2ui:` message renders via A2UI renderer (AC-008) |

### Frameworks

- Backend: Go standard `testing`, table-driven where applicable.
- Frontend: Vitest + React Testing Library. SSE events simulated by directly
  dispatching `STREAM_CHUNK` actions via the existing reducer harness;
  component tests drive `MessageBubble` with crafted `content` fixtures.

### Test data

- A2UI payload fixtures MUST be the actual `t-clarify` shape (questionnaire
  with ≥1 `choice` children) to exercise real `JSON.parse` size/shape.
- LaTeX math fixtures (`$$x^2 + y^2 = z^2$$`) MUST be included in the
  regression set to prove AC-009.

### CI

- All new and updated tests MUST run in the existing `go test ./...` and
  `npm run test` pipelines. No new jobs are introduced.

### Coverage expectations

- `cpn/hitl.go` A2UI branch: 100% line + branch coverage across
  success / `payload == nil` / `Marshal` error paths.
- `useChat.ts` reducer: every §4.1 table row MUST have at least one dedicated
  test.
- `MessageBubble.parsePayload`: whitespace, missing-marker, and parse-failure
  branches each MUST be tested (AC-004 / AC-005).

## 7. Rationale & Context

### Why a boundary-based fix instead of changing the marker

The protocol (parent spec §4.6) uses `$$a2ui:` explicitly and the constant is
already shared between backend (`hitl.go:104`), frontend
(`MessageBubble.tsx:7`), tests, and dev handlers. Changing it would require
coordinated migration across the A2A adapter, every emission site, every test,
and any external agent that consumes the marker via the published API. A
marker change is disproportionate to a two-seam integration bug.

Treating `$$a2ui:` as an implicit message boundary (PAT-001) is additive: it
relies on an invariant the backend already upholds (the marker only appears at
the start of its own dedicated chunk) and requires zero wire-format change.

### Why persist via `c.History` instead of a new persistence path

`session_service.go:293-307` already syncs `c.History` into `session.Messages`
on every run completion, and `persistAfterRun` writes those messages to the DB.
Appending to `c.History` in `fireHITL` (REQ-007) is the smallest change that
reuses this pipeline. Introducing a parallel persistence call from `hitl.go`
would violate CON-006 (CPN domain purity) and create two places that can
diverge.

### Why not buffer split A2UI payloads now

The parent spec §12.6 explicitly marks split-chunk buffering as an open item
pending tuning against real `t-clarify` payloads. Today the backend emits the
payload atomically (CON-004) so the practical failure mode is concatenation
with prior text, not split reconstruction. REQ-003 adopts the minimum
invariant needed for correctness without pre-committing to a buffering
strategy.

### Why the KaTeX collision is left untouched

`remark-math` + `rehype-katex` are load-bearing for assistant-authored math
content, which is a product feature independent of A2UI. The fix restores the
routing invariant (A2UI never reaches the markdown pipeline) rather than
removing the math plugins. This keeps the two features decoupled.

### Why `MessageBubble.parsePayload` trims leading whitespace

Session rehydration from persistence, message splicing across reload, and
certain LLM stream sequences can occasionally produce a leading newline
before the marker. REQ-004 is a small hardening that removes a class of
accidental fallbacks at essentially zero cost.

## 8. Dependencies & External Integrations

### Internal dependencies

- **INT-001**: `cpn/hitl.go` `fireHITL` — modified per §4.3 (REQ-007).
- **INT-002**: `cpn.Message`, `cpn.RoleAssistant`, `cpn.CPN.History` — used
  unchanged to append the A2UI surface record.
- **INT-003**: `internal/app/session_service.go` history-sync loop
  (`session_service.go:293-307`) — relied upon to promote `c.History` entries
  into `session.Messages`. No modification required.
- **INT-004**: `persist.MessageToRecord` and `Sessions.AppendMessage` — relied
  upon to persist. No modification required.
- **INT-005**: `front/react-assistant/src/hooks/useChat.ts` — reducer modified
  per §4.1.
- **INT-006**: `front/react-assistant/src/features/chat/MessageBubble.tsx` —
  `parsePayload` hardened per §4.2.

### External systems

- **EXT-001**: Postgres `messages` table — relied upon to store
  `content TEXT NOT NULL` verbatim. No schema migration is introduced.

### Third-party services

- **SVC-001**: None. The fix is contained to the BRAE backend and React
  frontend.

### Infrastructure dependencies

- **INF-001**: None. No new processes, queues, or storage.

### Data dependencies

- **DAT-001**: None. The fix does not add new data sources; it reuses the
  existing `messages` row shape.

### Technology platform dependencies

- **PLT-001**: React 19 + `remark-math` + `rehype-katex` (existing) — the fix
  is designed to coexist with these unchanged.
- **PLT-002**: Go 1.22+ (existing) — no version bump.

### Compliance dependencies

- **COM-001**: None. No PII, auth, or audit-log semantics change.

## 9. Examples & Edge Cases

### 9.1 Happy-path streaming sequence (AC-001)

```
SSE stream (frontend-observed order):
  stream_chunk  Content='$$a2ui:{"components":[{"type":"questionnaire",...}]}', Done=false, CPNID=c1
  stream_chunk  Content='',                                                     Done=true,  CPNID=c1

Reducer outcome:
  messages = [{ role: 'assistant', content: '$$a2ui:{"components":[...]}', isStreaming: false, cpnId: c1 }]

Render: A2UIRenderer displays questionnaire.
```

### 9.2 A2UI after a streaming LLM on the same CPN (AC-002)

```
SSE stream:
  stream_chunk  Content='Here is my take:', Done=false, CPNID=c1
  stream_chunk  Content=' a summary...',    Done=false, CPNID=c1
  stream_chunk  Content='$$a2ui:{...}',     Done=false, CPNID=c1
  stream_chunk  Content='',                 Done=true,  CPNID=c1

Reducer outcome (after 3rd chunk):
  messages = [
    { role: 'assistant', content: 'Here is my take: a summary...', isStreaming: false, cpnId: c1 },
    { role: 'assistant', content: '$$a2ui:{...}',                  isStreaming: true,  cpnId: c1 },
  ]

Render: markdown text bubble + A2UI questionnaire.
```

### 9.3 Literal `$$a2ui:` string inside an LLM text response (AC-005)

```
SSE stream:
  stream_chunk  Content='As an example, the marker `$$a2ui:` is used for...', Done=false, CPNID=c1

Reducer outcome:
  messages = [{ role: 'assistant', content: 'As an example, the marker `$$a2ui:` is used for...', ... }]

Render: markdown pipeline (the marker is not at the start, after whitespace trim).
```

### 9.4 Page reload mid-HITL (AC-008)

```
1. User sends ambiguous task → t-classify → t-ask → t-clarify.
2. t-clarify emits $$a2ui: stream chunk AND appends to c.History (REQ-007).
3. history-sync promotes it to session.Messages; persistAfterRun writes row.
4. User reloads browser.
5. GET /sessions/{id} returns messages including the $$a2ui:{...} row.
6. SESSION_LOADED dispatches; MessageBubble.parsePayload detects marker; A2UI renders.
7. User submits answers; POST /sessions/{id}/hitl/t-clarify resolves the still-blocked HITL.
```

### 9.5 Marshal failure (AC-007)

```
cfg.A2UIPayloadBuilder returns (someStruct, nil) but json.Marshal fails (e.g. unsupported type).
fireHITL MUST NOT emit the EventStreamChunk, MUST NOT append to c.History, and MUST proceed to
emit EventHITLRequested with the plain-string prompt (existing backward-compatible path).
```

### 9.6 Split-chunk A2UI payload (explicitly out of scope — REQ-003)

```
Hypothetical future SSE stream:
  stream_chunk  Content='$$a2ui:{"components":[{"type":"question',   Done=false, CPNID=c1
  stream_chunk  Content='naire",...}]}',                             Done=false, CPNID=c1

Reducer behaviour per REQ-003:
  - Chunk 1 lands in a new A2UI bubble (partial JSON); JSON.parse fails in parsePayload; falls
    back to markdown (§9 of parent spec).
  - Chunk 2 is NOT concatenated onto the A2UI bubble; it starts a new bubble.
  - End result: both chunks render as degraded markdown.

This failure mode is tolerated here because the backend does not split today (CON-004).
Buffering will be added when the parent spec §12.6 open item is resolved.
```

## 10. Validation Criteria

- **V-001**: `go test ./cpn/...` passes, including new assertions that
  `fireHITL` appends exactly one `$$a2ui:` entry to `c.History` on success and
  zero on the failure branches enumerated in AC-007.
- **V-002**: `go test ./internal/app/...` passes, including a test that drives
  a session through a HITL A2UI flow and asserts the persisted row survives
  `GetSession`.
- **V-003**: `npm run test` in `front/react-assistant/` passes, including new
  reducer tests for every row of the §4.1 decision table and hardened
  `parsePayload` tests covering REQ-004 / REQ-005.
- **V-004**: Manual smoke: running the happy-path `t-clarify` flow end-to-end
  with a real LLM renders the questionnaire as an interactive surface on first
  stream; reloading the browser re-renders the same surface and submitting
  answers resolves the HITL.
- **V-005**: No new deprecation warnings, no new ESLint / golangci-lint
  findings, no diff in `rehype-katex` / `remark-math` configuration, and no
  change to `package.json` or `go.mod`.
- **V-006**: Grep audit: `$$a2ui:` literal occurrences are reduced or held
  constant (via the new constants in GUD-001 / GUD-002); no new ad-hoc sites
  are introduced.

## 11. Related Specifications / Further Reading

- [spec-architecture-a2a-a2ui-protocol-integration.md](spec-architecture-a2a-a2ui-protocol-integration.md)
  — Parent protocol spec. §4.6 defines `A2UI_MARKER`; §9 defines partial-payload
  fallback; §12 defines the `questionnaire` / `choice` components and
  `t-clarify` flow; §12.6 lists the split-chunk buffering open item this fix
  defers.
- [spec-architecture-block20-llm-streaming.md](spec-architecture-block20-llm-streaming.md)
  — `StreamChunk` semantics and Done-sentinel conventions referenced by
  REQ-009.
- [spec-design-sse-streaming-bugfixes.md](spec-design-sse-streaming-bugfixes.md)
  — Prior SSE broker correctness work; this spec does not modify the broker
  but shares its invariants.
- [spec-process-bugfix-ghost-session-rehydration.md](spec-process-bugfix-ghost-session-rehydration.md)
  — Related rehydration fix; AC-008 here depends on the rehydration pipeline
  it established.
