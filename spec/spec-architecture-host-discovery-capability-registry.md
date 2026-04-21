---
title: Host Discovery CPN + HostCapabilityRepository — Making brae Self-Aware of Its Linux Host
version: 1.0
date_created: 2026-04-17
last_updated: 2026-04-17
owner: liwaisi
tags: [architecture, cpn, discovery, host, postgres, brae, gap-2]
---

# Introduction

Before `brae` can author tools, compile software, or decide which binaries to invoke, it must first **know what the host gives it**. Today the engine has no idea whether `gcc`, `go`, `python3`, `curl`, or `git` exist — there is no `whoami`, no `uname`, no probing of `$PATH`. The agent would hallucinate tools and fail at first contact.

This specification defines a first-boot CPN (`host-discovery-cpn`) that runs once per host to enumerate OS facts — identity, kernel, distribution, user, `$HOME`, CPU count, memory, PATH entries, and the presence/version of a curated list of binaries — and writes them to a new `HostCapabilityRepository` persisted in Postgres. Every subsequent CPN can then `Peek` those facts from a well-known place `p-host-capabilities` instead of re-running discovery.

This spec depends on GAP-1 (`HostAdapter` + `NodeKindBash`). It is a prerequisite for GAP-5 (tool-forge) and GAP-4 (CPN synthesiser).

## 1. Purpose & Scope

**Purpose.** Give `brae` structured self-knowledge of its Linux host so that downstream CPNs can make capability-aware decisions (e.g. "compile in C if `gcc` is present, else emit a Bash script fallback").

**In scope.**
- A new CPN topology `host-discovery-cpn` registered in `cmd/server/topologies_host_discovery.go` and wired into the FlowRegistry under name `"host-discovery"`.
- A new port `HostCapabilityRepository` in `cpn/persist/host_capability.go` with methods: `Save(ctx, HostCapabilitySnapshot) error`, `LatestForHost(ctx, hostID string) (HostCapabilitySnapshot, error)`, `AppendProbeResult(ctx, hostID, probe) error`.
- Postgres adapter `store/postgres/host_capability_store.go` + migration `0015_host_capability.sql`.
- In-memory adapter `cpn/persist/host_capability_memory.go` for tests.
- A new probe registry listing the binaries to check: `{bash, sh, gcc, cc, g++, clang, go, python3, node, npm, curl, wget, git, make, tar, unzip, ssh, jq, sqlite3, docker, podman, bwrap, firejail}`. Each entry: `name, detection_method (which|--version|env), parse_regex, required_for (array of higher-level capabilities)`.
- A new well-known place `p-host-capabilities` (color `ColorHostFact`, space `SpaceComputation`) always seeded at session bootstrap from the latest repo snapshot.
- CLI entry `cmd/server/main.go --bootstrap-discovery` to run host-discovery without requiring a user session (for initial deploy).
- A `HostCapabilitySnapshot` struct and the `HostCapabilityProbe` union types.
- An HTTP endpoint `GET /api/host/capabilities` returning the latest snapshot (read-only).
- A session-level hook: when a new session starts, if no snapshot exists or the snapshot is older than 24 h, spawn `host-discovery-cpn` as a sub-CPN of the session root.
- Unit + integration tests.

**Out of scope.**
- Ubuntu-vs-Fedora-vs-Arch specialisation — v1 treats all Linux as a single target, parses `/etc/os-release` generically.
- Darwin / Windows — explicitly deferred.
- Continuous re-probing — v1 is run-once-then-cache; re-runs are manual or scheduled.
- Using the facts — that belongs to consumer specs (GAP-4, GAP-5).

**Audience.** `golang-pro` for backend implementation; SQL migrations; REST endpoint added to the existing admin router.

## 2. Definitions

- **Host**: The Linux machine where `go-assistant` runs. Identified by `machine-id` (from `/etc/machine-id` or `hostnamectl`).
- **Probe**: A single capability check (e.g. "does `gcc` exist and what version").
- **Snapshot**: The result of running the full probe suite at a point in time.
- **Well-known place**: A place ID reserved by the engine and guaranteed to be present in every CPN's initial marking (v1 handles this via the session bootstrap, not the topology constructor).
- **Capability**: A named atomic fact derivable from one or more probes (e.g. "can-compile-c" is true iff `gcc` ∨ `clang`).

## 3. Requirements, Constraints & Guidelines

### CPN topology

- **REQ-001**: Topology `host-discovery-cpn` MUST exist with the following places:
  - `p-trigger` (`ColorString`, `SpaceComputation`) — input signal.
  - `p-identity` (`ColorHostFact`, `SpaceComputation`) — machine-id, hostname, user, home.
  - `p-kernel` (`ColorHostFact`, `SpaceComputation`) — uname, os-release.
  - `p-binaries` (`ColorHostFact`, `SpaceComputation`) — per-binary probe results (one token per binary).
  - `p-capabilities` (`ColorHostFact`, `SpaceComputation`) — derived atomic capabilities.
  - `p-snapshot` (`ColorHostFact`, `SpaceComputation`) — final aggregated snapshot (terminal).
- **REQ-002**: Transitions required:
  - `t-who` (`NodeKindBash`) — runs `whoami` / `id` / `hostname` / `printenv HOME`; fans out to `p-identity`.
  - `t-uname` (`NodeKindBash`) — runs `uname -a`, reads `/etc/os-release`; fans out to `p-kernel`.
  - `t-probe-<binary>` (`NodeKindBash`) — one transition per binary in the probe registry. Each fans a single token into `p-binaries`. The 22 probes MAY be authored as a single variadic builder function for concision.
  - `t-derive` (`NodeKindTool`) — consumes all tokens from `p-identity`, `p-kernel`, `p-binaries` and emits derived atomic capabilities into `p-capabilities`.
  - `t-persist` (`NodeKindTool`) — consumes `p-capabilities` → builds `HostCapabilitySnapshot` → calls `HostCapabilityRepository.Save` → deposits the snapshot in `p-snapshot`.
- **REQ-003**: The topology MUST NOT use any LLM transition. Discovery is deterministic by design (`LLM is the last resort`).
- **REQ-004**: The 22 bash probes MUST run in parallel (they share no input places) — this exercises and validates the concurrent firing of the executor.
- **REQ-005**: The topology MUST complete within 5 seconds on a warm host (all binaries present) and within 30 seconds on a cold host (several timeouts).

### Repository

- **REQ-010**: Port `HostCapabilityRepository` MUST live in `cpn/persist/host_capability.go` and MUST NOT import any storage package.
- **REQ-011**: Snapshots MUST be append-only. `Save` inserts a new row; old snapshots are retained for audit. `LatestForHost` returns the most recent.
- **REQ-012**: The Postgres schema MUST store: `id UUID PK`, `host_id TEXT` (indexed), `captured_at TIMESTAMPTZ`, `identity JSONB`, `kernel JSONB`, `binaries JSONB`, `capabilities JSONB`, `raw_probes JSONB`, `source TEXT` (‘bootstrap’ or ‘session’).
- **REQ-013**: `HostCapabilityRepository` MUST cache the latest snapshot in memory with a 60 s TTL to prevent hammering Postgres on every new session.

### Session bootstrap

- **REQ-020**: On session start, `SessionService` MUST call `HostCapabilityRepository.LatestForHost(machineID)`.
- **REQ-021**: If no snapshot exists or the snapshot is older than 24 hours, `SessionService` MUST spawn `host-discovery-cpn` as a sub-CPN at depth 1. Subsequent session CPNs MUST peek from `p-host-capabilities` (seeded from the snapshot) without waiting.
- **REQ-022**: Every root CPN's initial marking MUST include a single token in `p-host-capabilities` with the full snapshot payload. Consumers MUST use `Peek` (not `Consume`); a terminal transition MAY consume at the very end if desired.

### HTTP endpoint

- **REQ-030**: `GET /api/host/capabilities` MUST return `200 {snapshot}` or `404 {reason: "no-snapshot-yet"}`.
- **REQ-031**: The endpoint MUST require admin auth (same middleware as `/api/admin/models`).
- **REQ-032**: A companion endpoint `POST /api/host/capabilities/rediscover` MUST trigger a manual `host-discovery-cpn` run and return the new snapshot ID.

### Derived capabilities (`t-derive`)

- **REQ-040**: The following derived capabilities MUST be emitted based on probe results:
  - `can-compile-c` ← `gcc` ∨ `cc` ∨ `clang` present.
  - `can-compile-go` ← `go` present AND version ≥ 1.22.
  - `can-run-python` ← `python3` present.
  - `can-run-node` ← `node` present.
  - `can-fetch-url` ← `curl` ∨ `wget` present.
  - `can-sandbox` ← `bwrap` ∨ `firejail` present.
  - `can-version-control` ← `git` present.
  - `can-pack` ← `tar` present.
- **REQ-041**: The derivation logic MUST live in a pure Go function, fully unit-testable.

### Guidelines

- **GUD-001**: Probe timeouts MUST be short (2 s each) — a missing binary should not delay discovery.
- **GUD-002**: Redact environment variables containing `TOKEN`, `KEY`, `PASSWORD`, `SECRET` before persisting.
- **GUD-003**: `t-probe-<binary>` transitions MUST set `AllowNonZeroExit=true` so that "binary not found" is a normal result, not a CPN failure.

## 4. Interfaces & Data Contracts

### Go types

```go
type HostIdentity struct {
    MachineID string `json:"machine_id"`
    Hostname  string `json:"hostname"`
    User      string `json:"user"`
    UID       int    `json:"uid"`
    GID       int    `json:"gid"`
    Home      string `json:"home"`
    Shell     string `json:"shell"`
}

type HostKernel struct {
    OS       string            `json:"os"`       // e.g. "Linux"
    Kernel   string            `json:"kernel"`   // e.g. "6.17.9-76061709-generic"
    Arch     string            `json:"arch"`     // e.g. "x86_64"
    OSRelease map[string]string `json:"os_release"` // parsed /etc/os-release
    CPUCount int              `json:"cpu_count"`
    MemMB    int              `json:"mem_mb"`
}

type BinaryProbe struct {
    Name         string `json:"name"`
    Path         string `json:"path,omitempty"`
    Present      bool   `json:"present"`
    Version      string `json:"version,omitempty"`
    DurationMs   int64  `json:"duration_ms"`
    DetectionErr string `json:"detection_err,omitempty"`
}

type Capability struct {
    Name        string   `json:"name"`
    Satisfied   bool     `json:"satisfied"`
    DerivedFrom []string `json:"derived_from"` // which probe names contributed
}

type HostCapabilitySnapshot struct {
    ID           string          `json:"id"`
    HostID       string          `json:"host_id"`
    CapturedAt   time.Time       `json:"captured_at"`
    Source       string          `json:"source"`
    Identity     HostIdentity    `json:"identity"`
    Kernel       HostKernel      `json:"kernel"`
    Binaries     []BinaryProbe   `json:"binaries"`
    Capabilities []Capability    `json:"capabilities"`
    RawProbes    json.RawMessage `json:"raw_probes,omitempty"`
}

type HostCapabilityRepository interface {
    Save(ctx context.Context, s HostCapabilitySnapshot) error
    LatestForHost(ctx context.Context, hostID string) (HostCapabilitySnapshot, error)
    AppendProbeResult(ctx context.Context, hostID string, probe BinaryProbe) error
}
```

### Postgres migration

```sql
-- 0015_host_capability.sql
CREATE TABLE host_capability_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    host_id TEXT NOT NULL,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    source TEXT NOT NULL,
    identity JSONB NOT NULL,
    kernel JSONB NOT NULL,
    binaries JSONB NOT NULL,
    capabilities JSONB NOT NULL,
    raw_probes JSONB
);
CREATE INDEX idx_hcs_host_captured ON host_capability_snapshots(host_id, captured_at DESC);
```

### HTTP

```
GET  /api/host/capabilities
  200 → application/json: HostCapabilitySnapshot
  404 → {"reason":"no-snapshot-yet"}

POST /api/host/capabilities/rediscover
  202 → {"snapshot_id":"...","queued_at":"..."}
```

## 5. Acceptance Criteria

- **AC-001**: Given a fresh DB, when the server boots with `--bootstrap-discovery`, then `host-discovery-cpn` runs once and one snapshot row exists in `host_capability_snapshots`.
- **AC-002**: Given a snapshot ≤ 24 h old, when a new session starts, then `host-discovery-cpn` is NOT re-run and the session's root CPN has `p-host-capabilities` marked with the cached snapshot.
- **AC-003**: Given a host with `gcc` present, when probes run, then the snapshot has `capabilities[can-compile-c].satisfied == true` and `binaries[gcc].present == true`.
- **AC-004**: Given a host missing `python3`, when probes run, then `binaries[python3].present == false` and no CPN failure occurs.
- **AC-005**: Given `GET /api/host/capabilities`, when called by an admin user, then it returns the latest snapshot JSON.
- **AC-006**: Given `POST /api/host/capabilities/rediscover`, when called, then a new snapshot is written within 30 seconds.
- **AC-007**: The full discovery run on the dev host (with all expected binaries) MUST complete in ≤ 5 seconds wall-clock.
- **AC-008**: Environment variables matching `/TOKEN|KEY|PASSWORD|SECRET/i` MUST NOT appear in any persisted field.

## 6. Test Automation Strategy

- **Test Levels**:
  - **Unit**: `t-derive` logic, probe parsers (`go version go1.22.3 linux/amd64` → `"1.22.3"`), repository cache expiration.
  - **Integration (host)**: run the real `host-discovery-cpn` on CI (`ubuntu-latest`), assert at least `{bash, sh, curl, git, tar}` are detected.
  - **Integration (Postgres)**: Ryuk-managed Postgres container, migrate, `Save`, `LatestForHost`.
- **Frameworks**: `testing`, `testify/require`, `testcontainers-go` for Postgres.
- **Test data**: fixture JSON snapshots under `testdata/host_snapshots/` for regression.
- **CI integration**: GitHub Action step `make test-host-discovery`.
- **Coverage requirements**: ≥ 85% on `cmd/server/topologies_host_discovery.go` and the derive function.
- **Performance**: benchmark the probe parallel fan-out; target < 2 s on the CI runner.

## 7. Rationale & Context

**Why persist?** A fresh probe every session wastes 2-5 s of user-facing latency and churns `/proc`. One snapshot every 24 h is the right balance between staleness and cost.

**Why deterministic (no LLM)?** Host state is finite and regular; LLMs hallucinate binaries, versions, and flag names. The one place we should *not* delegate to a language model is checking whether `gcc` exists.

**Why `ColorHostFact` as its own color?** Downstream transitions shouldn't accidentally feed host facts into a planner that expects user input. The color separation is enforced by `Validate()`.

**Why expose a REST endpoint?** Debugging "the agent thinks it can compile but it can't" requires an out-of-band read. The admin endpoint is the simplest surface.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: GAP-1 (`HostAdapter` + `NodeKindBash`) — hard dependency.

### Third-Party Services
- None.

### Infrastructure Dependencies
- **INF-001**: Postgres 15+ (already in the stack).
- **INF-002**: `/etc/machine-id` readable; fallback: hash of `hostname + first MAC`.

### Data Dependencies
- **DAT-001**: `/etc/os-release` parsed best-effort (missing file → `os_release={}`).

### Technology Platform Dependencies
- **PLT-001**: Linux only.

### Compliance Dependencies
- **COM-001**: Env var redaction (see `GUD-002`) to avoid accidental secret persistence.

## 9. Examples & Edge Cases

### Example snapshot JSON

```json
{
  "id": "01931d0a-...",
  "host_id": "a8f19ab…",
  "captured_at": "2026-04-17T10:00:00Z",
  "source": "bootstrap",
  "identity": {"user":"liwaisi","home":"/home/liwaisi","shell":"/usr/bin/zsh"},
  "kernel": {"os":"Linux","kernel":"6.17.9…","arch":"x86_64","cpu_count":16,"mem_mb":32768},
  "binaries": [
    {"name":"gcc","path":"/usr/bin/gcc","present":true,"version":"13.2.0","duration_ms":42},
    {"name":"python3","present":false}
  ],
  "capabilities": [
    {"name":"can-compile-c","satisfied":true,"derived_from":["gcc"]},
    {"name":"can-run-python","satisfied":false,"derived_from":["python3"]}
  ]
}
```

### Edge case — machine-id absent

Fallback: `sha256(hostname + first-MAC-address)`. Recorded in `identity.machine_id_source: "fallback"`.

### Edge case — `/etc/os-release` unreadable

`kernel.os_release = {}`, no snapshot failure. `t-derive` tolerates missing data.

### Edge case — probe timeout

`BinaryProbe.DetectionErr = "timeout"`, `Present = false`. The CPN continues.

## 10. Validation Criteria

- All 22 probes have parsers with test coverage.
- `HostCapabilityRepository` cache invalidates exactly at TTL (test with fake clock).
- Running `host-discovery-cpn` twice in 24 h without `--force` returns cached (verify via timing).
- No secret-like env var appears in any snapshot (regex check in test).
- `Validate()` rejects a topology that tries to deposit a non-`ColorHostFact` token into `p-host-capabilities`.

## 11. Related Specifications / Further Reading

- [spec-architecture-host-adapter-nodekind-bash.md](./spec-architecture-host-adapter-nodekind-bash.md) — GAP-1, prerequisite.
- [spec-architecture-tool-forge-cpn.md](./spec-architecture-tool-forge-cpn.md) — GAP-5, primary consumer.
- [spec-architecture-cpn-synthesis-instantiate.md](./spec-architecture-cpn-synthesis-instantiate.md) — GAP-4, reads capabilities to decide synth targets.
- [motor_agentico_cpn.md](../.docs/motor_agentico_cpn.md) §11 Group Agents / Observer pattern.
