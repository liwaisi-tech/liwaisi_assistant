---
title: HostAdapter Port + NodeKindBash Transition — Foundation for OS-Level Execution
version: 1.0
date_created: 2026-04-17
last_updated: 2026-04-17
owner: liwaisi
tags: [architecture, cpn, host, bash, pty, go-assistant, brae, gap-1]
---

# Introduction

The CPN agentic engine at `back/go-assistant/cpn/` supports six `NodeKind` values (`tool`, `llm`, `validate`, `subnet`, `observer`, `hitl`) but has **zero ability to execute OS commands**. For `brae` to become a self-growing agentic operating system — discovering its host, enumerating binaries, compiling new tools, installing to `$HOME/.local/bin` — the engine needs a foundational execution primitive that is both hexagonally decoupled (domain must not import `os/exec`) and observable (long-running processes must stream progress as tokens the net can react to).

This specification adds: (a) a new hexagonal port `HostAdapter` to the `cpn` domain, parallel in shape to the existing `LLMClient` port; (b) an OS-backed adapter in `infra/host/` using `os/exec` and `github.com/creack/pty`; (c) a new `NodeKindBash` transition type with full retry / circuit-breaker / error-place semantics; (d) a persistent bash session manager addressed by logical IDs; (e) four new `ColorSet` values to type-safely move shell-domain tokens; (f) a non-blocking event bridge that emits PTY stdout lines as `Event` values consumable by `NodeKindObserver` transitions.

Policy enforcement is deliberately stubbed (GAP-6). Host discovery (GAP-2), tool forging (GAP-5), dynamic registration (GAP-3), and CPN synthesis (GAP-4) all depend on this spec.

## 1. Purpose & Scope

**Purpose.** Introduce a minimal, composable primitive that lets any CPN execute commands on the host — both one-shot (`Exec`) and long-lived (`SpawnPTY`) — through a sandboxable, observable, hexagonal port.

**In scope.**
- New domain port `HostAdapter` in `back/go-assistant/cpn/host.go`.
- New adapter `OSHostAdapter` in `back/go-assistant/infra/host/os_adapter.go` using stdlib `os/exec` and `github.com/creack/pty`.
- New `NodeKindBash` variant of `cpn.NodeKind` with its own config struct, dispatch function, and validate/retry/circuit-breaker wiring.
- New `ColorSet` values: `ColorShellCmd`, `ColorShellChunk`, `ColorShellResult`, `ColorHostFact`, `ColorProcess`.
- `BashSessionManager` (ports + in-memory impl) keyed by logical session ID, supporting `Open`, `List`, `Write`, `Kill`, `Close`.
- `HostGate` interface in the domain with an allow-all stub implementation in infra (real policy = GAP-6).
- Topology validator extensions: places typed with the new colors must pass `Validate()`; `NodeKindBash` transitions with missing `BashConfig` must fail validation.
- Event bridge: PTY stdout lines → `Event{Type: EventProcessStdout}` emitted on the CPN's `EventEmitter` channel (non-blocking drop on full buffer).
- Wire `OSHostAdapter` injection at server bootstrap (`cmd/server/main.go`) and pass into CPN constructor.
- Unit tests (domain, mocked `HostAdapter`) and integration tests (real OS, Linux-only CI).

**Out of scope.**
- Sandbox profile enforcement (readonly/fsjail/network-off runtime) — GAP-6.
- Host discovery topology and `HostCapabilityRepository` — GAP-2.
- Compilation pipeline (`tool-forge-cpn`) — GAP-5.
- Tool registration endpoint / `NodeKindRegisterTool` — GAP-3.
- CPN synthesis / `NodeKindSynthesize` — GAP-4.
- Windows / macOS support — Linux only for v1.

**Audience.** `golang-pro` implementing the Go backend; reviewers verifying hexagonal boundaries.

## 2. Definitions

- **CPN**: Coloured Petri Net — the execution model used by `go-assistant`.
- **NodeKind**: The kind tag on a `Transition` (`tool`, `llm`, `validate`, `subnet`, `observer`, `hitl`, and now `bash`).
- **ColorSet**: The typed color of tokens and places (`ColorString`, `ColorJSON`, …).
- **Port (hexagonal)**: A Go interface declared in the `cpn` domain package, implemented by an adapter in `infra/` or `store/`.
- **PTY**: Pseudo-terminal — a pair of character devices providing a controlling terminal interface for long-lived interactive processes.
- **HostAdapter**: The new port abstracting all OS interactions — process exec, PTY spawn, filesystem read/write, stat.
- **HostGate**: The new port that gates every `HostAdapter` operation. v1 is allow-all; v2 (GAP-6) injects policy.
- **BashSessionManager**: The subsystem managing long-lived PTY sessions, keyed by logical ID.
- **BashConfig**: The transition-specific config attached to a `NodeKindBash` transition.
- **SandboxProfile**: An enum declared now (`none`, `readonly`, `fsjail`, `network-off`) whose runtime effect is implemented in GAP-6.

## 3. Requirements, Constraints & Guidelines

### Domain port

- **REQ-001**: A Go interface `HostAdapter` MUST be defined in `back/go-assistant/cpn/host.go`. It MUST declare these methods:
  - `Exec(ctx context.Context, req ExecRequest) (ExecResult, error)`
  - `SpawnPTY(ctx context.Context, req PTYRequest) (PTYHandle, error)`
  - `KillPID(ctx context.Context, pid int, signal Signal) error`
  - `ReadFile(ctx context.Context, path string) ([]byte, error)`
  - `WriteFile(ctx context.Context, path string, data []byte, mode fs.FileMode) error`
  - `Stat(ctx context.Context, path string) (FileInfo, error)`
- **REQ-002**: The `cpn` package MUST NOT import `os/exec`, `os`, `io/fs`, `syscall`, or any OS-only stdlib beyond types (`time`, `context`, `errors`). All OS work lives behind the port.
- **REQ-003**: All `HostAdapter` method calls MUST be preceded by a call to `HostGate.Check(ctx, op)` where `op` describes the operation. On `ErrGateDenied`, the caller MUST route the error to the transition's `ErrorPlace` (if set) or fail the CPN.

### Bash transition

- **REQ-010**: A new `NodeKind` value `NodeKindBash = "bash"` MUST be added to `cpn/transition.go`.
- **REQ-011**: A new struct `BashConfig` MUST be added with fields: `Command string`, `Args []string`, `Stdin []byte`, `Env []string` (KEY=VALUE), `Cwd string`, `Timeout time.Duration`, `SandboxProfile SandboxProfile`, `Streaming bool` (default `false`), `SessionID string` (empty = one-shot).
- **REQ-012**: When `SessionID == ""`, the transition MUST call `HostAdapter.Exec`. When `SessionID != ""`, it MUST call `BashSessionManager.Write(SessionID, Stdin)` and read chunks until the session returns `READY` or `ERROR` on its status channel.
- **REQ-013**: `NodeKindBash` transitions MUST support `RetryPolicy`, `CircuitBreaker`, `ErrorPlace`, `Guard` exactly like `NodeKindTool`.
- **REQ-014**: When `Streaming == true`, each stdout/stderr line MUST be deposited as a token of color `ColorShellChunk` on every output place, and an `Event{Type: EventProcessStdout}` MUST be emitted on the CPN's `EventEmitter` (non-blocking: if the channel buffer is full, the event MUST be dropped, never block).
- **REQ-015**: On process exit, the transition MUST deposit one terminal token of color `ColorShellResult` on each output place, payload `{exit_code int, stdout string, stderr string, duration_ms int}`.
- **REQ-016**: On non-zero exit code, the transition MUST treat it as an error: route to `ErrorPlace` if set; otherwise fail the CPN. Exception: when `BashConfig.AllowNonZeroExit == true`, the result is deposited normally.

### Color set and space rules

- **REQ-020**: `ColorSet` MUST be extended with: `ColorShellCmd`, `ColorShellChunk`, `ColorShellResult`, `ColorHostFact`, `ColorProcess`.
- **REQ-021**: `Place.Deposit` color validation MUST accept the new colors.
- **REQ-022**: The default space for `ColorShellCmd`, `ColorShellChunk`, `ColorShellResult`, `ColorHostFact`, `ColorProcess` is `SpaceComputation`. A place may still declare `SpaceObservation` for audit streams.
- **REQ-023**: A surface token MUST NOT flow directly into a `NodeKindBash` transition without going through a computation-space place first (preserves §5 of `motor_agentico_cpn.md`).

### Session manager

- **REQ-030**: `BashSessionManager` MUST be defined as an interface in `cpn/host.go` with methods: `Open(ctx, req PTYRequest) (string, error)` returns session ID; `Write(ctx, id string, data []byte) error`; `List(ctx) []SessionInfo`; `Kill(ctx, id string) error`; `Close(ctx, id string) error`.
- **REQ-031**: The in-memory implementation MUST live in `infra/host/session_manager.go`. It MUST survive multiple reads/writes from different transitions in the same CPN, and MUST clean up PTY file descriptors on `Kill`/`Close` or context cancellation.
- **REQ-032**: Session IDs MUST be UUIDv7 (time-ordered). Collisions MUST be surfaced as `ErrSessionExists`.

### Hexagonal wiring

- **REQ-040**: `cmd/server/main.go` MUST build `OSHostAdapter`, `InMemoryBashSessionManager`, `AllowAllHostGate`, and inject them via a new `cpn.HostRuntime` struct on each created CPN.
- **REQ-041**: A new field `HostRuntime *HostRuntime` MUST be added to `cpn.CPN`. `HostRuntime` aggregates `HostAdapter`, `BashSessionManager`, `HostGate`.
- **REQ-042**: Existing CPNs that do not use bash MUST continue to work with `HostRuntime == nil`. The executor MUST return a descriptive error only when a `NodeKindBash` transition fires without a `HostRuntime`.

### Validation and errors

- **CON-001**: `Validate()` MUST fail fast if a `NodeKindBash` transition has `BashConfig == nil`, empty `Command`, or references a non-existent `ErrorPlace`.
- **CON-002**: `Validate()` MUST fail fast if any output place of a `NodeKindBash` transition has color outside `{ColorShellChunk, ColorShellResult, ColorJSON, ColorError, ColorHostFact}`.
- **SEC-001**: Even in v1 (stub gate), `ExecRequest.Command` MUST reject path traversal (`..`) and a configurable deny-list (default: `rm -rf /`, `:(){ :|:& };:`). This is a belt-and-braces baseline until GAP-6 lands.
- **SEC-002**: All `WriteFile` calls MUST reject paths outside `$HOME/.local/brae/` unless explicitly authorised via `HostGate`. v1 enforces this as an unconditional check; v2 delegates to gate policy.

### Guidelines

- **GUD-001**: Prefer `Exec` for deterministic, short (<30 s) commands. Prefer `SpawnPTY` for anything interactive or long-running.
- **GUD-002**: Stream output for any command that can exceed 1 s, to keep the user loop responsive.
- **GUD-003**: Never log raw stdin; it can contain secrets. Log `sha256(stdin) + len`.
- **PAT-001**: Error kinds: `ErrGateDenied`, `ErrCommandNotFound`, `ErrTimeout`, `ErrNonZeroExit`, `ErrPathDenied`, `ErrSessionNotFound`. All wrap a domain `HostError` with a stable `Code` field.

## 4. Interfaces & Data Contracts

### Go interface — `cpn/host.go`

```go
package cpn

import (
    "context"
    "io/fs"
    "time"
)

type Signal string

const (
    SignalTerm Signal = "SIGTERM"
    SignalKill Signal = "SIGKILL"
    SignalInt  Signal = "SIGINT"
)

type SandboxProfile string

const (
    SandboxNone       SandboxProfile = "none"
    SandboxReadonly   SandboxProfile = "readonly"
    SandboxFSJail     SandboxProfile = "fsjail"
    SandboxNetworkOff SandboxProfile = "network-off"
)

type ExecRequest struct {
    Command        string
    Args           []string
    Stdin          []byte
    Env            []string
    Cwd            string
    Timeout        time.Duration
    Sandbox        SandboxProfile
    AllowNonZero   bool
}

type ExecResult struct {
    ExitCode   int
    Stdout     []byte
    Stderr     []byte
    DurationMs int64
    Truncated  bool
}

type PTYRequest struct {
    Command string
    Args    []string
    Env     []string
    Cwd     string
    Rows    uint16
    Cols    uint16
    Sandbox SandboxProfile
}

type PTYHandle struct {
    SessionID string
    PID       int
    Stdout    <-chan []byte
    Stderr    <-chan []byte
    Status    <-chan PTYStatus
}

type PTYStatus struct {
    Kind     string // "ready", "exited", "error"
    ExitCode int
    Err      error
}

type FileInfo struct {
    Path    string
    Size    int64
    Mode    fs.FileMode
    ModTime time.Time
    IsDir   bool
}

type HostAdapter interface {
    Exec(ctx context.Context, req ExecRequest) (ExecResult, error)
    SpawnPTY(ctx context.Context, req PTYRequest) (PTYHandle, error)
    KillPID(ctx context.Context, pid int, sig Signal) error
    ReadFile(ctx context.Context, path string) ([]byte, error)
    WriteFile(ctx context.Context, path string, data []byte, mode fs.FileMode) error
    Stat(ctx context.Context, path string) (FileInfo, error)
}

type GateOp struct {
    Kind    string // "exec", "spawn_pty", "read_file", "write_file", "kill"
    Command string // populated for exec/spawn_pty
    Path    string // populated for read_file/write_file
    Sandbox SandboxProfile
}

type HostGate interface {
    Check(ctx context.Context, op GateOp) error
}

type SessionInfo struct {
    ID        string
    Command   string
    StartedAt time.Time
    PID       int
    Alive     bool
}

type BashSessionManager interface {
    Open(ctx context.Context, req PTYRequest) (string, error)
    Write(ctx context.Context, id string, data []byte) error
    List(ctx context.Context) []SessionInfo
    Kill(ctx context.Context, id string) error
    Close(ctx context.Context, id string) error
}

type HostRuntime struct {
    Adapter  HostAdapter
    Gate     HostGate
    Sessions BashSessionManager
}
```

### `BashConfig`

```go
type BashConfig struct {
    Command          string
    Args             []string
    Stdin            []byte
    Env              []string
    Cwd              string
    Timeout          time.Duration
    SandboxProfile   SandboxProfile
    Streaming        bool
    AllowNonZeroExit bool
    SessionID        string
}
```

### Color set extension

```go
const (
    ColorShellCmd    ColorSet = "shell_cmd"
    ColorShellChunk  ColorSet = "shell_chunk"
    ColorShellResult ColorSet = "shell_result"
    ColorHostFact    ColorSet = "host_fact"
    ColorProcess     ColorSet = "process"
)
```

### Event

```go
type EventKind string

const (
    EventProcessStdout EventKind = "process.stdout"
    EventProcessExit   EventKind = "process.exit"
)

type Event struct {
    Kind      EventKind
    PID       int
    SessionID string
    Payload   []byte
    Timestamp time.Time
}
```

### HTTP (no new endpoints in this spec)

None. Bash is an internal primitive; surfaced to the user only via higher-level CPNs.

## 5. Acceptance Criteria

- **AC-001**: Given a CPN with a `NodeKindBash` transition configured `Command="echo"`, `Args=["hello"]`, when the input place is marked, then the transition fires and a `ColorShellResult` token with `exit_code=0`, `stdout="hello\n"` is deposited on the output place.
- **AC-002**: Given a `NodeKindBash` transition with `Timeout=100ms` running `sleep 10`, when it fires, then the transition errors with `ErrTimeout` and the token is routed to `ErrorPlace`.
- **AC-003**: Given a `NodeKindBash` transition with `Streaming=true` running `for i in 1 2 3; do echo $i; sleep 0.1; done`, when it fires, then exactly three `ColorShellChunk` tokens are deposited on the output place before the final `ColorShellResult`, and three `EventProcessStdout` events are emitted.
- **AC-004**: Given a `BashSessionManager` session opened with command `bash`, when two different transitions each `Write` a command, then both writes execute in the same shell session (verifiable via `pwd` after `cd /tmp`).
- **AC-005**: Given a `NodeKindBash` transition with missing `BashConfig`, when `cpn.Validate()` runs, then it returns `ValidationError` with code `bash.missing_config`.
- **AC-006**: Given a CPN with `HostRuntime == nil`, when a `NodeKindBash` transition attempts to fire, then the executor returns a descriptive error and the CPN transitions to `Failed`.
- **AC-007**: Given `WriteFile` called with path `/etc/passwd`, when the default gate denies writes outside `$HOME/.local/brae/`, then the call returns `ErrPathDenied`.
- **AC-008**: Given a CPN with `HostGate` returning `ErrGateDenied` for a specific command, when the bash transition fires, then the token is routed to `ErrorPlace` with `Code="gate_denied"`.
- **AC-009**: Given a `NodeKindBash` transition with `AllowNonZeroExit=true` running `false`, when it fires, then a `ColorShellResult` with `exit_code=1` is deposited on the output place (no error routing).
- **AC-010**: No file under `back/go-assistant/cpn/` imports `os/exec`, `os`, `syscall`, or `github.com/creack/pty` (enforced by a static check in CI).

## 6. Test Automation Strategy

- **Test Levels**:
  - **Unit (domain)**: `cpn/transition_test.go` — test `NodeKindBash` dispatch with a mock `HostAdapter`. Use Go standard `testing`.
  - **Unit (infra)**: `infra/host/os_adapter_test.go` — test `OSHostAdapter` against real `echo`, `sleep`, `cat`, `false`.
  - **Integration**: `infra/host/session_manager_integration_test.go` — open a bash PTY, run `cd /tmp && pwd`, verify output, close session.
  - **CPN-level**: `cpn/cpn_bash_e2e_test.go` — full topology: input → bash(echo) → validate → output.
- **Frameworks**: Go `testing`, `testify/require` (already a dep), `goleak` to catch PTY goroutine leaks.
- **Test data management**: Temp dirs under `t.TempDir()`; no shared filesystem state.
- **CI integration**: Tests run on `ubuntu-latest` only; Darwin/Windows skipped with build tag `//go:build linux`.
- **Coverage requirements**: ≥ 85% on `cpn/host.go` dispatch and `infra/host/` package.
- **Performance**: One benchmark `BenchmarkBashEcho` must complete 1000 echo-hello cycles under 3 s on CI.

## 7. Rationale & Context

**Why a port, not a package?** The existing `LLMClient` port has proven its value: swapping OpenRouter for a local model or for Anthropic direct is a single-line infra change. Every subsequent GAP (discovery, forge, tool registration) must be able to substitute the host in tests. A domain-owned interface keeps that door open.

**Why `ColorShellChunk` in addition to `ColorString`?** Type safety prevents a raw user message from being routed into a bash executor and vice versa. The new colors make the place contracts self-documenting, and they let `Validate()` catch topology bugs at construction time instead of at 3 AM in production.

**Why bundle PTY sessions into the engine?** Bash is stateful. A user asking "cd into that repo and now what changed?" cannot be answered by independent `Exec` calls — the working directory would reset. The `BashSessionManager` preserves shell state across transitions, turning the agent's interaction with the host into a real conversation with a shell, not a stateless function call.

**Why stub the gate now?** Executing shell on a user's machine without policy is the single most dangerous thing `brae` will do. The interface must exist from day one even if the implementation is allow-all, so that GAP-6 is a pure substitution, not a refactor. `SEC-001` and `SEC-002` provide a baseline deny-list so the v1 allow-all is not fully open-barn.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: `github.com/creack/pty` — PTY spawning for Linux. MIT-licensed, single-file, no transitive deps.

### Infrastructure Dependencies
- **INF-001**: Linux kernel ≥ 5.4 (for `pidfd_open` fallback). Darwin support deferred.

### Data Dependencies
- None. `HostAdapter` is stateless per call; session state lives in `BashSessionManager` in memory.

### Technology Platform Dependencies
- **PLT-001**: Go ≥ 1.22 (already the repo baseline).

### Compliance Dependencies
- **COM-001**: Because `WriteFile` can land on user disk, artefact provenance (GAP-10) tracks every write. v1 enforces only the path jail (SEC-002).

## 9. Examples & Edge Cases

### Simple `echo` transition

```go
t := &cpn.Transition{
    ID:           "t-echo",
    Kind:         cpn.NodeKindBash,
    InputPlaces:  []string{"p-cmd"},
    OutputPlaces: []string{"p-result"},
    ErrorPlace:   "p-errors",
    BashConfig: &cpn.BashConfig{
        Command: "echo",
        Args:    []string{"hello"},
        Timeout: 2 * time.Second,
    },
}
```

### Streaming `uname -a` into an observer

```go
t := &cpn.Transition{
    ID:           "t-uname",
    Kind:         cpn.NodeKindBash,
    InputPlaces:  []string{"p-request"},
    OutputPlaces: []string{"p-uname-chunks"},
    BashConfig: &cpn.BashConfig{
        Command:   "uname",
        Args:      []string{"-a"},
        Streaming: true,
        Timeout:   1 * time.Second,
    },
}
```

### Persistent PTY session shared across two transitions

```go
// Transition A opens the session, deposits session ID as ColorProcess token.
// Transition B consumes the session ID and issues `cd /tmp && pwd`.
// Transition C consumes the session ID and issues `ls`.
// All three share the same bash shell; working-directory state persists.
```

### Edge case — command not found

```go
// BashConfig.Command = "nonexistent"
// HostAdapter.Exec returns ErrCommandNotFound.
// Transition routes error token to ErrorPlace "p-errors".
// CPN does not fail; downstream recovery transition reads from p-errors.
```

### Edge case — huge stdout

```go
// yes | head -c 10000000
// ExecResult.Truncated = true, Stdout capped at 1 MiB by default.
// Full output available only in streaming mode.
```

## 10. Validation Criteria

- Static check: no OS-specific imports in `cpn/` (CI grep).
- Static check: every `NodeKindBash` test covers success, timeout, non-zero exit, streaming, and gate-denied.
- Static check: every `HostAdapter` method has at least one test that does not use mocks.
- Runtime check: `goleak.VerifyNone(t)` at the end of every PTY test — no leaked goroutines.
- Runtime check: `pprof` heap profile after 1000 bash transitions shows no retained `*PTYHandle`.

## 11. Related Specifications / Further Reading

- [motor_agentico_cpn.md](../.docs/motor_agentico_cpn.md) §5 (space isolation), §7.1 (tool transition), §13 (retry / circuit breaker).
- [spec-architecture-host-discovery-capability-registry.md](./spec-architecture-host-discovery-capability-registry.md) — GAP-2 (consumer).
- [spec-architecture-host-gate-security-policy.md](./spec-architecture-host-gate-security-policy.md) — GAP-6 (policy substitute for v1 stub).
- [spec-architecture-tool-forge-cpn.md](./spec-architecture-tool-forge-cpn.md) — GAP-5 (primary consumer).
- `github.com/creack/pty` — <https://github.com/creack/pty>
- `os/exec` stdlib — <https://pkg.go.dev/os/exec>
