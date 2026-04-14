---
title: "Bug Fix — A2UI HITL Surface Survives Session Rehydration (Chat Switch / Logout-Login)"
version: 1.1
date_created: 2026-04-13
last_updated: 2026-04-13
owner: liwaisi-tech
tags: [process, bugfix, a2ui, hitl, rehydration, persistence, sse, cpn, frontend, backend]
supersedes: spec-process-bugfix-a2ui-chunk-boundary-and-persistence.md
---

## Changelog

- **1.1 (2026-04-13)**: Live forensic investigation against the running Postgres
  revealed that the `t-ask` LLM transition persists its raw JSON questionnaire
  output to `c.History` (via `fire_llm.go:225`), which is what rehydrated clients
  actually render. REQ-001 alone is therefore insufficient: without also
  suppressing the `t-ask` history contribution, the garbage JSON row will remain
  visible above the clean `$$a2ui:` row on reload. Added **REQ-016** requiring
  `t-ask` to run with `SkipHistory: true`, mirroring the existing `t-classify`
  precedent at `cmd/server/topologies.go:348`. Added **AC-016** and **INV-004**
  to guard the invariant. Frontend REQ-008..012 landed in commit `baa59f5` and
  fix the live-stream case; this revision narrows the remaining work to two
  backend changes plus one backfill decision.
- **1.0 (2026-04-13)**: Initial spec, superseding
  `spec-process-bugfix-a2ui-chunk-boundary-and-persistence.md`.

# Introduction

A user with a pending HITL (Human-in-the-Loop) A2UI surface in a chat loses
the surface whenever the frontend client is not the exact same connection
that received the original live SSE emission. Concretely: switching from
chat A to chat B (where B has a pending HITL), or logging out and back in
to chat B, renders chat B as a truncated conversation whose composer is
disabled ("Esperando respuesta…") with no HITL form visible. The CPN monitor
still shows the HITL transition alive — the net is correctly paused waiting
for human input — but the interactive surface that the human needs in order
to unblock it is gone from the DOM.

Two seams conspire to cause this:

1. **Backend persistence seam.** `fireHITL` in `back/go-assistant/cpn/hitl.go`
   emits the `$$a2ui:` payload via `EventStreamChunk` to the SSE broker but
   does NOT append it to `c.History`. `persistAfterRun`
   (`internal/app/session_service.go:327`) only promotes `c.History` entries
   into the `messages` table. Therefore the A2UI HITL row is never written to
   the database.
2. **SSE replay seam.** The SSE handler
   (`internal/driving/httpapi/handler_sse.go:24-148`) subscribes reconnecting
   clients to the broker for NEW chunks only. Historical emissions are gone.
   `spec-architecture-http-sse-api.md:724` explicitly defers replay to a
   future phase.

There is also a related frontend correctness gap on the reducer side: the
`STREAM_CHUNK` reducer in `front/react-assistant/src/hooks/useChat.ts`
concatenates A2UI chunks onto any prior streaming bubble sharing the same
`CPNID`, breaking marker detection in
`front/react-assistant/src/features/chat/MessageBubble.tsx`. This gap is
adjacent to the rehydration bug (it also breaks live streaming) and is kept
in this specification to deliver a single, complete fix.

This specification **supersedes**
`spec-process-bugfix-a2ui-chunk-boundary-and-persistence.md` (written
2026-04-13, not yet implemented). Requirements and acceptance criteria from
the predecessor are consolidated here with the rehydration scenario
promoted to the primary lens. The predecessor file SHOULD be left in place
as history; any future reader is directed to this document via the `supersedes`
frontmatter key.

Commit `ff419ca` (`fix(session): rehydrate sessions from persistence after
backend restart`, 2026-04-13) restored in-memory session rehydration from
the DB but did not, and could not, address A2UI surface persistence because
the surface was never written to the DB in the first place.

## 1. Purpose & Scope

**Purpose**: Make A2UI HITL surfaces survive every session rehydration
scenario — chat switch, page reload, logout/login, backend restart — by
persisting the surface into session history at the single emission site
(`fireHITL`) and rendering the persisted row through the A2UI pipeline on
reload. Fix the adjacent frontend chunk-boundary defect so both live
streaming and history replay produce identical DOM.

**In scope**:
- Backend: persist A2UI HITL surface to `c.History` at emission in
  `cpn/hitl.go` so `persistAfterRun` promotes it to the `messages` table
  (REQ-001).
- Backend: flip `t-ask` LLM transition to `SkipHistory: true` in
  `cmd/server/topologies.go` to stop persisting the raw questionnaire
  JSON that currently pollutes rehydrated chats (REQ-016, added in v1.1).
- Backend: extract the `$$a2ui:` literal to a package-level constant in
  `cpn` (REQ-005).
- Backend: preserve the `$$a2ui:` prefix verbatim through the full
  persistence pipeline (in-memory `c.History` → `session.Messages` →
  `messages.content` column → `GET /api/v1/sessions/{id}` response →
  `MessageResponse.content`).
- Frontend (**landed in commit `baa59f5`, v1.1**): `$$a2ui:` chunk
  boundary in the reducer, whitespace-tolerant marker detection, shared
  constant module. Retained in this spec as the documented contract;
  no further implementation required for the frontend slice.
- Regression tests covering: chat switch, logout/login, page reload,
  backend restart, happy-path live streaming, non-A2UI markdown
  (including `$$…$$` LaTeX math via `rehype-katex`), and INV-004
  (exactly one `$$a2ui:` row, zero `t-ask` rows per clarify cycle).

**Out of scope**:
- Changing the `$$a2ui:` marker string.
- Changing the HITL resolve HTTP contract
  (`POST /api/v1/sessions/{id}/hitl/{transitionId}` with `{action, content}`).
- Introducing SSE replay with `Last-Event-ID`. Rehydration remains:
  **REST history → render → subscribe to SSE for new events only**.
- Buffering of split A2UI payloads across multiple chunks (the backend emits
  atomically today; split-chunk reconstruction is tracked as
  spec-architecture-a2a-a2ui-protocol-integration.md §12.6).
- Changes to `remark-math` / `rehype-katex` configuration in
  `MarkdownContent.tsx`.
- Changes to the A2A driving adapter (`internal/driving/a2a/`).
- DB schema migrations. `messages.content TEXT` already stores the marker
  verbatim.

**Audience**: Backend Go engineers modifying `cpn/hitl.go`,
`internal/app/session_service.go`, and handler tests; frontend React
engineers modifying `hooks/useChat.ts`, `features/chat/MessageBubble.tsx`,
and associated tests.

**Assumptions**:
- Backend emits each A2UI HITL surface atomically in a single
  `StreamChunk.Content`. Verified at `cpn/hitl.go:96-107`.
- `persist.MessageToRecord` → `Sessions.AppendMessage` → Postgres
  `messages.content TEXT` preserves the `$$a2ui:` prefix byte-identically
  (no escaping, no trim, no collation coercion). Verified by reading the
  existing persistence code path.
- The frontend's `MessageResponse` shape
  (`front/react-assistant/src/types/api.ts`) passes `content` through
  unchanged from the HTTP response to the `ChatMessage` object.
- The existing `GET /api/v1/sessions/{id}` handler
  (`internal/driving/httpapi/handler_session.go:174`) returns
  `MessageResponse.Content = m.Content` without transformation.

## 2. Definitions

| Term | Definition |
|------|-----------|
| **A2UI** | Agent-to-UI protocol. See `spec-architecture-a2a-a2ui-protocol-integration.md`. |
| **A2UI marker** | The literal prefix `$$a2ui:` that precedes a JSON payload in a `StreamChunk.Content`. |
| **A2UI surface** | A rendered A2UI component tree — today, a `questionnaire` with `choice` children emitted by the `t-clarify` HITL transition. |
| **HITL** | Human-in-the-Loop. A CPN transition that blocks until a human responds. |
| **HITL surface** | The A2UI surface emitted by a HITL transition to collect human input. |
| **CPN** | Coloured Petri Net. The backend orchestration primitive (`back/go-assistant/cpn/`). |
| **Chunk boundary** | A point in the SSE stream at which the frontend reducer MUST start a fresh assistant bubble instead of appending to an existing streaming bubble. |
| **Rehydration** | Reconstruction of client-side chat state from backend persistence after the live SSE connection was not established, was dropped, or belongs to a different session than the currently-viewed one. Triggers: chat switch, page reload, logout/login, backend restart. |
| **Live-stream path** | The render pathway where an assistant message is materialized from consecutive `stream_chunk` SSE events. |
| **History-replay path** | The render pathway where an assistant message is materialized from `MessageResponse` rows returned by `GET /api/v1/sessions/{id}`. |
| **`c.History`** | The in-memory `[]*Message` slice on `cpn.CPN` that `session_service.go` syncs into `session.Messages()` and then persists via `persistAfterRun`. |
| **`$$a2ui:` prefix preservation** | The invariant that the marker is transported byte-identical from `fireHITL` emission through to the frontend reducer for both live-stream and history-replay paths. |

## 3. Requirements, Constraints & Guidelines

### Backend — A2UI persistence

- **REQ-001**: `fireHITL` in `back/go-assistant/cpn/hitl.go` MUST, after
  successfully emitting an A2UI `EventStreamChunk`, append a `RoleAssistant`
  `Message` to `c.History` whose `Content` is the full `"$$a2ui:" + json`
  payload (byte-identical to the chunk just emitted) and whose `CPNID`,
  `CPNRole`, and `CPNDepth` match the emitting CPN.
- **REQ-002**: The append in REQ-001 MUST occur only on the success branch
  where `A2UIPayloadBuilder` returned a non-nil payload, `json.Marshal`
  succeeded, and the `EventStreamChunk` was emitted. Failures MUST remain
  non-fatal: the HITL request proceeds without an A2UI surface and nothing
  is appended to `c.History`.
- **REQ-003**: The A2UI surface message appended in REQ-001 MUST NOT be
  duplicated by downstream paths. Specifically:
  - `session_service.go:293-307` (history sync after run) already copies
    `c.History` into `session.Messages`; no additional path is required.
  - `session_service.go:349-358` (terminal-places fallback when streaming
    was not active) MUST NOT emit the A2UI message again. This branch runs
    only when `streamingActive == false`; `fireHITL` sets
    `c.StreamedOutput = true`, so the guard already holds. Regression
    tests MUST assert no duplicate row.
- **REQ-004**: The Done-sentinel emission currently in `fireHITL` (empty
  `Content`, `Done = true`) MUST remain unchanged. It continues to act as
  an SSE-level boundary for live clients that connected before REQ-008
  landed.
- **REQ-005**: The `$$a2ui:` literal in the backend MUST be expressed as a
  single package-level constant in `cpn` (e.g. `const A2UIMarker =
  "$$a2ui:"`). `fireHITL` MUST use this constant for both the
  `StreamChunk.Content` emission and the `c.History` append. Any other
  backend code that compares against the marker (e.g. tests) MUST reference
  the same constant.

### Backend — Rehydration contract

- **REQ-006**: `GET /api/v1/sessions/{id}`
  (`internal/driving/httpapi/handler_session.go`) MUST return
  `MessageResponse.content` verbatim from the persisted `messages.content`
  column. No trimming, no transformation, no marker stripping.
- **REQ-007**: The rehydration path added in commit `ff419ca`
  (`SessionService.GetSession` → `persist.LoadSessionWithMessages`) MUST
  preserve persisted `$$a2ui:` rows as ordinary `cpn.Message` values on
  `c.History`. No special-case handling is required; the A2UI payload is
  opaque to rehydration.

### Frontend — Chunk boundary (live stream)

- **REQ-008**: The chat reducer (`useChat.ts`, `STREAM_CHUNK` case) MUST
  treat any incoming chunk whose `Content` starts with the A2UI marker as a
  **new assistant bubble**. The reducer MUST NOT append such a chunk to any
  existing streaming bubble, regardless of matching `CPNID`.
- **REQ-009**: When the reducer creates the new bubble for a
  marker-prefixed chunk, it MUST first mark every currently streaming
  assistant message as `isStreaming: false` to close out any in-flight
  bubble that belongs to an earlier transition. This mirrors the existing
  `Done && !Content` sentinel behaviour.
- **REQ-010**: The reducer MUST NOT buffer partial A2UI JSON across
  multiple chunks. If a `$$a2ui:` chunk is followed by a non-marker chunk
  on the same `CPNID` before a `Done` sentinel, the reducer MUST treat the
  follow-on chunk as a new assistant bubble (no concatenation onto the A2UI
  bubble). Split-payload buffering is deferred to
  `spec-architecture-a2a-a2ui-protocol-integration.md` §12.6.

### Frontend — A2UI detection & history replay

- **REQ-011**: `MessageBubble.parsePayload` MUST detect the A2UI marker
  after trimming leading ASCII whitespace (`\u0020`, `\t`, `\r`, `\n`).
  Content with leading whitespace followed by `$$a2ui:` and valid JSON MUST
  route to the A2UI renderer.
- **REQ-012**: `MessageBubble.parsePayload` MUST continue to fall through
  to the markdown text component when (a) the trimmed content does not
  start with `$$a2ui:`, or (b) the JSON after the marker fails to parse.
- **REQ-013**: Session rehydration in `useChat.ts` (the `SESSION_LOADED`
  path that maps `session.messages` into `ChatMessage` objects) MUST
  preserve `content` verbatim. A persisted message whose `content` starts
  with `$$a2ui:` MUST render via the A2UI renderer on reload, not via
  markdown.
- **REQ-014**: Switching chats MUST NOT carry over in-memory chunk buffers,
  streaming flags, or partial A2UI state from the previous chat. Each chat
  switch MUST: (a) abort any active SSE subscription, (b) clear the
  reducer's message list, (c) fetch history via `GET /api/v1/sessions/{id}`,
  (d) dispatch `SESSION_LOADED` with the fetched messages, (e) subscribe to
  SSE for new events only. Current behaviour MAY already implement this —
  verify in acceptance.

### Frontend — Logout/login

- **REQ-015**: On logout, the reducer state MUST be fully reset (empty
  messages, `sessionState: 'idle'`, no lingering streaming bubbles). On
  subsequent login and chat open, rehydration follows REQ-014.

### Backend — Suppress `t-ask` raw JSON in history

- **REQ-016**: The `t-ask` LLM transition (declared in
  `back/go-assistant/cmd/server/topologies.go`, around line 498) MUST run
  with `LLMConfig.SkipHistory = true`. The raw JSON questionnaire produced
  by `t-ask` is routing metadata consumed by `t-clarify`'s
  `A2UIPayloadBuilder` and `buildClarifiedToken` — it has no conversational
  value for downstream transitions (`t-plan`, `t-execute`) because the
  user's answers are merged into the downstream token via `OutputBuilder`.
  Persisting the raw JSON (the current behaviour) causes it to render as a
  fallback markdown bubble above the `$$a2ui:` surface on rehydration,
  which is the user-visible defect this revision (1.1) closes.

  The flip from `SkipHistory: false` (current, `topologies.go:504`) to
  `SkipHistory: true` mirrors the established precedent at `t-classify`
  (`topologies.go:346-348`), which runs `RequireJSON: true, SkipHistory:
  true` for the same reason: its JSON output is routing-only.

- **REQ-017**: The inline comment at `topologies.go:504-507` MUST be
  updated to reflect REQ-016's rationale. The existing comment about
  `SkipRegionalPreamble` remains accurate and stays.

### Invariants

- **INV-001**: `$$a2ui:` prefix preservation. For any HITL A2UI surface
  emitted by `fireHITL`, the byte sequence that reaches the frontend
  `MessageBubble.parsePayload` MUST be identical whether it arrived via the
  live SSE stream or via `GET /api/v1/sessions/{id}`. Byte-identical means:
  same `$$a2ui:` prefix, same JSON bytes, no normalization.
- **INV-002**: Single persistence site. The A2UI HITL surface is written to
  the DB exactly once per HITL emission, via the `c.History` →
  `session.Messages` → `persistAfterRun` pipeline. No other code path
  writes A2UI content to `messages`.
- **INV-003**: Live-stream and history-replay paths converge on the same
  DOM. A bubble rendered from a live `stream_chunk` and a bubble rendered
  from a persisted `MessageResponse` with identical `content` MUST produce
  identical React output.
- **INV-004**: For a single `t-clarify` HITL cycle, the `messages` table
  MUST contain exactly ONE assistant row whose `cpn_id` is the HITL
  transition (or its surrounding CPN), and whose `content` starts with
  `$$a2ui:`. It MUST NOT contain any assistant row whose `cpn_id` is
  `t-ask` (upstream JSON questionnaire generator), because `t-ask` runs
  with `SkipHistory: true` per REQ-016. This invariant is the concrete
  guarantee that rehydration does not surface the raw questionnaire JSON.

### Constraints

- **CON-001**: The `$$a2ui:` marker string MUST NOT change. It is the
  protocol contract in
  `spec-architecture-a2a-a2ui-protocol-integration.md` §4.6 and is shared
  across backend, frontend, tests, and any external agent.
- **CON-002**: The HITL resolve wire shape (`POST /api/v1/sessions/{id}/hitl/{transitionId}`
  with `{action, content}` where `action ∈ {approve, reject, revise,
  submit}`) MUST NOT change.
- **CON-003**: `remark-math` and `rehype-katex` in `MarkdownContent.tsx`
  MUST remain configured as they are today. The fix MUST work without
  removing or reconfiguring them.
- **CON-004**: The backend MUST continue to emit the A2UI payload
  atomically in a single `StreamChunk.Content`. Split-chunk buffering is
  out of scope (see REQ-010).
- **CON-005**: No new HTTP endpoints, SSE event types, `MessageResponse`
  fields, or `ResolveHITLRequest` fields are introduced.
- **CON-006**: The CPN domain layer (`cpn/`) MUST NOT import anything from
  `internal/app/` or `internal/driving/`. REQ-001 is satisfied by mutating
  `c.History` — the existing pre-established channel between the CPN and
  the app layer.
- **CON-007**: No DB schema migration. `messages.content TEXT NOT NULL`
  stores the full `$$a2ui:...` string as-is.
- **CON-008**: No SSE replay (no `Last-Event-ID` resume, no ring buffer).
  Rehydration is REST-driven.

### Guidelines

- **GUD-001**: On the frontend, extract or import the A2UI marker as a
  single shared constant so the literal appears exactly once in
  application code. If `MessageBubble.tsx` already exports `A2UI_MARKER`,
  reuse it from the reducer (refactor to a tiny shared module if needed to
  avoid a circular import).
- **GUD-002**: Keep the reducer change surgical. A full redesign (out-of-
  order chunk handling, reassembly) is out of scope; REQ-008/009/010 are
  sufficient.
- **GUD-003**: On the backend, extract `$$a2ui:` to a package constant in
  `cpn` per REQ-005 and reference it from every emission site. This also
  hardens `cpn/hitl_test.go` assertions against drift.
- **GUD-004**: Do not attempt to detect A2UI content at arbitrary offsets
  inside a chunk. The marker is authoritative only at offset 0 after
  leading-whitespace trim.
- **GUD-005**: Prefer adding regression tests that exercise the
  history-replay path by round-tripping an `$$a2ui:` row through Postgres
  (integration test with real DB or testcontainer) rather than mocking the
  persistence layer. The bug was invisible to mocks.

### Patterns

- **PAT-001**: **Marker as boundary.** `$$a2ui:` is an implicit message
  terminator for the preceding bubble and an implicit opener for the new
  bubble. This is the minimum semantics that prevents concatenation
  without introducing a new protocol field.
- **PAT-002**: **Single-site persistence.** The A2UI surface is persisted
  by appending to `c.History` at the emission site (`fireHITL`), reusing
  the existing history-sync pipeline. No new persistence call is added.
- **PAT-003**: **Convergent render pipelines.** Live-stream and
  history-replay paths feed identical content into
  `MessageBubble.parsePayload`. The component does not know which path it
  came from.

## 4. Interfaces & Data Contracts

### 4.1 Frontend reducer — `STREAM_CHUNK` decision table

`C` denotes `data.Content`; `M` denotes the A2UI marker `$$a2ui:`.

| Condition | Action |
|-----------|--------|
| `data.Done && C === ""` | **Done sentinel** (unchanged): mark every streaming assistant message `isStreaming: false`; set `sessionState = 'idle'`. |
| `C.startsWith(M)` | **A2UI boundary** (REQ-008/009): close every streaming assistant message; append a NEW assistant bubble with `content = C`, `isStreaming = !data.Done`, `cpnId = data.CPNID`, `cpnRole = data.CPNRole`. Do NOT look up an existing bubble. |
| `!C.startsWith(M)` AND a streaming assistant bubble exists with matching `CPNID` whose content already starts with `M` | **Post-A2UI chunk** (REQ-010): close that bubble; append a NEW assistant bubble with `content = C`. Prevents a trailing non-marker chunk from polluting a complete A2UI payload. |
| Otherwise | **Default append** (unchanged): find streaming assistant bubble with matching `CPNID`; concatenate `C` to its `content`; set `isStreaming = !data.Done`. If none found, create a new bubble. |

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
      messages: state.messages.map(m =>
        m.isStreaming ? { ...m, isStreaming: false } : m
      ),
    };
  }

  // 2. A2UI boundary (REQ-008/009)
  if (isA2UIChunk(data.Content)) {
    return {
      ...state,
      sessionState: data.Done ? 'idle' : 'running',
      messages: [
        ...state.messages.map(m =>
          m.isStreaming ? { ...m, isStreaming: false } : m
        ),
        newAssistantBubble(data),
      ],
    };
  }

  // 3. Post-A2UI follow-on (REQ-010)
  const a2uiIdx = state.messages.findIndex(
    m => m.role === 'assistant' && m.isStreaming &&
         m.cpnId === data.CPNID && isA2UIChunk(m.content)
  );
  if (a2uiIdx >= 0) {
    const updated = [...state.messages];
    updated[a2uiIdx] = { ...updated[a2uiIdx], isStreaming: false };
    updated.push(newAssistantBubble(data));
    return {
      ...state,
      messages: updated,
      sessionState: data.Done ? 'idle' : 'running',
    };
  }

  // 4. Default append (unchanged)
  // ... existing behaviour
}
```

### 4.2 Frontend — `MessageBubble.parsePayload` contract

```ts
// Leading-whitespace-tolerant marker detection (REQ-011).
// Fall-through to text renderer on missing marker OR JSON parse failure (REQ-012).
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
// back/go-assistant/cpn/hitl.go — inside fireHITL, success branch

const A2UIMarker = "$$a2ui:" // package-level constant, per REQ-005

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

            // Done sentinel (REQ-004, unchanged)
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

            // REQ-001: persist the surface via c.History so the existing
            // history-sync path in session_service.go:293-307 promotes it
            // into session.Messages and persistAfterRun writes it to the DB.
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

### 4.4 Persisted `messages.content` wire shape

No schema change. `messages.content` continues to store the full
`StreamChunk.Content` verbatim. After this fix, persisted rows for HITL
surfaces contain strings of the form:

```
$$a2ui:{"components":[{"type":"questionnaire","props":{...},"children":[...]}]}
```

This is the identical byte sequence live clients receive over SSE. INV-001
holds.

### 4.5 Session rehydration sequence

```
User action: switch to chat X (or login, or reload)
  ↓
Frontend: GET /api/v1/sessions/{X}
  ↓
Backend: handler_session.go HandleGetSession
  ↓
Backend: SessionService.GetSession (rehydrate from DB if needed, per ff419ca)
  ↓
Backend: persist.LoadSessionWithMessages → Postgres messages table
  ↓
Backend: build SessionDetailResponse; for each row,
         MessageResponse.content = row.content (verbatim — REQ-006)
  ↓
Frontend: dispatch SESSION_LOADED with messages
  ↓
Frontend: useChat reducer sets state.messages = mapped ChatMessage[]
          (content preserved verbatim — REQ-013)
  ↓
Frontend: MessageBubble.parsePayload routes each row:
          - content starts with `$$a2ui:` → A2UIRenderer (REQ-011)
          - otherwise → MarkdownContent (REQ-012)
  ↓
Frontend: subscribe to SSE stream for session X (new events only — CON-008)
  ↓
User submits HITL via A2UI form → POST /sessions/{X}/hitl/{transitionId}
  ↓
Backend: unblocks HITL transition on cfg.Channel; CPN proceeds.
```

### 4.6 `MessageResponse` contract (unchanged)

```json
{
  "id": "string",
  "role": "user | assistant | observer",
  "content": "string — verbatim from messages.content, MAY start with $$a2ui:",
  "cpn_id": "string (optional)",
  "cpn_role": "string (optional)",
  "timestamp": "RFC 3339 string"
}
```

No new fields. The `content` field carries the A2UI marker when present.
`spec-architecture-http-sse-api.md` §4 "Message History" SHOULD be updated
in a separate doc PR to note that `content` MAY begin with `$$a2ui:` and
that the prefix MUST be preserved.

## 5. Acceptance Criteria

### Rehydration — the primary scenarios

- **AC-001**: *Chat switch with pending HITL.* Given chat A and chat B
  exist for the same user, and chat B has a pending HITL whose A2UI
  questionnaire was emitted before the switch, When the user navigates
  from chat A to chat B, Then the frontend MUST render the HITL
  questionnaire as an interactive surface inside chat B, AND submitting an
  answer MUST resolve the still-blocked HITL via `POST
  /sessions/{B}/hitl/{transitionId}`.
- **AC-002**: *Logout / login with pending HITL.* Given a user with a chat
  B that has a pending HITL, When the user logs out and logs back in and
  opens chat B, Then the HITL questionnaire MUST render as an interactive
  surface, AND submitting MUST resolve the HITL.
- **AC-003**: *Page reload with pending HITL.* Given a user viewing chat B
  with a pending HITL questionnaire rendered live, When the user reloads
  the browser, Then the questionnaire MUST continue to render as an
  interactive surface (same `componentId` = HITL transition id), AND
  submitting MUST resolve the HITL.
- **AC-004**: *Backend restart with pending HITL.* Given a chat B with a
  pending HITL persisted to Postgres, When the backend restarts and the
  frontend reconnects, Then `GET /api/v1/sessions/{B}` MUST return a
  message row whose `content` starts with `$$a2ui:`, AND the frontend MUST
  render the HITL as an interactive surface. (Note: the HITL transition
  may require re-arming on the restored CPN; that behaviour is governed by
  commit `ff419ca` and is not re-specified here.)

### Live streaming — baseline that must keep working

- **AC-005**: *A2UI on a fresh bubble.* Given a session with no prior
  assistant messages, When the backend emits a `StreamChunk` with `Content
  = "$$a2ui:{…valid JSON…}"` followed by a Done sentinel, Then the
  frontend MUST render exactly one interactive A2UI surface for that
  chunk, with no accompanying text bubble.
- **AC-006**: *A2UI after a streaming LLM on the same CPN.* Given a CPN
  that has streamed N text chunks to an assistant bubble
  (`isStreaming = true`, `cpnId = X`) without a Done sentinel, When a
  chunk with `Content = "$$a2ui:{…}"` and `cpnId = X` arrives, Then the
  frontend MUST close the existing bubble (`isStreaming = false`) AND
  render the A2UI payload as a separate, new assistant bubble.
- **AC-007**: *Post-A2UI trailing chunk.* Given the state in AC-006 after
  the A2UI bubble is created, When a further non-marker chunk with `cpnId
  = X` arrives before the Done sentinel, Then the frontend MUST NOT
  append it to the A2UI bubble; it MUST create a new assistant bubble for
  that chunk.

### Rendering routing

- **AC-008**: *Whitespace-tolerant detection.* Given a message `content`
  that begins with arbitrary leading ASCII whitespace followed by
  `$$a2ui:` and valid JSON, When rendered, Then `MessageBubble` MUST
  route it to the A2UI renderer.
- **AC-009**: *Non-leading marker falls through.* Given a message
  `content` that contains the substring `$$a2ui:` NOT at offset 0 after
  whitespace trim — e.g. an LLM that literally mentions the marker — When
  rendered, Then `MessageBubble` MUST route it to the markdown renderer.
- **AC-010**: *LaTeX math unaffected.* Given a non-A2UI assistant message
  whose content uses `$$x^2 + y^2 = z^2$$` LaTeX math delimiters, When
  rendered, Then `MarkdownContent` with `remark-math` + `rehype-katex`
  MUST continue to produce the same rendered KaTeX output as before this
  fix.

### Backend persistence invariants

- **AC-011**: *Exactly-once persistence.* Given `fireHITL` successfully
  emits an A2UI `StreamChunk`, When `persistAfterRun` runs for the
  enclosing execution, Then exactly ONE row MUST exist in the `messages`
  table with `role = 'assistant'`, `content` starting with `$$a2ui:`, and
  `cpn_id = c.ID`. The terminal-places fallback path MUST NOT produce a
  duplicate.
- **AC-012**: *Non-fatal failure branches.* Given `fireHITL` where
  `A2UIPayloadBuilder` returned `(nil, nil)`, `(_, err != nil)`, or where
  `json.Marshal` failed, When the HITL flow proceeds, Then no `$$a2ui:`
  row MUST be appended to `c.History` AND the transition MUST still block
  on its channel waiting for human input.
- **AC-013**: *Byte-identical round-trip.* Given a `StreamChunk.Content`
  emitted by `fireHITL` of the form `$$a2ui:{…}`, When it is persisted to
  `messages.content` and subsequently returned by `GET
  /api/v1/sessions/{id}` as `MessageResponse.content`, Then the returned
  string MUST be byte-identical to the emitted string. INV-001 holds.

### Chat switch / logout state hygiene

- **AC-014**: *No cross-chat bleed.* Given chat A is open with a streaming
  assistant bubble (`isStreaming = true`), When the user switches to chat
  B, Then chat B's rendered message list MUST contain zero messages from
  chat A's reducer state, AND chat B's `sessionState` MUST reflect only
  chat B's backend state after rehydration.
- **AC-015**: *Logout resets state.* Given any chat state, When the user
  logs out, Then on next login the reducer MUST start with an empty
  `messages` array and `sessionState = 'idle'`, with no lingering
  streaming bubbles.

### `t-ask` history suppression

- **AC-016**: *No raw questionnaire JSON in history.* Given a session that
  has completed the `t-classify` → `t-ask` → `t-clarify` path, When the
  `messages` table is queried for that session's assistant rows, Then:
  - Exactly ONE assistant row with `content LIKE '$$a2ui:%'` MUST exist
    for the clarify turn (INV-004, REQ-001).
  - Zero assistant rows with `cpn_id = 't-ask'` MUST exist (REQ-016).
  - Zero assistant rows whose content begins with `{` and matches the
    classifier JSON shape (e.g. contains top-level keys `restated_goal`
    AND `questions`) MUST exist.
- **AC-017**: *Downstream transitions unaffected.* Given `t-ask` runs with
  `SkipHistory: true`, When the CPN proceeds through `t-clarify` and into
  `t-plan` / `t-execute`, Then the downstream transitions MUST produce
  plans and executions equivalent to the pre-change behaviour (the
  user's answers are merged into the `ColorJSON` token by
  `buildClarifiedToken` and flow to downstream places independently of
  `c.History`).

## 6. Test Automation Strategy

### Test levels & locations

| Level | Backend | Frontend |
|-------|---------|----------|
| Unit | `cpn/hitl_test.go` — assert `c.History` append on success (REQ-001), no append on failure branches (REQ-002 / AC-012), constant usage (REQ-005) | `src/hooks/useChat.test.ts` — cover every row of §4.1 decision table (AC-005/006/007) |
| Unit | `cpn/hitl_test.go` — assert Done-sentinel behaviour unchanged (REQ-004) | `src/features/chat/MessageBubble.test.tsx` — whitespace-tolerant detection (AC-008), non-leading-marker fallback (AC-009), LaTeX rendering regression (AC-010) |
| Integration | `internal/app/session_service_test.go` — drive a HITL session through emission + persist + `GetSession`; assert AC-011 and AC-013 via real Postgres (testcontainer) | `src/features/chat/ChatContainer.test.tsx` or similar — simulate `SESSION_LOADED` with an `$$a2ui:` message; assert A2UI surface renders (AC-001..004) |
| Integration | HTTP handler test: `GET /sessions/{id}` returns `MessageResponse.content` starting with `$$a2ui:` after a HITL flow | Mock `getSession` returning an `$$a2ui:` row; assert the DOM shows interactive surface, not raw text |
| End-to-end (manual or Playwright) | — | Scripted scenarios for AC-001..004 using the real backend and a mocked LLM |

### Frameworks

- **Backend**: Go standard `testing`, table-driven tests where applicable.
  Integration tests MAY use the existing Postgres testcontainer harness.
- **Frontend**: Vitest + React Testing Library. Reducer tested directly;
  component tests drive `MessageBubble` with crafted `content` fixtures;
  container tests dispatch `SESSION_LOADED` actions.

### Test data

- A2UI payload fixtures MUST use the actual `t-clarify` shape
  (`questionnaire` with ≥1 `choice` children) to exercise real
  `JSON.parse` shape and size.
- LaTeX math fixtures (`$$x^2 + y^2 = z^2$$`) MUST be included in the
  regression set to prove AC-010.
- Integration tests MUST exercise a full round-trip through Postgres
  (GUD-005), not a mock, to catch any transport-layer escaping regression.

### CI/CD integration

- All new and updated tests MUST run in the existing `go test ./...` and
  `npm run test` pipelines. No new CI jobs introduced.
- Integration tests that require Postgres MUST reuse the existing
  testcontainer setup. No new infrastructure.

### Coverage expectations

- `cpn/hitl.go` A2UI branch: 100% line + branch across success /
  `payload == nil` / `Marshal` error paths.
- `useChat.ts` reducer: every §4.1 decision-table row has at least one
  dedicated test.
- `MessageBubble.parsePayload`: whitespace-trim, missing-marker, and
  parse-failure branches each tested (AC-008 / AC-009 / AC-012).

### Performance testing

- Not applicable. The fix adds at most one struct append per HITL
  emission and one additional `findIndex` per `STREAM_CHUNK` in the
  frontend reducer. No perf regression is expected. No load testing
  required.

## 7. Rationale & Context

### Why rehydration is the primary lens

Users encounter this bug in everyday use: every chat switch, every
logout/login, every page reload with a pending HITL reproduces it. The
adjacent live-stream chunk-boundary defect (fixed by REQ-008..010) is
real but less user-visible today because `t-clarify` currently emits the
`$$a2ui:` chunk without prior text on the same CPN. Framing the spec
around rehydration ensures the integration tests that matter most
(round-trip through Postgres, AC-001..004) are first-class.

### Why persist via `c.History` instead of a new code path

`session_service.go:293-307` already syncs `c.History` into
`session.Messages` on every run completion, and `persistAfterRun` writes
those messages to the DB. Appending to `c.History` in `fireHITL`
(REQ-001) is the smallest change that reuses this pipeline. A parallel
persistence call from `hitl.go` would violate CON-006 (CPN domain purity)
and create two places that can diverge.

### Why treat `$$a2ui:` as an implicit boundary rather than changing the protocol

The marker is the protocol contract and is already shared between
backend, frontend, tests, and external agents. Changing it would require
coordinated migration across every emission site. Treating the marker as
an implicit message terminator (PAT-001) is additive: it relies on an
invariant the backend already upholds (the marker only appears at the
start of its own dedicated chunk) and requires zero wire-format change.

### Why not SSE replay

`spec-architecture-http-sse-api.md:724` explicitly defers replay to a
future phase; implementing it would require a broker ring buffer, event
sequencing, `Last-Event-ID` handling, and careful ordering semantics
between REST history and SSE stream. None of that is necessary to fix
this bug: persisting the A2UI surface so REST history carries it is a
strictly smaller change and aligns with the existing Phase-1 design.

### Why not buffer split A2UI payloads now

The backend emits the payload atomically today (CON-004). The practical
failure mode is concatenation with prior text, not split reconstruction.
REQ-010 adopts the minimum invariant needed for correctness without
pre-committing to a buffering strategy; split-chunk reconstruction
remains tracked in `spec-architecture-a2a-a2ui-protocol-integration.md`
§12.6.

### Why the KaTeX collision is left untouched

`remark-math` + `rehype-katex` are load-bearing for assistant-authored
math content, which is a product feature independent of A2UI. This fix
restores the routing invariant (A2UI never reaches the markdown pipeline)
rather than removing the math plugins. The two features stay decoupled.

### Why `parsePayload` trims leading whitespace

Rehydration, message splicing across reload, and certain LLM stream
sequences can produce a leading newline before the marker. REQ-011 is
small hardening that removes a class of accidental fallbacks at
essentially zero cost.

### Why `t-ask` must also be flipped to `SkipHistory: true` (REQ-016)

The v1.0 spec assumed that appending `$$a2ui:` to `c.History` in `fireHITL`
(REQ-001) would fully close the bug. Live forensic investigation against the
running Postgres revealed an additional persistence seam: the upstream
`t-ask` LLM transition runs with `RequireJSON: true, StreamOutput: false,
SkipHistory: false` and its raw JSON questionnaire output is appended to
`c.History` at `fire_llm.go:225`. That JSON then flows through
`persistAfterRun` into the `messages` table (observed: a 2542-byte row with
`cpn_id = t-ask` in the affected sessions).

On a live session the frontend hides this ugliness because the subsequent
`$$a2ui:` SSE chunk renders an interactive surface that visually "covers"
the bubble the raw JSON would otherwise produce. On rehydration there is no
SSE — only the DB content — and the raw JSON row renders through the
markdown fallback path, producing the garbled output the user reported
(plus `rehype-katex` collision artifacts because JSON contains `$$`, `{`,
`}`, `_`).

Simply appending `$$a2ui:` to `c.History` (REQ-001) creates the correct
row but leaves the garbage `t-ask` row in place — the user would now see
BOTH: a broken JSON bubble above a working questionnaire. REQ-016 closes
the loop by preventing the `t-ask` row from ever being written, exactly
mirroring the `t-classify` precedent (same `RequireJSON: true,
SkipHistory: true` shape) that has been in production without incident.

Safety of the flip: `t-ask`'s output is consumed by `t-clarify`'s
`A2UIPayloadBuilder` and `buildClarifiedToken` via the CPN token pipeline
(place `p-questions` → HITL → place `p-clarified`), not via `c.History`.
Downstream prompts (`PROMPT_PLAN`, `PROMPT_EXECUTE`) operate on the
`ColorJSON` token built by `buildClarifiedToken`, which already carries the
classifier context plus the user's answers. No transition reads the raw
questionnaire JSON from history. AC-017 codifies this as a testable
invariant.

### Why this spec supersedes the predecessor

The predecessor spec `spec-process-bugfix-a2ui-chunk-boundary-and-persistence.md`
was written the same day (2026-04-13) and covers the same territory but
is framed primarily around the KaTeX rendering regression. The
rehydration scenario (AC-008 in the predecessor) is the user-visible bug
we actually need to close. Promoting it to the primary lens, with
explicit logout/login and chat-switch acceptance criteria, produces a
clearer implementation target. Merging into a single document avoids
drift between two concurrent specs.

## 8. Dependencies & External Integrations

### Internal dependencies

- **INT-001**: `cpn/hitl.go` `fireHITL` — modified per §4.3 (REQ-001..005).
- **INT-002**: `cpn.Message`, `cpn.RoleAssistant`, `cpn.CPN.History`,
  `cpn.CPN.mu` — used unchanged to append the A2UI record.
- **INT-003**: `internal/app/session_service.go` history-sync loop
  (`session_service.go:293-307`) and `persistAfterRun`
  (`session_service.go:327`) — relied upon to promote `c.History` entries
  into `session.Messages` and write to the DB. No modification required.
- **INT-004**: `internal/app/session_service.go` rehydration path added by
  commit `ff419ca` — relied upon to restore sessions. No modification
  required.
- **INT-005**: `persist.MessageToRecord` and `Sessions.AppendMessage` —
  relied upon to persist. No modification required.
- **INT-006**: `internal/driving/httpapi/handler_session.go`
  `HandleGetSession` — relied upon to return
  `MessageResponse.content` verbatim. Verified; no modification required.
- **INT-007**: `internal/driving/httpapi/handler_sse.go` — unchanged. No
  replay added.
- **INT-008**: `front/react-assistant/src/hooks/useChat.ts` — reducer
  modified per §4.1 (REQ-008..010, REQ-013..015).
- **INT-009**: `front/react-assistant/src/features/chat/MessageBubble.tsx`
  — `parsePayload` hardened per §4.2 (REQ-011..012).
- **INT-010**: `front/react-assistant/src/types/api.ts` —
  `MessageResponse` unchanged (CON-005).

### External systems

- **EXT-001**: Postgres `messages` table — relied upon to store
  `content TEXT NOT NULL` verbatim. No schema migration.

### Third-party services

- **SVC-001**: None. The fix is contained to the BRAE backend and React
  frontend.

### Infrastructure dependencies

- **INF-001**: None. No new processes, queues, or storage.

### Data dependencies

- **DAT-001**: None. The fix reuses the existing `messages` row shape.

### Technology platform dependencies

- **PLT-001**: React 19 + `remark-math` + `rehype-katex` (existing) — the
  fix is designed to coexist with these unchanged.
- **PLT-002**: Go 1.22+ (existing) — no version bump.
- **PLT-003**: Postgres (existing) — `content TEXT` column, no change.

### Compliance dependencies

- **COM-001**: None. No PII, auth, or audit-log semantics change. A2UI
  surfaces are already persisted for non-HITL paths (per predecessor
  spec AC-010 covering the legacy review-card); extending the same
  persistence to HITL surfaces introduces no new compliance surface.

## 9. Examples & Edge Cases

### 9.1 Chat switch with pending HITL (AC-001)

```
State before switch:
  Chat A open, user is typing.
  Chat B has a t-clarify HITL pending; the live SSE for B is not connected
    from this browser tab.

User action: click chat B in the sidebar.

Frontend sequence:
  1. Abort SSE subscription to A (if any).
  2. Clear reducer state.
  3. GET /api/v1/sessions/{B}
  4. Response contains messages[…], one of which has
     content = "$$a2ui:{\"components\":[{\"type\":\"questionnaire\",...}]}"
  5. SESSION_LOADED dispatched.
  6. MessageBubble.parsePayload detects marker → A2UIRenderer → interactive
     questionnaire visible.
  7. Subscribe to SSE for B (new events only).

User submits an answer in the questionnaire.
  → POST /sessions/{B}/hitl/t-clarify {action:"submit", content:{...}}
  → backend unblocks the HITL channel
  → CPN proceeds; subsequent events arrive via SSE.
```

### 9.2 Logout / login (AC-002)

```
1. User is in chat B with live HITL questionnaire rendered.
2. User clicks "Cerrar sesión".
3. Frontend clears auth + reducer state; navigates to login.
4. User logs back in, opens chat B.
5. Same sequence as 9.1 from step 3 onward.
Expected: questionnaire renders from persisted history.
```

### 9.3 Page reload (AC-003)

```
1. User is in chat B with live HITL questionnaire rendered.
2. User presses F5.
3. Browser reloads; app bootstraps; auth session restores.
4. App opens last-viewed chat B; same sequence as 9.1 from step 3.
Expected: questionnaire renders with the same componentId (HITL transition
id) so that POST /sessions/{B}/hitl/t-clarify still targets the same blocked
HITL.
```

### 9.4 Backend restart (AC-004)

```
1. HITL emitted and persisted to Postgres before restart.
2. Backend restarts.
3. Frontend's open SSE drops; reconnects.
4. User's current chat triggers GET /api/v1/sessions/{B}.
5. Row with $$a2ui: content is returned.
6. Questionnaire renders.
Note: whether the HITL transition is still armed after restart depends on
commit ff419ca's rehydration semantics; that is out of scope for this spec.
```

### 9.5 A2UI after streaming LLM text (AC-006)

```
SSE stream:
  stream_chunk  Content='Here is my take:', Done=false, CPNID=c1
  stream_chunk  Content=' a summary...',    Done=false, CPNID=c1
  stream_chunk  Content='$$a2ui:{...}',     Done=false, CPNID=c1
  stream_chunk  Content='',                 Done=true,  CPNID=c1

Reducer outcome after third chunk:
  messages = [
    { role:'assistant', content:'Here is my take: a summary...', isStreaming:false, cpnId:c1 },
    { role:'assistant', content:'$$a2ui:{...}',                  isStreaming:true,  cpnId:c1 },
  ]
```

### 9.6 Literal `$$a2ui:` in LLM prose (AC-009)

```
SSE stream:
  stream_chunk  Content='As an example, the marker `$$a2ui:` is used for...',
                Done=false, CPNID=c1

Reducer outcome: single markdown bubble — marker is not at offset 0 after
trim.
```

### 9.7 Marshal failure (AC-012)

```
cfg.A2UIPayloadBuilder returns (someStruct, nil) but json.Marshal fails
(e.g. unsupported channel type).

fireHITL MUST NOT emit the EventStreamChunk, MUST NOT append to c.History,
and MUST proceed to emit EventHITLRequested with the plain-string prompt
(existing backward-compatible path). No row is written to messages.
```

### 9.8 Split-chunk A2UI payload (out of scope — REQ-010)

```
Hypothetical future SSE stream:
  stream_chunk  Content='$$a2ui:{"components":[{"type":"question',   Done=false, CPNID=c1
  stream_chunk  Content='naire",...}]}',                             Done=false, CPNID=c1

Reducer behaviour per REQ-010:
  - Chunk 1 → new A2UI bubble with partial JSON; parsePayload fails; falls
    back to markdown.
  - Chunk 2 → NOT concatenated; creates yet another new bubble.
  - End result: both chunks render as degraded markdown.

Tolerated here because CON-004 holds today. Buffering tracked in
spec-architecture-a2a-a2ui-protocol-integration.md §12.6.
```

### 9.9 Duplicate-persistence regression guard (AC-011)

```
After a HITL flow, assert:
  SELECT COUNT(*) FROM messages
    WHERE session_id = $1
      AND role = 'assistant'
      AND content LIKE '$$a2ui:%'
      AND cpn_id = $2
  == 1
```

## 10. Validation Criteria

- **V-001**: `go test ./cpn/...` passes, including new assertions that
  `fireHITL` appends exactly one `$$a2ui:` entry to `c.History` on success
  and zero on every failure branch enumerated in AC-012.
- **V-002**: `go test ./internal/app/...` passes, including a test that
  drives a session through a HITL A2UI flow and asserts the persisted row
  survives `GetSession` and round-trips byte-identically (AC-013).
- **V-003**: `npm run test` in `front/react-assistant/` passes, including
  new reducer tests for every row of the §4.1 decision table and hardened
  `parsePayload` tests covering REQ-011 / REQ-012.
- **V-004**: Manual smoke — run the happy-path `t-clarify` flow end to end
  with a real LLM; render the questionnaire live; then execute each
  rehydration scenario (chat switch / logout-login / page reload /
  backend restart) and verify the questionnaire renders interactively in
  every case.
- **V-005**: No new deprecation warnings, no new ESLint / golangci-lint
  findings, no diff in `rehype-katex` / `remark-math` configuration, no
  change to `package.json` or `go.mod`.
- **V-006**: Grep audit — `$$a2ui:` literal occurrences are reduced or
  held constant (via the new constants in GUD-001 / GUD-003); no new
  ad-hoc sites are introduced.
- **V-007**: DB inspection (manual or scripted) after a HITL flow — exactly
  one row in `messages` with `role = 'assistant'` and `content LIKE
  '$$a2ui:%'` per HITL emission, per AC-011.

## 11. Related Specifications / Further Reading

- [spec-process-bugfix-a2ui-chunk-boundary-and-persistence.md](spec-process-bugfix-a2ui-chunk-boundary-and-persistence.md)
  — **Superseded** by this document. Kept for history.
- [spec-architecture-a2a-a2ui-protocol-integration.md](spec-architecture-a2a-a2ui-protocol-integration.md)
  — Parent protocol spec. §4.6 defines `A2UI_MARKER`; §9 defines
  partial-payload fallback; §12 defines the `questionnaire` / `choice`
  components and the `t-clarify` flow; §12.6 tracks the deferred
  split-chunk buffering item.
- [spec-architecture-http-sse-api.md](../back/go-assistant/spec/spec-architecture-http-sse-api.md)
  — HTTP + SSE contract. §4 "Message History" SHOULD be updated in a
  follow-up doc PR to document that `MessageResponse.content` MAY begin
  with `$$a2ui:` and that the prefix MUST be preserved. §9.5 documents
  the Phase-1 "no SSE replay" decision this spec relies on (CON-008).
- [spec-architecture-block20-llm-streaming.md](spec-architecture-block20-llm-streaming.md)
  — `StreamChunk` semantics and Done-sentinel conventions referenced by
  REQ-004.
- [spec-process-bugfix-ghost-session-rehydration.md](spec-process-bugfix-ghost-session-rehydration.md)
  — Sibling rehydration bugfix (commit `ff419ca`) that this document
  builds on.
- [spec-design-sse-streaming-bugfixes.md](spec-design-sse-streaming-bugfixes.md)
  — Prior SSE broker correctness work; this spec does not modify the
  broker.
