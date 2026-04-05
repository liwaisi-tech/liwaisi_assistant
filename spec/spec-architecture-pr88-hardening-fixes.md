---
title: "PR #88 Hardening: Concurrency, Safety, Cost Tracking & Frontend Performance Fixes"
version: 1.0
date_created: 2026-04-05
owner: liwaisi-tech
tags: [architecture, concurrency, security, performance, hardening, pr-review, frontend, backend]
---

# Introduction

This specification addresses all findings from the expert panel code review of PR #88 (`feat: CPN tools engine, agent personality (brae), MCP standard adaptation`). The review identified 5 P1 (critical), 7 P2 (medium), and 4 P3 (low) findings across the Go backend CPN engine, HTTP API, persistence layer, infrastructure configuration, and React frontend.

This spec defines the exact fixes required, grouped by subsystem and expert panel responsibility, with acceptance criteria, test strategy, and implementation order that respects dependency chains.

## 1. Purpose & Scope

**Purpose**: Harden PR #88 before merge to `main` by resolving all P0/P1 findings (must-fix) and critical P2 findings (should-fix) identified during review. P3 findings are tracked but not blocking.

**Scope**:
- **Backend Go** (`cpn/`, `cpn/tools/`, `cpn/persist/`, `internal/driving/httpapi/`, `cmd/server/`): Concurrency fixes, error handling, cost propagation, type safety, rate limiting
- **Frontend React** (`front/react-assistant/src/`): State management refactor, memoization, list virtualization foundations
- **Infrastructure** (`docker-compose.yml`, `nginx.conf`, migrations): Network mode, CSP, missing constraints
- **Out of scope**: New features, UI redesign, new tools. This is strictly a hardening pass.

**Audience**: Backend Go engineers, frontend React engineers, DevOps engineers, and AI agents implementing these fixes.

**Assumptions**:
- The existing CPN `consume-before-launch` pattern (PAT-001) is correct and not changed
- The `dispatch()` signature change to `(float64, error)` is already merged in the PR branch
- Frontend uses Vite + React 19 + Tailwind CSS 4 (no Next.js, no SSR)
- All fixes are additive to the existing PR branch `fix/fix_persist_cpn_execution`

## 2. Definitions

| Term | Definition |
|------|-----------|
| **P1** | Critical finding: security vulnerability, data loss risk, race condition, production crash. Must fix before merge |
| **P2** | Medium finding: bad practice, code smell, missing safety check. Should fix before merge |
| **P3** | Low finding: style, optimization, future improvement. Track but not blocking |
| **Peek race** | Data race when multiple goroutines call `Place.Peek()` while another goroutine calls `Place.Deposit()` on the same place |
| **Cost propagation** | Threading LLM cost (USD) from inner functions (`callCorrectionLLM`, `fireLLMDirect`) back to the parent `dispatch()` return value |
| **HITL channel close** | Receiving from a closed Go channel returns the zero value; must use two-value receive to detect closure |
| **State explosion** | React anti-pattern where a single component has too many `useState` calls, causing cascading re-renders |

## 3. Requirements, Constraints & Guidelines

### 3.1 Backend — Concurrency Fixes (P1)

- **FIX-001**: Output place `Peek` race in `transition_completed` event. The fire goroutine in `executor.go` MUST NOT call `Place.Peek()` on shared output places after `dispatch()` returns, because multiple fire goroutines may share output places. Instead, `dispatch()` MUST return output token snapshots alongside `(costUSD float64, err error)`, changing the signature to `([]TokenSnapshot, float64, error)`. Each `fire*` function MUST build snapshots from its own freshly-created tokens BEFORE depositing them into places.

- **FIX-002**: Silent error swallowing in SubNet token deposit (`subnet.go:144`). When `p.Deposit(&tok)` fails, the error MUST be collected. If ANY deposit fails, `fireSubNet` MUST emit an `EventSubNetFailed` event with the deposit error details AND return the error to the parent. The `_ = p.Deposit(&tok)` pattern MUST be replaced with explicit error handling.

- **FIX-003**: HITL channel close protection (`fire_llm.go:333`). All receives from `t.HITLConfig.Channel` MUST use the two-value receive pattern:
  ```go
  case response, ok := <-t.HITLConfig.Channel:
      if !ok {
          c.setState(StateRunning)
          return "", loopCost, fmt.Errorf("transition %s: HITL channel closed unexpectedly", t.ID)
      }
  ```
  This applies to `fire_llm.go` (tool HITL gate) and `hitl.go` (standalone HITL fire). Both locations MUST be updated.

### 3.2 Backend — Error Handling & Type Safety (P1)

- **FIX-004**: Silent fallback in `makeGetIdentity` (`personality_tools.go:178-184`). The function MUST distinguish between "user not found" (fallback to default) and "repository error" (propagate error):
  ```go
  p, err := loadPersonality(ctx, deps, userID)
  if err != nil {
      if errors.Is(err, persist.ErrUserNotFound) || errors.Is(err, persist.ErrSessionNotFound) {
          p = cpn.DefaultPersonality()
      } else {
          return cpn.Token{}, fmt.Errorf("get_identity: repo error: %w", err)
      }
  }
  ```

- **FIX-005**: Unchecked type assertion on `userID` (`personality_tools.go:179`). ALL personality tool executors that extract `userID` from `in.Payload` MUST validate the type assertion:
  ```go
  userID, ok := in.Payload.(string)
  if !ok || userID == "" {
      return cpn.Token{}, fmt.Errorf("get_identity: expected non-empty string user_id, got %T", in.Payload)
  }
  ```
  This applies to: `makeGetIdentity`, `makeSetPrinciple`, `makeResetPersonality`, `makeGetTensions`, `makeSetHierarchy`.

### 3.3 Backend — Cost Propagation (P1)

- **FIX-006**: Incomplete cost tracking through correction and revision LLM calls. The following functions MUST return cost alongside their existing return values:
  - `callCorrectionLLM()` in `fire_validate.go` MUST return `(string, float64, error)` where the float64 is `LLMResponse.CostUSD`
  - `fireValidate()` MUST accumulate correction costs across retry iterations and include them in its returned cost
  - `fireLLMDirect()` in `hitl.go` MUST return `(string, float64, error)` and `fireHITLWithRevision()` MUST accumulate revision costs
  - All callers of these functions MUST thread cost through to `dispatch()` return

### 3.4 Backend — Guard Function Robustness (P2)

- **FIX-007**: Guard functions in `topologies.go` MUST parse the classifier JSON instead of string-matching:
  ```go
  func guardPlanTask(tokens []*cpn.Token) bool {
      for _, tok := range tokens {
          if s, ok := tok.Payload.(string); ok {
              var result struct{ Intent string `json:"intent"` }
              if err := json.Unmarshal([]byte(s), &result); err == nil {
                  return strings.EqualFold(result.Intent, "task")
              }
          }
      }
      return false
  }
  ```
  `guardDirectConversation` MUST be the inverse. Both MUST handle malformed JSON gracefully (default to `false` for task, `true` for conversation).

### 3.5 Backend — Rate Limiting (P2)

- **FIX-008**: Add per-user rate limiting middleware to the HTTP server. Requirements:
  - SSE connections: max 3 concurrent per user (identified by `auth.AuthenticatedUser.Sub`)
  - SendMessage: max 10 requests per minute per session
  - All other API endpoints: max 60 requests per minute per user
  - Dev-mode (no auth): rate limit by IP address
  - Use a token-bucket or sliding-window algorithm
  - Return `429 Too Many Requests` with `Retry-After` header when exceeded

### 3.6 Backend — Migration Data Integrity (P2)

- **FIX-009**: Add foreign key constraint on `forked_from_session_id`. Create migration `011_add_fork_fk.up.sql`:
  ```sql
  ALTER TABLE sessions
    ADD CONSTRAINT fk_sessions_forked_from
    FOREIGN KEY (forked_from_session_id)
    REFERENCES sessions(id)
    ON DELETE SET NULL;
  ```
  Down migration: `ALTER TABLE sessions DROP CONSTRAINT IF EXISTS fk_sessions_forked_from;`

### 3.7 Frontend — State Management Refactor (P2)

- **FIX-010**: Refactor `DesktopLayout.tsx` to reduce state explosion. Requirements:
  - Extract session-related state into a `useSessionManager` custom hook (activeSessionId, sessions, isRunning, pendingHITL, sseCallbacks)
  - Extract monitor-related state into a `useMonitorManager` custom hook (monitorSessionId, firedTransitions, monitorEvents, executionRuns)
  - Extract panel-related state into a `usePanelManager` custom hook (panelContent, panelOpen, selectedFlowHash)
  - Each hook MUST use `useReducer` internally instead of multiple `useState` calls
  - The main `DesktopLayout` component MUST have at most 3 hook calls for state (one per manager) plus standard hooks (useAuth, useCallback, useMemo)
  - Follow `rerender-split-combined-hooks`: split hooks with independent dependencies
  - Follow `rerender-derived-state-no-effect`: derive state during render, not in effects

### 3.8 Frontend — Memoization & Performance (P2/P3)

- **FIX-011**: Wrap list-rendered components in `React.memo`:
  - `ChatListItem` in `ChatSidebar.tsx`
  - `MessageBubble` in `MessageBubble.tsx`
  - `PlaceNode` in `PlaceNode.tsx`
  - `TransitionNode` in `TransitionNode.tsx`
  - `ExecutionList` items (the clickable run items)
  - `TokenCard` in `TransitionInspector.tsx`
  - Each MUST have an appropriate custom comparison function where props include objects or callbacks
  - Follow `rerender-memo`: extract expensive work into memoized components
  - Follow `rerender-memo-with-default-value`: hoist default non-primitive props to module-level constants

- **FIX-012**: Stabilize callbacks passed to memoized children:
  - Use `rerender-functional-setstate` pattern for all setState callbacks in DesktopLayout hooks
  - Hoist static arrays/objects (SUGGESTIONS in MessageList, COLOR_MAP in PlaceNode, KIND_ICONS in TransitionNode) to module scope
  - Follow `rerender-use-ref-transient-values`: use refs for transient frequent values like scroll position

- **FIX-013**: Debounce scroll handler in `ChatSidebar.tsx`:
  - `handleScroll` MUST be wrapped in a debounce (150ms) or use `IntersectionObserver` for infinite scroll
  - Follow `client-passive-event-listeners`: use passive listeners for scroll events
  - Follow `client-event-listeners`: deduplicate global event listeners (document click handlers in ChatListItem)

### 3.9 Infrastructure — Docker & Nginx (P2/P3)

- **FIX-014**: Docker Compose network mode. Add a shared network for all services instead of mixing host mode with bridge mode:
  ```yaml
  networks:
    liwaisi:
      driver: bridge
  ```
  All services MUST use this network. Backend connects to postgres via service name (`postgres:5432`) instead of `localhost:5432`. Update `.env.example` DSN accordingly. Keep `network_mode: host` as a documented dev-override option.

- **FIX-015**: Nginx CSP tightening. Replace `'unsafe-inline'` for styles with a nonce-based approach if feasible, or document the trade-off. At minimum, add `style-src-attr 'unsafe-inline'` (narrower than full style-src) and add cache headers for static assets:
  ```nginx
  location /assets/ {
      expires 1y;
      add_header Cache-Control "public, immutable";
  }
  ```

### 3.10 Frontend — Tooltip Deduplication (P3)

- **FIX-016**: Extract tooltip logic from `NavigationRail.tsx` into a `useTooltip(delay?: number)` hook:
  ```typescript
  function useTooltip(delay = 600): {
    visible: boolean;
    onMouseEnter: () => void;
    onMouseLeave: () => void;
  }
  ```
  Replace the 3 duplicated tooltip implementations (NavButton, settings, tools) with this hook.

## 4. Interfaces & Data Contracts

### 4.1 Updated `dispatch()` Signature

```go
// dispatch now returns output token snapshots for event emission,
// cost for billing, and error for routing.
func dispatch(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error)
```

All `fire*` functions MUST conform:
```go
func fireTool(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error)
func fireLLM(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error)
func fireValidate(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error)
func fireSubNet(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error)
func fireHITL(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error)
func fireHITLWithRevision(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error)
```

### 4.2 Updated `callCorrectionLLM()` Signature

```go
// callCorrectionLLM returns the corrected content, LLM cost, and error.
func callCorrectionLLM(ctx context.Context, c *CPN, modelID string, payload string, schemaError string) (string, float64, error)
```

### 4.3 Updated `fireWithRetry()` Signature

```go
// fireWithRetry wraps a fire function with retry policy and circuit breaker.
// The inner function now returns snapshots, cost, and error.
func fireWithRetry(ctx context.Context, policy *RetryPolicy, cb *CircuitBreakerState,
    fn func() ([]TokenSnapshot, float64, error)) ([]TokenSnapshot, float64, error)
```

### 4.4 Rate Limiter Middleware Interface

```go
// RateLimiter provides per-key rate limiting.
type RateLimiter interface {
    Allow(key string) bool
    RetryAfter(key string) time.Duration
}

// RateLimitConfig configures rate limits per endpoint category.
type RateLimitConfig struct {
    SSEMaxConcurrent   int           // max concurrent SSE connections per user
    MessageRateLimit   int           // max messages per window per session
    MessageRateWindow  time.Duration // window for message rate limit
    GeneralRateLimit   int           // max requests per window per user
    GeneralRateWindow  time.Duration // window for general rate limit
}
```

### 4.5 Frontend Hook Interfaces

```typescript
// useSessionManager hook return type
interface SessionManager {
  activeSessionId: string | null;
  sessions: SessionInfo[];
  isRunning: boolean;
  pendingHITL: HITLRequest | null;
  dispatch: (action: SessionAction) => void;
  sseCallbacks: SSECallbacks;
}

// useMonitorManager hook return type
interface MonitorManager {
  monitorSessionId: string | null;
  firedTransitions: Set<string>;
  monitorEvents: MonitorEvent[];
  executionRuns: ExecutionRun[];
  dispatch: (action: MonitorAction) => void;
}

// usePanelManager hook return type
interface PanelManager {
  panelContent: PanelContent | null;
  panelOpen: boolean;
  selectedFlowHash: string | null;
  dispatch: (action: PanelAction) => void;
}

// useTooltip hook return type
interface TooltipControls {
  visible: boolean;
  onMouseEnter: () => void;
  onMouseLeave: () => void;
}
```

### 4.6 Migration 011

```sql
-- 011_add_fork_fk.up.sql
ALTER TABLE sessions
  ADD CONSTRAINT fk_sessions_forked_from
  FOREIGN KEY (forked_from_session_id)
  REFERENCES sessions(id)
  ON DELETE SET NULL;

-- 011_add_fork_fk.down.sql
ALTER TABLE sessions
  DROP CONSTRAINT IF EXISTS fk_sessions_forked_from;
```

## 5. Acceptance Criteria

### Backend Concurrency & Safety

- **AC-001**: Given concurrent transitions sharing an output place, When both fire goroutines complete, Then NO data race is detected under `go test -race ./cpn/...`
- **AC-002**: Given a SubNet whose output place is full (MaxTokens reached), When `fireSubNet` attempts deposit, Then an `EventSubNetFailed` event is emitted AND the error is returned to the parent executor
- **AC-003**: Given a HITL channel that is closed while a fire goroutine blocks on receive, When the channel closes, Then the goroutine returns an error (not a zero-value response) AND does NOT auto-approve tool execution
- **AC-004**: Given a personality tool called with a non-string payload, When `makeGetIdentity` executes, Then it returns an error with message containing "expected non-empty string user_id"
- **AC-005**: Given the personality repository is down (returns connection error), When `makeGetIdentity` executes, Then it returns an error (not a silent default fallback)
- **AC-006**: Given a validation flow with 3 correction iterations, When each iteration calls the correction LLM, Then the total cost returned by `fireValidate` equals the sum of the initial LLM cost plus all correction LLM costs
- **AC-007**: Given a HITL revision loop with 2 revisions, When each revision calls `fireLLMDirect`, Then the total cost returned by `fireHITLWithRevision` includes all revision LLM costs

### Backend Guard & Rate Limiting

- **AC-008**: Given a classifier that returns `{"intent":"task"}`, When `guardPlanTask` evaluates, Then it returns `true`. Given malformed JSON, Then it returns `false`
- **AC-009**: Given a classifier that returns `{"intent":"conversation"}`, When `guardDirectConversation` evaluates, Then it returns `true`
- **AC-010**: Given a user with 3 active SSE connections, When they open a 4th, Then the server returns `429 Too Many Requests` with a `Retry-After` header
- **AC-011**: Given a session receiving 11 messages in 1 minute, When the 11th arrives, Then the server returns `429`

### Frontend Performance

- **AC-012**: Given `DesktopLayout` receives a new SSE event, When the event only affects monitor state, Then session-related components do NOT re-render (verified via React DevTools Profiler)
- **AC-013**: Given a `ChatSidebar` with 50 chat items, When one item is renamed, Then only that `ChatListItem` re-renders (verified via React.memo + Profiler)
- **AC-014**: Given `NavigationRail` with 3 tooltip targets, When hovering over settings, Then tooltip logic executes via the shared `useTooltip` hook (no duplicated timer logic in component)

### Infrastructure

- **AC-015**: Given `docker compose up`, When all services start, Then backend connects to postgres via service name `postgres:5432` (not `localhost`)
- **AC-016**: Given static assets served by nginx, When a browser requests `/assets/main.js`, Then the response includes `Cache-Control: public, immutable` and `Expires` header

## 6. Test Automation Strategy

### Test Levels

| Level | Scope | Framework | Files |
|-------|-------|-----------|-------|
| Unit (Go) | All FIX-001 through FIX-007 | `testing` + `-race` | `*_test.go` alongside each file |
| Unit (React) | FIX-010 through FIX-016 | Vitest + React Testing Library | `*.test.tsx` alongside each hook/component |
| Integration (Go) | Rate limiter middleware, cost propagation chain | `net/http/httptest` | `internal/driving/httpapi/*_test.go` |
| Regression | Full CPN execution with tools + HITL + validation | `testing` + `-race` | `cpn/executor_integration_test.go` |

### Go Test Requirements

- **All tests MUST pass with `-race` flag**: `go test -race ./...`
- **Coverage target**: 85%+ for changed files, measured with `go test -coverprofile`
- **Race-specific tests**: Add a test for FIX-001 that spawns 50 concurrent transitions sharing an output place and verifies no race
- **Channel close test**: Add a test that closes a HITL channel while a goroutine blocks on it, verifying error return
- **Cost tracking test**: Add a test for `fireValidate` with N correction iterations, asserting total cost = sum of all LLM calls

### React Test Requirements

- **Hook tests**: Each extracted hook (`useSessionManager`, `useMonitorManager`, `usePanelManager`, `useTooltip`) MUST have unit tests using `@testing-library/react` `renderHook`
- **Re-render tests**: Use `jest.fn()` render counters on memoized components to verify they don't re-render when unrelated props change
- **Snapshot stability**: Memoized components should not break existing visual snapshots

### CI/CD Integration

- Go: `go test -race -coverprofile=coverage.out ./...` in GitHub Actions
- React: `npx vitest run --coverage` in GitHub Actions
- Both MUST pass before PR merge

## 7. Rationale & Context

### Why FIX-001 changes dispatch() to return snapshots

The current approach (Peek on output places after dispatch) is racy because fire goroutines run concurrently and share output places. By building snapshots inside each `fire*` function from freshly-created tokens (before deposit), we guarantee each goroutine only reads its own data. This adds a return value but eliminates the race entirely without adding locks.

### Why FIX-004 distinguishes error types

The current silent fallback means a Postgres outage is indistinguishable from "user has no custom personality." This violates the principle of least surprise. Users who customized their personality will silently get the default when the DB is slow. Distinguishing "not found" from "error" preserves graceful degradation while surfacing real failures.

### Why FIX-007 parses JSON instead of string-matching

The guard functions use `strings.Contains(s, "\"task\"")` which matches the literal substring `"task"` anywhere in the payload. This works for well-formatted `{"intent":"task"}` but fails for edge cases (extra whitespace, different key names, payload containing the word "task" in conversation content). JSON parsing is deterministic and correct.

### Why FIX-008 adds rate limiting

Without rate limiting, a single client can:
1. Open unlimited SSE connections (memory exhaustion)
2. Spam `SendMessage` (unbounded goroutine creation + LLM cost amplification)
3. Hit all endpoints at wire speed (CPU exhaustion)

This is critical even for dev-mode because the app accepts unauthenticated requests with a synthetic dev user.

### Why FIX-010 splits DesktopLayout state

9 `useState` calls in a single component means every state change re-renders the entire component tree. SSE events (which fire frequently during streaming) trigger monitor state updates, which re-render session components, which re-render panel components. Splitting state into domain-specific hooks with `useReducer` limits re-render scope to the affected domain. This follows Vercel's `rerender-split-combined-hooks` rule.

## 8. Dependencies & External Integrations

### Technology Platform Dependencies

- **PLT-001**: Go 1.25+ — Required for existing codebase compatibility
- **PLT-002**: React 19+ — Required for existing frontend. No Next.js (pure Vite SPA)
- **PLT-003**: PostgreSQL 16 — Required for migration 011 FK constraint
- **PLT-004**: Tailwind CSS 4 — Existing styling framework, no changes

### Infrastructure Dependencies

- **INF-001**: Docker Compose v2 — Required for network configuration changes (FIX-014)
- **INF-002**: nginx 1.27+ — Required for cache header directives (FIX-015)

### No New External Dependencies

- Rate limiting (FIX-008): Implement with Go stdlib (`sync.Mutex`, `time.Ticker`, `map`). No external library required for a token-bucket implementation.
- Scroll debounce (FIX-013): Implement with `setTimeout`/`clearTimeout` or `IntersectionObserver` (browser API). No library needed.
- React memoization (FIX-011): Uses built-in `React.memo`. No library needed.

## 9. Examples & Edge Cases

### Example: dispatch() returning snapshots (FIX-001)

```go
func fireTool(ctx context.Context, t *Transition, c *CPN, consumed []Token) ([]TokenSnapshot, float64, error) {
    if t.Executor == nil {
        return nil, 0, fmt.Errorf("transition %s: nil Executor", t.ID)
    }

    result, err := t.Executor(ctx, consumed[0])
    if err != nil {
        return nil, 0, err
    }

    // Build snapshot from freshly-created token BEFORE deposit.
    snap := result.Snapshot()

    // Now deposit.
    for _, pid := range t.OutputPlaces {
        if p, ok := c.Places[pid]; ok {
            tok := result
            tok.Space = p.Space
            tok.Color = p.Color
            if depositErr := p.Deposit(&tok); depositErr != nil {
                return nil, 0, fmt.Errorf("transition %s: deposit to %s: %w", t.ID, pid, depositErr)
            }
        }
    }

    return []TokenSnapshot{snap}, 0, nil
}
```

### Example: HITL channel close protection (FIX-003)

```go
select {
case <-ctx.Done():
    return nil, loopCost, ctx.Err()
case response, ok := <-t.HITLConfig.Channel:
    if !ok {
        c.setState(StateRunning)
        return nil, loopCost, fmt.Errorf("transition %s: HITL channel closed", t.ID)
    }
    c.setState(StateRunning)
    // ... process response
}
```

### Edge Case: Type assertion with JSON payload (FIX-005)

When the LLM sends tool arguments as `{"user_id": 12345}` (number instead of string), the current code silently converts to `""`. After fix:

```go
// Input: Token{Payload: float64(12345)} — LLM sent a number
userID, ok := in.Payload.(string)
// ok == false, returns error: "expected non-empty string user_id, got float64"
```

### Edge Case: Guard with malformed classifier output (FIX-007)

```go
// Input: "I'm thinking about task management" (not JSON)
// json.Unmarshal fails → guardPlanTask returns false → routes to conversation
// This is correct: raw text isn't a classifier output, so default to conversation.

// Input: {"intent": "TASK"} (uppercase)
// strings.EqualFold(result.Intent, "task") → true → routes to task
// This is correct: case-insensitive matching.
```

### Edge Case: Cost propagation through nested correction (FIX-006)

```
fireValidate called → initial validation fails
  → callCorrectionLLM #1 → cost: $0.002
  → validation still fails
  → callCorrectionLLM #2 → cost: $0.003
  → validation passes
Total cost returned: $0.005

Previously returned: $0.000 (all correction costs lost)
```

## 10. Validation Criteria

### Go Backend Validation

1. `go test -race ./cpn/...` passes with 0 race conditions detected
2. `go test -race ./internal/...` passes with 0 race conditions detected
3. `go vet ./...` reports 0 issues
4. `golangci-lint run` reports 0 issues on changed files
5. Test coverage on changed files >= 85% (measured by `go test -coverprofile`)
6. All new error paths have corresponding test cases
7. Cost tracking test verifies accumulated cost matches expected sum (within float64 epsilon)

### React Frontend Validation

1. `npx tsc --noEmit` passes with 0 errors
2. `npm run build` succeeds with no warnings on changed files
3. React DevTools Profiler shows no unnecessary re-renders in DesktopLayout when:
   - An SSE event arrives that only affects monitor state
   - A chat list item is renamed
   - A tooltip appears on NavigationRail
4. All extracted hooks have unit tests via `renderHook`

### Infrastructure Validation

1. `docker compose up` starts all services and backend connects to postgres (verified by health check)
2. `curl -I http://localhost/assets/main.js` returns `Cache-Control: public, immutable`
3. Migration 011 applies cleanly on existing database with forked sessions

## 11. Related Specifications / Further Reading

- [spec-architecture-tools-engine-agent-personality.md](spec-architecture-tools-engine-agent-personality.md) — Original tools engine and personality system spec
- [spec-architecture-mcp-tool-standard-adaptation.md](spec-architecture-mcp-tool-standard-adaptation.md) — MCP standard adaptation spec
- [spec-design-cpn-execution-monitor.md](spec-design-cpn-execution-monitor.md) — Execution monitor spec (affected by FIX-001 event changes)
- [spec-design-ux-refresh-navigation.md](spec-design-ux-refresh-navigation.md) — Navigation rail spec (affected by FIX-016 tooltip refactor)
- Vercel React Best Practices — Rules `rerender-split-combined-hooks`, `rerender-memo`, `client-passive-event-listeners`
- Go Concurrency Patterns — Channel close detection, goroutine lifecycle management
