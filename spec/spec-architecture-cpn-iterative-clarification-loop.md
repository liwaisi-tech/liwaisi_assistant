---
title: CPN Iterative Clarification Loop — Bounded Re-firing of `t-clarify` via `t-reassess`
version: 1.0
date_created: 2026-04-15
last_updated: 2026-04-15
owner: liwaisi
tags: [architecture, cpn, clarification, a2ui, llm, go-assistant, react-assistant, hitl]
---

# Introduction

The unified CPN topology in `go-assistant` currently runs a single-pass clarification: `t-classify → t-ask → t-clarify → t-plan-clarified`. Once the user answers the first questionnaire, the token flows one-way into planning. Real conversations — for example, "ayúdame a planear un *fest* para emprendedores en Bogotá" — routinely need a *second, deeper* round of clarification before a plan is actionable (fest vs launch vs demo-day vs meetup, audience size, success metric, budget band). Today the agent produces a plan prematurely instead of asking.

This specification defines an **iterative, bounded, provably-terminating clarification loop**. A new LLM transition `t-reassess` scores residual ambiguity after each clarify round and either (a) re-fires `t-clarify` with a *deeper* questionnaire, (b) proceeds to plan with explicit stated assumptions, or (c) emits an escape-hatch binary choice to the user. A 1-bounded counter place `p-round` guarantees k-termination.

## 1. Purpose & Scope

**Purpose.** Replace the single-pass clarification with a bounded iterative loop so the agent can reduce residual ambiguity across up to `k = 3` rounds before committing to a plan, while remaining structurally sound (1-bounded) and terminating.

**In scope.**
- New CPN places `p-round` and `p-reassessed` in `back/go-assistant/cmd/server/topologies.go`.
- New LLM transition `t-reassess`; rewire of `t-plan-clarified`; modification of `t-ask` and `buildClarifiedToken`; new `buildPlannerPreamble`.
- New guards `guardResidualAmbiguous` and `guardResidualResolved`.
- Env-configurable thresholds: `CLARIFY_MAX_ROUNDS` (default 3), `REASSESS_AMBIGUITY_THRESHOLD_CLARIFY` (default 0.40), `REASSESS_AMBIGUITY_THRESHOLD_PROCEED` (default 0.20), `REASSESS_CONVERGENCE_DELTA_STOP` (default 0.10).
- Initial marking update: seed `p-round` with `{N: 0}` on unified-topology instantiation.
- Executor guarantee that place markings (incl. `p-round`) survive HITL pauses via snapshot.
- Frontend: `A2UIMessageRenderer` must render a "Follow-up n / k" badge on re-fired clarify surfaces; `useChat` must already tolerate N clarify surfaces per turn (verify, don't redesign).
- Escape-hatch binary HITL surface (Spanish-LATAM microcopy) when `t-reassess` detects frustration or contradiction.
- Unit + integration + regression tests covering the fest scenario, contradiction, escape hatch, and hard cap.

**Out of scope.**
- Redesigning the classifier (`t-classify`) or its schema.
- Changing `t-ask`'s questionnaire JSON schema (re-used verbatim for re-fired rounds).
- Changing the A2UI transport layer (`$$a2ui:` marker, SSE event shape).
- Changing `t-review` or any post-plan flow.
- Persisted chat-history schema migrations.
- UI redesign beyond the round badge, locked-state continuity, and the escape-hatch surface.

**Audience.** Implementation agents: `golang-pro` (backend CPN + LLM transitions), `vercel-react-best-practices` + `frontend-design` (frontend badge + escape-hatch surface), plus the UX designer (Spanish-LATAM copy) and reviewers.

## 2. Definitions

- **CPN**: Coloured Petri Net — the execution model used by go-assistant.
- **Place**: A typed token container in the net. Each place has a color set and a space (`SpaceSurface` or `SpaceComputation`).
- **Transition**: A named step that consumes tokens from input places, runs a computation (LLM, HITL, code), and deposits tokens on output places.
- **Guard**: A predicate on consumed tokens that gates firing.
- **1-bounded (safe)**: Every place holds at most one token under any reachable marking.
- **k-terminating**: Any firing sequence reaches a terminal state in at most a function of `k` steps.
- **HITL**: Human-In-The-Loop — a transition that suspends pending user input.
- **A2UI**: Agent-to-UI protocol — declarative component tree emitted after the `$$a2ui:` marker on an SSE chunk.
- **Clarify round**: One full cycle of `t-ask → t-clarify → t-reassess` (round counter increments on re-fire).
- **Residual ambiguity**: LLM-estimated probability in `[0, 1]` that the current token set is insufficient to plan.
- **Convergence delta**: LLM-estimated reduction in residual ambiguity between the previous round and the current one, in `[-1, +1]`. Negative means the round increased ambiguity (contradiction or topic drift).
- **Ambiguity axes** (LATAM entrepreneurship domain): `event_type`, `stage`, `audience`, `budget_band`, `geography`, `timeline`, `success_metric`.
- **Escape hatch**: The `request_user_choice` binary surface offered when frustration or contradiction is detected.
- **Topic shift**: A user answer that introduces a substantially new subject, detected by `t-reassess` via an explicit flag.

## 3. Requirements, Constraints & Guidelines

### Backend — CPN structure

- **REQ-001**: A new place `p-round` MUST exist with `ColorJSON` color and `SpaceSurface` space. Its token payload schema is `{ "n": integer, "reset": boolean }`. It MUST be seeded with `{n: 0, reset: false}` in the initial marking of the unified topology.
- **REQ-002**: A new place `p-reassessed` MUST exist with `ColorJSON` color and `SpaceSurface` space. Its token payload is the full `t-reassess` output (see §4).
- **REQ-003**: `p-round` MUST remain 1-bounded under every reachable marking. Every transition that reads it MUST also deposit exactly one token back to it (test-and-set pattern).
- **REQ-004**: A new transition `t-reassess` MUST exist with `NodeKind = NodeKindLLM`. Input places: `p-clarified`, `p-round`. Output places: `p-reassessed`, `p-round`. It MUST NOT have a guard (the net is structurally enabled iff both input places are marked).
- **REQ-005**: `t-plan-clarified` MUST be rewired to consume from `p-reassessed` (not `p-clarified`). Its guard becomes `guardResidualResolved`. Its OutputBuilder MUST be `buildPlannerPreamble` (new), which prepends a routing-aware preamble to the planner token.
- **REQ-006**: A new arc from `p-reassessed` via a new transition `t-followup` (`NodeKindTool`, deterministic — no LLM call) MUST exist when the loop re-fires. `t-followup` consumes `p-reassessed, p-round`, deposits to `p-questions, p-round` with `n` incremented by one. Its guard is `guardResidualAmbiguous`. Modeled as `NodeKindTool` because it is pure routing + counter arithmetic and requires no model invocation.
- **REQ-007**: `t-ask` input/output arcs MUST be extended to read from and re-emit `p-round` unchanged on the first round (counter passes through; increment happens only in `t-followup`).
- **REQ-008**: The counter token MUST be consumed (not re-emitted) by any planning exit: `t-plan-clarified`, `t-plan-direct`, `t-direct`. This guarantees the counter dies in terminal branches.

### Backend — Guards

- **REQ-010**: `guardResidualAmbiguous(tokens)` returns `true` iff ALL of:
  - Parsed `t-reassess` result is well-formed.
  - `residual_ambiguity >= REASSESS_AMBIGUITY_THRESHOLD_CLARIFY` (default 0.40) AND `len(missing_dimensions) > 0`.
  - `p-round` token satisfies `n + 1 < CLARIFY_MAX_ROUNDS` (default 3). I.e., there is budget for another round.
  - `decision != "proceed_to_plan"` (LLM did not explicitly request proceeding).
  - `frustration_signal != true`.
  - NOT (`contradiction_detected == true AND n >= 1`) (a second contradiction terminates the loop).
- **REQ-011**: `guardResidualResolved(tokens)` returns `true` iff `guardResidualAmbiguous(tokens)` returns `false` AND the `t-reassess` output is parseable OR the parse failed irrecoverably (fail-open to planner with synthetic assumption).
- **REQ-012**: The pair (`guardResidualAmbiguous`, `guardResidualResolved`) MUST partition the input space. Exactly one of `t-followup` and `t-plan-clarified` MUST be enabled for any marking with `p-reassessed` and `p-round` both marked.
- **REQ-013**: If the parsed `t-reassess` output carries `decision == "request_user_choice"`, the guard MUST route to a new `t-followup-choice` transition (or re-use `t-followup` with a branch) that emits a **binary** questionnaire via the existing `p-questions → t-clarify` path. This binary surface MUST NOT consume a round budget (counter increment is suppressed on binary-choice follow-ups).

### Backend — LLM contract (`t-reassess`)

- **REQ-020**: `t-reassess` MUST use LLM config: `Role: "structured"`, `Temperature: 0.2`, `MaxTokens: 1024`, `RequireJSON: true`, `ResponseFmtRequired: true`, `StreamOutput: false`, `SkipHistory: false`, `SkipOutputHistory: true`, `SkipRegionalPreamble: true`.
- **REQ-021**: `t-reassess` MUST receive, as a system-level context block, a JSON object containing: `round` (1-indexed), `max_rounds`, `original_user_message`, `classifier` (the full `t-classify` output), and `round_history` (an array of completed `{restated_goal, assumptions, questions, answers, answers_resolved}` per prior round).
- **REQ-022**: `t-reassess` MUST output a JSON object matching the schema in §4 exactly. Unused conditional keys MUST be omitted (not `null`).
- **REQ-023**: The system prompt MUST enforce "drill deeper, never wider": next-round questions MUST target `missing_dimensions` and MUST NOT reuse any question `id` from `round_history`, nor re-ask anything the user already stated.
- **REQ-024**: The system prompt MUST encode the LATAM entrepreneurship domain vocabulary (the seven ambiguity axes) so the LLM recognizes genuine under-specification.
- **REQ-025**: The system prompt MUST enforce round-aware bias: at `round == max_rounds - 1`, the LLM MUST bias toward `proceed_to_plan` unless a plan-invalidating axis is still missing.
- **REQ-026**: The system prompt MUST detect frustration signals in any language (case-insensitive substrings like "whatever", "lo que sea", "just pick", "tú decide", "no sé", "I don't know", "da igual", "you choose", "any of them", "doesn't matter") and set `frustration_signal = true` with `decision = "request_user_choice"`.
- **REQ-027**: The system prompt MUST detect contradictions between the latest answer and prior answers or the original message; set `contradiction_detected = true` and a negative `convergence_delta`.
- **REQ-028**: The `quote_from_user` grounding rule inherited from `t-ask` MUST apply to every question in `next_questions.questions`. Valid quote sources on round 2+ are: the original user message OR any user turn in `c.History`. If the LLM cannot ground a follow-up question, it MUST drop it and lower `residual_ambiguity` accordingly.

### Backend — Clarified token & planner preamble

- **REQ-030**: `buildClarifiedToken` MUST NO LONGER emit the directive "Produce the plan now — do not ask any further questions." Its responsibility is to render merged answers only.
- **REQ-031**: A new function `buildPlannerPreamble(reassess ReassessResult, round RoundToken)` MUST prepend a routing-aware preamble to the planner token on every `t-plan-clarified` firing. The preamble text depends on whether the loop exited via natural convergence, via hard cap, or via escape hatch (see §4).
- **REQ-032**: When `t-reassess` emits `stated_assumptions`, the preamble MUST include them verbatim, translated into the user's language, and instruct the planner to list them under an `Assumptions:` header in the plan.

### Backend — Topic-shift exception

- **REQ-040**: `t-reassess` output MAY include an optional field `topic_shift: boolean`. When `true`, `t-followup` MUST emit a `p-round` token with `n: 0, reset: true`, giving the new topic a fresh budget.
- **REQ-041**: The `reset: true` flag is observability-only; it MUST be logged to the execution history for telemetry. It does not change guard logic beyond setting `n = 0`.

### Backend — Persistence & executor

- **REQ-050** (SCOPE-DEFERRED v1): The executor MUST persist place markings across process restarts. **Scope decision**: the current executor does not snapshot markings (verified: `back/go-assistant/cpn/persist/types.go` holds Messages only). Implementing cross-restart persistence requires a schema change which CON-006 forbids in this feature. For v1, place markings survive *within* a single process lifetime (covers HITL pauses, which block goroutines in-memory). Cross-restart persistence is tracked as a separate follow-up issue. Mid-loop sessions restarted mid-process-crash resume at `n = 0` — acceptable degradation matching current behavior for all place markings in the system.
- **REQ-051**: When an existing session that pre-dates this feature is rehydrated, the absence of a `p-round` token MUST be treated as `n = 0` (default initial marking) and the session MUST continue without error.

### Frontend — Rendering

- **REQ-100**: `A2UIMessageRenderer.tsx` MUST render a "Follow-up n / k" badge on the header of any questionnaire surface whose payload includes a new top-level field `round: {n, max}`. For `n == 0` or missing `round`, the badge MUST NOT render (backward compatibility).
- **REQ-101**: Earlier locked questionnaires from prior rounds in the same chat turn MUST remain visible, read-only, with their answers shown (existing `QuestionnaireLocked` behavior). A new clarify surface in the same turn MUST appear as a new bubble (existing `useChat.ts` marker-boundary guard already ensures this — verify, do not redesign).
- **REQ-102**: The escape-hatch binary surface MUST render as a compact card with two large buttons. The card MUST NOT use the full questionnaire component; it is a distinct minimal surface with `type: "card"` + two `type: "button"` children.
- **REQ-103**: The binary choice surface MUST send the chosen option via the existing HITL resolve API with a flat `{answer: "drill" | "proceed"}` payload shape, consistent with recent HITL persistence work (see `spec-process-bugfix-treview-surface-and-locked-parser.md`).

### Frontend — State & history

- **REQ-110**: The frontend MUST support N clarify surfaces within a single assistant turn without state leaks. The existing 1:1 `hitlTransitionId → response` map is sufficient because each re-fired `t-clarify` creates a new deterministic surface ID.
- **REQ-111**: When the session is reloaded from history, every clarify surface in a multi-round turn MUST rehydrate with its locked state preserved, in order.

### Frontend — UX copy (Spanish-LATAM, neutral)

- **REQ-120**: Round-badge copy: `"Seguimiento {n} de {k}"` (Spanish) and `"Follow-up {n} of {k}"` (English), selected by the existing i18n mechanism. Copy MUST be neutral and non-shaming. Rationale: "Ronda" reads combative in LATAM register; "Seguimiento" matches the app's existing consulting-style voice.
- **REQ-121**: Escape-hatch card title: `"Quiero asegurarme de no atascarte"` (Spanish) / `"I want to make sure I don't get you stuck"` (English).
- **REQ-122**: Escape-hatch prompt: `"¿Afinamos una pregunta más o sigo con supuestos razonables?"` (Spanish) / `"Should we tighten one more question, or should I continue with reasonable assumptions?"` (English).
- **REQ-123**: Escape-hatch button labels: `"Una pregunta más"` / `"Sigue con supuestos"` (Spanish); `"One more question"` / `"Continue with assumptions"` (English). Rationale: button labels describe the concrete cost to the user (a question) rather than a meta-concept (a round).
- **REQ-124**: Contradiction-detected escape card title: `"Hay dos caminos posibles"` / `"I see two possible paths"`. The body MUST show both contradictory values as equal-weight selectable options; the `recommended` flag from the LLM MUST be ignored in this surface to avoid biasing the user. Rationale: non-shaming path-framing instead of surveillance-framing ("detecté"), consistent with the app tagline "La IA propone, tú decides".
- **REQ-125**: Escape-hatch body helper text (contradiction variant): `"Escoge cuál refleja mejor lo que buscas:"` / `"Pick the one that best fits what you want:"`.
- **REQ-126**: After the user selects `proceed` on the frustration escape-hatch (or picks a side on the contradiction escape), a brief assistant text bubble MUST appear before the plan streams, with copy: `"Listo. Sigo con estos supuestos y te muestro el plan."` / `"Got it. I'll continue with these assumptions and show you the plan."`. This bridges the handoff visually; it is a plain text bubble, not an A2UI surface.
- **REQ-127**: The round badge MUST NOT have a tooltip or any hover-activated explanation. It is purely informational. Rationale: tooltips invite explanation that would amplify anxiety mid-loop; quieter is better.

### Constraints

- **CON-001**: `CLARIFY_MAX_ROUNDS` MUST be in `[1, 5]`. Values outside the range MUST fall back to the default (3) with a logged warning.
- **CON-002**: The loop MUST be provably terminating. A Lyapunov function `V(M) = k - n` strictly decreases on every `t-followup` firing; `t-followup` is disabled when `n + 1 >= k`.
- **CON-003**: `t-reassess` adds exactly one structured-JSON LLM call per clarify round. Cost and latency MUST be acceptable within the existing HITL pause budget; use the cheapest available model role (`"structured"`).
- **CON-004**: The net MUST remain 1-bounded. No transition may deposit to a place that is already marked.
- **CON-005**: NO changes to the A2UI `$$a2ui:` marker, SSE `stream_chunk` shape, or `StreamChunkData` TypeScript type are permitted. The round badge is a NEW optional field `round` inside the existing A2UI component payload, not a new top-level message field.
- **CON-006**: NO persisted-history schema migration. The counter token and reassess result are transient execution state, not history rows.

### Guidelines

- **GUD-001**: Prefer explicit `decision` routing from the LLM over inferring from numeric thresholds alone. Thresholds are the safety net; `decision` is the primary signal.
- **GUD-002**: Never ask the user to re-state something they've already said. This is the single most important UX rule — violating it will be treated as a correctness bug.
- **GUD-003**: When in doubt at `round == max_rounds - 1`, PROCEED with stated assumptions. Users prefer a plan with explicit assumptions over a third questionnaire.
- **GUD-004**: Log `residual_ambiguity`, `convergence_delta`, `decision`, and `round` to execution telemetry for every `t-reassess` firing. These are the signals needed to tune thresholds.

### Patterns

- **PAT-001**: Counter token as a dedicated place (not a guard-local variable). Guards in CPN must be pure functions of consumed tokens; counters in places preserve formal soundness and survive pauses naturally.
- **PAT-002**: Binary choice as a mini questionnaire via `t-clarify`, re-using the existing HITL resolve path. Avoids inventing a new surface type.
- **PAT-003**: OutputBuilder pattern for planner-bound preambles. Keep `t-reassess` prompt-only; let the preamble function compose the final planner token deterministically from structured output.

## 4. Interfaces & Data Contracts

### 4.1 `p-round` token schema

```json
{
  "n": 0,
  "reset": false
}
```

- `n`: integer in `[0, CLARIFY_MAX_ROUNDS)`. Rounds completed so far.
- `reset`: boolean. `true` if the most recent `t-followup` was a topic-shift reset (telemetry only).

### 4.2 `p-reassessed` token schema (= `t-reassess` output)

```json
{
  "residual_ambiguity": 0.45,
  "convergence_delta": 0.35,
  "missing_dimensions": ["audience_size", "success_metric"],
  "resolved_dimensions": ["event_type", "budget_band"],
  "contradiction_detected": false,
  "frustration_signal": false,
  "topic_shift": false,
  "decision": "clarify_again",
  "rationale": "One sentence; telemetry only; never user-facing.",

  "next_questions": {
    "restated_goal": "string",
    "assumptions": ["string", "..."],
    "questions": [
      {
        "id": "q3",
        "prompt": "string",
        "quote_from_user": "literal substring of a prior user turn",
        "why_it_matters": "string",
        "recommended": "opt-b",
        "options": [
          {"id": "opt-a", "label": "string"},
          {"id": "opt-b", "label": "string"}
        ]
      }
    ]
  },

  "stated_assumptions": ["string", "..."],

  "user_choice": {
    "prompt": "string",
    "options": [
      {"id": "drill",   "label": "string"},
      {"id": "proceed", "label": "string"}
    ]
  }
}
```

Conditional-key rules:

| `decision`              | Required conditional keys                | Forbidden conditional keys                 |
| ----------------------- | ---------------------------------------- | ------------------------------------------ |
| `clarify_again`         | `next_questions`                         | `stated_assumptions`, `user_choice`        |
| `proceed_to_plan`       | `stated_assumptions`                     | `next_questions`, `user_choice`            |
| `request_user_choice`   | `user_choice`                            | `next_questions`, `stated_assumptions`     |

### 4.3 A2UI payload extension (frontend contract)

The existing `buildClarifyA2UIPayload` MUST add an optional top-level field `round` to the emitted A2UI envelope:

```json
{
  "round": {"n": 2, "max": 3},
  "components": [ /* existing questionnaire tree */ ]
}
```

Backward compatibility: surfaces without `round` MUST render without the badge (equivalent to first-round behavior).

### 4.4 Escape-hatch A2UI payload

Frustration variant:

```json
{
  "round": {"n": 2, "max": 3},
  "components": [
    {
      "type": "card",
      "props": {"title": "Quiero asegurarme de no atascarte", "variant": "escape-frustration"},
      "children": [
        {"type": "text", "props": {"content": "¿Afinamos una pregunta más o sigo con supuestos razonables?"}},
        {"type": "button", "props": {"label": "Una pregunta más", "action": "hitl:submit", "payload": {"answer": "drill"}, "size": "lg", "variant": "secondary"}},
        {"type": "button", "props": {"label": "Sigue con supuestos", "action": "hitl:submit", "payload": {"answer": "proceed"}, "size": "lg", "variant": "primary"}}
      ]
    }
  ]
}
```

Contradiction variant (both buttons equal-weight secondary; labels are the two contradictory values from the LLM):

```json
{
  "round": {"n": 2, "max": 3},
  "components": [
    {
      "type": "card",
      "props": {"title": "Hay dos caminos posibles", "variant": "escape-contradiction"},
      "children": [
        {"type": "text", "props": {"content": "Escoge cuál refleja mejor lo que buscas:"}},
        {"type": "button", "props": {"label": "<option_a_label>", "action": "hitl:submit", "payload": {"answer": "<option_a_id>"}, "size": "lg", "variant": "secondary"}},
        {"type": "button", "props": {"label": "<option_b_label>", "action": "hitl:submit", "payload": {"answer": "<option_b_id>"}, "size": "lg", "variant": "secondary"}}
      ]
    }
  ]
}
```

### 4.5 `buildPlannerPreamble` output contract

Three preamble variants, selected by `(decision, round_exit_reason)`:

**Variant A — natural convergence (`decision == "proceed_to_plan"`):**
```
You have everything you need. Produce the plan now — do not ask any further questions and do not emit another clarification surface. The clarification loop is closed.

The following assumptions were derived from a residual-ambiguity scan and MUST appear verbatim under the "Assumptions:" header of the plan (translated into the user's language):
- <stated_assumptions[0]>
- <stated_assumptions[1]>
...
```

**Variant B — hard cap (`n >= max_rounds - 1` and guard force-overrode):**
```
The clarification budget has been exhausted ({k} rounds). Produce the plan now with best-available information. Do not ask further questions. State assumptions for any remaining gaps under "Assumptions:" and proceed.
```

**Variant C — escape hatch resolved as "proceed":**
```
The user explicitly chose to proceed with assumptions instead of answering more questions. Produce the plan now. State clearly under "Assumptions:" each non-trivial inference you had to make.
```

### 4.6 Env config

| Variable                              | Default | Range       | Purpose                                       |
| ------------------------------------- | ------- | ----------- | --------------------------------------------- |
| `CLARIFY_MAX_ROUNDS`                  | `3`     | `[1, 5]`    | Hard cap on clarify loops.                    |
| `REASSESS_AMBIGUITY_THRESHOLD_CLARIFY`| `0.40`  | `[0.0, 1.0]`| Lower bound for another clarify round.        |
| `REASSESS_AMBIGUITY_THRESHOLD_PROCEED`| `0.20`  | `[0.0, 1.0]`| Upper bound for hard proceed to planner.      |
| `REASSESS_CONVERGENCE_DELTA_STOP`     | `0.10`  | `[0.0, 1.0]`| Minimum progress; below triggers proceed.     |

## 5. Acceptance Criteria

- **AC-001**: Given a user prompt "ayúdame a planear un fest para emprendedores en Bogotá" and a realistic `t-classify` output with `confidence: 0.58`, when the unified topology executes, then `t-ask` fires the first questionnaire, the user submits answers, `t-reassess` fires with `residual_ambiguity >= 0.40`, and `t-followup` re-fires `t-clarify` with a *deeper* questionnaire whose question `id`s are all new.
- **AC-002**: Given a completed second clarify round whose answers reduce `residual_ambiguity` below `0.20`, when `t-reassess` fires, then `t-plan-clarified` consumes from `p-reassessed` with `decision == "proceed_to_plan"` and the planner preamble includes the `stated_assumptions` verbatim.
- **AC-003**: Given the counter `n` reaches `CLARIFY_MAX_ROUNDS - 1`, when `t-reassess` fires with `decision == "clarify_again"`, then the guard force-routes to `t-plan-clarified` with Variant B preamble and `n + 1` is never deposited back.
- **AC-004**: Given the user's second answer contains "no sé, tú decide", when `t-reassess` fires, then `frustration_signal == true`, `decision == "request_user_choice"`, and the binary escape-hatch surface renders with the Spanish-LATAM copy from REQ-121/122/123.
- **AC-005**: Given the user's round-2 answer contradicts a round-1 answer, when `t-reassess` fires, then `contradiction_detected == true`, `convergence_delta < 0`, and the next surface is the contradiction escape-hatch (REQ-124), not another questionnaire.
- **AC-006**: Given a session with one completed clarify round is reloaded from history, when the frontend rehydrates, then the locked round-1 questionnaire renders with answers, the `round` badge shows "Ronda 1 de 3", and (if the session was mid-round-2) the round-2 surface renders as interactive.
- **AC-007**: Given `t-reassess` returns malformed JSON twice in a row, when the guard evaluates, then the token is routed to `t-plan-clarified` with a synthetic assumption "Proceeding with best-effort interpretation due to upstream parse error" prepended to the preamble.
- **AC-008**: Given concurrent sessions each fire `t-reassess`, the per-session `p-round` tokens MUST NOT cross-contaminate.
- **AC-009**: Given a session pre-dating this feature (no `p-round` in persisted state), when it is rehydrated, it MUST resume without error with `n = 0`.
- **AC-010**: Given `t-reassess` sets `topic_shift: true`, when `t-followup` fires, then the deposited `p-round` token has `n: 0, reset: true` and telemetry records a `topic_shift_reset` event.

## 6. Test Automation Strategy

- **Test levels**: Unit (guards, builders, schema parsing), integration (CPN firing sequence), end-to-end (HTTP SSE → frontend render).
- **Backend frameworks**: Go standard `testing`, table-driven tests following the style of `topology_test.go`.
- **Frontend frameworks**: Vitest + React Testing Library, following existing `A2UIMessageRenderer` tests if present; otherwise introduce them scoped to this feature only.
- **Test data**: Fixtures stored alongside `topology_test.go` for classifier outputs, t-ask outputs, reassess outputs, and round history snapshots. Three canonical fest scenarios (natural convergence, frustration escape, contradiction escape) MUST each have a fixture set.
- **CI/CD integration**: GitHub Actions must run Go tests (`go test ./...`) and frontend tests (`pnpm test` or equivalent) on every PR that touches the specified paths.
- **Coverage requirements**: New guards (`guardResidualAmbiguous`, `guardResidualResolved`) MUST have 100% line + branch coverage. `buildPlannerPreamble` MUST cover all three variants. `t-reassess` fixture tests MUST cover each `decision` value.
- **Performance**: `t-reassess` latency SHOULD be under 2× the latency of `t-classify` on the same model. A warn-level log fires if `t-reassess` exceeds 10s wall-clock.
- **Regression**: The fest scenario (AC-001 → AC-002 full trace) MUST run as an end-to-end integration test hitting a mock LLM with canned responses.

## 7. Rationale & Context

**Why a dedicated `p-round` place?** CPN soundness requires guards be pure functions of consumed tokens. A counter in a place (not a Go variable) is the only formally clean way to encode a bounded loop. It also survives HITL pauses naturally via the same snapshot mechanism that already preserves other place markings.

**Why a single-call `t-reassess` instead of split `t-reassess` + `t-followup-llm`?** Two LLM calls per round double cost and latency with no practical win: the same model that scores residual ambiguity can emit the next questionnaire in the same JSON. The separate `t-followup` *transition* (no LLM, just arc routing + counter increment) is kept for net clarity, but it is not an LLM call.

**Why drop the suppressive directive from `buildClarifiedToken`?** With `t-reassess` formally gating the planner, the directive is redundant when we want to ask again and harmful when we want to give the planner stated assumptions. Moving it into a routing-aware `buildPlannerPreamble` makes the directive *conditional on the exit path*, which is the semantically correct placement.

**Why `k = 3`?** Three rounds is the observed ceiling where marginal utility goes negative: round 1 closes the biggest ambiguities, round 2 drills a specific axis, round 3 tends to annoy the user. Making `k` configurable lets us tune without a code change; making it a CPN constant makes the termination proof easy to state.

**Why the escape hatch?** Frustration and contradiction are the two UX failure modes the loop can produce. Both warrant an explicit, binary, zero-friction handoff back to the user. Without them, the loop can feel like an interrogation.

**Why Spanish-LATAM copy in the spec?** The user base is primarily LATAM founders. Neutral, non-shaming microcopy is a correctness requirement, not a polish. The i18n mechanism already exists.

**Why not re-use `RevisionLoop=true` on `t-clarify`?** `RevisionLoop` is user-driven ("user rejects and asks to revise"). The clarification loop must be *agent-driven* (the agent decides residual ambiguity is too high). Conflating them would muddle the semantics and the A2UI surface.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: LLM provider (existing) — `t-reassess` adds one structured-JSON call per round; no new provider integration.

### Third-Party Services
- None new.

### Infrastructure Dependencies
- **INF-001**: Execution persistence layer must snapshot place markings, not only transition state, for `p-round` and `p-reassessed`.

### Data Dependencies
- None new.

### Technology Platform Dependencies
- **PLT-001**: Go 1.22+ (existing).
- **PLT-002**: React 18+ (existing).

### Compliance Dependencies
- None.

## 9. Examples & Edge Cases

### 9.1 Fest scenario — natural convergence

```
Turn 1 user:  "ayúdame a planear un fest para emprendedores en Bogotá el próximo mes"
t-classify:   {intent: "task", needs_clarification: true, confidence: 0.58,
               missing: ["event_type","audience","budget"]}
t-ask R1:     questions about event_type, budget_band
user R1:      event_type="festival comunitario", budget_band="1–10k USD"
t-reassess:   {residual_ambiguity: 0.45, convergence_delta: 0.35,
               missing_dimensions: ["audience_size","success_metric"],
               resolved_dimensions: ["event_type","budget_band"],
               decision: "clarify_again", next_questions: {...}}
t-followup:   deposits p-round={n:1}, p-questions=<next_questions>
t-clarify R2: renders with round badge "Ronda 2 de 3"
user R2:      audience_size="100–300", success_metric="networking"
t-reassess:   {residual_ambiguity: 0.15, convergence_delta: 0.30,
               decision: "proceed_to_plan",
               stated_assumptions: [
                 "Festival comunitario presencial en Bogotá.",
                 "Presupuesto 1–10k USD.",
                 "Asistencia objetivo 100–300 fundadores.",
                 "Optimizado para networking, no inversión ni prensa."
               ]}
t-plan-clarified: planner preamble = Variant A, plan produced.
```

### 9.2 Frustration escape at round 2

```
user R1:      picks defaults on three questions
user R1 text: "no sé, tú decide"
t-reassess:   {residual_ambiguity: 0.55, convergence_delta: 0.05,
               frustration_signal: true, decision: "request_user_choice",
               user_choice: {prompt: "Quiero asegurarme de no atascarte...",
                             options: [drill, proceed]}}
UI:           renders escape-hatch card with two buttons.
user picks:   "Avanza con supuestos"
Next cycle:   t-reassess re-fires with this choice in round_history,
              emits decision: "proceed_to_plan" with synthesized assumptions.
t-plan-clarified: planner preamble = Variant C, plan produced.
```

### 9.3 Contradiction escape at round 2

```
user R1:      audience="founders"
user R2 text: "ah, en realidad es para inversionistas ángeles"
t-reassess:   {residual_ambiguity: 0.50, convergence_delta: -0.20,
               contradiction_detected: true, decision: "request_user_choice",
               user_choice: {prompt: "Detecté una contradicción...",
                             options: [{id:"founders",...}, {id:"angels",...}]}}
UI:           renders contradiction card.
user picks:   "Inversionistas ángeles"
Next cycle:   t-reassess folds the disambiguation into stated_assumptions,
              emits decision: "proceed_to_plan".
```

### 9.4 Hard cap at round 3

```
round counter reaches n=2, t-reassess returns decision: "clarify_again".
Guard evaluates: n+1 >= CLARIFY_MAX_ROUNDS → guardResidualAmbiguous = false,
                  guardResidualResolved = true.
Route: t-plan-clarified with Variant B preamble.
```

### 9.5 Edge — malformed t-reassess JSON

```
First attempt: parse fails.
Retry (RequireJSON + ResponseFmtRequired): parse fails again.
Guard: treats as proceed_to_plan with stated_assumptions=[
         "Proceeding with best-effort interpretation due to upstream parse error."
       ], logs WARN.
```

### 9.6 Edge — concurrent sessions

```
Session A has p-round={n:1}. Session B fires t-classify concurrently.
Each session has its own CPN instance and its own p-round marking.
Cross-contamination is structurally impossible; the test asserts this via
parallel goroutines each running the fest scenario and verifying their
own round counters.
```

## 10. Validation Criteria

- All ACs (AC-001 → AC-010) pass in CI.
- `go test ./back/go-assistant/...` stays green.
- Frontend tests covering REQ-100, REQ-102, REQ-103 green.
- Manual smoke with the fest prompt reaches a plan in ≤ 2 clarify rounds with visible round badges.
- Telemetry dashboard (or logs) shows `residual_ambiguity`, `convergence_delta`, `decision`, `round` for every `t-reassess` firing in staging.
- No regressions in existing spec-process-bugfix-classifier-clarify-bypass acceptance tests.

## 11. Related Specifications / Further Reading

- `spec/spec-architecture-a2a-a2ui-protocol-integration.md` — A2UI protocol contract; this spec extends it with an optional `round` field on clarify payloads.
- `spec/spec-process-bugfix-classifier-clarify-bypass.md` — ensures the first round fires; this spec builds directly on that guarantee.
- `spec/spec-process-bugfix-treview-surface-and-locked-parser.md` — HITL response persistence pattern; the escape-hatch binary surface follows the same flat-answer shape.
- `spec/spec-process-bugfix-a2ui-hitl-response-persistence.md` — questionnaire locking behavior; multi-round rehydration relies on it.
- `spec/spec-design-regional-language-variant.md` — i18n selection mechanism used by the Spanish-LATAM escape-hatch copy.
- `back/go-assistant/cmd/server/topologies.go` — target file for all backend changes.
- `front/react-assistant/src/features/chat/a2ui/A2UIMessageRenderer.tsx` — target for the round badge and escape-hatch card.
- `front/react-assistant/src/hooks/useChat.ts` — verify multi-round tolerance (no redesign expected).
