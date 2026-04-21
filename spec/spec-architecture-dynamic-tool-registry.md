---
title: Dynamic ToolRegistry + NodeKindRegisterTool — Runtime Authoring of Agent Tools
version: 1.0
date_created: 2026-04-17
last_updated: 2026-04-17
owner: liwaisi
tags: [architecture, tools, registry, cpn, persistence, brae, gap-3]
---

# Introduction

Today `back/go-assistant/cpn/tools/registry.go` is a thread-safe map populated exclusively at server boot by Go code. The existing `ToolEntry` stores a JSON schema and an executor closure. This makes it impossible for a CPN to *author* a tool at runtime: there is no mutation surface, no persistent metadata beyond boot-time code, no provenance, no help text, no versioning, and no way for the tool-forge (GAP-5) to register what it just compiled.

This specification extends `ToolEntry` to carry full provenance (who authored it, from which flow, against which binary hash, with what help/man content), adds a new `NodeKindRegisterTool` transition kind whose effect is mutating the registry, persists the enriched entries to Postgres so they survive restarts, and exposes an admin HTTP surface for introspection and moderation.

## 1. Purpose & Scope

**Purpose.** Allow `brae` to publish new tools at runtime, with full audit trail and durable state, without refactoring the existing personality-tool registrations.

**In scope.**
- Enriched `ToolEntry` struct (add `Version`, `HelpText`, `ManPage`, `BinaryPath`, `BinarySHA256`, `Provenance`, `RegisteredAt`, `RegisteredBy`, `Deprecated`).
- New `NodeKindRegisterTool` transition variant; config carries the manifest.
- Thread-safe mutation API: `Register`, `Deprecate`, `Unregister`, `List`, `Get`, `Versions`.
- Postgres persistence layer `store/postgres/tool_registry_store.go` + migration `0017_tool_registry.sql`.
- Port `ToolRegistryRepository` in `cpn/persist/tool_registry.go`.
- Boot-time reconciliation: on startup, the in-memory registry is seeded from Postgres; pre-existing code-level registrations (like `personality.*`) are still authoritative but flagged `origin=builtin`.
- Version semantics: name is `namespace/name@version`; `namespace/name` resolves to the latest non-deprecated version unless pinned.
- HTTP endpoints for listing + moderating tools.
- HITL integration: registering a tool with `origin=agent-authored` AND `binary_sha256` MUST go through GAP-6 first-run-ledger approval before it is available for invocation.
- Tests: register, deprecate, re-register same name with higher version, moderation, boot reconciliation, race-condition stress test.

**Out of scope.**
- Tool execution sandboxing (covered by GAP-6).
- Tool authoring logic (that's the tool-forge, GAP-5).
- Frontend tool-browser UI (separate ticket; this spec exposes the REST surface only).
- Schema evolution of the `JSONSchema` field.

**Audience.** `golang-pro`, the admin REST reviewer.

## 2. Definitions

- **Qualified name**: `namespace/name@version`, e.g. `brae/http-get@1.0.0`. The anchor `namespace/name` resolves to latest-non-deprecated.
- **Origin**: `builtin | user | agent-authored`. Builtins are code-registered at boot; user tools are registered via admin UI; agent-authored tools come from the forge.
- **Provenance**: Structured record of how a tool came to exist (authoring CPN ID, flow hash, forge run ID, prompt digest).
- **Deprecation**: Soft-remove; the tool can no longer be *newly invoked* by a fresh topology, but in-flight CPNs that already hold a reference continue.

## 3. Requirements, Constraints & Guidelines

### Data model

- **REQ-001**: `ToolEntry` MUST be extended with these fields (all persisted):
  - `ID string` (UUID)
  - `Namespace string`
  - `Name string`
  - `Version string` (semver)
  - `Schema json.RawMessage`
  - `HelpText string` (short, <500 chars)
  - `ManPage string` (long-form, Markdown)
  - `BinaryPath string` (absolute, under `$HOME/.local/brae/bin/` or system path)
  - `BinarySHA256 string` (empty for pure-Go tools)
  - `Origin string` (`builtin`, `user`, `agent-authored`)
  - `Provenance Provenance` (nested JSONB)
  - `RegisteredAt time.Time`
  - `RegisteredBy string` (user ID or agent CPN ID)
  - `Deprecated bool`
  - `DeprecatedAt time.Time`
  - `DeprecationReason string`
- **REQ-002**: `Provenance` MUST carry: `AuthoringCPNID`, `FlowHash`, `ForgeRunID`, `PromptDigest` (sha256 of the prompt used), `SourcePath` (optional, for compiled tools), `SourceSHA256`.

### Ports & adapters

- **REQ-010**: Port `ToolRegistryRepository` in `cpn/persist/tool_registry.go` MUST expose: `Upsert(ctx, entry) error`, `Get(ctx, qualifiedName) (ToolEntry, error)`, `GetVersion(ctx, namespace, name, version) (ToolEntry, error)`, `ListByNamespace(ctx, namespace) ([]ToolEntry, error)`, `ListAll(ctx) ([]ToolEntry, error)`, `Deprecate(ctx, qualifiedName, reason string) error`, `Latest(ctx, namespace, name) (ToolEntry, error)`.
- **REQ-011**: Postgres schema (`0017_tool_registry.sql`):
  ```sql
  CREATE TABLE tools (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    namespace TEXT NOT NULL,
    name TEXT NOT NULL,
    version TEXT NOT NULL,
    schema JSONB NOT NULL,
    help_text TEXT NOT NULL DEFAULT '',
    man_page TEXT NOT NULL DEFAULT '',
    binary_path TEXT NOT NULL DEFAULT '',
    binary_sha256 TEXT NOT NULL DEFAULT '',
    origin TEXT NOT NULL,
    provenance JSONB NOT NULL DEFAULT '{}',
    registered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    registered_by TEXT NOT NULL,
    deprecated BOOLEAN NOT NULL DEFAULT false,
    deprecated_at TIMESTAMPTZ,
    deprecation_reason TEXT,
    UNIQUE (namespace, name, version)
  );
  CREATE INDEX idx_tools_latest ON tools(namespace, name, deprecated);
  ```

### Registry mutation

- **REQ-020**: In-memory `ToolRegistry` struct MUST wrap the current map with a `sync.RWMutex` and the new fields above.
- **REQ-021**: `Register(entry)` MUST: (a) validate the schema is parseable JSON Schema draft 2020-12; (b) check the Postgres repo for duplicate `(namespace,name,version)` → conflict error; (c) persist to Postgres; (d) update the in-memory map; (e) emit `EventToolRegistered`.
- **REQ-022**: Registration MUST be transactional: Postgres insert + in-memory update are a single logical operation; on Postgres failure, the in-memory state MUST NOT change.
- **REQ-023**: The registry MUST support multiple versions; the resolver returns the latest non-deprecated version when a caller asks for `namespace/name` without a version tag.

### NodeKindRegisterTool

- **REQ-030**: `NodeKindRegisterTool = "register_tool"` MUST be added.
- **REQ-031**: `RegisterToolConfig` fields: `Namespace string`, `Name string`, `Version string`, `Schema json.RawMessage`, `HelpText string`, `ManPage string`, `BinaryPath string`, `BinarySHA256 string`, `Provenance Provenance`.
- **REQ-032**: The transition consumes one token of color `ColorToolManifest` (new) and deposits one token of color `ColorArtifact` with payload `{qualified_name, id}`.
- **REQ-033**: New color `ColorToolManifest` MUST be added; default space = `SpaceComputation`.
- **REQ-034**: On `BinarySHA256 != ""`, the transition MUST first-run-HITL via GAP-6 (if GAP-6 present). Without GAP-6 live, the transition MUST log a warning and proceed (dev-mode allow).

### Admin HTTP

- **REQ-040**: Endpoints (admin-auth, same middleware as `/api/admin/models`):
  - `GET  /api/admin/tools` — paginated, filterable by origin/namespace/deprecated.
  - `GET  /api/admin/tools/:qn` (qn may be `namespace/name` or `namespace/name@version`).
  - `POST /api/admin/tools/:qn/deprecate` body `{reason}`.
  - `DELETE /api/admin/tools/:qn` — hard delete, admin-only, only allowed for `origin=agent-authored`.

### Boot reconciliation

- **REQ-050**: On server start, `bootToolRegistry()` MUST:
  1. Load all non-deprecated tools from Postgres → memory.
  2. Invoke existing code-level registrations (personality tools, etc.) with `origin=builtin`; these MAY overwrite a persisted entry only if the persisted `origin == "builtin"` (prevents a user-authored tool from being clobbered by a builtin with the same qualified name).
  3. Emit `EventBootToolsReconciled` with counts.

### Invocation semantics

- **REQ-060**: Existing `NodeKindTool` resolution MUST be extended: if the tool name is a qualified name, resolve latest-non-deprecated; if it is a bare name, fall back to the old exact-match path for backward compatibility.
- **REQ-061**: Invoking a deprecated tool MUST succeed but emit a `Deprecation` warning event.
- **REQ-062**: Invoking a tool whose `BinarySHA256` on disk no longer matches the manifest MUST fail with `ErrBinaryDrift` and be routed to the transition's `ErrorPlace`.

### Guidelines

- **GUD-001**: Never reuse a version once published. Bump.
- **GUD-002**: `HelpText` ≤ 500 chars; surface it to LLM tool-selection prompts.
- **GUD-003**: `ManPage` rendered Markdown; surface it on the admin UI only.
- **PAT-001**: All registry errors carry a `Code` (`ErrDuplicate`, `ErrNotFound`, `ErrSchemaInvalid`, `ErrBinaryDrift`).

## 4. Interfaces & Data Contracts

```go
type Provenance struct {
    AuthoringCPNID string `json:"authoring_cpn_id,omitempty"`
    FlowHash       string `json:"flow_hash,omitempty"`
    ForgeRunID     string `json:"forge_run_id,omitempty"`
    PromptDigest   string `json:"prompt_digest,omitempty"`
    SourcePath     string `json:"source_path,omitempty"`
    SourceSHA256   string `json:"source_sha256,omitempty"`
}

type ToolEntry struct {
    ID                string
    Namespace         string
    Name              string
    Version           string
    Schema            json.RawMessage
    Executor          ToolExecutor  // nil for binary-backed tools, populated for in-process
    HelpText          string
    ManPage           string
    BinaryPath        string
    BinarySHA256      string
    Origin            string
    Provenance        Provenance
    RegisteredAt      time.Time
    RegisteredBy      string
    Deprecated        bool
    DeprecatedAt      time.Time
    DeprecationReason string
}

type ToolRegistry interface {
    Register(ctx context.Context, e ToolEntry) error
    Get(ctx context.Context, qualifiedName string) (ToolEntry, error)
    Latest(ctx context.Context, namespace, name string) (ToolEntry, error)
    Deprecate(ctx context.Context, qualifiedName, reason string) error
    List(ctx context.Context, filter ToolFilter) ([]ToolEntry, error)
}

type RegisterToolConfig struct {
    Namespace    string
    Name         string
    Version      string
    Schema       json.RawMessage
    HelpText     string
    ManPage      string
    BinaryPath   string
    BinarySHA256 string
    Provenance   Provenance
}
```

## 5. Acceptance Criteria

- **AC-001**: Registering a tool with duplicate `(namespace,name,version)` returns `ErrDuplicate`.
- **AC-002**: `Latest(ns, name)` returns the highest semver among non-deprecated.
- **AC-003**: After deprecation, `Latest` skips the deprecated entry.
- **AC-004**: Boot reconciles Postgres-persisted tools into memory; a fresh process immediately knows about tools registered in the previous run.
- **AC-005**: A `NodeKindRegisterTool` transition with `BinarySHA256` set triggers GAP-6 first-run HITL (when GAP-6 present).
- **AC-006**: 1000 parallel Register calls from different goroutines produce exactly 1000 persisted rows (race test).
- **AC-007**: `GET /api/admin/tools?origin=agent-authored` returns only forge-produced tools.
- **AC-008**: Invoking `brae/http-get` resolves to the latest version by default; `brae/http-get@1.0.0` pins.
- **AC-009**: If the binary on disk at `BinaryPath` no longer matches `BinarySHA256`, invocation fails with `ErrBinaryDrift`.

## 6. Test Automation Strategy

- Unit: schema validation, semver comparison, deprecation flag logic.
- Integration: Postgres (`testcontainers-go`), end-to-end register → persist → reboot → resolve.
- Race: 1000 goroutines Register + Get concurrently.
- HTTP: admin endpoint authorisation + CRUD paths.
- Coverage ≥ 90% on `cpn/tools/registry.go` and `store/postgres/tool_registry_store.go`.

## 7. Rationale & Context

**Why versions?** Tools written by the agent will be rewritten by the agent. Without versioning, every rewrite is a replace-in-place that breaks in-flight flows. Latest-pointer semantics give us caching and rollback for free.

**Why separate `BinaryPath` and `Executor`?** Builtins are Go closures; agent-authored tools are binaries. The same `ToolEntry` shape describes both; the dispatcher branches on which field is populated.

**Why mandate GAP-6 first-run?** The forge will compile C code and install a binary. Trusting that file silently would be reckless. The registry is the integrity gate.

## 8. Dependencies & External Integrations

- **EXT-001**: GAP-6 (first-run ledger). If absent, fall open with a warning.
- **INF-001**: Postgres (existing).
- **PLT-001**: Go ≥ 1.22.

## 9. Examples & Edge Cases

### Register a compiled tool

```go
manifest := cpn.RegisterToolConfig{
    Namespace: "brae",
    Name:      "http-get",
    Version:   "1.0.0",
    Schema:    json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}`),
    HelpText:  "HTTP GET → stdout. Usage: http-get <url>",
    ManPage:   "# http-get(1)\n\nSYNOPSIS\n  http-get URL\n…",
    BinaryPath:   "/home/liwaisi/.local/brae/bin/http-get",
    BinarySHA256: "a3f5…",
    Provenance: cpn.Provenance{
        AuthoringCPNID: "forge-run-01931…",
        SourceSHA256:   "b7c8…",
    },
}
```

### Edge — re-register after binary drift

If someone replaces `/home/liwaisi/.local/brae/bin/http-get` on disk without bumping the version, the next invocation fails with `ErrBinaryDrift`. The fix: forge produces v1.0.1 and re-registers.

### Edge — two agents race-register the same version

Postgres unique constraint on `(namespace,name,version)` rejects the loser; loser receives `ErrDuplicate`.

## 10. Validation Criteria

- All new fields round-trip through Postgres.
- Invariant: in-memory map and Postgres table converge within 100 ms of any mutation (checked by test harness).
- No code path invokes a deprecated tool *by default* (must be explicit by version).

## 11. Related Specifications / Further Reading

- [spec-architecture-host-gate-security-policy.md](./spec-architecture-host-gate-security-policy.md) — GAP-6, first-run ledger.
- [spec-architecture-tool-forge-cpn.md](./spec-architecture-tool-forge-cpn.md) — GAP-5, primary producer.
- [spec-architecture-cpn-synthesis-instantiate.md](./spec-architecture-cpn-synthesis-instantiate.md) — GAP-4, secondary producer for CPN-backed tools.
