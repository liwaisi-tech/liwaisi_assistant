---
title: Brae Awakening — Toolbox & Hashtag Extension to Self-Discovery
version: 0.2
date_created: 2026-04-20
last_updated: 2026-04-20
owner: liwaisi-tech / brae agent team
tags: [architecture, agent, awakening, toolbox, hashtag, personality, progressive-disclosure, cpn]
changelog:
  - 0.2 (2026-04-20): P0 panel-review fixes — adopt `<domain>.<subject>.<verb>` event naming; pin lexicon priority formula; define catalogue truncation order; specify `Personality.Digest()` and eventual-consistency contract; add few-shot hashtag examples; cap awakening hashtags at 6; reference taxonomy hashtag normalization; require deterministic lexicon excerpt rendering.
---

# Introduction

This specification extends the existing **brae-awakens** topology defined in `spec/spec-architecture-brae-awakening-self-discovery.md`. It adds toolbox + hashtag tagging to the awakening report, threads the new fields through the tool-forge registration path, and updates the personality-prompt injection to expose a **toolbox catalogue** to `brae` on subsequent turns using a progressive-disclosure (metadata-first, body-lazy) pattern. It addresses gaps **G6** and **G7** of the JIT CPN Builder design.

This spec **does not** redefine the awakening topology, prompt skeleton, A2UI envelope, or fallback behaviour — those remain governed by the original spec. It only **extends** the data contract and prompt assembly.

## 1. Purpose & Scope

### Purpose

- Make every awakening-minted tool carry the same `Toolbox` and `Hashtags` fields as builtin tools, so the JIT CPN Builder can reason about runtime-discovered tools the same way it reasons about shipped ones.
- Update the awakening LLM prompt so the model is instructed to emit `toolbox` and `hashtags` for each `tools_to_register` entry, drawing from the lexicon shipped with the binary.
- Replace the current verbose "## Environment awareness" prompt block with a **toolbox catalogue** that lists toolbox titles, summaries, and hashtag unions — but NOT the per-tool details — so later turns see a small, relevant index instead of an unbounded dump (Anthropic Skills' progressive-disclosure pattern).
- Maintain backwards compatibility: an awakening that produces no `toolbox`/`hashtags` (e.g. fallback path) MUST still produce a usable session.

### In scope

- Additive fields on `AwakeningToolRegister` in `cpn/awakens/report.go`.
- Updates to `cpn/awakens/prompt.go` to embed the lexicon and request the new fields.
- Validation rules in `AwakeningReport.Validate()` for the new fields (lenient: empty arrays accepted; invalid tags dropped, not rejected).
- Updates to `cpn/awakens/toolforge.go::RegisterBatch` so the `Hashtags` and `Toolbox` flow into `cpn.ToolManifest`.
- A new personality block builder `cpn/awakens/personality.go::BuildToolboxCatalogue` that produces the prompt section consumed on turn ≥ 2.
- A length budget for the catalogue block (≤ 800 tokens estimated; exact byte cap enforced).

### Out of scope

- The toolbox taxonomy itself (see `spec-architecture-brae-toolbox-taxonomy.md`).
- Retrieval (see `spec-architecture-brae-tool-retriever.md`).
- The JIT composer (see `spec-architecture-brae-jit-cpn-builder.md`).
- Any change to `host_capability_snapshots` schema or A2UI first-turn envelope. The first-turn card remains exactly as the original spec defines.

### Intended audience

AI behaviour owners (awakening prompt), backend engineers (report validator + toolforge), personality-prompt maintainers.

### Assumptions

- The toolbox-taxonomy spec is implemented first; `cpn.ToolManifest` already carries `Toolbox` and `Hashtags`.
- The original `brae-awakens` topology in `cpn/awakens/topology.go` is unchanged.
- The lexicon (`cpn/tools/lexicon.yaml`) is loadable via the existing `Lexicon` port.

## 2. Definitions

| Term | Definition |
|---|---|
| **Toolbox catalogue** | The compact summary of available toolboxes injected into later-turn system prompts in place of the verbose "Environment awareness" block. |
| **Progressive disclosure** | The pattern (Anthropic Skills, Code Execution with MCP) of preloading only metadata and lazy-loading details on demand. |
| **AwakeningToolRegister (extended)** | The per-tool record in `AwakeningReport.tools_to_register`, gaining `toolbox` and `hashtags` fields. |
| **Catalogue digest** | A short hash of the catalogue block content, used to detect when the catalogue must be re-rendered. |

## 3. Requirements, Constraints & Guidelines

### Functional requirements

- **REQ-001**: `AwakeningToolRegister` (in `cpn/awakens/report.go`) MUST gain two optional JSON fields: `toolbox string` and `hashtags []string`.
- **REQ-002**: The awakening LLM prompt (`cpn/awakens/prompt.go`) MUST embed the lexicon's hashtag list and a one-line description per tag, capped at 32 entries (the most common; full lexicon is too verbose). The prompt MUST instruct the LLM to assign at minimum one **kind tag** and one **domain tag** per registered tool. Hashtag normalization rules are NOT redefined here; see `spec-architecture-brae-toolbox-taxonomy.md` (REQ-NORM-001).
- **REQ-002a**: The 32-entry lexicon excerpt MUST be selected by the following deterministic priority score:

  ```text
  priority = 0.4 * log1p(tool_count_per_tag)
           + 0.3 * coverage_jaccard(tag, registered_tools)
           + 0.2 * recency_score(last_used_days_ago)
           + 0.1 * human_curation_rank
  ```

  Sort entries descending by `priority`; take the top 32; tie-break deterministically by `tag` name ascending. A golden top-32 selection for a known input MUST live at `cpn/tools/testdata/lexicon_priority_test.yaml`.
- **REQ-003**: The awakening prompt MUST instruct the LLM to assign each tool to a `toolbox` from the canonical set: `system | developer | web | image | pdf | data | general`. If the LLM omits `toolbox`, the registrar MUST default to `system` (most awakening-minted tools are shell wrappers).
- **REQ-004**: `AwakeningReport.Validate()` MUST accept reports where `tools_to_register[*].hashtags` is empty or absent. Invalid hashtag tokens MUST be silently dropped; the registration proceeds with the cleaned set.
- **REQ-005**: `cpn/awakens/toolforge.go::RegisterBatch` MUST populate `cpn.ToolManifest.Toolbox` and `cpn.ToolManifest.Hashtags` from the report. On empty `Toolbox`, default to the namespace `awakens` (current behaviour) — a follow-up admin curation step may move tools to other toolboxes.
- **REQ-006**: A new function `cpn/awakens/personality.go::BuildToolboxCatalogue(toolboxes []ToolboxManifest) (string, error)` MUST produce a Markdown block matching the format in §4.3.
- **REQ-007**: The personality assembly path (currently injecting "## Environment awareness") MUST inject the toolbox catalogue ALONGSIDE the existing OS/shell environment summary on turn ≥ 2. Both blocks share the same fixed parent header.
- **REQ-008**: The catalogue block MUST be capped at 4 KiB. If the rendered block exceeds the cap, the renderer MUST sort catalogue entries by `(toolbox != "general") DESC, ToolCount DESC, Namespace ASC` and truncate from the END of that sorted list until the 4 KiB cap is satisfied. Each individual truncation step MUST emit one `personality.catalogue.truncated` event. All event names in this spec follow the canonical `<domain>.<subject>.<verb>` lowercase dot-form defined in `spec-architecture-brae-toolbox-taxonomy.md`.
- **REQ-009**: `Personality.Digest()` MUST be defined as `sha256(toolbox_catalogue_bytes || os_line || shell_line || lexicon_excerpt_bytes)`. The digest is recomputed on every personality assembly (every turn ≥ 2) and exposed for downstream cache-invalidation (e.g. by `jit-cpn-builder` and the retriever cache key).
- **REQ-009a**: Mid-session tool registrations are visible to the retriever within ~100 ms via the async event channel, but appear in the agent's catalogue view ONLY at the next turn's personality assembly. This eventual-consistency contract MUST be honoured by all downstream consumers: do not assume a freshly registered tool is in the agent-visible catalogue until the next turn.
- **REQ-010**: When awakening runs the fallback path, the resulting tools (registered with `Origin = "awakening-fallback"`) MUST receive the default `Toolbox = "system"` and `Hashtags = ["tools","shell"]`. No new prompt is injected on the fallback path; the existing fallback behaviour is preserved.
- **REQ-011**: `AwakeningReport.Validate()` MUST cap hashtags per registered tool at 6. If a tool exceeds the cap, the validator MUST deterministically truncate (sort hashtags ascending, take the first 6) and emit one `personality.tooltags.truncated` event per affected tool. This prevents a compromised personality from over-tagging to widen the retriever candidate set.
- **REQ-012**: The lexicon excerpt rendered into the awakening prompt MUST be deterministic: the same `lexicon.yaml` MUST always produce the same 32-entry list in the same order, so prompt replays are byte-for-byte auditable.

### Security requirements

- **SEC-001**: The lexicon embedded in the awakening prompt MUST NOT include any user-supplied or session-scoped strings. It is derived only from the static lexicon file.
- **SEC-002**: The catalogue block MUST NOT include `BinaryPath`, `BinarySHA256`, or `Provenance` for any tool. Toolbox-level metadata only (title, summary, hashtag union, tool count).
- **SEC-003**: The `personality.catalogue.truncated` event MUST NOT leak the omitted toolbox names if those names are session-scoped (e.g. user-private toolboxes added via admin endpoint). v0.1 ships only public toolboxes; this is a forward-compatibility note.

### Behaviour & product requirements

- **BEH-001**: The first-turn A2UI card remains exactly as defined in the original awakening spec (no toolbox catalogue on the user-visible message). The catalogue is **agent-facing**, not user-facing.
- **BEH-002**: When the LLM emits `tools_to_register` without `hashtags`, the toolforge MUST register the tools with empty hashtags and emit one `lexicon.tag.drifted` event per tool with the reason `awakening: tool registered without hashtags`. This makes "the model forgot to tag" observable without blocking awakening.
- **BEH-003**: Awakening latency MUST stay within the original 15-second normal / 30-second deadline budget despite the prompt growing to include the lexicon.

### Constraints

- **CON-001**: The lexicon excerpt embedded in the awakening prompt MUST NOT exceed 1.5 KiB. The selection MUST be deterministic: the 32 highest-priority entries from the lexicon (priority defined in `lexicon.yaml`).
- **CON-002**: The catalogue block MUST NOT exceed 4 KiB rendered. Truncation rules at REQ-008.
- **CON-003**: The toolbox-catalogue render MUST be pure (no I/O) and complete in ≤ 5 ms even at the deployment-cap toolbox count (≤ 16 toolboxes).
- **CON-004**: This spec MUST NOT alter the `host_capability_snapshots` schema. Toolbox/hashtag information lives on the registry rows, not on the snapshot.

### Guidelines

- **GUD-001**: Encourage the LLM (via prompt) to under-tag rather than over-tag. A tool with 3 well-chosen hashtags ranks better than one with 12 fuzzy ones.
- **GUD-002**: When in doubt about toolbox assignment, the LLM SHOULD pick `general`. Misclassification into `general` is a softer error than misclassification into a specific domain box.
- **GUD-003**: The catalogue block SHOULD lead with the most distinctive toolboxes (those with the rarest hashtag unions). The default render order is by descending `hashtag_count`.

### Patterns

- **PAT-001**: The catalogue block uses the same fixed header (`## Environment awareness`) as the OS/shell summary. Downstream prompt-budget pruning logic locates and prunes the section as a unit.
- **PAT-002**: Toolbox metadata is computed lazily from the registry on each personality assembly. Caching is the personality module's concern, not awakening's.
- **PAT-003**: The fallback path produces a degenerate but valid catalogue (one `system` toolbox, hashtags `[tools, shell]`).

## 4. Interfaces & Data Contracts

### 4.1 Extended `AwakeningToolRegister`

```go
type AwakeningToolRegister struct {
    Name     string   `json:"name"`
    Basis    string   `json:"basis"`
    Toolbox  string   `json:"toolbox,omitempty"`   // new in this spec
    Hashtags []string `json:"hashtags,omitempty"`  // new in this spec
}
```

### 4.2 Extended awakening prompt fragment (lexicon excerpt)

```text
You are also responsible for tagging each tool you register. For every entry of
`tools_to_register`, set:

  - `toolbox`: one of system | developer | web | image | pdf | data | general
  - `hashtags`: 2–5 short tokens chosen from the controlled lexicon below.

Always include AT LEAST one kind tag and one domain tag. Prefer fewer, sharper
tags over many fuzzy ones.

LEXICON (32 most useful entries):
  KIND tags  : tools, read, write, network, compute, transform, query, ...
  DOMAIN tags: shell, fs, proc, git, python, node, html, json, pdf, image, ...

Examples (few-shot anchors):
  { "name": "pdf-to-text", "basis": "pdftotext",
    "toolbox": "pdf",  "hashtags": ["tools","pdf","read","transform"] }
  { "name": "curl-fetch", "basis": "curl",
    "toolbox": "web",  "hashtags": ["tools","network","read","web"] }
  { "name": "sqlite-query", "basis": "sqlite3",
    "toolbox": "data", "hashtags": ["tools","data","read","query","sql"] }
```

(Full text is rendered by the prompt builder; the lexicon list is interpolated from `Lexicon.All()` filtered to priority ≤ 32.)

### 4.3 Toolbox catalogue (Markdown block)

```text
## Environment awareness

OS: Alpine 3.21 (aarch64). Shell: /bin/sh (busybox ash).
Available: sh, awk, sed, grep, find, tar, wget.
NOT available: git, python3, node, go, gcc, make.
If the user asks for a missing tool, state it is absent and propose an alternative.

### Toolboxes

- **system** (4 tools): POSIX shell, filesystem, and process tooling.
  Hashtags: tools, read, write, shell, fs, proc.
- **awakens** (2 tools): Tools minted during this session's awakening.
  Hashtags: tools, shell.
- **general** (1 tool): Long-tail utilities not specific to a domain.
  Hashtags: tools, compute.

To request tools beyond what is listed, emit a `tool_request` call with the
relevant hashtags.
```

### 4.4 Function signatures

```go
// cpn/awakens/personality.go
func BuildToolboxCatalogue(
    osLine string,
    shellLine string,
    presentTools []string,
    absentTools []string,
    toolboxes []cpn.ToolboxManifest,
    cap int,  // bytes
) (block string, digest string, truncated bool, err error)
```

### 4.5 Events

All events follow the canonical `<domain>.<subject>.<verb>` lowercase dot-form (see `spec-architecture-brae-toolbox-taxonomy.md`).

```json
{"event":"lexicon.tag.drifted","source":"awakening","qualified_name":"awakens/foo@0.1.0","reason":"awakening: tool registered without hashtags"}
{"event":"personality.catalogue.truncated","reason":"toolbox_catalogue_over_cap","dropped":["general"]}
{"event":"personality.tooltags.truncated","qualified_name":"awakens/foo@0.1.0","kept":6,"dropped":3}
```

## 5. Acceptance Criteria

- **AC-001**: Given an awakening that registers two tools with `toolbox = "system"` and `hashtags = ["tools","shell"]`, when the registrar runs, then the resulting `ToolEntry` rows MUST have `Toolbox = "system"` and `Hashtags = ["tools","shell"]` (after normalisation).
- **AC-002**: Given an awakening LLM emits a `tools_to_register` entry with no `hashtags`, when the registrar runs, then the tool MUST still be registered AND a `lexicon.tag.drifted` event with reason `awakening: tool registered without hashtags` MUST be emitted.
- **AC-003**: Given the awakening prompt is rendered, when the LLM call begins, then the prompt MUST contain the lexicon excerpt header `LEXICON` and at least one example.
- **AC-004**: Given a session enters turn 2 with three registered toolboxes, when the system prompt is assembled, then the prompt MUST contain `### Toolboxes` AND a bullet for each of the three toolboxes.
- **AC-005**: Given the rendered catalogue exceeds 4 KiB, when the renderer truncates, then for each dropped toolbox a `personality.catalogue.truncated` event MUST be emitted AND the drop order MUST follow the sort `(toolbox != "general") DESC, ToolCount DESC, Namespace ASC` truncated from the end (so `general` is dropped first, then the smallest non-general toolbox, then ties broken by namespace).
- **AC-006**: Given the fallback awakening path runs, when tools are minted, then they MUST receive `Toolbox = "system"` and `Hashtags = ["tools","shell"]`.
- **AC-007**: Given the LLM emits an invalid hashtag (`"#PDF!"`), when the registrar runs, then the bad tag MUST be silently dropped from the registered entry's hashtag list, and the rest of the registration MUST proceed.
- **AC-008**: Given two awakenings on the same host within 24 h, when cache reuse fires, then the catalogue digest MUST be identical AND no fresh prompt assembly MUST run.
- **AC-009**: Given the catalogue is rendered, when measured, then render latency MUST be < 5 ms at 16 toolboxes.
- **AC-010**: Given the new prompt is in place, when awakening completes, then the total awakening latency p95 MUST remain ≤ 15 seconds (CON-001 of the original spec).
- **AC-011**: Given the lexicon priority formula in REQ-002a and the fixture `cpn/tools/testdata/lexicon_priority_test.yaml`, when the prompt builder selects the excerpt, then the resulting top-32 list MUST exactly match the golden ordering in the fixture (deterministic tie-break by tag ascending).
- **AC-012**: Given the same `lexicon.yaml` is rendered twice (REQ-012), when both excerpts are compared byte-for-byte, then they MUST be identical.
- **AC-013**: Given a `Personality` is assembled twice on turn ≥ 2 with no intervening registry changes, when `Personality.Digest()` is computed, then the two digests MUST be identical AND MUST equal `sha256(toolbox_catalogue_bytes || os_line || shell_line || lexicon_excerpt_bytes)`. Given a tool is registered mid-session, the digest at the NEXT personality assembly MUST differ from the previous turn's digest.
- **AC-014**: Given 100 simulated awakenings with diverse host fixtures, when each registered tool's hashtags are inspected, then in ≥ 85 % of cases each tool MUST carry ≥ 2 kind tags AND ≥ 1 domain tag (anchored by the §4.2 few-shot examples).
- **AC-015**: Given the awakening LLM emits a tool with 9 hashtags, when `AwakeningReport.Validate()` runs (REQ-011), then the persisted tool MUST carry exactly 6 hashtags (the first 6 after ascending sort) AND one `personality.tooltags.truncated` event MUST be emitted for that tool.

## 6. Test Automation Strategy

- **Test levels**: Unit (prompt rendering, catalogue builder, validator on extended fields), Integration (full awakening flow with FakeLLM emitting tagged tools, asserting registry rows have toolbox/hashtags).
- **Frameworks**: Go `testing` stdlib; reuse the existing `cpn/awakens/testdata/` golden-file convention; extend goldens with `expected_registry_state.json`.
- **Test data**: New goldens under `cpn/awakens/testdata/with_tags/`. Backwards-compat goldens under `cpn/awakens/testdata/legacy_no_tags/` to ensure unchanged behaviour for missing fields. Lexicon priority golden under `cpn/tools/testdata/lexicon_priority_test.yaml` (REQ-002a, AC-011).
- **CI/CD integration**: Standard `go test ./cpn/awakens/...`. The legacy goldens act as a regression gate against accidentally requiring tags.
- **Coverage requirements**: ≥ 90 % line coverage on the new functions; existing awakening files MUST not regress in coverage.
- **Performance testing**: Benchmark `BuildToolboxCatalogue` with 16 toolboxes; assert < 5 ms.
- **Security testing**: Fuzz the prompt renderer with adversarial toolbox names (zero-width chars, control codes); ensure they are stripped before injection into the prompt.

## 7. Rationale & Context

The original awakening spec gave `brae` self-awareness: it knows what's on the host. This extension gives `brae` *retrieval-aware* self-awareness: it knows how to *ask for* the tools it needs. Without the toolbox catalogue, the JIT CPN Builder is a feature `brae` cannot reach — it has the means to retrieve and compose, but no signal that retrieval is even possible.

A naïve approach would dump every registered tool into the system prompt. That works for a session with 8 tools and breaks immediately at 80. The progressive-disclosure pattern — established by Anthropic Skills (SKILL.md preloaded, body lazy-loaded) and Code Execution with MCP (filesystem listing, content lazy-loaded) — is the only pattern known to scale. The catalogue is the SKILL.md frontmatter for tools; the per-tool details are loaded only when the agent's `tool_request` matches.

Lexicon-aware prompting is what makes the hashtags useful. The agent will only emit `#pdf #read` if it has been told that those are the canonical tokens. Embedding the lexicon excerpt in the prompt is cheap (≤ 1.5 KiB once per session) and pays for itself in retriever recall. The prompt instructs the LLM to under-tag deliberately because over-tagged tools score badly under BM25 IDF.

The catalogue digest matters for cache invalidation: the JIT composer caches built topologies under a key that includes the personality digest, which now reflects catalogue changes. When the catalogue changes — a new tool is registered, a toolbox is added — every cached JIT topology becomes stale and is rebuilt on next demand. This is the right behaviour: the agent's view of its own toolbox has changed.

## 8. Dependencies & External Integrations

### External systems

- **EXT-001**: LLM provider (OpenRouter) — required to support structured-output completions with the extended schema.

### Third-party services

- **SVC-001**: PostgreSQL — for the registry rows (toolbox/hashtags columns from the taxonomy spec).

### Infrastructure dependencies

- **INF-001**: Existing `brae-awakens` topology and `cpn/awakens/*` files. This spec is purely additive against them.

### Data dependencies

- **DAT-001**: `cpn/tools/lexicon.yaml` (priority field used to pick the 32 prompt-embedded entries).
- **DAT-002**: `ToolboxManifest` aggregate computed by the registry (from the taxonomy spec).

### Technology platform dependencies

- **PLT-001**: Go 1.25+.
- **PLT-002**: A2UI v0.8 — unchanged for first-turn rendering.

### Compliance dependencies

- **COM-001**: No change. Toolbox catalogue is operational metadata; user data is not exposed.

## 9. Examples & Edge Cases

### 9.1 Awakening report with extended fields

```json
{
  "tools_to_register": [
    { "name": "shell-exec", "basis": "sh",
      "toolbox": "system", "hashtags": ["tools","shell"] },
    { "name": "text-search", "basis": "grep",
      "toolbox": "system", "hashtags": ["tools","read","fs","text"] },
    { "name": "url-fetch", "basis": "wget",
      "toolbox": "web", "hashtags": ["tools","read","network"] }
  ]
}
```

### 9.2 Backwards-compat: report without new fields

```json
{
  "tools_to_register": [
    { "name": "shell-exec", "basis": "sh" }
  ]
}
```

Result: registers with `Toolbox = "awakens"` (default for unset on awakening path) and `Hashtags = []`. Emits one `lexicon.tag.drifted` event per tool.

### 9.3 Fallback path

Legacy `host-discovery-cpn` runs. Discovered tools persist with `Toolbox = "system"`, `Hashtags = ["tools","shell"]`. No new prompt; original "## Environment awareness" block is rendered without the `### Toolboxes` subsection (no toolbox manifests yet).

### 9.4 Edge case: catalogue overflow

A power user has registered 22 toolboxes (test fixture). Rendering would produce a 5.2 KiB block. The renderer sorts entries by `(toolbox != "general") DESC, ToolCount DESC, Namespace ASC` and drops from the end: `general` first, then the smallest non-general toolbox, etc., until the block fits in 4 KiB. One `personality.catalogue.truncated` event is emitted per dropped toolbox.

### 9.5 Edge case: lexicon missing

If `lexicon.yaml` failed to load, the awakening prompt falls back to a hardcoded minimum lexicon (the same 12 entries shown in §4.5 of the taxonomy spec). A `lexicon.source.fellback` event fires (with payload `{"source":"embedded-min"}`). Awakening still completes successfully.

### 9.6 Catalogue digest changes mid-session

A user-driven action registers a new tool through the admin endpoint (a forward-looking case). The retriever sees the new tool within ~100 ms via the async event channel (REQ-009a), but the agent-visible catalogue does NOT change until the NEXT turn's personality assembly. At that next assembly, `Personality.Digest()` changes; the JIT composer's per-session cache invalidates entries keyed by the old digest.

## 10. Validation Criteria

- All Acceptance Criteria (AC-001 through AC-015) pass.
- Awakening latency p95 stays within the original spec's 15 s budget.
- The catalogue block appears verbatim in turn-2 system prompts during an integration test.
- Backwards-compat goldens (no tags) still pass after this spec is implemented.
- The lexicon excerpt in the prompt deterministically matches the same 32 entries given the same `lexicon.yaml`.

## 11. Related Specifications / Further Reading

- `spec/spec-architecture-brae-awakening-self-discovery.md` — base spec extended here.
- `spec/spec-architecture-brae-toolbox-taxonomy.md` — supplies `Toolbox` and `Hashtags` fields.
- `spec/spec-architecture-brae-tool-retriever.md` — consumes the lexicon-aware tag emissions.
- `spec/spec-architecture-brae-tool-request-node.md` — the call surface the catalogue invites the agent to use.
- Anthropic (2025), *Engineering — Code Execution with MCP* — progressive disclosure precedent.
- Anthropic (2025), *Agent Skills (SKILL.md)* — metadata-first pattern.
- Borghoff, Bottoni, Pareschi (2025), arXiv 2502.14000.
- TB-CSPN, MDPI Future Internet, doi:10.3390/fi17080363.

## 12. Search Keywords

awakening extension, toolbox catalogue, progressive disclosure, lexicon prompt, hashtag tagging, tool-forge, personality injection, environment awareness, backwards compatibility, fallback path, brae.
