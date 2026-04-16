---
title: User Preferences Settings Page (Post-Onboarding Model & Locale Editing)
version: 1.0
date_created: 2026-04-08
owner: liwaisi
tags: [design, frontend, backend, settings, onboarding, llm]
---

# Introduction

Onboarding is one-shot: once `users.onboarding_completed_at` is set, the wizard never runs again. Users who completed onboarding before the LLM-preferences runtime fix landed (or who simply change their mind) currently have no UI to update `preferred_model`, `model_overrides`, `preferred_language`, or `regional_variant`. The backend `PUT /api/v1/user/preferences` endpoint exists but is missing the `regional_variant` field, and no frontend page calls it. This spec closes the gap by adding a Settings page reachable from the user menu, plus the missing backend field.

## 1. Purpose & Scope

In scope:
- Backend: extend `UpdatePreferencesRequest` to accept `regional_variant`, persist it, and validate it the same way `HandleCompleteOnboarding` does.
- Frontend: a new Settings route `/settings` with a "Preferences" panel that lets the authenticated user view and update their language, regional variant, default model, and per-role overrides; saves via `PUT /api/v1/user/preferences`.
- Frontend: an entry point in the existing user menu / header.

Out of scope: personality editing (separate page), admin global config, billing, account deletion, password/auth flows, re-running the full onboarding wizard.

Audience: implementers (Go backend, React frontend) and reviewers.

## 2. Definitions

- **Settings page**: a new authenticated route `/settings` distinct from the admin page.
- **Preferences panel**: the section of the Settings page dedicated to user-level preferences (this spec).
- **PUT preferences**: shorthand for `PUT /api/v1/user/preferences`.
- **Profile fetch**: `GET /api/v1/user/profile`, already returning `preferences.{preferred_language, regional_variant, preferred_model, model_overrides}`.

## 3. Requirements, Constraints & Guidelines

### Backend
- **REQ-001**: `UpdatePreferencesRequest` MUST accept an optional `regional_variant` string field (BCP-47).
- **REQ-002**: When `regional_variant` is non-empty, `HandleUpdatePreferences` MUST validate it against `prompts.IsSupported`, mirroring `HandleCompleteOnboarding`. Empty value MUST be allowed (resolved server-side to language default).
- **REQ-003**: When `regional_variant` is empty and `preferred_language` is non-empty, the handler MUST persist `prompts.DefaultVariant(lang)` (same behavior as onboarding).
- **REQ-004**: `preferred_language`, when present, MUST be validated against `{"en", "es"}`.
- **REQ-005**: Empty string values for `preferred_model` / `model_overrides` MUST be persisted as-is — empty means "no preference" and triggers env-fallback at session creation. The handler MUST NOT silently ignore them.
- **REQ-006**: Existing `applyUserModelPreferences` runtime path MUST require no changes — the new endpoint writes to the same columns.
- **CON-001**: No new database migrations.
- **CON-002**: No change to authentication or routing layer beyond the existing `PUT /api/v1/user/preferences` registration.
- **GUD-001**: Reuse the `prompts.IsSupported` / `prompts.DefaultVariant` helpers; do not duplicate validation logic.

### Frontend
- **REQ-101**: A new route `/settings` MUST be registered in the app router, gated by the existing auth guard.
- **REQ-102**: The user menu (header avatar dropdown, or equivalent existing component) MUST expose a "Settings" / "Configuración" entry that navigates to `/settings`.
- **REQ-103**: On mount, the Settings page MUST fetch the current profile via `getProfile()` and pre-populate all form fields. While loading, a skeleton or spinner MUST be visible; on error, a user-readable message in the active locale MUST be shown.
- **REQ-104**: The Preferences panel MUST allow editing: language (en/es), regional variant (filtered by language), default model, and per-role model overrides. The model controls MUST reuse the same UX affordances as the onboarding `ModelStep`: Simple/Advanced mode badge, reset-to-single button, scrollable advanced list.
- **REQ-105**: A "Save changes" button MUST be disabled when the form state equals the last-fetched server state (no dirty changes), and MUST show a loading state during the request.
- **REQ-106**: On successful save, the page MUST display an inline success indicator (toast or banner) and update its baseline state so the button re-disables. On failure, an error message MUST be shown and the form state MUST be preserved.
- **REQ-107**: All UI strings MUST come from i18n (`en` and `es` locales). New keys live under a new namespace `settings` or under existing `setup`/`common` if a clear fit exists.
- **REQ-108**: The page MUST NOT trigger a logout, page reload, or session reset on save. Existing chat sessions stay open; the next *new* session inherits the updated preferences (this is the documented behavior of `applyUserModelPreferences`, which runs at `CreateSession`).
- **CON-101**: No new state-management library. Use local component state + the existing `services/api.ts` client.
- **CON-102**: No new UI dependencies.
- **GUD-101**: Extract the model-picker portion of `ModelStep.tsx` into a reusable presentational component if doing so is mechanical; otherwise duplicate the markup. Do not refactor `ModelStep` itself in a way that breaks onboarding.
- **GUD-102**: vercel-react-best-practices: derive UI from props/state, no unnecessary effects, stable callbacks, no inline objects in dependency arrays.

## 4. Interfaces & Data Contracts

### Backend — updated request

```go
// UpdatePreferencesRequest is the body for PUT /api/v1/user/preferences.
type UpdatePreferencesRequest struct {
    PreferredLanguage string            `json:"preferred_language"`
    RegionalVariant   string            `json:"regional_variant"`
    PreferredModel    string            `json:"preferred_model"`
    ModelOverrides    map[string]string `json:"model_overrides"`
}
```

Response: `200 OK` with `{"ok": true}` (unchanged shape). Error responses: `400` for invalid language/variant, `401` unauthenticated, `503` if persistence is disabled, `500` on internal error.

### Frontend — services/api.ts

```ts
export interface UpdatePreferencesPayload {
  preferred_language?: string;
  regional_variant?: string;
  preferred_model?: string;
  model_overrides?: Record<string, string>;
}

export async function updatePreferences(payload: UpdatePreferencesPayload): Promise<void>;
```

If `updatePreferences` already exists, extend its payload type to include `regional_variant`.

### Routing

| Path | Component | Auth |
|---|---|---|
| `/settings` | `<SettingsPage>` | required |

## 5. Acceptance Criteria

- **AC-001**: Given a user has completed onboarding, When they open the user menu and click "Settings", Then they land on `/settings` and see their current preferences pre-filled.
- **AC-002**: Given the user changes the default model from minimax to gemma and clicks Save, When the request succeeds, Then `users.preferred_model` in Postgres equals `google/gemma-4-26b-a4b-it` and the success indicator appears.
- **AC-003**: Given the saved preferences include `preferred_model = "google/gemma-4-26b-a4b-it"`, When the user starts a *new* chat session and sends a message, Then every row in `llm_calls` for that session has `model_resolved = "google/gemma-4-26b-a4b-it"`.
- **AC-004**: Given the user adds a per-role override for `classifier`, When they save and start a new session, Then only the classifier transition uses the override and other transitions use `preferred_model`.
- **AC-005**: Given the user clicks "Reset to single model" inside Advanced, When they save, Then `users.model_overrides` equals `{}`.
- **AC-006**: Given the user changes the regional variant to `es-MX`, When they save, Then `users.regional_variant = 'es-MX'`. Given the user submits an unsupported variant, Then the backend returns `400` and the page surfaces the error without persisting.
- **AC-007**: Given the form is unchanged from the last fetched state, Then the Save button is disabled.
- **AC-008**: Given the locale is `es`, Then every visible string on the Settings page renders in Spanish.
- **AC-009**: Given the user is unauthenticated, When they navigate to `/settings`, Then they are redirected to the login flow (existing auth guard behavior).
- **AC-010**: Given existing onboarding behavior, When the new endpoint is deployed, Then `POST /api/v1/user/onboarding/complete` continues to work unchanged (no regressions in the wizard happy path).

## 6. Test Automation Strategy

- **Backend**: extend `handler_user_test.go` with table-driven tests for `HandleUpdatePreferences` covering: valid full payload, valid language-only, empty regional_variant resolved to default, invalid variant → 400, invalid language → 400, persistence-disabled → 503, unauthenticated → 401.
- **Frontend**: component test for the Settings page using existing test setup (vitest + RTL): renders fetched profile, dirty-tracking enables Save, mocked API success path shows toast, mocked API failure path shows error and preserves form.
- **Manual**: docker-compose end-to-end — change model in Settings, start new chat, query `llm_calls.model_resolved`.
- **CI**: `go test ./...`, `golangci-lint run`, frontend `pnpm lint && pnpm build && pnpm test` MUST be green.

## 7. Rationale & Context

The runtime fix in `spec-process-bugfix-llm-model-preferences-runtime.md` is correct, but it can only act on data the user wrote. Verification on the running stack revealed the user record had empty `preferred_model` because onboarding finished before the fix, and there is no UI to edit preferences afterwards. A Settings page is the smallest, most idiomatic close to the gap and unblocks every user who has already onboarded. Adding `regional_variant` to the update endpoint also closes a latent inconsistency: the field is collected at onboarding but never editable, even though `RegionStep` and the underlying CPN regional preamble logic expect it to be mutable per the regional-variant spec.

## 8. Dependencies & External Integrations

### Internal Systems
- **INT-001**: Existing `users` table (columns already present).
- **INT-002**: Existing `getModels()` API for the model dropdown population.
- **INT-003**: `prompts.IsSupported` / `prompts.DefaultVariant` (Go).

### Technology Platform Dependencies
- **PLT-001**: Go 1.22+, React 18, Vite, react-router (existing).

## 9. Examples & Edge Cases

```ts
// Settings page save flow (illustrative)
const onSave = useCallback(async () => {
  setSaving(true);
  setError(null);
  try {
    await updatePreferences({
      preferred_language: form.language,
      regional_variant: form.regionalVariant,
      preferred_model: form.model,
      model_overrides: form.modelOverrides,
    });
    setBaseline(form);  // re-disable Save
    setToast('saved');
  } catch (e) {
    setError(messageFromError(e));
  } finally {
    setSaving(false);
  }
}, [form]);
```

Edge cases:
- User clears `preferred_model` (sets it back to empty string) → backend MUST persist empty string; runtime falls back to env defaults. UI MUST allow this via an explicit "Use system default" option in the model dropdown (not silently coerced to the current default).
- User's saved `preferred_model` is no longer in `AvailableModels` (admin removed it) → dropdown MUST still show the stale value as selected with a warning hint, so the user is aware before saving over it.
- Network failure mid-save → form state preserved, error shown, Save remains enabled.
- User changes language but not variant → backend resolves variant to language default (REQ-003).

## 10. Validation Criteria

1. New backend tests pass; old onboarding tests still pass.
2. New frontend test renders Settings with mocked profile fetch and exercises save success + failure.
3. Manual: change model in Settings, send a chat message in a new session, confirm `llm_calls.model_resolved` matches in Postgres.
4. `go test ./...`, `golangci-lint run`, `pnpm lint`, `pnpm build`, `pnpm test` all green.
5. Both `en` and `es` locales render all new copy.

## 11. Related Specifications / Further Reading

- `spec/spec-process-bugfix-llm-model-preferences-runtime.md`
- `spec/spec-architecture-setup-mode-onboarding-wizard.md`
- `spec/spec-design-regional-language-variant.md`
