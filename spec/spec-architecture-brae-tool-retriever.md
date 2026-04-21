---
title: Brae Tool Retriever — Two-Stage Hybrid Retrieval over Toolboxes and Tools
version: 0.2
date_created: 2026-04-20
last_updated: 2026-04-20
owner: liwaisi-tech / brae agent team
tags: [architecture, agent, retrieval, bm25, embeddings, rrf, toolbox, cpn]
changelog:
  - 0.2 (2026-04-20): Panel-review P0 fixes — RRF in stage 1, dotted event names, cold-start fallback, embedding model versioning, latency-budget realism, hashtag-normalization reference, eval-harness governance.
  - 0.1 (2026-04-20): Initial draft.
---

# Introduction

This specification defines the **tool retriever**: the component that turns a hashtag-tagged intent emitted by `brae` into a ranked list of relevant toolboxes and, within them, a ranked list of relevant tools. It is the bridge between the toolbox taxonomy (`spec-architecture-brae-toolbox-taxonomy.md`) and the tool-request transition (`spec-architecture-brae-tool-request-node.md`). It addresses gap **G3** of the JIT CPN Builder design.

## 1. Purpose & Scope

### Purpose

- Provide a deterministic, auditable path from an `Intent` (hashtags + natural-language phrase) to a ranked `ToolMatchSet`.
- Implement two-stage retrieval: a cheap **router** stage that picks toolboxes by fusing two ranked lists — hashtag-Jaccard and BM25 over toolbox summaries — via Reciprocal Rank Fusion, then a **picker** stage that ranks individual tools within the selected toolboxes using hybrid sparse + dense retrieval (BM25 + embeddings, fused via Reciprocal Rank Fusion). Both stages share the same fusion strategy, eliminating any score-scale mismatch.
- Define the embedding adapter port so any backend (OpenRouter, local model, none) can satisfy the contract.
- Establish an evaluation harness so retrieval quality is measurable as the lexicon and tool inventory evolve.

### In scope

- A new package `cpn/retriever/` containing the `ToolRetriever` port, BM25 indexers for both stages, the RRF fusion, and an in-process implementation.
- A new port `cpn.EmbeddingClient` for dense retrieval. The OpenRouter adapter is implemented under `infra/openrouter/embeddings.go`. A no-op adapter ships for environments without a configured embedding model.
- An evaluation harness `cpn/retriever/eval/` that runs labelled query → expected-tool fixtures and reports Recall@K, MRR, and NDCG.
- Wiring in `cmd/server/main.go` that exposes the retriever to `SessionService` via `WithToolRetriever`.

### Out of scope

- The transition that consumes the retriever (see `spec-architecture-brae-tool-request-node.md`).
- The composer that builds CPNs from `ToolMatchSet` (see `spec-architecture-brae-jit-cpn-builder.md`).
- Training or fine-tuning of an embedding model. The retriever consumes whatever the configured embedding provider produces.
- Reranking by an LLM (deferred; the picker stage's RRF output is consumed directly by the composer).

### Intended audience

Backend engineers (retriever implementation), AI/retrieval engineers (eval, tuning), platform owners (embedding provider configuration).

### Assumptions

- The toolbox taxonomy spec is implemented; `Hashtags` and `Toolbox` are populated on every `ToolEntry` consulted by the retriever.
- The lexicon governs which hashtags appear in toolbox manifests.
- For this spec's first ship, embeddings are optional. BM25-only retrieval is the default-on configuration; embeddings are opt-in via `BRAE_EMBEDDING_MODEL`.
- Retrieval latency budget is **≤ 50 ms p95 for stage 1** (router) and **≤ 150 ms p95 for stage 2** (picker, BM25 only). With embeddings enabled, stage 2 is budgeted at **≤ 400 ms p95** for cached or local-embedding deployments and **≤ 600 ms p95** for remote-embedding deployments (network hop to OpenRouter or equivalent).
- Hashtag normalization is governed by `spec/spec-architecture-brae-toolbox-taxonomy.md` (REQ-NORM). This spec consumes the canonical normalized form and does not redefine it.
- Event names follow the `<domain>.<subject>.<verb>` lowercase dot-form convention from `spec/spec-architecture-brae-toolbox-taxonomy.md` (GUD-EVT).

## 2. Definitions

| Term | Definition |
|---|---|
| **Intent** | The structured input to the retriever: a list of hashtags, an optional natural-language phrase, and a `MaxK` cap. |
| **ToolMatch** | One ranked tool in the result set: qualified name + score + per-stage component scores. |
| **ToolMatchSet** | The retriever's output: list of `ToolMatch` plus the selected toolboxes and provenance metadata. |
| **Stage 1 / router** | The toolbox selection stage. Cheap. Hashtag overlap is the dominant signal. |
| **Stage 2 / picker** | The within-toolbox tool ranking stage. Hybrid BM25 + dense, fused via RRF. |
| **BM25** | Okapi BM25 sparse retrieval. Tunable parameters `k1` and `b`. |
| **RRF** | Reciprocal Rank Fusion: `score(d) = Σ_r 1 / (k + rank_r(d))`, with default `k = 60`. |
| **Embedding** | A dense vector representation of a text fragment. Cosine similarity is the comparison metric. |
| **Recall@K** | Fraction of expected tools present in the top K results. |
| **MRR** | Mean Reciprocal Rank: average of `1 / rank_first_relevant` across queries. |
| **NDCG** | Normalised Discounted Cumulative Gain. Standard graded-relevance metric. |

## 3. Requirements, Constraints & Guidelines

### Functional requirements

- **REQ-001**: A new port `cpn.ToolRetriever` MUST be defined with `FindToolboxes(ctx, intent Intent) (ToolboxRanking, error)` and `FindTools(ctx, intent Intent, toolboxes []string) (ToolMatchSet, error)`. A convenience method `Find(ctx, intent Intent) (ToolMatchSet, error)` MUST chain both stages.
- **REQ-002**: Stage 1 MUST rank toolboxes by **Reciprocal Rank Fusion** (default `k = 60`) of two ranked lists: (a) hashtag-Jaccard between `intent.Hashtags` and `toolbox.Hashtags`, and (b) BM25 over `intent.NL` against `toolbox.Title + " " + toolbox.Summary`. The router MUST NOT linearly combine the raw scores (hashtag Jaccard is bounded [0,1] while BM25 is unbounded; the rank-based fusion removes the score-scale mismatch and matches the stage-2 fusion strategy). RRF `k` is configurable via `RouterConfig{RRFK}`.
- **REQ-003**: Stage 1 MUST return the top `intent.MaxToolboxes` toolboxes (default 3, bounded ≤ 6).
- **REQ-004**: Stage 2 MUST score each candidate tool with two retrievers and fuse via RRF: a BM25 retriever over `tool.Hashtags + " " + tool.HelpText + " " + tool.ManPage`, and (when an embedding client is configured) a dense retriever over `embed(tool.Hashtags + " " + tool.HelpText)` compared against `embed(intent.Hashtags + " " + intent.NL)`.
- **REQ-005**: When the embedding client is `nil` or returns an error, the picker MUST fall back to BM25-only and emit a `retriever.embedding.degraded` observer event.
- **REQ-006**: The retriever MUST persist NOTHING beyond the embedding cache (REQ-013). Indexes are built in memory, refreshed when a `ToolRegistryEvent{Type: "tool_registered" | "tool_deprecated"}` is observed.
- **REQ-007**: `ToolMatchSet` MUST include a `Trace` field exposing per-stage scores so downstream consumers (composer, audit logs) can explain why a tool was chosen. The `PickerTrace` MUST include a per-call latency breakdown: `network_ms`, `openrouter_ms`, `batch_assembly_ms`.
- **REQ-008**: Index rebuild MUST be incremental. Adding one tool MUST cost O(L) where L is the average term length of the new tool's text. Full rebuilds are reserved for startup and explicit `Reindex(ctx)` calls.
- **REQ-009**: An `EmbeddingClient` port MUST be defined with `Embed(ctx, texts []string) ([][]float32, error)`. A no-op implementation `NoopEmbeddingClient` ships in `cpn/retriever/`.
- **REQ-010**: The OpenRouter adapter `infra/openrouter/embeddings.go` MUST implement `EmbeddingClient` against a configurable model (env: `BRAE_EMBEDDING_MODEL`, default unset → no-op).
- **REQ-011**: An evaluation harness `cpn/retriever/eval/` MUST run labelled fixtures (JSON: `{intent, expected_qualified_names}`) and report Recall@1, Recall@3, Recall@5, MRR, NDCG@5. The harness MUST enforce the governance criteria in REQ-014.
- **REQ-012 (cold-start fallback)**: When the live registry size is `< 30 tools`, BM25 statistics are unreliable. The retriever MUST disable BM25 weighting in stage 2 and fall back to **uniform sampling from hashtag-matched candidates** (with deprecated tools excluded), emitting a `retriever.coldstart.engaged` event in the trace and recording the registry size that triggered the fallback. The cold-start mode MUST also be reflected in `PickerTrace.ColdStart = true`.
- **REQ-013 (embedding model versioning & re-index)**: The embedding cache key MUST be `(qualified_name, embedding_model_id, sha256(text))`. On startup, the retriever MUST compare `BRAE_EMBEDDING_MODEL` against the persisted index marker; on mismatch it MUST emit `retriever.embedding.model_drift` and trigger a full re-index before serving any stage-2 query. On the first `Embed` call after startup, the retriever MUST validate `EmbeddingClient.Dim()` against the persisted dimension; on mismatch it MUST emit `retriever.embedding.dim_mismatch` and fall back to BM25-only.
- **REQ-014 (eval-harness governance)**: The seed fixture set MUST contain ≥ 24 labelled fixtures, every fixture independently labelled by **two human reviewers** with inter-reviewer agreement Cohen's κ ≥ 0.85, refreshed at least **quarterly**, and each fixture MUST record provenance (`labeller`, `iso8601_timestamp`, `reviewer_2`, `kappa`) under version control.

### Security requirements

- **SEC-001**: Indexed text fields MUST be escape-cleaned before storage in the BM25 index: control characters stripped, sequences resembling prompt-injection markers (`</tool>`, `<system>`, `## ignore previous`) recorded but not stored verbatim. The retriever's index is an attack surface (ToolHijacker, arXiv 2504.19793).
- **SEC-002**: The embedding adapter MUST NOT include the `BinaryPath`, `BinarySHA256`, `Provenance`, or `RegisteredBy` fields in any text it sends to the external embedding provider. Only `Hashtags`, `HelpText`, and `ManPage` are eligible.
- **SEC-003**: When the embedding provider is reached over the network, the request MUST honour the existing OpenRouter timeout, retry, and circuit-breaker configuration in `infra/openrouter/`.
- **SEC-004**: Retriever traces MUST NOT be exposed on any unauthenticated endpoint. They are session-scoped and follow the existing event-stream auth.

### Behaviour & product requirements

- **BEH-001**: When `intent.Hashtags` is empty AND `intent.NL` is empty, the retriever MUST return an empty `ToolMatchSet` and the error `ErrEmptyIntent`. Empty intents never invoke the LLM-side embedding path.
- **BEH-002**: When stage 1 returns zero toolboxes (no overlap, no BM25 hit above the floor), the retriever MUST widen to all toolboxes for stage 2 AND attach `Trace.Widened = true`. Empty results are never silently discarded.
- **BEH-003**: Deprecated tools MUST NEVER appear in `ToolMatchSet`.

### Constraints

- **CON-001**: Stage 1 latency MUST be ≤ 50 ms p95 with up to 64 toolboxes and 1 000 tools.
- **CON-002**: Stage 2 BM25-only latency MUST be ≤ 150 ms p95 with up to 1 000 tools indexed.
- **CON-003**: Stage 2 with embeddings MUST be ≤ 400 ms p95 for cached/local-embedding deployments OR ≤ 600 ms p95 for remote-embedding deployments (network hop), inclusive of the embedding round-trip. A path slower than the applicable budget MUST cancel and fall back to BM25-only.
- **CON-004**: The retriever MUST hold no more than 64 MiB of heap for indexes at any toolbox/tool count below the deployment cap (1 000 tools).
- **CON-005**: The retriever MUST NOT block the registry's write path; index updates happen asynchronously through a buffered channel of capacity 1 024. Overflow MUST log and trigger a deferred full rebuild.

### Guidelines

- **GUD-001**: Default RRF `k = 60` (Cormack et al., 2009 default). Tune only if eval shows ≥ 5 % NDCG lift.
- **GUD-002**: Default BM25 `k1 = 1.2`, `b = 0.75`. Hashtag tokens are short, so a smaller `b` is generally preferable; revisit during eval.
- **GUD-003**: When `intent.NL` is short (< 4 tokens), down-weight the BM25 NL component; hashtags should dominate.
- **GUD-004**: The eval harness SHOULD be run before merging any change to the retriever or the lexicon.

### Patterns

- **PAT-001**: All retriever state mutates through a single goroutine that consumes a channel of `ToolRegistryEvent`. Read paths take a copy of the index pointer; updates swap pointers atomically.
- **PAT-002**: Embedding calls are batched. The picker collects all candidate tools, issues one embedding request for each unique tool text, and one for the intent. Cache the tool-text embeddings keyed by `(qualified_name, sha256(text))`.
- **PAT-003**: Trace fields are always populated, even when the embedding path is skipped, so logs always answer "why this tool".

## 4. Interfaces & Data Contracts

### 4.1 Port `cpn.ToolRetriever` (`cpn/retriever_port.go`)

```go
type ToolRetriever interface {
    FindToolboxes(ctx context.Context, intent Intent) (ToolboxRanking, error)
    FindTools(ctx context.Context, intent Intent, toolboxes []string) (ToolMatchSet, error)
    Find(ctx context.Context, intent Intent) (ToolMatchSet, error)
}

type Intent struct {
    Hashtags      []string
    NL            string
    MaxToolboxes  int     // default 3, capped at 6
    MaxTools      int     // default 5, capped at 12
}

type ToolboxRanking struct {
    Toolboxes []ToolboxScore
    Trace     RouterTrace
}

type ToolboxScore struct {
    Namespace    string
    Score        float64  // RRF fused score
    HashtagRank  int      // rank in hashtag-Jaccard list (1-based, 0 if absent)
    BM25Rank     int      // rank in BM25 list (1-based, 0 if absent)
}

type ToolMatchSet struct {
    Matches    []ToolMatch
    Toolboxes  []string  // namespaces consulted
    Trace      PickerTrace
}

type ToolMatch struct {
    QualifiedName string
    Score         float64
    BM25Rank      int     // 0 if not ranked by BM25
    DenseRank     int     // 0 if not ranked by embeddings
    BM25Score     float64
    DenseScore    float64
}

type RouterTrace struct {
    RRFK       int
    Considered int
}

type PickerTrace struct {
    BM25Used         bool
    DenseUsed        bool
    Widened          bool
    ColdStart        bool
    EmbeddingMS      int
    BM25MS           int
    RRFK             int
    NetworkMS        int  // wall-clock for outbound network I/O
    OpenRouterMS     int  // server-reported processing time
    BatchAssemblyMS  int  // local time spent assembling embedding batch
}
```

### 4.2 Port `cpn.EmbeddingClient` (`cpn/embedding_port.go`)

```go
type EmbeddingClient interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Dim() int
}
```

### 4.3 OpenRouter adapter contract

```yaml
# Configuration via env
BRAE_EMBEDDING_MODEL: "openai/text-embedding-3-small"  # optional
BRAE_EMBEDDING_TIMEOUT_MS: 2000
BRAE_EMBEDDING_BATCH_SIZE: 32
```

### 4.4 Evaluation fixture format (`cpn/retriever/eval/fixtures/*.json`)

```json
{
  "id": "pdf-extract-001",
  "intent": {
    "hashtags": ["tools", "pdf", "read"],
    "nl": "extract text from a scanned PDF",
    "max_toolboxes": 3,
    "max_tools": 5
  },
  "expected_toolboxes": ["pdf"],
  "expected_qualified_names": [
    "pdf/pdf-to-text@0.1.0",
    "pdf/ocr@0.1.0"
  ]
}
```

### 4.5 Evaluation report format

```
brae-retriever-eval (n=24 fixtures)
  Recall@1 = 0.71
  Recall@3 = 0.89
  Recall@5 = 0.96
  MRR      = 0.81
  NDCG@5   = 0.84
  ToolboxRecall@1 = 0.92

Failures:
  pdf-extract-003: expected pdf/pdf-info@0.1.0 in top-5; got rank 7
  ...
```

## 5. Acceptance Criteria

- **AC-001**: Given a registry with 100 tools spread across 5 toolboxes, when `Find(ctx, Intent{Hashtags: ["pdf"]})` is called, then the result `ToolMatchSet.Matches` MUST contain only tools from the `pdf` toolbox AND `Trace.BM25Used = true`.
- **AC-002**: Given an `Intent{Hashtags: ["tools","pdf","read"], NL: "extract text"}` and an embedding client returning fixed vectors, when `Find` is called twice for the same intent, then both calls MUST return identical `Matches` (deterministic).
- **AC-003**: Given the embedding client returns an error, when `Find` is called, then the result MUST be a non-empty `ToolMatchSet` derived from BM25 alone AND a `retriever.embedding.degraded` event MUST be emitted.
- **AC-004**: Given an `Intent` with hashtags that match no toolbox, when `Find` is called, then `ToolMatchSet.Trace.Widened = true` AND the result MUST consider all toolboxes for stage 2.
- **AC-005**: Given a `tool_registered` event for a new tool, when 100 ms elapses, then a subsequent `Find` whose hashtags match that tool MUST return it in the result.
- **AC-006**: Given a `tool_deprecated` event for a tool, when a subsequent `Find` would otherwise return it, then the deprecated tool MUST NOT appear.
- **AC-007**: Given the eval harness runs against the seed fixtures (≥ 24 fixtures, dual-labelled with Cohen's κ ≥ 0.85, provenance recorded in version control, refreshed within the last quarter), when reporting completes, then Recall@5 MUST be ≥ 0.90 and ToolboxRecall@1 MUST be ≥ 0.85 (regression gate). The harness MUST fail if any governance criterion of REQ-014 is unmet.
- **AC-008**: Given an empty `Intent`, when `Find` is called, then the result MUST be `ErrEmptyIntent` AND no embedding call MUST be made.
- **AC-009**: Given the index queue overflows, when the next `Find` runs, then it MUST trigger a full rebuild AND log `index_rebuild_reason=overflow`.
- **AC-010**: Given a benchmark with 1 000 tools, when `Find` is called with embeddings enabled, then p95 latency MUST be ≤ 400 ms for cached/local-embedding deployments and ≤ 600 ms for remote-embedding deployments; when embeddings are disabled, p95 MUST be ≤ 200 ms total. The benchmark MUST report the per-call breakdown (`network_ms`, `openrouter_ms`, `batch_assembly_ms`).
- **AC-011 (stage-1 RRF)**: Given a registry where toolbox A wins on hashtag Jaccard but toolbox B wins on BM25 over title+summary, when `FindToolboxes` is called, then the returned `ToolboxRanking` MUST reflect Reciprocal Rank Fusion (k=60) and MUST NOT reduce to either signal alone. Replacing one toolbox's BM25 score by an arbitrary positive scaling factor MUST NOT change the returned ordering.
- **AC-012 (cold-start fallback)**: Given a registry of 10 tools, when `Find` is called with a hashtag-only intent that matches ≥ 3 tools, then the picker MUST take the cold-start path (uniform sampling from hashtag-matched candidates), `PickerTrace.ColdStart` MUST be `true`, a `retriever.coldstart.engaged` event MUST be emitted, and the eval-harness Recall@3 against the 10-tool fixture set MUST be ≥ 0.60.
- **AC-013 (embedding model versioning)**: Given a persisted index marker recorded under `BRAE_EMBEDDING_MODEL=A` and process startup with `BRAE_EMBEDDING_MODEL=B`, when the retriever initialises, then a `retriever.embedding.model_drift` event MUST be emitted, a full re-index MUST run before any stage-2 query, and cache lookups MUST key on `(qualified_name, embedding_model_id, sha256(text))`.
- **AC-014 (embedding dim mismatch)**: Given the persisted index dimension differs from `EmbeddingClient.Dim()` on first call, when `Find` is invoked, then a `retriever.embedding.dim_mismatch` event MUST be emitted and the call MUST fall back to BM25-only.

## 6. Test Automation Strategy

- **Test levels**: Unit (BM25 indexer, RRF fusion, hashtag Jaccard, intent validation), Integration (retriever + in-memory registry + fake embedding client), End-to-End (retriever wired into `SessionService` and exercised by a synthetic tool-request token).
- **Frameworks**: Go `testing` stdlib + table-driven fixtures. `testing.B` for latency budgets. A `FakeEmbeddingClient` returning seeded deterministic vectors for reproducibility.
- **Test data**: Eval fixtures in `cpn/retriever/eval/fixtures/`. Seed corpus of ≥ 24 labelled queries spanning every shipped toolbox, dual-labelled (Cohen's κ ≥ 0.85), refreshed quarterly, with provenance (`labeller`, `iso8601_timestamp`, `reviewer_2`, `kappa`) committed alongside each fixture (REQ-014). A separate cold-start fixture set against a 10-tool corpus exercises REQ-012 (target Recall@3 ≥ 0.60).
- **CI/CD integration**: Eval harness runs as a separate `go test -tags=eval` invocation; fails the build if Recall@5 drops below 0.90 versus the prior commit.
- **Coverage requirements**: ≥ 88 % line coverage on `cpn/retriever/`. Mutation testing on the BM25 scorer and RRF combiner.
- **Performance testing**: Benchmarks under `cpn/retriever/bench_test.go`. Latency budgets enforced as hard test failures.
- **Security testing**: A fuzz suite injecting prompt-injection-shaped strings into `HelpText` and asserting the retriever neither widens beyond the toolbox scope nor emits the malicious string in traces sent off-process.

## 7. Rationale & Context

The composer cannot build CPNs out of thin air; it needs a small, ranked subset of tools per intent. Naïve options are insufficient: a flat keyword grep does not handle paraphrase; pure embedding similarity loses the exact-match guarantees of hashtag tokens (`#pdfreader` retrieves `#pdfreader`, not "miscellaneous PDF tooling"); a single-stage retriever over thousands of tools is wasteful when the agent's intent localises to one or two domains.

The two-stage pattern is the published consensus: AnyTool (ICLR 2024) and Tool-to-Agent Retrieval (arXiv 2511.01854) both document the recall lift from a hierarchical router. Hybrid BM25 + dense fused via RRF is the well-studied default for sparse + semantic retrieval (Cormack et al., 2009; Lin et al., 2021); it gives reliable improvements without per-domain tuning, and degrades gracefully to BM25-only when the dense path is unavailable.

**Why RRF in stage 1 (v0.2 change).** The v0.1 draft scored toolboxes by a linear combination `0.7 * hashtag_jaccard + 0.3 * BM25(NL, title+summary)`. This is mathematically unjustified: hashtag Jaccard is bounded `[0, 1]` while BM25 is unbounded and corpus-dependent, so the weights are not commensurable and any tuning is a function of corpus size and average document length rather than retrieval quality. v0.2 replaces the linear fusion with Reciprocal Rank Fusion (`k = 60`) of the two ranked lists. RRF operates on ranks alone, eliminating the score-scale mismatch, and aligns stage 1 with the stage-2 fusion strategy so the retriever has a single, consistently auditable combiner.

**Why the cold-start fallback (v0.2 addition).** BM25's IDF term is unstable at small corpus sizes; below ~30 tools, a single high-IDF token can dominate the ranking and starve genuinely relevant tools. Uniform sampling over the hashtag-matched subset is a defensible neutral prior for the bootstrap window and is replaced as soon as the registry crosses the threshold. The `retriever.coldstart.engaged` event makes the regime change auditable.

**Why embedding-model versioning (v0.2 addition).** Embedding spaces are not stable across providers or model versions; mixing vectors from different models in a cosine search returns nonsense. Keying the cache on `(qualified_name, embedding_model_id, sha256(text))` and forcing a re-index on model drift is the only safe path. The `Dim()` check is the cheap last-mile guard against an upstream shipping a same-name, different-shape model.

The fallback-to-BM25 path is a deliberate operability choice. `brae` must boot and serve sessions in environments without a configured embedding model (CI, local dev, restricted networks). Recall is lower in that mode but the agent never freezes waiting on an embedding round-trip that will never come.

The eval harness is mandatory because retrieval quality drifts silently. A new tool with a misleading description, a lexicon entry that overlaps an existing one, a tweaked BM25 weight — any of these can shift recall by 10 % without any test failing. The harness makes the regression visible before merge.

## 8. Dependencies & External Integrations

### External systems

- **EXT-001**: Embedding model provider (default OpenRouter, optional). Required only when `BRAE_EMBEDDING_MODEL` is set.

### Third-party services

- **SVC-001**: OpenRouter (or equivalent) — embedding endpoint with batched POST and per-call latency budget. Existing credential plumbing reused.

### Infrastructure dependencies

- **INF-001**: None new. Retrieval is in-process.

### Data dependencies

- **DAT-001**: Tool entries from `cpn/tools/Registry` after the toolbox-taxonomy spec is implemented (`Hashtags` and `Toolbox` fields populated).
- **DAT-002**: Eval fixtures hand-curated under `cpn/retriever/eval/fixtures/`.

### Technology platform dependencies

- **PLT-001**: Go 1.25+ (slices, generics).
- **PLT-002**: For optional embeddings, network egress to the configured embedding provider.

### Compliance dependencies

- **COM-001**: Outbound text to the embedding provider is restricted to `Hashtags` + `HelpText` + `ManPage`. Operators MAY further restrict via `BRAE_EMBEDDING_MODEL=""` to disable embeddings entirely.

## 9. Examples & Edge Cases

### 9.1 Happy path

```go
intent := cpn.Intent{
    Hashtags: []string{"tools", "pdf", "read"},
    NL:       "I need to extract text from a scanned PDF the user just uploaded",
    MaxTools: 5,
}
matches, err := retriever.Find(ctx, intent)
// matches.Matches[0] = {QualifiedName: "pdf/pdf-to-text@0.1.0", Score: 0.87, BM25Rank: 1, DenseRank: 1}
// matches.Toolboxes  = ["pdf"]
// matches.Trace      = {BM25Used: true, DenseUsed: true, Widened: false, ColdStart: false, RRFK: 60}
```

### 9.2 BM25-only fallback

```go
// embedding client misconfigured
intent := cpn.Intent{Hashtags: []string{"tools","git"}}
matches, _ := retriever.Find(ctx, intent)
// matches.Trace.DenseUsed = false
// observer received {Type: "retriever.embedding.degraded", Reason: "embedding client error: ..."}
```

### 9.3 Edge case: hashtag misses, NL hits

```go
intent := cpn.Intent{NL: "convert a markdown file to HTML"}
// stage 1: no hashtag overlap; widens to all toolboxes
// stage 2: BM25 over NL surfaces "developer/md2html@0.1.0"
```

### 9.4 Edge case: ambiguous intent crosses toolboxes

```go
intent := cpn.Intent{Hashtags: []string{"tools","read","image"}, NL: "OCR a PDF"}
// stage 1 returns ["pdf", "image"] (both score above floor)
// stage 2 picker fuses results across both
```

### 9.5 Edge case: intent for a deprecated tool

```go
// tool "pdf/old-pdfreader@0.0.1" is deprecated.
intent := cpn.Intent{Hashtags: []string{"pdf","read"}}
// "pdf/old-pdfreader@0.0.1" never appears in matches.
```

### 9.6 Trace example for audit logging

```json
{
  "intent": {"hashtags":["tools","pdf","read"],"nl":"extract text"},
  "router": {"rrf_k":60, "considered":5},
  "picker": {
    "bm25_used":true, "dense_used":true, "widened":false, "cold_start":false,
    "embedding_ms":120, "bm25_ms":4, "rrf_k":60,
    "network_ms":95, "openrouter_ms":22, "batch_assembly_ms":3
  },
  "matches": [
    {"qn":"pdf/pdf-to-text@0.1.0","score":0.87,"bm25_rank":1,"dense_rank":1},
    {"qn":"pdf/pdf-info@0.1.0",   "score":0.61,"bm25_rank":2,"dense_rank":3}
  ]
}
```

## 10. Validation Criteria

- All fourteen Acceptance Criteria pass.
- Eval harness reports Recall@5 ≥ 0.90 on the seed fixture set.
- Latency budgets (CON-001, CON-002, CON-003) enforced by benchmarks in CI.
- Heap usage of retriever indexes stays below 64 MiB at the deployment-cap tool count, verified by a synthetic-load test.
- A retriever-trace JSON appears alongside every `tool_request` firing in the existing audit log.

## 11. Related Specifications / Further Reading

- `spec/spec-architecture-brae-toolbox-taxonomy.md` — supplies the indexed substrate.
- `spec/spec-architecture-brae-tool-request-node.md` — invokes the retriever from the CPN.
- `spec/spec-architecture-brae-jit-cpn-builder.md` — consumes the `ToolMatchSet`.
- Borghoff, Bottoni, Pareschi (2025), arXiv 2502.14000.
- TB-CSPN, MDPI Future Internet, doi:10.3390/fi17080363.
- Cormack, Clarke, Büttcher (2009), *Reciprocal Rank Fusion outperforms Condorcet and individual rank learning methods*.
- AnyTool (Du et al., 2024), arXiv 2402.04253.
- Tool-to-Agent Retrieval (Lumer et al., 2025), arXiv 2511.01854.
- ToolHijacker (2025), arXiv 2504.19793 — adversarial threat model for indexed tool descriptions.

## 12. Search Keywords

retriever, BM25, embedding, RRF, reciprocal rank fusion, hybrid retrieval, two-stage retrieval, toolbox routing, tool ranking, recall, MRR, NDCG, eval harness, brae, CPN, hexagonal architecture.
