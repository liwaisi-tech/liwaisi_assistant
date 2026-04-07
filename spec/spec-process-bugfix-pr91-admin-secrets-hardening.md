---
title: Bug Fix & Hardening — PR #91 Admin Secrets Manager
version: 1.0
date_created: 2026-04-07
last_updated: 2026-04-07
owner: liwaisi
tags: [process, bugfix, security, admin, secrets, go-assistant, react-assistant]
---

# Introduction

PR liwaisi-tech/liwaisi_assistant#91 introduces an admin-only secrets manager backed by an encrypted `platform_config` table, a hot-reloadable `Provider`, an admin HTTP surface, and a React panel. The expert-panel review surfaced two P0 security defects, six P1 issues (auth, concurrency, masking, tests), and three lower-severity gaps. This specification defines the exact, scoped fixes required to land the PR safely. It is a hardening pass — not a redesign.

## 1. Purpose & Scope

**Purpose.** Eliminate the information-disclosure, authorization, masking, and concurrency defects in PR #91 so the admin secrets manager meets the bar required of a system holding production credentials (master key, OAuth secrets, LLM API keys).

**In scope.**
- `back/go-assistant/internal/config/{provider.go,masterkey.go}` — concurrency, perms, masking.
- `back/go-assistant/internal/driving/httpapi/{handler_admin.go,middleware_admin.go,middleware_auth.go,routes.go}` — status endpoint shape, admin email check, multi-admin support.
- `back/go-assistant/store/postgres/` — new `audit_log` table, repository, migration `014_admin_audit_log.up.sql` / `.down.sql`.
- `back/go-assistant/cmd/server/main.go` — startup validation of `ADMIN_EMAILS`.
- `back/go-assistant/internal/driving/httpapi/middleware_admin_test.go` (new) — table-driven auth tests.
- `back/go-assistant/internal/config/provider_test.go` — race tests for OnChange.
- `docker-compose.yml` — drop `ADMIN_EMAIL` default.
- `front/react-assistant/src/features/admin/AdminSecretsPanel.tsx` — error differentiation.
- `front/react-assistant/src/services/api.ts` — typed error surface for admin endpoints.
- `front/react-assistant/src/i18n/locales/{en,es}/admin.json` — new error strings.

**Out of scope.**
- Encryption scheme (AES-256-GCM, master key format).
- `Encryptor`, nonce generation, key rotation.
- CPN topology, classifier, A2UI surfaces.
- The set of `PlatformConfigs` keys.
- Any new admin features (UI redesign, RBAC roles, secret versioning).
- Migration of existing data.

**Audience.** `golang-pro` (backend), `vercel-react-best-practices` + `frontend-design` (frontend), reviewers.

## 2. Definitions

- **PR #91**: liwaisi-tech/liwaisi_assistant#91 — feat: admin secrets manager with encrypted config storage.
- **Provider**: `internal/config.Provider` — env > db > default resolution layer with hot reload via `OnChange`.
- **Master key**: 32-byte AES-256-GCM key persisted to disk, loaded by `LoadOrGenerateMasterKey`.
- **Admin endpoint**: any route under `/api/v1/admin/`.
- **Status endpoint**: `GET /api/v1/admin/config/status` — public health/setup probe.
- **Dev mode**: server started with `APP_ENV=dev` (or equivalent existing flag). All other modes are non-dev.
- **Audit log**: append-only Postgres table recording every admin secret mutation.
- **Last-4 mask**: secret rendered as `********` followed by the last 4 characters of the secret if length ≥ 8, otherwise `********`.
- **Snapshot iteration**: copying a slice under a lock, releasing the lock, then iterating the copy.

## 3. Requirements, Constraints & Guidelines

### 3.1 P0 — Security

- **REQ-001** *(Status endpoint shape)*. `HandleConfigStatus` (`handler_admin.go:104-120`) MUST return **only** `{"ready": <bool>}`. It MUST NOT return `missing_required` or any other field that names config keys, categories, or counts. The endpoint remains public (no auth).
- **REQ-002** *(Master key file perms on read)*. `LoadOrGenerateMasterKey` (`masterkey.go:16-23`) MUST `os.Stat` the key file after a successful read and return an error if `info.Mode().Perm() & 0o077 != 0` or if the file is not a regular file. The error message MUST name the offending mode and path.
- **REQ-003** *(Master key parent dir perms)*. The parent directory of the master key MUST be checked on both the read and the generate paths; if its mode permits group or world access (`mode & 0o077 != 0`), startup MUST fail with a loud error. The existing `MkdirAll(... , 0o700)` call is retained.

### 3.2 P1 — Authorization, Concurrency, Masking, Tests

- **REQ-101** *(Case-insensitive admin compare)*. `AdminMiddleware` (`middleware_admin.go:14`) MUST compare the bearer email to the configured admin set with `strings.EqualFold`.
- **REQ-102** *(Multi-admin via env)*. The middleware MUST accept a comma-separated `ADMIN_EMAILS` env var and grant access to any user whose email matches any entry (case-insensitive, trimmed). For backward compatibility, if `ADMIN_EMAILS` is unset and the legacy `ADMIN_EMAIL` is set, the legacy single value is used.
- **REQ-103** *(Startup validation)*. `cmd/server/main.go` MUST refuse to start (`log.Fatal`) when, outside dev mode, the resolved admin set is empty OR contains `dev@localhost` OR contains the empty string. In dev mode the default `dev@localhost` is allowed but a warning MUST be logged.
- **REQ-104** *(OnChange snapshot iteration)*. `Provider.Set` and `Provider.Delete` (`provider.go:93-97, 109-113`) MUST snapshot `p.onChange` under `p.mu.RLock()` (or under the existing write lock) before invoking callbacks. The snapshot MUST be iterated **after** the cache mutation has been committed and the lock released.
- **REQ-105** *(OnChange callback ordering)*. The cache mutation and callback dispatch for a given `Set`/`Delete` call MUST be serialized such that, for any single key, callback N+1 cannot be observed before callback N when N+1 was committed after N. The simplest acceptable implementation is a per-Provider serialization mutex (`callMu`) held across `cache write → snapshot → fan-out`.
- **REQ-106** *(OnChange registration safety)*. `Provider.OnChange` (`provider.go:133-135`) MUST take `p.mu.Lock()` while appending. The append MUST happen on a fresh slice (`append(append([]func(...){}, p.onChange...), fn)`) to avoid aliasing with in-flight snapshots.
- **REQ-107** *(Mask format)*. `Provider.ListAll` (`provider.go:147-153`) MUST mask secret values as follows: if `len(value) >= 8`, render `"********" + value[len(value)-4:]`; otherwise render `"********"`. The function MUST NOT return any prefix bytes of the secret. Env-sourced secrets MUST be masked identically to db-sourced secrets.
- **REQ-108** *(Admin middleware tests)*. A new file `middleware_admin_test.go` MUST cover the truth table in §4.3 with table-driven tests, including nil user, wrong email, exact match, case-difference match, and multi-admin list match.
- **REQ-109** *(Provider race tests)*. `provider_test.go` MUST include a test that runs under `-race` and exercises ≥ 100 concurrent `Set`/`OnChange`/`Get` calls to prove REQ-104..REQ-106. The test MUST fail if `go test -race` reports a data race.

### 3.3 P2 — Audit Log & Compose Hardening

- **REQ-201** *(Audit log table)*. Migration `014_admin_audit_log.up.sql` MUST create:
  ```sql
  CREATE TABLE IF NOT EXISTS admin_audit_log (
      id          BIGSERIAL PRIMARY KEY,
      key         TEXT NOT NULL,
      action      TEXT NOT NULL CHECK (action IN ('set','delete')),
      updated_by  TEXT NOT NULL,
      ts          TIMESTAMPTZ NOT NULL DEFAULT NOW()
  );
  CREATE INDEX IF NOT EXISTS idx_admin_audit_log_ts ON admin_audit_log(ts DESC);
  ```
  The matching `.down.sql` MUST drop the index then the table.
- **REQ-202** *(Audit log writes)*. `HandleSetConfig` and `HandleDeleteConfig` MUST insert one row into `admin_audit_log` after the `Provider.Set`/`Delete` call succeeds. The row MUST NOT contain the secret value. A failed audit insert MUST be logged at ERROR but MUST NOT roll back the config change (the change is already durable).
- **REQ-203** *(Compose default removed)*. `docker-compose.yml` MUST NOT default `ADMIN_EMAIL` (or `ADMIN_EMAILS`). The line becomes `ADMIN_EMAILS=${ADMIN_EMAILS:?ADMIN_EMAILS is required}` (compose's required-var syntax).

### 3.4 P3 — Frontend Error Differentiation

- **REQ-301** *(Typed admin API errors)*. `services/api.ts` admin helpers (`getAdminConfig`, `setAdminConfig`, `deleteAdminConfig`) MUST throw a typed error carrying at minimum `{ status: number; message: string }`.
- **REQ-302** *(401 → login)*. `AdminSecretsPanel.tsx` MUST detect a 401 from `getAdminConfig` and trigger the existing auth-redirect flow (whatever `AuthContext` exposes — `signOut()` then route to login). It MUST NOT render the generic error state for 401.
- **REQ-303** *(Distinct error strings)*. The catch in `AdminSecretsPanel.tsx:33-34` MUST distinguish at least: `forbidden` (403), `unavailable` (503), and `generic` (anything else). The 401 case is handled by REQ-302 and never reaches the visible error state.
- **REQ-304** *(i18n)*. `admin.json` (en, es) MUST gain keys `errors.forbidden`, `errors.unavailable`, `errors.generic`. The legacy `error` key MAY remain as an alias for `errors.generic`.

### 3.5 Constraints

- **CON-001**: All Go code MUST pass `go vet ./...`, `go test ./...`, and `go test -race ./...`.
- **CON-002**: No new third-party Go dependencies. Standard library only.
- **CON-003**: No changes to encryption (`crypto.go`), `PlatformConfigs`, or the CPN topology.
- **CON-004**: Frontend MUST follow the project's existing React/Tailwind/i18n conventions; visual changes MUST go through the `frontend-design` skill.
- **CON-005**: Migration `014_*` MUST be additive and reversible.
- **CON-006**: No secret value MAY appear in logs, audit rows, or API responses, ever.

### 3.6 Guidelines

- **GUD-001**: Prefer the minimal diff that satisfies the acceptance criteria. Do not refactor adjacent code.
- **GUD-002**: Keep error messages actionable but free of secret material.
- **GUD-003**: Audit-log inserts SHOULD reuse the same `pgxpool` already wired through the config repository.
- **GUD-004**: Frontend error strings SHOULD be short and bilingual.

### 3.7 Patterns

- **PAT-001**: Snapshot-then-iterate for any callback fan-out under a mutex.
- **PAT-002**: Fail-loud-on-startup for misconfiguration, never silent fallback to insecure defaults.
- **PAT-003**: Mask secrets with a fixed prefix and at most last-4 of the value.

## 4. Interfaces & Data Contracts

### 4.1 Status endpoint (REQ-001)

Request:
```
GET /api/v1/admin/config/status
```

Response (200, public, no auth):
```json
{ "ready": true }
```

The response object MUST contain exactly one key: `ready`. No `missing_required`, no counts, no key names.

### 4.2 Mask truth table (REQ-107)

| Input value             | `len(value)` | `IsSecret` | Output                  |
|-------------------------|--------------|------------|-------------------------|
| `""`                    | 0            | true       | `""` (empty unchanged)  |
| `"abc"`                 | 3            | true       | `"********"`            |
| `"abcdefg"`             | 7            | true       | `"********"`            |
| `"abcdefgh"`            | 8            | true       | `"********efgh"`        |
| `"sk-or-v1-abcdef1234"` | 20           | true       | `"********1234"`        |
| `"sk-or-v1-abcdef1234"` | 20           | false      | `"sk-or-v1-abcdef1234"` |

### 4.3 Admin middleware truth table (REQ-101..103, REQ-108)

Configured `ADMIN_EMAILS = "owner@liwaisi.tech, ops@liwaisi.tech"`.

| Bearer user                            | Expected status |
|----------------------------------------|-----------------|
| `nil`                                  | 403             |
| `{Email: ""}`                          | 403             |
| `{Email: "intruder@example.com"}`      | 403             |
| `{Email: "owner@liwaisi.tech"}`        | 200             |
| `{Email: "OWNER@LIWAISI.TECH"}`        | 200             |
| `{Email: "  ops@liwaisi.tech  "}`      | 200             |
| `{Email: "ops@LIWAISI.tech"}`          | 200             |

Startup validation truth table (REQ-103):

| `APP_ENV` | `ADMIN_EMAILS`           | Behavior                          |
|-----------|--------------------------|-----------------------------------|
| `dev`     | unset                    | start with `[dev@localhost]`, WARN |
| `dev`     | `dev@localhost`          | start, WARN                        |
| `dev`     | `me@x.io`                | start                              |
| `prod`    | unset                    | **fatal**                          |
| `prod`    | `""`                     | **fatal**                          |
| `prod`    | `dev@localhost`          | **fatal**                          |
| `prod`    | `dev@localhost,me@x.io`  | **fatal** (any forbidden entry rejects the whole set) |
| `prod`    | `me@x.io,ops@x.io`       | start                              |

### 4.4 Audit log row (REQ-201..202)

```sql
INSERT INTO admin_audit_log (key, action, updated_by) VALUES ($1, $2, $3);
```

| Column      | Source                                       |
|-------------|----------------------------------------------|
| `key`       | path-value `key` from the admin handler      |
| `action`    | `'set'` or `'delete'`                        |
| `updated_by`| `auth.UserFromContext(r.Context()).Email`, or `""` |
| `ts`        | DB default `NOW()`                           |

### 4.5 Frontend admin API error (REQ-301)

```ts
export class AdminApiError extends Error {
  constructor(public readonly status: number, message: string) {
    super(message);
    this.name = 'AdminApiError';
  }
}
```

`AdminSecretsPanel.tsx` catch:
```ts
catch (e) {
  if (e instanceof AdminApiError) {
    if (e.status === 401) { auth.signOut(); return; }
    if (e.status === 403) { setError(t('errors.forbidden')); return; }
    if (e.status === 503) { setError(t('errors.unavailable')); return; }
  }
  setError(t('errors.generic'));
}
```

### 4.6 File anchors

| File                                                                                        | Area                          | REQs               |
|---------------------------------------------------------------------------------------------|-------------------------------|--------------------|
| `back/go-assistant/internal/driving/httpapi/handler_admin.go:104-120`                       | `HandleConfigStatus`          | REQ-001            |
| `back/go-assistant/internal/config/masterkey.go:16-43`                                      | `LoadOrGenerateMasterKey`     | REQ-002, REQ-003   |
| `back/go-assistant/internal/driving/httpapi/middleware_admin.go:10-21`                      | `AdminMiddleware`             | REQ-101, REQ-102   |
| `back/go-assistant/cmd/server/main.go`                                                      | startup wiring                | REQ-103            |
| `back/go-assistant/internal/config/provider.go:85-98`                                       | `Provider.Set`                | REQ-104, REQ-105   |
| `back/go-assistant/internal/config/provider.go:101-115`                                     | `Provider.Delete`             | REQ-104, REQ-105   |
| `back/go-assistant/internal/config/provider.go:133-135`                                     | `Provider.OnChange`           | REQ-106            |
| `back/go-assistant/internal/config/provider.go:139-175`                                     | `Provider.ListAll` / `maskSecret` | REQ-107        |
| `back/go-assistant/internal/driving/httpapi/middleware_admin_test.go` (new)                 | table-driven tests            | REQ-108            |
| `back/go-assistant/internal/config/provider_test.go`                                        | race tests                    | REQ-109            |
| `back/go-assistant/store/postgres/migrations/014_admin_audit_log.up.sql` (new)              | audit table                   | REQ-201            |
| `back/go-assistant/store/postgres/migrations/014_admin_audit_log.down.sql` (new)            | rollback                      | REQ-201            |
| `back/go-assistant/store/postgres/audit.go` (new)                                           | audit repo                    | REQ-202            |
| `back/go-assistant/internal/driving/httpapi/handler_admin.go:29-102`                        | wire audit calls              | REQ-202            |
| `docker-compose.yml`                                                                        | drop default                  | REQ-203            |
| `front/react-assistant/src/services/api.ts`                                                 | `AdminApiError`               | REQ-301            |
| `front/react-assistant/src/features/admin/AdminSecretsPanel.tsx:26-40`                      | error branching               | REQ-302, REQ-303   |
| `front/react-assistant/src/i18n/locales/{en,es}/admin.json`                                 | new keys                      | REQ-304            |

## 5. Acceptance Criteria

- **AC-001** *(REQ-001)*. Given any caller, When `GET /api/v1/admin/config/status` is invoked with a partially-configured platform, Then the response body is exactly `{"ready": false}` and contains no other top-level keys.
- **AC-002** *(REQ-001)*. Given a fully-configured platform, When the same endpoint is hit, Then the body is exactly `{"ready": true}`.
- **AC-003** *(REQ-002)*. Given the master key file exists with mode `0o644`, When the server starts, Then startup fails with an error mentioning the path and the offending mode, and no key bytes are returned.
- **AC-004** *(REQ-002)*. Given the master key file exists with mode `0o600`, When the server starts, Then startup proceeds and the key is loaded.
- **AC-005** *(REQ-003)*. Given the parent directory of the master key has mode `0o755`, When the server starts, Then startup fails loudly.
- **AC-006** *(REQ-101)*. Given `ADMIN_EMAILS=Owner@Liwaisi.Tech`, When a request arrives with `user.Email = "owner@liwaisi.tech"`, Then the middleware allows it (200).
- **AC-007** *(REQ-102)*. Given `ADMIN_EMAILS=a@x.io, b@x.io`, When a request arrives as `b@x.io`, Then 200; as `c@x.io`, Then 403.
- **AC-008** *(REQ-103)*. Given `APP_ENV=prod` and `ADMIN_EMAILS` unset, When the server starts, Then it exits non-zero with a fatal log line naming `ADMIN_EMAILS`.
- **AC-009** *(REQ-103)*. Given `APP_ENV=prod` and `ADMIN_EMAILS=dev@localhost,me@x.io`, When the server starts, Then it exits non-zero (forbidden entry rejects the whole set).
- **AC-010** *(REQ-104, REQ-105, REQ-106, REQ-109)*. Given the race test in `provider_test.go`, When `go test -race ./internal/config/...` is run, Then it passes with zero data races reported.
- **AC-011** *(REQ-105)*. Given two sequential `Set("k","v1")` then `Set("k","v2")` calls from the same goroutine, When OnChange callbacks have completed, Then the last value observed by every callback is `"v2"`.
- **AC-012** *(REQ-107)*. Given `ListAll` is called with a 20-char secret value, When inspected, Then `value` equals `"********" + last4` and never contains a prefix of the original.
- **AC-013** *(REQ-107)*. Given a 5-char secret value, When inspected, Then `value` equals `"********"`.
- **AC-014** *(REQ-108)*. Given the new middleware test file, When `go test ./internal/driving/httpapi/...` is run, Then every row in §4.3 is asserted and passes.
- **AC-015** *(REQ-201)*. Given migration `014` is applied, When `\d admin_audit_log` is inspected, Then the table exists with the columns and CHECK constraint defined in §3.3.
- **AC-016** *(REQ-202)*. Given an admin issues `PUT /api/v1/admin/config/openrouter_api_key` with a value, When the request returns 200, Then exactly one new row exists in `admin_audit_log` with `action='set'`, `key='openrouter_api_key'`, `updated_by` equal to the bearer email, and **no value column anywhere**.
- **AC-017** *(REQ-202)*. Given the audit insert fails (DB unreachable), When the admin issues a config change, Then the change still succeeds (200) and an ERROR log line is emitted.
- **AC-018** *(REQ-203)*. Given `ADMIN_EMAILS` is unset and the user runs `docker compose up`, When compose evaluates the env file, Then it exits with the required-var error and no container starts.
- **AC-019** *(REQ-301, REQ-302)*. Given `getAdminConfig` returns 401, When `AdminSecretsPanel` mounts, Then `AuthContext.signOut()` is invoked and the panel does not render the error state.
- **AC-020** *(REQ-303, REQ-304)*. Given the API returns 403, When the panel renders, Then the visible error string is `t('errors.forbidden')`, distinct from the 503 and generic strings.

## 6. Test Automation Strategy

- **Test Levels**
  - **Unit (Go)**: `provider_test.go` for snapshot/race/mask, `masterkey_test.go` for perm checks (use `t.TempDir()` and `os.Chmod`), `middleware_admin_test.go` for the auth truth table, `handler_admin_test.go` for the status-endpoint shape.
  - **Unit (TS)**: `AdminSecretsPanel.test.tsx` extended with mocked `AdminApiError` paths for 401/403/503/generic. Use the existing `i18n-test-utils`.
  - **Integration (Go)**: `handler_admin_test.go` round-trip that issues `PUT` then asserts a row in `admin_audit_log` via the test Postgres.
  - **Migration**: existing migration test harness applies `014` up, asserts schema, applies `014` down, asserts cleanup.
- **Frameworks**: Go standard `testing`; React Testing Library + Vitest as already configured.
- **Race requirement**: `go test -race ./internal/config/... ./internal/driving/httpapi/...` MUST be green. CI invocation MUST include `-race` for these packages.
- **Test data management**: No fixtures on disk. Postgres tests use the existing ephemeral database helper. Master-key perm tests use `t.TempDir()` exclusively.
- **CI/CD integration**: Add `-race` to the existing Go test step for the affected packages. Frontend tests run via the existing pnpm/vitest job.
- **Coverage**: `provider.go`, `masterkey.go`, `middleware_admin.go`, `handler_admin.go` MUST be ≥ 90% line coverage for the changed lines.
- **Performance**: Not applicable.

## 7. Rationale & Context

The expert-panel review of PR #91 found that the architecture is sound but the implementation cuts corners on three sensitive surfaces: the public health endpoint, the master-key load path, and the OnChange concurrency in `Provider`. None of these are theoretical:

1. **Status endpoint**. Listing the missing required keys to anonymous callers is free reconnaissance — it tells an attacker which provider you depend on, when you're misconfigured, and what to brute-force. The endpoint's job is "is the platform ready?" — a single boolean answers it.
2. **Master key**. The key file is the root of trust for every encrypted secret in the database. If the file ever ends up at `0644` (a bad volume mount, a sloppy `chmod -R`, an old backup restore), the service silently loads it and there is no signal until a breach. Cheap fix: `Stat` after `ReadFile`.
3. **OnChange concurrency**. The current code releases the lock before iterating callbacks and never locks `OnChange` registration. Two concurrent `Set` calls for the same key can fan out callbacks in an order that disagrees with the cache state, leaving the live LLM client built from the older API key. `go test -race` catches the registration race today; the ordering bug is harder to reproduce but ships the moment two admins click "Save" close together.
4. **Mask format**. Showing `value[:10]` for a 20-character secret leaks the high-entropy provider prefix. Last-4 (the Stripe convention) leaks nothing useful while still letting the admin recognize a key.
5. **Single-string admin email**. Case sensitivity locks legitimate admins out; single-admin makes rotation a redeploy. Both are paper cuts that compound during incidents.
6. **Audit log**. A secrets manager with no immutable record of who-changed-what is uninvestigatable. The table is 5 columns and writes are append-only — there is no excuse to ship without it.

The frontend error fix is small but real: today a 401 from token expiry renders as a generic red box instead of bouncing the user to login.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: PostgreSQL — hosts `platform_config` and the new `admin_audit_log` table.

### Infrastructure Dependencies
- **INF-001**: Existing `pgxpool` already wired by the config repository. No new pools.
- **INF-002**: Master key file path provided via `LIWAISI_MASTER_KEY_PATH` (unchanged).

### Technology Platform Dependencies
- **PLT-001**: Go toolchain as currently pinned.
- **PLT-002**: react-assistant frontend toolchain as currently pinned.

### Compliance Dependencies
- None.

## 9. Examples & Edge Cases

### 9.1 Corrected status handler

```go
func (h *Handlers) HandleConfigStatus(w http.ResponseWriter, r *http.Request) {
    ready := true
    if h.ConfigProvider != nil {
        ready = len(h.ConfigProvider.MissingRequired()) == 0
    }
    writeJSON(w, http.StatusOK, map[string]any{"ready": ready})
}
```

### 9.2 Master key perm check

```go
data, err := os.ReadFile(path)
if err == nil {
    info, statErr := os.Stat(path)
    if statErr != nil {
        return nil, fmt.Errorf("config: stat master key: %w", statErr)
    }
    if !info.Mode().IsRegular() {
        return nil, fmt.Errorf("config: master key at %s is not a regular file", path)
    }
    if perm := info.Mode().Perm(); perm&0o077 != 0 {
        return nil, fmt.Errorf("config: master key at %s has insecure mode %#o, want 0600", path, perm)
    }
    if len(data) != masterKeySize { /* unchanged */ }
    return data, nil
}
```

### 9.3 Snapshot-then-iterate OnChange

```go
func (p *Provider) Set(ctx context.Context, key, value, updatedBy string) error {
    def := DefByKey(key)
    if def == nil { return ErrNotFound }
    entry := &Entry{ /* ... */ }

    p.callMu.Lock()
    defer p.callMu.Unlock()

    if err := p.store.Set(ctx, entry); err != nil { return err }

    p.mu.Lock()
    p.cache[key] = entry
    callbacks := append([]func(string, string){}, p.onChange...)
    p.mu.Unlock()

    for _, fn := range callbacks { fn(key, value) }
    return nil
}
```

### 9.4 Mask function

```go
func maskSecret(value string) string {
    if value == "" { return "" }
    if len(value) < 8 { return "********" }
    return "********" + value[len(value)-4:]
}
```

### 9.5 Multi-admin parser

```go
func parseAdminEmails(raw string) []string {
    out := make([]string, 0)
    for _, p := range strings.Split(raw, ",") {
        p = strings.TrimSpace(p)
        if p != "" { out = append(out, strings.ToLower(p)) }
    }
    return out
}
```

### 9.6 Edge cases

- **EC-001**: Master key file is a symlink → `info.Mode().IsRegular()` is false → reject. Prevents symlink-swap attacks on the key path.
- **EC-002**: `ADMIN_EMAILS=" , , "` → parser returns empty slice → REQ-103 fatal in non-dev.
- **EC-003**: Concurrent `OnChange` registration during a `Set` storm → REQ-106 lock + REQ-104 snapshot prevents both the race and the iteration over a mutated slice.
- **EC-004**: Audit insert fails due to a DB hiccup → 200 is still returned, ERROR log emitted, change is durable in `platform_config`. Operator reconciles via `platform_config.updated_at` if needed.
- **EC-005**: `ListAll` called with `IsSecret=true` and `value=""` (no env, no db, empty default) → mask returns `""` so the UI can render "not set" rather than a fake `********`.
- **EC-006**: Frontend receives a non-`AdminApiError` (e.g. network failure) → falls through to `errors.generic`.

## 10. Validation Criteria

1. AC-001..AC-020 all pass.
2. `go vet ./...` clean.
3. `go test ./...` green.
4. `go test -race ./internal/config/... ./internal/driving/httpapi/...` green.
5. Frontend `pnpm test` green, including new `AdminSecretsPanel` 401/403/503 cases.
6. Migration `014_admin_audit_log` applies up and down cleanly against a fresh database.
7. `docker compose config` rejects an environment with `ADMIN_EMAILS` unset.
8. Diff is minimal: no edits outside the file anchors in §4.6 unless strictly required by a listed REQ.
9. Code review sign-off by `golang-pro` (backend) and `vercel-react-best-practices` + `frontend-design` (frontend).

## 11. Related Specifications / Further Reading

- `spec/spec-process-bugfix-classifier-clarify-bypass.md` — sibling bugfix spec, same format.
- PR liwaisi-tech/liwaisi_assistant#91 — the PR being hardened.
- `spec/spec-architecture-a2a-a2ui-protocol-integration.md` — broader architecture context.
- OWASP ASVS v4 §V2 (Authentication) and §V8 (Data Protection) — informative.
