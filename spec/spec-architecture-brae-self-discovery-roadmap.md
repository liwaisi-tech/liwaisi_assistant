# brae Self-Discovery Roadmap — Follow-up Tasks after PR #95

**Status:** draft
**Owner:** @liwaisi
**Related:** PR #95; `spec-architecture-brae-awakening-self-discovery.md`; `spec-architecture-host-discovery-capability-registry.md`; `spec-architecture-brae-awakening-probe-fanout.md`
**Created:** 2026-04-21

## Context

During review of PR #95 (`feat/model-registry-foundation`) the maintainer
rejected the compile-time `hostProbeRegistry` allow-list (previously defined
in `cmd/server/topologies_host_discovery.go`) and the placement of CPN
topology factories inside `cmd/server/`. brae is Linux-first and is meant to
wake up on arbitrary hosts, introspect the environment, and synthesize its
toolbox on the fly — the compile-time list actively prevented that.

As part of PR #95 we landed the minimum change to retire the most visible
symptom:

- **Landed (PR #95):** `hostProbeRegistry` replaced with
  `DefaultHostProbeResolver`, a runtime `$PATH` scanner with injectable
  override (`HostDiscoveryDeps.ProbeResolver`) and deterministic sort +
  cap. Tests inject a fixed resolver for reproducibility.
  (`cmd/server/topologies_host_discovery.go` + tests.)

The rest of the architectural work is tracked below. Each section is sized
so it can land as a self-contained PR without blowing up review.

## Guiding principles

1. **Core must not depend on a specific driving adapter.** The same CPN core
   that powers the HTTP server must power a future `cmd/brae` CLI and any
   other driver (A2A, MCP, etc.) without duplication.
2. **No hidden compile-time policy.** Every "list of X" that constrains
   discovery lives behind a port with a runtime default and a persistence
   backend.
3. **Safe by default, verbose on request.** First-run HITL + SHA256 pinning
   for anything the agent synthesizes from host binaries.
4. **Deterministic outputs.** Probe fan-out, tool synthesis, and capability
   derivation must produce the same serialized output for the same inputs.

---

## T2 — Move topology factories out of `cmd/server/`

### Objective
Decouple CPN topology construction from the HTTP composition root so the
same factories can be invoked by a CLI driving adapter.

### Gap
`cmd/server/` currently contains ~3 k LOC of topology code (`topologies.go`,
`topologies_host_discovery.go`, `topologies_tool_forge.go`,
`topologies_model_registry.go`, `system_tools.go`, `awakening_composer.go`,
plus tests). These live in `package main` and can only be used by the HTTP
binary.

### Design outline
1. New package `back/go-assistant/internal/app/topologies` (sister to
   `internal/app/session_service.go`).
2. Mechanical move of the files above; `package main` → `package topologies`.
3. Export every symbol `cmd/server/main.go`, `bootstrap_discovery.go`, and
   `reassess.go` reference (capitalize identifiers; add doc comments).
4. `cmd/server/main.go` imports `…/internal/app/topologies` and calls
   exported constructors. Globals currently defined in `cmd/server/`
   (`globalHostCapabilityRepo`, admin-emails env sentinel, etc.) either
   move with the topology files or are replaced by explicit dependency
   injection through the factory structs.
5. Stay in `cmd/server/` (they're boot-time composition, not CPN):
   `main.go`, `main_test.go`, `bootstrap_discovery.go`,
   `capability_derive.go`, `reassess*.go`, `os_seam.go`, `topology_test.go`.

### Acceptance criteria
- `go build ./...` and `go test ./...` green.
- A future `cmd/brae/main.go` can call
  `topologies.HostDiscoveryTopologyFactory(...)` without importing
  anything from `cmd/server/`.
- No symbol that used to be lowercase-unexported in `main` becomes
  accidentally part of a public API surface that external users would rely
  on. (The `internal/` directory guarantees this.)

### Rough size
~15 files, ~3 k LOC moved + rename + export; test updates; no behavior
change.

---

## T3 — Subprocess sandbox hardening

### Objective
Enforce per-command timeout, kill-on-exceed, and optional sandbox wrapping
(`bwrap` / `firejail`) for every bash transition.

### Gap
`cpn/fire_bash.go` honors `BashConfig.Timeout` weakly — there is no explicit
process-group kill on exceed and `infra/host/os_adapter.go` does not wrap
the child in a sandbox by default. Discovered binaries that misbehave on
`--help` (interactive prompts, infinite loops) can orphan processes.

### Design outline
1. New `infra/host/subprocess/runner.go`:
   - `type Runner struct { TimeoutPolicy, SandboxPolicy, GateEvents }`
   - `Run(ctx, ExecRequest) (ExecResult, error)` with:
     - `exec.CommandContext` + `SysProcAttr{Setpgid: true}`
     - Timeout → `syscall.Kill(-pgid, SIGTERM)` then SIGKILL after 500 ms.
     - Sandbox: if the host has `bwrap` or `firejail` AND the caller
       requested sandboxing, wrap the command.
   - Emits gate events on start / timeout / kill for audit.
2. `fire_bash.go` delegates to the runner; drops its ad-hoc timeout.
3. `HostAdapter` interface unchanged (adapter delegates to runner).

### Acceptance criteria
- Integration test: a command that sleeps 5 s with 1 s timeout is killed
  within 2 s and no child process remains (ps-grep in teardown).
- `bwrap`-wrapped command has restricted view of `/` (smoke test when
  `bwrap` is present; skipped otherwise).

---

## T4 — `--help` / man parser

### Objective
Extract structured usage information from a binary's help/man output so the
tool-forge can synthesize a JSON-Schema input contract.

### Gap
No parser exists. `awakens/toolforge` currently depends on LLM prompting to
turn raw help text into a manifest, which is non-deterministic and can't be
cached reliably.

### Design outline
1. New package `cpn/probe/help_parser`:
   - Tokenize by lines; detect `Usage:` / `USAGE` / `SYNOPSIS` blocks.
   - Flag extraction: recognize `-x`, `--long-flag`, `--flag=VALUE`,
     `-x VALUE`, bracket-optional `[--x]`, ellipsis `...`.
   - Positional arguments from the Usage line.
   - Examples section (best effort).
2. Output struct `ParsedHelp{ Usage []string; Flags []Flag; Positional
   []Arg; Examples []string; Raw string }`.
3. Golden fixtures in `cpn/probe/help_parser/testdata/` for ≥ 30 binaries
   (git, jq, ssh, curl, docker, tar, go, node, python3, …).

### Acceptance criteria
- ≥ 85 % flag-recall on the fixture set.
- Parser is pure (no I/O), deterministic, ≤ 5 ms on 200-line help text.

---

## T5 — Tool synthesis sub-net (`NodeKindSynthesizeTool`)

### Objective
For every binary discovered by host discovery, run `--help` (and fall back
to `man`) through the parser from T4, build a `ToolManifest`, and register
it through `NodeKindRegisterTool`. This turns the discovery pass from "I
know you have git" into "I can invoke `git` as a registered brae tool."

### Gap
`NodeKindRegisterTool` already exists and is wired through
`fire_register_tool.go` / `ToolRegistry.RegisterManifest`. Nothing currently
feeds it from host discovery output. `awakens/toolforge` is partially
wired but does not see the output of the `host-discovery` flow.

### Design outline
1. Add `NodeKindSynthesizeTool` (or reuse `NodeKindSynthesize` with a new
   handler variant) that consumes `(BinaryProbe, HelpText, Version)` and
   emits a `ToolManifest`.
2. Extend the host-discovery topology: after `t-parse-binaries`, fan out
   one `SynthesizeTool` transition per present binary (dynamically-built
   sub-net, following the `awakens/fanout` pattern). Each reads that
   binary's raw help via a new `t-help-<bin>` bash transition.
3. Downstream `NodeKindRegisterTool` writes to the existing `ToolRegistry`.
4. Gate registration behind T10 (first-run HITL).

### Acceptance criteria
- After a wakeup on a host with `jq` on `$PATH`, `jq` appears as a
  registered tool whose JSON Schema reflects the flags advertised by
  `jq --help`.
- Re-running wakeup does not create duplicate entries (registry keys by
  `namespace/name/version`).

---

## T6 — Full parallel wakeup umbrella

### Objective
Expand wakeup from binary-only discovery to a domain-parallel umbrella:
identity, filesystem layout, permissions, network interfaces, DB reach,
editors, processes, binaries.

### Gap
`awakens/fanout/composer.go` already builds a flat fanout sub-net. The
current `mandatoryInfoProbes()` covers OS / shell / user / identity /
hostname. There is no fs / net / db / editors sub-net.

### Design outline
1. New sub-net factories under `internal/app/topologies/wakeup/`:
   - `fs_net.go` — walks `$XDG_*`, mounts, writable scratch.
   - `net_net.go` — `ip -j addr`, `/etc/resolv.conf`, outbound probe to
     a configured STUN-like endpoint (opt-in).
   - `db_net.go` — presence probes for `postgres`, `mysql`, `sqlite3`
     sockets + binary probes.
   - `editors_net.go` — `$EDITOR`, `$VISUAL`, binaries in the common editor
     set.
   - `procs_net.go` — `ps` snapshot, filtered to user-owned processes.
2. `WakeupFactory()` composes all of these in parallel, joins into a
   single `HostCapabilitySnapshot` extended with domain-specific fields.
3. Each sub-net is independently testable via the same golden-fixture
   pattern.

### Acceptance criteria
- Single `WakeupFactory()` invocation yields a capability snapshot with
  ≥ 6 populated domain blocks on a typical Linux dev machine.
- Each domain sub-net has unit tests ≥ 80 % coverage.

---

## T7 — `cmd/brae` CLI driving adapter

### Objective
Ship a first-class CLI that drives the same core used by the HTTP server.

### Gap
Only `cmd/server` (and a minimal `cmd/cli` stub) exist. The CLI today does
not exercise CPN.

### Design outline
1. `cmd/brae/main.go` + `internal/driving/cli/` package.
2. Commands:
   - `brae wakeup` — runs `WakeupFactory()` and writes snapshot + tool
     registrations to the same backing store the HTTP server uses.
   - `brae tools list [--namespace NS]` — lists registered tools.
   - `brae ask "…"` — single-turn driven by `SessionService` with an
     stdout-stream sink instead of SSE.
   - `brae run <tool> [args]` — direct tool invocation for debugging.
3. Reuses `internal/app/topologies`, `internal/app/SessionService`,
   `infra/openrouter`, `store/postgres`. No HTTP dependency.

### Acceptance criteria
- `brae wakeup` produces the same `HostCapabilitySnapshot` as an HTTP
  bootstrap (verified by golden snapshot).
- `brae ask "hi"` returns a streamed response on stdout.

---

## T8 — Capability-based ACL

### Objective
Replace pattern-only allow/deny in `infra/host/gate/policy_gate.go` with a
capability-based model: decisions consider uid, binary identity (SHA256),
flag set, path targets.

### Gap
`policy_gate.go` matches command lines against regex/prefix rules. It
cannot express "git reading `/etc` is fine, but git writing outside
`$XDG_STATE_HOME/brae` is not."

### Design outline
1. New `infra/host/capabilities/acl.go` defining
   `Capability{BinarySHA, FlagPattern, PathGlob, Action}`.
2. Decision: `(uid, binary, flags, paths) → Allow | Prompt | Deny`.
3. Default policy file `config/acl/default.yaml` with safe baselines.
4. Audit log for every Prompt / Deny; Prompt answers persist via
   `FirstRunLedger`.

### Acceptance criteria
- Decision latency ≤ 1 ms on a 1000-rule policy.
- A `cat /etc/passwd` call is allowed; `tee /etc/passwd` is denied; both
  are audited.

---

## T9 — Bounded worker pool for wide probe fan-out

### Objective
Cap parallel subprocess spawns so a 2000-entry `$PATH` doesn't fork-bomb
the host.

### Gap
`cpn/executor.go` fires every enabled transition in parallel per batch. On
a very wide fan-out this can exceed file-descriptor limits and thrash CPU.

### Design outline
1. New executor option `MaxParallelBash int` with a default of e.g. 32.
2. Executor holds a semaphore; bash-kind transitions acquire before exec.
3. Non-bash transitions remain unlimited (they're in-process).
4. Config knob exposed via `internal/config`.

### Acceptance criteria
- A 2000-probe fan-out completes without fd exhaustion on a host with
  `ulimit -n 1024`.
- Throughput on a 100-probe fan-out within 10 % of the current
  unrestricted executor.

---

## T10 — First-run HITL for synthesized tools + SHA256 pin

### Objective
Before brae invokes a tool it synthesized from a host binary, require user
approval on first call; pin the binary's SHA256 at synthesis and require
re-approval if the hash changes.

### Gap
`FirstRunLedger` exists for topology first-runs but does not cover
synthesized tools. `ToolEntry.BinarySHA256` exists but is not verified at
call time.

### Design outline
1. Extend `FirstRunLedger` with a namespace for synthesized tools, keyed
   on `toolID + binarySHA256`.
2. `fire_tool.go` (or equivalent) checks the ledger before invocation;
   unknown key → issue HITL surface; verify SHA256 still matches before
   each call.
3. Surface card: "brae discovered `docker` at `/usr/bin/docker` (SHA
   `ab12…`). Allow it to call `docker ps`?"

### Acceptance criteria
- New synthesized tool cannot run without explicit approval.
- Replacing the binary with a different SHA forces re-approval.
- Approval persists across sessions.

---

## Suggested order

1. **T2** (unblocks CLI and clean cross-driver reuse)
2. **T3** (safety prerequisite for wide probing)
3. **T4 + T5** (the actual self-discovery feature)
4. **T6** (expand discovery domains)
5. **T7** (CLI driver — depends on T2)
6. **T8 + T10** (security hardening before shipping broadly)
7. **T9** (scale hardening — only matters once T5 ships wide fan-out)

Each can be its own PR; T5 is the centerpiece.
