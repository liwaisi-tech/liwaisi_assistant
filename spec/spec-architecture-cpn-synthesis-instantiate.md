---
title: CPN Synthesizer — NodeKindSynthesize + NodeKindInstantiate for Runtime Topology Authoring
version: 1.0
date_created: 2026-04-17
last_updated: 2026-04-17
owner: liwaisi
tags: [architecture, cpn, synthesis, meta, flow-repository, brae, gap-4]
---

# Introduction

`SubNetFactory` today is a `func() *CPN` closure sealed in Go at server boot. A CPN cannot *author* a new topology at runtime: there is no way for an LLM transition to emit "here is a 5-place, 4-transition net that solves this class of problem" and have that net be validated, persisted, and immediately instantiable as a sub-CPN.

The §8 of the source paper calls this out as open research: "high-level reconfigurable networks to enable agent fluidity." This spec fills the gap with two new `NodeKind` values — `NodeKindSynthesize` (produces a topology definition) and `NodeKindInstantiate` (consumes a topology ID and spawns it as a sub-CPN) — plus a dynamic extension of the `FuncRegistry` that allows agent-authored topologies to reference only a safe, enumerable set of primitive building blocks.

This is the keystone capability for `brae` to grow its own procedural library.

## 1. Purpose & Scope

**Purpose.** Let a running CPN produce and instantiate new CPN topologies without any new Go code, with strong safety (topology is validated pre-instantiation, uses only whitelisted primitives) and full persistence (topologies live in `FlowRepository`, indexable by hash).

**In scope.**
- Two new `NodeKind` values: `NodeKindSynthesize` (LLM-backed) and `NodeKindInstantiate` (tool-backed).
- A `CPNTopologyLinter` that, before persistence, checks that every referenced guard/executor/factory name resolves in the *safe* `FuncRegistry` subset.
- A `SafeFuncRegistry` — a curated whitelist of the guards/executors that agent-authored topologies may reference. Example: `llm-call`, `bash-exec`, `http-get-tool`, `validate-json-schema`, `guard-json-nonempty`, `guard-timer-elapsed`. Every entry is a vetted builtin.
- An extension to `FlowRepository`: `SaveAuthored(ctx, topology, provenance) (flow_id, error)`, `GetByID(ctx, flow_id) (CPNTopology, error)`.
- A new color `ColorTopology` (carrying a `CPNTopology` document as payload) and `ColorFlowRef` (carrying a `flow_id` string).
- Validation: a synthesised topology MUST pass `Validate()` AND the `SafeFuncRegistry` check AND a size cap (≤ 50 places, ≤ 50 transitions in v1).
- HITL gate: first-time instantiation of a newly synthesised topology within a session MUST go through HITL (user sees the topology summary before it runs).
- HTTP admin endpoints for topology introspection + rollback.
- Tests: synth → persist → instantiate → run; rejection on unsafe primitive reference; size-cap rejection; idempotency (same topology JSON → same flow_id hash).

**Out of scope.**
- Topology mutation while running (GAP-7).
- Compilation of binaries — that is the forge (GAP-5), which *uses* this spec but is not implemented here.
- Cross-session sharing of synthesised topologies — v1 is session-scoped; a topology persists, but a user's session doesn't automatically inherit another user's topologies.
- Topologies that reference other topologies (meta-meta). Possible but deferred; v1 flattens.

**Audience.** `golang-pro` for backend; careful code review required because this is the most powerful new capability.

## 2. Definitions

- **Topology**: A `CPNTopology` document (places, transitions, initial-marking, metadata). Already defined at `cpn/persist/topology.go`.
- **Synth transition**: `NodeKindSynthesize` — calls an LLM with a task description and a description of the safe primitive catalogue, receives a `CPNTopology` JSON, lints it, and deposits a token carrying the new flow's ID.
- **Instantiate transition**: `NodeKindInstantiate` — consumes a `ColorFlowRef` token, loads the topology, validates it fresh, and spawns it as a sub-CPN.
- **Safe primitive**: A guard/executor/factory function registered in `SafeFuncRegistry` and therefore legal for agent-authored topologies to reference by name.

## 3. Requirements, Constraints & Guidelines

### New NodeKinds

- **REQ-001**: `NodeKindSynthesize = "synthesize"` MUST be added.
- **REQ-002**: `SynthesizeConfig` MUST include: `Model string`, `SystemPrompt string`, `MaxTokens int`, `Temperature float32`, `TaskPlaceholder string` (the token's payload is interpolated into the prompt), `SizeCap SizeCap` (max places, max transitions).
- **REQ-003**: The synth transition MUST output a token of color `ColorFlowRef` with payload `{flow_id string, summary string}`.
- **REQ-010**: `NodeKindInstantiate = "instantiate"` MUST be added.
- **REQ-011**: `InstantiateConfig` MUST include: `InputMapping map[string]string` (maps parent output → child input place), `OutputMapping map[string]string` (child terminal → parent output place), `Timeout time.Duration`.
- **REQ-012**: The instantiate transition MUST consume a `ColorFlowRef`, load the topology from `FlowRepository`, re-validate, and spawn as a sub-CPN using the existing `NodeKindSubNet` execution path.

### LLM prompt contract for synth

- **REQ-020**: The synth LLM MUST receive, as part of its system prompt, a machine-readable catalogue listing every `SafeFuncRegistry` entry with: name, parameter schema, output schema, cost hint. Format: JSON.
- **REQ-021**: The synth LLM MUST return JSON conforming to a strict `CPNTopology` schema (provided in the prompt). The response MUST be wrapped in `<topology>…</topology>` delimiters to survive partial streaming.
- **REQ-022**: The synth transition MUST parse + schema-validate the response. Parse failure → route to `ErrorPlace` with reason `topology_parse_error`.

### Safe registry

- **REQ-030**: `SafeFuncRegistry` MUST be a *compile-time* constant set, registered at server boot. At a minimum it MUST contain:
  - Guards: `guard-json-nonempty`, `guard-timer-elapsed`, `guard-regex-match`, `guard-exit-code-zero`, `guard-cost-under-budget`.
  - Executors: `exec-noop`, `exec-http-get` (calls the `brae/http-get` tool), `exec-bash-preregistered` (runs a pre-registered safe bash snippet by ID), `exec-format-string`.
  - Factories: `factory-llm-respond`, `factory-validate-json`, `factory-observer`.
- **REQ-031**: Agent topologies MUST NOT reference any name outside `SafeFuncRegistry`. The linter rejects with `ErrUnsafePrimitive` listing the offending name(s).
- **REQ-032**: `NodeKindBash` in a synthesised topology MUST only use `BashConfig.Command ∈ {pre-registered ID via exec-bash-preregistered}`. Direct arbitrary shell is forbidden via synth.

### Persistence

- **REQ-040**: `FlowRepository` MUST gain two methods:
  - `SaveAuthored(ctx, topology CPNTopology, prov Provenance) (flow_id string, error)` — computes a deterministic SHA-256 hash over the canonicalised topology JSON; returns the existing ID if the hash matches (idempotent), else inserts.
  - `GetByID(ctx, flow_id string) (CPNTopology, error)`.
- **REQ-041**: Postgres migration `0018_flow_provenance.sql` adds `authored_by_cpn_id`, `authored_from_prompt_digest`, `safe_lint_passed`, `size_places`, `size_transitions` columns to `flows`.

### HITL gate

- **REQ-050**: First instantiation of a synthesised topology within a session MUST pause on a HITL prompt schema `"topology.approval"` payload `{flow_id, summary, size_places, size_transitions, referenced_primitives[]}`.
- **REQ-051**: Subsequent instantiations within the same session (or any session if the user approved-and-remember at the global level) skip HITL.
- **REQ-052**: Admin can globally reject a topology via `POST /api/admin/flows/:id/reject` — future instantiations fail fast.

### Size & safety caps

- **CON-001**: `SizeCap`: max 50 places, max 50 transitions, max 200 arcs. Rejections → `ErrTopologyTooLarge`.
- **CON-002**: Every synthesised topology MUST include at least one terminal place (validator check).
- **CON-003**: Space isolation rules apply: no surface→computation direct edges (except HITL). Linter enforces.
- **SEC-001**: No synthesised topology may declare a new bash command string; only references to pre-approved snippet IDs.
- **SEC-002**: No synthesised topology may declare a `NodeKindRegisterTool` — tool authoring stays in the forge (GAP-5), which is itself a curated topology.

### Admin HTTP

- **REQ-060**: `GET /api/admin/flows?origin=agent-authored`
- **REQ-061**: `GET /api/admin/flows/:id` — returns topology JSON.
- **REQ-062**: `POST /api/admin/flows/:id/reject` body `{reason}` — disables further instantiation of that `flow_id`.

### Guidelines

- **GUD-001**: Keep the safe primitive catalogue small. Every added primitive is a potential attack surface.
- **GUD-002**: When a topology fails linting, include the exact offending names in the error so the LLM can self-correct on a retry.
- **PAT-001**: Use JSON Schema draft 2020-12 for the `CPNTopology` shape the LLM must produce.

## 4. Interfaces & Data Contracts

### Synth prompt (high-level skeleton)

```text
You are the CPN synthesiser for brae. Produce a Colored Petri Net topology
that solves the task below.

TASK:
{{.task}}

RULES:
- You may reference only the primitives in <catalogue> (below).
- Max 50 places, 50 transitions, 200 arcs.
- Respect space isolation: surface → observation → computation.
- Output MUST be between <topology>…</topology> delimiters.
- Schema: <topology_schema/>

CATALOGUE:
<catalogue>
  [
    {"name":"guard-json-nonempty","kind":"guard","params":{},"output":"bool"},
    {"name":"exec-http-get","kind":"executor","params":{"url":"string"},"output":"json"},
    …
  ]
</catalogue>
```

### Go types

```go
type SynthesizeConfig struct {
    Model           string
    SystemPrompt    string
    MaxTokens       int
    Temperature     float32
    TaskPlaceholder string
    SizeCap         SizeCap
}

type SizeCap struct {
    MaxPlaces      int
    MaxTransitions int
    MaxArcs        int
}

type InstantiateConfig struct {
    InputMapping  map[string]string
    OutputMapping map[string]string
    Timeout       time.Duration
}

type TopologyLintResult struct {
    Passed             bool
    UnsafePrimitives   []string
    SizeViolations     []string
    ValidatorErrors    []string
    SpaceViolations    []string
}
```

### Postgres

```sql
ALTER TABLE flows
  ADD COLUMN authored_by_cpn_id TEXT,
  ADD COLUMN authored_from_prompt_digest TEXT,
  ADD COLUMN safe_lint_passed BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN size_places INT NOT NULL DEFAULT 0,
  ADD COLUMN size_transitions INT NOT NULL DEFAULT 0,
  ADD COLUMN rejected BOOLEAN NOT NULL DEFAULT false,
  ADD COLUMN rejected_reason TEXT;
```

## 5. Acceptance Criteria

- **AC-001**: Given a `NodeKindSynthesize` transition with a task "retrieve the title of a URL", when fired, then a `ColorFlowRef` token with a valid `flow_id` is deposited; a row exists in `flows` with `safe_lint_passed=true`.
- **AC-002**: Given an LLM response that references `exec-run-any-bash` (not in safe registry), then the lint rejects with `ErrUnsafePrimitive: exec-run-any-bash`.
- **AC-003**: Given a topology with 51 transitions, then the lint rejects with `ErrTopologyTooLarge`.
- **AC-004**: Given two synth runs producing byte-identical topologies, then both store returns the *same* `flow_id` (idempotent by hash).
- **AC-005**: Given a `NodeKindInstantiate` with a valid `flow_id`, when the user approves the HITL, then the topology runs as a sub-CPN and its terminal tokens flow back into the parent's mapped output places.
- **AC-006**: Given an admin `POST /api/admin/flows/:id/reject`, when the parent later tries to instantiate, then the instantiate transition fails with `ErrTopologyRejected`.
- **AC-007**: Given a topology referencing a deprecated tool, then instantiation fails with `ErrDeprecatedDependency`.
- **AC-008**: `CPNTopology` canonicalisation (for hashing) MUST be stable across Go map iteration order.

## 6. Test Automation Strategy

- Unit: linter against 20 adversarial topologies (unsafe primitive, oversized, violates space, missing terminal).
- Integration: full synth → persist → instantiate → run with a stubbed LLM returning a canned topology.
- Fuzz: generate random topologies, assert linter never accepts one that violates space isolation.
- Coverage ≥ 90% on `cpn/synthesis/` and the linter.

## 7. Rationale & Context

**Why a safe registry, not free-form code generation?** Letting an LLM emit raw Go closures is catastrophic. The catalogue approach gives the LLM sufficient expressivity (combinations of vetted primitives) while keeping the attack surface tiny.

**Why HITL on first instantiation?** Even safe primitives, composed creatively, can burn budget, write surprising files, or loop. A human-in-the-loop gate at the *first* run (per session) is cheap insurance, especially because subsequent runs skip.

**Why flatten (no meta-meta)?** Recursive synthesis can explode the validation state space. v1 defers; the use cases we have (tool-forge, url-reader, file-summariser) are all flat.

## 8. Dependencies & External Integrations

- **EXT-001**: GAP-3 (tool registry) — synth may reference tools.
- **EXT-002**: GAP-6 (host gate) — provides HITL plumbing.
- **INF-001**: Postgres flows table (existing).
- **PLT-001**: Go ≥ 1.22.

## 9. Examples & Edge Cases

### Canonical synthesised topology (url-reader)

```json
{
  "name":"url-reader@0.1",
  "places":[
    {"id":"p-in","color":"string","space":"computation"},
    {"id":"p-fetched","color":"json","space":"computation"},
    {"id":"p-out","color":"artifact","space":"surface"}
  ],
  "transitions":[
    {"id":"t-fetch","kind":"tool","inputs":["p-in"],"outputs":["p-fetched"],
     "executor":"exec-http-get"},
    {"id":"t-summarise","kind":"llm","inputs":["p-fetched"],"outputs":["p-out"],
     "factory":"factory-llm-respond"}
  ],
  "initial_marking":{}
}
```

### Edge — LLM drifts JSON

Parser falls back to `Validate`-with-autocorrect up to 3 rounds; if still invalid → error place.

### Edge — two synths same second

Hash-based idempotency keeps the table clean; the loser gets the winner's `flow_id`.

## 10. Validation Criteria

- Linter rejects every topology that violates space isolation (property test).
- No agent-authored topology in the DB has `safe_lint_passed=false`.
- Hash collisions are treated as identity (by construction of SHA-256).

## 11. Related Specifications / Further Reading

- [spec-architecture-tool-forge-cpn.md](./spec-architecture-tool-forge-cpn.md) — GAP-5, uses this spec.
- [spec-architecture-dynamic-tool-registry.md](./spec-architecture-dynamic-tool-registry.md) — GAP-3.
- [spec-architecture-host-gate-security-policy.md](./spec-architecture-host-gate-security-policy.md) — GAP-6 HITL plumbing.
- [motor_agentico_cpn.md](../.docs/motor_agentico_cpn.md) §10 Sub-CPNs, §16 Validation.
- Paper §8 (future work on high-level reconfigurable networks).
