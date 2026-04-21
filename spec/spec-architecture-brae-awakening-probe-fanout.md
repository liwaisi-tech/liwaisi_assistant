---
title: Brae Awakening — Probe-Fanout via Dynamically-Built Sub-CPN
version: 0.1
date_created: 2026-04-21
last_updated: 2026-04-21
owner: liwaisi-tech / brae agent team
tags: [architecture, agent, awakening, cpn, subnet, fanout, parallel, jit]
changelog:
  - "0.1 (2026-04-21): initial spec. Splits awakening into plan → fanout → reduce; introduces a minimal probe-fanout composer that builds an N-branch parallel Sub-CPN at runtime, bypassing the full JIT builder pipeline for v0."
---

# Introduction

This specification defines **probe-fanout**: a runtime-built sub-CPN that executes N parallel shell probes during the `brae-awakens` flow, one per candidate binary emitted by the awakening LLM. The goal is to stop depending on a single-shot LLM to be "thorough enough" to classify every binary it mentions, by replacing LLM self-probing with deterministic parallel host probes. The sub-CPN is **composed and materialised in-process** via the existing `NodeKindInstantiate` + `NodeKindSubNet` primitives; no new node kind is introduced. This spec is a concrete, minimal-surface v0 precursor to the full JIT CPN Builder defined in `spec-architecture-brae-jit-cpn-builder.md`.

## 1. Purpose & Scope

### Purpose

- Move binary verification from "LLM claims" to "host executes" while keeping the LLM in charge of enumeration and classification.
- Use a runtime-composed sub-CPN (not a hand-wired static topology) so the fanout width is driven by the LLM's plan, not by a compile-time constant.
- Prove the dynamic-topology pattern end-to-end in production against a constrained, well-understood input (a probe list), before generalising to the full JIT CPN Builder.
- Eliminate the failure mode observed 2026-04-21 where gemini-2.5-flash produced an `AwakeningReport` with 12 binaries but only 2 actual `raw_probes` executed.

### In scope

- A new package `cpn/awakens/fanout/` containing the composer (`composer.go`), the per-probe template (`template.go`), and the reducer wiring (`reduce.go`).
- Two new transitions in `cpn/awakens/topology.go`: `t-awaken-probe-compose` (NodeKindTool) and `t-awaken-probe-instantiate` (NodeKindInstantiate).
- Replacement of the single-turn `t-awaken-llm-bootstrap → t-awaken-report` path with `t-awaken-llm-plan → t-awaken-probe-compose → t-awaken-probe-instantiate → sub-CPN → t-awaken-report`.
- A new `AwakeningProbePlan` message type: the LLM emits this instead of a full `AwakeningReport` on the first turn.
- A host-gate policy reuse: every fanout branch runs through the same introspection gate the current awakening shell uses.
- Coverage in `cpn/awakens/fanout/` at ≥85% lines, including partial-failure and fanout-cap tests.

### Out of scope

- The generic JIT CPN Builder in `cpn/synthesis/jit/` — deferred to `spec-architecture-brae-jit-cpn-builder.md`. This spec ships a **minimal, awakening-specific** composer that does not go through the `cpn/synthesis` lint/canonicalise pipeline; that's what v0 buys us.
- The toolbox/hashtag extension defined in `spec-architecture-brae-awakening-toolbox-extension.md`. Probe-fanout is orthogonal and ships first.
- Non-POSIX probe types (HTTP probes, DNS probes, port scans). v0 only runs `sh -c "<introspection command>"`.
- Re-probing on subsequent turns. The fanout fires exactly once per session at awakening time; later turns read the persisted snapshot.

### Intended audience

CPN engine engineers, awakening-prompt maintainers, backend engineers implementing `cpn/awakens/`.

### Assumptions

- `brae-awakens` topology is wired as of commit `6508521` and the ToolHandler deposit-contract fix landed 2026-04-21.
- `NodeKindInstantiate` (`cpn/fire_instantiate.go:25`) and `NodeKindSubNet` (`cpn/subnet.go:24`) are production-stable.
- `HostAdapter.Exec` enforces per-call timeouts and returns structured `ExecResult` with `ExitCode`, `Stdout`, `Stderr`.
- The awakening LLM (whatever model is configured for the `awakening` role) can reliably emit a strict-JSON `AwakeningProbePlan` when the system prompt asks for one.

## 2. Definitions

| Term | Definition |
|---|---|
| **Probe** | A single `sh -c "<cmd>"` invocation executed on the host via `HostAdapter.Exec`, returning exit code + stdout + stderr. |
| **Probe-fanout** | The act of running N probes in parallel as independent branches of a sub-CPN. |
| **AwakeningProbePlan** | The first-turn LLM output. Carries the list of probes to run plus minimal context (target OS hint, probing intent). No classification. |
| **AwakeningProbeResult** | The per-branch output token: `{probe_id, command, exit_code, stdout, stderr, duration_ms, truncated}`. |
| **Probe aggregator** | The reducer transition that consumes N `AwakeningProbeResult` tokens and emits a single `AwakeningReport` token. |
| **Fanout cap** | The hard maximum of probes per sub-CPN (CON-002). |
| **Partial failure** | A sub-CPN run in which ≥1 branch timed out or errored but ≥1 branch succeeded. Tolerated; surfaces in the report. |
| **Total failure** | A sub-CPN run in which every branch timed out or errored. Surfaces as a CPN-level error to the caller. |

## 3. Requirements, Constraints & Guidelines

### Functional requirements

- **REQ-001**: The awakening LLM MUST emit a JSON object conforming to `AwakeningProbePlan` on the first turn. The existing `RequireJSON` setting on `t-awaken-llm-plan` SHALL enforce shape; the probe list is validated structurally before fanout.
- **REQ-002**: The `Compose` function in `cpn/awakens/fanout/composer.go` MUST accept an `AwakeningProbePlan` and return a `*cpn.CPN` with:
  - One input place `p-probe-plan` (ColorArtifact) seeded with the plan.
  - One input place `p-probe-trigger` (ColorString) seeded with `"go"`.
  - N NodeKindTool transitions named `t-probe-<probe_id>`, each consuming the trigger and depositing an `AwakeningProbeResult` on its own output place `p-probe-result-<probe_id>`.
  - One NodeKindTool transition `t-probe-reduce` that consumes all N result places (AND-join) and deposits a single `AwakeningReport` on `p-probe-report`.
  - A single terminal place `p-probe-egress` that mirrors `p-probe-report` for SubNet egress semantics.
- **REQ-003**: Each per-probe transition's Executor MUST route through `HostAdapter.Exec` with the same `HostGate` the current awakening shell uses (`cpn/awakens/topology.go:281`). Gate denial MUST deposit a `ColorError` token on the probe's result place, not abort the whole fanout.
- **REQ-004**: The per-probe timeout MUST be `min(2s, plan.TimeoutPerProbeMs/1000)`. A probe that exceeds it MUST deposit an `AwakeningProbeResult` with `ExitCode = -1`, `Stderr = "timeout"`, and not block the aggregator.
- **REQ-005**: The aggregator `t-probe-reduce` MUST wait for exactly N result tokens, then assemble an `AwakeningReport` populating `binaries[]` (from probe results where the probe is classified as a binary-presence check) and `capabilities[]` (from probe results classified as capability checks). The classification is carried in the plan entry itself — probes self-describe their intent.
- **REQ-006**: The parent awakening topology MUST spawn the fanout sub-CPN via `NodeKindInstantiate` with `InstantiateConfig.SkipHITL = true`. Rationale: the topology is auto-generated per-session from a validated plan, no user approval is possible at awakening time.
- **REQ-007**: On fanout total-failure (every branch errored), `runAwakening` in `internal/app/session_service_awakening.go` MUST propagate the error to the caller. There is no fallback path (per the bootstrap-fix spec).
- **REQ-008**: On fanout partial-failure, the `AwakeningReport` MUST still be emitted with `binaries[].present = false` for errored probes and a `report.notes[]` entry enumerating the failed probe IDs. Downstream persist/register/emit transitions MUST accept this and proceed.
- **REQ-009**: The system prompt for `t-awaken-llm-plan` MUST instruct the model to enumerate all binaries it wants checked in `plan.probes[]` without attempting to classify present/absent. The prompt MUST include one worked example.
- **REQ-010**: Observability: the composer MUST emit canonical events `awakening.probe.composed`, `awakening.probe.fired`, `awakening.probe.reduced` (naming per `spec-architecture-brae-toolbox-taxonomy.md` `<domain>.<subject>.<verb>`). Each event carries `session_id`, `probe_count`, and (for `.fired`) `exit_code` + `duration_ms`.

### Non-functional requirements

- **NFR-001 (latency)**: Total wall-clock time for a 12-probe fanout MUST complete within `max(per_probe_timeout) + 500ms` at p95, measured end-to-end from `t-awaken-probe-instantiate` firing to `t-probe-reduce` output. The +500ms budget covers composition, materialisation, goroutine scheduling, and aggregation.
- **NFR-002 (determinism)**: Given the same `AwakeningProbePlan`, `Compose` MUST produce a structurally identical `*cpn.CPN` (same place IDs, transition IDs, arc lists). Map-iteration order MUST NOT leak into IDs.
- **NFR-003 (observability)**: Every branch's start + end MUST appear in the `events` table tagged with the probe ID so partial failures are diagnosable from a single SQL query.

### Security requirements

- **SEC-001**: Every probe command MUST pass through the introspection `HostGate`. A probe whose command is not in the introspection allowlist MUST be rejected by the gate, deposited as `ColorError`, and logged at `WARN`.
- **SEC-002**: The LLM's probe plan is untrusted input. `Compose` MUST reject any plan whose probe count exceeds `MaxProbes` (CON-002) or whose command field contains a null byte, newline outside a quoted segment, or exceeds `MaxCommandLen` (CON-003). Rejection MUST return an error, not silently truncate.
- **SEC-003**: The fanout sub-CPN MUST inherit the parent's `SessionID` for token-ledger accounting. It MUST NOT be able to escalate host-gate policy class beyond "introspection".

### Constraints

- **CON-001 (Axiom A13)**: `cpn/awakens/fanout/` MUST NOT import `persist/` or `store/`. All persistence happens in the existing `t-awaken-persist` transition downstream of the fanout.
- **CON-002 (fanout cap)**: `MaxProbes = 16`. Plans exceeding the cap are rejected at compose time.
- **CON-003 (command size cap)**: `MaxCommandLen = 256` bytes per probe command.
- **CON-004 (no recursion)**: The fanout sub-CPN MUST NOT contain any `NodeKindInstantiate` or `NodeKindSubNet` transitions. Composer SHALL assert this before returning.
- **CON-005 (idempotence at topology level)**: Re-invoking `Compose` with the same plan on the same session MUST return a fresh `*cpn.CPN` instance (no cross-session sharing). Caching is deferred to the full JIT builder.
- **CON-006 (no persist.CPNTopology)**: Unlike the full JIT builder, probe-fanout does NOT round-trip through `persist.CPNTopology` JSON. It constructs a `*cpn.CPN` directly via the public `cpn.NewCPN`, `cpn.NewPlace`, `cpn.NewTransition` constructors. This is the minimal-surface choice for v0 and is the reason this spec can ship ahead of the full JIT spec.

### Guidelines

- **GUD-001**: Favour concrete `AwakeningProbeEntry` struct fields over free-form JSON maps in the `AwakeningProbePlan` message. The LLM produces better JSON against a strict struct schema.
- **GUD-002**: Probe IDs SHOULD be short, stable slugs derived from the command (e.g. `cmd-sh`, `cmd-busybox`). The composer SHALL deterministically slugify non-conformant IDs.
- **GUD-003**: When writing probe executors, prefer `HostAdapter.Exec` with `AllowNonZero = true` — a non-zero exit from `command -v foo` is a valid "not present" signal, not an error.

### Patterns

- **PAT-001 (AND-join reducer)**: The aggregator transition uses the existing multi-in / single-out ToolHandler pattern (`fireToolHandler` in `cpn/executor.go:425`). N input places, 1 output place, handler assembles the report.
- **PAT-002 (SubNet egress single terminal)**: The fanout sub-CPN has exactly one terminal place (`p-probe-egress`) whose token is the SubNet's return value to the parent — matches REQ-014 of the full JIT spec even though v0 doesn't use that codepath.

## 4. Interfaces & Data Contracts

### 4.1 AwakeningProbePlan (LLM output, Go struct)

```go
// AwakeningProbePlan is the first-turn output of the awakening LLM. It
// replaces the single-shot AwakeningReport on turn 1. The final report is
// assembled by the probe-aggregator transition after all probes complete.
type AwakeningProbePlan struct {
    // Reason the LLM chose these probes; one short sentence. Included in
    // the persisted snapshot's raw_probes metadata for observability.
    Rationale string `json:"rationale"`

    // TimeoutPerProbeMs bounds each probe. Composer clamps to [100, 2000].
    TimeoutPerProbeMs int `json:"timeout_per_probe_ms"`

    // Probes is the enumeration. Max 16 entries (CON-002).
    Probes []AwakeningProbeEntry `json:"probes"`
}

type AwakeningProbeEntry struct {
    // ID: short stable slug. If empty or non-conformant, composer derives
    // one from Command (GUD-002).
    ID string `json:"id"`

    // Kind: "binary" or "capability". Drives which field of the final
    // AwakeningReport receives the result.
    Kind string `json:"kind"`

    // Target: the binary name or capability name the probe verifies.
    // Used as the key into binaries[] / capabilities[].
    Target string `json:"target"`

    // Command: the full POSIX command passed to sh -c. Must pass the
    // introspection gate. Max 256 bytes (CON-003).
    Command string `json:"command"`
}
```

### 4.2 AwakeningProbeResult (internal token payload)

```go
type AwakeningProbeResult struct {
    ProbeID    string `json:"probe_id"`
    Kind       string `json:"kind"`
    Target     string `json:"target"`
    Command    string `json:"command"`
    ExitCode   int    `json:"exit_code"` // -1 reserved for timeout
    Stdout     string `json:"stdout"`
    Stderr     string `json:"stderr"`
    DurationMs int64  `json:"duration_ms"`
    Truncated  bool   `json:"truncated"`
    GateDenied bool   `json:"gate_denied"`
}
```

### 4.3 Composer API

```go
package fanout

// Compose builds an N-branch parallel sub-CPN from a validated plan.
// The returned *cpn.CPN is seeded and ready to Run; the caller wires
// HostAdapter/HostGate on the returned root before Run.
func Compose(sessionID string, plan AwakeningProbePlan, deps Deps) (*cpn.CPN, error)

// Deps mirrors cpn/awakens.Deps but only with fields used during fanout.
type Deps struct {
    HostAdapter cpn.HostAdapter
    HostGate    cpn.HostGate
    Clock       func() time.Time
}

// ValidatePlan checks CON-002, CON-003, SEC-002. Exported for re-use by
// the planner's LLM-response validator.
func ValidatePlan(plan AwakeningProbePlan) error
```

### 4.4 Topology wire-up (parent `brae-awakens`)

```
p-awaken-trigger ──┐
                   ├──▶ t-awaken-llm-plan ──▶ p-awaken-plan ──┐
p-awaken-system-prompt ──┘    (NodeKindLLM)                   │
                                                              ▼
                                          t-awaken-probe-compose (NodeKindTool)
                                          │ Compose(plan) -> *cpn.CPN blob
                                          ▼
                                          p-awaken-subnet-spec (ColorArtifact)
                                          │
                                          ▼
                                          t-awaken-probe-instantiate (NodeKindInstantiate, SkipHITL)
                                          │ spawns fanout sub-CPN
                                          ▼
                                          p-awakening-report (ColorArtifact)
                                          │
                                          ▼
                                          t-awaken-report → persist/register/emit (existing)
```

### 4.5 Events emitted

| Event | When | Fields |
|---|---|---|
| `awakening.probe.composed` | `Compose` returns | session_id, probe_count, plan_digest |
| `awakening.probe.fired` | Each branch executor ends | session_id, probe_id, kind, target, exit_code, duration_ms, gate_denied |
| `awakening.probe.reduced` | Reducer deposits report | session_id, probe_count, ok_count, fail_count, report_size_bytes |

## 5. Acceptance Criteria

- **AC-001**: Given an awakening session and an LLM producing a plan of 12 binary probes, When the fanout sub-CPN runs, Then `raw_probes` in the persisted snapshot MUST contain 12 entries, one per planned probe, and `binaries[].present` MUST agree with the probe's exit code (0 → present, non-zero → absent, -1 → absent with `notes` mentioning timeout).
- **AC-002**: Given an LLM plan with 20 probes (exceeds `MaxProbes`), When `ValidatePlan` runs, Then it MUST return an error of kind `ErrTooManyProbes` and `Compose` MUST NOT be called.
- **AC-003**: Given a plan containing a probe with a command outside the introspection allowlist (e.g. `rm -rf /`), When the sub-CPN fires, Then the gate MUST deny the probe, the branch MUST deposit a `ColorError` result with `GateDenied = true`, and the other branches MUST still run to completion.
- **AC-004**: Given a plan with 6 probes where 2 time out, When the sub-CPN completes, Then the reducer MUST emit an `AwakeningReport` containing all 6 classification entries, with the 2 timeouts reported as `present: false` and `report.notes[]` listing their probe IDs.
- **AC-005**: Given any valid plan, When `Compose` is called twice with the same session ID and plan, Then both returned `*cpn.CPN` instances MUST have identical place IDs, transition IDs, and arc lists (NFR-002).
- **AC-006**: Given a plan containing any probe whose command exceeds 256 bytes, When `ValidatePlan` runs, Then it MUST return `ErrCommandTooLong` referencing the offending probe ID (SEC-002, CON-003).
- **AC-007**: The composed sub-CPN MUST contain zero transitions of kind `NodeKindInstantiate` or `NodeKindSubNet` (CON-004). A structural self-check in `Compose` MUST assert this before returning.
- **AC-008**: End-to-end: Given a fresh Alpine/busybox container and awakening configured with Claude Haiku 4.5 as the `awakening` role, When a new session is created, Then after `awakening: complete` logs, the `tools` table MUST have ≥1 row with `origin='awakening'` AND `host_capability_snapshots.raw_probes` MUST have ≥8 entries.

## 6. Test Automation Strategy

- **Test levels**: Unit (composer, plan validator, reducer handler), integration (whole fanout sub-CPN with fake `HostAdapter`), end-to-end (real `docker compose` run against a seeded Alpine container, asserted via `docker compose exec postgres psql`).
- **Framework**: Go `testing` package + `-race`. Table-driven tests for `ValidatePlan`. Deterministic `fakeHostAdapter` that returns scripted `ExecResult` keyed by command.
- **Test data management**: Golden `AwakeningProbePlan` JSON fixtures under `cpn/awakens/fanout/testdata/`. One per scenario: all-success, one-timeout, one-gate-denied, all-fail, cap-exceeded, command-too-long.
- **CI/CD integration**: `go test -race ./cpn/awakens/...` on every PR. `go test -race -run TestProbeFanout_EndToEnd ./cpn/awakens/fanout/...` gated behind a `-tags=docker` build tag for the docker-bound test.
- **Coverage requirement**: ≥85% line coverage in `cpn/awakens/fanout/`. No uncovered error branches in `Compose` or `ValidatePlan`.
- **Performance testing**: Benchmark `BenchmarkCompose_12Probes` must complete Compose in <1 ms on the CI runner (no I/O). Integration benchmark for end-to-end fanout of 12 probes against a `fakeHostAdapter` sleeping 100ms per probe must finish under 500ms (demonstrates parallelism).
- **Regression guards**: A test asserting `len(c.Transitions)` of the composed CPN equals `len(plan.Probes) + 1` (the +1 is the reducer). A test asserting `Compose(plan)` twice returns CPNs with identical transition keys sorted.

## 7. Rationale & Context

**Why a minimal awakening-specific composer instead of using the full JIT CPN Builder?** The full builder (spec 0.2) requires `cpn/synthesis/jit/` with `Lint`, `Canonicalise`, `Materialise`, a digest cache, adversarial-lint fixtures, and personality-digest plumbing. That is ~2 weeks of work and solves a more general problem (per-turn tool-grounded topologies from a `ToolMatchSet`). Probe-fanout solves **one** concrete problem with a fixed-shape topology, a validated plan schema, and zero generalisation burden. It ships in days, proves the dynamic-CPN pattern under production load, and leaves the full JIT builder untouched for when the tool-retriever lands.

**Why fix-fanout instead of chain-of-probes in-LLM?** Fix-fanout with host probes is **deterministic and cheap**. Asking gemini-2.5-flash or even Haiku to iterate over 12 binaries and call a tool 12 times costs tokens, latency, and (as observed 2026-04-21) correctness — the model skipped 10 of 12. Deterministic probes cost one `sh -c` per binary and always produce the right classification.

**Why `SkipHITL = true`?** Awakening runs before the user has rendered the session UI. There is no human in the loop to approve a topology. The topology is auto-generated from a validated plan whose inputs are already bounded by `ValidatePlan` — SEC-002, CON-002, CON-003 carry the safety load that HITL would have carried. The host-gate still enforces command-level policy on every branch.

**Why cap at 16?** An Alpine container typically needs 10–14 probes (sh, bash, busybox, python, python3, git, curl, wget, ls, cat, head, tail, awk, sed, grep, tar). 16 covers everything plausible while keeping goroutine fanout trivially bounded. Increasing the cap later is a one-constant change.

**Why no persistence through `persist.CPNTopology`?** The fanout CPN is ephemeral — it exists only for the duration of the awakening flow. Persisting its shape would cost migration surface, lint round-trips, and schema-validation cost for zero replay value. The persisted artifact is the resulting `host_capability_snapshots` row, not the topology.

## 8. Dependencies & External Integrations

### External systems

- **EXT-001**: OpenRouter (or direct Anthropic/Google API) — inherits from existing awakening config. No new integration.

### Infrastructure dependencies

- **INF-001**: `HostAdapter` implementation that can run `sh -c` on the target host. Currently the in-process Docker-backed adapter in `cpn/host.go`.

### Data dependencies

- **DAT-001**: `host_capability_snapshots` table — receives the aggregated report via the existing persist transition. No schema change.
- **DAT-002**: `tools` table — receives registry entries from the existing `t-awaken-register-tools` transition downstream of the fanout. No schema change.

### Technology platform dependencies

- **PLT-001**: Go 1.22+ — required for the awakens package generally. No new version constraint.

### Compliance dependencies

- **COM-001**: None beyond the existing introspection-gate policy class, which is already audited at `cpn/host_snapshot.go` and `infra/host/gate/matcher.go`.

## 9. Examples & Edge Cases

### 9.1 Happy-path plan (12 probes on Alpine)

```json
{
  "rationale": "Alpine/busybox container — verify core busybox applets plus language runtimes.",
  "timeout_per_probe_ms": 2000,
  "probes": [
    { "id": "cmd-sh",      "kind": "binary", "target": "sh",      "command": "command -v sh" },
    { "id": "cmd-bash",    "kind": "binary", "target": "bash",    "command": "command -v bash" },
    { "id": "cmd-busybox", "kind": "binary", "target": "busybox", "command": "command -v busybox" },
    { "id": "cmd-ls",      "kind": "binary", "target": "ls",      "command": "command -v ls" },
    { "id": "cmd-cat",     "kind": "binary", "target": "cat",     "command": "command -v cat" },
    { "id": "cmd-awk",     "kind": "binary", "target": "awk",     "command": "command -v awk" },
    { "id": "cmd-sed",     "kind": "binary", "target": "sed",     "command": "command -v sed" },
    { "id": "cmd-grep",    "kind": "binary", "target": "grep",    "command": "command -v grep" },
    { "id": "cmd-git",     "kind": "binary", "target": "git",     "command": "command -v git" },
    { "id": "cmd-curl",    "kind": "binary", "target": "curl",    "command": "command -v curl" },
    { "id": "cmd-python3", "kind": "binary", "target": "python3", "command": "command -v python3" },
    { "id": "cmd-tar",     "kind": "binary", "target": "tar",     "command": "command -v tar" }
  ]
}
```

### 9.2 Rejected plan (gate-violating command)

```json
{
  "rationale": "malicious",
  "timeout_per_probe_ms": 1000,
  "probes": [
    { "id": "p1", "kind": "binary", "target": "x", "command": "rm -rf /" }
  ]
}
```

`ValidatePlan` accepts this structurally (shape is valid). The `HostGate` denies at execution time. The branch's result carries `GateDenied = true, ExitCode = -2`, and the final report marks `binaries[x].present = false` with a note `"probe p1 gate-denied"`.

### 9.3 Partial failure (2 of 6 time out)

Given a plan of 6 probes where probes `cmd-slow-a` and `cmd-slow-b` exceed the 2s timeout: the reducer emits a report where those two binaries have `present: false` and `report.notes[] = ["probe cmd-slow-a timeout", "probe cmd-slow-b timeout"]`. Register/persist/emit proceed normally.

### 9.4 Total failure (HostAdapter unreachable)

Given `HostAdapter.Exec` returning `context.DeadlineExceeded` for every branch: every result carries `ExitCode = -1`, the reducer emits a report with all binaries `present: false`, AND `runAwakening` treats this as an error (`awakening: all probes failed, aborting`). The session is marked as awakening-failed; no snapshot is persisted; the first-turn A2UI envelope is not emitted.

## 10. Validation Criteria

A conformant implementation MUST satisfy all of the following:

1. All acceptance criteria AC-001 through AC-008 pass under `go test -race`.
2. `go vet ./...` and `golangci-lint run ./cpn/awakens/...` report zero findings.
3. A grep of `cpn/awakens/fanout/` for `"persist"` or `"store"` imports returns zero matches (CON-001).
4. A grep of the composer output-CPN assembly for `NodeKindInstantiate` or `NodeKindSubNet` returns zero matches (CON-004).
5. End-to-end validation against a live Alpine container produces `tools` rows with `origin='awakening'` and a snapshot whose `raw_probes` count equals the plan's probe count.
6. Benchmarks `BenchmarkCompose_12Probes` and `BenchmarkFanout_12Probes_End2End` meet the budgets in §6.
7. The spec's author and reviewer agree the feature can be enabled by flipping a single config flag (no runtime fork).

## 11. Related Specifications / Further Reading

- `spec/spec-architecture-brae-awakening-self-discovery.md` — original awakening topology.
- `spec/spec-architecture-brae-awakening-topology-bootstrap-fix.md` — the bootstrap deadlock fix (prerequisite).
- `spec/spec-architecture-brae-jit-cpn-builder.md` — the general JIT CPN Builder that this spec is a precursor to.
- `spec/spec-architecture-brae-awakening-toolbox-extension.md` — orthogonal; ships after probe-fanout.
- `cpn/fire_instantiate.go` — the Instantiate primitive reused by this spec.
- `cpn/subnet.go` — the SubNet primitive reused by this spec.
- `cpn/executor.go:425` — `fireToolHandler`, the AND-join reducer pattern.
