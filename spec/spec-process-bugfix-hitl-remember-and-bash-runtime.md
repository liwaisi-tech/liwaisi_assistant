---
title: Bugfix — HOST·HITL "approve-and-remember" Persistence and Bash Runtime Availability
version: 1.0
date_created: 2026-04-20
last_updated: 2026-04-20
owner: liwaisi/go-assistant + react-assistant
tags: [process, bugfix, hitl, host-gate, shell, cpn, a2ui]
---

# Introduction

This specification defines the bugfix for two coupled defects observed in the HOST·HITL
(Human-In-The-Loop) shell-approval flow of the `liwaisi_assistant` product. Both defects
were reproduced on branch `feat/model-registry-foundation` (commit `5bd0c15`) and are
surfaced to the user as (a) an impossible-to-satisfy approval loop on shell commands,
and (b) a runtime error claiming `bash` is not installed even though the approval was
granted.

The spec is self-contained and written for direct consumption by implementation agents
(`golang-pro` for backend, `frontend-design` + `vercel-react-best-practices` for frontend,
shared integration owner for the wiring step).

## 1. Purpose & Scope

**Purpose.** Deliver the minimum set of code changes to make the "Aprobar y recordar"
(approve-and-remember) button behave per A2UI v0.8 ApprovalCard semantics *and* to make
the approved shell command actually execute successfully inside the runtime container.

**In scope.**
- Wiring the `gate.HandleRequiresHITL` handler from `cpn/fire_bash.go` through the
  existing session HITL channels.
- Persisting "approve-and-remember" decisions to the policy holder so subsequent
  identical tool calls short-circuit to `VerdictAllow` without emitting a new
  ApprovalCard.
- Normalising the command key used for remembered-approval lookups so that
  `bash -c "<cmd>"` and plain `<cmd>` collapse to the same match key (or the tool
  always canonicalises one way).
- Guaranteeing that whatever shell binary the LLM is instructed to call exists in the
  runtime container.

**Out of scope.**
- Introducing a new HITL endpoint or a new A2UI component. The existing
  `POST /api/v1/sessions/{id}/hitl/{transitionID}` endpoint and `HostApprovalCard`
  surface are authoritative.
- Changing the wire contract of `HITLAction` (stays `approve | reject | submit | revise`).
- Redesign of the host-gate policy model, first-run ledger, or sandbox profiles.
- Front-end visual refresh. UI changes are limited to behaviour of the existing card.

**Intended audience.** Implementation agents: `golang-pro` (BE-*), `frontend-design` +
`vercel-react-best-practices` (FE-*), integration owner (INT-*).

## 2. Definitions

| Term | Definition |
|---|---|
| HITL | Human-In-The-Loop. Protocol pause where user input is required before a transition completes. |
| HOST·HITL | The subset of HITL prompts emitted by the `PolicyHostGate` before a shell/file operation fires. |
| ApprovalCard | A2UI v0.8 component rendered for `host.approval` prompts. Buttons map to the four extended actions. |
| Extended action | One of `approve-once`, `approve-and-remember`, `deny`, `deny-and-blacklist`. Carried in the JSON `content` field of the HITL response. |
| Narrowed action | Wire-level `HITLAction` value, one of `approve | reject`. Sent in the `action` field. |
| CPN | Coloured Petri Net — the execution engine described in `.docs/motor_agentico_cpn.md`. |
| Gate | `PolicyHostGate` (`back/go-assistant/infra/host/gate/policy_gate.go`) — the component that decides `allow | deny | requires_hitl` for each `GateOp`. |
| Policy holder | `Policies` field on the gate. Owns `learned.yaml` overlay of user-approved safe patterns. |
| `rememberApproval` | Gate method at `infra/host/gate/hitl.go:121-129` that appends a literal-prefix regex of the command to the learned safe-patterns. |
| `normaliseCommand` / `CommandKey` | Whitespace-collapsing normaliser used to build both the safe-pattern key and the lookup key. |
| `HITLRouter` | Minimal interface (`Publish`, `AwaitResponse`) the gate uses to emit a prompt and await a response. Currently **not injected** on the fire path. |
| `SessionService` | CPN plumbing that already carries HITL channels end-to-end through the existing LLM-level HITL flow. |

## 3. Requirements, Constraints & Guidelines

### Functional Requirements

- **REQ-001 (BUG-2 core)**: When the user clicks "Aprobar y recordar" on a HOST·HITL
  ApprovalCard, the backend MUST persist the approval to the policy holder via
  `PolicyHostGate.rememberApproval` before the shell command fires.
- **REQ-002**: A subsequent `gate.Check(ctx, op)` for the same `GateOp` MUST return
  `VerdictAllow` without emitting a new ApprovalCard.
- **REQ-003**: "Aprobar solo esta vez" MUST allow the current call and MUST NOT modify
  the policy.
- **REQ-004**: "Rechazar" MUST deny the current call and MUST NOT modify the policy.
- **REQ-005**: "Rechazar y bloquear para siempre" MUST deny the current call and MUST
  append a forbidden pattern (in-memory only, per existing `blacklist()` semantics).
- **REQ-006 (BUG-1 core)**: The runtime container MUST provide the shell binary the LLM
  tool schema instructs it to invoke. The authoritative pair is:
  `system_tools.go` prompt ↔ runtime image. They MUST agree.
- **REQ-007**: The approved command's PATH resolution MUST succeed inside the runtime
  container. `os_adapter.go` MUST ensure either (a) the child process inherits a PATH
  that can locate the binary, or (b) the LLM is only ever asked to call absolute
  binary paths.
- **REQ-008 (lookup fidelity)**: The remembered-approval key MUST match the same
  logical command irrespective of whether the LLM invokes it as
  `bash -c "<cmd>"` or directly `<cmd>`. Acceptable solutions:
  - **Option A** (preferred): Canonicalise the tool contract so the LLM emits one
    shape only (update `system_tools.go` prompt + tool schema).
  - **Option B**: Extend `CommandKey` to unwrap a `bash -c` / `sh -c` prefix before
    normalising.

### Non-Functional Requirements

- **NFR-001**: Remembered approvals MUST survive process restarts (already satisfied by
  `Policies.AppendLearnedSafePattern` writing to `learned.yaml`).
- **NFR-002**: No regression on existing LLM-level tool HITL flow introduced in commit
  `f99b2f1`. The HOST·HITL path and the LLM-tool-HITL path remain independent channels
  per commit `5bd0c15`.
- **NFR-003**: Code coverage ≥ 85 % on every touched package (per user's dev workflow).
- **NFR-004**: No new dependencies added to `go.mod`; no new npm deps in
  `package.json`.

### Security Requirements

- **SEC-001**: When `HITLRouter` is nil (misconfiguration), the gate MUST deny
  (existing behaviour at `hitl.go:52-63` — preserve).
- **SEC-002**: `rememberApproval` MUST continue to escape regex metacharacters via
  `regexpQuote` and anchor with `^...($|\s)` (existing `hitl.go:127` — preserve).
- **SEC-003**: "Deny-and-blacklist" MUST NOT write to `learned.yaml`. The asymmetric
  persistence comment at `hitl.go:131-134` is load-bearing: a mistaken deny click must
  not permanently lock a user out.
- **SEC-004**: No PATH hijack. If Option Step 6.1 is chosen (inherit parent environ),
  the container base image PATH MUST be audited — Alpine ships with
  `/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin` by default, which is
  acceptable.

### Constraints

- **CON-001**: The HTTP wire contract for `POST /api/v1/sessions/{id}/hitl/{transitionID}`
  is frozen. The extended action travels in `content` as `{"action":"<extended>"}`.
  No change to `ResolveHITLRequest`.
- **CON-002**: `HITLAction` enum is frozen at `approve | reject | submit | revise`.
- **CON-003**: The gate's error sentinel `ErrRequiresHITL` must be extracted via
  `errors.As` + `IsRequiresHITL()`; do not introduce a new sentinel.
- **CON-004**: Do not modify the first-run ledger logic (`dec.FirstRun` branch at
  `hitl.go:107-111`) beyond what REQ-001 requires.

### Guidelines

- **GUD-001**: When in doubt, deny (see `hitl.go:53`).
- **GUD-002**: Prefer a pure routing change over behaviour change. The gate already
  knows how to process every extended action; this bug is a *wiring* bug.
- **GUD-003**: Prefer Option A for REQ-008 (contract canonicalisation) — a simpler
  tool schema reduces LLM drift and makes the key invariant trivially obvious.

### Patterns

- **PAT-001**: The CPN event bus is the single source of truth for A2UI surface
  publication. A new `HITLRouter` implementation MUST publish through `c.emit(...)`
  rather than a side channel.
- **PAT-002**: `AwaitResponse` MUST be cancellable via `ctx`; session teardown MUST
  unblock the wait (return `ctx.Err()`).

## 4. Interfaces & Data Contracts

### 4.1 Frontend → Backend (unchanged)

`POST /api/v1/sessions/{id}/hitl/{transitionID}`

```json
{
  "action": "approve",
  "content": "{\"action\":\"approve-and-remember\"}"
}
```

The four buttons map as follows (see `front/react-assistant/src/features/chat/MessageBubble.tsx:137-145`):

| Button label (es-CO) | `action` (wire) | `content.action` (extended) |
|---|---|---|
| Aprobar solo esta vez | `approve` | `approve-once` |
| Aprobar y recordar | `approve` | `approve-and-remember` |
| Rechazar | `reject` | `deny` |
| Rechazar y bloquear para siempre | `reject` | `deny-and-blacklist` |

### 4.2 Internal — new `HITLRouter` adapter over existing CPN channels

```go
// Proposed location: back/go-assistant/infra/host/gate/session_router.go
//
// SessionHITLRouter adapts the CPN's existing HITL channels to the gate's
// HITLRouter interface. It publishes HostApprovalPrompt as an A2UI surface
// event and blocks on the session's hitl-inject channel until the user
// responds. The extended action from req.Content is decoded into a
// HostApprovalResponse.

type SessionHITLRouter struct {
    CPN          *cpn.CPN           // for event emission
    TransitionID string
    Inject       <-chan cpn.HITLResponse
}

func (r *SessionHITLRouter) Publish(ctx context.Context, p HostApprovalPrompt) error { /* emit */ }
func (r *SessionHITLRouter) AwaitResponse(ctx context.Context) (HostApprovalResponse, error) { /* decode */ }
```

The router extracts `HostApprovalResponse.Action` from `cpn.HITLResponse.Content`:

```go
// Content shape: {"action":"approve-and-remember"}
var body struct { Action string `json:"action"` }
_ = json.Unmarshal([]byte(resp.Content), &body)
out := HostApprovalResponse{
    Action:      body.Action,         // may be "" → treated as deny (hitl.go:87)
    RespondedAt: time.Now(),
}
```

### 4.3 Tool contract canonicalisation (REQ-008, Option A)

**Before** (`system_tools.go:32`, excerpted):

> For multi-command pipelines use `command='bash'` with `args=['-c','cmd1 && cmd2']`.

**After** (proposed):

> Always invoke a concrete binary as `command=<name>` with its flags in `args=[...]`.
> For multi-command pipelines use `command='/bin/sh'` with
> `args=['-c','cmd1 && cmd2']`. Do not use `bash`.

The tool JSON schema's `command` field SHOULD constrain the LLM via description +
examples to emit `/bin/sh` (which exists on Alpine) for pipelined invocations.

### 4.4 Docker runtime baseline

- Base image: `alpine:3.x` (per `back/go-assistant/Dockerfile:25`).
- Installed: `sh` (via `busybox`). NOT installed: `bash`.
- Action: keep Alpine; canonicalise to `/bin/sh` (Option A). If Option B is chosen
  instead, `RUN apk add --no-cache bash` after the base-image line and document the
  ~1.2 MB image size increase.

## 5. Acceptance Criteria

### BE Acceptance

- **AC-BE-001**: Given a chat session with no remembered approvals, When the CPN fires
  a `NodeKindBash` transition whose command triggers `ErrRequiresHITL`, Then exactly
  one HOST·HITL ApprovalCard surface is emitted on the event bus.
- **AC-BE-002**: Given the ApprovalCard is pending, When the user POSTs
  `{action:"approve",content:"{\"action\":\"approve-and-remember\"}"}`, Then
  `rememberApproval` is invoked with the `GateOp` of the pending call AND the bash
  transition proceeds to `HostAdapter.Exec`.
- **AC-BE-003**: After AC-BE-002, When the CPN fires the same `GateOp` a second time,
  Then `gate.Check` returns nil (no `ErrRequiresHITL`), no ApprovalCard is emitted, and
  the command executes directly.
- **AC-BE-004**: Given a "Rechazar" decision, When the HTTP handler completes, Then the
  policy holder's `learned.yaml` is byte-for-byte identical to its pre-decision state.
- **AC-BE-005**: Given `HostAdapter.Exec(req)` where `req.Command == "/bin/sh"` and
  `req.Args == ["-c","uname -a && uptime"]`, Then the exec succeeds with exit 0 inside
  the Alpine runtime container.
- **AC-BE-006**: Given REQ-008 Option A is chosen, When the LLM emits a command, Then
  the normalised `CommandKey` for the first and every subsequent call is identical
  (byte-equal), verified by a unit test driven by the frozen tool-schema contract.

### FE Acceptance

- **AC-FE-001**: Given a HOST·HITL ApprovalCard is rendered, When the user clicks
  "Aprobar y recordar", Then the network layer POSTs
  `{action:"approve",content:"{\"action\":\"approve-and-remember\"}"}` and the card
  transitions to the "Respondido" resolved state without client-side state divergence.
  (Existing behaviour — this AC is a regression guard, not new work.)
- **AC-FE-002**: Given the card has been resolved with `approve-and-remember`, When a
  subsequent assistant turn executes the same shell command, Then no new ApprovalCard
  appears in the transcript for that command. (End-to-end confirmation, depends on BE
  fix.)
- **AC-FE-003**: The `MessageBubble` branch at lines 137-145 is UNCHANGED. The fix
  lives entirely server-side; FE work is verification only.

### Integration Acceptance

- **AC-INT-001**: An integration test (`back/go-assistant/integration/...`) drives a
  real CPN session end-to-end: first call produces an ApprovalCard, user POSTs
  `approve-and-remember`, second call with the same command runs without ApprovalCard.
- **AC-INT-002**: `docker compose up` produces a runtime in which
  `/bin/sh -c "uname -a && uptime && df -h /"` (the exact command from the reproducer
  screenshot) exits 0.

## 6. Test Automation Strategy

### Test Levels

- **Unit (Go)**. Target packages: `infra/host/gate`, `infra/host`, `cpn`.
  - New test file: `infra/host/gate/session_router_test.go` — table-driven tests for
    extended-action decoding (`approve-once`, `approve-and-remember`, `deny`,
    `deny-and-blacklist`, empty, garbage JSON).
  - Extend `cpn/fire_bash_test.go` with a fake `HITLRouter` to verify the routing path
    through `HandleRequiresHITL`.
  - Extend `infra/host/gate/policy_gate_test.go` to cover the REQ-008 key-invariance
    case (same-key-before-and-after).
- **Unit (TS)**. None required; REQ-001 is server-side.
- **Integration (Go)**. `integration/hitl_remember_test.go` — driving adapter spins a
  CPN, sends two identical bash requests, resolves HITL once, asserts the second fires
  directly.
- **E2E (optional, manual)**. Playwright or manual reproduction of the screenshot
  scenario through the UI.

### Frameworks

- Go: stdlib `testing` + `testify` (already used in the repo — do not add new libs).
- React: `vitest` + `@testing-library/react` (already present).

### Test Data Management

- No DB fixtures. `learned.yaml` written under `t.TempDir()` for each test; cleanup via
  `t.Cleanup`.
- CPN sessions constructed inline; no external state.

### CI/CD Integration

- `make test` must pass. Coverage gate: `go tool cover` ≥ 85 % on touched packages
  (NFR-003).
- `npm test` must pass on frontend, coverage ≥ 85 % on touched frontend files (none
  expected to change).

### Performance Testing

- Not required. HITL approve-and-remember is a cold-path decision; no latency budget
  change.

## 7. Rationale & Context

The bug is a **wiring gap**, not missing behaviour: the gate's
`HandleRequiresHITL` function (commit `5bd0c15`) was merged but the executor
(`cpn/fire_bash.go:55`) was left in its pre-HITL shape — it short-circuits on any
`gate.Check` error and returns it verbatim. The HTTP handler
(`internal/driving/httpapi/handler_hitl.go:49-52`) faithfully stores the extended
action in `cpn.HITLResponse.Content` but no downstream consumer decodes it. The
policy-side plumbing (`rememberApproval` → `AppendLearnedSafePattern` → `matches`) is
complete and unit-tested — it simply never receives the signal.

The bash error is a symptom of an implicit contract drift: the tool schema tells the
LLM to use `bash`, but the runtime image is Alpine (no bash). Option A (canonicalise
the prompt to `/bin/sh`) is preferred because it is a one-line prompt change with zero
image-size cost, and because it *also* fixes the command-key non-determinism called
out in REQ-008: when the LLM sometimes wraps commands in `bash -c` and sometimes
doesn't, the `CommandKey` for "approve-and-remember" fails to match on the next turn
even if everything else were correctly wired.

These two bugs are specified together because fixing only one does not fix the user's
observable problem: without BUG-1 the approved command fails; without BUG-2 the user
is prompted again even though the command now works.

## 8. Dependencies & External Integrations

### Infrastructure Dependencies

- **INF-001**: `alpine:3.x` base image — provides `/bin/sh`. No change required under
  Option A.

### Technology Platform Dependencies

- **PLT-001**: Go ≥ 1.22 (existing). No change.
- **PLT-002**: Node ≥ 20 (existing). No change.

### Compliance Dependencies

- **COM-001**: The asymmetric persistence of approve-and-remember vs
  deny-and-blacklist is an intentional UX-safety property (see SEC-003). Auditors may
  ask why a deny does not persist; the answer lives in `hitl.go:131-134` — preserve
  that comment.

## 9. Examples & Edge Cases

### 9.1 Happy path (REQ-001, REQ-002)

```text
t0  LLM proposes `/bin/sh -c "uname -a"`.
t1  CPN fires NodeKindBash. gate.Check → ErrRequiresHITL{prompt}.
t2  fire_bash extracts ErrRequiresHITL via errors.As.
t3  fire_bash builds SessionHITLRouter{cpn, transition, injectCh}.
t4  gate.HandleRequiresHITL publishes ApprovalCard (EventHITLRequested).
t5  User clicks "Aprobar y recordar" → POST /hitl/:id.
t6  handler_hitl forwards cpn.HITLResponse{Action:"approve", Content:`{"action":"approve-and-remember"}`}.
t7  router.AwaitResponse decodes → HostApprovalResponse{Action:"approve-and-remember"}.
t8  gate.HandleRequiresHITL: switch → rememberApproval → AppendLearnedSafePattern.
t9  HandleRequiresHITL returns nil. fire_bash proceeds to HostAdapter.Exec.
t10 Next turn: LLM proposes same `/bin/sh -c "uname -a"`.
t11 gate.Check → safe.matches(CommandKey) → true → VerdictAllow. No ApprovalCard.
```

### 9.2 Edge case — empty extended action

Input: `{"action":"approve","content":""}` or `content:"{}"`.
Decoded `HostApprovalResponse.Action == ""`. Per `hitl.go:87` this falls into the
`ActionDeny, ""` branch → `VerdictDeny`. This preserves GUD-001.

### 9.3 Edge case — malformed JSON in `content`

Input: `content:"not-json"`. `json.Unmarshal` returns an error; router treats it as
empty action (`""`) and denies. Test must assert the error is logged, not propagated
as a 500.

### 9.4 Edge case — prefix drift (REQ-008)

Without Option A, the LLM may emit:

- Turn 1: `command="bash", args=["-c","uname -a"]` → `CommandKey = "bash"`.
- Turn 2: `command="uname", args=["-a"]` → `CommandKey = "uname"`.

The remembered pattern `^bash($|\s)` would NOT match `uname`. Option A eliminates
this class of mismatch by freezing the LLM on `/bin/sh`.

### 9.5 Concurrency — two pending calls on the same command

Two CPN transitions fire concurrently with the same `GateOp`. Both reach
`gate.Check` before either is approved. Expected: two ApprovalCards, but the first
"approve-and-remember" resolution auto-approves the second (because
`safe.matches` is consulted on `Check`). Acceptable. Document in AC-BE-003.

## 10. Validation Criteria

An implementation is compliant with this spec iff ALL of the following hold:

1. All requirements REQ-001 through REQ-008 have at least one automated test whose
   failure blocks merge.
2. All acceptance criteria AC-BE-* and AC-INT-* pass locally via `make test` and in
   CI.
3. `grep -n 'bash -c' back/go-assistant/cpn/system_tools.go` returns no matches after
   the change (Option A) — OR the Dockerfile adds `bash` (Option B).
4. The reproducer scenario from the user's screenshot executes end-to-end without a
   second ApprovalCard and without a command-not-found error.
5. No regressions in `make test` for existing packages.
6. Coverage on `infra/host/gate`, `infra/host`, `cpn` remains ≥ 85 %.
7. Frontend `npm test` passes with no changes to test expectations in `MessageBubble`.
8. The asymmetric-persistence comment at `hitl.go:131-134` is preserved verbatim.

## 11. Related Specifications / Further Reading

- `spec/spec-process-bugfix-tool-hitl-single-gate.md` — prior single-gate unification.
- `spec/spec-architecture-host-gate-security-policy.md` — policy model background.
- `spec/spec-architecture-host-adapter-nodekind-bash.md` — `HostAdapter.Exec` contract.
- `spec/spec-process-bugfix-a2ui-hitl-response-persistence.md` — rehydration semantics.
- `.docs/motor_agentico_cpn.md` — CPN engine and HITL channel model.
- [A2UI v0.8 specification](https://a2ui.org/specification/v0.8-a2ui/) — ApprovalCard
  component contract.
- Source references:
  - `back/go-assistant/cpn/fire_bash.go:32-58` — current `fire_bash` entry + gate check.
  - `back/go-assistant/infra/host/gate/hitl.go:51-97` — `HandleRequiresHITL`.
  - `back/go-assistant/infra/host/gate/hitl.go:121-129` — `rememberApproval`.
  - `back/go-assistant/infra/host/gate/policy_gate.go:249-252` — pre-emit safe-match.
  - `back/go-assistant/infra/host/os_adapter.go:106-168` — `Exec` and PATH handling.
  - `back/go-assistant/internal/driving/httpapi/handler_hitl.go:39-52` — HTTP
    ingress.
  - `back/go-assistant/cpn/system_tools.go:32` — tool-schema prompt (REQ-008).
  - `back/go-assistant/Dockerfile:25` — runtime base image.
  - `front/react-assistant/src/features/chat/MessageBubble.tsx:137-145` — extended
    action encoding.
