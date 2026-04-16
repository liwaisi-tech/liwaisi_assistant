---
title: "Bug Fix — Persist A2UI HITL Responses to History and Lock the Surface After Submission"
version: 1.0
date_created: 2026-04-14
last_updated: 2026-04-14
owner: liwaisi-tech
tags: [process, bugfix, a2ui, hitl, persistence, immutability, rehydration, cpn, frontend, backend]
---

## Changelog

- **1.0 (2026-04-14)**: Initial spec. Closes the residual rehydration
  defect identified after
  `spec-process-bugfix-a2ui-rehydration-completion.md` v1.1 shipped: the
  A2UI HITL questionnaire surface persists correctly, but the user's
  ANSWER does not. On rehydration the form re-renders blank and is
  fully editable — the user can re-submit, the prior choice and any
  free-text "Other…" value are gone, and there is no visual lock to
  signal "this was already responded".

# Introduction

`fireHITL` in `back/go-assistant/cpn/hitl.go` emits the A2UI
questionnaire payload, blocks on its channel, receives a
`HITLResponse{Action, Content}` token from the user, and either
synthesizes a downstream token via `OutputBuilder` (the `t-clarify`
path) or deposits the raw human token. **Neither branch records the
human response in `c.History`.** The downstream token flows to
`t-plan` etc., but no row representing the user's answer ever reaches
`session.Messages` and therefore never reaches the `messages` Postgres
table.

On the frontend, `QuestionnaireComponent`
(`front/react-assistant/src/features/chat/a2ui/A2UIMessageRenderer.tsx`)
holds its draft state — selected option ids, free-text values for
"Other…" — entirely in React `useState`. After the user clicks Submit
the reducer marks the message with `hitlResolved`, but that flag is
**not** propagated to the renderer, so the form remains visually
editable. After unmount (chat switch, page reload, logout/login),
rehydration replays the persisted `$$a2ui:` row through the same
component with empty `useState` hooks → blank, editable form, no
trace of the prior answer.

This specification persists the response at the single emission site
(`fireHITL`) by appending a `RoleUser` message to `c.History` whose
`Content` is the canonical JSON of the answers. It propagates the
existing `hitlResolved` flag plus the persisted answer payload into
the A2UI renderer, and switches the renderer into a **locked
read-only mode** that displays the user's chosen options and any
free-text values inline. Both live and rehydrated paths converge on
the same DOM, satisfying the same convergence pattern established by
the predecessor spec (PAT-103).

## 1. Purpose & Scope

**Purpose.** Make A2UI HITL questionnaire answers survive every
session rehydration scenario (chat switch, page reload, logout/login,
backend restart) AND make a submitted questionnaire visually
**immutable**. After this PR:

1. For every successfully resolved A2UI HITL transition, exactly one
   `RoleUser` message MUST exist in the `messages` Postgres table for
   that session, whose `cpn_id` matches the A2UI surface row's
   `cpn_id`, and whose `content` is the canonical JSON of the user's
   answers (option ids and free-text values).
2. On rehydration, the A2UI questionnaire MUST render in a locked
   read-only state: every input MUST be `disabled`/`aria-disabled`,
   every option whose id matches the persisted answer MUST show as
   selected, every "Other…" free-text value MUST be visible verbatim,
   and the Submit/Next buttons MUST be replaced by a non-interactive
   "Respondido" / "Submitted" caption with the resolution timestamp.
3. On live submit, the same locked state MUST take effect immediately
   (no flicker between submit and re-render).

**In scope.**

- Backend (`back/go-assistant`):
  - Append a `RoleUser` message to `c.History` at the resolve site
    in `cpn/hitl.go` (both `OutputBuilder` and raw-deposit branches),
    using the existing `HITLResponse.Content` payload and the
    transition's owning `c.ID` for correlation.
  - Add a new optional `parent_message_id` field to the `Message` /
    `MessageRecord` types and its DTO, populated with the id of the
    A2UI surface row this response answers. Used by the frontend to
    correlate without relying on adjacency or `cpn_id` heuristics.
- Frontend (`front/react-assistant`):
  - Propagate `hitlResolved`, `resolvedPayload`, and `resolvedAt`
    from the reducer through `MessageBubble` →
    `A2UIProviderWrapper` → `QuestionnaireComponent`.
  - On rehydration, derive `resolvedPayload` from the linked
    `RoleUser` message in history and inject.
  - Render the locked state per REQ-104..108.
- Persistence: `messages` schema unchanged except for the additive
  `parent_message_id` column (nullable). Backfill is not required.
- Tests: backend unit + Postgres-roundtrip integration; frontend
  reducer + renderer + e2e fixture covering rehydration + live submit.

**Out of scope.**

- In-progress draft autosave (localStorage-backed partial form state
  before submit). Tracked separately as a follow-up — the bug closed
  here is "post-submit answer lost", not "mid-edit answer lost".
- HITL surfaces other than `questionnaire`+`choice` (e.g.
  `review-card`). Their resolve flow is structurally identical and
  benefits from the backend persistence change for free, but no
  renderer changes are made for them in this spec.
- Changes to the `$$a2ui:` marker, the wire format, or the
  `POST /sessions/{id}/hitl/{transitionId}` request shape.
- SSE replay / `Last-Event-ID` resume (still deferred per CON-106 of
  the predecessor spec).
- Rehydrating draft state for an UNRESOLVED form (the form was
  emitted, the user typed but did not submit, then refreshed). After
  this fix the form re-renders blank but editable, exactly as today —
  no regression, no improvement.

**Audience.** Backend Go engineers modifying `cpn/hitl.go`,
`cpn/session.go`, `internal/app/session_service.go`,
`internal/driving/httpapi/handler_session.go`,
`store/postgres/migrations/`, and tests; frontend React engineers
modifying `hooks/useChat.ts`, `features/chat/MessageBubble.tsx`,
`features/chat/a2ui/A2UIMessageRenderer.tsx`, the A2UI provider
wrapper, and tests.

**Assumptions.**

- Commits `2c8a989` and `1f779cf` are merged. The A2UI surface row is
  already persisted to `messages` with `role='assistant'`,
  `cpn_id=c.ID`, `content` starting with `$$a2ui:`. This spec extends
  that pipeline; it does not revisit it.
- `HITLResponse.Content` is a JSON string for `Action == HITLSubmit`.
  Its top-level shape today is `{"answers":{questionId: optionIdOrFreeText}}`
  per the frontend at `A2UIMessageRenderer.tsx:614-631`. This shape
  is the canonical record persisted by REQ-001.
- `persistAfterRun` (`session_service.go:897-923`) is not currently
  filtering by role — it persists every `c.History` entry — so a
  `RoleUser` append from `fireHITL` will be promoted to the
  `messages` table by the existing pipeline. Verified by reading the
  loop body.
- No external consumer reads `MessageResponse` rows by their position;
  the additive `parent_message_id` field is non-breaking.

## 2. Definitions

| Term | Definition |
|------|-----------|
| **A2UI surface** | A rendered A2UI component tree — for HITL flows today, a `questionnaire` component with `choice` children and optional `allowFreeText` ("Other…") input. |
| **HITL surface row** | The persisted `messages` row whose `role='assistant'` and `content` begins with `$$a2ui:`. Created by `fireHITL` per REQ-001 of the predecessor spec. |
| **HITL response row** | The persisted `messages` row introduced by this spec. `role='user'`, `cpn_id` equal to the HITL surface row's `cpn_id`, `content` is the canonical JSON of the user's answers, and `parent_message_id` references the HITL surface row. |
| **Resolved payload** | The decoded answer object — `{[questionId]: chosenOptionIdOrFreeText}` — derived from `HITLResponse.Content` and used to drive the locked render state on the frontend. |
| **Locked / immutable** | A render mode of the questionnaire in which every form control is non-interactive (`disabled` AND `aria-disabled='true'`), the chosen option is visually marked as selected, free-text answers are shown as a styled non-editable block, and the action footer shows "Respondido" / "Submitted" with the resolution timestamp instead of Submit/Next/Previous. |
| **OutputBuilder path** | The `fireHITL` branch (cpn/hitl.go:221-272) that synthesizes a typed downstream token via `cfg.OutputBuilder(consumed, resp)`. Used by `t-clarify`. |
| **Raw-deposit path** | The `fireHITL` branch (cpn/hitl.go:275-334) that deposits the raw human token to output places. Used by `t-review` and any HITL transition without an `OutputBuilder`. |
| **`HITLResponse`** | `cpn.HITLResponse{Action HITLAction; Content string}` (cpn/session.go:101-109). |
| **`HITLSubmit`** | The `HITLAction` value `"submit"` (cpn/session.go:98). Used for structured questionnaire payloads. |

## 3. Requirements, Constraints & Guidelines

### 3.1 Backend — Persist HITL response in c.History

- **REQ-001**: After a successful HITL channel receive in
  `cpn/hitl.go fireHITL` (i.e. after the channel-receive `case` at
  ~line 194 returns a non-nil `tok` with `Color == ColorHuman` AND
  the action is NOT `HITLReject`), `fireHITL` MUST append a
  `RoleUser` `Message` to `c.History` whose:
  - `Content` is the verbatim `HITLResponse.Content` string when
    `Action == HITLSubmit`, OR
  - `Content` is a single-key canonical JSON `{"action":"approve"}` /
    `{"action":"revise","content":"…"}` etc. for non-submit actions
    where `HITLResponse.Content` is empty or not JSON.
  - `CPNID` MUST equal `c.ID` (matching the A2UI surface row's
    `cpn_id` per REQ-001 of the predecessor spec).
  - `CPNRole` MUST equal `c.Role`.
  - `CPNDepth` MUST equal `c.Depth`.
  - `Timestamp` MUST be `time.Now()` measured at the append site.
  - A new `Message` field `ParentMessageID string` (REQ-006) MUST be
    populated with the id of the most recent A2UI surface row in
    `c.History` whose `CPNID == c.ID` and whose `Content` starts with
    `cpn.A2UIMarker`. If no such row exists in `c.History`
    (defensive; should not occur because `fireHITL` itself appends
    the surface row), the field MUST be left empty and the response
    row is still appended.

- **REQ-002**: REQ-001 MUST execute on BOTH `fireHITL` branches:
  - The `OutputBuilder` branch (cpn/hitl.go:221-272), AFTER
    `cfg.OutputBuilder` returns successfully and BEFORE the output
    token is deposited to `OutputPlaces`. Order matters so that an
    error in `Deposit` does not leave the response unrecorded.
  - The raw-deposit branch (cpn/hitl.go:275-334), AFTER the
    `EventHITLResolved` emission and BEFORE the per-place deposit
    loop.
  - Both appends MUST occur under `c.mu.Lock()` to match the
    convention established by REQ-001 of the predecessor spec
    (`hitl.go:145-154`).

- **REQ-003**: REQ-001 MUST NOT execute when `Action == HITLReject`
  (the function returns `ErrHITLRejected` at hitl.go:213). A
  rejection is not a "response" — the user explicitly declined to
  answer. Rejections are tracked via the existing
  `EventHITLResolved` only.

- **REQ-004**: REQ-001 MUST NOT execute on the channel-error or
  context-cancellation paths (hitl.go:196-204). No response was
  received.

- **REQ-005**: REQ-001 MUST NOT execute when the OutputBuilder
  returns an error (hitl.go:226-229). The transition fails before a
  response can be considered resolved.

- **REQ-006**: A new field `ParentMessageID string` MUST be added to
  `cpn.Message` (`cpn/session.go`) and to `persist.MessageRecord`
  (`cpn/persist/types.go`). The field is OPTIONAL — empty string is
  the legitimate "unlinked" value. Existing call sites MUST NOT be
  required to populate it.

- **REQ-007**: A new column `parent_message_id TEXT` MUST be added
  to the `messages` Postgres table via a new migration in
  `store/postgres/migrations/`. Column is `NULL`-able (no default).
  Existing rows are not backfilled. The persistence layer
  (`store/postgres/session.go`, `cpn/persist/memory.go`) MUST read
  and write the field through to/from `MessageRecord.ParentMessageID`.

- **REQ-008**: The `MessageResponse` HTTP DTO
  (`internal/driving/httpapi/response.go`) MUST gain a new optional
  field `parent_message_id` (json `omitempty`). Populated from
  `MessageRecord.ParentMessageID`. Empty for any row that does not
  link to a parent.

- **REQ-009**: For the `t-clarify` topology specifically
  (`cmd/server/topologies.go` `buildClarifiedToken`), no change to
  the OutputBuilder is required. The persisted response row is
  derived solely from the raw `HITLResponse.Content`, not from any
  downstream-token transformation.

- **REQ-010**: `internal/app/session_service.go`'s history-sync loop
  (`session_service.go:293-307`) and `persistAfterRun`
  (`session_service.go:327`) MUST continue to promote ALL new
  `c.History` entries — including the new `RoleUser` rows from
  REQ-001 — into `session.Messages` and the DB respectively. No code
  change is required if the existing loop is role-agnostic; if it
  filters by role, the filter MUST be relaxed so user rows are
  included.

### 3.2 Frontend — Propagate resolution into the renderer

- **REQ-101**: `useChat.ts` MUST extend `ChatMessage` with two new
  optional fields:
  - `parentMessageId?: string` — populated from
    `MessageResponse.parent_message_id` on rehydration.
  - `resolvedPayload?: Record<string, string>` — populated by the
    reducer when a `RoleUser` row links to an A2UI bubble (see
    REQ-102) AND already populated on the live `HITL_RESOLVED`
    branch.

- **REQ-102**: The `SESSION_LOADED` reducer case
  (`useChat.ts:50-61`) MUST, after mapping messages to `ChatMessage`
  shape, perform a second pass that:
  1. Iterates the user-role messages.
  2. For each user message whose `parentMessageId` is non-empty AND
     whose linked assistant message exists in the loaded list AND the
     linked message's `content` starts with `A2UI_MARKER`, attaches:
     - `hitlResolved: 'submit'` on the linked assistant message.
     - `resolvedPayload: parsedAnswers` on the linked assistant
       message, where `parsedAnswers` is the JSON-parsed `answers`
       field of the user message's `content` (graceful fallback to
       an empty object on parse error).
     - The user message itself MUST be removed from the rendered
       list IF the linked A2UI bubble will display the answer inline
       (REQ-104). Otherwise it MUST stay as a normal user bubble.
       This spec MANDATES inline display, so the user row IS
       suppressed.
  3. The pass MUST be O(n) in the message count using a single
     `Map<id, message>` pre-pass.

- **REQ-103**: The `HITL_RESOLVED` reducer case
  (`useChat.ts:242-251`) MUST be extended to accept and store the
  `payload` (the answers object) and `timestamp` of the resolution.
  Existing callers in `useChat.ts handleResolveHITL`
  (`useChat.ts:356-370`) MUST be updated to pass these. The reducer
  MUST set `resolvedPayload` on the matched message in addition to
  `hitlResolved`.

- **REQ-104**: `MessageBubble.tsx` MUST forward `hitlResolved`,
  `resolvedPayload`, and the resolution timestamp into the A2UI
  provider/wrapper props so the `QuestionnaireComponent` can read
  them.

- **REQ-105**: `QuestionnaireComponent`
  (`A2UIMessageRenderer.tsx:564`) MUST detect a non-empty
  `resolvedPayload` prop and switch into a locked render mode:
  - All `<input>` and `<button type="radio">` elements MUST receive
    `disabled` AND `aria-disabled='true'`.
  - For each question, the option whose id equals
    `resolvedPayload[questionId]` MUST render visually selected
    (existing checked styling). Other options MUST render dimmed
    (e.g. opacity 0.4) to make the contrast obvious.
  - If `resolvedPayload[questionId]` does NOT match any option id
    (i.e. the user picked "Other…" and typed free-text), the
    free-text VALUE MUST be rendered inside the disabled "Other…"
    input box (or a styled non-editable block immediately below the
    options).
  - The pagination footer (Previous / Next) MUST be hidden.
  - The Submit button MUST be replaced by a non-interactive caption
    (e.g. badge) reading the i18n key
    `chat:a2ui.questionnaire.respondedAt` interpolating the
    resolution timestamp.

- **REQ-106**: When `resolvedPayload` is present, the renderer's
  internal `useState` hooks (`answers`, `freeTextMode`,
  `freeTextValues`, `stepIndex`) MUST be initialised from the
  payload — `answers[qid]` set when the payload value matches an
  option id, `freeTextMode[qid]=true` and `freeTextValues[qid]=value`
  when it does not. This keeps the rendered DOM coherent if any
  child component reads these states via context.

- **REQ-107**: The locked render MUST be applied identically on the
  live-submit path (i.e. between the optimistic
  `HITL_RESOLVED` dispatch and the next `STREAM_CHUNK` from the
  downstream transition). No flicker between the editable form and
  the locked form.

- **REQ-108**: When `hitlResolved` is present BUT `resolvedPayload`
  is empty (defensive — e.g. a legacy row from before this fix
  shipped), the renderer MUST still apply the disabled state on every
  input AND show the "Respondido" caption, but MUST NOT attempt to
  visually mark any option as selected. This prevents accidental
  re-submission on legacy sessions.

### 3.3 Backwards compatibility & edge cases

- **REQ-201**: Sessions that were resolved BEFORE this fix shipped
  MUST NOT crash the renderer. They have an A2UI surface row but no
  linked `RoleUser` row. The renderer renders them in their original
  blank-editable state (no regression). Re-submitting via the live
  channel works as today and produces the new persisted row from
  this point forward.

- **REQ-202**: The `t-review` HITL transition (raw-deposit branch)
  MUST also produce a `RoleUser` row per REQ-001/002. Its content
  serialisation MUST be `{"action":"approve"}` /
  `{"action":"reject"}` / `{"action":"revise","content":"…"}` /
  `{"action":"submit","content":"…"}` depending on the action. The
  renderer changes (REQ-105..108) target the `questionnaire`
  component only, but the persistence change applies uniformly.

- **REQ-203**: When the user clicks "Reject" on any HITL surface
  (via the existing flow), no `RoleUser` row is appended (REQ-003).
  The frontend MUST still display the existing rejection badge
  (`MessageBubble.tsx:198-210`) by reading `hitlResolved='reject'`
  from the live `HITL_RESOLVED` action. On rehydration of a rejected
  flow, the renderer MUST NOT mark the form as locked-and-answered;
  the rejection badge alone is sufficient.

- **REQ-204**: If the persisted `HITLResponse.Content` JSON is
  malformed (e.g. truncated by an old client), the reducer MUST log
  a warning to `console.warn`, render the bubble in the
  defensive-locked mode (REQ-108), and proceed without throwing.

- **REQ-205**: `parent_message_id` MUST NOT be exposed as a
  client-controlled field on any inbound HTTP request. It is set
  server-side in `fireHITL` (REQ-001) and read-only on
  `MessageResponse`.

### Invariants

- **INV-001**: For every `t-clarify` HITL cycle that completes with
  `Action == HITLSubmit`, the `messages` table MUST contain exactly
  ONE `role='user'` row with `parent_message_id` equal to the A2UI
  surface row's id, AND zero such rows otherwise.

- **INV-002**: For every `t-review` HITL cycle (any action ∈
  `{approve, reject, revise, submit}` EXCEPT `reject`), the
  `messages` table MUST contain exactly ONE `role='user'` row
  whose `cpn_id` equals the t-review-emitting CPN's id.

- **INV-003**: For any rehydrated session, the rendered DOM of an
  A2UI questionnaire bubble whose `hitlResolved` is non-empty MUST
  contain ZERO `<input>` or `<button>` elements with
  `disabled === false`.

- **INV-004**: The byte sequence of `messages.content` for a
  `RoleUser` HITL row MUST be the verbatim
  `HITLResponse.Content` string for `HITLSubmit` actions (no
  re-serialisation, no whitespace normalisation).

### Constraints

- **CON-001**: The `$$a2ui:` marker string MUST NOT change
  (inherited from CON-001 of the predecessor spec).
- **CON-002**: The `POST /sessions/{id}/hitl/{transitionId}` request
  shape MUST NOT change. The fix consumes the existing
  `{action, content}` payload as-is.
- **CON-003**: The `ResolveHITLRequest` DTO MUST NOT gain new
  fields. Backend-side persistence requires no client cooperation.
- **CON-004**: The CPN domain (`cpn/`) MUST NOT import anything
  from `internal/app/` or `internal/driving/`. REQ-001's append uses
  the existing `c.History` channel between the CPN layer and the
  app layer.
- **CON-005**: The new `parent_message_id` column is the ONLY
  schema migration introduced here. No additional indexes are
  required for this fix (the linkage is consumed at session-detail
  load time, which already O(n)-walks the message list).
- **CON-006**: SSE replay remains out of scope. The `RoleUser` row
  emitted by REQ-001 is delivered to the live client via the
  existing `EventHITLResolved` event combined with the optimistic
  reducer update; rehydration uses REST history.
- **CON-007**: The `RoleUser` rows persisted by REQ-202 (t-review)
  MUST NOT be re-injected into LLM prompts. The downstream
  transitions consume the synthesized output token via the CPN
  pipeline, not via history. If a future LLM transition runs after
  a HITL resolve and reads `c.History`, the new `RoleUser` row will
  legitimately be part of the conversation context — that is the
  intended semantics. (Today's `t-plan` is gated by `SkipHistory`
  for some configurations; verify per-transition flags but do not
  change them in this spec.)

### Guidelines

- **GUD-001**: Keep the persisted JSON shape canonical. The
  frontend's `JSON.stringify({answers: consolidated})` already
  produces a stable shape; persist it byte-identical (do NOT decode
  + re-encode in `fireHITL`).
- **GUD-002**: Prefer adding regression tests that round-trip the
  persisted row through Postgres (testcontainer) so the
  `parent_message_id` linkage is exercised end-to-end.
- **GUD-003**: For the renderer's locked mode, reuse the existing
  Tailwind classes (`opacity`, `cursor-not-allowed`,
  `pointer-events-none`) rather than inventing new ones. A11y attrs
  MUST be added explicitly.
- **GUD-004**: When the A2UI surface row exists but the linked
  `RoleUser` row is missing (legacy flow), prefer rendering the
  blank editable form over crashing. Defensive nulls everywhere.

### Patterns

- **PAT-001**: **Single-site persistence.** The HITL response row
  is appended to `c.History` at the resolve site (`fireHITL`) and
  flows through the existing `persistAfterRun` pipeline. No new
  persistence call is added.
- **PAT-002**: **Convergent render pipelines.** Live-submit and
  history-rehydration paths feed identical inputs into the
  questionnaire renderer. The component does not branch on which
  path it came from; it branches on the presence of
  `resolvedPayload`.
- **PAT-003**: **Server-set linkage.** `parent_message_id` is
  populated server-side by reading the most recent A2UI surface row
  on `c.History`. Clients never set it. Tampering is impossible at
  the API surface.

## 4. Interfaces & Data Contracts

### 4.1 Go: `cpn.Message` extension

```go
// back/go-assistant/cpn/session.go (add field)
type Message struct {
    ID        string
    Role      MessageRole
    Content   string
    CPNID     string
    CPNRole   string
    CPNDepth  int
    Timestamp time.Time

    // ParentMessageID, when non-empty, references the ID of an earlier
    // Message that this row is a response to. Populated by fireHITL when
    // appending a RoleUser response row, with the id of the A2UI surface
    // row the response answers. Empty for all other messages.
    // See spec-process-bugfix-a2ui-hitl-response-persistence.md REQ-006.
    ParentMessageID string
}
```

### 4.2 Go: `persist.MessageRecord` extension

```go
// back/go-assistant/cpn/persist/types.go (add field)
type MessageRecord struct {
    ID              string
    SessionID       string
    Role            string
    Content         string
    CPNID           string
    CPNRole         string
    CPNDepth        int
    Timestamp       time.Time
    ParentMessageID string // REQ-007 (NEW)
}
```

### 4.3 SQL: migration

```sql
-- store/postgres/migrations/<NNNN>_add_message_parent_id.up.sql
ALTER TABLE messages
    ADD COLUMN parent_message_id TEXT;

-- No backfill. Existing rows have NULL parent_message_id.
-- No index added (linkage is consumed at session-detail load time).
```

```sql
-- store/postgres/migrations/<NNNN>_add_message_parent_id.down.sql
ALTER TABLE messages
    DROP COLUMN parent_message_id;
```

### 4.4 Go: `fireHITL` append (pseudocode)

```go
// back/go-assistant/cpn/hitl.go — inside fireHITL, AFTER channel-receive
// success and BEFORE OutputBuilder/raw deposit. Per REQ-001..006.

func appendHITLResponseToHistory(c *CPN, tok Token) {
    resp, ok := tok.Payload.(HITLResponse)
    if !ok {
        return // defensive; channel guarantees HITLResponse for HITL transitions
    }
    if resp.Action == HITLReject {
        return // REQ-003: rejection is not a response
    }

    var content string
    switch {
    case resp.Action == HITLSubmit && resp.Content != "":
        content = resp.Content // GUD-001: byte-identical
    case resp.Content != "":
        content = fmt.Sprintf(`{"action":%q,"content":%q}`, resp.Action, resp.Content)
    default:
        content = fmt.Sprintf(`{"action":%q}`, resp.Action)
    }

    // Find most recent A2UI surface row on c.History matching c.ID, for parent linkage.
    var parentID string
    c.mu.RLock()
    for i := len(c.History) - 1; i >= 0; i-- {
        m := c.History[i]
        if m.CPNID == c.ID && strings.HasPrefix(m.Content, A2UIMarker) {
            parentID = m.ID
            break
        }
    }
    c.mu.RUnlock()

    c.mu.Lock()
    c.History = append(c.History, &Message{
        Role:            RoleUser,
        Content:         content,
        CPNID:           c.ID,
        CPNRole:         c.Role,
        CPNDepth:        c.Depth,
        Timestamp:       time.Now(),
        ParentMessageID: parentID,
    })
    c.mu.Unlock()
}
```

This helper is invoked from both branches:

```go
// OutputBuilder branch — insert before deposit loop (~line 247)
appendHITLResponseToHistory(c, tok)

// Raw-deposit branch — insert before deposit loop (~line 298)
appendHITLResponseToHistory(c, tok)
```

### 4.5 HTTP: `MessageResponse` additive field

```json
{
  "id": "string",
  "role": "user | assistant | observer",
  "content": "string — verbatim from messages.content",
  "cpn_id": "string (optional)",
  "parent_message_id": "string (optional, NEW per REQ-008)",
  "timestamp": "RFC 3339 string"
}
```

### 4.6 Frontend: `ChatMessage` extension

```ts
// front/react-assistant/src/types/chat.ts
export interface ChatMessage {
  id: string;
  role: 'user' | 'assistant' | 'observer';
  content: string;
  isStreaming: boolean;
  cpnId?: string;
  cpnRole?: string;
  timestamp: Date;

  // Existing HITL flags
  hitlTransitionId?: string;
  hitlActions?: HITLAction[];
  hitlResolved?: HITLAction;

  // NEW — REQ-101
  parentMessageId?: string;
  resolvedPayload?: Record<string, string>; // questionId → optionId | freeText
  resolvedAt?: Date;
}
```

### 4.7 Frontend: SESSION_LOADED enrichment (pseudocode)

```ts
// useChat.ts SESSION_LOADED case, after mapping messages to ChatMessage
function enrichWithResolutions(messages: ChatMessage[]): ChatMessage[] {
  const byId = new Map(messages.map(m => [m.id, m]));
  const toSuppress = new Set<string>();

  for (const m of messages) {
    if (m.role !== 'user' || !m.parentMessageId) continue;
    const parent = byId.get(m.parentMessageId);
    if (!parent || parent.role !== 'assistant') continue;
    if (!parent.content.trimStart().startsWith(A2UI_MARKER)) continue;

    // Parse the canonical answers payload.
    let answers: Record<string, string> = {};
    try {
      const parsed = JSON.parse(m.content);
      if (parsed && typeof parsed.answers === 'object') {
        answers = parsed.answers as Record<string, string>;
      }
    } catch {
      console.warn('A2UI HITL response JSON malformed; falling back to defensive lock', m.id);
    }

    // Mutate parent (immutably) — record the resolution.
    parent.hitlResolved = 'submit';
    parent.resolvedPayload = answers;
    parent.resolvedAt = m.timestamp;

    // Suppress the user row from the rendered list (REQ-102).
    toSuppress.add(m.id);
  }

  return messages.filter(m => !toSuppress.has(m.id));
}
```

### 4.8 Frontend: questionnaire locked-mode contract

| Visual element | Editable mode (today) | Locked mode (this spec) |
|----------------|------------------------|--------------------------|
| Radio option matching `resolvedPayload[qid]` | unchecked / checked on click | **checked**, `disabled`, opacity 1.0 |
| Other radio options | enabled | `disabled`, `aria-disabled='true'`, opacity 0.4, `cursor-not-allowed` |
| "Other…" text input | editable | `disabled`, value = persisted free-text, no caret |
| Previous / Next / Submit buttons | visible | hidden |
| Footer caption | none | "Respondido — {timestamp}" (i18n: `chat:a2ui.questionnaire.respondedAt`) |
| Question step indicator | "Question 1 of 2" | hidden OR replaced by "{N} respuestas" caption |

## 5. Acceptance Criteria

### Backend persistence

- **AC-001**: *RoleUser row persisted on submit.* Given a session
  that completes the `t-classify → t-ask → t-clarify` path with
  `Action == HITLSubmit` and `Content` of
  `'{"answers":{"q1":"opt-a"}}'`, When the run is persisted,
  Then the `messages` table MUST contain exactly ONE row with
  `role='user'`, `cpn_id` equal to the cpn id of the t-clarify
  surface, `content = '{"answers":{"q1":"opt-a"}}'`, and
  `parent_message_id` equal to the t-clarify A2UI row id.

- **AC-002**: *No row on reject.* Given the same session with
  `Action == HITLReject`, When persisted, Then ZERO additional
  user-role rows MUST exist for that cpn id beyond what was already
  there pre-resolve.

- **AC-003**: *Verbatim content.* Given a persisted submit with
  `Content = '{"answers":{"q1":"texto con \"comillas\""}}'`, When
  the row is read back via `GET /api/v1/sessions/{id}`, Then
  `MessageResponse.content` MUST equal the input string
  byte-identically.

- **AC-004**: *parent_message_id surfaces in DTO.* Given AC-001's
  row, When `GET /api/v1/sessions/{id}` is called, Then the user
  row's `parent_message_id` MUST equal the A2UI assistant row's
  `id`.

- **AC-005**: *Migration is reversible.* `migrate up` then
  `migrate down` MUST not error and MUST leave the schema
  byte-equivalent to pre-migration.

### Frontend — locked rendering

- **AC-101**: *Locked render after live submit.* Given a freshly
  rendered questionnaire, When the user selects an option and clicks
  Submit, Then within one render frame the form MUST be in locked
  mode: every radio is `disabled`, the chosen option is visually
  selected, the Submit button is gone, and the "Respondido —
  {timestamp}" caption is visible.

- **AC-102**: *Locked render after rehydration.* Given a session
  with a persisted A2UI surface AND a linked `RoleUser` response
  row, When the chat is opened (chat-switch / page-reload /
  logout-login), Then the questionnaire MUST render in locked mode
  per AC-101.

- **AC-103**: *Free-text "Other…" preserved.* Given the user
  selected "Other…" and typed `"Mi respuesta personalizada"`, then
  submitted, Then on rehydration the disabled "Other…" input box
  MUST display `Mi respuesta personalizada` verbatim AND the option
  radio MUST be visually selected.

- **AC-104**: *No re-submission possible.* Given a locked-mode
  questionnaire, When the user attempts to click any input or any
  hidden Submit (e.g. by inspect-element re-enabling), Then the
  reducer MUST NOT dispatch `HITL_RESOLVED` again. Defensive: even
  if a click event reaches the form, `handleSubmit` MUST early-return
  when `resolvedPayload` is present.

- **AC-105**: *Legacy session graceful fallback.* Given a session
  resolved BEFORE this PR shipped (A2UI surface persisted, no
  linked user row), When the chat is opened, Then the questionnaire
  MUST render in its original blank-editable state (no crash, no
  stuck spinner).

- **AC-106**: *Rejected flow.* Given the user clicked "Reject" on
  any HITL surface, When the chat is rehydrated, Then the bubble
  MUST display the existing rejection badge AND MUST NOT lock the
  form (the form is already irrelevant — the flow was abandoned).

- **AC-107**: *User row suppressed from list.* Given AC-102's
  rehydration, Then the rendered message list MUST NOT contain a
  separate user bubble showing the raw answers JSON. The answers
  appear ONLY inside the locked questionnaire.

- **AC-108**: *Malformed JSON does not crash.* Given a persisted
  user row whose `content` is not valid JSON (defensive), When
  rehydrated, Then `console.warn` MUST log a single warning AND the
  form MUST render in defensive-locked mode (all inputs disabled,
  no option marked, "Respondido" caption visible).

### Cross-cutting

- **AC-201**: *Predecessor ACs unchanged.* All AC-001..AC-017 from
  `spec-process-bugfix-a2ui-hitl-rehydration.md` v1.2 and AC-101..405
  from `spec-process-bugfix-a2ui-rehydration-completion.md` v1.1
  continue to hold.

- **AC-202**: *Backend tests are integration-grade.* The persistence
  assertions in AC-001..004 MUST be exercised against a real
  Postgres testcontainer (per GUD-005 of the predecessor spec), not
  a mock.

## 6. Test Automation Strategy

### Test levels & locations

| Level | Backend | Frontend |
|-------|---------|----------|
| Unit | `cpn/hitl_test.go` — `appendHITLResponseToHistory` table tests across submit/approve/revise/reject + missing parent (REQ-001..005) | `src/hooks/useChat.test.ts` — `enrichWithResolutions` correctness (REQ-102), `HITL_RESOLVED` extended payload (REQ-103) |
| Unit | `cpn/session_test.go` — `Message.ParentMessageID` round-trip | `src/features/chat/a2ui/QuestionnaireComponent.test.tsx` — locked render: chosen option highlighted, others disabled+dimmed, free-text shown, Submit hidden (AC-101..104) |
| Integration | `internal/app/session_service_test.go` — extend `TestSessionService_AskThenHITL_NoRawTAskRowPersisted` to ALSO assert: exactly one `role='user'` row exists with the linked `parent_message_id`, content byte-identical (AC-001/003/004) | `src/features/chat/MessageBubble.test.tsx` — full SESSION_LOADED → MessageBubble → A2UIRenderer pipeline with a fixture pair (assistant + linked user), assert locked DOM (AC-102) |
| Integration | `store/postgres/session_test.go` — round-trip a row with `ParentMessageID` set; SELECT returns same value | `src/features/chat/ChatContainer.test.tsx` — chat-switch scenario: load session A with resolved questionnaire, switch, switch back, assert locked render (AC-102 cross-cutting) |
| End-to-end (manual) | — | Scripted: submit → reload → form locked + answers visible (V-006 in §10) |

### Frameworks

- **Backend**: Go standard `testing`, testify optional. Postgres
  integration via the existing testcontainer harness.
- **Frontend**: Vitest + React Testing Library. Locked-state assertions
  use `getByRole('radio', { name })` + `expect(el).toBeDisabled()`.

### Test data

- A2UI questionnaire fixture MUST include one `choice` question whose
  options include "Other…" (`allowFreeText`), so AC-103's free-text
  branch is exercised.
- A submit payload fixture for AC-003 MUST contain double-quotes
  inside a string value to prove byte-identical round-trip.
- A legacy fixture (assistant A2UI row, no linked user row) MUST be
  included to prove AC-105.
- A malformed JSON fixture (`{not-valid`) MUST be included to prove
  AC-108.

### CI/CD integration

- All new tests run in the existing `go test ./...` and
  `npm run test` pipelines. No new CI jobs.
- The new migration MUST be applied automatically by the existing
  migrations runner (`store/postgres/migrate.go` or equivalent). No
  manual ops step.

### Coverage expectations

- `cpn/hitl.go` HITL-resolve branch: 100% line + branch on the new
  `appendHITLResponseToHistory` paths.
- `useChat.ts` reducer `enrichWithResolutions`: branch coverage on
  (parent missing | parent not A2UI | content not JSON | content
  valid).
- `QuestionnaireComponent` locked mode: branch coverage on
  (`resolvedPayload` present | empty defensive lock | absent
  editable).

### Performance testing

- Not applicable. The new persistence adds at most one `Message` per
  HITL resolve. The frontend enrichment is O(n) over an already-O(n)
  load path.

## 7. Rationale & Context

### Why persist via `c.History` instead of a new column or table

`session_service.go` already syncs `c.History` into `session.Messages`
and writes them via `persistAfterRun`. Appending one more row reuses
the established pipeline and matches the established pattern from
REQ-001 of the predecessor spec (the A2UI surface row itself).
Introducing a sidecar table (`hitl_responses`) would duplicate the
write path and create a second source of truth that could drift.

### Why a new column instead of inferring linkage from adjacency

Adjacency-based linkage ("the user row immediately following an
assistant `$$a2ui:` row is the response") works today but is fragile:
any future event that interleaves rows (e.g., observer messages,
parallel transitions, mid-run user-side messages) breaks the
heuristic. An explicit `parent_message_id` column adds 8 bytes per
row and makes the linkage machine-verifiable.

### Why not cache `resolvedPayload` in localStorage

localStorage is per-browser and per-origin. The user can be on a
different device, in a private window, or after a clear-storage
event. Backend persistence is the only correct source. Once the
backend persists (this PR), the frontend has no reason to also
maintain a separate cache.

### Why suppress the user row from the rendered list

If the rendered list shows BOTH the locked questionnaire AND a
separate user bubble with `{"answers":{"q1":"opt-a"}}`, the user
sees their answer twice — once nicely formatted in the lock, once
as raw JSON. UX harm. The locked questionnaire IS the answer
display. Hiding the row from the message list is the right
trade-off; the row remains in the database for downstream agents and
audits.

### Why server-set linkage rather than client-supplied

`POST /sessions/{id}/hitl/{transitionId}` already carries enough
information (the `transitionId` plus the latest A2UI emission for
that transition is unambiguous server-side). Adding a
client-supplied `parent_message_id` would add a tampering surface
and a request shape change for no benefit.

### Why lock the form even when payload is empty (defensive mode)

A row with `hitlResolved` but no `resolvedPayload` means the backend
recorded a resolution we cannot interpret. Allowing the form to
remain editable would invite the user to submit AGAIN, racing the
backend and producing a second resolution. Locking the form with no
visual selection is the safe failure mode: the user knows something
happened, can scroll past the bubble, and the next chat turn will
clarify state.

### Why include t-review (REQ-202) when the user-visible defect is t-clarify

t-review uses the raw-deposit branch of `fireHITL` and currently
suffers from the same persistence gap (no record of the user's
approve/reject/revise decision in history). Fixing only `t-clarify`
would leave t-review's audit trail incomplete. The cost of REQ-202
is one extra branch in `fireHITL` (already implemented by REQ-002's
"both branches" requirement). The visible-UI lock (REQ-105..108)
remains questionnaire-only because review-cards have a different
component structure.

## 8. Dependencies & External Integrations

### Internal dependencies

- **INT-001**: `cpn/hitl.go` — `fireHITL` modified per §4.4.
- **INT-002**: `cpn/session.go` — `Message` struct gains
  `ParentMessageID` field per §4.1.
- **INT-003**: `cpn/persist/types.go` — `MessageRecord` gains
  `ParentMessageID` field per §4.2.
- **INT-004**: `cpn/persist/memory.go` and
  `store/postgres/session.go` — read/write the new field.
- **INT-005**: `internal/driving/httpapi/response.go` —
  `MessageResponse` gains optional `parent_message_id` field per
  §4.5; `handler_session.go` populates it.
- **INT-006**: `internal/app/session_service.go` —
  `persistAfterRun` continues to promote `c.History` entries
  unchanged. Verify role-agnostic; relax filter if any exists.
- **INT-007**: `front/react-assistant/src/types/api.ts` and
  `src/types/chat.ts` — extended per §4.6.
- **INT-008**: `front/react-assistant/src/hooks/useChat.ts` —
  reducer extended per §4.7 / REQ-101..103/107.
- **INT-009**: `front/react-assistant/src/features/chat/MessageBubble.tsx`
  — props passthrough per REQ-104.
- **INT-010**: `front/react-assistant/src/features/chat/a2ui/A2UIMessageRenderer.tsx`
  — `QuestionnaireComponent` locked mode per REQ-105..108.
- **INT-011**: `front/react-assistant/src/i18n/locales/{en,es}/chat.json`
  — new key `chat:a2ui.questionnaire.respondedAt` (interpolation
  variable `timestamp`).

### External systems

- **EXT-001**: Postgres `messages` table — gains nullable
  `parent_message_id TEXT` column. No data migration.

### Third-party services

- **SVC-001**: None.

### Infrastructure dependencies

- **INF-001**: None. The migration runs through the existing
  migration mechanism.

### Data dependencies

- **DAT-001**: Pre-existing rows in `messages` MAY have
  `parent_message_id IS NULL`. Application code MUST treat NULL as
  the legitimate "unlinked" value.

### Technology platform dependencies

- **PLT-001**: Go 1.22+ (existing).
- **PLT-002**: React 19 + TypeScript (existing).
- **PLT-003**: Postgres 16 (existing); `ALTER TABLE … ADD COLUMN`
  is online and non-blocking on this column type at our row count.

### Compliance dependencies

- **COM-001**: None. The persisted user response is content the
  user already typed and submitted; no new PII surface.

## 9. Examples & Edge Cases

### 9.1 Submit with single-choice answer

```
HITLResponse received:
  Action  = HITLSubmit
  Content = `{"answers":{"q1":"opt-a"}}`

c.History after fireHITL append:
  [..., {Role: RoleUser, Content: `{"answers":{"q1":"opt-a"}}`,
        CPNID: "cpn-clarify-1", ParentMessageID: "msg-a2ui-7"}]

messages table after persistAfterRun:
  id=msg-resp-9, role=user, cpn_id=cpn-clarify-1,
  parent_message_id=msg-a2ui-7,
  content='{"answers":{"q1":"opt-a"}}'

Frontend on rehydration:
  Locked questionnaire; "Vender el producto en el momento" highlighted;
  other options dimmed+disabled; footer = "Respondido — 07:39 AM".
```

### 9.2 Submit with "Other…" free-text

```
HITLResponse received:
  Action  = HITLSubmit
  Content = `{"answers":{"q1":"Mi respuesta personalizada"}}`

Renderer rehydration:
  resolvedPayload = {"q1": "Mi respuesta personalizada"}
  No option id matches → enter free-text branch:
    "Other…" radio shown checked, disabled.
    Disabled text input shows "Mi respuesta personalizada".
```

### 9.3 Reject

```
HITLResponse received:
  Action = HITLReject

fireHITL: returns ErrHITLRejected; NO RoleUser row appended (REQ-003).
Frontend: receives EventHITLResolved with action='reject'; existing
  rejection badge displayed; form NOT locked (REQ-203/AC-106).
```

### 9.4 Revise (t-review)

```
HITLResponse received:
  Action  = HITLRevise
  Content = "Please tighten the budget section"

fireHITL: appends RoleUser row with
  Content = `{"action":"revise","content":"Please tighten the budget section"}`,
  CPNID = c.ID (t-review's CPN), ParentMessageID = id of preceding A2UI
  review-card row (if any).

Frontend: questionnaire renderer NOT involved (review-card uses a different
  component); existing rejection/revise badge logic in MessageBubble continues.
```

### 9.5 Legacy session (no linked user row)

```
messages table (frozen state):
  id=msg-a2ui-7, role=assistant, content='$$a2ui:{...}'
  (no role=user row with parent_message_id=msg-a2ui-7)

Frontend rehydration:
  enrichWithResolutions finds no parent linkage for any user row.
  Questionnaire renders in editable mode (AC-105) with no warning.
  User can re-submit; submit produces a NEW persisted row from this
  point forward.
```

### 9.6 Malformed answers JSON

```
messages.content = "{not-valid-json"

enrichWithResolutions:
  JSON.parse throws → console.warn(...) → answers = {} (empty)
  parent.hitlResolved = 'submit' (still mark resolved)
  parent.resolvedPayload = {}    (defensive)

Renderer:
  resolvedPayload present + empty → defensive lock mode (REQ-108):
  every input disabled, no option highlighted, "Respondido" caption.
```

### 9.7 Concurrent answer + STREAM_CHUNK race

```
Timeline (live submit):
  t0  User clicks Submit.
  t1  handleSubmit → onAction → onHITLAction → POST /sessions/X/hitl/T
  t2  Optimistic dispatch: HITL_RESOLVED with payload (REQ-103).
      Reducer marks message hitlResolved='submit', resolvedPayload=...
      Renderer immediately shows locked mode (REQ-107).
  t3  Backend processes resolve, fires next transition, emits
      stream_chunk for downstream LLM output.
  t4  STREAM_CHUNK arrives; reducer creates new assistant bubble;
      questionnaire bubble unaffected.
```

### 9.8 SESSION_LOADED suppresses the answer row

```
Backend returns:
  [user "Hola", assistant "Hola...", user "Quiero ayuda...",
   assistant "$$a2ui:{...}" id=msg-a2ui-7,
   user '{"answers":{"q1":"opt-a"}}' id=msg-resp-9 parent=msg-a2ui-7]

After enrichWithResolutions:
  msg-resp-9 is suppressed (REQ-102/AC-107).
  msg-a2ui-7 carries hitlResolved='submit', resolvedPayload={q1:"opt-a"}.

Rendered list:
  [user "Hola", assistant "Hola...", user "Quiero ayuda...",
   <locked questionnaire bubble>]
```

## 10. Validation Criteria

- **V-001**: `go test ./cpn/...` passes, including new
  `appendHITLResponseToHistory` table tests (REQ-001..005).
- **V-002**: `go test ./internal/app/...` passes, including the
  extended INV-101 + new INV-001 checks via Postgres testcontainer
  (AC-001/003/004).
- **V-003**: `go test ./store/postgres/...` passes, including
  round-trip of `ParentMessageID`.
- **V-004**: `npm run test` in `front/react-assistant/` passes,
  including new reducer tests for `enrichWithResolutions` and new
  `QuestionnaireComponent` locked-mode tests.
- **V-005**: `make lint` introduces zero new findings vs base
  branch.
- **V-006**: Manual smoke matrix (PR description):
  1. Open chat, fill questionnaire, submit. Verify locked bubble
     immediately, no flicker.
  2. Reload page. Verify locked bubble persists with chosen option
     highlighted.
  3. Submit "Other…" with free-text. Reload. Verify free-text
     visible inside disabled input.
  4. Switch to a different chat, switch back. Verify lock holds.
  5. Logout, login, open same chat. Verify lock holds.
  6. `psql -c "SELECT id, role, content, parent_message_id FROM
     messages WHERE session_id='…' ORDER BY timestamp;"` shows
     exactly one user-role row with non-null `parent_message_id`
     for the resolved t-clarify cycle.
- **V-007**: All AC-001..AC-017 of the v1.2 predecessor and
  AC-101..AC-405 of the v1.1 sibling continue to pass.
- **V-008**: No diff in `package.json` or `go.mod`.

## 11. Related Specifications / Further Reading

- [spec-process-bugfix-a2ui-rehydration-completion.md](spec-process-bugfix-a2ui-rehydration-completion.md)
  — Sibling spec (v1.1) that established the convergent
  rehydration pattern this spec extends. PAT-101 / PAT-102 / PAT-103
  apply directly.
- [spec-process-bugfix-a2ui-hitl-rehydration.md](spec-process-bugfix-a2ui-hitl-rehydration.md)
  — Predecessor spec (v1.2) that introduced the A2UI surface
  persistence (REQ-001) on which this spec's parent-linkage relies.
- [spec-architecture-a2a-a2ui-protocol-integration.md](spec-architecture-a2a-a2ui-protocol-integration.md)
  — Parent protocol spec; §12 defines `questionnaire` / `choice` and
  the HITL resolve contract.
- [spec-architecture-http-sse-api.md](../back/go-assistant/spec/spec-architecture-http-sse-api.md)
  — HTTP + SSE contract. §4 "Message History" SHOULD be updated in a
  doc PR to note `MessageResponse.parent_message_id` is OPTIONAL and
  populated for HITL resolution rows.
- **Follow-up (deferred)**: in-progress draft autosave for
  unresolved questionnaires. Tracked separately; not blocked by
  this spec.
