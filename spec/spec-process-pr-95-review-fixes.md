---
title: PR #95 Review Fix-up — Model Registry Foundation + Host Gate Hardening
version: 1.0
date_created: 2026-04-21
last_updated: 2026-04-21
owner: brae platform / liwaisi
tags: [process, registry, security, concurrency, migration, postgres, host-gate]
---

# Introduction

This specification captures the concrete fixes required to resolve the findings of the expert-panel code review against PR #95 (`feat(registry): model registry foundation + awakening/HITL/CPN upgrades`). The review surfaced one P0 (guaranteed prod crash), four P1 (security / correctness / data), six P2 (reliability / performance), and two P3 (process / data-hygiene) issues. This spec locks the fix list and the acceptance criteria so implementation is deterministic and auditable.

## 1. Purpose & Scope

**Purpose**: Land all P0 + P1 + P2 findings from the PR #95 review, plus the P3 items that are cheap to include in the same sweep. Each finding is traceable to a file + line and to an `REQ-FIX-NNN` id below.

**Scope**: Backend only — `back/go-assistant/**` Go files and `back/go-assistant/store/postgres/migrations/**` SQL. No frontend changes are required by this fix sweep; the review produced no P0/P1/P2 findings in `front/react-assistant`.

**Out of scope**: Larger architectural changes previously scheduled for GAP-6 (full policy engine replacing `DefaultDenyList`), multi-tenant audit store redesign, and any feature work not already merged in PR #95.

**Audience**: Backend engineers implementing the fixes; code reviewers verifying the sweep.

**Assumptions**: Fixes land on the same branch (`feat/model-registry-foundation`) as follow-up commits, not a separate PR. Migration 021 is re-applied against dev databases after the fix.

## 2. Definitions

- **HITL**: Human-in-the-loop.
- **CPN**: Coloured Petri Net (this project's execution graph abstraction).
- **PR #95**: The parent pull request `feat(registry): model registry foundation + awakening/HITL/CPN upgrades`.
- **Invokable**: A model-registry row that passes REQ-GATE-001 (lifecycle `active` AND license in the approved set).
- **TOCTOU**: Time-of-check-to-time-of-use race.
- **P0/P1/P2/P3**: Severity levels from the review — critical, high, medium, low.
- **`AllowedRoot`**: `$HOME/.local/brae/` — the filesystem jail for `HostAdapter.ReadFile` / `WriteFile`.
- **Route JSON**: The `routes` JSONB column on `models`, carrying `{provider_adapter, provider_model_id, priority, enabled}` records.

## 3. Requirements, Constraints & Guidelines

All requirements are mandatory. Ids with `REQ-FIX-` prefix are issue-driven; `SEC-FIX-` are security-specific; `CON-` are constraints bounding the fix; `GUD-` are non-binding guidelines.

### P0 — Must land before any deploy

- **REQ-FIX-001** *(P0, Concurrency)*: `infra/host/gate/policy_gate.go:staticSandboxCapability.cache` MUST be safe for concurrent access from multiple `HostGate.Check` callers. The implementation MUST NOT panic with `fatal error: concurrent map read and map write` under any call pattern permitted by the `cpn.HostGate` contract.

### P1 — Must land before merge

- **SEC-FIX-002** *(P1, Security)*: `infra/host/os_adapter.go:checkPathJail` MUST resolve symbolic links before comparing against `AllowedRoot`. A symlink inside the jail that points outside MUST be rejected with `cpn.ErrPathDenied`. This applies to `ReadFile`, `WriteFile`, and `Stat`.
- **REQ-FIX-003** *(P1, API)*: `internal/driving/httpapi/handler_admin_models.go:HandleAdminListModels` MUST return in `AdminModelListResponse.Total` the total number of rows matching the filter (ignoring pagination), not the length of the current page. The admin adapter `cpn.ModelRegistry` MUST expose a way to obtain this count alongside a page of entries.
- **REQ-FIX-004** *(P1, Correctness)*: `store/postgres/model_registry.go:scanEntry` MUST NOT silently swallow `json.Unmarshal` errors for the `routes` column. A decode failure on `routes` MUST propagate as a wrapped error. `modalities`, `capabilities`, `supported_parameters`, `default_parameters`, and `source_metadata` MAY retain defaulting behaviour but MUST emit a `slog.Warn` with `registry_id` and column name when defaulting occurs.
- **DAT-FIX-005** *(P1, Data)*: Migration 021 `provider_model_id` values for OpenRouter routes MUST use dash-normalised slugs that match the provider's canonical ids (`anthropic/claude-opus-4.7` → `anthropic/claude-opus-4-7`, `anthropic/claude-sonnet-4.5` → `anthropic/claude-sonnet-4-5`, `anthropic/claude-haiku-4.5` → `anthropic/claude-haiku-4-5`). A new migration (`024_model_registry_openrouter_slug_fix.up.sql` + down) MUST correct in-place without duplicating rows. Idempotent UPDATE keyed on `registry_id`.

### P2 — Must land in this sweep

- **REQ-FIX-006** *(P2, Concurrency)*: `store/postgres/model_registry.go:Delete` MUST perform the default-check and the `DELETE` in a single transaction with `SELECT ... FOR UPDATE` on the target row. An FK violation on `models.registry_id` from `model_role_defaults` or `registry_config` MUST be translated to a typed sentinel (`cpn.ErrCannotDeleteDefault` for the default pointer case, `cpn.ErrModelInUse` for role-binding).
- **SEC-FIX-007** *(P2, Security)*: `infra/host/os_adapter.go:DefaultDenyList` + `guard` MUST either (a) be removed in favour of requiring a non-nil `cpn.HostGate` at construction time OR (b) be upgraded to canonicalise the command and match deny patterns against the literal argv[0]+args tuple (not substring of joined string). Option (a) is preferred because `PolicyHostGate` is already wired in this PR.
- **REQ-FIX-008** *(P2, Concurrency)*: `infra/host/gate/policy_gate.go:auditAsync` MUST use a bounded worker pool with an explicit queue. On graceful shutdown the pool MUST drain or record the loss via a metrics counter. Unbounded `go func()` spawning is forbidden.
- **REQ-FIX-009** *(P2, Correctness)*: `infra/host/gate/policy_gate.go:sessionIDFromOp` either (a) MUST be deleted along with any exported API that calls it, OR (b) MUST route through the real session id plumbed via `GateOp`. Returning a constant `"default"` is forbidden.
- **REQ-FIX-010** *(P2, Code quality)*: Add `cpn.ErrDuplicateRegistryID` sentinel. `store/postgres/model_registry.go:Insert` MUST return it on pg error code `23505`. `handler_admin_models.go:adminDuplicate` MUST use `errors.Is`; the string-probe helper is deleted.
- **REQ-FIX-011** *(P2, Performance)*: `internal/app/session_service.go:resolveModelWithGate` MUST NOT issue more than one registry round-trip per transition in the common case. Implementation options: per-request in-memory cache keyed by candidate id, or a batched `ListInvokable` pre-fetch at session setup. The chosen approach is at the implementer's discretion as long as acceptance criteria AC-011 holds.

### P3 — Include in the sweep

- **REQ-FIX-012** *(P3, Data-hygiene)*: Migration 020 product-default pointer MUST be moved from `google/gemma-4-31b-it` (unverified / likely-nonexistent slug) to `google/gemini-2.5-flash` (confirmed available via OpenRouter). Done either in `024_model_registry_openrouter_slug_fix.up.sql` or in a dedicated migration `025_fix_product_default.up.sql`. Idempotent.
- **GUD-FIX-013** *(P3, Process)*: The follow-up commit MUST include `go test -race ./...` output in its description, and a `go test -cover ./store/postgres/... ./internal/app/... ./infra/host/...` delta for the changed packages.

### Constraints

- **CON-001**: No public API (HTTP route shape, JSON field names, cpn port signatures) may change in a breaking way. New sentinel errors and an additional `Total` field ARE additive. Removing `AllowAllHostGate` (option REQ-FIX-007(a)) is ONLY permitted if every existing call site is migrated in the same change.
- **CON-002**: Migrations MUST be idempotent (`ON CONFLICT DO NOTHING` on inserts, `WHERE`-guarded UPDATEs so re-run is a no-op).
- **CON-003**: No changes to hexagonal boundaries. `cpn/` stays free of `os/exec`, `os`, `syscall`, `github.com/creack/pty`.
- **CON-004**: No changes to the frontend in this sweep. If a fix incidentally requires a type change in `front/react-assistant/src/services/api.ts` (e.g. `Total` surfaced to the client), the change is permitted and MUST preserve existing call sites.

### Guidelines

- **GUD-001**: Prefer sync primitives that match the access pattern: `sync.RWMutex` or pre-populated read-only map for `staticSandboxCapability.cache` (reads dominate); bounded `chan` + N workers for audit.
- **GUD-002**: Error sentinels live in `cpn/errors.go`. Adapters translate pg codes to sentinels at the adapter seam, not in the HTTP layer.
- **GUD-003**: Each fix gets a dedicated commit with a message of the form `fix(registry|hitl|host): <one-line> (REQ-FIX-NNN)`.

## 4. Interfaces & Data Contracts

### 4.1 `cpn.ModelRegistry` port additions

```go
// New sentinel in cpn/errors.go.
var ErrDuplicateRegistryID = errors.New("registry_id already exists")
var ErrModelInUse          = errors.New("model is bound to a role default")

// ListAll signature changes to return the total count so pagination is correct.
ListAll(ctx context.Context, filter ModelListFilter) (entries []*ModelRegistryEntry, total int, err error)
```

### 4.2 `AdminModelListResponse`

```json
{
  "items": [ /* ModelRegistryEntryResponse */ ],
  "total": 42,   // total rows matching filter, independent of page
  "page":  1,
  "size":  20
}
```

### 4.3 `cpn.GateOp` — session-id plumbing (REQ-FIX-009)

```go
type GateOp struct {
    Kind      string // "exec", "spawn_pty", "read_file", "write_file", "kill"
    Command   string
    Path      string
    Sandbox   SandboxProfile
    SessionID string // NEW: plumbed by fire_bash / host adapter callers
}
```

### 4.4 Migration 024 shape (DAT-FIX-005 + REQ-FIX-012)

```sql
-- 024_model_registry_openrouter_slug_fix.up.sql
BEGIN;

-- Dot → dash provider_model_id corrections on 021-seeded Anthropic models.
UPDATE models
SET routes = jsonb_set(
    routes,
    '{0,provider_model_id}',
    to_jsonb('anthropic/claude-opus-4-7'::text),
    false
)
WHERE registry_id = 'anthropic/claude-opus-4-7'
  AND routes->0->>'provider_model_id' = 'anthropic/claude-opus-4.7';

-- Repeat for claude-sonnet-4-5, claude-haiku-4-5.

-- REQ-FIX-012: repoint product default to a confirmed-available model.
UPDATE registry_config
SET product_default_model_id = (SELECT id FROM models WHERE registry_id = 'google/gemini-2.5-flash'),
    updated_by = 'migration-024',
    updated_at = NOW()
WHERE id = 1
  AND product_default_model_id = (SELECT id FROM models WHERE registry_id = 'google/gemma-4-31b-it');

COMMIT;
```

### 4.5 Audit worker pool shape (REQ-FIX-008)

```go
// policy_gate.go
type auditWorkerPool struct {
    queue chan *persist.GateDecisionRecord
    wg    sync.WaitGroup
    store persist.GateDecisionRepository
    log   *slog.Logger
    drops ProcessEventMetrics // reuse existing port for drop counting
}

func (p *auditWorkerPool) Submit(rec *persist.GateDecisionRecord) // non-blocking, drops with metric
func (p *auditWorkerPool) Shutdown(ctx context.Context) error     // drain or cancel
```

## 5. Acceptance Criteria

- **AC-001** *(REQ-FIX-001)*: `go test -race ./infra/host/gate/... -run TestPolicyHostGate_ConcurrentCheck -count=50` passes. A new test spawns ≥32 goroutines calling `Check` with distinct sandbox profiles; no race detector output, no panic.
- **AC-002** *(SEC-FIX-002)*: Given a symlink at `$HOME/.local/brae/evil -> /etc/passwd`, when `ReadFile($HOME/.local/brae/evil)` is called, then the call returns `cpn.ErrPathDenied` and does not read `/etc/passwd`. Same for `WriteFile` and `Stat`. Regression test in `os_adapter_test.go`.
- **AC-003** *(REQ-FIX-003)*: Given the DB contains 25 models and the request `GET /api/v1/admin/models?size=10&page=1`, when the handler responds, then `response.total == 25 && len(response.items) == 10 && response.page == 1 && response.size == 10`.
- **AC-004** *(REQ-FIX-004)*: Given a `models` row with malformed `routes` JSONB (e.g. `'{ bad'`), when `GetByID` is called, then the call returns a non-nil error wrapping the JSON decode error. Given a row with malformed `capabilities`, the call succeeds AND a `slog.Warn` is emitted.
- **AC-005** *(DAT-FIX-005)*: After applying migration 024, `SELECT routes->0->>'provider_model_id' FROM models WHERE registry_id = 'anthropic/claude-opus-4-7'` returns `'anthropic/claude-opus-4-7'`. The migration is idempotent — re-running it produces zero `UPDATE` rows.
- **AC-006** *(REQ-FIX-006)*: Given the row `X` is the current product default, when two concurrent `Delete(X)` calls race, then both receive `cpn.ErrCannotDeleteDefault` (not one success + one FK error). Given `X` is only referenced by `model_role_defaults`, `Delete(X)` returns `cpn.ErrModelInUse`.
- **AC-007** *(SEC-FIX-007)*: Given the adapter is constructed without `WithHostGate(...)`, when `Exec` is called with `rm -rf /` or `bash -c 'rm -rf /'`, then it returns `cpn.ErrGateDenied` (option a: constructor refuses to build; option b: deny still matches canonical argv). No substring-splat bypass (`rm  -rf /` with double space) succeeds.
- **AC-008** *(REQ-FIX-008)*: Load-test: 1000 `Check` calls in parallel. No more than 16 (worker pool cap) audit goroutines observable via `runtime.Stack`. On shutdown with a 1s timeout, either the queue drains fully or `metrics.OnDrop(..., "audit_shutdown_drop")` fires for the remainder.
- **AC-009** *(REQ-FIX-009)*: `grep -R '"default"' infra/host/gate/ | grep -v test` returns no hits in non-test code. Either `sessionIDFromOp` is deleted, or it receives the real session id via the updated `GateOp.SessionID`.
- **AC-010** *(REQ-FIX-010)*: `errors.Is(err, cpn.ErrDuplicateRegistryID)` is true when `Insert` is called with a conflicting `registry_id`. `handler_admin_models.go` no longer contains `strings.Contains(msg, "duplicate")`.
- **AC-011** *(REQ-FIX-011)*: Given a session topology with 8 LLM transitions, when `resolveModelWithGate` runs against a 10-row registry, then the total number of `ModelRegistry.*` calls for the session is ≤ 2 per transition on a cache hit and ≤ 4 on first miss (not 4 every time). Benchmark `BenchmarkResolveModelWithGate_Cached` in `session_service_test.go` demonstrates ≤ 1 allocation-backed registry hit on the hot path.
- **AC-012** *(REQ-FIX-012)*: After migration 024 (or 025), `SELECT registry_id FROM models WHERE id = (SELECT product_default_model_id FROM registry_config WHERE id = 1)` returns `'google/gemini-2.5-flash'`.
- **AC-013** *(GUD-FIX-013)*: Final commit message for the sweep contains `go test -race ./...` summary and coverage numbers for the three packages touched.

## 6. Test Automation Strategy

- **Test Levels**:
  - Unit — tighten existing `*_test.go` in `store/postgres`, `infra/host`, `infra/host/gate`, `internal/driving/httpapi`, `internal/app`.
  - Integration — use `testcontainers-go` or the existing postgres test harness to exercise migration 024 + adapter round-trip.
  - Concurrency — `go test -race` with new table tests specifically crafted for AC-001 and AC-008.
- **Frameworks**: Standard `testing` package. `github.com/stretchr/testify` is already in `go.mod` and may be used. For the HTTP handler, `net/http/httptest`.
- **Test Data Management**: Migration tests use a fresh postgres instance per package (existing pattern in `model_registry_migration_test.go`).
- **CI/CD Integration**: Existing GitHub Actions backend pipeline runs `go test -race ./...` and `go vet`. Add no new CI steps; ensure existing ones remain green.
- **Coverage Requirements**: No regressions against the pre-sweep branch. Target ≥ 85 % for `store/postgres/model_registry.go`, `infra/host/os_adapter.go`, `infra/host/gate/policy_gate.go`, `internal/driving/httpapi/handler_admin_models.go`.
- **Performance Testing**: One benchmark (`BenchmarkResolveModelWithGate_Cached`) gated by AC-011. No load-test infra changes.

## 7. Rationale & Context

Each fix in §3 traces to a specific review finding documented with file + line + severity. The review was conducted in a single pass against the 299-file / 53 575-line PR; the fixes herein are the minimum set that clears P0 + P1 + P2 without re-opening the larger GAP-6 scope. P3 items are included opportunistically because they share files with the P1/P2 work — separating them would cost more in review than in implementation.

Design choices worth recording:

- **Propagate-then-warn split for `scanEntry`**: `routes` is load-bearing for invocation (a model with empty routes cannot be dispatched), so a decode failure must be surfaced to the caller. `capabilities`, `modalities`, etc. are advisory — defaulting them preserves availability at the cost of a warning log line, which is the right trade-off.
- **Preferring option (a) for REQ-FIX-007**: The `DefaultDenyList` is already documented as a stub ("GAP-6 replaces this with a real policy engine"). `PolicyHostGate` is present in this PR. The deny-list stub is now dead weight. Deleting it makes the gate contract honest.
- **Worker pool sized to 16 for audit**: Matches the budget tracker's existing worker count conventions (see `BudgetTracker`). 16 is enough for realistic LLM fan-out, small enough to cap cost on burst.
- **Migration 024 vs. 025**: Single migration keeps the forward/reverse pair atomic (both the slug fix and the default-pointer fix are corrections to the same migration-020/021 seed, so rolling them back together is sane). Splitting is acceptable if the implementer wants to isolate blast radius.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: OpenRouter — provider-model-id naming authority for DAT-FIX-005. Verification is manual at implementation time (curl `https://openrouter.ai/api/v1/models`) since the project has no automated provider-slug audit.

### Infrastructure Dependencies
- **INF-001**: Postgres 15+ — migration 024 uses `jsonb_set` with the `create_missing` flag, available since 9.5. No new infra.

### Technology Platform Dependencies
- **PLT-001**: Go 1.22+ — `http.Request.PathValue` is already used in the admin handlers; no version bump required.

### Compliance Dependencies
- **COM-001**: Audit trail integrity (SEC-FIX-008) — pre-existing REQ-OBS-series in `spec-architecture-host-gate-security-policy.md` requires every gate decision to be persisted. The bounded worker pool preserves that contract under load; drop events are metric-observable.

## 9. Examples & Edge Cases

### 9.1 Symlink escape regression (AC-002)

```go
// os_adapter_test.go
func TestReadFile_RejectsSymlinkEscape(t *testing.T) {
    tmp := t.TempDir()
    root := filepath.Join(tmp, ".local", "brae")
    require.NoError(t, os.MkdirAll(root, 0o755))

    victim := filepath.Join(tmp, "secret")
    require.NoError(t, os.WriteFile(victim, []byte("shh"), 0o600))

    evil := filepath.Join(root, "evil")
    require.NoError(t, os.Symlink(victim, evil))

    a := &OSHostAdapter{AllowedRoot: root, Logger: slog.Default()}
    _, err := a.ReadFile(context.Background(), evil)
    require.ErrorIs(t, err, cpn.ErrPathDenied)
}
```

### 9.2 Concurrent sandbox capability (AC-001)

```go
func TestStaticSandboxCapability_RaceFree(t *testing.T) {
    s := NewStaticSandboxCapability()
    runtimes := []string{"bwrap", "firejail", "nsjail", "docker"}
    var wg sync.WaitGroup
    for i := 0; i < 64; i++ {
        wg.Add(1)
        go func(i int) {
            defer wg.Done()
            s.Available(runtimes[i%len(runtimes)])
        }(i)
    }
    wg.Wait()
}
```

### 9.3 Pagination contract (AC-003)

```
GET /api/v1/admin/models?size=10&page=1  →  {items: [10], total: 25, page: 1, size: 10}
GET /api/v1/admin/models?size=10&page=3  →  {items: [5],  total: 25, page: 3, size: 10}
GET /api/v1/admin/models?vendor=google   →  {items: [N],  total: N,  page: 0, size: 0}
```

### 9.4 Idempotent migration (AC-005)

```sql
-- First run
UPDATE models SET routes = jsonb_set(...) WHERE routes->0->>'provider_model_id' = '...4.7';
-- → UPDATE 1
-- Second run
-- → UPDATE 0   (predicate no longer matches)
```

### 9.5 Edge case: adapter constructed without gate (AC-007, option a)

```go
_, err := host.NewOSHostAdapter(logger)       // no WithHostGate → compile or construction error
_, err := host.NewOSHostAdapter(logger, host.WithHostGate(gate.NewAllowAllHostGate())) // explicit opt-in
```

## 10. Validation Criteria

- All acceptance criteria AC-001..AC-013 pass in CI.
- `go test -race ./...` is green.
- `go vet ./...` is clean.
- Reviewer (the PR #95 author) manually applies migration 024 to a scratch database and confirms:
  - `SELECT registry_id, routes->0->>'provider_model_id' FROM models WHERE vendor='anthropic';` returns dashed slugs only.
  - `SELECT registry_id FROM models m JOIN registry_config rc ON rc.product_default_model_id = m.id;` returns `google/gemini-2.5-flash`.
- No new `TODO` / `FIXME` comments introduced by this sweep.

## 11. Related Specifications / Further Reading

- `spec/spec-architecture-model-registry-and-a2ui-management.md` — parent spec for the registry; REQ-GATE-001, REQ-API-004..008, REQ-LIC-003 originate here.
- `spec/spec-architecture-host-gate-security-policy.md` — parent spec for `PolicyHostGate`; REQ-021, REQ-031, REQ-042, AC-007, AC-008 originate here.
- `spec/spec-architecture-host-adapter-nodekind-bash.md` — parent spec for `HostAdapter`; SEC-001, SEC-002 originate here.
- PR https://github.com/liwaisi-tech/liwaisi_assistant/pull/95 — the target PR.
