---
title: Brae Awakening — Consolidated Architecture & Gap Closure
version: 1.0
date_created: 2026-04-21
last_updated: 2026-04-21
owner: brae-core
tags: [architecture, cpn, awakening, brae, host-discovery, toolbox, probe-fanout]
supersedes:
  - spec-architecture-brae-awakening-self-discovery.md
  - spec-architecture-brae-awakening-toolbox-extension.md
  - spec-architecture-brae-awakening-probe-fanout.md
  - spec-architecture-brae-awakening-topology-bootstrap-fix.md
  - spec-architecture-brae-self-discovery-roadmap.md
---

# Introduction

The **brae-awakens** topology is the first CPN that fires when a session boots. It performs LLM-driven host introspection, runs deterministic capability probes, projects an `AwakeningReport`, registers discovered tools in the tool-forge, and emits the first-turn A2UI card. This specification consolidates five prior specs (deleted in commit `0c593c1`) into a single divisible architecture, enumerates implemented state against `back/go-assistant/`, and defines each remaining gap as an independently shippable **sub-CPN** following brae philosophy: *if a problem is divisible, factor it into its own CPN; if a requirement is divisible, factor it into its own leaf REQ*.

## 1. Purpose & Scope

**Purpose.** Define the root `brae-awakens` CPN plus 18 sub-CPNs (SC-01…SC-18) that collectively implement host self-discovery, probe fanout, toolbox/hashtag taxonomy, personality digest, first-turn A2UI emission, and the gap-closure work (sandbox, help-parser, tool-synthesis, capability ACL, model fallback, observability, E2E).

**Scope.**
- **In scope:** `back/go-assistant/` — CPN topology, LLM transitions, host-gate classes, tool-forge, A2UI envelope, persistence.
- **Out of scope:** Frontend rendering beyond the A2UI v0.8 contract; model-registry admin UI (covered by its own spec); conversation-rewind semantics.

**Audience.** CPN/Go engineers, security reviewers, LLM prompt engineers, observability engineers, test engineers, frontend integrators consuming the A2UI first-turn card.

**Assumptions.**
- CPN runtime supports `NodeKindInstantiate` (GAP-4 has landed; see `cpn/fire_instantiate.go`).
- A ToolRegistry with hashtag+toolbox columns is live (Chunk 1 — commit `1f0df23`).
- JIT builder minimal slice is in place (commit `8648178`) and will consume `PersonalityDigest()` for turn ≥2 prompt cache keys.

## 2. Definitions

| Term | Definition |
|---|---|
| **CPN** | Coloured Petri Net — the execution model of the runtime. Places hold typed tokens; transitions fire when input places have tokens matching the transition's color signature. |
| **Sub-CPN** | A self-contained CPN instantiated by a parent transition (via `NodeKindInstantiate`). Has its own lifecycle, trigger, places, transitions, and reducer back to the parent. |
| **Awakening** | The first-turn CPN run that introspects the host before any user message is processed. |
| **AwakeningReport** | Structured JSON the LLM produces: OS, shell, binaries, tool registrations, toolbox, hashtags. |
| **Probe** | A deterministic, read-only shell command run through host-gate class `introspection` to confirm a capability. |
| **Probe fanout** | Parallel execution of N probes through a dynamically composed sub-CPN; AND-joined by a reducer. |
| **Toolbox** | Canonical bucket a tool belongs to (`shell`, `vcs`, `pkg`, `editor`, `net`, `build`, `container`, `test`, `doc`, `misc`). |
| **Hashtag** | Fine-grained capability tag drawn from a curated lexicon (`lexicon.yaml`). |
| **Personality Digest** | `sha256(catalogue‖os‖shell‖lexicon)` — stable cache key for turn ≥2 prompt assembly. |
| **Host-gate** | The security shim that decides whether a shell command requires HITL, runs freely, or is rejected. Classes: `introspection`, `read`, `mutating`, `dangerous`. |
| **HITL** | Human-in-the-loop — UI prompt that blocks execution pending explicit approval. |
| **A2UI** | Agent-to-UI protocol (v0.8). First-turn card is the user-visible summary of awakening. |
| **Tool-forge** | The subsystem that registers a tool manifest into the `tool_registry` table. |
| **Lexicon** | Curated YAML of hashtags (`cpn/tools/lexicon.yaml`) with priority weights. |

## 3. Requirements, Constraints & Guidelines

### 3.1 Root CPN (`brae-awakens`)

- **REQ-001**: A `brae-awakens` CPN MUST fire exactly once per session before any user-message transition is enabled.
- **REQ-002**: The root CPN MUST produce an `AwakeningReport` token on `p-awakening-report`.
- **REQ-003**: The root CPN MUST persist the report as `cpn_snapshots` row with `source="awakening"`.
- **REQ-004**: The root CPN MUST emit an A2UI v0.8 first-turn envelope on `p-awakening-message`.
- **REQ-005**: The root CPN MUST invoke tool-forge `RegisterBatch()` with the report's `Tools[]`.
- **REQ-006**: The root CPN MUST complete within `awakeningDeadline` (30 s) or fail the session with a structured error. No silent fallback.
- **REQ-007**: The root CPN MUST be idempotent: re-firing on a cache hit MUST NOT duplicate tool registrations or snapshot rows.

### 3.2 Divisibility Principle

- **GUD-001**: Every transition whose body exceeds one external system call (LLM, shell, DB, registry) MUST be factored into a sub-CPN.
- **GUD-002**: Every requirement with an `AND` in its body MUST be split into two leaf requirements.
- **GUD-003**: Sub-CPNs MUST declare: `trigger`, `input places`, `transitions`, `output places`, `host-gate class`, `HITL policy`, `observability events`, `failure modes`.
- **PAT-001**: When a sub-CPN yields a result that the parent must reduce, always use an explicit **reducer transition** (e.g., `t-awaken-probe-reduce`) — never in-line aggregation.

### 3.3 Security

- **SEC-001**: Every probe command MUST pass through `host.Gate` with class `introspection`; non-matching commands MUST be rejected (not escalated to HITL).
- **SEC-002**: The `introspection` allow-list is prefix-based (`which`, `command -v`, `uname`, `echo $SHELL`, `ls`, `cat /etc/os-release`, `printenv PATH`) — no shell metacharacters beyond `$SHELL`, `$PATH`, `$HOME`.
- **SEC-003**: Synthesized tools (SC-12) MUST NOT be callable until a **first-run HITL** (SC-13) records explicit user approval.
- **SEC-004**: Sandbox wrappers (SC-10) MUST wrap every probe subprocess in `bwrap` (Linux) or `firejail` (fallback) with `--ro-bind / /`, no network, no `/tmp` write outside a scratch dir.
- **SEC-005**: The capability ACL (SC-14) MUST decide on `(binary SHA256, flag set)` tuples — not on raw command strings.

### 3.4 Constraints

- **CON-001**: `awakeningPinnedModel = "google/gemini-2.5-flash"` is the bootstrap default; a fallback chain (SC-15) MUST be consulted on provider failure.
- **CON-002**: Probe fanout compose MUST cap at **16** concurrent probes per session.
- **CON-003**: Probe fanout sub-CPN MUST NOT round-trip through `persist.CPNTopology` (ephemeral only).
- **CON-004**: Personality digest inputs MUST be byte-stable (sorted keys, LF line endings) so the SHA256 is reproducible across runs.
- **CON-005**: Tool registration MUST be idempotent; duplicate `(session_id, tool_name)` MUST be silently skipped.
- **CON-006**: Cache TTL for `AwakeningReport` snapshots is **24 h**; after TTL the full loop MUST re-run.

## 4. Interfaces & Data Contracts

### 4.1 Root Topology (current state)

Places (`cpn/awakens/topology.go:30-44`):

| Place | Purpose |
|---|---|
| `p-awaken-trigger` | Session-start token |
| `p-awaken-system-prompt` | Rendered system prompt for bootstrap LLM |
| `p-awaken-plan` | `AwakeningProbePlan` produced by bootstrap LLM |
| `p-awaken-subnet-spec` | Composed sub-CPN spec for fanout instantiation |
| `p-awakening-report` | Final `AwakeningReport` token |
| `p-awakening-snapshot` | Persisted snapshot receipt |
| `p-awakening-message` | A2UI first-turn envelope |
| `p-awakening-toolbatch` | Batch of tool manifests to register |

Transitions (`cpn/awakens/topology.go:54-69`):

| Transition | Kind | Role |
|---|---|---|
| `t-awaken-llm-bootstrap` | LLM | Produce `AwakeningProbePlan` from system prompt |
| `t-awaken-probe-compose` | Compose | Build fanout sub-CPN spec |
| `t-awaken-probe-instantiate` | Instantiate | Fire the sub-CPN (currently via `NodeKindTool` workaround — see SC-09) |
| `t-awaken-report` | LLM | Reduce probe results into an `AwakeningReport` |
| `t-awaken-persist` | DB | Persist snapshot |
| `t-awaken-register-tools` | Registry | `RegisterBatch()` tools |
| `t-awaken-emit-message` | A2UI | Emit first-turn card |

### 4.2 `AwakeningReport` (schema) — `cpn/awakens/report.go:14-80`

```go
type AwakeningReport struct {
    SessionID string                   `json:"session_id"`
    Host      AwakeningHost            `json:"host"`            // os, shell, arch
    Binaries  []AwakeningBinary        `json:"binaries"`        // name, path, version
    Tools     []AwakeningToolRegister  `json:"tools"`           // batch to register
    Toolbox   map[string][]string      `json:"toolbox"`         // bucket -> tool names
    Hashtags  []string                 `json:"hashtags"`        // flat capability tags
    Lexicon   AwakeningLexiconStamp    `json:"lexicon"`         // {version, sha256}
    CapturedAt time.Time               `json:"captured_at"`
}

type AwakeningToolRegister struct {
    Name     string   `json:"name"`
    Kind     string   `json:"kind"`       // bash|mcp|synthesized
    Command  string   `json:"command"`
    Toolbox  string   `json:"toolbox"`    // one of the 10 canonical buckets
    Hashtags []string `json:"hashtags"`   // subset of lexicon
}
```

### 4.3 `AwakeningProbePlan` — `cpn/awakens/fanout/types.go:88`

```go
type AwakeningProbePlan struct {
    Probes []AwakeningProbe `json:"probes"`   // ≤ 16
}
type AwakeningProbe struct {
    Slug    string   `json:"slug"`     // stable id, used for place names
    Command string   `json:"command"`  // must match introspection allow-list
    Expect  string   `json:"expect"`   // optional regex over stdout
}
```

### 4.4 `AwakeningProbeResult` — `cpn/awakens/fanout/types.go`

```go
type AwakeningProbeResult struct {
    Slug     string `json:"slug"`
    ExitCode int    `json:"exit_code"`
    Stdout   string `json:"stdout"`
    Stderr   string `json:"stderr"`
    Latency  time.Duration `json:"latency_ms"`
}
```

### 4.5 A2UI First-Turn Envelope — `cpn/awakens/a2ui.go:27`

```json
{
  "version": "0.8",
  "cpnRole": "awakening",
  "components": [
    {"type": "summary",  "os": "linux", "shell": "zsh", "binaries": 42},
    {"type": "toolbox",  "buckets": {"shell": ["rg","fd"], "vcs": ["git"]}},
    {"type": "hashtags", "tags": ["#search","#git","#pkg-apt"]}
  ]
}
```

### 4.6 Observability Event Taxonomy (SC-08)

| Event | Fields | Emitter |
|---|---|---|
| `awakening.started` | session_id, model | session_service_awakening |
| `awakening.llm.bootstrap.completed` | duration_ms, tokens_in, tokens_out | t-awaken-llm-bootstrap |
| `awakening.probe.composed` | probe_count, slugs[] | fanout/composer |
| `awakening.probe.fired` | slug, duration_ms, exit_code | fanout/executor |
| `awakening.probe.reduced` | success_count, fail_count | fanout/reduce |
| `awakening.report.projected` | tool_count, toolbox_buckets[] | report.Project |
| `awakening.registered` | tool_count, duplicate_count | toolforge.RegisterBatch |
| `awakening.emitted` | component_count | a2ui.BuildFirstTurnMessage |
| `awakening.failed` | stage, error_class, error_msg | any |

All events MUST be emitted via `log/slog` **and** an OTEL span attribute with name `brae.awakening.<event>`.

## 4.A Sub-CPN Index (divisibility map)

| # | Sub-CPN | Status | Owner hat | REQ range |
|---|---|---|---|---|
| **SC-01** | Bootstrap LLM split (`t-awaken-llm-bootstrap` + `t-awaken-llm-followup`) | ✅ DONE | CPN architect | REQ-101…105 |
| **SC-02** | Probe fanout (compose/instantiate/reduce) | ✅ DONE (workaround) | Go runtime | REQ-201…208 |
| **SC-03** | Report persistence + 24 h cache | ⚠️ PARTIAL | Go runtime | REQ-301…305 |
| **SC-04** | Tool-forge `RegisterBatch` | ✅ DONE | Go runtime | REQ-401…403 |
| **SC-05** | Toolbox catalogue + personality digest | ✅ DONE | LLM eng. | REQ-501…506 |
| **SC-06** | First-turn A2UI emission | ✅ DONE | Frontend/A2UI | REQ-601…603 |
| **SC-07** | Turn ≥2 prompt injection of catalogue | ❌ MISSING | LLM eng. | REQ-701…704 |
| **SC-08** | Observability spine (events + otel) | ⚠️ PARTIAL | Observability | REQ-801…804 |
| **SC-09** | Native `NodeKindInstantiate` for fanout (replace workaround) | ❌ MISSING | CPN architect | REQ-901…903 |
| **SC-10** | Sandbox wrappers (bwrap/firejail) | ❌ MISSING | Security | REQ-1001…1004 |
| **SC-11** | Help-parser sub-CPN | ❌ MISSING | LLM eng. | REQ-1101…1104 |
| **SC-12** | Tool-synthesis sub-CPN (from help-parser output) | ❌ MISSING | CPN architect | REQ-1201…1205 |
| **SC-13** | First-run HITL for synthesized tools | ❌ MISSING | Security | REQ-1301…1303 |
| **SC-14** | Capability ACL (`sha256(binary)` + flag rules) | ❌ MISSING | Security | REQ-1401…1403 |
| **SC-15** | Model fallback chain | ❌ MISSING | LLM eng. | REQ-1501…1503 |
| **SC-17** | E2E harness + compose latency benchmarks | ⚠️ PARTIAL | Test eng. | REQ-1701…1704 |
| **SC-18** | Lexicon priority fixture (AC-011 closeout) | ⚠️ PARTIAL | LLM eng. | REQ-1801…1802 |

---

### SC-01 — Bootstrap LLM Split ✅

- **Trigger:** session boot token on `p-awaken-trigger`.
- **Places:** `p-awaken-system-prompt` (in) → `p-awaken-plan` (out).
- **Transitions:** `t-awaken-llm-bootstrap` (prompt-only), `t-awaken-llm-followup` (post-shell-result).
- **Host-gate:** N/A (pure LLM call).
- **HITL:** none.
- **Observability:** `awakening.llm.bootstrap.completed`.
- **Failure modes:** LLM timeout → fail session (no fallback per REQ-006).
- **REQ-101**: Bootstrap MUST emit a single `AwakeningProbePlan`, not a classification.
- **REQ-102**: Followup MUST consume probe results and emit `AwakeningReport`.
- **REQ-103**: Neither transition MAY share a place with the other (no deadlock).
- **REQ-104**: Bootstrap MUST NOT accept sentinel-token fallbacks (explicit removal of legacy behavior).
- **REQ-105**: Both transitions MUST use `awakeningPinnedModel` unless overridden by SC-15.

### SC-02 — Probe Fanout ✅ (with SC-09 workaround)

- **Trigger:** plan token on `p-awaken-plan`.
- **Places:** `p-awaken-subnet-spec` (intermediate) → `p-awaken-probe-results[slug]` (one per probe) → reducer.
- **Transitions:** `t-awaken-probe-compose`, `t-awaken-probe-instantiate`, N × `t-probe-<slug>`, `t-awaken-probe-reduce`.
- **Host-gate:** `introspection` (every probe).
- **HITL:** none (auto-approved by SEC-001).
- **Observability:** `awakening.probe.{composed,fired,reduced}`.
- **Failure modes:** individual probe failure → recorded; AND-join tolerates up to N-1 failures but reducer must mark `degraded=true`.
- **REQ-201**: Compose MUST validate each probe against the introspection allow-list before instantiation.
- **REQ-202**: Compose MUST cap at 16 probes (CON-002).
- **REQ-203**: The sub-CPN MUST be ephemeral (CON-003).
- **REQ-204**: Reducer MUST be a distinct transition, never inlined (PAT-001).
- **REQ-205**: Reducer MUST emit `awakening.probe.reduced` with success/fail counts.
- **REQ-206**: Duplicate slugs in the plan MUST be rejected at compose time.
- **REQ-207**: Probe stdout captured MUST be truncated to 4 KiB.
- **REQ-208**: Probe execution timeout per probe MUST be 3 s.

### SC-03 — Persistence & Cache ⚠️

- **Trigger:** report token on `p-awakening-report`.
- **Places:** `p-awakening-report` → `p-awakening-snapshot`.
- **Host-gate:** N/A.
- **HITL:** none.
- **REQ-301**: Snapshot row MUST carry `source="awakening"` and `ttl=24h`.
- **REQ-302**: Re-boot within TTL MUST call `LookupCachedSnapshot` and skip bootstrap + fanout.
- **REQ-303**: Cache hit MUST still emit a first-turn A2UI card (re-render path).  *(current gap)*
- **REQ-304**: Cache hit path MUST emit `awakening.cache.hit` event.
- **REQ-305**: Cache invalidation MUST be triggered by: (a) TTL, (b) binary-SHA256 change of any registered tool, (c) manual admin flush.

### SC-04 — Tool-Forge RegisterBatch ✅

- **REQ-401**: `RegisterBatch(ctx, registry, report, logger)` MUST return the slice of registered tool names.
- **REQ-402**: Duplicate `(session_id, tool_name)` MUST be silently skipped (CON-005).
- **REQ-403**: Registration MUST populate `toolbox` and `hashtags` columns in `tool_registry`.

### SC-05 — Toolbox Catalogue + Personality Digest ✅

- **REQ-501**: `BuildToolboxCatalogue(report, lexicon)` MUST return a deterministic JSON with sorted keys.
- **REQ-502**: Catalogue MUST truncate per-bucket tool lists to 16 entries (progressive-disclosure).
- **REQ-503**: `PersonalityDigest(catalogue, os, shell, lexicon)` MUST be `sha256` of LF-joined byte-stable inputs.
- **REQ-504**: Digest MUST change when any input byte changes (CON-004).
- **REQ-505**: Digest MUST NOT change across runs when inputs are identical.
- **REQ-506**: Lexicon excerpt MUST be bounded at 32 top-priority tags (see SC-18).

### SC-06 — First-Turn A2UI Emission ✅

- **REQ-601**: Envelope version MUST be `"0.8"`.
- **REQ-602**: Envelope MUST include `summary`, `toolbox`, `hashtags` components.
- **REQ-603**: Envelope MUST be emitted once per awakening run (cache hits included).

### SC-07 — Turn ≥2 Prompt Injection ❌

- **Trigger:** any user message after awakening.
- **REQ-701**: Prompt assembler MUST fetch the latest `AwakeningReport` snapshot.
- **REQ-702**: Prompt assembler MUST build `EnvironmentAwarenessBlock()` from `BuildToolboxCatalogue()`.
- **REQ-703**: The block MUST be prefix-cached keyed by `PersonalityDigest()`.
- **REQ-704**: On digest change, the cache entry MUST be invalidated and rebuilt.

### SC-08 — Observability Spine ⚠️

- **REQ-801**: All events in §4.6 MUST be emitted as both `slog` and OTEL spans.
- **REQ-802**: OTEL span name format MUST be `brae.awakening.<event>`.
- **REQ-803**: Latency SLOs: p50 ≤ 8 s, p95 ≤ 15 s, p99 ≤ 30 s (session deadline).
- **REQ-804**: An SLO breach MUST fire `awakening.slo.breached` with the offending stage.

### SC-09 — Native NodeKindInstantiate Fanout ❌

Currently `t-awaken-probe-instantiate` uses a `NodeKindTool` workaround; migrate to native `NodeKindInstantiate`.

- **REQ-901**: `t-awaken-probe-instantiate` MUST be `NodeKindInstantiate` with an `InstantiateConfig` pointing at the composed spec.
- **REQ-902**: Instantiation MUST use `AuthoredFlowRepository` with an ephemeral in-memory backend (no DB write).
- **REQ-903**: Workaround code path MUST be deleted (not gated by flag) once native path passes `fanout/executor_test.go`.

### SC-10 — Sandbox Wrappers ❌

- **REQ-1001**: On Linux, probe subprocesses MUST be wrapped with `bwrap --ro-bind / / --dev /dev --tmpfs /tmp --unshare-net`.
- **REQ-1002**: On non-Linux, `firejail --net=none --private-tmp --read-only=/` MUST be used.
- **REQ-1003**: If neither wrapper is present, awakening MUST fail fast with `error_class="sandbox_missing"`.
- **REQ-1004**: The wrapper CLI choice and its version MUST be captured in the report's `Host` block.

### SC-11 — Help-Parser Sub-CPN ❌

- **Trigger:** each binary in `report.Binaries` that lacks a known manifest.
- **Places:** `p-help-binary` → `p-help-raw` → `p-help-schema`.
- **Transitions:** `t-help-invoke` (`<bin> --help`), `t-help-llm-parse` (LLM → JSON flag schema), `t-help-validate`.
- **Host-gate:** `introspection`.
- **REQ-1101**: Each binary MUST be probed with `--help` and `-h` (AND-join best-effort).
- **REQ-1102**: LLM parse MUST emit `{flags[], subcommands[], examples[]}` JSON.
- **REQ-1103**: Parse output MUST pass a JSON-schema validator before flowing to SC-12.
- **REQ-1104**: Parse failure MUST NOT abort awakening; binary is emitted without schema.

### SC-12 — Tool-Synthesis Sub-CPN ❌

- **Trigger:** validated help-schema from SC-11.
- **Transitions:** `t-synth-materialise` (NodeKindSynthesize), `t-synth-instantiate` (NodeKindInstantiate, first-run only).
- **REQ-1201**: Synthesized manifests MUST carry `kind="synthesized"` and `origin="brae-awakens/help-parser"`.
- **REQ-1202**: Synthesized manifests MUST carry a `provenance_sha256` of their source help-schema.
- **REQ-1203**: Materialisation MUST NOT auto-register (requires SC-13 approval).
- **REQ-1204**: Re-synthesis MUST produce byte-stable output for identical inputs.
- **REQ-1205**: Synthesis failures are non-fatal; logged as `awakening.synthesis.failed`.

### SC-13 — First-Run HITL ❌

- **Trigger:** first attempted call of a `kind="synthesized"` tool.
- **REQ-1301**: Call MUST block on an A2UI `hitl` component with full preview (command, flags, scope).
- **REQ-1302**: Approval MUST be recorded as `(session_id, tool_name, sha256)` row with a timestamp.
- **REQ-1303**: Subsequent calls with the same `(tool_name, sha256)` MUST auto-approve; sha256 drift MUST re-trigger HITL.

### SC-14 — Capability ACL ❌

- **REQ-1401**: ACL decisions MUST key on `(binary_sha256, flag_tuple)` — never raw strings (SEC-005).
- **REQ-1402**: Denied decisions MUST include a human-readable `reason` propagated to A2UI.
- **REQ-1403**: ACL rules MUST be hot-reloadable from `config/host-acl.yaml`.

### SC-15 — Model Fallback Chain ❌

- **REQ-1501**: Primary: `google/gemini-2.5-flash`; fallbacks in order: `anthropic/claude-haiku-4-5`, `openai/gpt-4o-mini`.
- **REQ-1502**: Fallback MUST trigger only on provider 5xx / rate-limit / timeout; auth errors fail fast.
- **REQ-1503**: Fallback use MUST be recorded as `awakening.llm.fallback_used` event with the chosen model.

### SC-17 — E2E Harness + Benchmarks ⚠️

- **REQ-1701**: `integration/awakening_e2e_test.go` MUST spin up docker-compose (Postgres + LLM mock) and exercise the full root CPN.
- **REQ-1702**: Benchmark `BenchmarkProbeFanoutCompose` MUST enforce ≤ 5 ms per compose at N=16.
- **REQ-1703**: Benchmark `BenchmarkAwakeningE2E` MUST enforce ≤ 12 s with mocked LLM (+1 s probe quota).
- **REQ-1704**: CI MUST fail on > 10 % regression vs the stored baseline.

### SC-18 — Lexicon Priority Fixture ⚠️

- **REQ-1801**: `SelectTopTags(n=32)` MUST pass `lexicon_priority_golden_test.go` against a committed fixture.
- **REQ-1802**: Priority formula MUST be `p = freq * 0.6 + recency * 0.4`; committed as a pure function.

## 5. Acceptance Criteria

General (root CPN):

- **AC-001**: Given a fresh session, When awakening fires, Then a snapshot row with `source="awakening"` exists within 30 s.
- **AC-002**: Given a probe plan with a denied command, When composer validates, Then the awakening fails with `error_class="sec_denied"` before any subprocess runs.
- **AC-003**: Given two sessions with identical host state, When both awaken, Then their `PersonalityDigest` are byte-equal.
- **AC-004**: Given a cache hit within 24 h, When awakening fires, Then no LLM call is made and a first-turn A2UI card is still emitted (closes SC-03 gap).
- **AC-005**: Given a synthesized tool, When first called, Then execution is blocked pending HITL approval (SC-13).
- **AC-007**: Given an LLM provider 503, When bootstrap fires, Then the fallback chain is consulted and the event `awakening.llm.fallback_used` is emitted.
- **AC-008**: Given a sandbox binary missing, When awakening fires on Linux, Then it fails fast with `error_class="sandbox_missing"` (no probes run).
- **AC-009**: Given 16 probes, When compose runs, Then p95 compose latency ≤ 5 ms over 1000 runs.
- **AC-010**: Given a binary whose SHA256 changes between runs, When awakening fires, Then the cache is invalidated and the full loop re-runs.

Per-sub-CPN ACs are expressed as the REQ-xx leaf itself (the REQ **is** the test target).

## 6. Test Automation Strategy

- **Test Levels.** Unit (per package), Integration (CPN-level with mocked LLM+registry), E2E (docker-compose).
- **Frameworks.** Go stdlib `testing`, `testify/require`, `golden` files under `cpn/awakens/testdata/`.
- **Test Data.** Golden fixtures for `AwakeningReport`, `AwakeningProbePlan`, A2UI envelope. Lexicon fixture under `cpn/tools/testdata/lexicon_golden.yaml`.
- **CI/CD.** GitHub Actions — all sub-CPNs must pass unit + integration; E2E gated behind `make e2e` and CI matrix.
- **Coverage.** ≥ 85 % per package (`go test -coverprofile`), strict for `cpn/awakens/fanout` (≥ 95 %).
- **Performance.** `go test -bench -benchmem`; baselines committed under `bench/awakening_baseline.txt`; CI compares via `benchstat` with 10 % tolerance.
- **Security tests.** Introspection allow-list fuzz test (`introspection_fuzz_test.go`), sandbox wrapper contract test (SC-10).

## 7. Rationale & Context

**Why divisible?** The prior monolithic specs grew to thousands of lines and blurred ownership (prompt eng vs CPN vs security vs infra). Decomposing into sub-CPNs matches the runtime's own model: each sub-CPN is independently shippable, reviewable by one expert hat, and observable as a distinct OTEL span. This mirrors brae philosophy ("if a problem is divisible, factor it").

**Why fail-fast (no fallback)?** The bootstrap-fix spec resolved a deadlock caused by a sentinel-token fallback that masked LLM outages. Silent degradation is worse than a visible fail — session truly depends on an accurate host report.

**Why `NodeKindInstantiate` migration (SC-09)?** The `NodeKindTool` workaround is hard to introspect (no sub-CPN span, no AuthoredFlow record). Migrating unlocks tool-synthesis (SC-12) which reuses the same runtime path.

**Why capability ACL on SHA256, not strings (SEC-005)?** A regex-based ACL is easily bypassed by binary substitution (e.g., a shadowed `git` in `~/bin`). Hashing closes that gap.

**Why SC-16 was removed.** A quarantine escape hatch contradicts REQ-006's fail-fast rail. If the fanout path misbehaves in production, the remedy is to fix it or revert the release, not to route around it silently with a mode flag. Removed prior to shipping.

**Why 18 sub-CPNs and not fewer?** Each of SC-10…SC-15 is independently reversible and owned by a different hat. Bundling them hides risk and slows review. The ship-order index (§10) shows which are parallel-safe vs. gating.

**Expert consultation summary:**
- *CPN architect:* endorsed SC-09 migration priority and the reducer-as-distinct-transition pattern (PAT-001).
- *Go runtime:* flagged CON-003 (no persist round-trip) as easy to regress — recommended a lint rule (not in this spec scope).
- *Security:* pushed SEC-005 upgrade from regex to SHA256 and required SC-10 as a prerequisite for SC-11/12 (no help-parsing without sandbox).
- *LLM eng:* recommended 32-tag cap (REQ-506) from prompt-budget analysis; confirmed the digest input order.
- *Observability:* requested dual emission (slog + OTEL) and the `brae.awakening.*` namespace (REQ-802).
- *Testing:* set the 10 % regression tolerance and required E2E-behind-`make e2e` not in default CI.
- *Frontend/A2UI:* confirmed v0.8 envelope shape is stable and rehydration already reads the same snapshot row.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001** OpenRouter / Vertex — primary LLM provider; fallback chain in SC-15.
- **EXT-002** Postgres — `cpn_snapshots`, `tool_registry` tables.

### Infrastructure Dependencies
- **INF-001** `bwrap` (Linux ≥ 5.x) OR `firejail` — required by SC-10.
- **INF-002** Docker-compose test harness — required by SC-17.

### Data Dependencies
- **DAT-001** `cpn/tools/lexicon.yaml` — versioned, hash-stamped in every report.

### Platform Dependencies
- **PLT-001** Go ≥ 1.24 (generics + structured logging).
- **PLT-002** OTEL SDK (tracing + metrics).

### Compliance Dependencies
- **COM-001** User data boundary — awakening MUST NOT emit `$HOME` contents, SSH key material, or env var values beyond `$SHELL`, `$PATH`. Enforced by SEC-002.

## 9. Examples & Edge Cases

**Example — plan rejected at compose time (REQ-201):**
```json
{"probes":[{"slug":"curl-x","command":"curl https://evil.example","expect":""}]}
// → reject; not in introspection allow-list; emit awakening.failed{error_class:"sec_denied"}
```

**Example — cache hit re-render (REQ-303):**
```
t=0    first boot; full loop runs; snapshot id=S1 saved
t=+1h  reboot; LookupCachedSnapshot(session_key) → S1
        → skip bootstrap + fanout
        → still emit A2UI first-turn card from S1
        → awakening.cache.hit{age_ms: 3600000}
```

**Edge case — binary SHA256 drift (REQ-305c / AC-010):**
```
snapshot S1 registered git@sha256:abc...
next boot: which git → /usr/bin/git, sha256=def... (upgraded)
→ cache invalidated; full loop re-runs; new snapshot S2.
```

**Edge case — probe timeout (REQ-208):**
```
probe "ls /mnt/slow" exceeds 3 s
→ Stdout truncated, ExitCode=-1, "timeout" recorded
→ reducer marks degraded=true but awakening proceeds
```

## 10. Validation Criteria (ship order)

Ship-order index — respect dependency arrows:

```
Live (no action):      SC-01, SC-02*, SC-04, SC-05, SC-06   (*w/ workaround)
Wave 1 (close gaps):   SC-03, SC-08, SC-18
Wave 2 (prereqs):      SC-09, SC-10, SC-15
Wave 3 (synthesis):    SC-11  →  SC-12  →  SC-13
Wave 4 (hardening):    SC-14, SC-17 (E2E + benches)
Turn ≥2 consumer:      SC-07  (parallel; depends only on SC-05 which is live)
```

Per-wave validation:
- **Wave 1 exit:** all OTEL events in §4.6 visible in staging; cache hit AC-004 passes.
- **Wave 2 exit:** `NodeKindTool` workaround deleted; sandbox missing fails fast; fallback event observed in chaos test.
- **Wave 3 exit:** a synthesized tool is registered, first-run HITL blocks, approved run succeeds, re-run skips HITL.
- **Wave 4 exit:** benchmarks enforce SLOs; E2E test green in CI; ACL denies a flag-variant with correct `reason`.

## 11. Related Specifications / Further Reading

- `spec-architecture-brae-jit-cpn-builder.md` — consumer of `PersonalityDigest()` for turn ≥2 prefix caching (SC-07).
- `spec-architecture-model-registry-and-a2ui-management.md` — source of truth for model IDs used by SC-15.
- `spec-architecture-cpn-synthesis-instantiate.md` — runtime semantics behind SC-09 and SC-12.
- `spec-architecture-tool-forge-cpn.md` — the registry SC-04 writes into.
- `spec-architecture-host-gate-security-policy.md` — defines the `introspection` / `read` / `mutating` / `dangerous` classes used by SEC-001 and SC-14.
- Source anchors: `back/go-assistant/cpn/awakens/topology.go`, `.../report.go`, `.../personality_catalogue.go`, `.../fanout/{composer,reduce,executor}.go`, `.../toolforge.go`, `.../a2ui.go`, `back/go-assistant/internal/app/session_service_awakening.go`.
