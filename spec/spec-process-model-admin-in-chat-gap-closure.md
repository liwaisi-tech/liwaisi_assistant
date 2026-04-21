---
title: In-Chat Model Admin — Gap Closure (CPN Trigger, Register Form, Responding-Model Badge, Tests, i18n)
version: 1.0
date_created: 2026-04-17
last_updated: 2026-04-17
owner: liwaisi — model registry workstream
tags: [process, architecture, design, frontend, backend, cpn, a2ui, i18n, tests]
---

# Introduction

Close the five remaining gaps blocking end-to-end delivery of the in-chat model-admin feature on branch `feat/model-registry-foundation`. The backend admin REST surface and registry cascade are already merged (commits `ef21759`, `e5ab910`, `5000c56`, `cf916cd`, `87b48f8`, `9d0f80a`). Uncommitted frontend work lives in `front/react-assistant/src/features/chat/modelAdmin/` plus edits to `ChatContainer.tsx`, `DesktopLayout.tsx`, `MessageBubble.tsx`, `MessageList.tsx`, `A2UIMessageRenderer.tsx`, `useChat.ts`, and `services/api.ts`. This spec freezes the scope, requirements, and acceptance criteria for the final slices.

Parent specs (READ FIRST):

- [`spec/spec-architecture-model-registry-and-a2ui-management.md`](spec-architecture-model-registry-and-a2ui-management.md)
- [`spec/spec-architecture-model-selection-centralization.md`](spec-architecture-model-selection-centralization.md)

This spec is **additive** to the parent spec and MUST NOT contradict it. Where the parent spec uses `REQ-*` identifiers (`REQ-CPN-*`, `REQ-FE-*`, `REQ-A2UI-*`), this spec references them verbatim and adds `REQ-GAP-*` for the new, gap-level requirements.

## 1. Purpose & Scope

### 1.1 Purpose
Deliver a testable end-to-end path where an admin user types a natural-language intent (e.g. "list my models", "registra un modelo nuevo", "bloquea gpt-5") in chat, receives a CPN-emitted A2UI management surface, and completes a round-trip mutation with correct locking, persistence, and localized copy.

### 1.2 In-scope (the five gaps)
1. **GAP-CPN**: CPN topology fragment `manage-models-flow` that classifies the intent, emits `ModelCardList` / `RegisterModelForm` / `ConfirmStateChange` / `LicenseReviewCard` / `DefaultSelector` surfaces, applies mutations via `ModelRegistryService`, and performs the stale-surface replay lock.
2. **GAP-REG**: Client-side wiring for the `RegisterModelForm` surface — rendering the form the agent emits, capturing field values, emitting `submit_register`, and handling server-side validation re-emission.
3. **GAP-IND**: Responding-model indicator on the assistant message bubble (`REQ-FE-004`, `REQ-FE-005`) — backend populates `responding_model` on the final `stream_chunk`, frontend shows a `RoundBadge` below content.
4. **GAP-TEST**: Vitest coverage for `buildModelAdminSurface`, `useModelAdminFlow`, the new `useChat` reducer actions (`INJECT_LOCAL_MESSAGE`, `UPDATE_MESSAGE_CONTENT`), and the responding-model badge rendering.
5. **GAP-I18N**: English + Spanish translations for every user-facing string on the four A2UI surfaces and the responding-model badge label.

### 1.3 Out-of-scope (deferred)
- License refresh / weekly Hugging Face re-enrichment scheduler (separate slice).
- Settings-page "Manage model registry" deep-link (optional polish, tracked in parent spec §4.7 but not gated on this spec).
- Admin middleware re-hardening (already landed in `cf916cd`).
- Further A2UI catalog entries — `REQ-A2UI-002` forbids them; this spec upholds that.

### 1.4 Audience
Backend (Go/CPN) engineer, frontend (React) engineer applying `/vercel-react-best-practices` and `/frontend-design`, QA for vitest + Playwright, localization.

### 1.5 Assumptions
- `cpn/hitl.go` `A2UIMarker`, `fireHITL`, timeout plumbing, and `ParentMessageID` linking are stable.
- `internal/driving/httpapi/handler_admin_models.go` endpoints remain at their current paths: `GET|POST /api/v1/admin/models`, `PATCH|GET|DELETE /api/v1/admin/models/{registryID...}`, `POST /api/v1/admin/models/set-default`, `POST /api/v1/admin/models/license-review`.
- `ModelRegistryService` exposes `SetProductDefault`, `ReviewLicense`, `RegisterModel`, `DeleteModel`, each enforcing invariants from parent spec §4.4.
- `REQ-A2UI-001..006` component trees in parent spec §4.6 are the authoritative source for surface shape. This spec does not redefine them.

## 2. Definitions

| Term | Definition |
|------|------------|
| A2UI | Agent-to-UI rendering protocol (v0.8, https://a2ui.org/specification/v0.8-a2ui/). Surfaces are transmitted as JSON prefixed with the `$$a2ui:` marker on an SSE chunk. |
| CPN | Colored Petri Net — the backend execution engine in `back/go-assistant/cpn/`. |
| HITL | Human-In-The-Loop — CPN transition that blocks on a user response channel (`cpn/hitl.go`). |
| `manage-models-flow` | New topology fragment added in GAP-CPN; the five transitions and nine places defined in parent spec §4.5. |
| Model admin surface | The `$$a2ui:`-prefixed content that the user interacts with — one of `ModelCardList`, `RegisterModelForm`, `LicenseReviewCard`, `ConfirmStateChange`, `DefaultSelector`. |
| Stale surface lock | The pattern established in commits `c1e1396` + `d3590bf`: once the user acts on a surface, the backend emits `deleteSurface` + an inert "frozen" replay Card so older bubbles cannot be re-hydrated. |
| Responding-model badge | A `RoundBadge` rendered below assistant message content, displaying the resolved route's model id (e.g. `claude-opus-4-6 · openrouter`) — see `REQ-FE-004`. |
| `/models` slash command | Client-only shortcut already implemented in `useModelAdminFlow.tryHandleSlashCommand`. GAP-CPN does **not** remove it; it remains as a dev fallback but is no longer the primary entry point. |
| `registry_id` | The canonical `vendor/family-version` identifier used as the registry primary key (e.g. `google/gemma-4-31b-it`). |
| `AdminApiError` | Typed error class exported from `services/api.ts`; wraps HTTP status + server-supplied `code` and `message`. |

## 3. Requirements, Constraints & Guidelines

### 3.1 GAP-CPN — Backend CPN trigger

- **REQ-GAP-CPN-001**: A new file `back/go-assistant/cmd/server/topologies_model_registry.go` MUST define the `manage-models-flow` topology fragment per parent spec §4.5 (places `P_MgmtEnter`…`P_MgmtExit`, transitions `t-classify-mgmt`…`t-summarize`). It MUST register with the existing topology registry in `topologies.go` so the CPN executor can dispatch into it.
- **REQ-GAP-CPN-002**: The existing classifier (see `classifierResult` in `topologies.go:69`) MUST be extended (additively) to return `intent = "manage-models"` with a sub-`kind` ∈ `{list, register, toggle, set-default, review-license, delete}`. Classifier prompt edits MUST preserve existing intents; confidence threshold remains `classifierConfidenceThreshold()`.
- **REQ-GAP-CPN-003**: Every A2UI-emitting transition MUST reuse the `A2UIPayloadBuilder` + `fireHITL` pattern from `cpn/hitl.go:111`. New payload builders live in `back/go-assistant/cpn/a2ui_model_admin.go` and produce JSON whose component trees match parent spec §4.6 byte-for-byte (no UI primitives invented here).
- **REQ-GAP-CPN-004**: `t-apply` MUST call `ModelRegistryService` methods; no direct SQL in handler arcs (parent `REQ-CPN-007`).
- **REQ-GAP-CPN-005**: `t-lock-replay` MUST emit `deleteSurface` followed by a frozen replay Card whose Buttons have no `action` wiring, consistent with `d3590bf`. The response row's `parent_message_id` MUST link the user submission to the emitted surface (parent `REQ-CPN-004`).
- **REQ-GAP-CPN-006**: The `t-reask` validation loop MUST cap at 3 rounds (mirrors clarify-loop in `spec-architecture-cpn-iterative-clarification-loop.md`). On exhaustion, the flow routes to `P_MgmtExit` with the localized "no pude validar" summary.
- **REQ-GAP-CPN-007**: Admin gating — the CPN flow MUST only enter when the session's user is in `LIWAISI_ADMIN_EMAILS` (same admin predicate used by `AdminMiddleware`). Non-admin users receive a localized "this feature is admin-only" reply and are routed to `P_MgmtExit` without emitting any management surface.
- **CON-CPN-001**: The `manage-models-flow` MUST be bounded (parent `REQ-CPN-006`) and deadlock-free (parent `REQ-CPN-005`). The accompanying topology test `cmd/server/topologies_model_registry_test.go` MUST assert these properties using the existing CPN test helpers.
- **CON-CPN-002**: The `/models` / `/model` / `/modelos` slash command MUST continue to work as a client-side shortcut (do not remove `useModelAdminFlow.tryHandleSlashCommand`). It falls back to the existing local surface when the CPN path is not yet deployed.

### 3.2 GAP-REG — Frontend RegisterModelForm wiring

- **REQ-GAP-REG-001**: `A2UIMessageRenderer.tsx` MUST render the `RegisterModelForm` surface (parent spec §4.6.2) using existing v0.8 primitives — no new catalog entries (parent `REQ-A2UI-002`, `REQ-FE-001`). Fields: `TextField` (7), `MultipleChoice` with `maxAllowedSelections: 1` (1), `CheckBox` (1), two `Button` (submit, cancel).
- **REQ-GAP-REG-002**: On `submit_register`, the client MUST forward the action to the backend via the normal A2UI `userAction` pipeline — **not** a direct `fetch(POST /admin/models)` call. Parent `REQ-FE-006` forbids the frontend from calling admin endpoints; only the agent's CPN flow may call them.
- **REQ-GAP-REG-003**: The client MUST surface server-side validation errors by re-rendering the same `surfaceId` when the backend emits a `surfaceUpdate` (parent `REQ-A2UI-005`). Preserve existing form field values across re-renders.
- **REQ-GAP-REG-004**: `submit_register` context MUST include every field from parent spec §4.6.2: `registry_id`, `vendor`, `family`, `version`, `display_name`, `hugging_face_id`, `context_length`, `primary_route_adapter`, `primary_route_model_id`, `enable_enrichment`. The renderer MUST cast `context_length` to an integer and `enable_enrichment` to a boolean before dispatch.
- **REQ-GAP-REG-005**: The local `useModelAdminFlow` hook MUST NOT handle `submit_register` — it only handles actions starting with `model:` (its existing namespace). CPN-emitted form actions use the `name` field (e.g. `submit_register`) and flow through the standard SSE `userAction` pipeline, not the client-only shortcut. This clean separation preserves `REQ-FE-006`.
- **GUD-REG-001**: Apply `/vercel-react-best-practices` — prefer controlled form components via the existing `A2UIMessageRenderer` form-state bindings; avoid uncontrolled refs; memoize per-field change handlers; respect React 19 `useTransition` for the submit dispatch so the button can surface a pending state without blocking the main thread.
- **GUD-REG-002**: Apply `/frontend-design` — use the existing design tokens (typography scale, spacing, color roles) already present in `A2UIMessageRenderer.tsx`. Do not introduce new palette entries. Submit button uses the existing `primary` variant; cancel uses `secondary`. Field density matches the onboarding wizard's `ModelStep.tsx` style for visual coherence.

### 3.3 GAP-IND — Responding-model indicator

- **REQ-GAP-IND-001**: `back/go-assistant/cpn/fire_llm.go` MUST populate a new optional field `responding_model` on the FINAL `stream_chunk` (`Done: true`) event. Value is the resolved route's `registry_id` plus adapter hint (`<registry_id> · <adapter>`), e.g. `anthropic/claude-opus-4-6 · openrouter`. Earlier chunks' values MAY be empty. (Parent `REQ-FE-005`.)
- **REQ-GAP-IND-002**: `front/react-assistant/src/types/sse.ts` MUST extend `StreamChunkPayload` with an optional `responding_model?: string`. Existing shape is preserved.
- **REQ-GAP-IND-003**: `front/react-assistant/src/hooks/useChat.ts` MUST capture `responding_model` from the final chunk and persist it on the completed assistant message's `metadata.responding_model`. The reducer action added for this SHOULD reuse `UPDATE_MESSAGE_CONTENT`'s pattern or be explicit; either is acceptable as long as it does not re-render the full message list.
- **REQ-GAP-IND-004**: `MessageBubble.tsx` MUST render a `RoundBadge` below the content when `message.metadata.responding_model` is truthy. The badge SHOULD display only the trailing model id segment (e.g. `claude-opus-4-6 · openrouter`), stripping the vendor prefix for scannability. Tooltip MAY show the full registry id.
- **REQ-GAP-IND-005**: The badge MUST NOT render for user messages, HITL surfaces, or when `responding_model` is absent. Backward-compat: messages created before this change (no metadata) render unchanged.
- **GUD-IND-001**: Apply `/frontend-design` — badge sits bottom-right of the bubble content area with `usageHint=caption` styling. Color variant: `neutral` when the model matches the user's preferred model, `info` when a fallback route was used (future extension — for now always `neutral`).

### 3.4 GAP-TEST — Vitest coverage

- **REQ-GAP-TEST-001**: `front/react-assistant/src/features/chat/modelAdmin/buildModelAdminSurface.test.ts` MUST cover (a) sort order (default first, invokable before non-invokable, then display_name alpha), (b) button visibility rules (no `set-default` on current default; no `delete` on current default; license-status dependent buttons), (c) summary alert severity for mixed unreviewed/blocked states, (d) the `$$a2ui:` marker prefix is present on the returned string.
- **REQ-GAP-TEST-002**: `front/react-assistant/src/features/chat/modelAdmin/useModelAdminFlow.test.tsx` MUST cover (a) `/models` triggers `openSurface` and calls `adminListModels`, (b) `model:set-default` dispatch → `adminSetDefaultModel` called with the correct id → surface refreshed, (c) stale-surface guard: action against a stale message id is swallowed (returns true, no API call), (d) error path: when the admin API throws `AdminApiError`, the surface renders the error alert instead of crashing.
- **REQ-GAP-TEST-003**: `front/react-assistant/src/hooks/useChat.test.ts` (new or extended) MUST cover the `INJECT_LOCAL_MESSAGE` and `UPDATE_MESSAGE_CONTENT` reducer actions — appending an assistant bubble with a given id/content/role; updating content by id without touching unrelated messages; update-by-unknown-id is a no-op.
- **REQ-GAP-TEST-004**: `MessageBubble.test.tsx` MUST cover (a) badge renders when `metadata.responding_model` set, (b) badge does not render for user role, (c) badge does not render when metadata absent.
- **REQ-GAP-TEST-005**: An integration-level vitest (may live in `front/react-assistant/src/features/chat/__tests__/modelAdmin.integration.test.tsx`) MUST assert the full client loop: slash command → loading surface → rendered card list → set-default click → refreshed surface with new default badge.
- **REQ-GAP-TEST-006**: Backend — `cmd/server/topologies_model_registry_test.go` MUST cover (a) classifier emits `manage-models` intent for each seeded utterance (`"list my models"`, `"lista mis modelos"`, `"registra un modelo"`, `"bloquea gpt-5"`), (b) `t-emit-list` produces a chunk whose content starts with `$$a2ui:` and whose parsed JSON has `surfaceId` matching `/^model-registry:list:/`, (c) `t-recv-response` accepts a well-formed `userAction`, (d) admin-gating: non-admin session ends in `P_MgmtExit` with the localized admin-only message, no surface emitted.
- **CON-TEST-001**: Coverage threshold — the existing project-level threshold applies. New files MUST meet or exceed 85% line coverage (user's standard per dev-workflow memory). No new file is committed without at least one test.

### 3.5 GAP-I18N — Translations (en, es)

- **REQ-GAP-I18N-001**: `front/react-assistant/src/i18n/locales/en/chat.json` and `es/chat.json` MUST each gain a `models` namespace with the following keys, at minimum:
  - `models.list.title` (card-list heading)
  - `models.list.subtitle` (one-line guidance about approved-license gating)
  - `models.list.summary.total` / `.invokable` / `.needs_review` / `.default`
  - `models.list.actions.set_default` / `.approve_commercial` / `.approve_non_commercial` / `.block` / `.delete` / `.refresh`
  - `models.form.title` / `.intro` / `.submit` / `.cancel`
  - `models.form.fields.registry_id` / `.vendor` / `.family` / `.version` / `.display_name` / `.hugging_face_id` / `.context_length` / `.primary_route_adapter` / `.primary_route_model_id` / `.enable_enrichment`
  - `models.confirm.title_delete` / `.title_setdefault` / `.title_license` / `.before_label` / `.after_label` / `.irreversible_notice`
  - `models.errors.admin_only` / `.validation_failed_cap` (the 3-round cap exit message)
  - `models.badge.responding_model` (label prefix when composing the badge tooltip)
- **REQ-GAP-I18N-002**: Spanish translations MUST use the `es` regional variant already in use (neutral Latin American Spanish — see `spec/spec-design-regional-language-variant.md`). No `tú/vos` switching within a single string.
- **REQ-GAP-I18N-003**: Backend literal strings inside A2UI surface JSON (parent spec §4.6 uses Spanish literals like `"Registrar un modelo nuevo"`) MUST be swapped to look up the same i18n keys via the existing server-side localization layer (if present) OR, if no server-side i18n exists, the backend MUST accept a `locale` hint (from the session) and emit the appropriate literal. A helper `localizedLiteral(key, locale) string` SHOULD live in `back/go-assistant/cpn/a2ui_model_admin.go`.
- **REQ-GAP-I18N-004**: Every frontend string added in GAP-IND / GAP-REG MUST be looked up through the existing `useNamespace('chat')` pattern. No hard-coded English strings in JSX.
- **GUD-I18N-001**: Key naming MUST follow the existing convention in `chat.json` (lowercase dot paths, singular nouns for leaf values). Do not introduce ICU message format where a plain string suffices.

### 3.6 Cross-cutting constraints

- **CON-X-001**: No destructive migrations. This spec adds code only — no schema changes beyond what has already landed in `ef21759` migration 020.
- **CON-X-002**: Commits MUST follow the existing conventional-commit convention in the branch (`feat(registry): ...`, `test(registry): ...`, `chore(i18n): ...`). One commit per gap is preferred; per `feedback_autonomous_feature_work` memory, do not stall between slices.
- **CON-X-003**: Branch stays `feat/model-registry-foundation`. PR is opened at the end per existing workflow.
- **PAT-X-001**: Mirror the stale-surface replay pattern from commits `c1e1396` and `d3590bf` exactly — the CPN emits `deleteSurface` then a frozen Card with inert Buttons. The frontend already handles this.

## 4. Interfaces & Data Contracts

### 4.1 Extended classifier result

Existing struct at `cmd/server/topologies.go:69` — extended additively:

```go
type classifierResult struct {
    Intent             string   `json:"intent"`              // adds: "manage-models"
    ManageKind         string   `json:"manage_kind,omitempty"` // NEW: list|register|toggle|set-default|review-license|delete
    ManageArgs         map[string]any `json:"manage_args,omitempty"` // NEW: pre-extracted args (e.g. registry_id)
    NeedsClarification bool     `json:"needs_clarification"`
    Missing            []string `json:"missing"`
    Confidence         *float64 `json:"confidence,omitempty"`
}
```

Existing intents (`task`, `direct`) continue to work unchanged. Absence of `ManageKind` when `Intent="manage-models"` means classifier is uncertain — route to `t-emit-list` as safe default.

### 4.2 Extended `StreamChunkPayload` (SSE)

```ts
// front/react-assistant/src/types/sse.ts — additive
export interface StreamChunkPayload {
  message_id: string;
  content: string;
  done: boolean;
  // NEW — populated only on the final chunk of an LLM transition:
  responding_model?: string; // e.g. "anthropic/claude-opus-4-6 · openrouter"
}
```

### 4.3 ChatMessage metadata

```ts
export interface ChatMessage {
  // ...existing fields...
  metadata?: {
    responding_model?: string; // NEW — set from final stream_chunk
  };
}
```

### 4.4 i18n namespace shape (abbreviated example)

`en/chat.json` (additive):

```json
{
  "models": {
    "list": {
      "title": "Model registry",
      "subtitle": "Pick an action. Only models with an approved license can become the product default.",
      "summary": {
        "total": "{{count}} registered",
        "invokable": "{{count}} invokable",
        "needs_review": "{{count}} awaiting license review",
        "default": "default: {{name}}"
      },
      "actions": {
        "set_default": "Set as default",
        "approve_commercial": "Approve commercial",
        "approve_non_commercial": "Approve non-commercial",
        "block": "Block",
        "delete": "Delete",
        "refresh": "Refresh"
      }
    },
    "errors": {
      "admin_only": "Model management is available to admins only.",
      "validation_failed_cap": "I couldn't validate the form after 3 attempts. You can resume whenever you like."
    },
    "badge": {
      "responding_model": "Responding: {{model}}"
    }
  }
}
```

`es/chat.json` mirrors the shape with Spanish copy (`"Registrar un modelo nuevo"`, `"¿Eliminar este modelo?"`, etc.), matching the literals already present in parent spec §4.6 JSON samples.

### 4.5 CPN topology registration point

The new fragment MUST register through the same seam the existing topologies use. Concretely, `topologies.go` currently exports a registration map; `topologies_model_registry.go` MUST add its entry via the same idiom (import-side-effect `init()` or explicit constructor, whichever the file currently uses — read `topologies.go` at implementation time and follow the existing pattern, do not invent a new one).

### 4.6 Admin-gating predicate

Reuse `internal/driving/httpapi.isAdminEmail(email string) bool` or its equivalent from `AdminMiddleware`. If the function is not exported, export it (non-breaking) or relocate it to a shared `internal/auth/admin.go`.

## 5. Acceptance Criteria

### 5.1 GAP-CPN

- **AC-CPN-001**: Given an admin user types `"lista mis modelos"`, When the classifier runs, Then `intent = "manage-models"`, `manage_kind = "list"`, and within 3 SSE events a `stream_chunk` arrives whose content begins with `$$a2ui:` and whose parsed JSON `surfaceId` matches `/^model-registry:list:/`. (Mirrors parent AC-CPN-001.)
- **AC-CPN-002**: Given a `ModelCardList` is rendered and the user clicks `set_default` on a non-default row, Then the CPN receives a `userAction` with `name = "set_default"` and `registry_id = <row>`, emits a `ConfirmStateChange` Modal, and on accept issues `ModelRegistryService.SetProductDefault`. (Mirrors parent AC-CPN-002.)
- **AC-CPN-003**: Given a non-admin user types `"registra un modelo"`, When the classifier emits `intent = "manage-models"`, Then the CPN routes directly to `P_MgmtExit` with the localized admin-only message; no `$$a2ui:` surface is emitted.
- **AC-CPN-004**: Given a user submits `RegisterModelForm` with `registry_id = ""`, Then the agent emits `surfaceUpdate` re-rendering the same `surfaceId` with an inline `Text` error above `fld_registry_id`; form values are preserved; session stays `running`.
- **AC-CPN-005**: Given 3 consecutive validation failures on `RegisterModelForm`, Then the flow routes to `P_MgmtExit` with the localized cap message and no mutation persists.

### 5.2 GAP-REG

- **AC-REG-001**: Given the agent emits a `RegisterModelForm`, When the user types into `fld_registry_id`, Then the value is bound to `/form/registry_id` in the surface's data model (the existing A2UI state binding mechanism) — no prop-drilling into the renderer.
- **AC-REG-002**: Given the user clicks "Registrar", Then the client emits one `userAction` with `name = "submit_register"` and context containing all 10 fields per REQ-GAP-REG-004; `context_length` is a number (not a string); `enable_enrichment` is a boolean.
- **AC-REG-003**: Given the server re-emits the same `surfaceId` via `surfaceUpdate`, Then the client renders the updated tree inline in the same message bubble without appending a new message.

### 5.3 GAP-IND

- **AC-IND-001**: Given an assistant message completes with `responding_model = "google/gemma-4-31b-it · openrouter"` on the final `stream_chunk`, Then `MessageBubble` renders a `RoundBadge` reading `gemma-4-31b-it · openrouter` below the content.
- **AC-IND-002**: Given a pre-existing assistant message (stored before this change) is rehydrated from session history, Then `MessageBubble` renders without error and without the badge.
- **AC-IND-003**: Given a user message, Then no badge renders regardless of metadata.

### 5.4 GAP-TEST

- **AC-TEST-001**: `npm --prefix front/react-assistant test -- --run` passes all new files with ≥ 85% line coverage on `buildModelAdminSurface.ts`, `useModelAdminFlow.ts`, the new `useChat.ts` reducer actions, and the badge branch in `MessageBubble.tsx`.
- **AC-TEST-002**: `go test ./cmd/server/... ./cpn/...` passes, including `topologies_model_registry_test.go` asserting bounded + deadlock-free properties for the new flow.

### 5.5 GAP-I18N

- **AC-I18N-001**: Every i18n key listed in REQ-GAP-I18N-001 exists in both `en/chat.json` and `es/chat.json` with non-empty values.
- **AC-I18N-002**: A render smoke test switching locale from `en` to `es` via the existing i18n context re-renders the `ModelCardList` summary alert, buttons, and confirm modal with Spanish copy.
- **AC-I18N-003**: No hard-coded user-visible strings remain in `buildModelAdminSurface.ts` client-side builder — all come from the i18n layer (note: the backend CPN builder is still authoritative for surfaces it emits; the client builder covers only the legacy `/models` shortcut path).

## 6. Test Automation Strategy

- **Test Levels**: Unit (vitest, Go table-driven), Integration (vitest + mocked SSE, Go CPN flow tests), E2E (Playwright for the happy path).
- **Frameworks**:
  - Frontend: Vitest + React Testing Library + MSW for network mocks.
  - Backend: stdlib `testing` + existing CPN test helpers (`cpn/cpn_test.go` fixtures) + `httptest` for handler layer (already in place from `cf916cd`).
- **Test Data Management**: Reuse the in-memory `fakeRegistry` introduced in `cf916cd` for handler tests. For CPN tests, seed `ModelRegistryPort` with a static fixture (`fixtures/model_registry_seed.json`) keyed by `registry_id`.
- **CI/CD Integration**: GitHub Actions workflow (existing) runs `make test-back` and `make test-front` on every push. GAP-TEST additions MUST NOT increase CI runtime > 10% — use `vi.mock` for the admin API surface rather than spinning up the full backend.
- **Coverage Requirements**: ≥ 85% line coverage on all new frontend files (per user workflow standard). Backend targets ≥ 85% on `cmd/server/topologies_model_registry.go` and `cpn/a2ui_model_admin.go`.
- **Performance Testing**: Not required for this slice. Follow-up issue if CPN classifier latency regresses > 20% on the `manage-models` intent.

## 7. Rationale & Context

1. **Why wire CPN in addition to the slash command?** The parent spec requires an *agent-driven* flow (REQ-CPN-001…007) so that free-form natural language, not only slash commands, can trigger model management. The slash command remains as an escape hatch but is not the committed UX.
2. **Why forbid the frontend from calling `/admin/models` directly from form submissions?** Parent `REQ-FE-006` reserves admin endpoints for the agent. Bypassing the agent would break the audit trail (every mutation is anchored to a CPN turn with a `parent_message_id`). The existing client-only shortcut in `useModelAdminFlow` is tolerated because it is explicitly admin-only and is covered by the same admin email gate on the backend.
3. **Why the responding-model badge now?** Once users can register new models and reassign the default, they need a passive feedback channel confirming which route answered. Without it, fallback routing (parent spec §9) is invisible and erodes trust.
4. **Why 3-round validation cap?** Matches the existing clarify-loop cap from `spec-architecture-cpn-iterative-clarification-loop.md`. Keeps the topology bounded (REQ-CPN-006) and avoids trapping the user in a form loop.
5. **Why server-side i18n for surface literals?** The agent emits the surface JSON; the frontend only renders it. If the literals are English-only, non-English admins see mixed-language UI. A locale-aware backend builder is the minimum viable fix.
6. **Why Vitest coverage mandatory, not optional?** Per user workflow memory, coverage ≥ 85% is a quality gate. Shipping the hook + builder + reducer without tests violates the gate and the `feedback_autonomous_feature_work` expectation that a slice is "testable" before report.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: Hugging Face Hub — consulted at registration time when `enable_enrichment=true` to populate license metadata. Already wired in existing registry code.
- **EXT-002**: OpenRouter — primary adapter for most registry entries. Already integrated via `infra/openrouter`.

### Third-Party Services
- **SVC-001**: Anthropic, OpenAI, Google AI — optional direct routes per registered model. Used by `cpn/fire_llm.go` when `primary_route_adapter` selects them.

### Infrastructure Dependencies
- **INF-001**: Postgres ≥ 15 with migration 020 applied (already committed).

### Data Dependencies
- **DAT-001**: Classifier prompt file (location: wherever `t-classify` reads from in `topologies.go` — confirm at implementation time) — must be updated additively with the `manage-models` intent and its seeded utterances.

### Technology Platform Dependencies
- **PLT-001**: Go 1.22+ (existing).
- **PLT-002**: Node 20+ / React 19 / Vite (existing frontend toolchain).
- **PLT-003**: A2UI v0.8 — frozen for this spec; no new catalog entries.

### Compliance Dependencies
- **COM-001**: License-state machine from parent spec §4 — all mutations go through `ModelRegistryService` which enforces the invariants (REQ-LIC-*).

## 9. Examples & Edge Cases

### 9.1 Happy path (list → set-default)

```
User: "cámbiame el modelo por defecto a claude opus 4.6"
 ↓ t-classify-mgmt → intent=manage-models, manage_kind=set-default, manage_args={candidate: "anthropic/claude-opus-4-6"}
 ↓ t-emit-setdefault → $$a2ui:{"surfaceId":"model-registry:confirm:..."} (ConfirmStateChange)
User clicks "Confirmar"
 ↓ t-recv-response → payload={action:"apply_confirm", op:"set-default", registry_id:"anthropic/claude-opus-4-6"}
 ↓ t-validate → passes
 ↓ t-apply → ModelRegistryService.SetProductDefault("anthropic/claude-opus-4-6")
 ↓ t-lock-replay → deleteSurface + frozen replay Card
 ↓ t-summarize → "Listo — Claude Opus 4.6 es ahora el modelo por defecto."
```

### 9.2 Register-new-model with server-side validation

```
User: "registra un modelo nuevo"
 → RegisterModelForm emitted (all fields empty)
User submits with registry_id="" (missing)
 → t-validate fails → t-reask loops back to t-emit-register → surfaceUpdate with inline error Text above fld_registry_id
User fills registry_id, submits again
 → t-validate passes → t-emit-confirm (ConfirmStateChange showing the new row)
User confirms
 → t-apply → ModelRegistryService.RegisterModel(...) (forces lifecycle=registered, license=unreviewed per REQ-LIC-003)
 → t-lock-replay → t-summarize → "Modelo registrado. Pendiente de revisión de licencia."
```

### 9.3 Non-admin attempt

```
User (non-admin): "bloquea gpt-5"
 → t-classify-mgmt → intent=manage-models, kind=review-license
 → admin gate → route to P_MgmtExit with i18n key models.errors.admin_only
 → assistant bubble: "Model management is available to admins only."
 → no $$a2ui: surface emitted
```

### 9.4 Responding-model badge (fallback visible)

```
User has preferred_model = "anthropic/claude-opus-4-6"
Anthropic direct route fails → OpenRouter fallback succeeds
Final stream_chunk: { done: true, responding_model: "anthropic/claude-opus-4-6 · openrouter" }
MessageBubble renders: content + <RoundBadge>claude-opus-4-6 · openrouter</RoundBadge>
```

### 9.5 Edge: rapid successive `set_default` clicks

The existing `currentIdRef` guard in `useModelAdminFlow.tryHandleA2UIAction` protects the client-only shortcut path. For the CPN path, the `t-lock-replay` step makes older surfaces inert, so the server-side equivalent is already enforced. Tests MUST cover both paths.

### 9.6 Edge: stream_chunk arrives without responding_model

Older messages, rehydrated sessions, or errors that terminate before the LLM transition emit no `responding_model`. `MessageBubble` branches on `message.metadata?.responding_model` — no crash, no badge.

## 10. Validation Criteria

1. All acceptance criteria in §5 pass on CI.
2. `npm --prefix front/react-assistant test -- --run` green; coverage ≥ 85% on all new/modified files.
3. `go test ./...` green; `topologies_model_registry_test.go` asserts boundedness + deadlock-freedom.
4. Manual walkthrough in the deployed dev environment:
   - `"list my models"` → ModelCardList renders
   - Click `set-default` on a non-default row → ConfirmStateChange → Accept → replay card shows new default
   - `"registra un modelo nuevo"` → RegisterModelForm → submit missing field → inline error
   - Fill form, submit → ConfirmStateChange → Accept → summary
   - Send any prompt → assistant reply shows responding-model badge
   - Switch UI to Spanish → all surfaces render in Spanish
   - Log in as non-admin → `"bloquea gpt-5"` yields the admin-only message with no surface
5. No changes to `A2UIMessageRenderer.tsx` catalog (REQ-A2UI-002 guard).
6. No frontend call to `/admin/models` originating from a RegisterModelForm submit path (REQ-FE-006 guard).

## 11. Team & Delegation

Execute this spec with `/handle-issue`-style team assembly. Suggested composition:

| Role | Scope | Skills to invoke |
|------|-------|------------------|
| Backend Engineer (Go / CPN) | GAP-CPN topology fragment, classifier extension, `fire_llm.go` responding-model field, admin-gating reuse | `/golang-pro` for idiomatic Go + table-driven CPN tests |
| Frontend Engineer (React) | GAP-REG rendering, GAP-IND badge, GAP-TEST vitest, `useChat` reducer updates | `/vercel-react-best-practices` for hooks/transitions, `/frontend-design` for badge + form polish |
| A2UI / Designer | Validate `RegisterModelForm` + `ConfirmStateChange` visual hierarchy; ensure no new catalog entries | `/frontend-design` |
| QA Engineer | Vitest suites, Go topology tests, Playwright happy path | — |
| Localization | `en/chat.json` + `es/chat.json` keys per REQ-GAP-I18N-001; verify regional variant per `spec-design-regional-language-variant.md` | — |

Coordination notes:

- Commit per gap (`feat(registry): cpn manage-models flow (GAP-CPN)`, `feat(registry): register-model surface + responding-model badge (GAP-REG, GAP-IND)`, `test(registry): vitest + cpn coverage (GAP-TEST)`, `chore(i18n): model admin strings en/es (GAP-I18N)`).
- Per `feedback_autonomous_feature_work`: run the full chain end-to-end before reporting; do not pause between slices.
- User pushes manually per `feedback_git_push` — do not run `git push`.

## 12. Related Specifications / Further Reading

- [`spec/spec-architecture-model-registry-and-a2ui-management.md`](spec-architecture-model-registry-and-a2ui-management.md) — parent spec; authoritative for REQ-CPN-*, REQ-FE-*, REQ-A2UI-*.
- [`spec/spec-architecture-model-selection-centralization.md`](spec-architecture-model-selection-centralization.md) — cascade resolution (role → user → product default).
- [`spec/spec-architecture-block20-llm-streaming.md`](spec-architecture-block20-llm-streaming.md) — SSE stream model; GAP-IND extends the final `stream_chunk` with `responding_model`.
- [`spec/spec-architecture-cpn-iterative-clarification-loop.md`](spec-architecture-cpn-iterative-clarification-loop.md) — reference for the 3-round validation cap in `t-reask`.
- [`spec/spec-process-bugfix-a2ui-hitl-response-persistence.md`](spec-process-bugfix-a2ui-hitl-response-persistence.md) — `ParentMessageID` linking pattern.
- [`spec/spec-design-regional-language-variant.md`](spec-design-regional-language-variant.md) — Spanish regional variant guidance for REQ-GAP-I18N-002.
- A2UI v0.8 specification — https://a2ui.org/specification/v0.8-a2ui/
- Hugging Face gated-model consent pattern — https://huggingface.co/docs/hub/en/models-gated
