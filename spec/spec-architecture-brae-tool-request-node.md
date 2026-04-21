---
title: Brae Tool-Request Node — NodeKindToolRequest and Communication-Space Discipline
version: 0.2
date_created: 2026-04-20
last_updated: 2026-04-20
owner: liwaisi-tech / brae agent team
tags: [architecture, agent, cpn, transition, tool-request, communication-space, hashtag]
changelog:
  - "0.2 (2026-04-20): P0 panel-review fixes — event-naming convention aligned with taxonomy (`<domain>.<subject>.<verb>`), explicit intent-validation contract, hashtag normalization references taxonomy REQ-NORM-001, lint rule formalized at materialization time, cache-hit and quota visibility surfaced to the agent's personality, activity-bubble copy tightened, three few-shot examples for tool_request emission."
---

# Introduction

This specification defines **`NodeKindToolRequest`**, the new CPN transition kind through which `brae` emits a structured intent (hashtags + natural-language phrase) and receives a `ToolMatchSet` back from the retriever. It also defines the new colour `ColorToolMatchSet` and the **communication-space discipline** that ensures retrieval results never bypass the Observation space en route to Computation. It addresses gap **G4** of the JIT CPN Builder design.

## 1. Purpose & Scope

### Purpose

- Give the LLM a sanctioned, schema-bound way to request tools mid-flow without parsing free-form prose for `#hashtags` (Option A in the design brief; rejected as brittle and hijackable).
- Bind the request to a typed token (`ColorToolMatchSet`) that the JIT CPN Builder can consume as input.
- Enforce the paper's communication-space rule (§4): retrieval is an **observation**, not a **computation**. The match set MUST land on an Observation place before a Computation transition can use it.
- Surface tool requests in the session event stream so the user sees `brae` looking up tooling rather than being silently rerouted.

### In scope

- A new node kind constant `NodeKindToolRequest` and its handler `cpn/fire_tool_request.go`.
- A new colour constant `ColorToolMatchSet` and its serialisation rules.
- Schema definitions for the structured LLM call that triggers the request.
- Space-violation enforcement in `Place.Deposit` extended for the new colour.
- An A2UI activity event surfaced through the existing event stream (`tool_request.transition.started` / `tool_request.transition.completed`).

> **Event-naming convention.** All events in this spec follow the canonical `<domain>.<subject>.<verb>` lowercase dot-form mandated by `spec-architecture-brae-toolbox-taxonomy.md` (event-naming requirement). No other shape is permitted.

### Out of scope

- The retriever itself (`spec-architecture-brae-tool-retriever.md`).
- The composer that consumes the resulting `ToolMatchSet` (`spec-architecture-brae-jit-cpn-builder.md`).
- LLM prompt engineering for *when* to emit a tool request (covered by personality/prompt updates in the awakening extension and downstream).
- Multi-step intent refinement (e.g. conversational "are you sure?" loop). Out of scope for v0.1; the LLM either emits a request or it does not.

### Intended audience

CPN engine engineers, AI behaviour owners (system prompts that instruct the LLM in when to fire `tool_request`), frontend engineers (activity surfacing).

### Assumptions

- The toolbox taxonomy and retriever specs are implemented; `ToolRetriever` is wired into `SessionService` and reachable from the transition handler via the existing dependency-injection pattern.
- The LLM provider supports structured-output (function-call) requests. OpenRouter's `tools` parameter is the current path.
- The existing six node kinds (`tool, llm, validate, subnet, observer, hitl`) plus the runtime-authored ones (`register_tool, synthesize, instantiate, topology_mutate`) remain unchanged. This spec adds one new kind.

## 2. Definitions

| Term | Definition |
|---|---|
| **NodeKindToolRequest** | New transition kind. Consumes an `Intent` token, produces a `ToolMatchSet` token. |
| **Intent token** | A token of colour `ColorIntent` carrying hashtags, natural-language phrase, and caps. |
| **ToolMatchSet token** | A token of colour `ColorToolMatchSet` carrying the retriever's ranked output. |
| **Communication space** | One of `Surface | Observation | Computation`, per Borghoff/Bottoni/Pareschi 2025 §4. |
| **Space violation** | Attempt to deposit a token whose `Space` field disagrees with the Place's declared space, or whose flow would skip a layer (Surface → Computation directly). |
| **Tool request schema** | The JSON-Schema fragment registered with the LLM provider as a callable function. |

## 3. Requirements, Constraints & Guidelines

### Functional requirements

- **REQ-001**: A new constant `NodeKindToolRequest cpn.NodeKind = "tool_request"` MUST be added in `cpn/kinds.go`.
- **REQ-002**: A new colour `ColorIntent` MUST be added in `cpn/colors.go` for the input token. Payload type: `cpn.Intent` (defined in the retriever spec).
- **REQ-003**: A new colour `ColorToolMatchSet` MUST be added in `cpn/colors.go` for the output token. Payload type: `cpn.ToolMatchSet`.
- **REQ-004**: A new firing handler `cpn/fire_tool_request.go` MUST consume one input `ColorIntent` token, call `ToolRetriever.Find(ctx, intent)`, and deposit one output `ColorToolMatchSet` token. On retriever error, deposit a token of colour `ColorError` on the transition's error place.
- **REQ-005**: The handler MUST emit two CPN events: `tool_request.transition.started` (with the intent) when firing begins, and `tool_request.transition.completed` (with the match summary: count, top qualified names, latency, `cache_hit`, `cache_age_ms`) when firing succeeds. Names follow the canonical `<domain>.<subject>.<verb>` form per `spec-architecture-brae-toolbox-taxonomy.md`.
- **REQ-006**: An `LLMTool` schema named `tool_request` MUST be registered with the LLM provider when a transition of kind `tool_request` exists in the topology. The schema is defined in §4.2.
- **REQ-007**: The LLM-driven path that produces the intent token (typically a transition of kind `llm` upstream) MUST validate the structured-output payload against the `tool_request` schema before depositing the `ColorIntent` token.
- **REQ-008**: The linter MUST inspect each output Place of any `tool_request` transition; if `Place.Space != SpaceObservation`, it MUST return `ErrSpaceViolationToolRequest` carrying the offending place id. Lint MUST run at **materialization time, BEFORE the topology is registered** in the executor; invalid topologies MUST NOT be persisted, materialised, or fired.
- **REQ-009**: Any downstream transition that consumes the `ColorToolMatchSet` token MUST have at least one input Place in `Observation` space; depositing the consumed-then-derived result back into a Computation place is the bridge transition's responsibility (typically the JIT composer).
- **REQ-010**: The handler MUST honour a per-firing timeout of 2 seconds (configurable via `ToolRequestConfig.Timeout`). On timeout, the firing fails and the transition routes a `ColorError` token to the error place; the LLM may retry on the next turn.
- **REQ-011**: The handler MUST be deduplicated within a single session: identical consecutive intents (same hashtags + same NL within 5 seconds) MUST return the previous `ToolMatchSet` from a small in-flight cache rather than re-querying the retriever.
- **REQ-INTENT-VAL**: Before producing a `ColorIntent` token, the handler MUST validate the structured-output payload as follows:
  1. Normalise hashtags via the canonical procedure (see SEC-002).
  2. If `len(hashtags_after_normalization) == 0` AND `len(strings.TrimSpace(nl)) == 0`, the handler MUST return `ErrEmptyIntent` to the transition's error place and MUST NOT produce an Intent token.
  3. Otherwise, the (possibly empty) hashtag list together with the trimmed `nl` becomes the payload of the `ColorIntent` token.
  Invalid hashtag tokens are silently dropped per the canonical normalization procedure (they do not, on their own, cause `ErrEmptyIntent`).

### Security requirements

- **SEC-001**: The `tool_request` schema MUST cap `hashtags` at 12 entries and `nl` at 1 024 characters. Any LLM emission exceeding these caps MUST be rejected before the intent token is produced.
- **SEC-002**: Hashtag normalization is **not** redefined here. LLM-emitted hashtags MUST pass through the canonical procedure defined in `spec-architecture-brae-toolbox-taxonomy.md` (REQ-NORM-001) before becoming part of the `ColorIntent` token. Invalid hashtag tokens are silently dropped by that procedure.
- **SEC-003**: The `ToolMatchSet` deposited on the Observation place MUST contain ONLY qualified names, scores, and trace metadata. It MUST NOT include `BinaryPath`, `BinarySHA256`, `Provenance`, or `RegisteredBy` from the underlying `ToolEntry`.
- **SEC-004**: Activity events emitted to the session stream MUST NOT include `Provenance` or `BinaryPath`. The user sees qualified names and scores only.

### Behaviour & product requirements

- **BEH-001**: If the LLM emits a `tool_request` whose `hashtags` are all rejected by normalisation AND `nl` is empty, the handler MUST return `ErrEmptyIntent`. The LLM may retry.
- **BEH-002**: If the resulting `ToolMatchSet` is empty after retriever widening, the handler MUST still produce a token (with `Matches = []`) and emit `transition.tool_request.completed` with `count = 0`. Downstream behaviour decides whether to ask the user for clarification.
- **BEH-003**: The session event stream MUST surface `tool_request` activity as a distinct A2UI activity bubble so the user can see the agent shopping for tools rather than the UI freezing.

### Constraints

- **CON-001**: The tool-request transition MUST NOT itself execute any tool. It only translates intent into ranked candidates.
- **CON-002**: The handler MUST NOT block on HITL approval. Tool *requests* are introspective; tool *use* (subsequent transitions) is what may require approval.
- **CON-003**: Per-firing handler latency MUST be ≤ 500 ms p95 (inclusive of retriever; matches retriever's CON-003).
- **CON-004**: A single session MUST NOT fire more than 16 `tool_request` transitions; further attempts MUST return `ErrToolRequestQuotaExceeded` and emit a `tool_request.quota.exceeded` event. This is a guardrail against runaway loops.

### Guidelines

- **GUD-001**: Topology authors SHOULD pair every `tool_request` transition with a downstream `instantiate` transition (the JIT composer) and an HITL gate if the resulting tools are not all in the safe band.
- **GUD-002**: The system prompt SHOULD instruct `brae` to call `tool_request` BEFORE attempting tools it is not certain are present. The awakening extension spec covers this (BEH/GUD entries there).
- **GUD-003**: The 5-second deduplication cache prevents oscillation; do NOT raise the window without measurement, since legitimate refinements within seconds are common.
- **GUD-CACHE-VIS**: When the next turn's personality is assembled and the most recent `tool_request.transition.completed` event carried `cache_hit = true`, the system prompt SHOULD include a one-line note such as: `"Last tool_request returned a cached match (age 1.2s); call again with different hashtags or NL if you need a fresh result."` The note is informational only — it MUST NOT block re-firing.
- **GUD-QUOTA**: The remaining `tool_request` budget for the session MUST be exposed on `cpn.Personality` as `ToolRequestQuotaRemaining int` (initial value: 16). From turn ≥ 2, the personality assembler MUST inject a line into the system-prompt block of the form `"Tool-request budget: X of 16 calls remaining"` (where X is the current `ToolRequestQuotaRemaining`). Turn 1 omits the line so first-turn prompts stay terse.

### Patterns

- **PAT-001**: `tool_request` always sits at the boundary between Surface (LLM emission) and Observation (match set). It is a *bridge* transition whose input space MAY be Surface or Computation but whose output MUST be Observation.
- **PAT-002**: The dedup cache is keyed by `(sessionID, hash(canonical(intent)))`, TTL 5 seconds, capacity 32. Eviction is LRU.
- **PAT-003**: All errors route to a single error place per transition, consistent with the existing executor convention.

## 4. Interfaces & Data Contracts

### 4.1 New CPN constants

```go
// cpn/kinds.go
const NodeKindToolRequest NodeKind = "tool_request"

// cpn/colors.go
const (
    ColorIntent        ColorSet = "Intent"
    ColorToolMatchSet  ColorSet = "ToolMatchSet"
)
```

### 4.2 LLM-side schema (registered as `LLMTool`)

```json
{
  "name": "tool_request",
  "description": "Request a ranked set of tools matching an intent. Use BEFORE attempting tools you are not certain exist. Returns ranked qualified names you can then invoke. Few-shot examples follow in §4.2.1.",
  "parameters": {
    "type": "object",
    "properties": {
      "hashtags": {
        "type": "array",
        "items": {"type": "string", "pattern": "^#?[A-Za-z][A-Za-z0-9-]{0,31}$"},
        "maxItems": 12,
        "description": "Controlled-vocabulary tags. Prefer kind tags (tools, read, write, network, compute, transform) plus domain tags (pdf, image, git, html, json, ...). Leading '#' is optional."
      },
      "nl": {
        "type": "string",
        "maxLength": 1024,
        "description": "Brief natural-language phrase describing what you need to do."
      },
      "max_toolboxes": {"type": "integer", "minimum": 1, "maximum": 6, "default": 3},
      "max_tools":     {"type": "integer", "minimum": 1, "maximum": 12, "default": 5}
    },
    "required": ["hashtags"]
  }
}
```

#### 4.2.1 Few-shot examples (inline in the LLM-side schema preamble)

Three diverse anchors are provided so the LLM does not collapse onto a single shape. These examples MUST be included in the schema description block when the `tool_request` LLMTool is registered.

```json
// Example 1 — read text from a PDF
{
  "name": "tool_request",
  "arguments": {
    "hashtags": ["tools", "pdf", "read", "transform"],
    "nl": "extract the readable text out of the PDF the user just attached",
    "max_tools": 5
  }
}

// Example 2 — fetch a remote web page
{
  "name": "tool_request",
  "arguments": {
    "hashtags": ["tools", "network", "html", "read"],
    "nl": "fetch the HTML body of a public URL so I can summarise it",
    "max_toolboxes": 2,
    "max_tools": 4
  }
}

// Example 3 — query a local SQLite database
{
  "name": "tool_request",
  "arguments": {
    "hashtags": ["tools", "sqlite", "compute", "read"],
    "nl": "run a SELECT against a local sqlite file and return rows as JSON",
    "max_tools": 6
  }
}
```

### 4.3 Transition specification (per topology)

```yaml
- id: t-tool-request
  kind: tool_request
  inputs:
    - place: p-tool-request-intent       # ColorIntent, Space: Surface or Computation
  outputs:
    - place: p-tool-request-matches      # ColorToolMatchSet, Space: Observation (REQUIRED)
  errors:
    - place: p-tool-request-error        # ColorError, Space: Observation
  config:
    timeout_ms: 2000
    max_per_session: 16
```

### 4.4 Token payloads

```go
// On p-tool-request-intent
Token{
    Color:   ColorIntent,
    Space:   SpaceSurface,         // typically; may be Computation
    Payload: cpn.Intent{ Hashtags: ["tools","pdf","read"], NL: "...", MaxTools: 5 },
}

// On p-tool-request-matches (after firing)
Token{
    Color:   ColorToolMatchSet,
    Space:   SpaceObservation,     // enforced
    Payload: cpn.ToolMatchSet{ Matches: [...], Toolboxes: [...], Trace: {...} },
}
```

### 4.5 Event payloads (existing event-bus envelope)

```json
{
  "event": "tool_request.transition.started",
  "session_id": "...",
  "transition_id": "t-tool-request",
  "intent": {
    "hashtags": ["tools","pdf","read"],
    "nl": "extract text from a scanned PDF"
  },
  "ts": "2026-04-20T18:31:02Z"
}

{
  "event": "tool_request.transition.completed",
  "session_id": "...",
  "transition_id": "t-tool-request",
  "match_count": 2,
  "top": ["pdf/pdf-to-text@0.1.0", "pdf/pdf-info@0.1.0"],
  "toolboxes": ["pdf"],
  "latency_ms": 38,
  "cache_hit": false,
  "cache_age_ms": 0,
  "ts": "2026-04-20T18:31:02Z"
}

{
  "event": "tool_request.quota.exceeded",
  "session_id": "...",
  "transition_id": "t-tool-request",
  "limit": 16,
  "ts": "2026-04-20T18:31:02Z"
}
```

When a firing is served from the dedup cache, `cache_hit` MUST be `true` and `cache_age_ms` MUST be the milliseconds since the cached `ToolMatchSet` was first computed.

### 4.6 A2UI activity bubble (frontend renders this)

```json
{
  "components": [
    { "type": "activity",
      "props": {
        "kind": "tool-request",
        "label": "Buscando herramientas para: extract text from a scanned PDF",
        "tags": ["tools","pdf","read"],
        "state": "running"
      }
    }
  ]
}
```

UX copy rules for the bubble:

- `label` MUST be truncated to **80 characters** (UTF-8 grapheme-aware); longer phrases MUST be truncated with a trailing ellipsis (`…`).
- Locale MUST be inherited from `Personality.Language`. Templates MUST exist for **at least `en` and `es`** in v0.1:
  - `en`: `"Looking up tools for: {nl}"`
  - `es`: `"Buscando herramientas para: {nl}"`
  - Additional locales are out of scope for v0.1.
- `tags` MUST be displayed **without** the leading `#` (the bubble adds visual styling; the data is bare tokens).
- When the request completes, the same bubble is updated with `state: "done"` and the matched qualified names.

## 5. Acceptance Criteria

- **AC-001**: Given a topology containing a `tool_request` transition with output Place declared `Space = Computation`, when the topology is submitted for materialization, then the linter MUST fail with `ErrSpaceViolationToolRequest` carrying the offending place id, lint MUST run BEFORE the topology is registered in the executor, AND the topology MUST NOT be persisted, materialised, or fired.
- **AC-002**: Given the LLM emits a valid `tool_request` call, when the upstream `llm` transition fires, then a `ColorIntent` token MUST appear on the input place AND the `tool_request` transition MUST become enabled.
- **AC-003**: Given the `tool_request` transition fires successfully, when firing completes, then a `ColorToolMatchSet` token MUST be deposited on the output Observation place AND `tool_request.transition.completed` MUST be emitted with non-zero `latency_ms` and with `cache_hit`/`cache_age_ms` fields present.
- **AC-004**: Given the retriever returns an error, when the transition fires, then a `ColorError` token MUST land on the error place AND the original `ColorIntent` token MUST NOT remain on the input place (consume-on-launch semantics preserved).
- **AC-005**: Given two consecutive identical intents within 5 seconds in the same session, when the second fires, then the retriever MUST be called only once AND the second firing MUST return the cached match set.
- **AC-006**: Given a session has fired 16 `tool_request` transitions, when a 17th attempts to fire, then it MUST fail with `ErrToolRequestQuotaExceeded` AND a `tool_request.quota.exceeded` event MUST be emitted.
- **AC-007**: Given an LLM emits `hashtags = ["pdf!", "Foo$"]` and `nl = "extract"`, when the upstream validator runs, then the invalid hashtags MUST be silently dropped and the intent MUST proceed with `hashtags = []` and the `nl` field intact.
- **AC-008**: Given an LLM emits `hashtags = []` and `nl = ""` (or `nl` containing only whitespace), when REQ-INTENT-VAL runs, then the intent MUST be rejected with `ErrEmptyIntent` BEFORE any `ColorIntent` token is produced AND the error MUST be routed to the transition's error place.
- **AC-008a**: Given an LLM emits `hashtags = ["pdf!", "Foo$"]` (all dropped by normalization) AND `nl = "extract text"`, when REQ-INTENT-VAL runs, then a `ColorIntent` token MUST be produced with `hashtags = []` and the trimmed `nl` intact (NOT rejected as empty).
- **AC-009**: Given the firing exceeds 2 seconds, when the deadline fires, then the transition MUST cancel the retriever call AND deposit a `ColorError` token with `Reason = "tool_request: deadline exceeded"`.
- **AC-010**: Given a `tool_request.transition.completed` event fires, when the frontend receives it, then the activity bubble MUST update from `state: "running"` to `state: "done"` with the matched qualified names visible.
- **AC-011**: Given a `tool_request` firing is served from the dedup cache, when the next turn's personality is assembled, then the system prompt MUST include a one-line note referencing the cache hit and its age (per GUD-CACHE-VIS) AND the `tool_request.transition.completed` event MUST carry `cache_hit: true` with non-zero `cache_age_ms`.
- **AC-012**: Given a session is on turn N ≥ 2 with `ToolRequestQuotaRemaining = X`, when the personality assembler builds the system-prompt block, then it MUST inject exactly one line of the form `"Tool-request budget: X of 16 calls remaining"` AND `cpn.Personality.ToolRequestQuotaRemaining` MUST equal `X`.
- **AC-013**: Given a tool-request activity bubble is rendered with `Personality.Language = "en"` (resp. `"es"`) and an `nl` longer than 80 characters, when the bubble is emitted, then the `label` MUST use the `en` (resp. `es`) template, MUST be truncated to 80 characters with a trailing `…`, AND the `tags` array MUST contain bare tokens without leading `#`.

## 6. Test Automation Strategy

- **Test levels**: Unit (handler with mock retriever, dedup cache, schema validator), Integration (full topology with `t-llm → t-tool-request → t-instantiate`), End-to-End (HTTP API → SSE event stream surfaces both events).
- **Frameworks**: Go `testing` stdlib; `FakeLLM` transcript replay; `FakeToolRetriever` returning seeded match sets.
- **Test data**: Fixtures under `cpn/testdata/tool_request/` covering happy path, error path, dedup, quota, timeout, schema rejection.
- **CI/CD integration**: Standard `go test ./...` plus a contract test that asserts the `tool_request` LLM schema matches the Go `Intent` struct (regression gate).
- **Coverage requirements**: ≥ 90 % line coverage on `cpn/fire_tool_request.go`. The space-violation lint check MUST have a dedicated table-driven test.
- **Performance testing**: Benchmark of the dedup-cache hit path; should be < 50 µs per call.
- **Security testing**: Fuzz the schema validator with malformed JSON, oversized arrays, and prompt-injection-shaped strings in `nl`. Assert no panic, correct rejection.

## 7. Rationale & Context

The brittle path is to parse `#hashtags` out of free-form LLM prose. That couples retrieval to tokenisation accidents, gives prompt-injection a free lane, and makes the contract between the LLM and the retriever invisible to tooling. The structured-output path (Option B in the design brief) makes the contract explicit, validates it on both sides, and surfaces it in the event stream so the user actually sees what is happening.

The communication-space discipline is the paper's central guarantee. Surface contains user-visible artefacts; Observation contains derived facts; Computation is where work happens. Retrieval is *derived fact* — it is the system observing its own toolbox in light of an intent. Allowing a retrieval result to be deposited directly into Computation would erase the layering that makes the CPN auditable and makes the formal properties (deadlock-freedom, reachability) tractable. The lint check at REQ-008 makes the violation a build error rather than a runtime surprise.

The dedup cache is small but matters. LLMs are prone to retry-with-tiny-variation loops; a 5-second cache absorbs the common cases without papering over genuine refinements. The 16-per-session quota is the second guardrail: in the worst case where the cache misses every time (because the LLM keeps tweaking the NL phrase), the session still cannot consume an unbounded amount of retrieval work.

The activity bubble is product-critical. Without it, a session that spends 200 ms on `tool_request` followed by 1.5 s on `instantiate` looks indistinguishable to the user from a hung LLM. Surfacing the activity makes the agent's reasoning legible — which is, ultimately, the whole point of the CPN substrate.

## 8. Dependencies & External Integrations

### External systems

- **EXT-001**: LLM provider — required to support structured-output (function-call) requests. Existing OpenRouter integration covers this.

### Third-party services

- **SVC-001**: None new.

### Infrastructure dependencies

- **INF-001**: Existing CPN executor. New node kind plugs into the `switch t.Kind` in `cpn/cpn.go`.
- **INF-002**: Existing event bus and SSE fan-out. Two new event types added; transport unchanged.

### Data dependencies

- **DAT-001**: Tool retriever (`spec-architecture-brae-tool-retriever.md`) injected via `WithToolRetriever`.

### Technology platform dependencies

- **PLT-001**: Go 1.25+.
- **PLT-002**: A2UI v0.8 `activity` component on the frontend.

### Compliance dependencies

- **COM-001**: No persistent storage of intent text beyond the existing audit log; aligns with the existing message-retention policy.

## 9. Examples & Edge Cases

### 9.1 Happy path topology fragment

```yaml
places:
  - id: p-tool-request-intent,  color: Intent,        space: Surface
  - id: p-tool-request-matches, color: ToolMatchSet,  space: Observation
  - id: p-tool-request-error,   color: Error,         space: Observation

transitions:
  - id: t-llm-emit-intent
    kind: llm
    outputs: [{place: p-tool-request-intent}]
    config:
      tools: [tool_request]   # registers the schema with the LLM
  - id: t-tool-request
    kind: tool_request
    inputs:  [{place: p-tool-request-intent}]
    outputs: [{place: p-tool-request-matches}]
    errors:  [{place: p-tool-request-error}]
```

### 9.2 LLM emission

```json
{
  "tool_calls": [
    { "id": "call_1",
      "function": {
        "name": "tool_request",
        "arguments": "{\"hashtags\":[\"tools\",\"pdf\",\"read\"],\"nl\":\"extract text from a scanned PDF\",\"max_tools\":5}"
      }
    }
  ]
}
```

### 9.3 Resulting token after firing

```json
{
  "color": "ToolMatchSet",
  "space": "Observation",
  "payload": {
    "matches": [
      {"qualified_name": "pdf/pdf-to-text@0.1.0", "score": 0.87},
      {"qualified_name": "pdf/pdf-info@0.1.0",    "score": 0.61}
    ],
    "toolboxes": ["pdf"],
    "trace": {"bm25_used": true, "dense_used": true, "rrf_k": 60}
  }
}
```

### 9.4 Edge case: retriever returns empty matches

The token is still produced (`matches: []`). A downstream observer transition surfaces this to the LLM as "no tools matched; consider clarifying your intent or asking the user".

### 9.5 Edge case: space violation in topology

```yaml
- id: p-bad-output, color: ToolMatchSet, space: Computation   # ← lint failure
- id: t-tool-request
  kind: tool_request
  outputs: [{place: p-bad-output}]
```

`Lint()` returns `ErrSpaceViolationToolRequest`; the topology never materialises.

### 9.6 Edge case: LLM emits malformed JSON

The upstream `llm` transition's structured-output validator rejects the call before producing a `ColorIntent` token. The LLM receives the validation error in the next turn's tool-result message and retries.

## 10. Validation Criteria

- All Acceptance Criteria (AC-001 through AC-013, including AC-008a) pass.
- The lint check at REQ-008 fires for at least one fixture topology with a deliberate space violation, and the offending place id appears in the returned error.
- The activity bubble appears and updates within 200 ms of the corresponding events on the SSE stream.
- The dedup cache hit rate exceeds 30 % under a synthetic LLM-loop fixture (regression guard against the cache becoming useless).
- A session-long fuzz of 1 000 random intents never panics and never exceeds the 16-per-session quota beyond the documented error.

## 11. Related Specifications / Further Reading

- `spec/spec-architecture-brae-toolbox-taxonomy.md`
- `spec/spec-architecture-brae-tool-retriever.md`
- `spec/spec-architecture-brae-jit-cpn-builder.md`
- `spec/spec-architecture-brae-tool-compose-policy.md`
- Borghoff, Bottoni, Pareschi (2025), arXiv 2502.14000 — communication-space framework.
- TB-CSPN, MDPI Future Internet, doi:10.3390/fi17080363 — CPN orchestration of LLM agents.

## 12. Search Keywords

NodeKindToolRequest, ColorToolMatchSet, ColorIntent, structured output, LLM tool call, hashtag intent, communication space, observation place, lint, space violation, activity bubble, dedup cache, brae, CPN.
