---
title: HostGate Security Policy — Authorising Every OS Operation Before It Runs
version: 1.0
date_created: 2026-04-17
last_updated: 2026-04-17
owner: liwaisi
tags: [architecture, security, policy, cpn, hitl, brae, gap-6]
---

# Introduction

GAP-1 introduces `HostAdapter` and `NodeKindBash`. Without a real policy layer, those primitives can run arbitrary shell on the user's machine — the single most dangerous capability in `brae`. This spec replaces the allow-all `HostGate` stub with a policy engine that classifies every operation, enforces deny-lists, rejects sandbox-incompatible commands, and mandates HITL approval for first-time-run binaries and high-risk patterns.

The gate is declarative, auditable, and extensible. It is the analogue — for tools and the host — of the `REQ-GATE-001` gate that already protects LLM model invocations.

## 1. Purpose & Scope

**Purpose.** Make `brae`'s interaction with the host provably safe-by-default, with explicit escalation paths for anything risky, and with full audit of every authorised operation.

**In scope.**
- `HostGate` policy implementation replacing the allow-all stub from GAP-1.
- A YAML-loaded `HostPolicy` declaring: allow/deny command patterns, path jails (default: `$HOME/.local/brae/`), network allow-lists per sandbox profile, per-command HITL requirements, per-session resource budgets (max CPU-seconds, max bytes written, max outbound requests).
- A `FirstRunLedger` (Postgres) that records each binary's first execution and requires HITL approval on that first run.
- An audit log (`host_gate_decisions`) persisting every gate decision.
- Sandbox runtime: integration with `bwrap` (preferred) or `firejail` (fallback) — enforces `readonly`, `fsjail`, `network-off` profiles. If neither is present on the host, profiles degrade with loud warnings.
- A HITL prompt schema `host-approval` distinct from the clarification HITL.
- HTTP endpoints for inspecting and editing policy (admin only).
- A pre-seeded v1 policy bundled with the repo (`infra/host/policies/default.yaml`).
- Test coverage: allow, deny, HITL path, first-run path, budget exhaustion, sandbox fallback.

**Out of scope.**
- SELinux / AppArmor profile authoring — pushed to a future spec.
- Remote-host execution (only localhost in v1).
- Per-user (multi-tenant) policy — single-user `brae` only in v1.

**Audience.** `golang-pro` for the policy engine; the ops/security reviewer for the default policy.

## 2. Definitions

- **Op**: A single `GateOp` passed to `HostGate.Check` by the `HostAdapter` before it does anything.
- **Decision**: `allow | deny | require-hitl`. `require-hitl` pauses the CPN and routes a question to the user.
- **Sandbox profile**: `none | readonly | fsjail | network-off`. Runtime enforcement is declared by `bwrap`/`firejail` flag mapping.
- **First-run**: The first time a given binary path (by SHA-256 of the file) is executed on this host by `brae`. Even allow-listed binaries hit first-run HITL.
- **Budget**: A session-scoped counter; exceeding it denies further ops of that kind.
- **Risk band**: `safe | caution | dangerous | forbidden`. Pattern-matched against command/args.

## 3. Requirements, Constraints & Guidelines

### Policy engine

- **REQ-001**: A `HostPolicy` struct MUST be YAML-loadable from `infra/host/policies/*.yaml`. Server boots with `HOST_POLICY_PATH` env var (default `infra/host/policies/default.yaml`).
- **REQ-002**: `HostGate.Check(ctx, op)` MUST evaluate policy in this order: forbidden patterns (deny) → session budgets (deny if exhausted) → first-run ledger (require-hitl if new binary) → HITL patterns (require-hitl) → allow patterns (allow) → default deny.
- **REQ-003**: Every decision MUST be recorded in `host_gate_decisions` with fields: `id, session_id, op_kind, command_hash, path, sandbox, decision, reason, risk_band, decided_at, hitl_response_id nullable`.
- **REQ-004**: On `require-hitl`, the gate MUST return `ErrRequiresHITL` carrying the prompt. The calling bash transition MUST route to a companion HITL transition (`t-host-approve`) automatically created by the gate harness — not every topology author's responsibility.

### First-run ledger

- **REQ-010**: Schema (`0016_first_run_ledger.sql`): `id UUID, host_id TEXT, binary_path TEXT, binary_sha256 TEXT, first_seen TIMESTAMPTZ, first_approved_by TEXT nullable, first_approved_at TIMESTAMPTZ nullable, revoked BOOLEAN DEFAULT false`.
- **REQ-011**: Before executing any binary (including system ones like `/usr/bin/gcc`), the gate MUST SHA-256 the file and check the ledger. Unknown hash → HITL prompt asking the user whether to trust this binary for this host (approve / deny / approve-and-pin-version).
- **REQ-012**: System binaries discovered during GAP-2 (`host-discovery-cpn`) MUST be auto-approved on discovery if the user confirms once at bootstrap (one approval for the whole probe set, not 22 separate prompts).
- **REQ-013**: Binaries authored by `brae` (produced by the tool-forge, GAP-5) MUST *always* hit first-run HITL on their first execution regardless of origin.

### Sandbox runtime

- **REQ-020**: Sandbox profile mapping to `bwrap`:
  - `readonly`: `--ro-bind / / --tmpfs /tmp --proc /proc --dev /dev --unshare-all --share-net`
  - `fsjail`: `readonly` + `--bind $HOME/.local/brae/work /work --chdir /work`
  - `network-off`: `readonly` + `--unshare-net`
  - `none`: no wrapping; command runs directly (must be explicitly allowed by policy).
- **REQ-021**: If `bwrap` is absent and `firejail` is present, an equivalent mapping MUST be used. If both are absent, `readonly`/`fsjail`/`network-off` profiles MUST be rejected with `ErrSandboxUnavailable` (the command does NOT silently downgrade to `none`).
- **REQ-022**: Sandbox availability is read from `HostCapabilityRepository` (GAP-2) — no re-probing per call.

### Session budgets

- **REQ-030**: Default per-session budgets: 120 CPU-seconds, 256 MiB written under `$HOME/.local/brae/`, 50 outbound HTTP requests.
- **REQ-031**: Budgets MUST be enforced in the gate (pre-flight estimate) and reconciled post-flight (actual consumption). Overrun during execution MUST kill the process via `HostAdapter.KillPID(SIGTERM)`.
- **REQ-032**: Budget exhaustion MUST emit an `EventBudgetExhausted` event and a HITL prompt offering `[raise-budget | terminate-task]`.

### HITL integration

- **REQ-040**: The HITL prompt for host approvals MUST have schema identifier `"host.approval"` and payload fields: `operation (exec|spawn_pty|write_file|kill)`, `command`, `risk_band`, `rationale (string)`, `alternatives (optional string[])`.
- **REQ-041**: The UI renderer (front) MUST display host-approval HITLs with distinct visual treatment from normal clarification (red border, explicit `Risk: caution/dangerous`).
- **REQ-042**: User actions: `approve-once`, `approve-and-remember` (adds to allow-list for this binary+arg-pattern), `deny`, `deny-and-blacklist`.

### Policy format

- **REQ-050**: Default policy YAML MUST ship with sensible baselines:
  - Forbidden: `rm -rf /`, `:(){ :|:& };:`, `dd if=/dev/zero of=/dev/sd*`, `curl | sh`, `wget | sh`, `mkfs.*`, `shutdown`, `reboot`, `init 0`, `systemctl poweroff`.
  - Safe (always-allow unless first-run): `/bin/ls`, `/usr/bin/cat`, `/usr/bin/pwd`, `/usr/bin/echo`, `/usr/bin/uname`, `/usr/bin/whoami`, `/usr/bin/id`, `/usr/bin/hostname`, `/usr/bin/which`, `/usr/bin/env`.
  - Caution (HITL-every-run unless path-pinned + hash-pinned): all compilers (`gcc`, `cc`, `clang`, `go build`), all package managers (`apt`, `dnf`, `pacman`, `pip`, `npm`, `cargo`), network clients (`curl`, `wget`, `ssh`).
  - Dangerous (HITL-always, even after remember): anything under `/etc/`, writes to `~/.ssh/`, `chmod +x` of a just-written file.

### Guidelines

- **GUD-001**: Prefer deny-by-default. If a pattern is ambiguous, fall through to HITL, not allow.
- **GUD-002**: Log denied ops with the same fidelity as allowed ops. Denial traces are the primary debugging signal.
- **GUD-003**: Never suppress a gate decision in tests. Integration tests that need bash MUST inject a test policy, not disable the gate.
- **PAT-001**: Gate errors propagate as first-class `HostError` values with stable `Code` — do not string-match error messages.

## 4. Interfaces & Data Contracts

### `HostPolicy` YAML

```yaml
# infra/host/policies/default.yaml
version: 1
defaults:
  sandbox: readonly
  cpu_seconds_budget: 120
  bytes_written_budget: 268435456
  outbound_requests_budget: 50
forbidden_patterns:
  - "^rm\\s+-rf\\s+/(\\s|$)"
  - "^mkfs\\."
  - "^shutdown"
  - "^reboot"
  - "^systemctl\\s+(poweroff|reboot)"
  - "^:\\(\\)\\s*\\{"
  - ".*curl\\s+.*\\s*\\|\\s*sh\\s*$"
safe_patterns:
  - "^(/(usr|bin))?/(ls|cat|pwd|echo|uname|whoami|id|hostname|which|env)(\\s|$)"
caution_patterns:
  - "^(/(usr|bin))?/(gcc|cc|clang|go|cargo|rustc|javac)(\\s|$)"
  - "^(/(usr|bin))?/(apt|dnf|pacman|pip3?|npm|yarn)(\\s|$)"
  - "^(/(usr|bin))?/(curl|wget|ssh)(\\s|$)"
dangerous_patterns:
  - "^chmod\\s+\\+x\\s+"
  - "write_file:\\s*/etc/.*"
  - "write_file:\\s*~/.ssh/.*"
path_jail:
  write: "$HOME/.local/brae/"
  read:  "/"
sandbox_mappings:
  readonly: { preferred: "bwrap", fallback: "firejail" }
  fsjail:   { preferred: "bwrap", fallback: "firejail" }
  network-off: { preferred: "bwrap", fallback: "firejail" }
```

### Go types

```go
type RiskBand string
const (
    RiskSafe      RiskBand = "safe"
    RiskCaution   RiskBand = "caution"
    RiskDangerous RiskBand = "dangerous"
    RiskForbidden RiskBand = "forbidden"
)

type GateDecision struct {
    Verdict   string // "allow" | "deny" | "require-hitl"
    RiskBand  RiskBand
    Reason    string
    Sandbox   SandboxProfile
    Budget    BudgetEstimate
}

type BudgetEstimate struct {
    CPUSeconds       int
    BytesWritten     int64
    OutboundRequests int
}

type HostGate interface {
    Check(ctx context.Context, op GateOp) (GateDecision, error)
    RecordActual(ctx context.Context, op GateOp, actual BudgetEstimate) error
    ApproveFirstRun(ctx context.Context, hostID, sha256 string, userID string) error
    Blacklist(ctx context.Context, pattern string, reason string) error
}
```

### HTTP

```
GET  /api/host/policy             200 → current policy YAML (admin only)
PUT  /api/host/policy             202 → validates + reloads (admin only)
GET  /api/host/gate-decisions?limit=100   200 → audit log
GET  /api/host/first-run-ledger   200 → ledger rows
POST /api/host/first-run-ledger/:sha/revoke 202 → revokes approval
```

## 5. Acceptance Criteria

- **AC-001**: Given policy default, when a bash transition attempts `rm -rf /`, then `HostGate.Check` returns `Verdict=deny`, `RiskBand=forbidden`, and no process is spawned.
- **AC-002**: Given a caution-pattern command (`gcc hello.c`), when run the first time, then a HITL prompt is issued; on `approve-and-remember`, a second run of the same command with the same binary hash runs without HITL.
- **AC-003**: Given the default session budget, when a bash transition would write 512 MiB, then `Verdict=require-hitl` with reason `budget_exceeded`.
- **AC-004**: Given neither `bwrap` nor `firejail` is present, when a command requests `sandbox=readonly`, then `Verdict=deny` with `reason=sandbox_unavailable`.
- **AC-005**: Every gate decision within a session appears in `host_gate_decisions` with the correct verdict and reason.
- **AC-006**: Given admin user, when `PUT /api/host/policy` receives an invalid YAML, then 422 with a parsed error location is returned and the running policy is unchanged.
- **AC-007**: Given a dangerous-pattern op, when the user has previously `approve-and-remember`'d it, then it STILL requires HITL (dangerous never sticks).
- **AC-008**: Given a tool-forge-produced binary with unknown SHA-256, when first executed, then HITL is mandatory even if the command pattern is in `safe_patterns`.
- **AC-009**: Given CPU-seconds overrun mid-execution, when 10s past budget, then the process receives `SIGTERM` and 5s later `SIGKILL`.

## 6. Test Automation Strategy

- **Test Levels**: unit (pattern matcher, budget arithmetic), integration (sandbox wrapping with real `bwrap`), e2e (CPN with HITL approval path).
- **Frameworks**: standard Go + `testify` + `testcontainers-go` for Postgres + GitHub Actions matrix (`bwrap-present`, `bwrap-absent`).
- **Test data**: fixture policy YAMLs under `testdata/policies/`.
- **CI integration**: security-specific job that runs the forbidden-pattern suite.
- **Coverage requirements**: ≥ 95% on `infra/host/gate/` (security-critical code).
- **Performance**: every gate check < 2 ms on CI.

## 7. Rationale & Context

**Why pattern matching, not a full capabilities model?** `brae` needs to ship. A fine-grained capability model (like SELinux labels) is a rabbit hole; pattern + jail + first-run-HITL is 80% of the risk for 20% of the complexity. The interface allows a future capability layer to slot in.

**Why first-run HITL for every new binary?** The tool-forge will author novel binaries that never existed before. A policy that knows nothing about them is useless unless it pauses and asks.

**Why dangerous-always-HITL?** Anti-footgun: a user who types "approve-and-remember" on a surprising prompt should not permanently hand over `chmod +x` authority.

**Why bwrap over Docker?** No daemon, no image management, no root. A flag-string transformation.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: GAP-1 — provides the call site for every `Check`.
- **EXT-002**: GAP-2 — tells us whether `bwrap` / `firejail` exist.

### Infrastructure Dependencies
- **INF-001**: Postgres (existing).
- **INF-002**: `bwrap` or `firejail` installed on host for sandboxing. Without either, dangerous/caution commands are hard-denied.

### Technology Platform Dependencies
- **PLT-001**: Linux. Sandbox mechanisms are Linux-specific.

### Compliance Dependencies
- **COM-001**: Audit log retention ≥ 90 days. Gate decisions are tamper-evident (row hash-chained).

## 9. Examples & Edge Cases

### Example — caution with remember

```
User: build a URL reader
→ tool-forge emits gcc hello.c
→ gate: caution pattern, first-run not approved → require-hitl
→ HITL to user: "Permitir gcc hello.c? [approve-once | approve-and-remember | deny]"
→ User: approve-and-remember
→ First-run ledger + allow-list updated
→ Next gcc invocation of the same binary path + sha → allow silently
```

### Edge case — dangerous pattern in a "remembered" context

Even after `approve-and-remember`, a `chmod +x some-new-binary` always re-prompts because the dangerous band never caches.

### Edge case — policy hot-reload races

`PUT /api/host/policy` serialises behind a mutex; ongoing gate checks complete under the previous policy; subsequent checks use the new one.

### Edge case — hash collision of unrelated binaries

SHA-256 is assumed collision-free. If a future worry arises, switch to BLAKE3.

## 10. Validation Criteria

- Policy YAML is schema-validated on load; invalid YAML keeps the previous policy.
- Property-based tests on the pattern matcher (`go-fuzz`) MUST not allow a bypass of a forbidden pattern with shell quoting tricks.
- Every `HostAdapter` method call must flow through `HostGate.Check` (enforced by a `go vet` lint or a wrapper assertion).
- No gate check ever blocks the executor loop > 100 ms (checked by benchmark).

## 11. Related Specifications / Further Reading

- [spec-architecture-host-adapter-nodekind-bash.md](./spec-architecture-host-adapter-nodekind-bash.md) — GAP-1.
- [spec-architecture-host-discovery-capability-registry.md](./spec-architecture-host-discovery-capability-registry.md) — GAP-2.
- [spec-architecture-tool-forge-cpn.md](./spec-architecture-tool-forge-cpn.md) — GAP-5 (consumer).
- bwrap manual: <https://manpages.debian.org/bwrap>
- firejail: <https://firejail.wordpress.com/>
