---
title: Brae Toolbox Taxonomy — Hashtag-Indexed Tool Organisation
version: 0.2
date_created: 2026-04-20
last_updated: 2026-04-20
owner: liwaisi-tech / brae agent team
tags: [architecture, agent, toolbox, taxonomy, hashtag, registry, cpn]
changelog:
  - 0.2 (2026-04-20): Fold panel P0 fixes — own event-naming convention, canonical hashtag normalization, BRAE_LEXICON_PATH override, lexicon overflow policy, testdata fixtures.
  - 0.1 (2026-04-20): Initial spec.
---

# Introduction

This specification defines the **toolbox taxonomy**: the first-class organisation of `brae`'s tools into named toolboxes (`system`, `developer`, `web`, `image`, `pdf`, `general`, …), the **hashtag vocabulary** that describes what each tool can do, and the **lexicon governance** that keeps that vocabulary stable as agents mint new tools at runtime. It is the foundation on which `spec-architecture-brae-tool-retriever.md` and `spec-architecture-brae-jit-cpn-builder.md` build. It addresses gaps **G1**, **G2**, and **G10** of the JIT CPN Builder design.

## 1. Purpose & Scope

### Purpose

- Make toolboxes a first-class concept in the `brae` runtime, not an implicit side-effect of namespace strings.
- Add a controlled hashtag vocabulary to every tool so the agent can reason about what tools mean, not only what they are called.
- Aggregate per-toolbox metadata (`ToolboxManifest`) so router-stage retrieval can rank toolboxes cheaply before drilling into individual tools.
- Establish a governance loop (curated seed lexicon + drift detection) that prevents the hashtag namespace from collapsing into per-agent slang.

### In scope

- Additive schema changes to `ToolManifest` and `ToolEntry` in `cpn/tool_registry_port.go` and `cpn/tools/registry.go`.
- A new `ToolboxManifest` aggregate computed from the registry contents.
- A YAML lexicon file (`cpn/tools/lexicon.yaml`) shipping with the binary, plus a runtime `LexiconRegistry`.
- A Postgres migration adding `hashtags` and `toolbox` columns to `tool_registry`.
- An observer event (`lexicon.entry.dropped`) emitted whenever an agent registers a tool with a hashtag absent from the lexicon.
- An admin read-only endpoint to inspect the current toolbox catalogue.

### Out of scope

- The retriever that consumes this taxonomy (see `spec-architecture-brae-tool-retriever.md`).
- The intent-emission node that drives retrieval (see `spec-architecture-brae-tool-request-node.md`).
- The JIT composer that builds CPNs from selected tools (see `spec-architecture-brae-jit-cpn-builder.md`).
- Per-tool input/output schema composition (deferred; covered by builder spec §6).
- Frontend toolbox catalogue UI (covered indirectly by `spec-architecture-brae-tool-compose-policy.md`).

### Intended audience

Backend engineers (registry, persistence, lexicon governance), platform owners (admin tooling), AI behaviour owners (lexicon curation).

### Assumptions

- The existing tool registry (`cpn/tools/registry.go`) is the single source of truth for tools; no parallel registry is introduced.
- The Postgres `tool_registry` table from GAP-3 exists and supports additive ALTER TABLE without downtime.
- `cpn/awakens/toolforge.go` is the dominant runtime tool-creation path today; awakening will be updated separately to emit hashtags (see `spec-architecture-brae-awakening-toolbox-extension.md`).
- Hashtag matching is case-insensitive, ASCII-only.

## 2. Definitions

| Term | Definition |
|---|---|
| **Toolbox** | A named domain grouping of tools (e.g. `system`, `developer`, `web`). 1:1 with the `Namespace` field on `ToolEntry`. |
| **ToolboxManifest** | An aggregate descriptor of one toolbox: title, summary, union of hashtags, tool count. Derived state, not persisted. |
| **Hashtag** | A controlled-vocabulary token describing one capability dimension of a tool (e.g. `pdf`, `read`, `network`). Stored without the leading `#`. |
| **Kind tag** | Subset of hashtags describing operational class: `tools`, `read`, `write`, `network`, `compute`, `transform`. |
| **Domain tag** | Subset of hashtags describing subject area: `pdf`, `image`, `git`, `python`, `html`, `json`. |
| **Lexicon** | The curated set of allowed hashtags shipped in `cpn/tools/lexicon.yaml`, plus runtime extensions vetted by drift review. |
| **Lexicon drift** | The condition when an agent registers a tool with a hashtag not present in the lexicon. Always emits a `lexicon.entry.dropped` observer event; never blocks registration. |
| **Anchor** | Existing concept: `namespace/name` pair identifying a tool family across versions. |
| **Qualified name** | Existing concept: `namespace/name@version`. |
| **Origin** | Existing concept: `builtin | user | agent-authored | awakening`. |

## 3. Requirements, Constraints & Guidelines

### Functional requirements

- **REQ-001**: `ToolManifest` (in `cpn/tool_registry_port.go`) and `ToolEntry` (in `cpn/tools/registry.go`) MUST gain a `Hashtags []string` field. The field is optional at the type level; tools registered without hashtags persist with an empty array.
- **REQ-002**: `ToolManifest` and `ToolEntry` MUST gain a `Toolbox string` field. When empty, the value MUST default to the tool's `Namespace` at insertion time so legacy entries remain consistent.
- **REQ-003**: The `Registry` MUST expose `ListByHashtag(ctx, tag string) []*ToolEntry` and `ListByToolbox(ctx, toolbox string) []*ToolEntry`, both returning latest non-deprecated entries only.
- **REQ-004**: The `Registry` MUST expose `Toolboxes(ctx) []ToolboxManifest`, computed on demand from current entries (no caching at the registry layer; consumers cache as needed).
- **REQ-005**: A `ToolboxManifest` MUST contain: `Namespace`, `Title`, `Summary`, `Hashtags []string` (deduped union), `ToolCount int`, `LatestRegisteredAt time.Time`.
- **REQ-006**: A `LexiconRegistry` MUST be constructed at server bootstrap from `cpn/tools/lexicon.yaml` and exposed through a new port `cpn.Lexicon` so consumers can query `IsKnown(tag string) bool`, `Kind(tag string) (kind string, ok bool)`, and `All() []LexiconEntry`.
- **REQ-007**: When `RegisterEntry` (or `RegisterManifest`) inserts a tool whose hashtags include any tag absent from the lexicon, the registry MUST emit a `ToolRegistryEvent{Type: "lexicon.entry.dropped", QualifiedName, Hashtags}` event AND silently drop the offending tokens from the persisted hashtag set. Registration MUST still succeed (drift never blocks registration).
- **REQ-008**: A Postgres migration MUST add columns `hashtags text[] NOT NULL DEFAULT '{}'` and `toolbox text NOT NULL DEFAULT ''` to `tool_registry`, with a partial index `CREATE INDEX tool_registry_hashtags_gin ON tool_registry USING GIN (hashtags)`.
- **REQ-009**: The persistence layer (`cpn/persist/tool_registry.go`) MUST round-trip both new columns through `ToolRegistryEntry`.
- **REQ-010**: A read-only admin endpoint `GET /admin/toolboxes` MUST return a JSON array of `ToolboxManifest` objects, gated by the existing admin auth middleware.
- **REQ-011**: Hashtag tokens MUST be normalised on insertion: lower-cased, leading `#` stripped, validated against the regex `^[a-z][a-z0-9-]{0,31}$`. Invalid tokens reject the registration with `ErrInvalidHashtag`. (See REQ-NORM-001 for the canonical ordered procedure.)
- **REQ-NORM-001** (canonical hashtag normalization — single source of truth): Producers MUST normalize hashtag tokens before depositing them into any registry, retriever, intent, or compose-policy structure using the following EXACT ordered procedure: (1) strip a single leading `#` if present; (2) trim leading and trailing whitespace; (3) lowercase using ASCII case-folding; (4) validate against regex `^[a-z][a-z0-9-]{0,31}$` — tokens that fail validation are dropped from the set (no error raised at this step beyond REQ-011's reject-on-explicit-registration semantics); (5) deduplicate, preserving first-seen order. Consumers MAY assume input is already normalized and skip re-normalization. Sibling specs (retriever, tool-request-node, jit-cpn-builder, awakening-toolbox-extension, tool-compose-policy) MUST reference this requirement and MUST NOT redefine the procedure.
- **REQ-LEX-001** (operator lexicon override): The lexicon loader MUST accept an override path via the environment variable `BRAE_LEXICON_PATH`. When unset, the embedded `cpn/tools/lexicon.yaml` is used. On successful load the loader MUST emit a structured log line `lexicon.source=embedded` or `lexicon.source=file path=<resolved-path>`. If `BRAE_LEXICON_PATH` is set but the path is unreadable, malformed, or violates SEC-004 caps, server startup MUST fail fast with a clear error message naming the env var and the underlying cause.
- **REQ-LEX-002** (lexicon overflow reconciliation): The shipped lexicon is capped at 256 entries (CON-003). When an unknown hashtag arrives at registration, the registry MUST emit `lexicon.entry.dropped` (REQ-007) AND drop the unknown token silently from the persisted hashtag set; registration of the tool itself MUST never be blocked by drift. A `lexicon.coverage.gauge` metric (percentage of registered, non-deprecated tools whose hashtag set is fully covered by the lexicon) MUST be exported. The v1 acceptance gate is `lexicon.coverage.gauge ≥ 95%`.

### Security requirements

- **SEC-001**: Hashtag values MUST NOT be interpreted as code, glob patterns, or regular expressions anywhere in the runtime. They are opaque identifiers.
- **SEC-002**: The admin endpoint `GET /admin/toolboxes` MUST require the same authentication as other `/admin/*` routes; it MUST NOT be reachable from anonymous clients.
- **SEC-003**: Lexicon drift events MUST NOT include user-supplied free text in the event payload beyond the registered tool's qualified name and the offending hashtags. They MUST NOT include the tool's `HelpText` or `Provenance` fields.
- **SEC-004**: The lexicon YAML loader MUST reject files exceeding 64 KiB and entries exceeding 256 entries; this caps memory and prevents resource exhaustion via mounted overrides.

### Behaviour & product requirements

- **BEH-001**: Existing tools registered before this spec ships MUST remain resolvable by qualified name, anchor, and bare-name lookup. Their `Hashtags` MUST be the empty array and their `Toolbox` MUST equal their `Namespace`.
- **BEH-002**: When two tools share an anchor but have different `Toolbox` values, the registry MUST emit a `ToolRegistryEvent{Type: "toolbox.conflict.detected"}` and accept the latest write; toolbox is per-version, not per-anchor.

### Constraints

- **CON-001**: The change MUST be additive at the type level. No existing field of `ToolManifest` or `ToolEntry` may be renamed or have its semantics altered.
- **CON-002**: The Postgres migration MUST be runnable on a live cluster (no table rewrite, no exclusive lock beyond a brief `ALTER TABLE`). Defaults are NULL-safe so deploys roll forward without backfill.
- **CON-003**: Lexicon size MUST stay ≤ 256 entries in the shipped YAML. Larger vocabularies indicate the system needs a structural fix, not more tags.
- **CON-004**: A single tool MUST NOT carry more than 12 hashtags. Beyond this, retrieval signal degrades to noise.
- **CON-005**: `cpn/` MUST NOT import `cpn/tools/` or `cpn/persist/` to read lexicon state. The lexicon port lives in `cpn/` (interface only); the implementation lives in `cpn/tools/` and is injected via the existing dependency-injection seam in `cmd/server/main.go`.

### Guidelines

- **GUD-001**: Prefer composing multiple atomic hashtags over inventing a compound one. Use `pdf` + `read` over a new `pdf-read`.
- **GUD-002**: Keep toolbox names singular and lower-case (`developer`, not `developers`; `image`, not `images`).
- **GUD-003**: The lexicon SHOULD include a one-line description per hashtag; the agent uses these descriptions when ranking ambiguous matches.
- **GUD-004**: When awakening mints a new tool, it SHOULD assign at least one kind tag and one domain tag. The lexicon entry for `tools` is reserved as a universal kind tag.
- **GUD-EVT-001** (event-naming convention — panel-wide, OWNED by this spec): Every event emitted by any spec in this taxonomy/retriever/tool-request/jit-builder/awakening-toolbox/tool-compose family MUST follow the pattern `<domain>.<subject>.<verb>` rendered in lowercase dot-form. Examples: `lexicon.entry.dropped`, `tool_request.quota.exceeded`, `jit.subnet.timeout`, `manifest.verification.failed`, `tool_compose.evaluated`, `toolbox.conflict.detected`. Verbs SHOULD be past-tense for completed facts and present-tense (e.g. `.requested`) for in-flight intents. All sibling specs (retriever, tool-request-node, jit-cpn-builder, awakening-toolbox-extension, tool-compose-policy) MUST conform to this convention; legacy snake_case event types (e.g. `lexicon_drift`, `toolbox_conflict`) are renamed to `lexicon.entry.dropped` and `toolbox.conflict.detected` respectively in this spec.

### Patterns

- **PAT-001**: All registry mutations remain single-writer through the existing `r.mu` mutex. Hashtag indexing is a derived map maintained inside the same critical section as `byAnchor` / `byQualified`.
- **PAT-002**: Drift detection emits an event but never blocks. Governance is asynchronous: a human reviews drift events and either promotes the hashtag to the lexicon or deprecates the tool.
- **PAT-003**: The lexicon file is a build-time artefact embedded via `//go:embed`. Operators may override via the `BRAE_LEXICON_PATH` environment variable (see REQ-LEX-001); the loader logs which source it loaded (`lexicon.source=embedded` or `lexicon.source=file path=...`).

## 4. Interfaces & Data Contracts

### 4.1 Extended `ToolManifest` (port type, `cpn/tool_registry_port.go`)

```go
type ToolManifest struct {
    Namespace    string
    Name         string
    Version      string
    Schema       json.RawMessage
    HelpText     string
    ManPage      string
    BinaryPath   string
    BinarySHA256 string
    Origin       string
    Provenance   ProvenanceSnapshot
    RegisteredBy string

    // ── new in this spec ──
    Toolbox  string   `json:"toolbox,omitempty"`  // defaults to Namespace if empty
    Hashtags []string `json:"hashtags,omitempty"` // normalised, deduped
}
```

### 4.2 Extended `ToolEntry` (registry type, `cpn/tools/registry.go`)

```go
type ToolEntry struct {
    // ... all existing fields unchanged ...

    Toolbox  string
    Hashtags []string
}
```

### 4.3 `ToolboxManifest` (new aggregate, `cpn/tools/toolbox.go`)

```go
type ToolboxManifest struct {
    Namespace          string    `json:"namespace"`
    Title              string    `json:"title"`
    Summary            string    `json:"summary"`
    Hashtags           []string  `json:"hashtags"`
    ToolCount          int       `json:"tool_count"`
    LatestRegisteredAt time.Time `json:"latest_registered_at"`
}
```

`Title` and `Summary` come from `cpn/tools/toolboxes.yaml` (a small fixed file, distinct from the hashtag lexicon). When a toolbox is observed in the registry but absent from `toolboxes.yaml`, the manifest falls back to `Title = strings.Title(Namespace)`, `Summary = ""`.

### 4.4 Lexicon port (`cpn/lexicon_port.go`)

```go
type Lexicon interface {
    IsKnown(tag string) bool
    Kind(tag string) (kind string, ok bool)   // "kind" | "domain"
    Describe(tag string) (desc string, ok bool)
    All() []LexiconEntry
}

type LexiconEntry struct {
    Tag         string
    Kind        string  // "kind" | "domain"
    Description string
}
```

### 4.5 Lexicon YAML schema (`cpn/tools/lexicon.yaml`)

```yaml
version: 1
entries:
  - tag: tools
    kind: kind
    description: Generic indicator that the agent is requesting tooling.
  - tag: read
    kind: kind
    description: Reads or fetches data without side effects.
  - tag: write
    kind: kind
    description: Mutates state on disk, network, or external systems.
  - tag: network
    kind: kind
    description: Performs network I/O.
  - tag: compute
    kind: kind
    description: Performs computation without I/O.
  - tag: transform
    kind: kind
    description: Converts data from one shape to another.
  - tag: pdf
    kind: domain
    description: Operates on PDF documents.
  - tag: image
    kind: domain
    description: Operates on raster or vector images.
  - tag: html
    kind: domain
    description: Operates on HTML markup.
  - tag: json
    kind: domain
    description: Operates on JSON values.
  - tag: git
    kind: domain
    description: Interacts with git repositories.
  - tag: shell
    kind: domain
    description: Executes shell commands.
  # ... up to 256 entries
```

### 4.6 Postgres migration

```sql
ALTER TABLE tool_registry
    ADD COLUMN IF NOT EXISTS hashtags text[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS toolbox  text   NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS tool_registry_hashtags_gin
    ON tool_registry USING GIN (hashtags);

CREATE INDEX IF NOT EXISTS tool_registry_toolbox_idx
    ON tool_registry (toolbox);
```

### 4.7 Admin endpoint

```
GET /admin/toolboxes

200 OK
[
  {
    "namespace": "system",
    "title": "System",
    "summary": "POSIX shell, filesystem, and process tooling.",
    "hashtags": ["tools","read","write","shell","fs","proc"],
    "tool_count": 7,
    "latest_registered_at": "2026-04-20T18:31:02Z"
  },
  ...
]
```

### 4.8 New observer events

```go
ToolRegistryEvent{
    Type:          "lexicon.entry.dropped",
    QualifiedName: "developer/git-blame@0.1.0",
    Reason:        "unknown hashtags: blame, archeology",
    Timestamp:     time.Now().UTC(),
}

ToolRegistryEvent{
    Type:          "toolbox.conflict.detected",
    QualifiedName: "developer/lint@0.2.0",
    Reason:        "previous version assigned to toolbox=general; latest assigned to toolbox=developer",
}
```

## 5. Acceptance Criteria

- **AC-001**: Given the migration is applied, when the server boots, then `SELECT hashtags, toolbox FROM tool_registry LIMIT 1` MUST succeed and existing rows MUST return `{}` and `''` respectively.
- **AC-002**: Given a builtin tool is registered with `Hashtags = ["tools","read","pdf"]`, when `Registry.ListByHashtag(ctx, "pdf")` is called, then the result MUST include that tool's latest non-deprecated entry.
- **AC-003**: Given two tools with different toolboxes are registered, when `Registry.Toolboxes(ctx)` is called, then the result MUST contain exactly two `ToolboxManifest` entries with correct `ToolCount`.
- **AC-004**: Given a tool is registered with a known hashtag `tools` and an unknown hashtag `frobnicate`, when registration completes, then a `ToolRegistryEvent{Type: "lexicon.entry.dropped"}` MUST be emitted naming `frobnicate`, the unknown token MUST be silently dropped from the persisted hashtag set, AND registration MUST succeed (the tool remains queryable by `ListByHashtag(ctx, "tools")`).
- **AC-005**: Given a tool is registered with hashtag `Foo!`, when `RegisterEntry` is called, then the call MUST return `ErrInvalidHashtag` AND no row MUST be inserted in `tool_registry`.
- **AC-006**: Given a tool has 13 hashtags, when `RegisterEntry` is called, then the call MUST return `ErrTooManyHashtags`.
- **AC-007**: Given the lexicon YAML is missing from disk, when the server starts, then the embedded default lexicon MUST be loaded and a structured log entry `lexicon.source=embedded` MUST be emitted.
- **AC-008**: Given the lexicon YAML exceeds 64 KiB, when the loader runs, then startup MUST fail fast with `ErrLexiconTooLarge`.
- **AC-009**: Given a non-admin client requests `GET /admin/toolboxes`, when the request is processed, then the response MUST be `401 Unauthorized` and no toolbox data MUST be returned.
- **AC-010**: Given a tool is registered with no `Toolbox` and `Namespace = "developer"`, when read back through `Registry.Get`, then `Toolbox` MUST equal `"developer"`.
- **AC-011** (REQ-NORM-001 ordering): Given the input set `["#PDF", "pdf", " PDF "]`, when the canonical normalization procedure runs, then the output MUST be exactly `["pdf"]` (single token, lower-cased, deduplicated, first-seen order preserved).
- **AC-012** (REQ-LEX-001 override happy-path): Given `BRAE_LEXICON_PATH=/etc/brae/lexicon.yaml` is set and the file is readable and conformant, when the server boots, then the loader MUST load that file and emit a structured log line `lexicon.source=file path=/etc/brae/lexicon.yaml`.
- **AC-013** (REQ-LEX-001 override failure): Given `BRAE_LEXICON_PATH=/missing/lexicon.yaml` is set and the file does not exist, when the server boots, then startup MUST fail fast with an error message naming `BRAE_LEXICON_PATH` and the underlying I/O cause; the embedded fallback MUST NOT be silently substituted.
- **AC-014** (REQ-LEX-002 overflow does not block): Given a tool is registered carrying ten unknown hashtags and one known hashtag, when registration completes, then registration MUST succeed, all ten unknown tokens MUST be dropped from the persisted set, exactly one `lexicon.entry.dropped` event MUST be emitted enumerating the dropped tokens, and `lexicon.coverage.gauge` MUST reflect the new tool as covered (since its persisted set is fully covered post-drop).

## 6. Test Automation Strategy

- **Test levels**: Unit (hashtag normaliser, lexicon loader, ToolboxManifest aggregation), Integration (registry + Postgres migration round-trip), End-to-End (admin endpoint with auth).
- **Frameworks**: Go `testing` stdlib + table-driven fixtures. `dockertest` or the existing Postgres test scaffold for migration tests.
- **Test data**: Lexicon fixture under `cpn/tools/testdata/lexicon_minimal.yaml`; tool-entry fixtures under `cpn/tools/testdata/entries/`. Hashtag normalization fixtures live in `cpn/tools/testdata/normalize_fixtures.json` and MUST contain ≥ 20 edge cases covering at minimum: leading `#` (`"#pdf"` → `"pdf"`), mixed case (`"PDF"` → `"pdf"`), trailing/leading whitespace (`"  read  "` → `"read"`), underscores (`"web_scrape"` → dropped), unicode (`"pdfé"` → dropped), too-long (>32 chars → dropped), empty string (dropped), and dedup-with-different-casing (`["pdf", "PDF", "Pdf"]` → `["pdf"]`).
- **CI/CD integration**: Migration runs as part of the existing migrations gate; new tests in the same package and run by `go test ./...`.
- **Coverage requirements**: ≥ 90 % line coverage on new files (`cpn/tools/toolbox.go`, `cpn/tools/lexicon.go`, the migration round-trip path).
- **Performance testing**: Microbenchmark `ListByHashtag` and `Toolboxes` with 10 000 registered entries; fail if either exceeds 1 ms p95.
- **Security testing**: Fuzz the hashtag normaliser with arbitrary unicode and shell metacharacters; ensure all are rejected or transformed safely.

## 7. Rationale & Context

The current registry classifies tools by `Origin` (builtin / user / agent-authored) and addresses them by `namespace/name@version`. That is enough to *find a known tool* but nothing else. It cannot answer "what tools can read PDFs?" or "which toolboxes do we have today?" — both of which the JIT CPN Builder needs to answer dozens of times per session.

The literature converges on a two-stage retrieval pattern: a cheap router stage selects relevant *toolboxes*, then a more expensive picker stage selects individual *tools* (AnyTool, ICLR 2024; Tool-to-Agent Retrieval, arXiv 2511.01854). Anthropic's Skills (SKILL.md) and the MCP code-execution pattern (anthropic.com/engineering/code-execution-with-mcp, 2025) both demonstrate that *metadata-first, body-lazy* loading is the only way to scale agent tool inventories beyond a handful of items without exploding context windows. This spec lays the metadata layer.

The hashtag vocabulary is the concession to interpretability. Pure embedding-based retrieval is opaque; the agent cannot explain why it picked a tool. With hashtags, the retrieval trace is auditable: the LLM emits `#tools #pdf #read`, the router matches by exact-string overlap on the lexicon, the user can read why a toolbox was picked. Hybrid retrieval (BM25 + dense) is layered on top in the next spec, but the symbolic substrate must come first.

A controlled lexicon prevents the namespace from collapsing. AnyTool's published failure mode is that LLM-generated category labels diverge over time and the router degrades. The lexicon plus drift events keep this honest: every new tag is visible in observability before it influences retrieval at scale.

## 8. Dependencies & External Integrations

### External systems

- **EXT-001**: None new. Existing dependency on the LLM provider is unaffected.

### Third-party services

- **SVC-001**: PostgreSQL — required for the additive migration and the GIN index on `hashtags`. Existing dependency.

### Infrastructure dependencies

- **INF-001**: The existing `golang-migrate` migration runner. New migration file `NNNN_tool_registry_taxonomy.up.sql` slotted into the existing sequence.

### Data dependencies

- **DAT-001**: `tool_registry` table — extended by additive ALTER TABLE. No data migration of existing rows is required.

### Technology platform dependencies

- **PLT-001**: Go 1.25+ for `slices` and `//go:embed`.
- **PLT-002**: PostgreSQL 12+ for `text[]` columns and GIN indexes (already required).

### Compliance dependencies

- **COM-001**: No new compliance impact. Hashtag values are operational metadata, not user data.

## 9. Examples & Edge Cases

### 9.1 Registering a tool with hashtags

```go
manifest := cpn.ToolManifest{
    Namespace: "pdf",
    Name:      "pdf-to-text",
    Version:   "0.1.0",
    HelpText:  "Extract plain text from a PDF document.",
    Origin:    "builtin",
    Toolbox:   "pdf",
    Hashtags:  []string{"tools", "read", "pdf", "transform"},
}
result, err := registry.RegisterManifest(ctx, manifest)
```

### 9.2 Querying by hashtag

```go
entries := registry.ListByHashtag(ctx, "pdf")
// → [pdf/pdf-to-text@0.1.0, pdf/pdf-info@0.1.0]
```

### 9.3 Lexicon drift event

A user-defined tool registers with `Hashtags = ["tools", "blame", "archeology"]`. The lexicon contains `tools` but not `blame` or `archeology`. Registration succeeds; the unknown tokens are dropped from the persisted set (final stored hashtags: `["tools"]`). The observer receives:

```json
{
  "type": "lexicon.entry.dropped",
  "qualified_name": "developer/git-blame@0.1.0",
  "reason": "unknown hashtags: blame, archeology",
  "timestamp": "2026-04-20T18:31:02Z"
}
```

### 9.4 Edge case: hashtag normalisation

| Input | Stored | Notes |
|---|---|---|
| `"#PDF"` | `"pdf"` | Leading hash stripped, lower-cased. |
| `"  read  "` | `"read"` | Trimmed. |
| `"web-scrape"` | `"web-scrape"` | Hyphens allowed. |
| `"web_scrape"` | rejected (`ErrInvalidHashtag`) | Underscores forbidden. |
| `"pdf!"` | rejected | Non-alphanumeric/non-hyphen forbidden. |
| `"1pdf"` | rejected | Must start with a letter. |
| `"a"` | `"a"` | Single-letter tags allowed but discouraged (GUD). |
| `(33 chars)` | rejected | Max 32 chars. |

### 9.5 Edge case: duplicate hashtags

```go
Hashtags: []string{"tools", "Tools", "TOOLS", "read"}
// → stored as ["tools", "read"] (dedup after normalisation)
```

### 9.6 Edge case: toolbox conflict across versions

`developer/lint@0.1.0` was registered with `Toolbox = "general"`. A later `developer/lint@0.2.0` is registered with `Toolbox = "developer"`. Both rows persist; `Registry.Latest("developer", "lint")` returns the 0.2.0 entry; a `toolbox_conflict` event is emitted to observability.

## 10. Validation Criteria

- All Acceptance Criteria (AC-001 through AC-014) pass under `go test ./cpn/tools/...`.
- The migration applies cleanly against a database with prior `tool_registry` rows; round-trip preserves all existing fields.
- The lexicon YAML loads at server boot in under 50 ms and consumes less than 1 MiB of heap.
- An observability sink (logs or events table) shows at least one `lexicon.entry.dropped` entry when intentionally registering a tool with a foreign hashtag in a smoke test.
- The admin endpoint returns a non-empty `ToolboxManifest` array on a freshly-bootstrapped server with builtin tools registered.

## 11. Related Specifications / Further Reading

- `spec/spec-architecture-brae-awakening-self-discovery.md` — establishes the tool-forge pattern this spec extends.
- `spec/spec-architecture-brae-tool-retriever.md` — consumer of the taxonomy added here.
- `spec/spec-architecture-brae-jit-cpn-builder.md` — downstream consumer that composes CPNs from retrieved tools.
- `spec/spec-architecture-brae-awakening-toolbox-extension.md` — applies this taxonomy to the awakening flow.
- Borghoff, Bottoni, Pareschi (2025), *Human-Artificial Interaction in the Age of Agentic AI: A System-Theoretical Approach*, arXiv 2502.14000.
- Pareschi et al. (2025), *Topic-Based Communication Space Petri Net (TB-CSPN)*, MDPI Future Internet, doi:10.3390/fi17080363.
- AnyTool (Du et al., 2024), arXiv 2402.04253 — hierarchical category retrieval.
- Anthropic Engineering (2025), *Code Execution with MCP* — metadata-first, body-lazy loading.

## 12. Search Keywords

toolbox, hashtag, lexicon, tool registry, taxonomy, namespace, GIN index, ToolboxManifest, lexicon drift, controlled vocabulary, tool retrieval substrate, brae, CPN, hexagonal architecture, additive migration.
