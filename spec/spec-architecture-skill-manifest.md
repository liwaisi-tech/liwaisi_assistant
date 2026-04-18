---
title: Skill Manifest — Unified Discovery of Flows, Tools, and Host Capabilities
version: 1.0
date_created: 2026-04-17
last_updated: 2026-04-17
owner: liwaisi
tags: [architecture, discovery, manifest, cpn, http, brae, gap-8]
---

# Introduction

`brae` will soon have (a) built-in CPN topologies, (b) agent-authored topologies (GAP-4), (c) built-in tools, (d) agent-authored tools (GAP-3 + GAP-5), and (e) host capabilities (GAP-2). Today nobody can enumerate any of these in one place — there is no "what do I know how to do?" API. The classifier cannot make informed routing decisions, and operators have no one-stop introspection.

The `SkillManifest` is a read-only aggregator surfacing the union of these four sources as a single JSON payload, both to authenticated admin users and to the LLM classifier via system-prompt injection. It is not a new datastore; it stitches existing stores.

## 1. Purpose & Scope

**Purpose.** Expose a single, authoritative view of `brae`'s current abilities — consumed by the classifier (to route intents to the right flow/tool) and by admins (to debug "what can it actually do right now?").

**In scope.**
- A `SkillManifestService` aggregating data from: `FlowRepository`, `ToolRegistry`, `HostCapabilityRepository`, plus the built-in topology factory list.
- Cached build (TTL 60 s, invalidated by `EventToolRegistered`, `EventFlowAuthored`, `EventHostCapabilityUpdated`).
- HTTP `GET /api/skills` returning the manifest JSON; filterable by `?kind=flow|tool|capability`.
- LLM classifier integration: the unified topology's `t-classify` system prompt MUST include a compact summary of the manifest (name + one-line description per skill), with a hard cap on prompt length.
- Admin UI (out of scope UI-side in this spec; frontend task separately).
- Tests: aggregation correctness, cache invalidation on mutation events, size-cap truncation.

**Out of scope.**
- Skill recommendation engine (what to use for a given intent) — the classifier already does that with the manifest as input.
- Cross-host manifest — single-host for v1.
- Write endpoints; this is read-only.

**Audience.** `golang-pro` for the backend aggregator; `vercel-react-best-practices` + `frontend-design` for an admin UI (separate follow-up).

## 2. Definitions

- **Skill**: A unit of capability — either a flow (CPN topology), a tool (executable or in-process), or a host capability (derived from probes).
- **Manifest**: The aggregated JSON returned by `GET /api/skills`.
- **Compact summary**: A single-line description of each skill used to inject the manifest into LLM prompts without blowing the context window.

## 3. Requirements, Constraints & Guidelines

- **REQ-001**: `SkillManifestService` MUST provide: `Build(ctx) (Manifest, error)`, `BuildCompact(ctx, sizeCap int) (string, error)`, `Invalidate(reason string)`.
- **REQ-002**: Aggregation order: built-in topologies first, user flows, agent-authored flows, built-in tools, user tools, agent-authored tools, host capabilities. Stable ordering matters for prompt caching.
- **REQ-003**: The compact form is Markdown of the shape:
  ```
  ## Flows
  - `classifier` — routes user intent to the right handler
  - `tool-forge` — authors and installs new tools

  ## Tools
  - `brae/http-get@1.0.0` — HTTP GET → stdout
  - `system/personality.set_principle` — …

  ## Host capabilities
  - `can-compile-c`: yes (gcc 13.2.0)
  - `can-run-python`: no
  ```
- **REQ-004**: Cache: lazy build on first request; refresh on event; TTL 60 s as a safety fallback.
- **REQ-005**: The HTTP endpoint MUST require admin auth and MUST return `application/json` (not the Markdown compact form; compact is LLM-only).
- **REQ-006**: The compact form MUST be truncatable to an input byte cap with deterministic deterministic last-keep policy: host capabilities dropped first, then agent-authored tools, then user flows, then agent-authored flows, never dropping built-ins.
- **REQ-007**: Each `Skill` entry MUST include: `id, kind, name, description, version, origin, deprecated, provenance_summary`.

### HTTP

- **REQ-010**: `GET /api/skills?kind=flow&origin=agent-authored` — filterable.
- **REQ-011**: `GET /api/skills/:id` — full detail of one skill (topology JSON for flows, schema for tools, full probe row for capabilities).

### Classifier integration

- **REQ-020**: `cmd/server/topologies.go::buildUnifiedTopology` MUST be extended so that `t-classify` pulls the compact manifest via `SkillManifestService.BuildCompact(ctx, 2000)` and prefixes the system prompt.
- **REQ-021**: Prompt-cache friendliness: the manifest compact form MUST have stable ordering so the prompt digest changes only when skills actually change.

### Guidelines

- **GUD-001**: Prefer short descriptions (<80 chars each). LLMs over-index on verbose entries.
- **GUD-002**: Omit deprecated skills from the default compact form; include them only in the full JSON.

## 4. Interfaces & Data Contracts

```go
type SkillKind string
const (
    SkillKindFlow      SkillKind = "flow"
    SkillKindTool      SkillKind = "tool"
    SkillKindHostCap   SkillKind = "host_capability"
    SkillKindBuiltin   SkillKind = "builtin"  // built-in topology
)

type Skill struct {
    ID                 string    `json:"id"`
    Kind               SkillKind `json:"kind"`
    Name               string    `json:"name"`
    Description        string    `json:"description"`
    Version            string    `json:"version,omitempty"`
    Origin             string    `json:"origin"`
    Deprecated         bool      `json:"deprecated"`
    ProvenanceSummary  string    `json:"provenance_summary,omitempty"`
}

type Manifest struct {
    BuiltAt time.Time `json:"built_at"`
    Skills  []Skill   `json:"skills"`
    Counts  map[string]int `json:"counts"`
}
```

HTTP:
```
GET /api/skills                          → Manifest JSON
GET /api/skills?kind=tool&origin=agent-authored   → filtered
GET /api/skills/:id                      → full detail
```

## 5. Acceptance Criteria

- **AC-001**: After registering a new tool via GAP-3, `GET /api/skills` within 1 s reflects it.
- **AC-002**: After deprecating a tool, `GET /api/skills?deprecated=false` omits it; `?deprecated=all` includes it.
- **AC-003**: The compact form MUST fit under a 2 KB cap without dropping any built-in flow or tool.
- **AC-004**: `BuildCompact` output is byte-stable across calls when nothing has changed (prompt-cache friendly).
- **AC-005**: Unauthorised access to `/api/skills` returns 401.
- **AC-006**: The classifier's system prompt, post-integration, includes the compact manifest header; absence breaks a regression test.

## 6. Test Automation Strategy

- Unit: aggregation ordering, size-cap truncation policy, event invalidation.
- Integration: post-register → manifest reflects; post-deprecate → manifest hides.
- Snapshot: compact Markdown output is snapshot-tested.
- Coverage ≥ 85%.

## 7. Rationale & Context

**Why aggregate, not replicate?** The sources (FlowRepository, ToolRegistry, HostCapabilityRepository) are already authoritative. A denormalised third store would drift. A caching aggregator is the minimum viable abstraction.

**Why include in classifier prompt?** Without it, the LLM can't know whether a given capability exists. The classifier should decide "use existing tool X" vs "invoke the forge to build one" based on current reality, not training priors.

**Why not write endpoints?** Mutations already have their own endpoints (tool CRUD, flow reject, capability rediscover). Centralising writes here would blur responsibilities.

## 8. Dependencies & External Integrations

- **EXT-001**: GAP-2, GAP-3, GAP-4 — data sources.
- **INF-001**: In-memory cache (no new infra).

## 9. Examples & Edge Cases

### Example compact manifest (truncated)

```md
## Flows
- `classifier` — route user intent
- `tool-forge` — author and install new tools
- `host-discovery` — enumerate OS facts

## Tools
- `brae/http-get@1.0.0` — HTTP GET → stdout
- `system/personality.set_principle` — set agent principle

## Host capabilities
- can-compile-c: yes (gcc 13.2.0)
- can-run-python: no
```

### Edge — 10k tools registered

Compact form truncates; full JSON is paginated (`?limit=&offset=`).

### Edge — cache miss during event storm

Coalesce: if N invalidations arrive in 50 ms, rebuild once.

## 10. Validation Criteria

- `BuildCompact` output length never exceeds its cap.
- Every invalidation event triggers a rebuild within 200 ms.

## 11. Related Specifications / Further Reading

- [spec-architecture-host-discovery-capability-registry.md](./spec-architecture-host-discovery-capability-registry.md) — GAP-2.
- [spec-architecture-dynamic-tool-registry.md](./spec-architecture-dynamic-tool-registry.md) — GAP-3.
- [spec-architecture-cpn-synthesis-instantiate.md](./spec-architecture-cpn-synthesis-instantiate.md) — GAP-4.
