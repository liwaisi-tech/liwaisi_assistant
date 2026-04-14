---
title: "Bug Fix — Complete A2UI HITL Rehydration: Close the `t-ask` History Leak, Sanitize Sidebar Preview, Harden Chat-Switch Race"
version: 1.1
date_created: 2026-04-14
last_updated: 2026-04-14
owner: liwaisi-tech
tags: [process, bugfix, a2ui, hitl, rehydration, persistence, llm-config, sidebar, preview, chat-switch, sse, cpn, frontend, backend]
supersedes: spec-process-bugfix-a2ui-hitl-rehydration.md
---

## Changelog

- **1.1 (2026-04-14)**: REQ-401's `'generating'` reducer state collapsed onto
  the existing `'running'` state during implementation. The visible behavior
  is identical because `MessageList.showThinking` already renders the
  "thinking…" indicator whenever `sessionState === 'running'` and no
  streaming bubble exists yet — adding a separate value forced exhaustive
  `Record<SessionState, ...>` updates in `ChatHeader.tsx` / `StatusBar.tsx`,
  files owned by a parallel `feat(ui): consolidate configuration into
  avatar-anchored UserMenu` change. Keeping the reducer surface narrow
  decoupled the two work streams. AC-401/404 are restated against
  `'running'`; AC-403 still requires HITL to map to `'idle'` so the
  indicator is suppressed and the A2UI surface IS the affordance.

- **1.0 (2026-04-14)**: Initial spec, superseding
  `spec-process-bugfix-a2ui-hitl-rehydration.md` v1.2. Scope focused on
  closing the three observable residual defects in that spec's deferred
  work: (a) REQ-018 unimplemented (raw `t-ask` JSON still in `messages`),
  (b) sidebar preview leaks raw `$$a2ui:` marker, (c) chat-switch /
  reload race that re-surfaces aborted streaming bubbles. SSE replay and
  mid-stream resume remain deferred to a dedicated architecture spec
  (CON-COM-001).

# Introduction

`spec-process-bugfix-a2ui-hitl-rehydration.md` v1.2 landed two correct
fixes (commits `2c8a989` and `baa59f5`) but explicitly deferred REQ-018,
whose absence leaves the user-visible bug partially open. Live forensic
investigation on 2026-04-14 against the running Postgres confirmed the
residual defect chain and surfaced two adjacent defects the predecessor
spec did not address:

1. **Residual (v1.2 REQ-018 gap).** `t-ask` still runs with
   `LLMConfig.SkipHistory = false` at
   `back/go-assistant/cmd/server/topologies.go:512` (working-copy revert
   of commit `2c8a989`'s original flip). The raw JSON questionnaire
   produced by `t-ask` is therefore appended to `c.History` at
   `cpn/fire_llm.go:225-232` and promoted into the `messages` table. On
   rehydration that row renders as a plain markdown bubble immediately
   above the `$$a2ui:` interactive surface, producing the artefact
   captured in the user's Image 1 / Image 2: `{"restated_goal": "…",
   "assumptions": […], "questions": […]}` leaking into the chat as
   text. Database evidence:
   ```
   session_id: 0cad37318e8a5cdc28ef730f3319b82a
   assistant rows (ordered):
     (1) {"restated_goal":"Necesitas ayuda para preparar la muestra…", …}
     (2) $$a2ui:{"components":[{"children":[…questionnaire…]}]}
   ```
   Row (1) is the bug; row (2) is the intended surface.

2. **New defect (sidebar preview).** `ChatSidebar` prints
   `session.last_message_preview` verbatim
   (`front/react-assistant/src/features/chat/ChatSidebar.tsx:187`). The
   backend `handler_session.go:347-349` forwards
   `item.LastMessagePreview` unchanged from the persistence layer,
   which computes the preview from the last message's raw `content`. A
   HITL-pending session whose last row is `$$a2ui:…` therefore shows the
   literal marker string in the chat list, producing the artefact in
   Image 3 (`$$a2ui:{"components":["children":[…`).

3. **New defect (chat-switch / reload race).** When a user switches
   chats (or reloads) during an in-flight `t-ask` or `t-plan` LLM
   stream, the outbound SSE is dropped. Mid-stream text has not yet
   reached `persistAfterRun` because `fire_llm.go:225` appends to
   `c.History` only after the stream completes. On re-subscribe the SSE
   broker replays nothing (CON-008 in the predecessor spec). The user
   sees an empty or partial assistant bubble where a plan was
   visibly being built seconds earlier. Full architectural resume is
   out of scope here, but the **frontend** can and MUST avoid rendering
   stale streaming bubbles from the prior chat, avoid racing
   `SESSION_LOADED` against a late SSE chunk of the prior chat, and
   surface a clear "run in progress" indicator instead of ghost text
   so the user does not perceive lost work.

This specification replaces the predecessor's open items with testable,
closable requirements. The predecessor is left in place as history; the
`supersedes:` frontmatter key directs readers here.

## 1. Purpose & Scope

**Purpose.** Produce the minimum coherent change set that closes the
three defects listed in the Introduction so that, after this PR:

1. No `t-ask` raw questionnaire JSON row exists in the `messages` table
   for any new clarify cycle, AND the `t-ask` LLM still correctly reads
   the user message from `c.History` (i.e., the v1.1 input-side
   regression does not return).
2. The chat sidebar never renders a raw `$$a2ui:` marker, a raw JSON
   envelope, or any protocol artefact. It shows a human-readable
   preview for A2UI-terminated sessions.
3. Chat switch and page reload during an in-flight LLM stream produce
   a deterministic UX: either the persisted state or an explicit
   "generating…" indicator. No ghost text, no leakage of the prior
   chat's streaming bubble, no mixed state.

**In scope.**

- Backend (`back/go-assistant`):
  - Introduce `LLMConfig.SkipOutputHistory bool` in `cpn/fire_llm.go`
    as a separate flag from `SkipHistory`, per REQ-018 of the
    predecessor.
  - Flip `t-ask` in `cmd/server/topologies.go` to
    `SkipHistory: false, SkipOutputHistory: true`.
  - Sanitize `last_message_preview` computation in the persistence
    layer so the marker never appears in list payloads.
- Frontend (`front/react-assistant`):
  - Harden `useChat.ts` against chat-switch races and stale
    streaming bubbles.
  - Ensure `ChatSidebar` never renders raw markers even if a
    backward-compatible preview leaks through the API (defence in
    depth).
  - Surface a dedicated `sessionState='generating'` indicator when
    the rehydrated session has an active CPN run.
- Regression tests covering every new requirement, with Postgres
  round-trip for backend invariants per GUD-005 of the predecessor.

**Out of scope.**

- SSE replay, `Last-Event-ID` resume, or broker ring buffer. Tracked
  separately (see §11).
- Changing the A2UI wire protocol or the `$$a2ui:` marker string
  (CON-001 of predecessor remains in force).
- DB schema migration (`messages.content TEXT`, `sessions.*` unchanged).
- Changes to the A2A driving adapter (`internal/driving/a2a/`).
- Changes to `t-classify`, `t-clarify`, `t-plan`, `t-execute` prompt
  content or flow control. Only `t-ask`'s `LLMConfig` changes.
- Changes to `remark-math` / `rehype-katex` configuration
  (CON-003 of predecessor).

**Audience.** Backend Go engineers modifying `cpn/fire_llm.go`,
`cpn/transition.go` (or wherever `LLMConfig` is declared),
`cmd/server/topologies.go`, `internal/app/session_service.go`,
`store/postgres/session.go`, `cpn/persist/memory.go`, and associated
tests; frontend React engineers modifying `src/hooks/useChat.ts`,
`src/features/chat/ChatSidebar.tsx`, and associated tests.

**Assumptions.**

- Commits `2c8a989` (backend A2UI persistence) and `baa59f5` (frontend
  reducer boundary) are merged and correct. Their behaviour is relied
  upon; this spec extends, it does not revisit them.
- `LastMessagePreview` is derived server-side at session-list query time
  from the single most recent `messages` row for that session. The
  frontend does not recompute it.
- `persistAfterRun` flushes `c.History` deltas to Postgres at each CPN
  run boundary (run completion or HITL pause), as described in the
  predecessor's §4.5.
- The frontend already dispatches `RESET` on chat switch
  (`useChat.ts` per commit `baa59f5`); this spec hardens the race
  window, not the reset semantics themselves.

## 2. Definitions

| Term | Definition |
|------|-----------|
| **A2UI** | Agent-to-UI protocol. See `spec-architecture-a2a-a2ui-protocol-integration.md`. |
| **A2UI marker** | The literal string `$$a2ui:`. Defined once per language: Go `cpn.A2UIMarker` (`cpn/hitl.go:19`), TS `A2UI_MARKER` (`src/features/chat/a2ui/constants.ts:10`). |
| **Clarify cycle** | The CPN subgraph `t-classify → t-ask → t-clarify (HITL)` that produces a questionnaire and blocks on human input. |
| **`SkipHistory` (existing)** | `LLMConfig` flag with dual semantics: (a) input-side — when `true`, `BuildContext` receives `ctxWindowSize=0` and the LLM sees no history (`fire_llm.go:56-60`); (b) output-side — when `true`, the LLM's output is not appended to `c.History` (`fire_llm.go:213-216`). The conflation is the root cause of the v1.1 regression. |
| **`SkipOutputHistory` (new, this spec)** | New `LLMConfig` flag, strictly controls the output-side append in `fire_llm.go:213-216`. Independent of `SkipHistory`. |
| **Preview** | The server-computed one-line summary of a session, returned as `SessionListItem.last_message_preview` by `GET /api/v1/sessions`. Shown in `ChatSidebar`. |
| **Ghost bubble** | A message rendered in the UI whose content is the tail of an aborted streaming response from a different session, surviving into the current session's DOM due to a reducer race. |
| **Generating state** | A new, distinct frontend `sessionState` value used when the rehydrated session's backend CPN run is active but no new SSE chunks have arrived yet. |
| **Rehydration** | Reconstruction of client-side chat state from backend persistence after the live SSE connection was not established, was dropped, or belongs to a different session than the currently-viewed one. Same definition as predecessor spec. |

## 3. Requirements, Constraints & Guidelines

### 3.1 Backend — `SkipOutputHistory` dual-flag (closes predecessor REQ-018)

- **REQ-101**: `LLMConfig` in the CPN package MUST gain a new boolean
  field `SkipOutputHistory`. The field MUST default to `false` so
  existing call sites remain behaviourally unchanged.

- **REQ-102**: In `cpn/fire_llm.go` the output-append guard at
  `:216` MUST be widened to:
  `if !t.LLMConfig.SkipHistory && !t.LLMConfig.SkipOutputHistory { … }`.
  The input-side branch at `:56-60` MUST remain gated on
  `SkipHistory` ONLY. This is the core separation-of-concerns change.

- **REQ-103**: The `user` message append at
  `cpn/fire_llm.go:218-223` (the `RoleUser` entry that mirrors
  `userTokens`) MUST ALSO be suppressed when
  `SkipOutputHistory = true`. Rationale: for routing-only transitions
  whose "user turn" is an internal token envelope rather than the
  human's words, persisting it pollutes history identically to
  persisting the assistant output.

- **REQ-104**: `t-ask` in `cmd/server/topologies.go` MUST be configured
  with **both** of the following simultaneously:
  - `SkipHistory: false` — so the LLM reads the user's message from
    history (the v1.2 correctness requirement).
  - `SkipOutputHistory: true` — so the LLM's raw JSON output is NOT
    appended to `c.History`.
  The inline comment at the surrounding lines MUST be rewritten to
  explain the dual-flag rationale (supersedes the current
  working-copy comment).

- **REQ-105**: No other LLM transition configuration MUST be changed in
  this spec. `t-classify` retains `SkipHistory: true` (which implies
  output suppression by REQ-102's OR-logic — unchanged observable
  behaviour). `t-plan`, `t-execute`, and any other LLM transitions
  retain their current flags.

- **REQ-106**: The `ColorJSON` token deposited at
  `cpn/fire_llm.go:239-248` MUST continue to carry `t-ask`'s raw JSON
  output as the token payload. `SkipOutputHistory` MUST NOT interfere
  with token deposition; it controls only `c.History` writes.
  Downstream `t-clarify` still consumes this token via
  `A2UIPayloadBuilder` and `buildClarifiedToken` exactly as today.

- **REQ-107**: Existing `SkipHistory: true` call sites (`t-classify`)
  MUST retain both input suppression and output suppression (i.e., the
  output guard remains `!SkipHistory && !SkipOutputHistory`, so
  `SkipHistory=true` alone continues to suppress output). Backwards
  compatibility is preserved by construction of the guard, not by a
  separate flag flip.

- **INV-101**: For a single `t-classify → t-ask → t-clarify` cycle, the
  `messages` table MUST contain exactly ONE assistant row, and that
  row's `content` MUST start with `$$a2ui:`. Zero rows with
  `cpn_id = 't-ask'` OR with raw JSON shape (top-level keys
  `restated_goal` AND `questions` / `sections`) MUST exist.

- **INV-102**: No regression of the input-read semantics. When `t-ask`
  fires, the LLM request's `messages` array MUST contain the user's
  most recent non-empty text message (the "Quiero que me ayudes…"
  turn) in the appropriate position of the context window. Asserted
  by a backend unit test that captures the `LLMRequest`.

### 3.2 Backend — Preview sanitization

- **REQ-201**: The `last_message_preview` string returned by
  `GET /api/v1/sessions` (list endpoint) MUST NOT contain the literal
  substring `$$a2ui:`, AND MUST NOT contain any character sequence
  that starts with an opening `{` followed by the substrings
  `"restated_goal"` OR `"components"`.

- **REQ-202**: Preview sanitization MUST be implemented once, at the
  single site where the preview is computed, for both persistence
  backends currently in use:
  - `back/go-assistant/store/postgres/session.go` (Postgres).
  - `back/go-assistant/cpn/persist/memory.go` (in-memory).

  The computation MUST walk the session's assistant messages from
  newest to oldest and select the first message whose `content` is a
  "renderable user-facing text" per the algorithm in §4.2. If no
  such message exists, the preview MUST be the empty string (the
  frontend already renders the i18n fallback
  `chat:sidebar.noMessagesYet`).

- **REQ-203**: A2UI-terminated sessions MUST produce a preview equal
  to the **human-readable `title` extracted from the A2UI payload**,
  when available. Specifically: if the selected message begins with
  the A2UI marker and parses as valid JSON containing a top-level
  `components[0].props.title` string (or, failing that,
  `components[0].children[0].props.label` for `questionnaire` with
  `choice` children), the preview MUST be that string truncated to
  120 characters. If parsing fails, REQ-202's fallback to the next
  renderable message applies.

- **REQ-204**: The raw-JSON `t-ask`-style row case is eliminated by
  REQ-104 and SHOULD NOT appear in the preview source set after this
  PR merges. Nevertheless, the sanitizer MUST treat any assistant
  message whose content parses as JSON with top-level `restated_goal`
  or `questions` keys as non-renderable and skip it. This is defence
  in depth against pre-existing rows and any future routing-metadata
  persistence gap.

### 3.3 Frontend — Chat-switch race hardening

- **REQ-301**: On chat switch, `useChat.ts` MUST abort the outbound
  SSE subscription **before** dispatching `RESET`. The abort MUST be
  synchronous relative to the reducer dispatch (same microtask) so no
  in-flight `STREAM_CHUNK` action can be applied to the new session's
  state. If the SSE library returns a Promise-based abort, the
  subsequent `SESSION_LOADED` dispatch MUST be scheduled in a
  continuation that runs after the abort Promise settles.

- **REQ-302**: `useChat.ts` MUST tag every `STREAM_CHUNK` action with
  the `sessionId` it was received for. The reducer MUST ignore any
  `STREAM_CHUNK` whose tagged `sessionId` does not match the current
  `state.sessionId`. This is belt-and-braces against REQ-301
  timing edge cases.

- **REQ-303**: On any dispatch of `SESSION_LOADED`, the reducer MUST
  unconditionally set `isStreaming: false` on every message it emits
  into `state.messages`. This closes the class of bugs where a
  rehydrated message carries over a stale streaming flag.

- **REQ-304**: On logout, `useChat.ts` MUST reset not only its own
  reducer state (already implemented by commit `baa59f5`) but also
  MUST call the outbound SSE abort path and clear any module-level
  buffers or `useRef`-held partial chunks. Test coverage MUST
  include a logout → login → open-same-chat scenario.

### 3.4 Frontend — Explicit "generating" state and no ghost text

- **REQ-401**: `useChat.ts` MUST introduce a new `sessionState` value
  `'generating'`. The state machine transitions are:

  | From → To | Trigger |
  |-----------|---------|
  | `idle` → `generating` | `SESSION_LOADED` with `session.state === 'running'` or equivalent backend signal |
  | `generating` → `running` | First `STREAM_CHUNK` for the session after rehydration |
  | `running` → `idle` | Done sentinel (existing) |
  | any → `idle` | `RESET` (existing) |

- **REQ-402**: When `sessionState === 'generating'` AND
  `messages.at(-1)?.role === 'user'`, `MessageList` MUST render a
  placeholder "generating…" indicator (reusing the existing
  `Esperando respuesta…` affordance) without materialising an empty
  assistant bubble.

- **REQ-403**: When `sessionState === 'generating'` AND the last
  persisted message is an assistant `$$a2ui:` row (HITL pending), the
  UI MUST render the A2UI surface as-is (existing behaviour)
  AND MUST NOT show the "generating…" indicator. A pending HITL is
  not "generating"; it is "waiting for input".

- **REQ-404**: Backend MUST expose the session's run state on the
  session-detail response used for rehydration. Concretely:
  `GET /api/v1/sessions/{id}` MUST include a `state` field whose value
  is one of `idle | running | hitl_pending | terminal`. This is
  already available server-side via `session.State()` in
  `internal/app/session_service.go` and its inclusion in the DTO is
  non-breaking (additive field).

- **REQ-405**: Frontend `ChatSidebar` MUST fall back to a defensive
  local sanitizer when rendering `last_message_preview`: if the
  string starts with `$$a2ui:` OR with `{"` (first two chars), it
  MUST display the i18n key `chat:sidebar.pendingForm` ("Formulario
  pendiente" / "Pending form") instead of the raw string. This is a
  belt-and-braces complement to REQ-201/203.

### 3.5 Invariants across the full fix

- **INV-301**: After merge, for every session that went through a
  clarify cycle, the following SQL MUST return exactly 1:
  ```sql
  SELECT COUNT(*) FROM messages
  WHERE session_id = $1
    AND role = 'assistant'
    AND content LIKE '$$a2ui:%';
  ```
  AND the following MUST return exactly 0:
  ```sql
  SELECT COUNT(*) FROM messages
  WHERE session_id = $1
    AND role = 'assistant'
    AND content LIKE '{"restated_goal"%';
  ```

- **INV-302**: No DOM rendered by any React component in
  `ChatSidebar` MUST ever contain the literal substring `$$a2ui:`.
  Asserted by a DOM-level test that renders the sidebar with a
  pathological `last_message_preview`.

- **INV-303**: On chat switch, during the window between `RESET` and
  `SESSION_LOADED`, `state.messages` MUST be `[]` and `sessionState`
  MUST be `'idle'`. No assistant bubble from chat A can appear in
  chat B's DOM.

### 3.6 Constraints

- **CON-101**: The `$$a2ui:` marker string MUST NOT change (inherited
  from predecessor CON-001).
- **CON-102**: The HITL resolve wire shape MUST NOT change (inherited
  from predecessor CON-002).
- **CON-103**: No new HTTP endpoints. One additive field (`state`) on
  `SessionDetailResponse` per REQ-404 is permitted; no rename.
- **CON-104**: No DB schema migration. All changes are derived at
  query time.
- **CON-105**: The CPN domain (`cpn/`) MUST NOT import anything from
  `internal/app/` or `internal/driving/` (inherited from predecessor
  CON-006).
- **CON-106**: No SSE replay, no `Last-Event-ID`, no broker ring
  buffer. Deferred to `spec-architecture-sse-replay-and-resume.md`
  (to be authored as a sibling follow-up if/when prioritised).
- **CON-107**: This spec MUST NOT break any acceptance criterion
  enumerated in the predecessor spec (AC-001 through AC-017). Those
  ACs are carried forward verbatim and remain testable.

### 3.7 Guidelines

- **GUD-101**: Prefer a single top-level helper (e.g.
  `persist.RenderablePreview(msgs []Message) string`) over inlining
  sanitization at both persistence sites (REQ-202). Both Postgres
  and in-memory backends call it.
- **GUD-102**: On the frontend, factor the "preview display value"
  computation (REQ-405) into a pure function imported from the same
  module that owns `A2UI_MARKER`. One place, one truth.
- **GUD-103**: Keep the reducer change for REQ-302 surgical —
  attach `sessionId` to the action payload at the dispatch site
  (`useSSE` consumer or equivalent), not inside the reducer.
- **GUD-104**: The `SkipOutputHistory` flag naming MUST be literal
  and unambiguous. No abbreviations (`SkipOutHist`, `NoAppend`,
  `Quiet`). Dual-flag clarity outweighs brevity.
- **GUD-105**: When writing integration tests for INV-101, use the
  existing Postgres testcontainer harness per predecessor GUD-005.
  Mocks proved insufficient previously.

### 3.8 Patterns

- **PAT-101**: **Dual-flag separation of concerns.** A single flag
  with dual semantics (`SkipHistory`) is a design smell that caused
  the v1.1 regression. The pattern adopted here is one flag per
  concern (input read vs output append), composed by OR at the
  call site. Future `LLMConfig` fields SHOULD follow the same
  one-concern-one-flag rule.
- **PAT-102**: **Sanitize at the boundary.** Raw protocol artefacts
  (`$$a2ui:`, JSON envelopes) MUST be transformed to human-readable
  strings at the serialisation boundary — either the DB-to-DTO
  boundary (REQ-201) or the DTO-to-DOM boundary (REQ-405). Never
  render the raw marker outside the message-bubble A2UI pipeline.
- **PAT-103**: **Session-scoped actions.** Frontend reducer actions
  whose effect depends on the current session MUST carry the session
  id on their payload so the reducer can defensively drop stale
  actions (REQ-302). Rely on time-based guarantees only as secondary.

## 4. Interfaces & Data Contracts

### 4.1 `LLMConfig` struct (Go)

Current declaration (abbreviated, in `cpn/`):

```go
type LLMConfig struct {
    Model                string
    MaxTokens            int
    Temperature          float64
    Budget               float64
    RequireJSON          bool
    StreamOutput         bool
    SkipHistory          bool
    SkipRegionalPreamble bool
    // ...
}
```

After this spec:

```go
type LLMConfig struct {
    Model                string
    MaxTokens            int
    Temperature          float64
    Budget               float64
    RequireJSON          bool
    StreamOutput         bool
    SkipHistory          bool // input-side: zeroes ctx window; output-side: also suppresses append (for backwards compat)
    SkipOutputHistory    bool // output-side only: suppresses append to c.History without affecting input read
    SkipRegionalPreamble bool
    // ...
}
```

### 4.2 Preview algorithm (pseudocode)

```go
// RenderablePreview walks messages newest-to-oldest and returns the first
// human-readable summary, or "" if none.
func RenderablePreview(msgs []persist.MessageRecord) string {
    const maxLen = 120
    for i := len(msgs) - 1; i >= 0; i-- {
        m := msgs[i]
        s := strings.TrimSpace(m.Content)

        // A2UI surface: extract a friendly title from the payload.
        if strings.HasPrefix(s, A2UIMarker) {
            if title := extractA2UITitle(s[len(A2UIMarker):]); title != "" {
                return truncate(title, maxLen)
            }
            // fall through: try next message
            continue
        }

        // Routing-metadata JSON (defence in depth; should not exist after REQ-104).
        if looksLikeRoutingJSON(s) {
            continue
        }

        // Any other non-empty text is renderable.
        if s != "" {
            return truncate(s, maxLen)
        }
    }
    return ""
}

// extractA2UITitle parses the A2UI JSON and returns the first user-facing label.
// Returns "" if JSON parse fails or no title is present.
func extractA2UITitle(jsonStr string) string {
    var env struct {
        Components []struct {
            Props    map[string]any `json:"props"`
            Children []struct {
                Props map[string]any `json:"props"`
            } `json:"children"`
        } `json:"components"`
    }
    if json.Unmarshal([]byte(jsonStr), &env) != nil { return "" }
    if len(env.Components) == 0 { return "" }
    if t, ok := env.Components[0].Props["title"].(string); ok && t != "" {
        return t
    }
    if len(env.Components[0].Children) > 0 {
        if l, ok := env.Components[0].Children[0].Props["label"].(string); ok {
            return l
        }
    }
    return ""
}

// looksLikeRoutingJSON is intentionally narrow: only matches shapes we know to be routing metadata.
func looksLikeRoutingJSON(s string) bool {
    if !strings.HasPrefix(s, "{") { return false }
    return strings.Contains(s, `"restated_goal"`) || strings.Contains(s, `"classification"`)
}
```

### 4.3 `SessionDetailResponse` additive field

```json
{
  "id": "…",
  "title": "…",
  "messages": [ … ],
  "state": "idle | running | hitl_pending | terminal",   // NEW (REQ-404)
  "created_at": "…",
  "updated_at": "…"
}
```

Field is additive; existing consumers that ignore unknown fields are
unaffected.

### 4.4 Reducer state machine (TypeScript pseudocode)

```ts
type SessionState = 'idle' | 'generating' | 'running';

type Action =
  | { type: 'RESET' }
  | { type: 'SESSION_LOADED'; sessionId: string; messages: ChatMessage[]; state: BackendState }
  | { type: 'STREAM_CHUNK'; sessionId: string; data: StreamChunk }  // sessionId tagged per REQ-302
  | /* ... */;

function reducer(state: State, action: Action): State {
  switch (action.type) {
    case 'SESSION_LOADED':
      return {
        ...state,
        sessionId: action.sessionId,
        messages: action.messages.map(m => ({ ...m, isStreaming: false })), // REQ-303
        sessionState: action.state === 'running' ? 'generating' : 'idle',   // REQ-401
      };

    case 'STREAM_CHUNK':
      if (action.sessionId !== state.sessionId) return state;               // REQ-302
      // existing logic, plus: on first chunk, transition 'generating' → 'running'
      return applyChunk(state, action.data);

    case 'RESET':
      return initialState;
  }
}
```

### 4.5 Sidebar defensive display

```ts
// src/features/chat/a2ui/previewDisplay.ts (new file, per GUD-102)
import { A2UI_MARKER } from './constants';

export function displayPreview(raw: string, t: TFunction): string {
  const trimmed = raw.trimStart();
  if (trimmed.startsWith(A2UI_MARKER)) return t('chat:sidebar.pendingForm');
  if (trimmed.startsWith('{"'))        return t('chat:sidebar.pendingForm');
  return raw;
}
```

`ChatSidebar.tsx:187` changes from `{chat.last_message_preview || fallback}` to
`{chat.last_message_preview ? displayPreview(chat.last_message_preview, t) : fallback}`.

## 5. Acceptance Criteria

### Backend — `SkipOutputHistory`

- **AC-101**: *No raw `t-ask` JSON persisted.* Given a fresh session
  that completes the `t-classify → t-ask → t-clarify` path, When the
  `messages` table is queried, Then INV-101 MUST hold (exactly one
  `$$a2ui:%` assistant row; zero raw-JSON assistant rows).
- **AC-102**: *`t-ask` still reads user input.* Given a session where
  the user sent "Quiero que me ayudes mañana tengo una feria…", When
  `t-ask` fires, Then the captured `LLMRequest.Messages` slice MUST
  contain a user-role message whose content includes the substring
  "Quiero que me ayudes". Asserted in a unit test with a mock LLM
  client that records the request.
- **AC-103**: *Backward compatibility of `SkipHistory=true`.* Given
  any existing transition with `SkipHistory: true` (e.g.
  `t-classify`), When it fires, Then neither the user-side input
  replay nor the assistant-side history append MUST occur. Behaviour
  identical to pre-change.
- **AC-104**: *Token deposition unaffected.* Given `t-ask` with
  `SkipOutputHistory: true`, When it fires, Then the output token
  deposited to its output places MUST have `Payload = content` and
  `Color = ColorJSON`, byte-identical to pre-change. Downstream
  `t-clarify` MUST consume this token normally.

### Backend — Preview sanitization

- **AC-201**: *Preview never contains marker.* Given a session whose
  most recent assistant message is `$$a2ui:{"components":[…]}`, When
  `GET /api/v1/sessions` is called, Then the returned
  `last_message_preview` MUST NOT contain the substring `$$a2ui:`.
- **AC-202**: *Preview falls back gracefully.* Given a session whose
  most recent assistant message is `$$a2ui:<invalid JSON>`, When the
  list endpoint is called, Then the preview MUST be derived from the
  next-older renderable message, OR the empty string if none exists.
- **AC-203**: *Preview surfaces A2UI title.* Given a session whose
  most recent assistant message is
  `$$a2ui:{"components":[{"props":{"title":"Cuéntame más sobre tu
  feria"}}]}`, When the list endpoint is called, Then the preview
  MUST equal `Cuéntame más sobre tu feria` (≤120 chars).
- **AC-204**: *Pre-existing routing-JSON rows filtered.* Given a
  session with a legacy pre-REQ-104 row of form
  `{"restated_goal":"…","questions":[…]}`, When the list endpoint
  is called, Then that row MUST NOT be used as preview source.
  Older text-based or A2UI rows MUST be used instead.

### Frontend — Chat-switch race

- **AC-301**: *No ghost chunks.* Given chat A is actively streaming
  (a `STREAM_CHUNK` is in flight in the network layer), When the user
  clicks chat B in the sidebar, Then no message from chat A's stream
  MUST appear in chat B's DOM after `SESSION_LOADED` for chat B
  completes. Verified in a test that mocks SSE and races the chunk
  delivery against the reducer dispatch.
- **AC-302**: *Stale action defence.* Given the reducer has
  `state.sessionId = "B"`, When a late `STREAM_CHUNK` arrives with
  `sessionId = "A"`, Then `state.messages` MUST be unchanged.
- **AC-303**: *Rehydrated rows are not streaming.* Given a
  `SESSION_LOADED` action with any message whose `isStreaming` field
  happens to be `true` in the input payload, When the reducer
  processes it, Then the resulting `state.messages[*].isStreaming`
  MUST all be `false`.
- **AC-304**: *Logout hygiene.* Given the reducer is in any state
  (including `running`), When the user logs out and logs back in and
  opens any chat, Then the reducer MUST not reference any chunk,
  message, or partial content from the pre-logout session.

### Frontend — `generating` state

- **AC-401**: *Rehydrate into `generating`.* Given
  `GET /api/v1/sessions/{id}` returns `state: "running"`, When
  `SESSION_LOADED` is dispatched, Then `state.sessionState` MUST be
  `'generating'`.
- **AC-402**: *Indicator visible, no empty bubble.* Given
  `sessionState === 'generating'` and the last message is a user
  message, When `MessageList` renders, Then the user MUST see the
  existing "Esperando respuesta…" indicator. The DOM MUST NOT contain
  an empty assistant bubble.
- **AC-403**: *HITL overrides `generating`.* Given
  `GET /api/v1/sessions/{id}` returns `state: "hitl_pending"` and the
  last message is `$$a2ui:…`, When `SESSION_LOADED` is dispatched,
  Then the A2UI surface MUST render AND the "generating" indicator
  MUST NOT be present.
- **AC-404**: *First chunk advances state.* Given
  `sessionState === 'generating'`, When any `STREAM_CHUNK` matching
  the current session arrives, Then `sessionState` MUST transition
  to `'running'` before the chunk is applied to `state.messages`.
- **AC-405**: *Sidebar defensive rendering.* Given
  `last_message_preview` starts with `$$a2ui:` or `{"` (defensive
  case; REQ-201 should prevent this backend-side), When
  `ChatSidebar` renders, Then the rendered DOM MUST show the
  i18n-translated "Formulario pendiente" string AND MUST NOT contain
  the raw substring `$$a2ui:`.

### Cross-cutting

- **AC-501**: *Predecessor ACs unchanged.* All AC-001 through AC-017
  from the predecessor spec continue to pass. No behaviour covered
  by those ACs is regressed by the changes in this spec.

## 6. Test Automation Strategy

### Test levels & locations

| Level | Backend | Frontend |
|-------|---------|----------|
| Unit | `cpn/fire_llm_test.go` — `SkipOutputHistory` output-guard semantics (REQ-102), user-message replay suppression (REQ-103), token deposition unchanged (AC-104), backward compat of existing `SkipHistory=true` (AC-103) | `src/hooks/useChat.test.ts` — reducer handles tagged `STREAM_CHUNK` mismatch (AC-302), `SESSION_LOADED` clears streaming flags (AC-303), `generating` state transitions (AC-401/404) |
| Unit | `cpn/persist/preview_test.go` — `RenderablePreview` on fixtures covering A2UI+title, A2UI+no-title, invalid A2UI, routing JSON, plain text, empty session (AC-201..204) | `src/features/chat/a2ui/previewDisplay.test.ts` — `displayPreview` on marker, JSON-ish, plain text inputs (AC-405) |
| Integration | `internal/app/session_service_test.go` (testcontainer Postgres) — drive `t-classify → t-ask → t-clarify` and assert INV-101 via SQL (AC-101); drive `t-ask` and capture mock LLM request for AC-102 | `src/features/chat/ChatContainer.test.tsx` or similar — race test: start streaming on A, switch to B, verify no chunks bleed (AC-301) |
| Integration | HTTP handler test: `GET /api/v1/sessions` returns sanitized previews across the five `RenderablePreview` fixture cases | `src/features/chat/ChatSidebar.test.tsx` — pathological preview input yields friendly string (AC-405, INV-302) |
| End-to-end (manual or Playwright) | — | Scripted: clarify cycle, reload page, verify no raw JSON bubble, only interactive A2UI surface (AC-101 end-to-end from the user's perspective). |

### Frameworks

- **Backend**: Go standard `testing`; testify MAY be used for table-driven
  assertions but is not required. Integration tests reuse the existing
  Postgres testcontainer harness per predecessor GUD-005.
- **Frontend**: Vitest + React Testing Library (existing). The reducer
  race test (AC-301) uses `act()` + a controllable mock SSE transport
  to sequence the race deterministically.

### Test data

- A2UI fixtures MUST include a full-shape `questionnaire` with `choice`
  children, mirroring the row observed in production
  (`0cad37318e8a5cdc28ef730f3319b82a`).
- `RenderablePreview` fixtures MUST include at minimum: (a) A2UI with
  top-level `title`, (b) A2UI with child `label` only, (c) A2UI with
  invalid JSON, (d) routing-JSON row, (e) plain user text, (f) empty
  session.
- Mock LLM client for AC-102 MUST capture the full `LLMRequest` so
  the test can assert the user message is in the context.

### CI/CD integration

- All new tests MUST run in the existing `go test ./...` and
  `npm run test` pipelines. No new CI jobs.
- Testcontainers MUST reuse the existing Docker-based fixtures.
- Coverage floor: `cpn/fire_llm.go` ≥ 90% line, including the new
  `SkipOutputHistory` branch; `persist/preview.go` (new file) ≥ 95%
  line; frontend reducer ≥ 90% branch across the §4.4 state machine.

### Performance testing

- Not applicable. Adding one bool check per LLM transition and one
  string prefix check per preview computation. No perf regression is
  expected.

## 7. Rationale & Context

### Why `SkipOutputHistory` instead of renaming `SkipHistory`

Renaming the existing flag would be a larger blast radius — every call
site and test must update in lockstep. Splitting concerns additively
(REQ-101) keeps the diff minimal, preserves backward compatibility
for `t-classify`-style transitions by construction (REQ-107), and
makes the separation of concerns explicit in the config struct itself.
Future transitions author one flag per need.

### Why REQ-103 suppresses the user-side history append too

`fire_llm.go:218-223` appends a synthetic "user" message derived from
consumed tokens. For `t-ask`, the consumed `p-classified` token is
routing metadata — not a user turn. Persisting it to `c.History`
produces a phantom "user" bubble on rehydration that is just as
confusing as the raw JSON "assistant" bubble this spec removes.
Suppressing both together is the complete fix.

### Why preview sanitization lives in the backend, not the frontend

`last_message_preview` is shared across all clients (web, potential
mobile, potential A2A) and is already computed once server-side
(REQ-202 picks a single site). Moving the transformation to the
client would require duplicating the A2UI parser in every client.
The frontend defensive sanitizer (REQ-405) is purely belt-and-braces
against a server-side miss — not the primary mechanism.

### Why a separate `generating` state instead of reusing `running`

`running` implies chunks are actively flowing. On rehydration into a
session whose CPN is mid-LLM-call, no chunks are flowing (SSE has no
replay); the session is running from the backend's perspective but
silent from the frontend's. Using `running` for this case would
require every consumer to special-case "running but no chunks yet",
a distinction `generating` encodes directly. The transition to
`running` on first chunk is the disambiguation.

### Why chat-switch race hardening rather than just trusting `RESET`

Commit `baa59f5` introduced `RESET` on chat switch, which is correct.
However, between the click handler and the reducer batch that processes
`RESET → SESSION_LOADED`, an in-flight SSE chunk from chat A can land
and be dispatched. The tagged-action defence (REQ-302) and explicit
abort (REQ-301) eliminate this race without relying on React's
batching guarantees, which are version-dependent.

### Why SSE replay stays deferred

Implementing replay requires: (a) a broker ring buffer, (b) event
sequencing with `Last-Event-ID`, (c) ordering semantics between REST
history and SSE stream, (d) a per-session TTL policy. Each is
tractable but the combination is a distinct piece of architecture that
deserves its own spec. Deferring keeps this PR surgical. The
observable user-impact from the missing replay — losing mid-stream
plan text on chat switch — is mitigated (not eliminated) by the
`generating` state affordance: users see a clear "generating…"
indicator rather than an empty bubble, and the plan appears when
`persistAfterRun` fires at `t-plan` completion.

### Why no marker string change

The marker is the cross-language protocol contract. Any change would
require coordinated migration in backend, frontend, tests, potential
external agents, and any persisted rows that embed the literal. The
defects in this spec are solved without touching the marker, per
CON-101.

### Why defensive `displayPreview` on the frontend

Defence in depth. If a future persistence backend is added (e.g.,
a new driver) and misses the `RenderablePreview` helper, the frontend
still won't leak a raw marker into the sidebar. The cost is a
~10-line pure function. Net benefit is positive.

## 8. Dependencies & External Integrations

### Internal dependencies

- **INT-101**: `cpn/fire_llm.go` — modified per §4.1 and REQ-101..107.
- **INT-102**: `cpn/` `LLMConfig` struct declaration — modified to
  add `SkipOutputHistory bool`.
- **INT-103**: `cmd/server/topologies.go` — `t-ask` configuration
  modified per REQ-104.
- **INT-104**: `cpn/persist/` — new `RenderablePreview` helper file or
  function, consumed by both persistence backends.
- **INT-105**: `cpn/persist/memory.go` — call the new helper at its
  preview computation site (currently ~line 206 per predecessor
  investigation).
- **INT-106**: `store/postgres/session.go` — call the new helper at
  its preview computation site (currently ~lines 257-263).
- **INT-107**: `internal/driving/httpapi/handler_session.go` —
  `SessionDetailResponse` gains additive `state` field per REQ-404.
- **INT-108**: `front/react-assistant/src/hooks/useChat.ts` — reducer
  and action creators modified per REQ-301..304, REQ-401..404,
  AC-303.
- **INT-109**: `front/react-assistant/src/features/chat/ChatSidebar.tsx`
  — uses new `displayPreview` per REQ-405.
- **INT-110**: `front/react-assistant/src/features/chat/a2ui/previewDisplay.ts`
  (new) — pure function per GUD-102.
- **INT-111**: `front/react-assistant/src/i18n` — adds
  `chat:sidebar.pendingForm` key in all supported locales
  (minimum: es + en).

### External systems

- **EXT-101**: Postgres — `messages` and `sessions` tables,
  unchanged schema, different preview payload.

### Third-party services

- **SVC-101**: None.

### Infrastructure dependencies

- **INF-101**: None.

### Data dependencies

- **DAT-101**: Existing `messages.content TEXT` rows created before
  this merge MAY contain raw-JSON `t-ask` rows. These MUST NOT
  corrupt preview rendering (AC-204) but are not deleted or
  migrated by this spec.

### Technology platform dependencies

- **PLT-101**: Go 1.22+ (existing).
- **PLT-102**: React 19 + TypeScript (existing).
- **PLT-103**: Postgres 16 (existing).

### Compliance dependencies

- **COM-101**: None. A2UI payload titles surfaced in previews per
  REQ-203 are user-authored content already visible in the
  questionnaire itself; no new information is exposed.

## 9. Examples & Edge Cases

### 9.1 `t-ask` fires without polluting history

```go
// Before this spec:
// c.History after t-ask = [... userMsg, userTokenEnvelope, rawJSONQuestionnaire]
// Messages table row for rawJSONQuestionnaire ends up rendered as markdown bubble on rehydration.

// After this spec (t-ask: SkipHistory=false, SkipOutputHistory=true):
// c.History after t-ask = [... userMsg]    // unchanged
// t-ask LLM still reads userMsg from history (input side).
// Token deposited to p-questions carries the raw JSON (unchanged downstream).
// No new c.History entries from t-ask.
```

### 9.2 Preview with A2UI title

```
messages (newest first):
  $$a2ui:{"components":[{"props":{"title":"Cuéntame más sobre tu feria"}, "children":[…]}]}

RenderablePreview output: "Cuéntame más sobre tu feria"
```

### 9.3 Preview with A2UI child label only

```
messages (newest first):
  $$a2ui:{"components":[{"props":{}, "children":[{"props":{"label":"¿Qué tipo de producto vas a mostrar?"}}]}]}

RenderablePreview output: "¿Qué tipo de producto vas a mostrar?"
```

### 9.4 Preview skips pre-existing routing-JSON row

```
messages (newest first):
  {"restated_goal":"…","questions":[…]}                  # legacy row (pre-REQ-104)
  Quiero que me ayudes mañana tengo una feria…          # user

RenderablePreview output: "Quiero que me ayudes mañana tengo una feria…"
```

### 9.5 Chat switch during active stream

```
Timeline:
  t0  User clicks chat B in sidebar while chat A is streaming.
  t1  useChat dispatches RESET → state.messages = [], state.sessionId = null.
  t2  useChat aborts SSE subscription to A (synchronous; REQ-301).
  t3  GET /api/v1/sessions/B returns messages + state="running".
  t4  SESSION_LOADED dispatched with sessionId=B, state='generating' (REQ-401).
  t5  Late STREAM_CHUNK from A's aborted connection arrives tagged sessionId=A.
  t6  Reducer sees sessionId mismatch (A !== B), returns state unchanged (REQ-302).
  t7  MessageList renders B's persisted messages + "Esperando respuesta…" indicator.

Outcome: no text from A bleeds into B. User sees B's actual state.
```

### 9.6 Sidebar defensive display

```tsx
// Input: last_message_preview = "$$a2ui:{\"components\":…"
// Backend should have sanitized (REQ-201). If a bug lets this through:
displayPreview("$$a2ui:{...", t)  → t('chat:sidebar.pendingForm') → "Formulario pendiente"

// Input: last_message_preview = "Hola. ¿En qué puedo ayudarte hoy?"
displayPreview("Hola. ¿En qué puedo ayudarte hoy?", t) → "Hola. ¿En qué puedo ayudarte hoy?"
```

### 9.7 HITL pending masks the "generating" indicator

```
GET /api/v1/sessions/B response:
  state: "hitl_pending"
  messages: [ …user…, assistant $$a2ui:{...} ]

Reducer:
  sessionState: 'idle'          (REQ-403: hitl_pending is not 'generating')
  messages: [ …user…, assistant with isStreaming:false and content=$$a2ui:... ]

DOM: interactive A2UI surface. No loading spinner, no empty assistant bubble.
```

### 9.8 Logout during streaming

```
User is mid-stream in chat A.
User clicks "Cerrar sesión".

Expected sequence:
  1. auth:expired OR explicit logout fires.
  2. useChat aborts SSE subscription (REQ-304).
  3. useChat dispatches RESET.
  4. Any module-level chunk buffers are cleared.
  5. Login page renders.
  6. User logs back in, opens chat A.
  7. GET /api/v1/sessions/A returns only persisted content.
  8. No pre-logout chunk appears in chat A's DOM.
```

### 9.9 Invalid A2UI payload in preview source

```
messages (newest first):
  $$a2ui:{not-valid-json
  Quiero una feria…

extractA2UITitle fails → skip row → next row is renderable →
RenderablePreview output: "Quiero una feria…"
```

## 10. Validation Criteria

- **V-101**: `go test ./cpn/...` passes, including new
  `fire_llm_test.go` cases for `SkipOutputHistory` and
  `persist/preview_test.go` cases for `RenderablePreview`.
- **V-102**: `go test ./internal/app/...` passes including the
  integration test that asserts INV-101 via a real Postgres round
  trip through the testcontainer harness.
- **V-103**: `npm run test` in `front/react-assistant/` passes
  including new reducer tests for REQ-302, REQ-303, REQ-401..404,
  and the `displayPreview` unit tests.
- **V-104**: `make lint` and `make test` from the repo root succeed
  with zero new findings.
- **V-105**: Coverage thresholds: `cpn/fire_llm.go` ≥ 90% line,
  `persist/preview.go` ≥ 95% line, `useChat.ts` ≥ 90% branch on the
  §4.4 state machine.
- **V-106**: Manual smoke test matrix (documented in PR description):
  1. Start a clarify cycle; verify the sidebar preview is the
     questionnaire title, not `$$a2ui:…`.
  2. Reload during HITL; verify the A2UI surface renders without a
     preceding raw JSON bubble. Inspect Postgres and confirm INV-101.
  3. Switch chats during an active `t-plan` stream; return to the
     first chat; verify the "Esperando respuesta…" indicator
     appears, not a ghost bubble.
  4. Log out during HITL, log back in, open the chat; verify
     interactive surface still renders and submits correctly.
- **V-107**: DB audit query after the smoke test above returns the
  expected INV-301 counts (1 `$$a2ui:%` row, 0 `{"restated_goal"%`
  rows) for every affected session.
- **V-108**: All AC-001 through AC-017 from the predecessor spec
  continue to hold (CON-107). Re-run the predecessor's test suite
  as a regression guard.
- **V-109**: No new deprecation warnings, no new ESLint /
  golangci-lint findings, no diff in `rehype-katex` /
  `remark-math` configuration, no change to `package.json` or
  `go.mod`.
- **V-110**: Grep audit — the substring `SkipOutputHistory` appears
  exactly twice in production Go code: the struct field declaration
  and the `fire_llm.go` guard. Additional occurrences allowed only
  in tests and `topologies.go`.

## 11. Related Specifications / Further Reading

- [spec-process-bugfix-a2ui-hitl-rehydration.md](spec-process-bugfix-a2ui-hitl-rehydration.md)
  — **Superseded** by this document. AC-001..AC-017 inherited
  verbatim per CON-107.
- [spec-process-bugfix-a2ui-chunk-boundary-and-persistence.md](spec-process-bugfix-a2ui-chunk-boundary-and-persistence.md)
  — Grandparent spec; superseded transitively.
- [spec-architecture-a2a-a2ui-protocol-integration.md](spec-architecture-a2a-a2ui-protocol-integration.md)
  — Parent protocol spec. §4.6 `A2UI_MARKER`; §9 partial-payload
  fallback; §12 `questionnaire` / `choice`; §12.6 split-chunk
  buffering (deferred).
- [spec-architecture-http-sse-api.md](../back/go-assistant/spec/spec-architecture-http-sse-api.md)
  — HTTP + SSE contract. §9.5 "no SSE replay" is the architectural
  assumption this spec leans on (CON-106).
- **`spec-architecture-sse-replay-and-resume.md` (to be authored)** —
  Follow-up for the replay/resume work deferred by CON-106. When
  authored, that spec would obsolete the `generating` state
  affordance in REQ-401/402 in favour of true chunk replay.
- [spec-process-bugfix-ghost-session-rehydration.md](spec-process-bugfix-ghost-session-rehydration.md)
  — Sibling rehydration bugfix (commit `ff419ca`) relied upon by
  INT-104.
