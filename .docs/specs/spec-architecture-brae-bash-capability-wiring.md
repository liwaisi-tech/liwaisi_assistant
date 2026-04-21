---
title: "GAP-11: Bash Capability Wiring — Connecting NodeKindBash to the Unified Conversation Topology"
version: "1.0"
date_created: 2026-04-18
last_updated: 2026-04-18
owner: liwaisi-tech
tags: [architecture, cpn, bash, tool-registry, topology, gap-11]
---

# Introduction

GAP-1 implemented the `NodeKindBash` execution engine (HostAdapter, fireBash, BashSessionManager, BashConfig) and GAP-2 implemented host capability discovery. Neither was connected to the main conversation CPN that users interact with. As a result, brae responds to requests like "execute this command" or "write a file" with refusals derived from LLM training data, because the system prompt does not declare these capabilities and the topology has no tool transitions the LLM can call.

This specification defines the complete wiring from the existing infrastructure to the unified conversation topology, including: three system tool registrations (`bash_exec`, `file_read`, `file_write`), topology changes to expose those tools to the LLM via the `LLMTools` mechanism, host capability context injection into LLM system prompts, a `braeIdentity` update declaring execution capabilities, and a critical bug fix for `findSourcePlace` that causes ~50% of message submissions to return HTTP 500.

## 1. Purpose & Scope

**Purpose**: Enable brae to execute shell commands, read and write files, and proactively surface its OS capabilities in conversation, by wiring the already-implemented GAP-1/GAP-2 infrastructure into the unified topology's LLM transitions.

**Scope**:
- `back/go-assistant/cmd/server/main.go` — register system tools after host runtime creation
- `back/go-assistant/cmd/server/topologies.go` — add places, transitions, LLMTools wiring, update `braeIdentity`
- `back/go-assistant/cpn/fire_llm.go` — inject host capability context into effective system prompt
- `back/go-assistant/internal/app/session_service.go` — fix `findSourcePlace` race condition
- `front/react-assistant/` — visual affordance for tool execution events in the chat UI

**Not in scope**: PTY session management (GAP-9), new binary compilation tools, and changes to the HostGate policy engine (GAP-6).

**Intended audience**: Backend engineers implementing GAP-11 in Go, frontend engineers adding tool execution UI, and reviewers validating correctness against the CPN execution model.

## 2. Definitions

| Term | Definition |
|------|------------|
| **GAP-1** | The NodeKindBash implementation: HostAdapter port, fireBash dispatcher, BashSessionManager. Lives in `cpn/fire_bash.go` and `infra/host/`. |
| **GAP-2** | Host capability discovery: host-discovery-cpn, HostCapabilityRepository, HostCapabilitySnapshot. The snapshot is seeded into `p-host-capabilities` on every session CPN at bootstrap. |
| **GAP-11** | This specification. Wires GAP-1/GAP-2 into the unified conversation topology. |
| **LLMTools** | A `[]string` field on a `*cpn.Transition` (`NodeKindLLM`). Each entry is the map key of another transition in the same CPN. The LLM receives a tool schema for each and may emit tool-call responses invoking them. |
| **ToolEntry** | A record in `cpn/tools.Registry` containing a `ToolSchema`, `ToolExecutor` closure, and metadata. Registry.InjectIntoCPN() populates `Transition.ToolMeta` and `Transition.Executor` from these entries. |
| **buildToolSchema** | `cpn/fire_llm.go` function that converts a `*Transition` with a non-nil `ToolMeta` into an `LLMTool` (name + description + JSON Schema parameters). This is what the LLM sees. |
| **tc.Arguments** | Raw `[]byte` of the JSON object the LLM provided as tool call arguments. Passed to `toolTransition.Executor` as `Token{Color: ColorJSON, Payload: string(tc.Arguments)}`. |
| **findSourcePlace** | `internal/app/session_service.go` helper that returns the first `*Place` in `c.Places` not referenced by any transition's `OutputPlaces`. Used by `SendMessage` to find where to deposit the user's input token. |
| **p-host-capabilities** | Well-known CPN place key (`cpn.WellKnownHostCapabilitiesPlace`). Created by `cpn.SeedHostSnapshot` and seeded at session creation with `HostCapabilitySnapshot`. Color: `ColorHostFact`, Space: `SpaceComputation`. |
| **SeedHostSnapshot** | `cpn/host_snapshot.go` function that dynamically adds `p-host-capabilities` to the CPN's `Places` map if it is not already present, then deposits a `ColorHostFact` token. |
| **HostAdapter** | `cpn.HostAdapter` interface: `Exec`, `SpawnPTY`, `KillPID`, `ReadFile`, `WriteFile`, `Stat`. Production implementation: `infra/host.OSHostAdapter`. |
| **OSHostAdapter.AllowedRoot** | Security boundary for ReadFile/WriteFile. Defaults to `$HOME/.local/brae/`. Paths outside this root are rejected with `HostErrCodePathDenied`. |
| **BashConfig** | Per-transition configuration for `NodeKindBash`. Fields: `Command`, `Args`, `Stdin`, `Env`, `Cwd`, `Timeout`, `SandboxProfile`, `AllowNonZeroExit`, `SessionID`, `EmitMode`. |
| **ShellResultPayload** | Go struct `{ExitCode int, Stdout string, Stderr string, DurationMs int64, Truncated bool}`. Used as token payload by `fireBash`. |
| **HostCapabilitySnapshot** | `cpn/persist.HostCapabilitySnapshot`. Fields: `Identity` (hostname, user, env), `Kernel` (OS, arch, CPU, mem), `Binaries` ([]BinaryProbe), `Capabilities` ([]Capability). |

## 3. Requirements, Constraints & Guidelines

### Bug Fix

- **REQ-FIX-001**: `SendMessage` in `internal/app/session_service.go` MUST look up the user input deposit place by the well-known key `"p-input"` first, falling back to `findSourcePlace` only when `"p-input"` is absent. This eliminates the non-deterministic 500 error caused by `SeedHostSnapshot` adding `p-host-capabilities` (ColorHostFact, SpaceComputation) to the CPN map, which makes it a candidate source alongside `p-input` (ColorString, SpaceSurface) under random Go map iteration. Depositing a ColorString/SpaceSurface token into `p-host-capabilities` fails with `ErrSpaceViolation`, producing a 500.

### System Tool Registration

- **REQ-001**: Three system tools MUST be registered in the tool registry at startup in `cmd/server/main.go`, after `hostAdapter` and `hostSessions` are created and before `appService` is constructed: `bash_exec`, `file_read`, and `file_write`. All three MUST use namespace `"system"` and origin `tools.OriginBuiltin`.
- **REQ-002**: The `bash_exec` executor MUST capture `hostAdapter` and `hostGateImpl` by closure. It MUST parse `tc.Arguments` (a JSON string from `fireLLM`) into `{command string, args []string, cwd string, timeout_seconds int}` and call `hostAdapter.Exec()`. It MUST return a `ColorShellResult` token with `ShellResultPayload`. It MUST respect the gate; the gate is already wired in `fireBash`, but since `bash_exec` is a `NodeKindTool` (not `NodeKindBash`), it calls the adapter directly — the executor MUST enforce the gate itself via `hostGateImpl.Check()` before calling `Exec`.
- **REQ-003**: The `file_read` executor MUST capture `hostAdapter` by closure. It MUST parse `{"path": string}` from `tc.Arguments` and call `hostAdapter.ReadFile()`. It MUST return a `ColorArtifact` token with the file contents as a UTF-8 string payload. `HostErrCodePathDenied` MUST be returned as-is (not wrapped) so the LLM sees a structured error.
- **REQ-004**: The `file_write` executor MUST capture `hostAdapter` by closure. It MUST parse `{"path": string, "content": string, "mode": int}` from `tc.Arguments` and call `hostAdapter.WriteFile()`. `mode` defaults to `0o644` when absent or zero. It MUST return a `ColorArtifact` token with `{"written": true, "path": <path>}` on success.
- **REQ-005**: All three tool schemas MUST include a `Parameters` JSON Schema (type: object, required fields, property descriptions). The schemas are used by `buildToolSchema` in `fireLLM` to generate the tool description sent to the LLM.
- **REQ-006**: The tool registry MUST be sealed (if the registry implementation supports sealing) after all system tools are registered and before user tools can be registered. System-namespace tools must not be overwritten by user registrations.

### Topology Changes

- **REQ-007**: `unifiedTopologyFactory` in `cmd/server/topologies.go` MUST declare three new `NodeKindTool` transitions keyed exactly as `"bash_exec"`, `"file_read"`, and `"file_write"` in the `transitions` map. Each MUST have `ToolName` set to its map key (e.g., `tBashExec.ToolName = "bash_exec"`). The key and `ToolName` must match because `fireLLM` looks up `c.Transitions[tc.ToolName]` using the tool call name emitted by the LLM, which equals `LLMTool.Name` = `tool.ToolName`.
- **REQ-008**: `t-direct` MUST have `LLMTools: []string{"bash_exec", "file_read", "file_write"}` set.
- **REQ-009**: `t-execute` MUST have `LLMTools: []string{"bash_exec", "file_read", "file_write"}` set. The executor transition (task execution path) must have the same tool access as the direct conversation transition.
- **REQ-010**: The three tool transitions MUST have no `InputPlaces` and no `OutputPlaces` set (`[]string{}`). They are inline tool executors invoked via the LLM tool-call loop in `fireLLM`; they are not CPN arc-connected places. The executor receives the token directly from `fireLLM` (not from a CPN place) and returns a result token that `fireLLM` formats as a tool result message.
- **REQ-011**: `unifiedTopologyFactory` MUST declare `p-host-capabilities` as an explicit place: `cpn.NewPlace("p-host-capabilities", cpn.ColorHostFact, cpn.SpaceComputation)`. This prevents `Validate` from raising arc reference errors if any future transition references it, and makes the topology self-documenting. The session service will seed it via `cpn.SeedHostSnapshot` as before.
- **CON-001**: Tool transitions with no `InputPlaces` and no `OutputPlaces` MUST NOT cause `Validate` arc reference errors. Verify that `checkArcReferences` handles empty slices without error.
- **CON-002**: `p-host-capabilities` MUST NOT be referenced as an `InputPlace` of any transition in the unified topology. It is read via `cpn.PeekHostSnapshot` in `fireLLM`; consuming it would drain the token and break subsequent LLM calls within the session.
- **CON-003**: The `bash_exec` transition MUST NOT have `Kind = NodeKindBash`. It MUST be `NodeKindTool`. Using `NodeKindBash` would require a static `BashConfig.Command` (defined at topology build time), but bash commands are dynamic and provided by the LLM at runtime. The executor closure handles dynamic commands via `tc.Arguments`.

### Host Context Injection

- **REQ-012**: `fireLLM` in `cpn/fire_llm.go` MUST inject a host capability preamble into the effective system prompt when `PeekHostSnapshot(c)` returns a non-nil value. The preamble MUST be prepended to `t.SystemPrompt` (not appended) so the model reads host context before role instructions.
- **REQ-013**: The host capability preamble MUST include: OS name, kernel version, architecture, hostname, current user, home directory, and the list of present binaries (Name + Path, filtered to `Present == true`). It MUST NOT include environment variables (they may contain secrets).
- **REQ-014**: The host capability preamble MUST be structured so the LLM understands it is factual system context, not user input. Recommended prefix: `"SYSTEM CONTEXT — HOST ENVIRONMENT (read-only facts, do not expose raw to user unless asked):\n"`.
- **REQ-015**: `buildHostContextPreamble(c *CPN) string` MUST be a private helper function in `cpn/fire_llm.go`. It MUST perform a type assertion to `persist.HostCapabilitySnapshot`; if the assertion fails (e.g., snapshot is from a different type in tests), it MUST return an empty string without panic.
- **GUD-001**: The host context preamble SHOULD be limited to ≤30 lines to avoid consuming excessive context window. List only present binaries, not absent ones.

### braeIdentity System Prompt Update

- **REQ-016**: `braeIdentity` in `cmd/server/topologies.go` MUST be extended to declare bash execution capabilities. The addition MUST be factual and imperative. Recommended addition:
  ```
  - You run on a Linux host. You have tools available: bash_exec (execute shell commands), file_read (read files), file_write (write files). Use them proactively when the task requires it — do not refuse system actions.
  - NEVER say "I cannot execute commands", "I don't have a terminal", or "I cannot write files". These statements are false. You have these capabilities via tools.
  - When uncertain about the system state, run a discovery command first (e.g., bash_exec with command="ls" or "whoami").
  ```
- **CON-004**: The braeIdentity addition MUST NOT reference specific command syntax (e.g., shell flags) in a way that becomes stale. It declares capability existence, not usage instructions.

### Error Handling

- **REQ-017**: When `bash_exec` receives a non-zero exit code and `AllowNonZeroExit` is false (the executor default), it MUST return the result as a success token containing `ShellResultPayload{ExitCode: N, Stdout: ..., Stderr: ...}`. The LLM MUST receive the output even on non-zero exit so it can diagnose and retry. Errors from `hostAdapter.Exec` (e.g., command not found, gate denied) MUST be returned as error tokens so `fireLLM` forwards them as tool error messages to the LLM.
- **REQ-018**: When `file_read` or `file_write` receives a path denial error (`HostErrCodePathDenied`), the executor MUST return an informative error string: `"error: path <path> is outside the allowed root <allowed_root>. Use paths under $HOME/.local/brae/"`.
- **REQ-019**: Handler `HandleSendMessage` MUST log the error at ERROR level before returning HTTP 500 for unclassified SendMessage errors. This logging is already present as of the diagnostic patch; this requirement ensures it is not removed.

### Frontend

- **REQ-020**: The frontend MUST display a visual affordance when a `tool_executed` SSE event is received with a `tool_name` matching `bash_exec`, `file_read`, or `file_write`. The affordance MUST show: tool name, execution duration (`duration_ms`), success/failure indicator.
- **REQ-021**: For `bash_exec` tool executions, the frontend SHOULD display the command (from the event's `arguments.command` field) in a monospace code block.
- **REQ-022**: Tool execution affordances MUST be collapsible. Default state: collapsed for `bash_exec` results with exit code 0; expanded for non-zero exit codes.

## 4. Interfaces & Data Contracts

### 4.1 Tool Arguments JSON Schema

#### bash_exec

```json
{
  "type": "object",
  "required": ["command"],
  "properties": {
    "command": {
      "type": "string",
      "description": "The executable to run (e.g., 'ls', 'git', 'python3')."
    },
    "args": {
      "type": "array",
      "items": { "type": "string" },
      "description": "Arguments passed to the command. Do not inline args into command string."
    },
    "cwd": {
      "type": "string",
      "description": "Working directory for the command. Defaults to the user home directory."
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

#### file_read

```json
{
  "type": "object",
  "required": ["path"],
  "properties": {
    "path": {
      "type": "string",
      "description": "Absolute path to the file to read. Must be inside $HOME/.local/brae/ or user home."
    }
  }
}
```

#### file_write

```json
{
  "type": "object",
  "required": ["path", "content"],
  "properties": {
    "path": {
      "type": "string",
      "description": "Absolute path to write. Must be inside $HOME/.local/brae/."
    },
    "content": {
      "type": "string",
      "description": "UTF-8 content to write to the file."
    },
    "mode": {
      "type": "integer",
      "description": "Unix file permission mode as decimal integer (e.g., 420 = 0644). Defaults to 420."
    }
  }
}
```

### 4.2 Executor Input Token

All three tool executors receive a token with:
```go
Token{
    Color:   cpn.ColorJSON,
    Payload: string(tc.Arguments),  // JSON-encoded arguments object
}
```
The `Payload` is always a `string` containing the raw JSON from the LLM tool call. Executors MUST use `json.Unmarshal([]byte(in.Payload.(string)), &args)`.

### 4.3 Executor Output Tokens

#### bash_exec output (success)
```go
Token{
    Color:   cpn.ColorShellResult,
    Payload: cpn.ShellResultPayload{
        ExitCode:   0,
        Stdout:     "<captured stdout>",
        Stderr:     "<captured stderr>",
        DurationMs: <elapsed>,
        Truncated:  false,
    },
}
```

#### bash_exec output (gate denied / exec error)
Return `error` from executor. `fireLLM` converts it to `"error: <message>"` in the tool result message to the LLM.

#### file_read output (success)
```go
Token{
    Color:   cpn.ColorArtifact,
    Payload: "<file contents as string>",
}
```

#### file_write output (success)
```go
Token{
    Color:   cpn.ColorArtifact,
    Payload: map[string]any{"written": true, "path": path},
}
```

### 4.4 Host Context Preamble Format

```
SYSTEM CONTEXT — HOST ENVIRONMENT (read-only facts, do not expose raw to user unless asked):
- OS: <kernel.OS> <kernel.Kernel> (<kernel.Arch>)
- Hostname: <identity.Hostname>
- User: <identity.User> (uid=<identity.UID> home=<identity.Home>)
- CPU: <kernel.CPUCount> cores | RAM: <kernel.MemMB> MB
- Present binaries: <name>:<path>, <name>:<path>, ... (present only)
```

### 4.5 Tool Transition Declarations in Topology

```go
// In unifiedTopologyFactory, inside the transitions map:

tBashExec := cpn.NewTransition("bash_exec", cpn.NodeKindTool, []string{}, []string{})
tBashExec.ToolName = "bash_exec"

tFileRead := cpn.NewTransition("file_read", cpn.NodeKindTool, []string{}, []string{})
tFileRead.ToolName = "file_read"

tFileWrite := cpn.NewTransition("file_write", cpn.NodeKindTool, []string{}, []string{})
tFileWrite.ToolName = "file_write"

// Keyed by their ToolName so fireLLM's c.Transitions[tc.ToolName] lookup succeeds:
transitions["bash_exec"] = tBashExec
transitions["file_read"] = tFileRead
transitions["file_write"] = tFileWrite

// Wire LLMTools on direct and execute transitions:
tDirect.LLMTools = []string{"bash_exec", "file_read", "file_write"}
tExecute.LLMTools = []string{"bash_exec", "file_read", "file_write"}
```

### 4.6 findSourcePlace Fix

```go
// internal/app/session_service.go — SendMessage, replace:
//   source := findSourcePlace(session.Root)
// with:

source := session.Root.Places["p-input"]
if source == nil {
    source = findSourcePlace(session.Root)
}
```

### 4.7 ToolEntry Registration (main.go)

```go
// After hostAdapter is initialized, before NewSessionService is called:

toolReg.Register(&tools.ToolEntry{
    Schema: &tools.ToolSchema{
        Name:        "bash_exec",
        Namespace:   "system",
        Description: "Execute a shell command on the host OS. Returns stdout, stderr, exit code.",
        InputColor:  cpn.ColorJSON,
        OutputColor: cpn.ColorShellResult,
        Parameters:  json.RawMessage(bashExecParamsSchema),
    },
    Executor:  makeBashExecExecutor(hostAdapter, hostGateImpl),
    Origin:    tools.OriginBuiltin,
    Namespace: "system",
    Name:      "bash_exec",
    Version:   "1.0",
})

// Similar for file_read and file_write.
```

`makeBashExecExecutor`, `makeFileReadExecutor`, `makeFileWriteExecutor` are constructor functions defined in `cmd/server/` that return closure-based `tools.ToolExecutor`.

### 4.8 SSE Event for Tool Execution (Frontend)

Existing `tool_executed` event payload (already emitted by `fireLLM`):
```json
{
  "type": "tool_executed",
  "transition_id": "bash_exec",
  "transition_kind": "tool",
  "payload": {
    "tool_name": "bash_exec",
    "namespace": "system",
    "duration_ms": 142,
    "success": true,
    "error": ""
  }
}
```

Frontend reads `payload.tool_name` to determine display affordance. The `arguments` are NOT in the event payload (they are only in the LLM message history). The frontend retrieves displayed commands from the streaming SSE `chunk` events OR from the `tool_executed` event's `transition_id`.

> **Note for frontend**: To show the command, the backend should add `arguments` to `ToolExecutedPayload`. See REQ-FE-001 below.

- **REQ-FE-001**: `ToolExecutedPayload` in `cpn/event.go` MUST be extended with `Arguments json.RawMessage \`json:"arguments,omitempty"\`` to carry the tool call arguments. `fireLLM` already has `tc.Arguments` at the point of event emission (`"arguments": string(tc.Arguments)` in the payload map); this field should be promoted to the typed struct.

## 5. Acceptance Criteria

- **AC-001**: Given a new session and the message "what is your hostname?", when the CPN runs, then `t-direct` classifies this as conversation, the LLM calls `bash_exec` with `{"command": "hostname"}`, and the response contains the actual hostname of the container.
- **AC-002**: Given a new session and the message "write hello world to /home/app/.local/brae/test.txt", when the CPN runs, then `t-direct` or `t-execute` calls `file_write`, the file is created at the given path, and the response confirms success.
- **AC-003**: Given a new session and the message "read /home/app/.local/brae/test.txt", when the CPN runs, then `file_read` is called, and the response contains the file's content.
- **AC-004**: Given a message attempt with `POST /api/v1/sessions/{id}/messages`, when the CPN has a `p-host-capabilities` place in its map (from `SeedHostSnapshot`), then `source.Deposit(tok)` MUST succeed — verified by HTTP 202, never HTTP 500 for this specific cause.
- **AC-005**: Given a session whose `p-host-capabilities` token contains a `HostCapabilitySnapshot` with `Kernel.OS = "linux"` and 3 present binaries, when `t-direct` fires, then the LLM receives a system prompt that begins with `"SYSTEM CONTEXT — HOST ENVIRONMENT"` and includes the OS and binary names.
- **AC-006**: Given a `bash_exec` call with `{"command": "ls", "args": ["/nonexistent"]}`, when the command exits with code 2, then the LLM receives a tool result containing `ShellResultPayload{ExitCode: 2, Stderr: "ls: cannot access..."}` and does not treat it as an executor error.
- **AC-007**: Given a `file_read` call with `{"path": "/etc/passwd"}`, when the path is outside `AllowedRoot`, then the executor returns an error string `"error: path /etc/passwd is outside the allowed root..."` and the LLM receives it as a tool error message.
- **AC-008**: Given brae is asked "can you execute shell commands?", when the classifier routes to `t-direct`, then the response MUST NOT contain phrases like "I cannot execute", "no access to terminal", or "privacy reasons".
- **AC-009**: Given the frontend receives a `tool_executed` event with `tool_name = "bash_exec"`, when rendered, then a collapsed code block shows the executed command and a duration badge is visible.
- **AC-010**: Given `go test ./... -race ./back/go-assistant/...`, then all tests pass with zero data race reports.

## 6. Test Automation Strategy

- **Test Levels**: Unit tests for executor closures (mock HostAdapter), integration tests for topology (real CPN run with stub LLM), end-to-end tests in Docker.
- **Frameworks**: Go standard `testing`, `testify/assert` for assertions. Frontend: Vitest + React Testing Library.
- **Unit Tests — Executor Closures**:
  - `TestMakeBashExecExecutor_ParsesArguments` — verifies JSON argument parsing to `ExecRequest`
  - `TestMakeBashExecExecutor_GateDenied` — mock gate returns error, verify executor returns error
  - `TestMakeBashExecExecutor_NonZeroExit` — verify `ShellResultPayload` returned, not error
  - `TestMakeFileReadExecutor_PathDenied` — mock adapter returns `HostErrCodePathDenied`, verify friendly error message
  - `TestMakeFileWriteExecutor_DefaultMode` — missing `mode` in args → `0o644` used
- **Unit Tests — findSourcePlace Fix**:
  - `TestSendMessage_NoPanicWhenHostCapabilitiesPresent` — CPN with `p-host-capabilities` seeded, `SendMessage` returns nil error and no 500
- **Unit Tests — Host Context Preamble**:
  - `TestBuildHostContextPreamble_FormatsSnapshot` — verifies all required fields appear
  - `TestBuildHostContextPreamble_EmptyOnNilSnapshot` — CPN without snapshot returns empty string
  - `TestBuildHostContextPreamble_WrongTypeReturnsEmpty` — type assertion failure returns empty string
- **Integration Tests — Topology**:
  - `TestUnifiedTopology_ContainsBashTools` — `unifiedTopologyFactory` returns CPN where `t-direct.LLMTools` contains `"bash_exec"`, `"file_read"`, `"file_write"`
  - `TestUnifiedTopology_ToolTransitionsHaveMatchingKey` — for each tool transition, `t.ToolName == mapKey`
  - `TestUnifiedTopology_ValidatePasses` — `cpn.Validate(places, transitions)` returns nil
- **Frontend Tests**:
  - `TestToolExecutionBadge_RendersForBashExec` — given `tool_executed` event with `tool_name="bash_exec"`, the component renders a tool badge with duration
  - `TestToolExecutionBadge_CollapsedByDefault` — bash_exec with exit 0 renders collapsed
  - `TestToolExecutionBadge_ExpandedOnNonZeroExit` — bash_exec with exit ≠ 0 renders expanded
- **CI**: All tests run in `docker compose exec backend go test -race ./...` equivalent in CI pipeline.
- **Coverage**: Executor closures: 100%. findSourcePlace fix: 100%. Host context preamble: 100%.

## 7. Rationale & Context

**Why NodeKindTool instead of NodeKindBash for LLM-callable tools?**
`NodeKindBash` requires a static `BashConfig.Command` defined at topology build time. The LLM provides commands dynamically at inference time. A `NodeKindTool` executor closure can parse the LLM's tool call arguments at runtime. This loses GAP-1's streaming (EmitPerLine) for tool-call invocations, which is acceptable because tool-call loop results are short (they return to the LLM as tool result messages, not streamed to the user). GAP-9 PTY streaming remains available for long-running sessions via `NodeKindBash` + `BashConfig.SessionID` in sub-CPNs.

**Why inject host context into the system prompt rather than history?**
System prompts (T1 tier in `BuildContext`) are always included regardless of history window size. History messages (T3) can be truncated. Host capabilities are stable facts that the LLM needs in every turn; they belong in T1. Injecting into history would cause them to be lost in long conversations.

**Why fix findSourcePlace instead of fixing SeedHostSnapshot?**
`SeedHostSnapshot` is correct: it dynamically adds the capabilities place to whatever topology it's called on. Constraining it would break GAP-2's portability guarantee (it works on any topology). `findSourcePlace` should be token-type-aware; returning any non-output place is too broad. The direct `Places["p-input"]` lookup is the simplest fix with zero behavioral change for all other code paths.

**Why no dedicated p-bash-result place?**
The LLM tool-call loop in `fireLLM` manages tool result routing internally without CPN arcs. Adding arc-connected bash result places would require the LLM to stop after each tool call and be re-triggered by the next transition, breaking the agentic loop. The inline execution pattern (tool call → executor → tool result in the same LLM message sequence) is the correct model here.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: Linux host OS — `bash_exec` requires the `command` to exist in PATH inside the container. The host capability snapshot (GAP-2) provides the present-binary list so the LLM can make informed choices.

### Infrastructure Dependencies
- **INF-001**: `infra/host.OSHostAdapter` — already initialized in `main.go`. GAP-11 adds closure-based tool executors that capture it by reference.
- **INF-002**: `infra/host/gate.PolicyHostGate` — already initialized in `main.go`. `bash_exec` executor MUST call `hostGateImpl.Check()` before `hostAdapter.Exec()`.
- **INF-003**: `cpn/tools.Registry` — already initialized in `main.go`. GAP-11 adds three `ToolEntry` registrations to it.

### Architecture Dependencies
- **ARC-001**: GAP-1 (`NodeKindBash`, `HostAdapter`, `BashConfig`, `ShellResultPayload`) — existing, no changes required.
- **ARC-002**: GAP-2 (`SeedHostSnapshot`, `PeekHostSnapshot`, `HostCapabilitySnapshot`) — existing, no changes required.
- **ARC-003**: GAP-3 (`tools.Registry`, `InjectIntoCPN`) — existing. `InjectIntoCPN` populates `Executor` and `ToolMeta` on topology transitions from registry entries. GAP-11 adds entries; the injection mechanism is unchanged.
- **ARC-004**: `fireLLM` tool-call loop — existing. GAP-11 adds: (a) host context preamble injection before `Complete/CompleteStream`, (b) no changes to the dispatch logic itself.

## 9. Examples & Edge Cases

### Example 1 — LLM calls bash_exec

LLM tool call (from OpenRouter response):
```json
{"id": "call_abc", "name": "bash_exec", "arguments": "{\"command\":\"hostname\",\"args\":[]}"}
```

`fireLLM` dispatches to `c.Transitions["bash_exec"].Executor` with:
```go
Token{Color: cpn.ColorJSON, Payload: "{\"command\":\"hostname\",\"args\":[]}"}
```

Executor result:
```go
Token{Color: cpn.ColorShellResult, Payload: cpn.ShellResultPayload{ExitCode: 0, Stdout: "caa45fd489f8\n", Stderr: "", DurationMs: 3}}
```

`fireLLM` sends tool result to LLM:
```json
{"role": "tool", "tool_use_id": "call_abc", "content": "{ExitCode:0 Stdout:caa45fd489f8\\n ...}"}
```

### Example 2 — Gate denies command

```go
// hostGateImpl.Check returns error for "rm" command
bash_exec({"command": "rm", "args": ["-rf", "/"]})
// → executor returns error "gate: operation denied: command 'rm' requires approval"
// → LLM receives tool error, may try alternative
```

### Example 3 — findSourcePlace with p-host-capabilities

Before fix (non-deterministic):
```
c.Places = {"p-input": ..., "p-host-capabilities": ..., "p-classified": ..., ...}
// outputRefs = {p-classified, p-output, p-plan, ...} — p-input and p-host-capabilities are NOT in outputRefs
// Go map iteration: may return p-host-capabilities first
// source.Deposit(ColorString, SpaceSurface token) → ErrSpaceViolation → HTTP 500
```

After fix:
```go
source := session.Root.Places["p-input"]  // always p-input, deterministic
// source.Deposit(ColorString, SpaceSurface token) → success → HTTP 202
```

### Example 4 — Host context preamble injected into t-direct

```
SYSTEM CONTEXT — HOST ENVIRONMENT (read-only facts, do not expose raw to user unless asked):
- OS: linux 6.17.9-76061709-generic (amd64)
- Hostname: caa45fd489f8
- User: app (uid=1000 home=/home/app)
- CPU: 8 cores | RAM: 15876 MB
- Present binaries: bash:/bin/bash, python3:/usr/bin/python3, git:/usr/bin/git, curl:/usr/bin/curl

YOU ARE brae.
- Your name is "brae" ...
- You run on a Linux host. You have tools available: bash_exec, file_read, file_write ...
```

### Edge Case — Empty snapshot after Reset

After `session.Root.Reset()`, `p-host-capabilities` exists in `c.Places` but is empty (tokens cleared). `PeekHostSnapshot(c)` returns `(nil, false)`. `buildHostContextPreamble` returns `""`. `t-direct.SystemPrompt` is used unmodified. The LLM still declares capabilities via `braeIdentity` but has no specific host facts. Acceptable; the session service re-seeds `p-host-capabilities` via `SeedHostSnapshot` only at session creation, not at each Reset. If host context is needed after Reset, the spec for a future version may add `p-host-capabilities` to the session CPN's `SeedFunc`.

### Edge Case — Type assertion failure in buildHostContextPreamble

If `PeekHostSnapshot(c)` returns a non-nil value that is not `persist.HostCapabilitySnapshot` (e.g., a mock or a future snapshot type), the function returns `""`. No panic, no log. This is correct: silent degradation, not crash.

## 10. Validation Criteria

1. `go build ./...` compiles with zero errors after all changes.
2. `go test -race ./...` passes with zero test failures and zero data race reports.
3. `curl -X POST .../sessions/{id}/messages -d '{"content":"hello"}'` returns HTTP 202 on first message, regardless of whether `p-host-capabilities` is in the CPN.
4. `curl -X POST .../sessions/{id}/messages -d '{"content":"what is your hostname?"}'` produces an SSE stream containing the container hostname in the assistant response.
5. `docker compose exec backend cat /home/app/.local/brae/test.txt` returns the content written by a `file_write` tool call from brae.
6. A message "can you execute shell commands?" produces a response that does not contain the phrases "cannot execute", "no terminal", or "privacy".
7. `cpn.Validate(places, transitions)` returns nil for the CPN produced by `unifiedTopologyFactory`.
8. `t-direct.LLMTools` and `t-execute.LLMTools` both contain `["bash_exec", "file_read", "file_write"]`.
9. Frontend renders a tool badge with `> bash_exec` and a duration when `tool_executed` SSE event arrives.

## 11. Related Specifications / Further Reading

- `.docs/specs/agentic-cpn-v1.3.md` — CPN persistence layer spec
- GAP-1 spec (host adapter and NodeKindBash) — `.docs/specs/blocks/` (if present)
- GAP-2 spec (host discovery) — `.docs/specs/blocks/`
- GAP-3 spec (dynamic tool registry) — `.docs/specs/blocks/`
- GAP-6 spec (HostGate policy engine) — `.docs/specs/blocks/`
- `cpn/fire_bash.go` — NodeKindBash execution implementation
- `cpn/host_snapshot.go` — PeekHostSnapshot / SeedHostSnapshot
- `cpn/tools/registry.go` — ToolEntry, ToolSchema, InjectIntoCPN
- `cpn/fire_llm.go` — LLMTools dispatch, handleToolCalls, buildToolSchema
- `cmd/server/topologies.go` — unifiedTopologyFactory
- `infra/host/os_adapter.go` — OSHostAdapter (Exec, ReadFile, WriteFile)
