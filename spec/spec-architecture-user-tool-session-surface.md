---
title: User-Authored Tool Session Surface
version: 1.0
date_created: 2026-04-21
last_updated: 2026-04-21
owner: liwaisi / go-assistant backend
tags: [architecture, tools, cpn, session, llm, registry]
---

# Introduction

This specification closes a gap between brae's persistent tool catalogue (Postgres `tools` table), the UI's Tool Browser, and the tool surface the LLM actually sees when it plans a turn. Today a tool registered via `system/register_tool` in session *N* is visible in the UI and persisted in the DB, but sessions *N+1…* do not expose it to the LLM as a callable tool. The LLM must rediscover the binary through `bash_exec` and invoke it as a raw subprocess, losing per-tool schema, HITL, observability, and taxonomy routing. This specification defines the session-level contract that makes persisted user-authored tools first-class callable tools in every new session.

## 1. Purpose & Scope

**Purpose.** Ensure that every non-deprecated `user-authored` tool stored in the in-process tool registry is materialised as a `NodeKindTool` transition on each conversation CPN at session-creation time, with its schema, description, and an executable bound to the recorded `binary_path`, and appended to the `LLMTools` allow-list of every LLM transition that may call tools.

**Scope.**
- Conversation topologies built by `cmd/server/topologies.go` (`t-direct`, `t-execute`).
- Session bootstrap path in `internal/app/session_service.go`.
- Tool-registry entry lifecycle in `cpn/tools/registry.go` for `origin = user-authored`.
- System-tool wiring in `cmd/server/system_tools.go` (`register_tool` executor).

**Out of scope.**
- Awakening-minted tools (`origin = awakening`) — already covered by `spec-architecture-brae-awakening-toolbox-extension.md`.
- Tool-forge / JIT-synthesised tools (`origin = agent-authored`).
- Cross-host tool portability or multi-tenant catalogues.
- Changes to the `tools` SQL schema or the `GET /api/v1/tools` HTTP contract.

**Audience.** Backend engineers modifying `cpn/tools`, `cmd/server`, and `internal/app`.

## 2. Definitions

| Term | Definition |
|---|---|
| **CPN** | Coloured Petri Net — the topology engine powering brae sessions. |
| **Transition** | A node in a CPN. `NodeKindTool` transitions execute an in-process `ToolExecutor`. |
| **LLMTools** | A `[]string` on a `NodeKindLLM` transition listing the IDs of sibling `NodeKindTool` transitions the LLM may invoke as tool calls. |
| **ToolEntry** | The registry record in `cpn/tools`. Carries `Schema`, `Executor`, `BinaryPath`, `Origin`, etc. |
| **ToolMeta** | Subset of registry metadata (`Description`, `Parameters`, `RequiresHITL`, `Namespace`) injected onto a tool transition so `fire_llm.buildToolSchema` can render the LLM-visible schema. |
| **User-authored tool** | A tool with `Origin = "user-authored"` and `Namespace = "user"`, authored during a prior session via `system/register_tool`. |
| **Session bootstrap** | The block in `internal/app/session_service.go` that instantiates a conversation CPN, wires its `LLMClient`, `HostRuntime`, `ToolRegistry`, and HITL channels. |
| **HITL** | Human-in-the-loop approval gate. |

## 3. Requirements, Constraints & Guidelines

### Functional requirements

- **REQ-001** On session bootstrap, every non-deprecated `ToolEntry` with `Origin = "user-authored"` in the process-wide registry MUST be materialised as a `NodeKindTool` transition on the conversation CPN before `InjectIntoCPN` is called.
- **REQ-002** The synthesised transition's `ID` and `ToolName` MUST equal the entry's bare `Name` (not the qualified `namespace/name`). Rationale: OpenAI / OpenRouter tool-call names disallow `/`, and the CPN transition map is keyed by ID; `LLMTools` references IDs.
- **REQ-003** The synthesised transition MUST be appended to `LLMTools` of every `NodeKindLLM` transition that already lists any of the built-in system tools (`bash_exec`, `file_read`, `file_write`, `register_tool`). Today those are `t-direct` and `t-execute`; future transitions that opt into the system-tool surface inherit the user catalogue automatically.
- **REQ-004** After materialisation, `tools.Registry.InjectIntoCPN` MUST populate `ToolMeta.Description`, `ToolMeta.Parameters`, `ToolMeta.RequiresHITL = true`, `ToolMeta.Namespace = "user"` from the entry and attach a bound `ToolExecutor` (REQ-005).
- **REQ-005** The `ToolExecutor` bound to a user-authored entry MUST execute the recorded `BinaryPath` through the same `HostAdapter` + `HostGate` path that `bash_exec` uses, passing any `args` array from the LLM tool call as positional arguments to the binary. Stdin and environment are not forwarded in v1.
- **REQ-006** The executor MUST require HITL approval on every invocation in v1 (`RequiresHITL = true`). Rationale: the binary path came from the LLM in a prior session; re-verification each invocation preserves the "user controls execution" invariant until a fingerprinted trust model exists.
- **REQ-007** `system/register_tool` MUST attach the same `ToolExecutor` described in REQ-005 to the freshly registered entry so that the tool is callable *within the same session it was registered in*, not only in subsequent sessions.
- **REQ-008** When a user-authored entry is marked `Deprecated = true`, the session bootstrap MUST NOT materialise it.
- **REQ-009** The LLM-visible schema for a user-authored tool MUST include a `command`-free parameter surface derived from the entry's `JSONSchema` when present; when absent, fall back to a generic `{ "args": ["string"], "timeout_seconds": "integer" }` object so the LLM has a stable contract.

### Security requirements

- **SEC-001** `BinaryPath` MUST resolve inside the brae user's `$HOME` (same rule enforced by `system/register_tool` at authoring time). Session bootstrap MUST re-validate this on load; entries that no longer resolve inside `$HOME` are skipped with a warning log.
- **SEC-002** The HITL preview surface for a user-authored tool call MUST display the resolved `BinaryPath`, the exact `args`, and the entry's `Origin = "user-authored"` marker so the operator can distinguish authored-binary calls from `bash_exec` calls.
- **SEC-003** No binary is copied, recompiled, or fingerprinted by this specification. Integrity of the on-disk binary between authoring and invocation is a host-level concern out of scope here.

### Observability requirements

- **OBS-001** Session bootstrap MUST emit one structured log line per materialised user tool: `session.user_tools.injected` with fields `session_id`, `tool_name`, `binary_path`, `version`, `toolbox`.
- **OBS-002** Session bootstrap MUST emit `session.user_tools.skipped` with `reason` when skipping an entry (reasons: `deprecated`, `binary_path_outside_home`, `binary_missing`).
- **OBS-003** Each invocation MUST emit an `llm_calls` row with `transition_id = <bare tool name>` (consistent with existing system-tool rows).

### Constraints

- **CON-001** `LLMTools` is a flat `[]string` on the transition. Ordering is not semantically significant but SHOULD be stable across bootstraps for deterministic prompts. Sort user entries by bare name.
- **CON-002** No new SQL migration. All data already exists in `tools`.
- **CON-003** The fix MUST NOT alter the prompt-cache key shape used by upstream LLM transitions (`LLMTools` contributes to the tools block in the request, which is cache-sensitive). Adding/removing user tools invalidates the cache for that session — this is acceptable and expected.
- **CON-004** No changes to the UI. The Tool Browser remains a read-through of `GET /api/v1/tools`.

### Guidelines

- **GUD-001** Prefer synthesising transitions in `session_service.go` near the existing `InjectIntoCPN` call rather than in the topology factory. The factory runs once per model definition; the session bootstrap runs once per conversation and already has the registry handle.
- **GUD-002** A helper `tools.Registry.MaterialiseUserTools(c *cpn.CPN, llmHosts []string) []string` — returning the list of bare names it attached — keeps the wiring testable in isolation.

### Patterns

- **PAT-001** Follow the same shape as `registerSystemTools` in `cmd/server/system_tools.go` for executor construction: parse JSON args, resolve path, call `HostAdapter.Exec`, return `ShellResultPayload`.

## 4. Interfaces & Data Contracts

### 4.1 Registry → CPN handshake

```go
// MaterialiseUserTools synthesises a NodeKindTool transition on c for each
// non-deprecated user-authored entry in r, attaches a HostAdapter-bound
// executor, and appends the new transition IDs to every LLM transition listed
// in llmHostTransitionIDs. Returns the ordered list of bare tool names it
// attached.
func (r *Registry) MaterialiseUserTools(
    c *cpn.CPN,
    adapter cpn.HostAdapter,
    gate cpn.HostGate,
    llmHostTransitionIDs []string,
) []string
```

### 4.2 LLM tool-call payload contract (fallback schema when `JSONSchema` is empty)

```json
{
  "type": "object",
  "properties": {
    "args": {
      "type": "array",
      "items": { "type": "string" },
      "description": "Positional arguments passed to the user-authored binary."
    },
    "timeout_seconds": {
      "type": "integer",
      "minimum": 1,
      "maximum": 300,
      "description": "Maximum execution time. Defaults to 30."
    }
  }
}
```

### 4.3 Executor return payload

Same shape as `bash_exec`:

```go
cpn.ShellResultPayload{
    ExitCode   int
    Stdout     string
    Stderr     string
    DurationMs int64
    Truncated  bool
}
```

### 4.4 LLM-visible tool entry (example)

```json
{
  "name": "brae-monitor",
  "description": "Monitor dinámico de CPU, Memoria, Disco y Latencia de Red para brae.",
  "parameters": { "...": "as 4.2 or as stored JSONSchema" }
}
```

## 5. Acceptance Criteria

- **AC-001** Given a persisted `tools` row with `namespace='user'`, `origin='user-authored'`, `deprecated=false`, and a `binary_path` inside `$HOME`, when a new session starts, then the LLM request sent by `t-direct` MUST include an entry in `tools[]` whose `name` equals the row's `Name`.
- **AC-002** Given the precondition of AC-001, when the LLM emits `tool_calls` with `name = "<bare name>"` and `arguments = {"args": [...]}`, then the CPN MUST dispatch to the user-tool executor and the resulting `messages` row MUST carry `cpn_role` reflecting the tool invocation (not `bash_exec`).
- **AC-003** Given a tool marked `deprecated=true`, when a new session starts, then no transition with that bare name is added to the CPN, and the LLM tool surface MUST NOT include it.
- **AC-004** Given a registered tool whose `binary_path` no longer resolves inside `$HOME` (e.g., moved), when a new session starts, then the bootstrap MUST log `session.user_tools.skipped` with reason `binary_path_outside_home`, MUST NOT attach the transition, and MUST NOT fail session creation.
- **AC-005** Given the session in which `system/register_tool` is invoked, when the LLM's next turn runs, then the newly registered tool MUST be callable as a tool call in that same session (no reconnect required).
- **AC-006** Given two concurrently open sessions and a tool registered in session A, when session B reaches its next LLM turn, then session B MUST see the new tool in its LLM request. (Today's process-wide registry already supports this; the spec only requires that the materialisation step run per-turn or listen to registry updates — v1 MAY require session reload, documented under OPEN-001.)
- **AC-007** Given the ToolBrowser UI and a fresh tool registration, when the user reopens the UI, then the tool is listed (unchanged behaviour, regression guard).

## 6. Test Automation Strategy

- **Test Levels**: unit + integration (in-process CPN) + end-to-end (HTTP + Postgres).
- **Frameworks**: `testing` + table-driven tests (Go stdlib), existing `cpn/tools/registry_test.go` patterns.
- **Unit tests**
  - `Registry.MaterialiseUserTools` with fixtures: zero entries, one entry, deprecated entry, entry with empty `JSONSchema` (fallback schema), entry whose `BinaryPath` escapes `$HOME`.
  - Executor: args forwarding, non-zero exit surfaced as `ShellResultPayload`, path-denied error pretty-printed.
- **Integration tests**
  - Session-bootstrap path asserts `t-direct.LLMTools` includes the materialised name.
  - HITL channel wiring asserts a `needsHITL = true` wiring now fires for the new tool transition.
- **E2E**
  - Reuse the existing session-service + Postgres harness: register a tool in session A, open session B, trigger `t-direct`, assert the LLM `request_messages.tools[]` contains the bare name.
- **CI/CD Integration**: covered by the existing `go test ./...` gate; no new workflow.
- **Coverage Requirements**: maintain the project's ≥85% line coverage on `cpn/tools` and `internal/app`.
- **Performance Testing**: not required. Materialisation is O(N) over a registry that is in practice <50 user tools.

## 7. Rationale & Context

The `tools` table, the `ToolBrowser` UI, and `tools.Registry.Bootstrap` all correctly converge on the idea that user-authored tools persist across sessions. The LLM surface was the last hop that never got wired: `cmd/server/topologies.go:456` and `:836` hard-code `LLMTools = []string{"bash_exec","file_read","file_write","register_tool"}`, and the topology factory creates `NodeKindTool` transitions only for those four. `tools.Registry.InjectIntoCPN` fills in `ToolMeta` and `Executor` only for transitions that *already* exist in the CPN — it does not create new ones. The net effect is: (a) the LLM in session *N+1* is told it has four tools; (b) if it nevertheless guesses the binary name and invokes `bash_exec ~/.local/bin/<name>`, the call works but bypasses the per-tool schema/HITL/observability the authoring path promised; (c) users perceive brae as amnesic about tools it literally just registered.

Observed in the wild (2026-04-21, DB snapshot of sessions `4056da7f…` and `790f3eef…`): session A registered `user/brae-monitor` successfully; session B's first LLM response listed only `bash_exec` and `register_tool` as tools. The user had to walk brae through `ls ~/.local/bin` before it discovered its own prior work.

Fixing this at session-bootstrap (rather than in the topology factory) keeps the factory stateless and pushes the session-scoped state — "which user tools exist right now" — to the layer that already owns session-scoped wiring.

## 8. Dependencies & External Integrations

### Internal packages

- **INT-001** `cpn/tools` — registry lookup and materialisation helper.
- **INT-002** `cpn/` — `Transition`, `CPN`, `HostAdapter`, `HostGate`, `ShellResultPayload`.
- **INT-003** `cmd/server/system_tools.go` — reuse `resolveUserPath`, gate-denied error formatting, the `bash_exec` executor as a template.
- **INT-004** `internal/app/session_service.go` — insertion point for the new bootstrap call.

### Infrastructure

- **INF-001** Postgres `tools` table — already populated. No schema change.

### Data

- **DAT-001** Existing `ToolRegistryStore` Postgres repository. Loaded into memory once at process start via `toolReg.Bootstrap(ctx, repo)` in `cmd/server/main.go`.

### Compliance

- **COM-001** No PII or secret handling introduced. User-authored binaries live under the user's own `$HOME`.

## 9. Examples & Edge Cases

### 9.1 Happy path

```text
registry contains:
  user/brae-monitor@0.1.0  binary=/home/liwaisi/.local/bin/brae-monitor

session bootstrap:
  - creates transition t-brae-monitor (NodeKindTool, ToolName="brae-monitor")
  - appends "brae-monitor" to t-direct.LLMTools and t-execute.LLMTools
  - InjectIntoCPN fills ToolMeta and Executor

LLM turn:
  tools[] sent to model includes:
    { name: "brae-monitor", description: "Monitor dinámico de CPU…", parameters: {…} }
  model responds:
    tool_call { name: "brae-monitor", arguments: {"args": []} }
  runtime:
    HITL preview → adapter.Exec("/home/liwaisi/.local/bin/brae-monitor", [])
    → ShellResultPayload{ExitCode:0, Stdout:"…"}
```

### 9.2 Deprecated tool

```text
registry row: deprecated=true
bootstrap: skipped, no transition created, LLMTools unchanged.
```

### 9.3 Binary moved outside $HOME

```text
registry row: binary_path="/usr/local/bin/brae-monitor"
bootstrap: log session.user_tools.skipped reason=binary_path_outside_home
LLMTools unchanged. Session still starts.
```

### 9.4 In-session registration (REQ-007)

```text
turn N:   register_tool → new entry attached with executor
turn N+1: t-direct's LLMTools is re-derived from the live registry
          the tool appears in the LLM request
```

## 10. Validation Criteria

- **VAL-001** Unit test: `MaterialiseUserTools` attaches exactly one transition per non-deprecated user entry and skips deprecated ones.
- **VAL-002** Unit test: executor forwards `args`, handles non-zero exits without erroring, surfaces gate denials.
- **VAL-003** Integration test: session bootstrap with a registry pre-loaded from Postgres produces a `t-direct` whose `LLMTools` contains the bare user-tool name.
- **VAL-004** Integration test: `system/register_tool` makes the tool immediately callable in the same session.
- **VAL-005** Manual: open a second conversation after registering a tool in the first; ask brae to list its tools; brae MUST include the user tool in its enumeration without running `ls`.
- **VAL-006** Regression: `GET /api/v1/tools` continues to list the tool with unchanged shape.

## 11. Related Specifications / Further Reading

- `spec/spec-architecture-brae-awakening.md` — awakening-time tool registration.
- `.docs/specs/spec-architecture-brae-bash-capability-wiring.md` — `bash_exec` wiring and gate contract (template for the user-tool executor).
- `back/go-assistant/cpn/tools/registry.go` — current registry and `InjectIntoCPN` implementation.
- `back/go-assistant/cmd/server/topologies.go` — conversation topology factory (lines 456, 836).
- `back/go-assistant/cmd/server/system_tools.go` — `register_tool` executor (authoring path).
