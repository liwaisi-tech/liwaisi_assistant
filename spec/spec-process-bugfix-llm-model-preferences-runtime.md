---
title: Bugfix — Apply User LLM Model Preferences at Runtime
version: 1.0
date_created: 2026-04-08
owner: liwaisi
tags: [process, bugfix, backend, frontend, llm, onboarding]
---

# Introduction

The onboarding wizard collects per-user LLM model preferences (`preferred_model` plus optional per-role `model_overrides`) and persists them via `POST /api/v1/user/onboarding/complete`. The CPN execution engine, however, never reads them: every LLM call resolves models through a static registry built once at startup from environment variables. The user's choices are silently discarded. This specification defines the fix and a one-click "single model for everything" UX.

## 1. Purpose & Scope

Restore the contract that user-selected LLM models drive runtime behavior, and make the most common UX path — pick one model, use it everywhere — a single click. In scope: `back/go-assistant` session creation path and `front/react-assistant` setup wizard copy. Out of scope: rotating env defaults, OpenRouter client internals, topology factories, hot-reload of admin config.

Audience: implementers (Go backend, React frontend) and reviewers.

## 2. Definitions

- **CPN**: Colored Petri Net — execution graph used by the assistant runtime.
- **Role key**: A short symbolic name (`classifier`, `structured`, `reasoning`, `long-context`, `summarize`, `thinking`) that the OpenRouter client maps to a concrete model ID.
- **PreferredModel**: User's chosen default model (concrete OpenRouter ID).
- **ModelOverrides**: Map of role-key → concrete model ID for power users.
- **Single-model UX**: Selecting one `PreferredModel` and leaving `ModelOverrides` empty results in every LLM call using `PreferredModel`.

## 3. Requirements, Constraints & Guidelines

- **REQ-001**: When a session is created for a user with persisted preferences, every transition with an `LLMConfig` MUST resolve its model from the user's preferences before the CPN executes.
- **REQ-002**: For each transition, the precedence MUST be: (1) `ModelOverrides[roleKey]` if present, (2) `PreferredModel` if non-empty, (3) the existing `LLMConfig.Model` value (role key or empty) untouched — preserving env/default fallback.
- **REQ-003**: The fix MUST apply to both `SessionService.CreateSession` and `SessionService.ForkSession`.
- **REQ-004**: Anonymous sessions, sessions without persistence, and users with no stored preferences MUST behave exactly as today (no regression in env-fallback path).
- **REQ-005**: The frontend setup wizard MUST clearly communicate that the default model selection applies to every task unless overridden in the advanced section.
- **CON-001**: No changes to `infra/openrouter/openrouter.go` (`Client.ModelRegistry`, `resolveModel`).
- **CON-002**: No changes to topology factories in `cmd/server/topologies.go`.
- **CON-003**: No new database migrations; the existing `users.preferred_model` and `users.model_overrides` columns are sufficient.
- **CON-004**: The fix MUST NOT pass user IDs into the topology factory signature (avoid topology-layer coupling to identity).
- **GUD-001**: Mirror the existing `resolveRegionalVariant` helper pattern in `internal/app/session_service.go` for symmetry and discoverability.
- **GUD-002**: Failures fetching user preferences MUST be non-fatal — log at debug and proceed with role-key fallback.
- **PAT-001**: Resolution happens once at session creation, not per LLM call. The resolved concrete model ID is written into `t.LLMConfig.Model` so the OpenRouter client receives it as-is and skips registry lookup.

## 4. Interfaces & Data Contracts

No public API changes. Internal Go signature added:

```go
// applyUserModelPreferences rewrites LLMConfig.Model on every transition in
// root using the user's persisted preferences. Best-effort: any error path
// leaves the topology unchanged.
func (s *SessionService) applyUserModelPreferences(ctx context.Context, root *cpn.CPN, userID string)
```

Existing persisted shape (unchanged):

```json
{
  "preferred_model": "anthropic/claude-sonnet-4-6",
  "model_overrides": { "classifier": "google/gemini-2.0-flash-001" }
}
```

Frontend i18n additions (keys, English copy shown; Spanish mirrors):

| Key | Copy |
|---|---|
| `setup:model.appliesToAll` | "This model handles every task. Customize per-task models below if you want finer control." |

## 5. Acceptance Criteria

- **AC-001**: Given a user has completed onboarding with `preferred_model = "anthropic/claude-haiku-4-5-20251001"` and empty `model_overrides`, When the user sends a chat message, Then every LLM call recorded in `llm_calls.model_resolved` MUST equal `"anthropic/claude-haiku-4-5-20251001"`.
- **AC-002**: Given a user has `preferred_model = "X"` and `model_overrides = {"classifier": "Y"}`, When a session runs, Then the classifier transition MUST call model `Y` and all other LLM transitions MUST call model `X`.
- **AC-003**: Given a user has no stored preferences (or persistence is disabled), When a session runs, Then model resolution MUST behave identically to the pre-fix codebase (env vars → defaults).
- **AC-004**: Given a forked session, When the fork is created via `ForkSession`, Then it MUST receive the same model preference application as a fresh `CreateSession`.
- **AC-005**: Given the user opens the setup wizard model step, When they view the default model dropdown, Then a hint MUST be visible explaining the choice applies to every task unless advanced overrides are set.
- **AC-006**: The user repository fetch error path MUST NOT abort session creation; the session MUST be created with role-key fallback.

## 6. Test Automation Strategy

- **Test Levels**: Unit (Go), Integration (Go, against real `SessionService` with stub `UserRepo`), Frontend visual (manual smoke).
- **Frameworks**: Go standard `testing`, table-driven tests; existing test utilities in `internal/app`.
- **Test Data**: Stub `persist.UserRepository` returning canned `UserRecord` values.
- **Coverage**: New helper `applyUserModelPreferences` MUST have unit tests covering all four precedence branches plus the nil-persist path.
- **CI/CD**: Existing `go test ./...` in CI must remain green.

## 7. Rationale & Context

The OpenRouter client builds its `ModelRegistry` once at process startup (`buildModelRegistry`, `infra/openrouter/openrouter.go:69`). At call time, `resolveModel` (L413) maps a role-key in `LLMConfig.Model` to a concrete ID via that frozen map. Topology factories (`cmd/server/topologies.go:343,499`) hardcode role-keys into transitions. The user-preference write path (`internal/driving/httpapi/handler_user.go:191`) persists choices but no read path consumes them at session-creation time.

The cleanest fix injects user-aware resolution in `SessionService.CreateSession` — already the place where user-specific topology customization happens (`resolveRegionalVariant` at L122, `injectPersonality` at L154). Writing the resolved concrete ID directly into `t.LLMConfig.Model` short-circuits the registry lookup (`resolveModel` returns the value as-is when not found in the registry, L417-420), so the OpenRouter client needs no changes.

The single-click UX falls out for free: an empty `ModelOverrides` map combined with a non-empty `PreferredModel` causes every transition to be rewritten to `PreferredModel`. The frontend already collects this shape; only copy clarification is needed.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: OpenRouter — receives the resolved concrete model ID; no contract change.

### Data Dependencies
- **DAT-001**: `users` table columns `preferred_model` and `model_overrides` (existing).

### Technology Platform Dependencies
- **PLT-001**: Go 1.22+ (existing); React 18 + Vite (existing).

## 9. Examples & Edge Cases

```go
// internal/app/session_service.go (illustrative)
func (s *SessionService) applyUserModelPreferences(ctx context.Context, root *cpn.CPN, userID string) {
    if s.persist == nil || s.persist.Users == nil || userID == "" {
        return
    }
    rec, err := s.persist.Users.GetByID(ctx, userID)
    if err != nil {
        return // best-effort; preserve role-key fallback
    }
    if rec.PreferredModel == "" && len(rec.ModelOverrides) == 0 {
        return
    }
    for _, t := range root.Transitions {
        if t.LLMConfig == nil {
            continue
        }
        roleKey := t.LLMConfig.Model // may be "" or a role key like "classifier"
        if override, ok := rec.ModelOverrides[roleKey]; ok && override != "" {
            t.LLMConfig.Model = override
            continue
        }
        if rec.PreferredModel != "" {
            t.LLMConfig.Model = rec.PreferredModel
        }
    }
}
```

Edge cases:
- Empty `LLMConfig.Model` (transitions like `tDirect` that rely on the global default) — covered by REQ-002 step 2: `PreferredModel` overrides the empty string.
- `ModelOverrides` contains a role key that does not exist in any transition — silently ignored.
- `PreferredModel` set but not in `AvailableModels` — accepted as-is; OpenRouter will reject at call time. Validation belongs to a separate spec.
- User record missing — fallback to current behavior, no error surfaced to user.

## 10. Validation Criteria

1. New unit tests for `applyUserModelPreferences` cover: nil persist, missing user, both fields empty, only `PreferredModel` set, only `ModelOverrides` set, both set.
2. Integration test creates a session for a user with preferences and asserts every transition's `LLMConfig.Model` matches expectation.
3. Manual: complete onboarding picking a non-default model, send a chat message, inspect `llm_calls.model_resolved` in Postgres — matches the chosen model.
4. Frontend: setup wizard model step shows the new hint string under the default dropdown, in both `en` and `es` locales.
5. `go test ./...`, `golangci-lint run`, frontend `pnpm lint` & `pnpm build` all green.

## 11. Related Specifications / Further Reading

- `spec/spec-architecture-setup-mode-onboarding-wizard.md`
- `spec/spec-design-regional-language-variant.md` (resolveRegionalVariant precedent)
