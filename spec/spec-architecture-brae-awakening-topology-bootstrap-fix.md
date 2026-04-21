---
title: brae-awakens Topology Bootstrap Fix — Split LLM Transition, Remove Fallback
version: 1.0
date_created: 2026-04-20
last_updated: 2026-04-20
owner: go-assistant / agentic-engine
tags: [architecture, cpn, awakening, topology, backend]
---

# Introduction

The `brae-awakens` CPN topology currently deadlocks on every new session because the `t-awaken-llm` transition declares an AND-join over three input places, one of which (`p-awaken-shell-result`) is never seeded at bootstrap and can only be produced by a downstream transition that itself depends on `t-awaken-llm` having already fired. The executor detects the stall and emits `ErrDeadlock` ("no enabled transitions and no terminal marking"), after which a legacy fallback path runs. This specification mandates the topology-level fix and removes the fallback entirely: first-boot awakening MUST succeed via the LLM-driven path.

## 1. Purpose & Scope

Purpose: eliminate the structural circular dependency in the `brae-awakens` topology so that the LLM-driven awakening path is the sole, reliable mechanism for producing the initial `AwakeningReport` and the first A2UI assistant message on new sessions.

Scope:
- `back/go-assistant/cpn/awakens/topology.go` — split `t-awaken-llm` into two NodeKindLLM transitions with disjoint input sets.
- `back/go-assistant/cpn/awakens/prompt.go` — confirm the system prompt is valid for both entry points; add a followup-only hint if needed.
- `back/go-assistant/internal/app/session_service_awakening.go` — remove `runAwakeningFallback` and all fallback branching; propagate the LLM-path error to the caller unchanged.
- Unit and integration tests in `back/go-assistant/cpn/awakens/` and `back/go-assistant/internal/app/`.

Out of scope: frontend, CPN executor engine, HostAdapter, A2UI rendering, other topologies.

Audience: backend engineers maintaining the agentic CPN engine.

Assumptions: the LLM provider (OpenRouter) is reachable at session creation. Offline or provider-down scenarios are handled by the transport layer returning an error to the caller; they do NOT trigger a fallback awakening.

## 2. Definitions

- **CPN**: Colored Petri Net — the execution model used by go-assistant.
- **Place**: a typed container holding tokens; edges connect places to transitions.
- **Transition**: a node that consumes tokens from input places and produces tokens on output places when enabled.
- **Marking**: the assignment of tokens to places at a given instant.
- **Terminal place**: a place with no outgoing arcs; its presence of a token contributes to the net's completion condition.
- **Terminal marking**: a marking where every terminal place holds at least one token.
- **AND-join**: transition semantics requiring tokens on ALL input places to fire.
- **Awakening**: the first-boot self-discovery sequence that produces a `HostCapabilitySnapshot` and the first assistant A2UI envelope.
- **Bootstrap turn**: the first LLM call of the awakening sequence (no prior shell result exists).
- **Followup turn**: any subsequent LLM call triggered after a shell probe result returns.
- **A2UI**: Agent-to-UI protocol v0.8 (https://a2ui.org/specification/v0.8-a2ui/).

## 3. Requirements, Constraints & Guidelines

- **REQ-001**: The `brae-awakens` topology MUST reach a terminal marking when the LLM returns a valid `AwakeningReport` in either zero or more than zero shell round trips.
- **REQ-002**: The topology MUST NOT declare any transition whose enablement depends on a place that cannot be produced transitively from the initial marking.
- **REQ-003**: `t-awaken-llm` MUST be replaced by exactly two NodeKindLLM transitions:
  - **t-awaken-llm-bootstrap**: inputs `[p-awaken-trigger, p-awaken-system-prompt]`; outputs `[p-awaken-shell-call, p-awakening-report]` (LLM produces exactly one).
  - **t-awaken-llm-followup**: inputs `[p-awaken-shell-result]`; outputs `[p-awaken-shell-call, p-awakening-report]` (LLM produces exactly one).
- **REQ-004**: `t-awaken-llm-bootstrap` MUST fire at most once per session.
- **REQ-005**: `t-awaken-llm-followup` MUST fire once per shell result token produced by `t-awaken-shell`.
- **REQ-006**: Both LLM transitions MUST declare `LLMTools = [TransitionAwakenShell]` so the model retains its single permitted tool.
- **REQ-007**: The `SystemPrompt` used by both transitions MUST be identical OR differ only by a followup-only hint ("Prior shell result attached.") appended when the runtime renders the followup prompt.
- **REQ-008**: `session_service_awakening.go` MUST remove the `runAwakeningFallback` function and every call site, including any `source="awakening-fallback"` persistence branch.
- **REQ-009**: When `root.Run(ctx)` returns a non-nil error, `session_service_awakening.go` MUST propagate the error to the caller; the session creation MUST fail rather than fall back.
- **REQ-010**: Seeded initial marking for the topology MUST remain `{p-awaken-trigger: 1, p-awaken-system-prompt: 1}`. No sentinel token on `p-awaken-shell-result`.
- **CON-001**: No changes to CPN executor semantics (`cpn/executor.go`, `cpn/cpn.go`) are permitted.
- **CON-002**: No changes to the frontend (`front/react-assistant/**`).
- **CON-003**: No new place IDs are introduced; the set `{p-awaken-trigger, p-awaken-system-prompt, p-awaken-shell-call, p-awaken-shell-result, p-awakening-report, p-awakening-snapshot, p-awakening-toolbatch, p-awakening-toolbatch-done, p-awakening-message, p-awakening-message-emitted, p-host-capabilities}` is unchanged.
- **CON-004**: Transition IDs `t-awaken-llm-bootstrap` and `t-awaken-llm-followup` are stable identifiers and MUST be exported as Go constants alongside existing `TransitionAwaken*` names.
- **CON-005**: The removal of `runAwakeningFallback` MUST NOT leave dead imports, unused symbols, or orphaned log lines.
- **GUD-001**: Prefer symmetric outputs (both transitions emit to the same downstream places) to keep `t-awaken-report` and siblings unchanged.
- **GUD-002**: Keep the followup prompt minimal — the shell result token itself is the payload; the system prompt need not be restated.
- **PAT-001**: Model "first-turn vs loop-turn" differences as **distinct transitions**, not as guards on a single transition or as sentinel tokens. This matches CPN idiom and keeps the net self-documenting.

## 4. Interfaces & Data Contracts

### 4.1 Topology edges (before → after)

| Transition | Inputs (before) | Outputs (before) | Inputs (after) | Outputs (after) |
|---|---|---|---|---|
| `t-awaken-llm` | trigger, system-prompt, shell-result | shell-call \| report | REMOVED | REMOVED |
| `t-awaken-llm-bootstrap` | — | — | trigger, system-prompt | shell-call \| report |
| `t-awaken-llm-followup` | — | — | shell-result | shell-call \| report |
| `t-awaken-shell` | shell-call | shell-result | unchanged | unchanged |
| `t-awaken-report` | report | snapshot, toolbatch, message | unchanged | unchanged |
| `t-awaken-persist` | snapshot | host-capabilities | unchanged | unchanged |
| `t-awaken-register-tools` | toolbatch | toolbatch-done | unchanged | unchanged |
| `t-awaken-emit-message` | message | message-emitted | unchanged | unchanged |

### 4.2 Go constants (additions)

```go
const (
    TransitionAwakenLLMBootstrap = "t-awaken-llm-bootstrap"
    TransitionAwakenLLMFollowup  = "t-awaken-llm-followup"
)
```

The existing `TransitionAwakenLLM` constant MUST be removed (not aliased) to prevent accidental reuse.

### 4.3 session_service_awakening.go contract

```go
// Before
capabilities, envelope, err := runAwakeningLLM(ctx)
if err != nil {
    log.Warn("awakening: LLM path failed; falling through to legacy discovery", "error", err)
    capabilities, envelope, err = runAwakeningFallback(ctx)
}

// After
capabilities, envelope, err := runAwakeningLLM(ctx)
if err != nil {
    return nil, fmt.Errorf("awakening: %w", err)
}
```

## 5. Acceptance Criteria

- **AC-001**: Given a fresh session and a reachable LLM, When session creation triggers `brae-awakens`, Then the topology reaches a terminal marking and emits exactly one A2UI envelope with `cpnRole="awakening"`.
- **AC-002**: Given the LLM returns a valid `AwakeningReport` on the first call (no shell probe), When the bootstrap transition fires, Then the net completes without `t-awaken-llm-followup` firing.
- **AC-003**: Given the LLM returns one shell call followed by a valid report, When `t-awaken-shell` produces `p-awaken-shell-result`, Then `t-awaken-llm-followup` fires and the net completes.
- **AC-004**: Given the LLM is unreachable, When the bootstrap transition fires, Then `root.Run` returns an error and session creation fails with a propagated error — no fallback runs.
- **AC-005**: The log line `"awakening: LLM path failed; falling through to legacy discovery"` MUST NOT appear anywhere in the codebase.
- **AC-006**: The function `runAwakeningFallback` MUST NOT exist in the codebase.
- **AC-007**: `grep -r "awakening-fallback"` returns zero hits in `back/go-assistant/`.
- **AC-008**: Unit tests for the topology cover: (a) zero-probe happy path, (b) one-probe happy path, (c) LLM error on bootstrap, (d) LLM error on followup, (e) invalid report triggers existing retry logic in `t-awaken-report` (unchanged).
- **AC-009**: Integration test: `POST /api/v1/sessions` with a stubbed LLM returning a valid report produces `201 Created` and the SSE stream delivers an A2UI awakening envelope.

## 6. Test Automation Strategy

- **Test Levels**: Unit (topology assembly, transition enablement), Integration (session service end-to-end with stubbed LLM), contract (`AwakeningReport` validation unchanged).
- **Frameworks**: Go standard `testing` package; `testify/assert` where already used; existing fake LLM harness in `back/go-assistant/cpn/awakens/` test helpers.
- **Test Data Management**: Deterministic fake `AwakeningReport` fixtures (os=linux, shell=/bin/sh, arch=amd64). Table-driven tests for zero/one/many probe loops.
- **CI/CD Integration**: Existing `make test` target in `back/go-assistant/Makefile` must pass. No new CI jobs required.
- **Coverage Requirements**: ≥ 85% line coverage in `cpn/awakens/` package. `session_service_awakening.go` net coverage MUST NOT regress.
- **Performance Testing**: Not applicable — this is a correctness fix; expected latency is bounded by the LLM call, unchanged from current primary path.

## 7. Rationale & Context

The original `t-awaken-llm` transition attempted to unify the "first call" and "loop call" semantics into a single CPN node by listing `p-awaken-shell-result` as an input. This compiles and passes static inspection but creates a latent deadlock: AND-join semantics require a token on every input place, and `p-awaken-shell-result` has no source in the initial marking. The net reaches a state with no enabled transitions and no terminal marking on turn zero — the exact deadlock the user observes in production logs.

Commit `077238c` added a deterministic fallback probe as mitigation but explicitly deferred the topology fix. Leaving the fallback in place permanently has two costs: (1) the LLM-driven introspection, which produces richer `HostCapabilitySnapshot` data and a crafted A2UI envelope, never runs — users see the generic fallback card on every session; (2) two code paths means two behaviors to reason about, test, and document.

Splitting the transition is the smallest change consistent with CPN idiom. Alternative designs considered and rejected:

- **Sentinel token on `p-awaken-shell-result`**: violates the color-type contract (a sentinel is not a real shell result), leaks conditional logic into the LLM prompt builder, and requires tokens to be filtered downstream. Rejected.
- **Arc guard / optional input**: the executor does not implement optional input arcs; adding them would be an engine change, violating CON-001. Rejected.
- **Single transition with weighted arcs**: same executor-change problem. Rejected.

Removing the fallback is a policy choice requested by the product owner: first-boot awakening is treated as a hard requirement; if the LLM is unreachable, the session creation fails fast and the user retries rather than operating on a degraded snapshot.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: OpenRouter (LLM provider) — single upstream for bootstrap and followup LLM calls. No new integration; behavior unchanged.

### Third-Party Services
- **SVC-001**: Gemini 3 Flash (default model via OpenRouter) with Haiku 4.5 404-retry fallback — unchanged from commit `45bc480`.

### Infrastructure Dependencies
- **INF-001**: CPN executor (`cpn/executor.go`) — consumed as-is; no changes required.

### Data Dependencies
- **DAT-001**: `host_capability_snapshots` table — existing schema; `source` column will only ever be written with `"awakening"` after this change.

### Technology Platform Dependencies
- **PLT-001**: Go 1.22+ (current project toolchain) — no change.

### Compliance Dependencies
- **COM-001**: Host-gate security policy (`spec-architecture-host-gate-security-policy.md`) — shell calls from both LLM transitions continue to pass through the existing gate.

## 9. Examples & Edge Cases

### 9.1 Zero-probe happy path

```
initial marking:
  p-awaken-trigger: [token{}]
  p-awaken-system-prompt: [token{prompt:"..."}]

t-awaken-llm-bootstrap fires
  consumes: trigger, system-prompt
  LLM returns: {"type":"report", "payload": <valid AwakeningReport JSON>}
  produces: p-awakening-report: [token{report:...}]

t-awaken-report fires → snapshot, toolbatch, message
t-awaken-persist, t-awaken-register-tools, t-awaken-emit-message fire
terminal marking reached: {p-host-capabilities, p-awakening-toolbatch-done, p-awakening-message-emitted}
```

### 9.2 One-probe path

```
t-awaken-llm-bootstrap fires → produces p-awaken-shell-call: [token{cmd:"uname -a"}]
t-awaken-shell fires          → produces p-awaken-shell-result: [token{stdout:"Linux ..."}]
t-awaken-llm-followup fires   → produces p-awakening-report: [token{report:...}]
(downstream chain as in 9.1)
```

### 9.3 LLM error on bootstrap

```
t-awaken-llm-bootstrap fires → LLM returns error
executor aborts root.Run with the wrapped error
session_service_awakening returns the error to the HTTP handler
POST /api/v1/sessions responds 5xx; the client retries. No fallback path executes.
```

### 9.4 Go sketch (topology.go)

```go
root.AddTransition(Transition{
    ID:            TransitionAwakenLLMBootstrap,
    Kind:          NodeKindLLM,
    InputPlaces:   []string{PlaceAwakenTrigger, PlaceAwakenSystemPrompt},
    OutputPlaces:  []string{PlaceAwakenShellCall, PlaceAwakeningReport},
    SystemPrompt:  awakenSystemPrompt,
    LLMTools:      []string{TransitionAwakenShell},
})

root.AddTransition(Transition{
    ID:            TransitionAwakenLLMFollowup,
    Kind:          NodeKindLLM,
    InputPlaces:   []string{PlaceAwakenShellResult},
    OutputPlaces:  []string{PlaceAwakenShellCall, PlaceAwakeningReport},
    SystemPrompt:  awakenSystemPrompt, // optionally appended with followup hint at render time
    LLMTools:      []string{TransitionAwakenShell},
})
```

## 10. Validation Criteria

- `go test ./cpn/awakens/... ./internal/app/...` passes with ≥ 85% coverage for `cpn/awakens/`.
- `grep -rn "runAwakeningFallback\|awakening-fallback\|falling through to legacy discovery" back/go-assistant/` returns no matches.
- Manual smoke test: `POST /api/v1/sessions` with live OpenRouter credentials produces a session whose first assistant message is an A2UI awakening envelope with `cpnRole="awakening"` and a populated `HostCapabilitySnapshot`.
- Running the backend with an invalid `OPENROUTER_API_KEY` causes session creation to return an error (no stub awakening card appears in the UI).

## 11. Related Specifications / Further Reading

- `spec/spec-architecture-brae-awakening-self-discovery.md` — parent spec for the awakening feature; this document supersedes its fallback provisions (REQ-010 of that spec).
- `spec/spec-architecture-host-adapter-nodekind-bash.md` — shell tool contract used by `t-awaken-shell`.
- `spec/spec-architecture-host-gate-security-policy.md` — command authorization gate.
- `spec/spec-architecture-a2a-a2ui-protocol-integration.md` — A2UI v0.8 envelope contract.
- `.docs/motor_agentico_cpn.md` — CPN engine foundations.
- A2UI v0.8 specification — https://a2ui.org/specification/v0.8-a2ui/
