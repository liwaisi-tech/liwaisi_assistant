---
title: Bug Fix — Classifier Clarify Node Bypassed for Ambiguous Task Prompts
version: 1.0
date_created: 2026-04-06
last_updated: 2026-04-06
owner: liwaisi
tags: [process, bugfix, cpn, classifier, a2ui, go-assistant, react-assistant]
---

# Introduction

The unified CPN topology in `go-assistant` routes ambiguous task prompts straight to `t-plan-direct`, skipping the `t-ask` / `t-clarify` clarification loop introduced in commit `c889b5b`. This specification defines the bug, its root causes, and the exact changes required to make clarification fire as designed. It is scoped as a narrow bug-fix, not a feature redesign.

## 1. Purpose & Scope

**Purpose.** Restore the contract defined by `spec-architecture-a2a-a2ui-protocol-integration.md` around the classifier → clarify → plan routing, so that ambiguous task prompts raise an A2UI questionnaire before any plan is produced.

**In scope.**
- Guard logic in `back/go-assistant/cmd/server/topologies.go`.
- Classifier system prompt in the same file.
- Unit/integration tests in `back/go-assistant/cmd/server/topology_test.go`.
- Regression test reproducing the Spanish JWT-API prompt.
- Frontend verification of the questionnaire A2UI component in `front/react-assistant` (rendering of recommended answers + free-text input). Fix only if a defect is observed.

**Out of scope.**
- Changing the CPN structure (places, transitions, arcs).
- Redesigning the classifier model or replacing it.
- Altering the A2UI protocol or the questionnaire schema.
- Persistence-layer refactors.
- UI redesign beyond defects directly blocking this flow.

**Audience.** Implementing agents: `golang-pro` (backend), `vercel-react-best-practices` + `frontend-design` (frontend verification), plus reviewers.

## 2. Definitions

- **CPN**: Coloured Petri Net — execution model used by go-assistant.
- **Classifier**: LLM transition `t-classify` that emits a JSON intent record.
- **Clarification loop**: `t-ask` (emit questionnaire) → `p-questions` → user answers → `t-clarify` → `p-clarified` → `t-plan-clarified`.
- **Direct-plan path**: `t-plan-direct` fires when a task is fully specified.
- **A2UI**: Agent-to-UI protocol used to render interactive surfaces (questionnaire, plan review).
- **HITL**: Human-in-the-loop transition (review gate).
- **Ambiguous prompt**: A task request missing load-bearing details (stack, persistence, auth strategy, deployment target, etc.).

## 3. Requirements, Constraints & Guidelines

### Backend requirements

- **REQ-001**: `guardPlanTaskDirect` MUST evaluate to `true` if and only if `intent == "task" && needs_clarification == false`. It MUST NOT inspect `missing[]` length.
- **REQ-002**: `guardNeedsClarification` MUST continue to require `intent == "task" && needs_clarification == true`. It MUST accept the token regardless of `missing[]` length once REQ-005 is enforced upstream, but SHOULD defensively treat an empty `missing[]` as still firing `t-ask` (never silently fall through to plan).
- **REQ-003**: The classifier prompt MUST state explicitly that when `needs_clarification == true`, `missing` MUST contain at least one item and at most four. If the classifier cannot name a missing field, it MUST set `needs_clarification = false`.
- **REQ-004**: The classifier prompt MUST include a worked example whose shape matches the regression prompt (Spanish, ambiguous backend-API request) and routes to `needs_clarification: true` with a non-empty `missing[]`.
- **REQ-005**: The classifier prompt MUST list concrete ambiguity dimensions for backend/API requests: `persistence`, `framework`, `auth_strategy`, `deployment_target`, `scale`, `testing_expectations`.
- **REQ-006**: Test `topology_test.go` cases that assert `{needs_clarification:true, missing:[]}` routes to `t-plan-direct` MUST be flipped to assert it routes to `t-ask` (or is rejected at the guard boundary).
- **REQ-007**: A new regression test MUST cover the exact user prompt `"crea un plan detallado para construir un API en golang con JWT"` and assert `guardNeedsClarification == true` and `guardPlanTaskDirect == false` given a realistic classifier output.
- **REQ-008**: Existing green tests not covered by REQ-006/REQ-007 MUST remain green.

### Frontend requirements

- **REQ-101**: The questionnaire A2UI component in `react-assistant` (from commit `c889b5b`) MUST render each question with (a) a label, (b) the list of recommended answers as selectable chips/options, and (c) a free-text fallback input.
- **REQ-102**: Submitting the questionnaire MUST send one consolidated response payload back through the A2UI channel feeding `t-clarify`.
- **REQ-103**: The questionnaire surface MUST NOT be duplicated alongside a HITL review surface for the same turn (honour the existing dedupe rule from commit `9abc976`).
- **REQ-104**: Frontend changes are only required if REQ-101..103 are observed to fail during manual verification against a fixed backend. No speculative refactors.

### Constraints

- **CON-001**: Must remain aligned with `spec-architecture-a2a-a2ui-protocol-integration.md` guard definitions.
- **CON-002**: Go code must pass `go vet`, `go test ./...`, and existing linters. No new dependencies.
- **CON-003**: Frontend changes (if any) must follow the repository's React/Next.js conventions and use the `frontend-design` skill for any visual change.
- **CON-004**: No changes to the CPN topology wiring (place/transition/arc set).

### Guidelines

- **GUD-001**: Prefer the minimal diff that satisfies the acceptance criteria.
- **GUD-002**: Keep the classifier prompt examples bilingual (Spanish + English) to reflect observed user traffic.
- **GUD-003**: When flipping tests, rename them to reflect the corrected invariant rather than leaving misleading names.

### Patterns

- **PAT-001**: Guard functions stay pure and derive their decision from the classifier token only.
- **PAT-002**: Regression tests encode the exact failing prompt and a realistic classifier JSON output, not a synthetic minimal one.

## 4. Interfaces & Data Contracts

### Classifier output schema (unchanged, constraint tightened)

```json
{
  "intent": "conversation" | "task",
  "needs_clarification": true,
  "missing": ["persistence", "framework", "auth_strategy"]
}
```

Tightened invariants:

| Field | Constraint |
|---|---|
| `intent` | Enum: `conversation` \| `task`. |
| `needs_clarification` | `false` when `intent == "conversation"`. |
| `missing` | `[]` iff `needs_clarification == false`. `1..4` items iff `needs_clarification == true`. |

### Guard truth table (target behaviour)

| intent | needs_clarification | missing | guardNeedsClarification | guardPlanTaskDirect |
|---|---|---|---|---|
| conversation | false | [] | false | false |
| task | false | [] | false | **true** |
| task | true | ["framework"] | **true** | false |
| task | true | [] (invalid, see REQ-003) | **true** (defensive) | **false** |

### Files & line anchors

| File | Area | Change |
|---|---|---|
| `back/go-assistant/cmd/server/topologies.go:71-80` | `guardPlanTaskDirect` | Replace body per REQ-001. |
| `back/go-assistant/cmd/server/topologies.go:61-67` | `guardNeedsClarification` | Adjust per REQ-002 (defensive). |
| `back/go-assistant/cmd/server/topologies.go:215-240` | `PROMPT_CLASSIFIER` | Tighten per REQ-003..005, add example per REQ-004. |
| `back/go-assistant/cmd/server/topology_test.go:~228-231` | Guard test cases | Flip per REQ-006. |
| `back/go-assistant/cmd/server/topology_test.go` (new) | Regression test | Add per REQ-007. |
| `front/react-assistant/**` questionnaire component (from `c889b5b`) | A2UI rendering | Verify REQ-101..103; fix only if broken. |

## 5. Acceptance Criteria

- **AC-001**: Given the classifier emits `{"intent":"task","needs_clarification":true,"missing":["persistence","auth_strategy"]}`, When the topology evaluates guards, Then `guardNeedsClarification` returns `true` and `guardPlanTaskDirect` returns `false`.
- **AC-002**: Given the classifier emits `{"intent":"task","needs_clarification":false,"missing":[]}`, When the topology evaluates guards, Then `guardPlanTaskDirect` returns `true` and `guardNeedsClarification` returns `false`.
- **AC-003**: Given the classifier emits the invalid shape `{"intent":"task","needs_clarification":true,"missing":[]}`, When the topology evaluates guards, Then `guardPlanTaskDirect` returns `false` (no silent bypass).
- **AC-004**: Given the user sends `"crea un plan detallado para construir un API en golang con JWT"`, When the classifier is prompted with the updated `PROMPT_CLASSIFIER`, Then its JSON output has `needs_clarification == true` and `missing` contains at least one of `{persistence, framework, auth_strategy, deployment_target}`. *(Validated via a prompt-level test using a recorded/mocked classifier response.)*
- **AC-005**: `go test ./...` in `back/go-assistant` passes, including the new regression test from REQ-007.
- **AC-006**: Manual E2E: the JWT-API Spanish prompt produces an A2UI questionnaire surface (not a plan) as the first assistant turn. Selecting recommended answers and/or typing free-text then submitting produces a plan that reflects the provided answers.
- **AC-007**: No duplicate HITL + questionnaire surface is rendered for the same turn (REQ-103).

## 6. Test Automation Strategy

- **Test Levels**
  - Unit: guard functions in `topologies.go` against hand-crafted tokens.
  - Unit: classifier prompt examples captured as golden JSON, parsed and validated against the tightened schema.
  - Integration: CPN firing test that walks `p-input → t-classify (mocked) → p-classified → t-ask` for the regression prompt.
  - E2E (manual, documented): one scripted scenario using the Spanish JWT-API prompt.
- **Frameworks**: Go standard `testing` + existing test helpers already used in `topology_test.go`. Frontend uses the existing react-assistant test setup; add tests only if a frontend fix is required.
- **Test Data Management**: Classifier outputs are inlined as JSON string literals in test files. No fixtures on disk.
- **CI/CD Integration**: Runs through the existing `go test ./...` pipeline for `back/go-assistant`.
- **Coverage Requirements**: Guard functions in `topologies.go` MUST reach 100% line coverage for the three branches in the truth table above.
- **Performance Testing**: Not applicable to this bug fix.

## 7. Rationale & Context

The clarification loop was added in `c889b5b` to prevent premature plan generation on under-specified requests. Two independent defects defeat the loop:

1. **Guard over-fires.** `guardPlanTaskDirect` was written as `!(needs_clarification && len(missing) > 0)`, which is `true` whenever `missing` is empty — even if the classifier explicitly asked for clarification. The spec definition is simply `needs_clarification == false`; the implementation added an unintended second clause that creates a silent bypass.
2. **Classifier prompt permits the invalid state.** The prompt never forbids `needs_clarification=true` with `missing=[]`, so the LLM can legitimately produce the exact shape that triggers the guard bug. Additionally, the ambiguity criteria under-emphasise backend-API dimensions, so "build a JWT API in Go" is sometimes classified as fully specified even though it lacks persistence/framework/deployment choices.

Flipping the guard without tightening the prompt would still leave the clarify loop dependent on LLM discipline. Tightening the prompt without flipping the guard would still allow regressions the next time a model drifts. Both changes are required.

The existing test at `topology_test.go:~228-231` encodes the current (wrong) behaviour and therefore must be inverted; otherwise the fix cannot land without breaking CI.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: Classifier LLM (configured via `classifier` model alias in `LLMConfig`) — must honour `RequireJSON` and the tightened schema.

### Infrastructure Dependencies
- **INF-001**: Existing go-assistant runtime; no new infrastructure.

### Technology Platform Dependencies
- **PLT-001**: Go toolchain as currently pinned by the repo.
- **PLT-002**: react-assistant frontend toolchain as currently pinned.

### Compliance Dependencies
- None.

## 9. Examples & Edge Cases

### Corrected guard (Go)

```go
// guardPlanTaskDirect fires t-plan-direct when the classifier returned a task
// intent that is fully-specified (no clarification needed).
// Spec: intent == "task" && needs_clarification == false.
func guardPlanTaskDirect(tokens []*cpn.Token) bool {
    r, ok := parseClassified(tokens)
    if !ok {
        return false
    }
    if !strings.EqualFold(r.Intent, "task") {
        return false
    }
    return !r.NeedsClarification
}
```

### Classifier prompt — tightened rules (excerpt)

```
Rules for "needs_clarification" / "missing" (only when intent == "task"):
- Set needs_clarification == true when the request omits load-bearing details
  that materially change the plan. For backend/API requests, load-bearing
  dimensions include: persistence, framework, auth_strategy,
  deployment_target, scale, testing_expectations.
- If needs_clarification == true, "missing" MUST contain 1..4 short field
  names. If you cannot name at least one missing field, set
  needs_clarification = false.
- If needs_clarification == false, "missing" MUST be [].

Examples:
User: "crea un plan detallado para construir un API en golang con JWT"
  → {"intent":"task","needs_clarification":true,
     "missing":["persistence","framework","auth_strategy","deployment_target"]}
```

### Edge cases

- **EC-001**: Classifier returns malformed JSON → `parseClassified` returns `ok=false` → both guards return `false` → existing fallback path handles it (unchanged).
- **EC-002**: Classifier returns `intent="conversation"` with `needs_clarification=true` → ignored; conversation path fires. No change required.
- **EC-003**: Classifier returns `missing` with 5+ items → schema violation; treat as if truncated. Not blocking this fix; file a follow-up if observed.
- **EC-004**: User submits questionnaire with all free-text empty → frontend MUST block submission (REQ-101). Not introduced by this fix unless regression discovered.

## 10. Validation Criteria

1. All acceptance criteria AC-001..AC-007 pass.
2. `go test ./...` green in `back/go-assistant`.
3. `go vet ./...` clean.
4. Manual reproduction of the Spanish JWT-API prompt produces a questionnaire, not a plan.
5. Code review by `golang-pro` (backend) and `vercel-react-best-practices` + `frontend-design` (frontend, if touched) signs off.
6. Diff is minimal and does not touch unrelated code.

## 11. Related Specifications / Further Reading

- `spec/spec-architecture-a2a-a2ui-protocol-integration.md` (guard definitions, §946).
- Commit `c889b5b` — feat(a2ui): add clarification node (t-ask/t-clarify) with questionnaire component.
- Commit `9abc976` — fix(a2ui): suppress duplicate HITL surface when transition owns the A2UI.

## 12. Addendum — Confidence Field and Principled Ambiguity Rule (2026-04-06)

A second wave of observed failures (an ambiguous Spanish product-launch prompt
slipped through as `needs_clarification=false`, and the previous prompt over-fit
to hardcoded domain examples) drove the following changes. This section is
additive and does not supersede prior sections.

### 12.1 Principled ambiguity rule

`PROMPT_CLASSIFIER` no longer enumerates domain-specific dimension tables or
worked examples for specific languages / launch scenarios. Instead it gives the
model a decision procedure:

> A task is fully specified if a senior practitioner in the relevant field could
> produce a concrete, non-generic plan without making more than ONE significant
> assumption about an unnamed parameter. Otherwise it is under-specified.

The prompt lists a generic mental checklist (audience, success criterion,
stack/medium/format, scope, timeline, constraints) and instructs the model to
count how many of those are load-bearing AND unnamed. The prompt keeps a tiny
set of shape-only examples across unrelated domains so no single domain
dominates. The Spanish JWT-API and Spanish product-launch prompts are NOT
present verbatim — they are covered by the principle.

### 12.2 `confidence` field

The classifier schema gains a required `confidence` float in `[0.0, 1.0]`
reflecting certainty about BOTH the intent and (for tasks) the specification-
completeness judgment. Calibration anchors are written into the prompt
(1.0 certain, 0.85 clearly right, 0.7 more likely than not, 0.5 coin flip).

Backward compatibility: if `confidence` is absent (legacy classifier), it is
treated as `1.0` by `classifierResult.confidence()`.

### 12.3 Threshold and guard behavior

A new env var `CLASSIFIER_CONFIDENCE_THRESHOLD` (default `0.7`) gates the
planner path:

- `guardPlanTaskDirect` fires only when
  `intent=="task" && !needs_clarification && confidence >= threshold`.
- `guardNeedsClarification` fires when
  `intent=="task" && (needs_clarification || confidence < threshold)`.
  The second disjunct is the **safety net** against overconfident skips.
- `guardDirectConversation` is unchanged. Low confidence on a conversation
  intent does NOT block the direct path — conversation is the safe fallback.

The default of `0.7` was chosen so that "more likely right than not" still
plans directly, but any honest "coin flip" bucket (≤0.5) routes through
clarification. Operators who observe either too much or too little clarifying
can tune the env var without a redeploy of prompt text.

### 12.4 `t-ask` fallback for empty `missing[]`

Because a low-confidence task can now reach `t-ask` with an empty `missing[]`
array, `PROMPT_ASK` gains a short fallback instruction: generate 2–4 generic
clarifying questions drawn from the same load-bearing dimensions (audience,
success criterion, scope, stack/medium/format, timeline) when `missing` is
absent or empty. The schema and option rules are unchanged.

