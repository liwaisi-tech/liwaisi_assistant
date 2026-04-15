---
title: "Bug Fix — Persist t-review HITL Surface and Response, Fix Locked Questionnaire Payload Parser"
version: 1.0
date_created: 2026-04-15
last_updated: 2026-04-15
owner: liwaisi-tech
tags: [process, bugfix, hitl, a2ui, persistence, rehydration, observability, cpn, frontend, backend]
---

## Changelog

- **1.0 (2026-04-15)**: Initial spec. Closes two coupled defects observed in
  the production session `3a05f35278cd0ae29e8d659185b2a64c` after the user
  re-logged in 24h later: (A) the locked questionnaire renders `—` for every
  answer because the frontend parser hardcodes a wrapped `{"answers":{...}}`
  shape while the live submitter posts a flat `{questionId: value}` shape;
  (B) the `t-review` approve/revise/reject card vanishes after rehydration
  because its surface is emitted by a legacy SSE-only path
  (`cmd/server/main.go buildHITLReviewCard`) that bypasses `c.History`, and
  the persist gate `if a2uiRowID != ""` in `cpn/hitl.go` therefore also
  skips the response row. Spec INV-002 from the predecessor
  (`spec-process-bugfix-a2ui-hitl-response-persistence.md`) is currently
  violated for every t-review cycle.

# Introduction

The predecessor spec
[`spec-process-bugfix-a2ui-hitl-response-persistence.md`](spec-process-bugfix-a2ui-hitl-response-persistence.md)
established that every HITL transition — both `t-clarify` (questionnaire)
and `t-review` (approve/revise/reject card) — MUST persist its A2UI
surface row AND a paired `RoleUser` response row to the `messages` table,
so that rehydration after page reload, chat switch, or logout/login
re-renders the surface in a locked, read-only state with the user's
recorded answer visible inline.

That spec fully implemented the questionnaire path. This spec closes two
residual defects:

- **Defect A** — the canonical persisted shape for questionnaire answers
  is a flat `{questionId: value}` map (because
  `MessageBubble.tsx:83-85` strips the `{answers:...}` envelope before
  POSTing). The locked-render parser at
  `A2UIMessageRenderer.tsx:594-612` only accepts the wrapped shape and
  returns `{}` for any flat row, leaving every field rendered as `—`.
- **Defect B** — `t-review` is configured without an `A2UIPayloadBuilder`
  in `cmd/server/topologies.go:243-245` and `:561-565`. Its review card
  is built and emitted by the legacy event-subscriber path at
  `cmd/server/main.go:256-333` (`buildHITLReviewCard`), which writes
  directly to the SSE broker and never appends to `c.History`. Because
  both `appendHITLResponseToHistory` calls in `cpn/hitl.go` (lines 259
  and 318) are gated on `if a2uiRowID != ""`, the user's
  approve/revise/reject decision is also never persisted. The result:
  zero `messages` rows for either side of every t-review cycle, and a
  blank chat tail after rehydration.

This specification (a) makes the locked-render parser accept BOTH the
flat and wrapped shapes (backwards-compatible — pre-existing rows in
production must display correctly with no data backfill), (b) gives
`t-review` a transition-owned `A2UIPayloadBuilder` so its surface flows
through the same persistence path as `t-clarify`, (c) retires the legacy
SSE-only emit path (downgrading it to a defensive WARN whose presence
indicates a regression), and (d) adds structured `slog`/`console.warn`
tracing at every persistence decision point so the next regression is
debuggable end-to-end.

## 1. Purpose & Scope

**Purpose.** Restore the rehydration invariants the predecessor spec
established, for every HITL transition kind, on every login. After this
PR:

1. Every successful `t-review` cycle whose action is not `reject` MUST
   produce exactly one `messages` row with `role='assistant'` whose
   content begins with `$$a2ui:` AND exactly one row with `role='user'`
   whose `parent_message_id` references that surface row.
2. Every successful `t-clarify` cycle continues to honor the predecessor
   spec's INV-001 (already passing in production).
3. On rehydration of any session with a persisted HITL surface plus
   linked response row, the renderer MUST display the user's recorded
   answer inline — never `—` for a question that was actually answered.
4. The legacy `cmd/server/main.go` review-card emission path MUST no
   longer be the source of `t-review` surfaces; if it ever fires, it
   MUST log a WARN identifying the offending transition so on-call sees
   it in the logs.

**In scope.**

- Frontend (`front/react-assistant`):
  - Patch `parseResolvedAnswers` in
    `src/features/chat/a2ui/A2UIMessageRenderer.tsx` to accept BOTH
    `{"answers":{...}}` (legacy/forward-compat) and flat
    `{questionId: value}` (current emitter) shapes.
  - Add `console.warn` on unrecognized shapes with key set + truncated
    preview so the next mismatch surfaces in DevTools immediately.
- Backend (`back/go-assistant`):
  - Add a transition-owned `A2UIPayloadBuilder` for `t-review` in
    `cmd/server/topologies.go`. The builder MUST return the same
    component tree currently produced by `buildHITLReviewCard` in
    `cmd/server/main.go`. Configure it on BOTH t-review instances
    (default topology at line 241-245 and unified topology at line
    561-565).
  - Move (or delete + reuse) `buildHITLReviewCard` so a single source of
    truth produces the JSON payload.
  - In `cmd/server/main.go`, demote the legacy emit branch
    (`!transitionOwnsSurface`) to a WARN-and-emit defensive fallback.
    The branch MUST log `"legacy review-card emit path used (NOT
    persisted) — t-review topology may be misconfigured"` with
    `transition_id`, `cpn_id`, `session_id`. The t-clarify
    error-recovery banner sub-branch MUST remain unchanged (REQ-PAR-003
    of the model-centralization spec).
  - Add structured `slog` tracing at every persistence decision point
    in `cpn/hitl.go` (REQ-OBS-001..005).
- Tests:
  - Backend unit + integration tests covering the new persisted t-review
    pair (surface + response) for each action (approve, revise,
    reject — reject MUST NOT persist a response row per REQ-003 of the
    predecessor).
  - Frontend Vitest covering `parseResolvedAnswers` for flat, wrapped,
    `{action,content}` envelopes, and malformed input.

**Out of scope.**

- Schema migrations. The `parent_message_id` column already exists from
  the predecessor migration and is fully wired through `MessageRecord`
  and `MessageResponse`.
- Changes to the `POST /sessions/{id}/hitl/{transitionId}` request
  shape, the `ResolveHITLRequest` DTO, or the `$$a2ui:` marker.
- SSE replay / `Last-Event-ID` resume (deferred per CON-106 of
  predecessor).
- Mid-edit draft autosave for unresolved questionnaires (deferred per §1
  out-of-scope of predecessor).
- Changing the visual design of the t-review card. The locked render of
  the t-review card after this fix uses the existing `QuestionnaireLocked`
  / button-locking pattern; no new components.
- Re-canonicalizing the persisted answer shape on the wire. The flat
  shape stays; the parser is what changes. Re-shaping the wire would be
  a breaking change for any other consumer of the historical messages
  table.

**Audience.** Backend Go engineers modifying `cpn/hitl.go`,
`cmd/server/topologies.go`, `cmd/server/main.go`,
`internal/app/session_service.go`, and tests. Frontend React engineers
modifying `features/chat/a2ui/A2UIMessageRenderer.tsx` and tests.

**Assumptions.**

- Branch `feat/fix_bugs_front_and_back` is checked out. The working
  directory contains many uncommitted changes from prior
  model-centralization work. This spec MUST NOT roll those back; it
  only adds what its requirements mandate.
- The `messages` table has a `parent_message_id TEXT NULL` column
  populated from `MessageRecord.ParentMessageID`. Verified against
  production.
- `appendHITLResponseToHistory` already byte-identically persists
  `HITLResponse.Content` for `HITLSubmit` actions and the canonical
  `{"action":"approve"}` / `{"action":"revise","content":"…"}` shapes
  for non-submit actions. Verified by reading `cpn/hitl.go:612-680`.
- The frontend already supports a locked render via
  `QuestionnaireComponent`'s `resolvedPayload` branch
  (`A2UIMessageRenderer.tsx:711-727`). For the t-review card, the
  existing button-disable pattern in `MessageBubble.tsx` covers the
  approve/revise/reject buttons once `hitlResolved` is set on the
  message. The `QuestionnaireLocked` component does NOT need to be
  reused for the review card; the existing message-level lock signal is
  sufficient and is the currently-shipping pattern for live submits.

## 2. Definitions

| Term | Definition |
|------|-----------|
| **HITL surface row** | A `messages` row with `role='assistant'` whose `content` begins with `$$a2ui:`. Created via `c.History` append inside `fireHITL` after `A2UIPayloadBuilder` returns successfully. |
| **HITL response row** | A `messages` row with `role='user'` whose `parent_message_id` references the matching HITL surface row. Created by `appendHITLResponseToHistory` after `fireHITL` receives a non-reject `HITLResponse`. |
| **Flat answer shape** | The persisted JSON shape `{"q1":"value","q2":"value"}` — a top-level object whose keys are question ids and whose values are option ids or free-text strings. Produced by `MessageBubble.tsx:83-85`. |
| **Wrapped answer shape** | The persisted JSON shape `{"answers":{"q1":"value","q2":"value"}}` — assumed by the predecessor spec's parser. Not currently produced by any live emitter, but a legitimate forward-compatible shape that the parser MUST continue to accept. |
| **Action envelope** | The persisted JSON shape `{"action":"approve"}` or `{"action":"revise","content":"…"}` produced by `appendHITLResponseToHistory` for non-`HITLSubmit` actions. The parser MUST NOT confuse this with a flat answer map. |
| **Transition-owned surface** | A HITL surface whose JSON payload is produced by the transition's `cfg.A2UIPayloadBuilder`. The surface IS persisted to `c.History` by `fireHITL`. The frontend signals this via `HITLRequestedPayload{CustomSurface: true}`. |
| **Legacy emit path** | The branch in `cmd/server/main.go` (lines 273-333) that, when `!transitionOwnsSurface`, builds an A2UI review card via `buildHITLReviewCard` and publishes it directly to the SSE broker, bypassing `c.History`. After this fix, this branch is a defensive fallback that MUST NOT be taken in healthy production. |
| **Locked render** | The read-only render mode of a HITL surface after its response row has been recorded. For questionnaires, this is `QuestionnaireLocked`. For review cards, this is the existing `MessageBubble` action-bar disable pattern keyed on `hitlResolved`. |

## 3. Requirements, Constraints & Guidelines

### 3.1 Frontend — Locked questionnaire parser

- **REQ-FE-001**: `parseResolvedAnswers` in
  `front/react-assistant/src/features/chat/a2ui/A2UIMessageRenderer.tsx`
  MUST accept BOTH wrapped (`{"answers":{...}}`) and flat
  (`{questionId: value}`) shapes. Wrapped MUST be tried first; if
  `obj.answers` is a non-null object, its entries form the result. If
  `obj.answers` is absent, the top-level entries form the result.
- **REQ-FE-002**: When falling back to flat-shape acceptance, keys
  `action` and `content` MUST be treated as reserved and MUST cause the
  parser to return `{}` (these signal an action-envelope payload —
  approve/revise/reject — which is not an answer map and MUST NOT be
  rendered as questionnaire answers). Detection: if EITHER of those
  keys is present at top-level AND `obj.answers` is absent, return
  `{}`.
- **REQ-FE-003**: All values in the returned map MUST be coerced to
  string via `String(v ?? '')`. Numeric or boolean answer values
  (defensive against future emitters) MUST not crash the renderer.
- **REQ-FE-004**: When `JSON.parse` succeeds but the value is not an
  object (`null`, array, primitive), the parser MUST return `{}` and
  log `console.warn('parseResolvedAnswers: non-object JSON', preview)`
  with the first 120 characters of `resolvedPayload`.
- **REQ-FE-005**: When the parsed object reaches the catch-all return
  (no recognized shape), the parser MUST log
  `console.warn('parseResolvedAnswers: unrecognized shape', { keys,
  preview })` with the result of `Object.keys(obj)` and the first 120
  characters of `resolvedPayload`. This is the loud signal for the
  next shape regression.
- **REQ-FE-006**: When `JSON.parse` throws, the parser MUST return
  `{}` AND log `console.warn('parseResolvedAnswers: JSON parse error',
  preview)`. No throw escapes the parser.
- **REQ-FE-007**: The parser MUST remain pure (no side effects beyond
  the warnings above) and MUST NOT mutate the input string. It MAY be
  invoked inside `useMemo` as it is today.

### 3.2 Backend — t-review surface persistence

- **REQ-BE-001**: A new function `buildReviewA2UIPayload` MUST be added
  to `cmd/server/topologies.go` (or extracted to a shared helper) with
  the signature
  `func(transitionID string) func(consumed []cpn.Token) (any, error)`.
  Its inner function MUST return the same component tree currently
  produced by `cmd/server/main.go buildHITLReviewCard`, decoded from
  the JSON string into the typed component tree (the existing
  `A2UIPayloadBuilder` contract returns `any`; `fireHITL` then
  re-encodes via `json.Marshal`). The closure receives `transitionID`
  so each card's button `id` props point at the correct transition.
- **REQ-BE-002**: Both `t-review` instances in `topologies.go`
  (default topology at line 241-245 and unified topology at line
  561-565) MUST set
  `tReview.HITLConfig.A2UIPayloadBuilder = buildReviewA2UIPayload("t-review")`.
  The empty `Prompt` field MUST be preserved (per existing rationale
  comment at line 558-560).
- **REQ-BE-003**: The legacy emit branch in `cmd/server/main.go`
  (lines 273-333, the `!transitionOwnsSurface` block) MUST log a WARN
  before publishing the legacy card:
  `slog.WarnContext(ctx, "legacy review-card emit path used (NOT
  persisted) — t-review topology may be misconfigured",
  "session_id", sessionID, "cpn_id", evt.CPNID,
  "transition_id", evt.TransitionID, "prompt_len", len(prompt))`.
  The branch MUST continue to publish the card so a misconfigured
  deploy does not strand the user; the WARN is the regression signal.
- **REQ-BE-004**: The t-clarify error-recovery banner sub-branch
  (lines 281-315 of main.go) MUST be left intact. It is the
  user-facing recovery path when `t-clarify`'s `A2UIPayloadBuilder`
  fails on a flaky LLM JSON output, per REQ-PAR-003 of
  `spec-architecture-model-selection-centralization.md`. After this
  fix, that sub-branch fires only when t-clarify fails — never for
  t-review.
- **REQ-BE-005**: `cpn/hitl.go fireHITL` requires no behavioral
  change for the surface emit / response persist gates. The existing
  `if a2uiRowID != ""` gate at lines 259 and 318 unblocks naturally
  once t-review owns its surface (REQ-BE-002). Do NOT remove the gate
  — defensive value is preserved (legacy untyped HITL transitions
  must not crash).
- **REQ-BE-006**: When `appendHITLResponseToHistory` writes a row for
  the raw-deposit branch (the t-review path), the persisted
  `messages.content` MUST follow the existing serialization rules in
  `cpn/hitl.go:612-660`:
  - `HITLApprove` → `{"action":"approve"}`
  - `HITLRevise` with non-empty content →
    `{"action":"revise","content":"…"}`
  - `HITLSubmit` with non-empty content → `Content` byte-identical
  - `HITLReject` → no row written (per REQ-003 of predecessor)
  No re-serialization or whitespace normalization. (INV-004 of
  predecessor continues to hold.)

### 3.3 Backend — Structured tracing

- **REQ-OBS-001**: Inside `fireHITL` at the surface-emit decision
  (around `cpn/hitl.go:101`, immediately before the
  `if cfg.A2UIPayloadBuilder != nil` check),
  `slog.DebugContext(ctx, "fireHITL surface decision",
  "transition_id", t.ID, "cpn_id", c.ID, "has_builder",
  cfg.A2UIPayloadBuilder != nil)` MUST be emitted. The `ctx` is the
  one passed into `fireHITL`.
- **REQ-OBS-002**: After a successful surface append to `c.History`
  (around `cpn/hitl.go:160`, after `c.mu.Unlock()`),
  `slog.InfoContext(ctx, "fireHITL appended A2UI surface to History",
  "transition_id", t.ID, "cpn_id", c.ID, "a2ui_row_id", a2uiRowID,
  "content_len", len(content))` MUST be emitted.
- **REQ-OBS-003**: At BOTH `appendHITLResponseToHistory` call sites
  (lines 259 and 318 of `cpn/hitl.go`), immediately BEFORE the
  `if a2uiRowID != ""` gate,
  `slog.DebugContext(ctx, "fireHITL hitl-response persistence
  decision", "transition_id", t.ID, "cpn_id", c.ID, "a2ui_row_id",
  a2uiRowID, "branch", "<output_builder|raw_deposit>", "appended",
  a2uiRowID != "")` MUST be emitted.
- **REQ-OBS-004**: Inside `appendHITLResponseToHistory` (around
  `cpn/hitl.go:660`, after the `c.History = append(...)` call),
  `slog.InfoContext(ctx, "appendHITLResponseToHistory wrote row",
  "cpn_id", c.ID, "parent_id", parentID, "action", string(resp.Action),
  "content_len", len(content))` MUST be emitted. NOTE: this requires
  threading `ctx` into `appendHITLResponseToHistory`. The signature MUST
  change to
  `func appendHITLResponseToHistory(ctx context.Context, c *CPN, resp
  HITLResponse, parentID string)`. Both call sites MUST pass the
  `fireHITL` ctx.
- **REQ-OBS-005**: Inside `internal/app/session_service.go`'s
  history-sync path (around line 298, the loop that promotes
  `c.History` deltas to `session.Messages`), at the END of the loop
  (after deciding which rows are new),
  `slog.DebugContext(ctx, "history sync delta", "session_id",
  s.ID, "new_rows", n, "assistant_count", aCount, "user_count",
  uCount)` MUST be emitted. `aCount` and `uCount` count NEW rows by
  role.
- **REQ-OBS-006**: All new `slog` calls MUST use the package-level
  logger (`slog.InfoContext`/`slog.DebugContext`/`slog.WarnContext`
  with the appropriate `ctx`). They MUST NOT take a new logger
  parameter or alter the function signatures beyond REQ-OBS-004.

### 3.4 Frontend — Tracing

- **REQ-FE-008**: `console.warn` calls REQ-FE-004, REQ-FE-005,
  REQ-FE-006 use stable message strings (the exact strings listed in
  those requirements) so log-monitoring tooling can grep them
  consistently.
- **REQ-FE-009**: No new tracing inside `MessageBubble.tsx` is
  required. The submit-time shape is already deterministic
  (`JSON.stringify(action.payload.answers ?? {})` at line 84). The
  diagnostic value of a debug log there is low compared to the
  rehydration-time warning at the parser, which is where mismatches
  actually surface.

### 3.5 Constraints

- **CON-001**: The `$$a2ui:` marker MUST NOT change.
- **CON-002**: The `parent_message_id` column and the
  `MessageResponse.parent_message_id` field MUST NOT change shape.
- **CON-003**: The `POST /sessions/{id}/hitl/{transitionId}` request
  shape MUST NOT change.
- **CON-004**: The persisted byte sequence of `messages.content` for
  a `HITLSubmit` row MUST remain byte-identical to the
  `HITLResponse.Content` the client sent (predecessor INV-004). This
  spec patches the parser, not the persisted shape.
- **CON-005**: The CPN domain (`cpn/`) MUST NOT import anything from
  `internal/app/` or `internal/driving/`. The new `slog` calls in
  `cpn/hitl.go` MUST use the standard library `log/slog` package only.
- **CON-006**: The composition root (`cmd/server/main.go`) MUST
  remain the only caller of `buildHITLReviewCard` (or its replacement)
  in the legacy fallback branch. Topology factories MUST NOT call
  `cmd/server/main.go` symbols (no inversion).
- **CON-007**: No env-var feature flag for this change. Both fixes
  ship together, on by default. The legacy branch's WARN is the
  rollback signal; if it fires in production, the on-call response
  is to roll back.

### 3.6 Guidelines

- **GUD-001**: Reuse `buildHITLReviewCard`'s component tree verbatim
  in the new builder. Maintain a single source of truth for the card
  layout, button labels, and action types so the live and rehydrated
  paths produce byte-identical surfaces. Prefer either:
  - moving the function from `main.go` to `topologies.go` and having
    `main.go`'s legacy branch call into it, OR
  - extracting the typed component tree into a small helper that both
    sites consume.
- **GUD-002**: For the new `A2UIPayloadBuilder`, return the typed
  component tree (e.g. `map[string]any{"components": [...]}`) — NOT
  the marshaled JSON string. `fireHITL` calls `json.Marshal` on the
  return value (cpn/hitl.go:103). Returning a pre-marshaled string
  would double-encode.
- **GUD-003**: Do not refactor `parseResolvedAnswers` into multiple
  files. Keep it co-located with `QuestionnaireLocked` so the
  contract is reviewable in one place.
- **GUD-004**: When adding the `slog` calls, prefer
  `slog.DebugContext` for high-frequency / decision-point traces and
  `slog.InfoContext` for write-confirmation traces. WARN is reserved
  for the legacy-branch regression signal.
- **GUD-005**: When threading `ctx` into
  `appendHITLResponseToHistory`, do not introduce a deadline or a
  cancellation check inside the function. The function is fire-and-
  forget (a History append); `ctx` is solely for log correlation.

### 3.7 Patterns

- **PAT-001** (inherited from predecessor): **Single-site
  persistence.** The HITL response row is appended at the resolve
  site in `fireHITL` and flows through the existing `persistAfterRun`
  pipeline. This spec extends the pattern to t-review by giving it a
  transition-owned surface so the gate `if a2uiRowID != ""` unlocks.
- **PAT-002** (inherited from predecessor): **Convergent render
  pipelines.** Live submit and history rehydration feed identical
  inputs into the renderer. This spec restores convergence for
  questionnaires (the parser was the asymmetry) and establishes it
  for the t-review card (the surface row was missing on rehydration).
- **PAT-003** (new): **Defensive WARN over branch deletion.** The
  legacy emit path is downgraded to a WARN-and-emit fallback rather
  than deleted, so a misconfigured topology cannot strand the user
  silently. Loud telemetry beats silent correctness regressions.
- **PAT-004** (new): **Permissive parser, strict emitter.** The
  parser accepts BOTH historical answer shapes; the emitter is left
  alone. This is the cheap, backwards-compatible direction (no data
  backfill, no wire change). Future emitters can converge on the
  wrapped shape at their leisure.

### 3.8 Invariants

- **INV-001** (inherited from predecessor): For every successful
  `t-clarify` cycle with `Action == HITLSubmit`, exactly ONE
  `messages` row with `role='user'` and
  `parent_message_id == <surface row id>` MUST exist. (Already
  passing — verified in production.)
- **INV-002** (inherited from predecessor, currently violated, this
  spec restores it): For every successful `t-review` cycle whose
  action is not `reject`, exactly ONE `messages` row with
  `role='user'` and `parent_message_id == <surface row id>` MUST
  exist.
- **INV-003** (new): For every `t-review` cycle, exactly ONE
  `messages` row with `role='assistant'` whose content begins with
  `$$a2ui:` AND whose `cpn_id == c.ID` of the t-review CPN MUST
  exist. (Surface-row analog of INV-001 for t-review.)
- **INV-004** (inherited from predecessor): The byte sequence of
  `messages.content` for a `HITLSubmit` row MUST be the verbatim
  `HITLResponse.Content` string. (Already passing.)
- **INV-005** (new): The legacy emit branch in `cmd/server/main.go`
  MUST NOT be taken for `t-review` events in healthy production. A
  WARN line (REQ-BE-003) appearing in production logs whose
  `transition_id == "t-review"` is a regression signal.

## 4. Interfaces & Data Contracts

### 4.1 Frontend — `parseResolvedAnswers` extended contract

```ts
// front/react-assistant/src/features/chat/a2ui/A2UIMessageRenderer.tsx (~line 594)
function parseResolvedAnswers(resolvedPayload: string): Record<string, string> {
  let parsed: unknown;
  try {
    parsed = JSON.parse(resolvedPayload);
  } catch {
    console.warn(
      'parseResolvedAnswers: JSON parse error',
      resolvedPayload.slice(0, 120),
    );
    return {};
  }
  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    console.warn(
      'parseResolvedAnswers: non-object JSON',
      typeof parsed,
      String(resolvedPayload).slice(0, 120),
    );
    return {};
  }

  const obj = parsed as Record<string, unknown>;

  // Wrapped shape — REQ-FE-001 (legacy / forward-compat).
  if (obj.answers && typeof obj.answers === 'object' && !Array.isArray(obj.answers)) {
    return coerceAll(obj.answers as Record<string, unknown>);
  }

  // Action envelope — REQ-FE-002. Approve/revise/reject MUST NOT be
  // misread as a questionnaire answer map.
  if ('action' in obj || 'content' in obj) {
    return {};
  }

  // Flat shape — REQ-FE-001 (current emitter). Treat the top-level
  // object as the answer map, coercing each value to string.
  const out = coerceAll(obj);
  if (Object.keys(out).length === 0) {
    console.warn(
      'parseResolvedAnswers: unrecognized shape',
      { keys: Object.keys(obj), preview: resolvedPayload.slice(0, 120) },
    );
  }
  return out;
}

function coerceAll(o: Record<string, unknown>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(o)) {
    out[k] = typeof v === 'string' ? v : String(v ?? '');
  }
  return out;
}
```

### 4.2 Backend — `buildReviewA2UIPayload` signature

```go
// back/go-assistant/cmd/server/topologies.go (new helper)
//
// buildReviewA2UIPayload returns an A2UIPayloadBuilder for the t-review
// HITL transition. The returned builder ignores `consumed` (the review
// card content is fixed) and produces the same component tree the
// legacy main.go buildHITLReviewCard emits, so live and rehydrated
// renders converge byte-identically.
//
// The transitionID is captured by closure so each button's `id` prop
// points at the correct transition.
func buildReviewA2UIPayload(transitionID string) func([]cpn.Token) (any, error) {
    return func(_ []cpn.Token) (any, error) {
        return reviewCardPayload(transitionID), nil
    }
}

// reviewCardPayload returns the typed component tree for a HITL review
// card. The shape MUST match what cmd/server/main.go buildHITLReviewCard
// historically produced — single source of truth.
func reviewCardPayload(transitionID string) any {
    return map[string]any{
        "components": []any{
            map[string]any{
                "type":  "card",
                "props": map[string]any{"title": "Review Required"},
                "children": []any{
                    map[string]any{"type": "text", "props": map[string]any{"content": "Please review and confirm."}},
                    map[string]any{"type": "divider"},
                    map[string]any{"type": "text", "props": map[string]any{"content": "Choose an action to continue:", "variant": "secondary"}},
                },
            },
            map[string]any{"type": "button", "props": map[string]any{
                "label": "✓ Approve", "variant": "success",
                "actionType": "hitl:approve", "id": transitionID,
            }},
            map[string]any{"type": "button", "props": map[string]any{
                "label": "✎ Request Changes", "variant": "primary",
                "actionType": "hitl:revise", "id": transitionID,
            }},
            map[string]any{"type": "button", "props": map[string]any{
                "label": "✗ Discard", "variant": "danger",
                "actionType": "hitl:reject", "id": transitionID,
            }},
        },
    }
}
```

`buildHITLReviewCard` in `cmd/server/main.go` MAY be deleted and the
legacy branch MAY call `reviewCardPayload(transitionID)` + `json.Marshal`
directly. Either approach satisfies REQ-BE-001/002/003 as long as a
single source of truth produces the JSON.

### 4.3 Backend — Topology wiring

```go
// back/go-assistant/cmd/server/topologies.go default topology (~line 241)
tReview := cpn.NewTransition("t-review", cpn.NodeKindHITL,
    []string{"p-plan"}, []string{"p-reviewed"})
tReview.HITLConfig = &cpn.HITLConfig{
    Prompt:             "",
    A2UIPayloadBuilder: buildReviewA2UIPayload("t-review"),
}

// Same change in unifiedTopologyFactory (~line 561)
```

### 4.4 Backend — `appendHITLResponseToHistory` signature change

```go
// back/go-assistant/cpn/hitl.go (~line 612)
//
// REQ-OBS-004: ctx is added so the persistence write log line carries
// trace correlation. No deadline / cancellation logic inside.
func appendHITLResponseToHistory(
    ctx context.Context,
    c *CPN,
    resp HITLResponse,
    parentID string,
)
```

Both call sites at `cpn/hitl.go:260` and `cpn/hitl.go:320` MUST pass
the same `ctx` they receive in `fireHITL`.

### 4.5 Backend — Legacy WARN log line

```go
// back/go-assistant/cmd/server/main.go (~line 273, before the
// !transitionOwnsSurface block publishes the legacy card)
slog.WarnContext(ctx, "legacy review-card emit path used (NOT persisted) — t-review topology may be misconfigured",
    "session_id", sessionID,
    "cpn_id", evt.CPNID,
    "transition_id", evt.TransitionID,
    "prompt_len", len(prompt),
)
```

The `ctx` available at this site is the event-subscriber callback's
context (the same context already passed to other broker operations).
If no `ctx` is in scope at this exact site, use the server's root
context.

### 4.6 No DB schema change

The `parent_message_id` column already exists. No migration is added.

### 4.7 No HTTP DTO change

`MessageResponse.parent_message_id` already exists from the
predecessor spec.

## 5. Acceptance Criteria

### Frontend — locked questionnaire

- **AC-FE-001**: Given a session with a persisted A2UI questionnaire
  surface AND a linked `RoleUser` row whose content is the FLAT shape
  `{"q1":"opt-a","q2":"Mi respuesta"}`, When the chat is opened after
  page reload, Then `QuestionnaireLocked` MUST display "opt-a"'s
  resolved option label for q1 and "Mi respuesta" verbatim for q2,
  AND MUST NOT display `—` for either.
- **AC-FE-002**: Given the same session whose linked row instead
  carries the WRAPPED shape `{"answers":{"q1":"opt-a"}}`, When the
  chat is opened, Then `QuestionnaireLocked` MUST display "opt-a"'s
  resolved option label for q1 (forward-compat path).
- **AC-FE-003**: Given a session whose linked row is an action
  envelope `{"action":"approve"}`, When the chat is opened, Then
  `parseResolvedAnswers` MUST return `{}` and the questionnaire MUST
  render in defensive locked mode (every field shows `—`, no console
  error). This guards against an unrelated transition's response being
  rendered in a questionnaire bubble.
- **AC-FE-004**: Given a malformed `messages.content` (e.g.
  `{not-valid`), When the chat is opened, Then exactly one
  `console.warn` MUST fire with message
  `"parseResolvedAnswers: JSON parse error"` AND the renderer MUST
  display defensive locked mode without crashing.
- **AC-FE-005**: Given the production session
  `3a05f35278cd0ae29e8d659185b2a64c` (FLAT shape with q1 free-text =
  "es software yo soy el AI engineer..." and q2 = "opt-c"), When the
  chat is opened, Then q1 MUST show the long free-text verbatim and
  q2 MUST show "App, software o plataforma digital" (opt-c's
  resolved label). No data backfill is required to satisfy this AC.

### Backend — t-review surface persistence

- **AC-BE-001**: Given a session that completes a full
  `t-classify → t-plan → t-review (approve)` cycle, When the cycle
  finishes, Then `messages` MUST contain BOTH:
  - exactly one `role='assistant'` row whose `content` begins with
    `$$a2ui:` and whose `cpn_id` matches the t-review CPN's id, AND
  - exactly one `role='user'` row whose `parent_message_id` matches
    that surface row's id and whose `content` is `{"action":"approve"}`.
- **AC-BE-002**: Same as AC-BE-001 but with a `revise` action and
  content "tighten the budget", the user row's `content` MUST be
  `{"action":"revise","content":"tighten the budget"}`.
- **AC-BE-003**: Same as AC-BE-001 but with a `reject` action, NO
  user row MUST be appended for that cycle. The surface row MUST
  still be persisted (the user did see the card).
- **AC-BE-004**: Given the live UI flow at the end of t-review, the
  legacy WARN log line in `main.go` MUST NOT be emitted. (Verified
  via integration test asserting against captured log output, OR via
  manual smoke matrix grep.)
- **AC-BE-005**: Given a future regression that removes
  `A2UIPayloadBuilder` from t-review's config, When the cycle
  fires, Then the legacy WARN line MUST be emitted at least once
  with the correct `transition_id` field. (Verified via test that
  flips the config and asserts the WARN.)
- **AC-BE-006**: Given the persisted assistant row from AC-BE-001,
  When the chat is rehydrated after page reload, Then the renderer
  MUST display the locked t-review card (Approve / Request Changes /
  Discard buttons all visually disabled, action visible). No new
  bubble for the user's choice — the response is reflected in the
  card lock per the existing live-submit pattern.

### Tracing

- **AC-OBS-001**: For every t-clarify cycle, the backend log stream
  MUST contain exactly one
  `"fireHITL appended A2UI surface to History"` info line whose
  `transition_id == "t-clarify"` AND exactly one
  `"appendHITLResponseToHistory wrote row"` info line whose
  `parent_id` matches the surface's `a2ui_row_id`.
- **AC-OBS-002**: Same as AC-OBS-001 for every t-review cycle whose
  action is not reject.
- **AC-OBS-003**: For a `reject` action, the
  `"appendHITLResponseToHistory wrote row"` info line MUST NOT be
  emitted (REQ-003 of predecessor) but the
  `"fireHITL appended A2UI surface to History"` line MUST be (the
  surface IS persisted).
- **AC-OBS-004**: The frontend, on opening session
  `3a05f35278cd0ae29e8d659185b2a64c` BEFORE this fix, MUST emit a
  `"parseResolvedAnswers: unrecognized shape"` warn (verified by
  reverting REQ-FE-001 in a test and asserting the warn fires).
  After this fix, the same session MUST NOT emit any
  `parseResolvedAnswers` warns.

### Cross-cutting

- **AC-X-001**: All AC-001..AC-405 and INV-001..INV-004 of the three
  predecessor specs continue to hold.
- **AC-X-002**: `make build-server` exits 0 with no new warnings.
  `go vet ./...` exits 0. `go test ./cpn/... ./internal/app/...
  ./store/postgres/...` exits 0.
- **AC-X-003**: `cd front/react-assistant && npm run build` exits 0
  with no new warnings. `npm run test` exits 0.

## 6. Test Automation Strategy

| Level | Backend | Frontend |
|-------|---------|----------|
| Unit | `cpn/hitl_test.go` — extend the existing `appendHITLResponseToHistory` table tests with a new case proving the t-review raw-deposit branch persists `{"action":"approve"}`, `{"action":"revise","content":"…"}`, and skips `reject`. Add a t-review-specific subtest that runs `fireHITL` with a stubbed channel and asserts (a) the surface row is appended to `c.History` (b) the response row follows. | `A2UIMessageRenderer.test.tsx` — new test suite for `parseResolvedAnswers`: (i) wrapped shape, (ii) flat shape, (iii) action envelope returns `{}`, (iv) malformed JSON, (v) non-object root, (vi) reserved keys at top level. Each warning case asserts the `console.warn` mock was called with the stable message string. |
| Unit | `cmd/server/topologies_test.go` — add a test asserting both `defaultTopologyFactory` and `unifiedTopologyFactory` produce a t-review transition whose `HITLConfig.A2UIPayloadBuilder` is non-nil and returns a payload that JSON-marshals to a non-empty string containing `"hitl:approve"`, `"hitl:revise"`, `"hitl:reject"`. | `QuestionnaireComponent.test.tsx` — extend existing locked-mode tests with the production fixture (FLAT shape, q1 free-text + q2 opt id) and assert both render verbatim. |
| Unit | `cmd/server/main_test.go` — add a small test that simulates an `EventHITLRequested` event for a transition NOT owning its surface and asserts the WARN log line is emitted with the correct fields. Use `slog`'s `slogtest` helper or capture via a custom handler. | — |
| Integration | `internal/app/session_service_test.go` — extend the existing `TestSessionService_AskThenHITL_NoRawTAskRowPersisted` (or sibling) with a t-review variant: drive a full plan→review→execute cycle, assert messages table contains the surface + response pair for t-review per INV-002/003. Use the existing Postgres testcontainer harness. | `MessageBubble.test.tsx` — new fixture pair: assistant t-review surface row + user `{"action":"approve"}` row. Assert the rendered bubble has its action buttons visually disabled (existing locked pattern) and no parsing warning fires. |
| Manual | `psql` query (V-006 below) | DevTools warn audit (V-007 below) |

### Frameworks

- Backend: Go standard `testing`, `slog`'s `slogtest` for log capture.
  Postgres integration via the existing testcontainer harness.
- Frontend: Vitest + React Testing Library. `vi.spyOn(console,
  'warn')` for parser warning assertions.

### Test data

- A flat-shape fixture matching production session
  `3a05f35278cd0ae29e8d659185b2a64c`'s row content is REQUIRED to prove
  AC-FE-005.
- A wrapped-shape fixture proves AC-FE-002.
- An action-envelope fixture proves AC-FE-003.
- A reject-action fixture proves AC-BE-003.

### CI/CD integration

All new tests run in the existing `go test ./...` and `npm run test`
pipelines. No new CI jobs.

### Coverage expectations

- `parseResolvedAnswers`: branch coverage on (parse error | non-object
  | wrapped | action-envelope | flat | empty flat).
- `cpn/hitl.go fireHITL` t-review path: line coverage on the surface
  emit + History append branch.
- `appendHITLResponseToHistory`: branch coverage on
  (submit | approve | revise | reject) with reject expected to
  no-op.

### Performance testing

Not applicable. The change adds at most one `Message` row and a
handful of `slog` calls per HITL cycle.

## 7. Rationale & Context

### Why permissive parser instead of fixing the emitter

The flat shape is what production rows already contain. Switching the
emitter to wrapped would either (a) require a data backfill on the
production `messages` table (operationally disruptive for an issue
that affects rendering only), or (b) leave existing rows broken
forever. The parser change is two extra branches, fully backwards-
compatible, and decouples the fix from any wire-format negotiation.

### Why give t-review a transition-owned surface instead of patching the legacy emit path

The legacy path lives in the composition root (`cmd/server/main.go`),
outside the CPN domain. Persisting from there would require either
calling back into `fireHITL`'s history (a layering violation per
CON-005) or introducing a parallel persistence write (a second source
of truth for the messages table — exactly the failure mode the
predecessor spec was designed to avoid).

The transition-owned surface route is structurally clean: the surface
is owned by the transition that needs it, the existing
`fireHITL`-driven persistence pipeline applies for free, and the
legacy emit branch becomes a defensive fallback rather than a primary
path. PAT-001 of the predecessor spec is honored end-to-end.

### Why keep the legacy branch instead of deleting it

Two reasons:
1. **Defensive**: a future HITL transition that ships without
   `A2UIPayloadBuilder` (oversight, in-progress migration) MUST not
   strand the user. The legacy branch keeps the card visible.
2. **Detectability**: a deleted branch produces silence on
   misconfiguration. A WARN-and-emit branch produces telemetry. PAT-003.

### Why the empty Prompt on t-review stays

The Prompt was deliberately set empty (cmd/server/topologies.go:558-560
comment) because the A2UI card already self-labels with "Review
Required". Keeping it empty in the new builder maintains visual
parity. The surface payload's "text" component is what carries any
human-facing prompt text, and the existing default
`"Please review and confirm."` is fine; the unified topology may
override it later if needed.

### Why threading ctx into appendHITLResponseToHistory is worth the signature change

REQ-OBS-004 needs `ctx` for trace correlation. Without it, the write-
confirmation log loses the request scope, and grepping a single t-review
flow in production becomes hard. The signature change is small (two
call sites, both inside the same file), and `ctx` propagation is the
standard Go idiom for log correlation. The function does NOT honor
`ctx.Done()`; it uses `ctx` purely as a logging vehicle. GUD-005.

### Why `console.warn` instead of throwing or returning an error type

The parser is on the rendering critical path. Throwing would crash
the locked questionnaire bubble and likely the entire chat list
render. Returning an error type would require touching every caller
and the existing `useMemo` invariant. `console.warn` is observable
in DevTools, monitorable via Sentry/LogRocket if the team adds them
later, and zero-cost in headless contexts.

### Why no end-user toast on parser failure

The parser failure mode is a developer-visible regression, not a
user-actionable error. Showing a toast would frighten users without
giving them anything to do. The defensive locked mode (every field
`—`) is itself a visible-but-quiet signal that something went wrong,
combined with the developer-facing warn.

## 8. Dependencies & External Integrations

### Internal dependencies

- **INT-001**: `back/go-assistant/cpn/hitl.go` — `appendHITLResponseToHistory`
  signature change + slog calls (REQ-OBS-001..004).
- **INT-002**: `back/go-assistant/cmd/server/topologies.go` — new
  `buildReviewA2UIPayload` + wiring on both t-review instances
  (REQ-BE-001..002).
- **INT-003**: `back/go-assistant/cmd/server/main.go` — WARN log on
  legacy branch + optional removal of `buildHITLReviewCard` if moved
  (REQ-BE-003..004).
- **INT-004**: `back/go-assistant/internal/app/session_service.go` —
  history-sync delta log (REQ-OBS-005). No behavioral change.
- **INT-005**: `front/react-assistant/src/features/chat/a2ui/A2UIMessageRenderer.tsx`
  — extended `parseResolvedAnswers` (REQ-FE-001..007).

### External systems

- **EXT-001**: Postgres `messages` table — read/written via existing
  `MessageRecord` pipeline. No schema change.

### Third-party services

- **SVC-001**: None.

### Infrastructure dependencies

- **INF-001**: None.

### Data dependencies

- **DAT-001**: Pre-existing rows in `messages` for resolved
  questionnaires use the FLAT shape. The parser fix MUST display them
  correctly without backfill.

### Technology platform dependencies

- **PLT-001**: Go 1.22+ (existing).
- **PLT-002**: React 19 + TypeScript (existing).
- **PLT-003**: Postgres 16 (existing).

### Compliance dependencies

- **COM-001**: None. The persisted user response is content the
  user already typed and submitted; no new PII surface.

## 9. Examples & Edge Cases

### 9.1 Production session — flat shape with mixed free-text

```
DB row content (verbatim, byte-for-byte):
  {"q1":"es software yo soy el AI engineer de liwaisi y quiero vender a `brae` o sea este mismo software que estoy usando","q2":"opt-c"}

Parser output (after fix):
  { q1: "es software yo soy el AI engineer de liwaisi y quiero vender a `brae` o sea este mismo software que estoy usando",
    q2: "opt-c" }

Rendered locked questionnaire:
  q1 → "es software..." (verbatim, free-text fall-through via resolveLabel)
  q2 → "App, software o plataforma digital" (opt-c's resolved option label)

Rendered console:
  no warns (recognized shape)
```

### 9.2 Approve action

```
HITLResponse received:
  Action  = HITLApprove
  Content = ""

After REQ-BE-001/002, fireHITL surface emit:
  c.History gains assistant row id "<sid>-a2ui-<ns>", content
    "$$a2ui:{"components":[{"type":"card",...},
    {"type":"button","props":{"label":"✓ Approve",...}},...]}"

After REQ-006:
  c.History gains user row content `{"action":"approve"}` with
    parent_message_id = surface row id.

After persist:
  messages: [..., {role=assistant, $$a2ui:...},
                  {role=user, {"action":"approve"},
                   parent_message_id=<surface>}]

Rendered after rehydration:
  Locked t-review card. Buttons disabled. The persisted user row's
  shape `{"action":"approve"}` is intercepted by REQ-FE-002 (action
  envelope returns {}) so the card is NOT misread as a questionnaire
  with answers. The card's buttons are locked because hitlResolved is
  set on the assistant message (existing live-submit lock pattern).
```

### 9.3 Revise action with content

```
HITLResponse received:
  Action  = HITLRevise
  Content = "tighten the budget section to under $5k"

Persisted user row content:
  `{"action":"revise","content":"tighten the budget section to under $5k"}`

Parser when this row is paired with a questionnaire bubble (DEFENSIVE):
  REQ-FE-002 detects the `action` reserved key → returns {}.
  Questionnaire renders defensive locked mode (every field —).

This case is impossible in practice because t-review's parent linkage
points at a review-card bubble, not a questionnaire bubble. The
defensive return is to prevent cross-bubble misreads if a future bug
crosses the linkage.
```

### 9.4 Reject action

```
HITLResponse received:
  Action = HITLReject

fireHITL: returns ErrHITLRejected. NO RoleUser row appended.
Surface row IS appended (the user did see the card).

After rehydration:
  Locked t-review card. Existing rejection badge displayed via
  MessageBubble's hitlResolved='reject' branch (existing).
```

### 9.5 Misconfigured deploy (regression detection)

```
Scenario: a future engineer accidentally removes A2UIPayloadBuilder
from t-review's config in topologies.go.

Live behavior:
  - fireHITL takes the !customSurface branch.
  - !customSurface → main.go legacy branch fires.
  - WARN line: "legacy review-card emit path used (NOT persisted) —
    t-review topology may be misconfigured"
    session_id=... cpn_id=... transition_id=t-review prompt_len=0
  - Card IS published to SSE so the user is not stranded.
  - Card NOT persisted, response NOT persisted.

Detection: production log alert fires on the WARN message string.
On-call rolls back the offending change.
```

### 9.6 Wrapped-shape forward-compat

```
DB row content (hypothetical future emitter):
  {"answers":{"q1":"opt-a","q2":"opt-b"}}

Parser output:
  { q1: "opt-a", q2: "opt-b" }

Rendered locked questionnaire:
  q1 → resolved label of opt-a
  q2 → resolved label of opt-b

Rendered console: no warns.
```

### 9.7 Malformed persisted row

```
DB row content:
  {not-valid-json

Parser output:
  {}
console.warn: "parseResolvedAnswers: JSON parse error" "{not-valid-jso"

Rendered locked questionnaire:
  Every field "—". Defensive lock.
```

## 10. Validation Criteria

- **V-001**: `cd back/go-assistant && go test ./cpn/... ./internal/app/...
  ./store/postgres/... ./cmd/server/...` exits 0, including new
  t-review persistence tests and new WARN-log assertion.
- **V-002**: `cd back/go-assistant && go vet ./...` exits 0.
- **V-003**: `cd back/go-assistant && go build ./...` exits 0.
- **V-004**: `cd front/react-assistant && npm run test` exits 0,
  including new `parseResolvedAnswers` test suite.
- **V-005**: `cd front/react-assistant && npm run build` exits 0.
- **V-006**: Manual psql verification on a freshly resolved t-review
  cycle in a test session:
  ```sql
  SELECT id, role, cpn_id, parent_message_id,
         left(content, 80) AS preview
  FROM messages
  WHERE session_id = '<test-session>'
    AND cpn_id = '<test-cpn>'
  ORDER BY timestamp;
  ```
  MUST return BOTH an `assistant` row whose preview begins with
  `$$a2ui:{"components":[{"type":"card"...` AND a `user` row whose
  `parent_message_id` matches the assistant row's id and whose preview
  begins with `{"action":"`.
- **V-007**: Manual DevTools audit on the production session
  `3a05f35278cd0ae29e8d659185b2a64c`: open Network → reload → open
  Console. Console MUST be free of any `parseResolvedAnswers:` warns.
  The chat MUST show the user's q1 free-text and q2 option label
  inline.
- **V-008**: Production log audit (post-deploy): grep
  `"legacy review-card emit path used"` over a 24-hour window MUST
  return zero matches.
- **V-009**: All AC-FE-001..005 and AC-BE-001..006 pass via the
  test suite; AC-OBS-001..004 pass via captured log/console
  inspection.

## 11. Related Specifications / Further Reading

- [spec-process-bugfix-a2ui-hitl-response-persistence.md](spec-process-bugfix-a2ui-hitl-response-persistence.md)
  — predecessor; defines REQ-001..205, INV-001..004, the locked-mode
  contract. This spec extends REQ-202 / PAT-001 / INV-002 to t-review
  and patches the parser asymmetry on the frontend.
- [spec-process-bugfix-a2ui-hitl-rehydration.md](spec-process-bugfix-a2ui-hitl-rehydration.md)
  — parent; introduced the A2UI surface persistence on which the
  parent-linkage relies.
- [spec-process-bugfix-a2ui-rehydration-completion.md](spec-process-bugfix-a2ui-rehydration-completion.md)
  — sibling; established the convergent rendering pattern (PAT-103)
  this spec extends to t-review.
- [spec-architecture-a2a-a2ui-protocol-integration.md](spec-architecture-a2a-a2ui-protocol-integration.md)
  — parent protocol; §12 defines `card`, `button`, `questionnaire`,
  `choice` and the HITL resolve contract.
- [spec-architecture-model-selection-centralization.md](spec-architecture-model-selection-centralization.md)
  — sibling; established REQ-PAR-003 (the t-clarify error-recovery
  banner) which this spec leaves intact.
- **Follow-up (deferred)**: in-progress draft autosave for unresolved
  questionnaires; SSE replay for HITL surfaces (Last-Event-ID).
