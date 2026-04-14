---
title: Bug Fix — Ghost Session After Backend Restart (Rehydration + Frontend Recovery)
version: 1.0
date_created: 2026-04-13
last_updated: 2026-04-13
owner: liwaisi
tags: [process, bugfix, session, persistence, sse, go-assistant, react-assistant]
---

# Introduction

After the backend process restarts (deploy, crash, machine reboot), the in-memory `sessions` map inside `SessionService` is wiped while Postgres still holds the session rows. The frontend reads the persisted list via `GET /sessions`, restores a saved active session id from `localStorage`, and then every session-scoped call (`GET /sessions/{id}/events`, `POST /sessions/{id}/messages`, etc.) returns `{"error":"session not found"}`. Logging out and logging back in does not recover because the frontend re-picks an old session id from the persisted list. This specification defines the bug, its scope, and the exact backend + frontend changes required to make session lookups resilient to restarts and to degrade gracefully when a session is truly missing.

## 1. Purpose & Scope

**Purpose.** Make session-scoped endpoints in `go-assistant` resilient to backend restarts by rehydrating sessions from persistence on miss, and teach `react-assistant` to detect and recover from a session-not-found response without requiring a manual logout.

**In scope.**
- `back/go-assistant/internal/app/session_service.go` lookup paths (`GetSession`, `StreamChannel`, `SendStreamChunk`, `ResolveHITL`, message send path).
- Rehydration from `persist.Sessions` (Postgres) and optional Redis warm-up.
- Concurrency control for rehydration (single-flight, no thundering herd).
- `front/react-assistant/src/hooks/useChatList.ts` startup flow.
- `front/react-assistant/src/hooks/useSSE.ts` error handling and reconnection policy.
- `front/react-assistant/src/services/api.ts` session-not-found mapping to a typed error.
- `front/react-assistant/src/contexts/AuthContext.tsx` logout cleanup.
- A minimal, non-intrusive UI affordance that signals "reconnecting / session restored".

**Out of scope.**
- Changing the CPN execution model or its state machine.
- Replacing Postgres/Redis or redesigning persistence schemas.
- Migrating session auth from query-string token on SSE to cookies or headers.
- Introducing server-side session revocation lists or a backend `/logout` endpoint (track separately).
- UI redesign beyond the minimum surface required to expose recovery state.

**Audience.** Implementing agents: `golang-pro` (backend), `vercel-react-best-practices` + `frontend-design` (frontend), plus reviewers.

## 2. Definitions

- **SessionService**: `internal/app/session_service.go` — owns in-memory `sessions` map, `states` map, and `persist` handle.
- **In-memory session**: A live `*cpn.Session` value in the `sessions` map, required by CPN execution, stream channel delivery, and HITL resolution.
- **Persisted session**: A row in the Postgres `sessions` table (via `persist.Sessions.Get`). Holds identity, channel, created_at, title, soft-delete flag, and message history but not the live stream channel.
- **Rehydration**: Reconstructing an in-memory session from the persisted row (and message history) on a lookup miss, then inserting it into `sessions` and `states` so subsequent operations succeed.
- **Ghost session**: A session id that exists in persistence and is returned by `ListSessions` / restored from `localStorage`, but is absent from the in-memory map.
- **Single-flight**: Concurrency pattern where only one goroutine performs rehydration for a given session id while others wait for its result.
- **SSE**: Server-Sent Events endpoint `GET /api/v1/sessions/{id}/events?token=...`.

## 3. Requirements, Constraints & Guidelines

### 3.1 Backend requirements (`golang-pro`)

- **REQ-001**: `SessionService.GetSession` MUST, on in-memory miss, attempt rehydration from `s.persist.Sessions`. On rehydration success it MUST return a `SessionInfo` identical in shape to the hot path. On rehydration failure (row missing, soft-deleted, ownership mismatch, persistence unavailable) it MUST return `ErrSessionNotFound` wrapped with the session id.
- **REQ-002**: `SessionService.StreamChannel` MUST follow the same rehydration-on-miss behavior as `GetSession`. After rehydration the returned channel MUST be the live stream channel of the rehydrated `*cpn.Session`.
- **REQ-003**: `SessionService.SendStreamChunk` and `SessionService.ResolveHITL` MUST either (a) also rehydrate-on-miss, or (b) explicitly document and return `ErrSessionNotFound` because they operate on transient execution state that cannot be reconstructed. Decision: REQ-003a — rehydrate; however, if the rehydrated session has no in-flight execution, these paths MUST return a new sentinel `ErrSessionInactive` so callers can distinguish "ghost" from "idle".
- **REQ-004**: Rehydration MUST be protected by a single-flight mechanism keyed by session id so that N concurrent misses for the same id trigger exactly one persistence round-trip. Use `golang.org/x/sync/singleflight` or an equivalent per-id mutex map.
- **REQ-005**: Rehydration MUST verify that the persisted session is not soft-deleted. A soft-deleted session MUST return `ErrSessionNotFound` and MUST NOT be inserted into the in-memory map.
- **REQ-006**: Rehydration MUST replay (or lazily attach) message history so that `SessionInfo.Messages` is not silently empty after a restart. If full replay is expensive, messages MAY be fetched on demand from `persist.Sessions.Messages(ctx, id)` rather than eagerly loaded.
- **REQ-007**: When persistence is not configured (`s.persist == nil || s.persist.Sessions == nil`), rehydration MUST be a no-op and the existing in-memory-only behavior MUST be preserved (backwards compatible with ephemeral deployments).
- **REQ-008**: All rehydration paths MUST use `context.Context` derived from the caller's request context. Rehydration MUST NOT launch detached goroutines that outlive the request.
- **REQ-009**: Locking discipline MUST NOT hold `s.mu` across persistence I/O. Read the map under `RLock`, release, perform I/O, then take `Lock` to insert the rehydrated entry — and re-check for a concurrently inserted entry before overwriting (double-checked locking).
- **REQ-010**: Structured logging MUST record every rehydration event at `info` level with fields `{session_id, user_id, source: "postgres"|"redis", duration_ms}`; misses that fall through to `ErrSessionNotFound` MUST log at `warn` with `{session_id, reason}`.
- **REQ-011**: A Prometheus (or existing metrics system) counter `session_rehydration_total{result}` MUST be incremented for each rehydration attempt, with `result ∈ {hit, miss, soft_deleted, error}`.
- **REQ-012**: No additional authentication or authorization change is introduced by this fix; ownership checks that already exist in the handler layer MUST continue to apply to the rehydrated session.

### 3.2 Frontend requirements (`vercel-react-best-practices`, `frontend-design`)

- **REQ-101**: `src/services/api.ts` MUST map the backend payload `{"error":"session not found"}` (case-insensitive) to a typed `SessionNotFoundError` distinct from `ApiError`. The HTTP status code path (404/410) MUST also trip this mapping.
- **REQ-102**: `useChatList` MUST, on startup, treat a saved `localStorage.liwaisi_active_session_{userId}` as a *hint*, not a commitment. If the subsequent `GET /sessions/{id}` or first SSE subscription returns `SessionNotFoundError`, the hook MUST (a) drop the saved id, (b) remove the `localStorage` key, (c) pick the next viable session from the list, and (d) if none remain, call `createSession` exactly once.
- **REQ-103**: When the chat list from `GET /sessions` is non-empty but every candidate fails with `SessionNotFoundError` (e.g., after a catastrophic backend wipe), `useChatList` MUST fall back to creating a fresh session and MUST NOT loop.
- **REQ-104**: `useSSE` MUST treat a `SessionNotFoundError` surface from the EventSource error channel as non-retriable for the current session id. It MUST close the EventSource, emit a typed error to its caller, and MUST NOT enter an exponential-backoff reconnect loop against the dead id. Retrying against a *new* id is the caller's responsibility.
- **REQ-105**: `useSSE` reconnect policy for transient errors (network, 5xx) MUST use exponential backoff with jitter, capped at a reasonable ceiling (e.g., 30s), and MUST stop when the component unmounts or the session id changes. No new polling loops.
- **REQ-106**: `AuthContext.logout` MUST clear `localStorage.liwaisi_active_session_{userId}` using the same `userId` key used by `useChatList` (currently `user.email`-keyed vs `userId`-keyed — unify on a single identifier; see REQ-110).
- **REQ-107**: On login, the frontend MUST NOT assume that a previously active session id is still valid on the backend. The first session-scoped request MUST be treated as a probe; recovery path from REQ-102 applies on failure.
- **REQ-108**: React-specific: recovery logic MUST live in the existing `useChatList` and `useSSE` hooks. No new top-level providers. State updates MUST use the existing reducer dispatches (`SET_ACTIVE`, `ADD_CHAT`, `SET_ERROR`) and MUST NOT introduce duplicate sources of truth for the active session.
- **REQ-109**: Effects that react to a session id change MUST have stable dependencies. Do not inline object/array literals in `useEffect` deps. Memoize callbacks passed into `useSSE` with `useCallback` to avoid tearing down the EventSource on every render.
- **REQ-110**: The `localStorage` key for the active session MUST be unified under a single identifier. Decision: use `user.email` everywhere (to match the logout cleanup already present). Rename or migrate the `useChatList` key from `userId`-based to `email`-based in a single atomic change; add a one-time migration that reads the old key, writes to the new, and deletes the old.
- **REQ-111**: The UI MUST present a non-blocking, low-visual-weight indicator when a session has just been auto-recovered (e.g., a toast or inline banner: "Sesión reanudada en un nuevo chat"). It MUST NOT interrupt input focus and MUST auto-dismiss within 4s. Respect existing i18n keys — add under `chat` namespace.
- **REQ-112**: The offline dot in `ChatHeader` MUST distinguish three states: `connected` (green), `reconnecting` (amber, animated), `disconnected-terminal` (red). Terminal state is only used when `SessionNotFoundError` has fired and recovery has also failed.

### 3.3 Constraints

- **CON-001**: No schema migrations. All changes MUST work against the current Postgres `sessions`/`messages` tables.
- **CON-002**: Rehydration MUST NOT change the public HTTP contract. Request and response shapes, status codes, and error payloads already emitted on success/failure MUST be unchanged.
- **CON-003**: The SSE token-in-query-string mechanism is preserved. No move to cookies or Authorization headers in this change.
- **CON-004**: Go module additions MUST be limited to `golang.org/x/sync` if not already present; no other new dependencies on the backend. No new frontend dependencies.
- **CON-005**: The fix MUST be deployable as a single PR and reversible via revert without data migration.

### 3.4 Guidelines

- **GUD-001**: Prefer early-return error handling in rehydration paths. Wrap errors with `fmt.Errorf("rehydrate session %s: %w", id, err)`.
- **GUD-002**: Keep the rehydration helper internal (lowercase `rehydrate`) and single-purpose. Do not merge it with `CreateSession`.
- **GUD-003**: On the frontend, prefer typed errors + narrow catch sites over broad try/catch. Keep reducer actions minimal and expressive.
- **GUD-004**: Log rehydration as an operational signal; metrics as a trending signal. Neither should leak to users.

### 3.5 Patterns

- **PAT-001** (Go, double-checked locking):
  ```go
  s.mu.RLock()
  sess, ok := s.sessions[id]
  s.mu.RUnlock()
  if ok { return sess, nil }

  v, err, _ := s.sf.Do(id, func() (any, error) { return s.loadFromPersist(ctx, id) })
  if err != nil { return nil, err }
  loaded := v.(*cpn.Session)

  s.mu.Lock()
  if existing, ok := s.sessions[id]; ok {
      s.mu.Unlock()
      return existing, nil
  }
  s.sessions[id] = loaded
  s.states[id] = newSessionState()
  s.mu.Unlock()
  return loaded, nil
  ```
- **PAT-002** (React, typed error recovery):
  ```ts
  try {
    await ensureSession(activeId);
  } catch (err) {
    if (err instanceof SessionNotFoundError) {
      clearActiveSessionStorage(userEmail);
      await recoverOrCreateSession();
      return;
    }
    throw err;
  }
  ```

## 4. Interfaces & Data Contracts

### 4.1 Backend — internal API surface (unchanged externally)

| Symbol | File | Change |
|---|---|---|
| `SessionService.GetSession(id string) (*SessionInfo, error)` | `internal/app/session_service.go` | Add rehydration-on-miss; same signature. |
| `SessionService.StreamChannel(id string) (<-chan cpn.StreamChunk, error)` | same | Add rehydration-on-miss; same signature. |
| `SessionService.SendStreamChunk(id string, chunk cpn.StreamChunk) error` | same | Rehydrate-on-miss; may return new `ErrSessionInactive`. |
| `SessionService.ResolveHITL(ctx, id, transitionID, resp) error` | same | Rehydrate-on-miss; may return `ErrSessionInactive`. |
| `SessionService.rehydrate(ctx, id) (*cpn.Session, error)` | same (new, unexported) | New helper. |
| `ErrSessionInactive` | `internal/app/errors.go` | New sentinel. |

### 4.2 Backend — HTTP error payloads

| Condition | Status | Body |
|---|---|---|
| Session missing in persistence (or soft-deleted) | 404 | `{"error":"session not found"}` (unchanged) |
| Session rehydrated but no live execution for stream-only op | 409 | `{"error":"session inactive"}` (new, only on `SendStreamChunk`/`ResolveHITL` paths) |
| Persistence unavailable during rehydration | 503 | `{"error":"persistence unavailable"}` (new; existing behavior was 500) |

### 4.3 Frontend — typed errors

```ts
export class SessionNotFoundError extends Error {
  readonly name = 'SessionNotFoundError';
  constructor(public sessionId: string) {
    super(`session not found: ${sessionId}`);
  }
}

export class SessionInactiveError extends Error {
  readonly name = 'SessionInactiveError';
  constructor(public sessionId: string) {
    super(`session inactive: ${sessionId}`);
  }
}
```

### 4.4 Frontend — storage keys

| Key | Value | Set by | Cleared by |
|---|---|---|---|
| `liwaisi_token` | Google JWT | `AuthContext.handleCredentialResponse` | `AuthContext.logout`, session-not-found recovery does NOT clear this |
| `liwaisi_user` | `User` JSON | `AuthContext` | `AuthContext.logout` |
| `liwaisi_active_session_{email}` | session id string | `useChatList` | `AuthContext.logout`, `useChatList` recovery on `SessionNotFoundError` |

### 4.5 Metrics

| Metric | Type | Labels |
|---|---|---|
| `session_rehydration_total` | counter | `result ∈ {hit, miss, soft_deleted, error}` |
| `session_rehydration_duration_seconds` | histogram | `source ∈ {postgres, redis}` |

## 5. Acceptance Criteria

- **AC-001**: Given a session created before a backend restart and persisted in Postgres, When the backend restarts and the frontend calls `GET /api/v1/sessions/{id}`, Then the response is 200 with the session payload and `session_rehydration_total{result="hit"}` increments by 1.
- **AC-002**: Given a session id that does not exist in Postgres, When any session-scoped endpoint is called, Then the response is 404 `{"error":"session not found"}` and `session_rehydration_total{result="miss"}` increments by 1.
- **AC-003**: Given a soft-deleted session, When any session-scoped endpoint is called, Then the response is 404 and the session is NOT inserted into the in-memory map.
- **AC-004**: Given 50 concurrent requests for the same unknown-in-memory session id, When they arrive simultaneously after a restart, Then exactly one call hits `persist.Sessions.Get` (verified via a test-double counter) and all 50 requests either succeed or fail consistently.
- **AC-005**: Given persistence is not configured, When `GetSession` misses in memory, Then behavior is identical to the pre-change code path (returns `ErrSessionNotFound`).
- **AC-006**: Given the frontend has a stale `liwaisi_active_session_{email}` pointing to a ghost session AND no backend rehydration (e.g., session soft-deleted), When the app loads, Then `useChatList` clears the stale key, selects another chat or creates a new one, and no infinite loop occurs (assert max one `createSession` call per startup).
- **AC-007**: Given a live session whose EventSource receives a `SessionNotFoundError` mid-stream, When the error is emitted, Then `useSSE` closes the connection, does not reconnect to the same id, and surfaces the typed error to the caller.
- **AC-008**: Given auto-recovery succeeds, Then the UI shows a non-blocking "session resumed" notice that auto-dismisses ≤4s and input focus is preserved.
- **AC-009**: Given the user is in the middle of typing when recovery fires, Then the draft text in the composer is NOT lost.
- **AC-010**: Given the offline indicator shows amber during reconnection, When connection is restored, Then the indicator returns to green without a manual refresh.

## 6. Test Automation Strategy

- **Test Levels**: Unit (both), integration (backend), component (frontend), end-to-end (one golden scenario).
- **Backend frameworks**: Standard `testing`, `github.com/stretchr/testify` if already in use; in-memory `persist.Sessions` fake for rehydration tests; `httptest` for handler-level integration; goroutine race detector (`go test -race`) required green.
- **Frontend frameworks**: Vitest + React Testing Library for hooks/components; MSW for API mocking; Playwright (if already configured) for the one end-to-end scenario.
- **Test Data Management**: Backend tests build sessions via a factory helper; frontend tests use fixtures under `src/__tests__/fixtures/`. No shared database state between tests; each test gets an isolated fake persistence layer.
- **CI/CD Integration**: Tests MUST run in the existing GitHub Actions pipeline on every PR. Race detector on backend. Type-check + eslint on frontend.
- **Coverage Requirements**: New/changed backend files ≥ 85% line coverage. New/changed frontend hooks ≥ 80%. No coverage regression overall.
- **Performance Testing**: Benchmark `BenchmarkGetSession_Rehydrate` to confirm rehydration overhead is < 5ms p50 against the in-memory fake and that the singleflight path scales to 1k concurrent misses without lock contention.

## 7. Rationale & Context

The current design keeps authoritative live state in memory and uses Postgres only as a best-effort archival store. That split is fine while the process is alive. The split becomes load-bearing and visible the moment the process restarts: `ListSessions` reads Postgres, `GetSession`/`StreamChannel` read memory, and the two disagree on the id space. The frontend, reasonably, treats `ListSessions` as the source of truth and pins an active session id accordingly — so every session-scoped call lands on a dead id and fails with "session not found". The user's experience is "logout doesn't help", because logout only clears client state; on re-login the list still returns the ghost ids.

Rehydration on miss collapses the split: any id that exists in persistence becomes usable again after a restart. Single-flight prevents a restart storm from fanning out into N identical Postgres round-trips. The distinction between "not in persistence" (→ 404) and "in persistence but no live execution" (→ new `ErrSessionInactive`) gives the frontend enough information to recover gracefully — resume the session for read/list flows, and prompt a new run for execute-only flows.

On the frontend, the recovery policy must be strictly bounded (REQ-103, REQ-104) because the failure mode "every id in the list is a ghost" used to be impossible and silently creating sessions in a loop would be worse than the original bug. Unifying the `localStorage` key (REQ-110) removes a latent footgun that already exists in the codebase (`useChatList` uses `userId`, `AuthContext.logout` uses `email`) and is what let the bug persist across logout.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: Postgres (existing) — authoritative session store consulted during rehydration.
- **EXT-002**: Redis (existing, optional) — warm cache for recently active sessions; rehydration MAY prefer Redis then fall back to Postgres.

### Third-Party Services
- **SVC-001**: Google Identity Services — unchanged; still the JWT issuer for `liwaisi_token`.

### Infrastructure Dependencies
- **INF-001**: No new infrastructure. Existing Postgres connection pool and Redis client are reused.

### Data Dependencies
- **DAT-001**: `sessions` table (existing columns: `id`, `user_id`, `channel`, `created_at`, `title`, `deleted_at`).
- **DAT-002**: `messages` table (existing) — consulted lazily by `SessionInfo.Messages` after rehydration if eager replay is deferred.

### Technology Platform Dependencies
- **PLT-001**: Go ≥ 1.22 (existing). Backend code MAY use `golang.org/x/sync/singleflight`.
- **PLT-002**: React ≥ 18 (existing). Frontend code MUST NOT introduce Suspense-breaking patterns; respect the existing data-fetching model (hook + reducer).

### Compliance Dependencies
- **COM-001**: None. No PII flows change. Logging at `info` does not include message content.

## 9. Examples & Edge Cases

### 9.1 Happy-path rehydration (backend)

```go
// GetSession after restart: id is in Postgres, not in memory.
info, err := svc.GetSession(ctx, sessionID)
// → err == nil; svc.sessions[sessionID] now populated.
```

### 9.2 Concurrent miss (backend)

```go
var wg sync.WaitGroup
for i := 0; i < 100; i++ {
    wg.Add(1)
    go func() { defer wg.Done(); _, _ = svc.GetSession(ctx, sessionID) }()
}
wg.Wait()
// Exactly one persistence load fired; 100 goroutines saw the same *cpn.Session.
```

### 9.3 Soft-deleted session (backend)

```go
// persist.Sessions.Get returns row with deleted_at != nil.
_, err := svc.GetSession(ctx, sessionID)
// → errors.Is(err, ErrSessionNotFound) == true
// → svc.sessions[sessionID] is absent (no ghost insertion).
```

### 9.4 Ghost session on frontend startup

```ts
// localStorage.liwaisi_active_session_alice@example.com = "sess_old"
// Backend wiped; "sess_old" is gone from Postgres too.
await init();
// → useChatList drops the key, picks res.items[0] if any, else createSession().
// → Exactly one createSession() call even if the list shrinks to zero during recovery.
```

### 9.5 Mid-stream SSE failure

```ts
// EventSource for sess_live starts, then backend marks sess_live soft-deleted.
// Next event arrives as "session not found".
// → useSSE closes the EventSource, emits SessionNotFoundError, does NOT reconnect.
// → useChat triggers recovery per REQ-102.
```

### 9.6 Persistence unavailable

```go
// persist.Sessions.Get returns context.DeadlineExceeded or driver.ErrBadConn.
_, err := svc.GetSession(ctx, sessionID)
// → HTTP layer maps to 503 "persistence unavailable".
// → Metric: session_rehydration_total{result="error"} += 1
```

## 10. Validation Criteria

- Backend unit tests for `rehydrate` cover: hit, miss, soft-deleted, persistence error, nil persistence, concurrent single-flight.
- Handler-level integration tests assert the HTTP status + body table in §4.2 for every session-scoped endpoint.
- `go test -race ./internal/app/...` passes.
- Frontend unit tests for `useChatList`, `useSSE`, and the `api.ts` error-mapping layer cover: stale-key recovery, zero-list recovery, mid-stream SSE failure, transient-error backoff.
- One Playwright scenario: "user restores a session after a simulated backend restart" goes green.
- Manual smoke test: reproduce the original reported scenario (3-day shutdown → home loads → first message sends successfully without logout/re-login).
- Observability: `session_rehydration_total{result="hit"}` is non-zero in staging after deploy.
- No regression in existing spec suites: `spec-design-sse-streaming-bugfixes.md`, `spec-architecture-google-oauth-login.md`, `spec-design-multi-chat-conversation-forking.md`.

## 11. Related Specifications / Further Reading

- `spec/spec-design-sse-streaming-bugfixes.md`
- `spec/spec-architecture-google-oauth-login.md`
- `spec/spec-design-multi-chat-conversation-forking.md`
- `spec/spec-design-user-preferences-settings-page.md`
- Go stdlib: `sync.RWMutex` correctness patterns; `golang.org/x/sync/singleflight`.
- Vercel React best practices: stable effect dependencies, typed errors at the data layer, avoid derived-state duplication.
- EventSource MDN reference and SSE reconnection semantics.
