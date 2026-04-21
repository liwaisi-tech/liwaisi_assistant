---
title: Brae JIT CPN Builder — Topology Composer over the Synthesis Pipeline
version: 0.2
date_created: 2026-04-20
last_updated: 2026-04-20
owner: liwaisi-tech / brae agent team
tags: [architecture, agent, cpn, synthesis, jit, composer, subnet]
changelog:
  - "0.2 (2026-04-20): P0 panel-review fixes — canonical event naming, per-tool SubNet timeout, SubNet egress contract, template arc inscriptions, adversarial lint, multilingual scope, Personality.Digest definition, in-process cache constraint."
---

# Introduction

This specification defines the **JIT CPN Builder**: the component that turns a `ToolMatchSet` (produced by `spec-architecture-brae-tool-request-node.md`) into a runnable Coloured Petri Net composed at runtime. The builder emits `persist.CPNTopology` JSON; the existing `cpn/synthesis` pipeline materialises it. The materialised CPN runs as a **SubNet** under the parent session CPN. It addresses gap **G5** of the JIT CPN Builder design.

## 1. Purpose & Scope

### Purpose

- Compose, lint, and instantiate a tool-grounded sub-CPN per turn from a ranked `ToolMatchSet`.
- Reuse the existing synthesis machinery (`Lint`, `Canonicalise`, `SafeRegistry`, `Materialise`, digest cache) instead of building a parallel runtime.
- Enforce the architectural invariant Axiom A13: `cpn/` never imports `persist/`; the builder communicates with the synthesis pipeline through the JSON-opaque hooks already established by `cpn/synthesis/Bootstrap`.
- Run the materialised CPN as a SubNet so blast radius, token-ledger accounting, and replay snapshots remain bounded.

### In scope

- A new package `cpn/synthesis/jit/` containing the composer (`composer.go`), templates (`templates.go`), and per-session digest cache (`cache.go`).
- An emission of `persist.CPNTopology` JSON via the existing canonicalise/materialise hooks.
- A new transition wiring pattern: `t-tool-request → t-jit-compose → t-jit-instantiate (NodeKindInstantiate) → spawned SubNet`.
- Reuse of `NodeKindInstantiate` (already exists in `cpn/fire_instantiate.go`) — no new node kind.
- Per-session caching keyed by `(matchSetDigest, personalityDigest)`.

### Out of scope

- The retriever (`spec-architecture-brae-tool-retriever.md`).
- The intent emission / `tool_request` transition (`spec-architecture-brae-tool-request-node.md`).
- Per-tool input/output schema *composition* (e.g. piping output of tool A as input of tool B). v0.1 composes parallel-or-sequential templates; cross-tool schema typing is deferred.
- LLM-driven topology authoring beyond template selection. The composer is template-based, not free-form generative.

### Intended audience

CPN engine engineers, synthesis-pipeline maintainers.

### Assumptions

- The toolbox-taxonomy, retriever, and tool-request specs are implemented.
- `cpn/synthesis.Bootstrap` is invoked at server startup and registers the four hooks (`LintTopology`, `CanonicaliseTopology`, `MaterialiseTopology`, `TopologyDigest`).
- `NodeKindInstantiate` exists and accepts a JSON-opaque topology blob via the existing fire handler.

## 2. Definitions

| Term | Definition |
|---|---|
| **JIT CPN Builder** | The composer in `cpn/synthesis/jit/` that turns a `ToolMatchSet` into a `persist.CPNTopology` JSON blob. |
| **Topology template** | A parameterised skeleton (places, transitions, arcs) instantiated with the matched tools. v0.1 ships two templates: `parallel-fanout` and `sequential-pipeline`. |
| **TopologyDraft** | The in-memory pre-canonical representation produced by the composer before serialisation. |
| **Digest cache** | Per-session map keyed by `sha256(canonical topology JSON) + personality digest`; values are reusable `persist.CPNTopology` byte blobs. |
| **SubNet** | An existing CPN concept (`NodeKindSubNet`) for nested composition. The materialised JIT CPN is spawned as a SubNet whose lifecycle is bounded by the parent transition firing. |
| **Axiom A13** | Hexagonal invariant: `cpn/` package may not import `persist/` directly; only via the JSON hooks. |

## 3. Requirements, Constraints & Guidelines

### Functional requirements

- **REQ-001**: A new package `cpn/synthesis/jit/` MUST contain `composer.go` exposing `func Compose(matchSet cpn.ToolMatchSet, intent cpn.Intent, opts ComposeOptions) (cpn.TopologyDraft, error)`.
- **REQ-002**: The composer MUST select a template based on `intent.NL` heuristics + match cardinality: `parallel-fanout` when no obvious data dependency is expressed, `sequential-pipeline` when the intent contains pipeline cues ("then", "after", "pipe", "→"). Template selection MUST be deterministic given the same inputs.
- **REQ-003**: The composer MUST produce a `TopologyDraft` whose places carry correct Space tags: a single Surface input (the user request), Observation places for retrieval intermediates, Computation places for tool invocations and their results, a single Surface output for the user-visible final answer.
- **REQ-004**: The composer MUST produce a draft whose every transition references either a primitive in the `SafeRegistry` or a tool resolvable through `ToolRegistry.Get`. Unknown references MUST cause `Compose` to return `ErrUnknownPrimitive`.
- **REQ-005**: A serialiser `func Emit(draft TopologyDraft) ([]byte, error)` MUST produce a `persist.CPNTopology` JSON blob without importing `persist`. The blob format is the same one already accepted by `cpn.MaterialiseTopology`; the serialiser writes through a struct identical-by-shape to `persist.CPNTopology` and round-trips through `json.Marshal`.
- **REQ-006**: The composer MUST consult a per-session digest cache (`cpn/synthesis/jit/cache.go`) keyed by `sha256(canonicalJSON) + sha256(personalityDigest)`. On cache hit, the cached blob MUST be returned and a `jit.cache.hit` event MUST be emitted.
- **REQ-007**: After emission, the topology MUST be validated through the existing `cpn.LintTopology` hook; lint failure MUST abort with the lint result's first error.
- **REQ-008**: The composer's output MUST be deposited as a `ColorTopology` token (existing colour) on a place whose downstream transition is of kind `NodeKindInstantiate`. The instantiate transition (existing) materialises the topology and spawns it as a SubNet.
- **REQ-009**: The spawned SubNet MUST inherit `SessionID`, `TraceID`, and `Personality` from the parent CPN. Its token-ledger accounting MUST be attributed to the parent session.
- **REQ-010**: When the SubNet completes (terminal markings reached), its terminal token (typically `ColorString` carrying the user-facing answer) MUST flow back to the parent CPN through the existing SubNet egress mechanism.
- **REQ-011**: The composer MUST emit two events: `jit.compose.started` (with intent + match summary) and `jit.compose.completed` (with topology digest, place count, transition count, cache hit/miss, lint outcome). All JIT events MUST follow the canonical `<domain>.<subject>.<verb>` lowercase dot-form mandated by `spec-architecture-brae-toolbox-taxonomy.md`.
- **REQ-012**: A new size cap `JITCompositionCap{MaxPlaces: 32, MaxTransitions: 24}` MUST be passed to `LintTopology`. JIT-built topologies that exceed it MUST fail lint, not silently truncate.
- **REQ-013 (per-tool timeout)**: Each tool transition within a spawned SubNet MUST inherit a per-tool timeout of `min(30s / max(tool_count, 1), 5s)`. On per-tool timeout, that branch MUST deposit a `ColorError` token on its result place and abort; the aggregator/observer MUST tolerate partial errors and emit a final answer that explicitly enumerates which branches failed.
- **REQ-014 (SubNet egress contract)**: Every spawned JIT SubNet MUST have a single terminal place whose token is forwarded to a parent place named `p-jit-subnet-result`. The parent's observer transition consumes that token and produces the final user-visible answer. On whole-SubNet timeout (BEH-002), partial-result tokens on intermediate places MUST be discarded and exactly one `ColorError` token MUST be deposited on the parent's error place named `p-jit-subnet-error`.
- **REQ-LINT-ADV (adversarial lint)**: Lint of a JIT topology MUST reject topologies exhibiting any of: (a) self-loop on a single place (i.e. an arc whose source and destination resolve to the same place via a single transition cycle of length 1), (b) any place with no upstream transition or no downstream transition (dead-end / unreachable), excluding the designated input and output places, (c) fanout > 8 from any single transition (resource bound). Adversarial fixtures MUST live under `cpn/synthesis/jit/testdata/adversarial/`.
- **REQ-PERSONALITY-DIGEST (digest definition)**: `Personality.Digest()` MUST be defined as `sha256(toolbox_catalogue_bytes || os_line || shell_line || lexicon_excerpt_bytes)`, where `||` denotes byte concatenation in that fixed order. The digest MUST change whenever any of the four inputs change. Cache entries keyed in part by `personalityDigest` MUST be invalidated when the digest changes (in addition to LRU eviction at cap 64).

### Security requirements

- **SEC-001**: Templates MUST NOT contain executable code; they are data-only structures parameterised by tool qualified names. Template selection logic lives in Go and is auditable.
- **SEC-002**: Every tool referenced in the draft MUST be re-resolved from `ToolRegistry` at compose time (not relied upon from the `ToolMatchSet` payload alone). This guards against the match set being tampered with between retrieval and composition.
- **SEC-003**: A composed topology that references any tool with `Origin = "agent-authored"` MUST flow through the `tool_compose` policy gate (see `spec-architecture-brae-tool-compose-policy.md`) before instantiation. Builtin and awakening-minted tools bypass.
- **SEC-004**: The digest cache MUST be per-session. Cross-session leakage of cached topologies is forbidden because a topology may reference session-scoped registry entries.
- **SEC-005**: The emitted JSON MUST NOT carry tool `BinaryPath`, `BinarySHA256`, `Provenance`, or `RegisteredBy` fields. These belong to the registry, not to the topology.

### Behaviour & product requirements

- **BEH-001**: When `matchSet.Matches` is empty, `Compose` MUST return `ErrNoMatches` and the parent CPN MUST surface a polite "no tooling found, please clarify" message to the user.
- **BEH-002**: When the composed SubNet exceeds a per-firing budget (default 30 seconds), the parent transition MUST cancel the SubNet and emit a `jit.subnet.timeout` event. Partial results, if any, are discarded (see REQ-014 for the egress/error contract).
- **BEH-003**: The composer SHOULD emit a structured "topology preview" event so the frontend can show the user what graph is about to run (matches the existing topology-preview pattern from authored flows).
- **BEH-004 (multilingual scope)**: v0.1 pipeline-cue detection is English-only (`(then|after|pipe|→|next)`); non-English intents — including the working language es-CO — default to the `parallel-fanout` template. Multilingual cue tables are deferred to v0.2 of this spec and tracked as an explicit gap. A Spanish-language testdata fixture MUST exist asserting the current English-only behaviour so that any future implementer extending cues is forced to address the gap.

### Constraints

- **CON-001**: `cpn/synthesis/jit/` MUST NOT import `cpn/persist/`. It MAY import `cpn/synthesis/` (which is the seam allowed to bridge cpn ↔ persist via JSON).
- **CON-002**: The composer MUST run in ≤ 50 ms p95 (excluding Lint time, which is bounded separately).
- **CON-003**: Lint of a JIT topology MUST run in ≤ 100 ms p95 at the size cap.
- **CON-004**: The digest cache MUST hold ≤ 64 entries per session, evicted LRU.
- **CON-005**: Composed SubNets MUST be parented; orphaned spawn (no parent transition) MUST fail materialisation with `ErrOrphanedSubNet`.
- **CON-006 (in-process cache only)**: The per-session digest cache MUST reside in process memory only — no Redis, no Memcached, no external store. The cache is session-scoped, evicted LRU at cap 64 (CON-004), and MUST be cleared on session destruction. Additionally, all cache entries for a session MUST be invalidated whenever `Personality.Digest()` changes (see REQ-PERSONALITY-DIGEST below).

### Guidelines

- **GUD-001**: Keep templates minimal. Two templates in v0.1 (`parallel-fanout`, `sequential-pipeline`); add a third only when fixture-driven evidence shows the existing two distort common intents.
- **GUD-002**: Prefer hashtag-driven template hints over NL parsing where possible. Hashtags are unambiguous; NL is not.
- **GUD-003**: Always thread `ctx` through compose → emit → lint → instantiate so cancellation propagates uniformly.
- **GUD-004**: Tag every emitted JIT topology with `metadata.jit = true` and `metadata.intent_digest = ...` so observability can distinguish JIT from authored flows.

### Patterns

- **PAT-001**: Compose → Emit → Lint → Cache-store → Deposit. The four steps are sequenced inside `Compose` so callers see one entry point and one error.
- **PAT-002**: Templates are functions of `(matches, intent) -> TopologyDraft`. Pure, no I/O. Easy to test.
- **PAT-003**: SubNet spawning reuses `NodeKindInstantiate`; the JIT builder does not introduce a new node kind. This keeps the executor surface flat.
- **PAT-004**: Personality is folded into the cache key so a personality change invalidates JIT topology reuse — the same matches under a different ethical posture might yield a different topology in future template revisions.

## 4. Interfaces & Data Contracts

### 4.1 Composer signature (`cpn/synthesis/jit/composer.go`)

```go
package jit

type ComposeOptions struct {
    SessionID         string
    TraceID           string
    PersonalityDigest string  // from cpn.Personality.Digest()
    Template          TemplateID  // optional override; default = auto
    Cap               cpn.SizeCap // default JITCompositionCap
}

type TemplateID string
const (
    TemplateAuto              TemplateID = "auto"
    TemplateParallelFanout    TemplateID = "parallel-fanout"
    TemplateSequentialPipeline TemplateID = "sequential-pipeline"
)

func Compose(
    ctx context.Context,
    matchSet cpn.ToolMatchSet,
    intent cpn.Intent,
    opts ComposeOptions,
) (cpn.TopologyDraft, []byte, error)   // returns draft AND emitted JSON

var JITCompositionCap = cpn.SizeCap{MaxPlaces: 32, MaxTransitions: 24}
```

### 4.2 `TopologyDraft` (in `cpn/`, draft-only mirror of persist shape)

```go
// cpn/topology_draft.go (already exists in concept; this spec formalises)
type TopologyDraft struct {
    ID          string
    Role        string  // "jit-tool-flow"
    Metadata    map[string]string
    Places      []PlaceSpec
    Transitions []TransitionSpec
    Arcs        []ArcSpec
    InitialMarking []MarkingSpec
}
```

### 4.3 Template skeletons

#### parallel-fanout

Used when matches are independent (no pipeline cue in `intent.NL`):

```
                ┌─→ t-tool-1 ─→ p-result-1 ┐
p-input ─→ t-fanout                            ├─→ t-aggregate ─→ p-output
                ├─→ t-tool-2 ─→ p-result-2 ┤
                └─→ t-tool-N ─→ p-result-N ┘
```

Arc inscriptions (colours on each arc):

- `p-input → t-fanout` carries `ColorIntent`.
- `t-fanout → p-input-N` (one per tool branch) carries `ColorIntent` (the fanout splits the intent into per-tool sub-intents).
- `t-tool-N → p-result-N` carries `ColorArtifact`. On per-tool timeout (REQ-013) the same arc carries `ColorError`.
- `p-result-N → t-aggregate` carries `ColorArtifact` (or `ColorError`); `t-aggregate` consumes the multiset `[ColorArtifact, ...]` (tolerating `ColorError` per REQ-013) and emits exactly one `ColorArtifact` to `p-output`.

#### sequential-pipeline

Used when `intent.NL` matches `(then|after|pipe|→|next)` regex (English-only, see BEH-004):

```
p-input ─→ t-tool-1 ─→ p-mid-1 ─→ t-tool-2 ─→ p-mid-2 ─→ ... ─→ p-output
```

Arc inscriptions:

- `p-input → t-step-1` carries `ColorIntent`.
- `t-step-N → p-mid-N` carries `ColorArtifact`; `p-mid-N → t-step-(N+1)` carries `ColorArtifact`.
- The final `t-step-N → p-output` carries `ColorArtifact`.
- On per-tool timeout (REQ-013) at any `t-step-N`, that arc carries `ColorError` to a sink place that short-circuits to `p-jit-subnet-error` per REQ-014.

### 4.4 Emitted JSON shape (compatible with `persist.CPNTopology`)

```json
{
  "id": "jit-9f1c7a...",
  "role": "jit-tool-flow",
  "metadata": {
    "jit": "true",
    "intent_digest": "sha256:...",
    "match_count": "2",
    "template": "parallel-fanout"
  },
  "places": [
    {"id":"p-input","color":"String","space":"Surface"},
    {"id":"p-result-pdf-to-text","color":"Artifact","space":"Computation"},
    {"id":"p-result-pdf-info","color":"Artifact","space":"Computation"},
    {"id":"p-output","color":"String","space":"Surface"}
  ],
  "transitions": [
    {"id":"t-fanout","kind":"observer", "executor_func":"jit-fanout"},
    {"id":"t-pdf-to-text","kind":"tool","tool_name":"pdf/pdf-to-text@0.1.0"},
    {"id":"t-pdf-info","kind":"tool","tool_name":"pdf/pdf-info@0.1.0"},
    {"id":"t-aggregate","kind":"observer","executor_func":"jit-aggregate"}
  ],
  "arcs": [...],
  "initial_marking": [{"place":"p-input","tokens":1}]
}
```

The `executor_func` references `jit-fanout` and `jit-aggregate` are entries in the SafeRegistry contributed by this package at startup.

### 4.5 Events

All event names follow the canonical `<domain>.<subject>.<verb>` lowercase dot-form mandated by `spec-architecture-brae-toolbox-taxonomy.md`.

```json
{"event":"jit.compose.started","intent":{...},"match_count":2,"template":"auto"}
{"event":"jit.compose.completed","digest":"sha256:...","places":4,"transitions":4,"cache_hit":false,"lint_ok":true,"latency_ms":12}
{"event":"jit.cache.hit","digest":"sha256:...","session_id":"..."}
{"event":"jit.subnet.spawned","topology_id":"jit-9f1c7a...","parent_transition":"t-jit-instantiate"}
{"event":"jit.subnet.completed","topology_id":"jit-9f1c7a...","outcome":"success","tokens_emitted":1}
{"event":"jit.subnet.timeout","topology_id":"jit-9f1c7a...","scope":"subnet","budget_ms":30000}
```

### 4.6 SafeRegistry contributions

The package registers two new safe primitives at startup:

```go
safe.Register("jit-fanout",    fanoutExecutor)     // splits one input token into N
safe.Register("jit-aggregate", aggregateExecutor)  // joins N result tokens into one
```

Both are pure-Go, deterministic, no I/O.

## 5. Acceptance Criteria

- **AC-001**: Given a `ToolMatchSet` with 2 matches and an intent without pipeline cues, when `Compose` is called, then the emitted topology MUST use template `parallel-fanout` AND MUST contain exactly 4 places and 4 transitions.
- **AC-002**: Given the same intent + match set is composed twice in the same session, when the second `Compose` runs, then a `jit.cache.hit` event MUST be emitted AND the byte blob MUST be byte-equal to the first.
- **AC-003**: Given the personality digest changes between two compositions of the same intent, when `Compose` runs, then the cache MUST miss and a fresh blob MUST be produced.
- **AC-004**: Given a match set referencing a tool that is not in `ToolRegistry`, when `Compose` is called, then `ErrUnknownPrimitive` MUST be returned and no token MUST be deposited.
- **AC-005**: Given the emitted topology exceeds 32 places or 24 transitions, when Lint runs, then it MUST fail with the cap-violation reason and the SubNet MUST NOT spawn.
- **AC-006**: Given a successful compose → emit → lint → instantiate sequence, when the SubNet runs to completion, then the terminal token MUST appear on the parent's output place AND `jit.subnet.completed` MUST fire with `outcome: "success"`.
- **AC-007**: Given the SubNet exceeds the 30-second budget, when the deadline fires, then the SubNet MUST be cancelled AND `jit.subnet.timeout` MUST be emitted AND no partial results MUST be observed by the parent.
- **AC-008**: Given the match set is empty, when `Compose` is called, then `ErrNoMatches` MUST be returned.
- **AC-009**: Given a composed topology references an `agent-authored` tool, when the instantiate transition is reached, then the `tool_compose` policy gate MUST evaluate it before SubNet spawn.
- **AC-010**: Given any composed topology, when serialised, then `metadata.jit = "true"` and `metadata.intent_digest` MUST be present in the JSON.
- **AC-011 (per-tool timeout)**: Given a `parallel-fanout` SubNet with 3 tools where one tool exceeds its per-tool budget (`min(30s/3, 5s) = 5s`), when the deadline fires for that branch, then a single `ColorError` token MUST appear on that branch's `p-result-N` AND the aggregator MUST still produce one final `ColorArtifact` on `p-output` enumerating the failed branch.
- **AC-012 (SubNet egress on timeout)**: Given a SubNet times out mid-execution (whole-SubNet 30s budget), when the parent regains control, then exactly one `ColorError` token MUST appear on the parent's `p-jit-subnet-error` place AND zero `ColorArtifact` tokens on the parent's `p-jit-subnet-result` place.
- **AC-013 (adversarial lint)**: Given a topology fixture under `cpn/synthesis/jit/testdata/adversarial/` containing (a) a self-loop on a single place, (b) a place with no upstream or no downstream transition, or (c) a transition with fanout > 8, when Lint runs, then it MUST fail with the corresponding adversarial-rule reason and the SubNet MUST NOT spawn.
- **AC-014 (Spanish-language fallback)**: Given an intent in Spanish (es-CO) such as `"descarga el archivo y luego extrae el título"`, when `Compose` runs in v0.1, then the chosen template MUST be `parallel-fanout` (English-only cue regex does not match) AND a fixture under `cpn/synthesis/jit/testdata/multilingual/es-co-pipeline.json` MUST assert this behaviour.
- **AC-015 (Personality.Digest)**: Given `Personality.Digest()` is called on the same toolbox catalogue, OS line, shell line, and lexicon excerpt, then the returned digest MUST equal `sha256(toolbox_catalogue_bytes || os_line || shell_line || lexicon_excerpt_bytes)` AND any change to any of the four inputs MUST yield a different digest AND MUST invalidate cache entries keyed on the prior digest.

## 6. Test Automation Strategy

- **Test levels**: Unit (template builders, cache, emit/parse round-trip), Integration (compose → lint → materialise → spawn against an in-memory registry), End-to-End (HTTP API turn that drives `tool_request → jit-compose → jit-instantiate → user-visible answer`).
- **Frameworks**: Go `testing` stdlib; deterministic SafeRegistry seeded with `jit-fanout` / `jit-aggregate`; transcript-replay LLM provider.
- **Test data**: Fixtures under `cpn/synthesis/jit/testdata/`: per template, a (matchSet, intent) input and a golden JSON output.
- **CI/CD integration**: Standard `go test ./...`. A separate `go test -tags=jit-golden -update` flag refreshes goldens after intentional template edits.
- **Coverage requirements**: ≥ 90 % line coverage on `cpn/synthesis/jit/`. Mutation testing on the template selectors.
- **Performance testing**: Benchmark `Compose` and `Lint` to verify CON-002 and CON-003. Benchmark cache hit rate under a synthetic 100-turn session.
- **Security testing**: Adversarial fixtures where the `ToolMatchSet` references a tool that the registry has since deprecated or removed; assert correct error path and no SubNet spawn.

## 7. Rationale & Context

The synthesis pipeline already does the hard work of validating, canonicalising, and materialising topologies (see `cpn/synthesis/Bootstrap` in `bootstrap.go`). Building a parallel "JIT runtime" would duplicate this and immediately diverge from the safety and replay guarantees the synthesis pipeline provides. The JIT CPN Builder is therefore designed as a *producer* of `persist.CPNTopology` JSON, using the same hooks that authored flows go through. This is the cheapest possible path to JIT composition that preserves Axiom A13 and the digest-based deduplication infrastructure.

Templates over generative composition is a deliberate choice for v0.1. Generative composition (asking an LLM to author the topology) is the AFlow-style direction, but it requires either MCTS (computationally heavy) or fine-tuned models (operationally heavy). Templates are predictable, auditable, and easy to test; they handle the 80 % of intents that are simple "use one tool" or "use these tools in parallel" cases. The seam to add a third template — or eventually an LLM-authored path — is a clean drop-in: a new `TemplateID` constant and a new template function.

SubNet spawning is the right boundary because the parent CPN already understands SubNets (see `cpn/subnet.go`). Inheriting `SessionID`, `TraceID`, and `Personality` keeps audit and accounting consistent. Cancellation, retries, and HITL gates all compose naturally: the parent transition can wrap the SubNet spawn in an HITL guard if required by policy, without the SubNet needing to know.

The cache key includes the personality digest because `brae`'s personality contributes to the system prompt and may, in future template revisions, shape the topology (e.g. inserting a `validate` transition after every tool call when the ethical posture demands paranoia). Today the personality digest is a guard against accidental reuse; tomorrow it is the hook for personality-aware composition.

## 8. Dependencies & External Integrations

### External systems

- **EXT-001**: None.

### Third-party services

- **SVC-001**: None new.

### Infrastructure dependencies

- **INF-001**: Existing `cpn/synthesis/Bootstrap` hooks (`LintTopology`, `CanonicaliseTopology`, `MaterialiseTopology`, `TopologyDigest`).
- **INF-002**: Existing `NodeKindInstantiate` handler (`cpn/fire_instantiate.go`).
- **INF-003**: Existing `cpn/subnet.go` SubNet spawning machinery.

### Data dependencies

- **DAT-001**: `ToolMatchSet` payload from the tool-request transition.
- **DAT-002**: `ToolRegistry` (re-resolves matches at compose time).
- **DAT-003**: `cpn.Personality` digest (already exposed by the personality module).

### Technology platform dependencies

- **PLT-001**: Go 1.25+.

### Compliance dependencies

- **COM-001**: Composed topologies do not carry user data beyond the intent token they were derived from. No additional retention concerns.

## 9. Examples & Edge Cases

### 9.1 Parallel-fanout end-to-end

```
matchSet = {pdf/pdf-to-text@0.1.0, pdf/pdf-info@0.1.0}
intent   = {hashtags: [tools, pdf, read], nl: "extract text and get the page count"}
template = parallel-fanout (no pipeline cue)
result   = SubNet runs both tools concurrently; aggregator joins outputs into one final token
```

### 9.2 Sequential-pipeline

```
matchSet = {web/curl@0.1.0, web/jq@0.1.0}
intent   = {hashtags: [tools, web, json], nl: "fetch the URL then extract the .data[0].title"}
template = sequential-pipeline (cue: "then")
result   = SubNet pipes curl's output to jq, jq's output to user
```

### 9.3 Cache hit

A user asks the same question twice in one session. First turn: compose → lint → materialise → spawn (12 ms compose + 28 ms lint). Second turn: compose returns from cache (< 1 ms); materialise re-uses the cached blob; same SubNet spawns.

### 9.4 Edge case: deprecated tool surfaces in match set

The retriever filters deprecated tools, but a race is possible if a tool is deprecated between retrieval and composition. The composer's re-resolution (SEC-002) catches this: `ErrUnknownPrimitive` aborts compose, the parent retries `tool_request` on the next LLM turn.

### 9.5 Edge case: cap exceeded

A pathological match set with 24 tools triggers `parallel-fanout` and produces 26 transitions (24 tools + fanout + aggregate). Lint fails at `MaxTransitions: 24`. The composer surfaces the lint error to the LLM, which is expected to lower `max_tools` on its next intent.

### 9.6 Edge case: SubNet timeout mid-pipeline

A sequential pipeline fires three tools; the second hangs. At 30 s, the parent transition cancels the SubNet. The first tool's intermediate result is discarded; per REQ-014, exactly one `ColorError` token appears on the parent's `p-jit-subnet-error` place. The user sees `jit.subnet.timeout` rendered as a polite "tooling timed out" message.

## 10. Validation Criteria

- All ten Acceptance Criteria pass.
- Compose latency p95 ≤ 50 ms; Lint p95 ≤ 100 ms at the size cap.
- Cache hit rate ≥ 25 % on a synthetic 100-turn fixture session.
- A successful end-to-end run logs the full chain: `tool_request.started → tool_request.completed → jit.compose.started → jit.compose.completed → jit.subnet.spawned → jit.subnet.completed`.
- Linter rejects every fixture deliberately exceeding the cap.

## 11. Related Specifications / Further Reading

- `spec/spec-architecture-brae-toolbox-taxonomy.md`
- `spec/spec-architecture-brae-tool-retriever.md`
- `spec/spec-architecture-brae-tool-request-node.md`
- `spec/spec-architecture-brae-tool-compose-policy.md`
- Borghoff, Bottoni, Pareschi (2025), arXiv 2502.14000.
- TB-CSPN, MDPI Future Internet, doi:10.3390/fi17080363 — published precedent for CPN-coordinated LLM agents.
- AFlow (Zhang et al., ICLR 2025 oral), arXiv 2410.10762 — generative workflow induction; the next direction beyond templates.
- LLM Compiler (Kim et al., ICML 2024), arXiv 2312.04511 — DAG-of-tool-calls precedent.

## 12. Search Keywords

JIT, composer, topology synthesis, SubNet, NodeKindInstantiate, SafeRegistry, digest cache, parallel fanout, sequential pipeline, template selection, hexagonal A13, cpn synthesis Bootstrap, brae.
