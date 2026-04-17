---
title: Database-backed LLM Model Registry with A2UI-driven In-Chat Management
version: 1.0
date_created: 2026-04-17
last_updated: 2026-04-17
owner: backend + frontend + ux
tags: [architecture, llm, model-registry, a2ui, cpn, licensing, openrouter, huggingface, postgres]
---

# Introduction

Today the set of LLM models brae can invoke is hardcoded in Go source (`back/go-assistant/infra/openrouter/openrouter.go:20-65`): a product default constant (`PRODUCT_DEFAULT_MODEL = "google/gemma-4-31b-it"`), a `DefaultModelRegistry` role map, a 9-entry `AvailableModels` list, and a `ModelCosts` pricing table (`openrouter.go:81-90`). Adding, removing, re-pricing, deprecating, or disabling a model requires a code change, a compile, and a release. Users cannot register a provider we do not already ship (Gemini direct, OpenAI direct, Anthropic direct, self-hosted models). The system has no concept of a model license, so operators cannot gate commercial use on license clearance, and users cannot see or accept a model's terms before use. There is no runtime lifecycle — a model is either in the slice or it is not.

This specification replaces that frozen catalog with a **Postgres-backed registry** whose rows carry rich OpenRouter-shaped metadata augmented with license information sourced from the Hugging Face Hub API (for open-weight models) or hand-curated vendor-terms entries (for proprietary API-only models). It introduces two orthogonal state machines — **administrative lifecycle** (`discovered → pending-license-review → registered → active ⇄ disabled; active → deprecated → sunset → removed`) and **license status** (`unreviewed → review-in-progress → approved-commercial | approved-non-commercial | restricted | blocked | unknown`) — with an explicit runtime gating rule: a model is only invokable when `lifecycle = active` AND `license.status ∈ {approved-commercial, approved-non-commercial, restricted}`. It enforces a singleton invariant — **exactly one model is the product default at all times**, and the default cannot be deleted or deactivated without first reassigning it.

The registry is exposed two ways: (1) REST endpoints under `/api/v1/admin/models` for programmatic management, and (2) a conversational management flow in which the CPN agent emits A2UI v0.8 surfaces the user interacts with directly in chat — listing models, registering new ones, toggling state, reviewing licenses, and reassigning the default. This dogfoods A2UI v0.8 for a non-trivial CRUD workload and hardens the extended surface catalog already established by the clarify-loop work (recent commits `1dab111`, `8a9a051`, `d3590bf`, `c1e1396`).

## 1. Purpose & Scope

### Purpose

1. Move the model catalog from source code to `store/postgres/` — a new `models` table and a `ModelRegistry` port/adapter pair following the existing hexagonal layout (`back/go-assistant/cpn/`, `internal/app/`, `infra/`, `store/postgres/`).
2. Enrich every registry row with OpenRouter-shaped metadata (canonical_slug, hugging_face_id, modalities, pricing-as-decimal-strings, capabilities, context length, tokenizer, supported_parameters, per-provider `routes[]`) plus **license** (the single field OpenRouter's `/api/v1/models` does not expose — confirmed by the 345-row live sample).
3. Add two orthogonal state machines (lifecycle, license status) and a runtime gate that rejects invocation of any model not simultaneously `active` and license-approved.
4. Enforce the singleton-default invariant at the database layer (unique partial index) and service layer (cascade rules).
5. Ship a conversational in-chat management flow — the agent emits A2UI v0.8 surfaces (`ModelCardList`, `RegisterModelForm`, `LicenseReviewCard`, `ConfirmStateChange`, `DefaultSelector`) and receives user actions via the existing `$$a2ui:` + `userAction` pipeline (see `back/go-assistant/cpn/hitl.go:100-187`, `front/react-assistant/src/features/chat/a2ui/A2UIMessageRenderer.tsx`).
6. Preserve backward compatibility with the two existing consumers of `GET /api/v1/models` — `ModelStep.tsx` (onboarding) and `SettingsPage.tsx` — so no frontend release is required purely for the registry cut-over.
7. Surface the actual model that answered each assistant turn, in line with OpenRouter's `response.model` pattern, to eliminate the silent model-drift class of complaints documented across Cursor / ChatGPT / Perplexity pickers.

### In Scope

- Postgres schema (new migration `020_model_registry.up.sql`) + seed from the current hardcoded catalog.
- Domain: `cpn.ModelRegistry` port, `store/postgres/model_registry.go` adapter.
- Service: `internal/app/model_registry_service.go` for admin operations, with cascade + invariant enforcement.
- REST: `GET /api/v1/models` (extended, backward-compatible), `GET /api/v1/admin/models`, `POST /api/v1/admin/models`, `PATCH /api/v1/admin/models/:registry_id`, `DELETE /api/v1/admin/models/:registry_id`, `POST /api/v1/admin/models/:registry_id/set-default`, `POST /api/v1/admin/models/:registry_id/license/review`.
- License enrichment: `infra/huggingface/license.go` adapter calling `GET https://huggingface.co/api/models/{id}` plus a manual-curation code path for proprietary models. Results cached in `models.license_source_raw` (JSONB) with `license_fetched_at`.
- CPN topology for the management flow: places, transitions, guards, token colors, deadlock-freedom and boundedness analysis.
- Five A2UI v0.8 surfaces (catalog extensions documented), one per management intent.
- Runtime gate in `fire_llm.go` / `SessionService.applyUserModelPreferences` that refuses to stamp a non-invokable model.
- Responding-model indicator in the assistant message bubble (frontend).
- i18n (en, es) for every new user-visible string.
- Go tests + Vitest coverage.

### Out of Scope

The following are explicitly **excluded** from this spec. Each is noted as a candidate follow-up:

- **BYOK** (user-supplied API keys). Per memory `project_secrets_model.md`, liwaisi owns OAuth and API keys; users do not bring their own. Follow-up: `spec-architecture-byok-model-credentials.md`.
- **Per-user / per-workspace model visibility.** The registry is shared across the single liwaisi tenant. Follow-up: `spec-architecture-workspace-registry.md`.
- **Automatic ingestion** of OpenRouter's `/api/v1/models` feed on a schedule. Manual registration via REST / A2UI is the MVP. Follow-up: `spec-process-openrouter-registry-sync.md`.
- **Multi-pane compare / Council mode** (Perplexity / OpenRouter / T3 Chat patterns). Follow-up: `spec-design-model-compare-pane.md`.
- **Favorites / pinning** in the picker. Deferred.
- **Audit trail for license acceptance** beyond the row-level `license_reviewed_at` / `license_reviewed_by` columns. No PDF, no immutable log. Follow-up: `spec-process-license-acceptance-audit.md`.
- **Per-model MCP tool capability** (which tools a given model can drive reliably). Orthogonal; follow-up: `spec-architecture-mcp-model-tool-matrix.md`.
- Redesigning the onboarding wizard itself beyond adjusting `ModelStep.tsx` to read the new, richer `/api/v1/models` response.

### Audience

Backend, frontend, and UX engineers implementing the feature. The document is written so an AI code-generation agent can implement each REQ-* independently and verify against the AC-* acceptance criteria without needing the original conversation transcript.

### Stakeholders / Expert Panel

| Role | Responsibility for this spec |
|------|------------------------------|
| **CPN mathematician** | Owns §4.5 (CPN topology) — designs places, transitions, guards, token colors for the management flow. Proves deadlock-freedom and boundedness. |
| **Go backend engineer** | Owns §4.1–§4.4 (schema, repo, service, REST). Seed migration. Runtime gate. |
| **React engineer** | Owns §4.6–§4.7 (A2UI surfaces, catalog extensions, responding-model indicator). Vitest coverage. |
| **UX designer** | Owns §7.3 (information architecture). Ensures the default picker is a radio not a checkbox. Paces decision-fatigue mitigations (pre-selected default, progressive disclosure). |
| **Legal / compliance reviewer (virtual)** | Ratifies the license-status vocabulary (§3 REQ-LIC-*) and the gating rule. Documents which licenses are commercially safe for brae's use. |

## 2. Definitions

| Term | Definition |
|------|------------|
| **Registry row / ModelRegistryEntry** | A single row in the `models` Postgres table representing one invocable LLM identity. Primary key: `registry_id`. |
| **registry_id** | Stable internal primary key. When an OpenRouter `canonical_slug` is available it is reused verbatim (e.g. `anthropic/claude-4.7-opus-20260416`); otherwise `<vendor>/<family>-<version>`. Never includes variant suffixes (`:free`, `:nitro`) — variants live in `routes[]`. |
| **canonical_slug** | OpenRouter's dated, stable model slug. Copied into `registry_id` when present. |
| **id (OpenRouter)** | OpenRouter's short, user-facing model identifier (`anthropic/claude-opus-4.7`). May include routing variant suffixes. Stored on a `routes[]` entry, never as the registry PK. |
| **hugging_face_id** | The Hugging Face repository identifier (e.g. `meta-llama/Llama-3.3-70B-Instruct`) used to fetch license metadata. `NULL` / empty for proprietary API-only models. |
| **Lifecycle state** | The administrative state of the registry row. Enum: `discovered`, `pending-license-review`, `registered`, `active`, `disabled`, `deprecated`, `sunset`, `removed`. |
| **License status** | The legal-clearance state of the model. Enum: `unreviewed`, `review-in-progress`, `approved-commercial`, `approved-non-commercial`, `restricted`, `blocked`, `unknown`. |
| **Invokable** | A model for which both `lifecycle = active` AND `license.status ∈ {approved-commercial, approved-non-commercial, restricted}`. Only invokable models may be stamped onto an `LLMConfig.Model`. |
| **Product default** | The single registry row referenced by `registry_config.product_default_model_id`. Exactly one at all times, guaranteed by a singleton `registry_config` table with NOT NULL FK and `CHECK (id = 1)` (REQ-REG-008). Replaces the `PRODUCT_DEFAULT_MODEL` compile-time constant from `spec-architecture-model-selection-centralization.md:REQ-CFG-001`. |
| **Route** | A `routes[]` JSONB element describing one way to invoke a model (`provider_adapter`, `provider_model_id`, `endpoint_base_url`, `priority`, `enabled`, `region`, `is_moderated`). Enables `openrouter`, `anthropic`, `openai`, `google-gemini`, `vertex`, `bedrock`, `azure-openai`, `groq`, `together`, `fireworks`, `self-hosted` per-model. |
| **A2UI** | Agent-to-UI protocol. brae uses v0.8 of the spec — see `https://a2ui.org/specification/v0.8-a2ui/`. Surfaces are streamed over SSE as JSON payloads whose first bytes on the wire are `$$a2ui:` (see `A2UI_MARKER` in `front/react-assistant/src/features/chat/a2ui/constants.ts`, and `A2UIMarker` at `back/go-assistant/cpn/hitl.go:14`). |
| **Surface** | An A2UI interactive JSON payload identified by `surfaceId`. Persists across stream restarts; replaced via `surfaceUpdate`, torn down via `deleteSurface`. |
| **userAction** | The v0.8 top-level message key used by the client to send a user-triggered action (and its resolved `context`) back to the agent. Renamed to `action` in v0.9; brae is on v0.8. |
| **HITL** | Human-in-the-loop: a `NodeKindHITL` CPN transition that blocks on a channel read until the user submits a response. |
| **License enrichment** | The procedure that populates `license.*` fields from upstream metadata (Hugging Face `cardData.license` / `license_name` / `license_link` / `tags[license:*]`) or from manual vendor-terms curation when `hugging_face_id` is empty. |
| **Responding model** | The `provider_model_id` of the `routes[]` entry actually used for a given LLM call. Surfaced on the assistant message bubble to prevent silent model drift (per SOTA research; see `https://openrouter.ai/docs/guides/routing/model-fallbacks`). |

## 3. Requirements, Constraints & Guidelines

### Registry storage (REQ-REG-*)

- **REQ-REG-001**: A new Postgres table `models` MUST be created by migration `back/go-assistant/store/postgres/migrations/020_model_registry.up.sql`. Columns, types, and constraints as specified in §4.2. A matching `.down.sql` MUST drop the table (after detaching the FK from `users.preferred_model`, which remains a TEXT column; no FK is introduced because legacy string values must continue to resolve).
- **REQ-REG-002**: Each model MUST be addressed by a stable string primary key `registry_id`. When ingesting an OpenRouter entry, `registry_id` MUST equal `canonical_slug` verbatim. When registering a model manually, the operator supplies `registry_id` of the form `<vendor>/<family>-<version>` — enforced by CHECK constraint matching `^[a-z0-9][a-z0-9._-]*\/[a-z0-9][a-z0-9._-]*$`.
- **REQ-REG-003**: The `models` table MUST store, in the shape specified in §4.1 (JSON Schema): `vendor`, `family`, `version`, `variant`, `display_name`, `description`, `hugging_face_id`, `modalities`, `context`, `pricing`, `capabilities`, `supported_parameters`, `default_parameters`, `license`, `lifecycle`, `routes`, `source_metadata`, `created_at`, `updated_at`. The boolean `is_product_default` on the API shape is **derived** (not a table column) — see REQ-REG-008.
- **REQ-REG-004**: Pricing values MUST be stored as **NUMERIC(20,12)** columns per-token (`pricing_input_per_token`, `pricing_output_per_token`, `pricing_cache_read_per_token`, `pricing_cache_write_per_token`, `pricing_reasoning_per_token`, `pricing_image_per_input`, `pricing_image_per_output`, `pricing_audio_per_input_unit`, `pricing_audio_per_output_unit`, `pricing_web_search_per_call`, `pricing_request_flat`). Display-time conversion to per-1M is the caller's job. This mirrors OpenRouter's decimal-string convention (`https://openrouter.ai/docs/api/api-reference/models/get-models`) while avoiding float drift.
- **REQ-REG-005**: The `routes` column MUST be `JSONB NOT NULL DEFAULT '[]'` holding the array of route objects described in §2 and §4.1. A GIN index on `routes` is required for `provider_adapter`-prefixed queries.
- **REQ-REG-006**: The `source_metadata` column MUST be `JSONB NOT NULL DEFAULT '{}'` holding the raw OpenRouter row (`openrouter_raw`) and raw Hugging Face card (`huggingface_raw`) captured at enrichment time, plus `fetched_at` timestamps. This is the audit trail referenced by REQ-LIC-005.
- **REQ-REG-007**: `lifecycle` MUST be stored as a Postgres ENUM (`model_lifecycle_state`) defined inside the migration. `license.status` MUST be stored as a Postgres ENUM (`model_license_status`). ENUMs (not CHECK-constraints on TEXT) are required so downstream BI / read-replicas inherit the type safety.
- **REQ-REG-008**: The singleton-default invariant MUST be enforced via a dedicated `registry_config` singleton table (exactly one row, guarded by `CHECK (id = 1)`) whose `product_default_model_id UUID NOT NULL REFERENCES models(id) ON DELETE RESTRICT` column points at the current default. This enforces **both** halves of the invariant at the database level: `NOT NULL` guarantees "at least one" and `UNIQUE` on the single row guarantees "at most one"; `ON DELETE RESTRICT` prevents accidental deletion of the pointed-to model. Rationale: a partial unique index (earlier draft) only enforces "at most one" and can silently leave the registry with zero defaults after a stray `UPDATE ... SET is_product_default=FALSE`. The `models` table therefore MUST NOT carry an `is_product_default` column. `ModelRegistryEntry.is_product_default` in the JSON schema is a derived boolean computed in the repo adapter via `LEFT JOIN registry_config ON registry_config.product_default_model_id = models.id`. DDL in §4.2.
- **REQ-REG-009**: The table MUST have secondary indexes on `(vendor, family)`, `(lifecycle)`, and `((license->>'status'))` (if license is stored as JSONB) or on the dedicated license status column. Exact DDL in §4.2.
- **REQ-REG-010**: Every mutation of a `models` row MUST update `updated_at` via a trigger (`trg_models_set_updated_at`). The trigger MUST be created in the same migration as the table.

### Seed migration (REQ-SEED-*)

- **REQ-SEED-001**: Migration `020_model_registry.up.sql` MUST insert one row per entry of the current hardcoded `AvailableModels` slice in `back/go-assistant/infra/openrouter/openrouter.go:43-54`. Pricing is copied from the `ModelCosts` map (`openrouter.go:81-90`) converted to per-token NUMERIC. Modalities / capabilities / tokenizer / supported_parameters / license fields are populated from a hand-authored `scripts/seed-registry/seed_data.json` committed to the repo. License is **hand-curated per row** per REQ-SEED-006 (not a blanket `unreviewed`) so the upgrade does not silently break a running chat session with models that are known-safe. The `is_product_default` boolean does not appear on the table — see REQ-REG-008; the singleton pointer is set separately in REQ-SEED-002.
- **REQ-SEED-006**: The 9 currently-hardcoded models MUST be seeded with the following curated licenses and lifecycle states. `huggingface_fetched_at` is NULL at seed time (curation is manual; the weekly refresh job from REQ-LIC-004 will enrich HF-backed rows on its first run). Any model in this list whose license is flagged as `unreviewed` MUST seed with `lifecycle = 'registered'` (not `active`) and therefore is NOT invokable until an admin reviews via `POST /api/v1/admin/models/:id/license/review`.

| registry_id | license.kind | license.spdx_id / name | license.status | lifecycle | reviewed_by |
|---|---|---|---|---|---|
| `google/gemma-4-31b-it` | community | `apache-2.0` | approved-commercial | active | `seed-migration-014` |
| `anthropic/claude-opus-4-6` | proprietary-api | Anthropic Commercial Terms | approved-commercial | active | `seed-migration-014` |
| `anthropic/claude-sonnet-4-6` | proprietary-api | Anthropic Commercial Terms | approved-commercial | active | `seed-migration-014` |
| `anthropic/claude-haiku-4-5` | proprietary-api | Anthropic Commercial Terms | approved-commercial | active | `seed-migration-014` |
| `openai/gpt-4o-mini` | proprietary-api | OpenAI Business Terms | approved-commercial | active | `seed-migration-014` |
| `openai/gpt-5-mini` | proprietary-api | OpenAI Business Terms | approved-commercial | active | `seed-migration-014` |
| `google/gemini-2.5-flash` | proprietary-api | Google Generative AI Additional Terms | approved-commercial | active | `seed-migration-014` |
| `meta-llama/llama-3.3-70b-instruct` | community | `llama3.3` | unreviewed | registered | NULL |
| `qwen/qwen-2.5-72b-instruct` | community | `qwen` | unreviewed | registered | NULL |

Rationale: the four Anthropic/OpenAI/Google proprietary rows use well-known commercial API terms that the product already relies upon (parent spec COM-001). Gemma has a permissive community license that has been reviewed and approved as the product default. Llama-3.3 and Qwen ship `unreviewed` because both carry provider-specific community terms (Llama 3.3 Community License, Qwen License) that include acceptable-use clauses and field-of-use restrictions that require a human reviewer sign-off before enabling. An admin MUST approve these before they can be invoked; emergency rollback path is `license.status = 'blocked'` which is rejected by REQ-GATE-001 even for models otherwise `active`.

Emergency rollback: any admin can pull a model by `PATCH /api/v1/admin/models/:id` with `{"license":{"status":"blocked"}}`. The next session-resolution call falls back to the product default and emits the REQ-OBS-004 WARN.
- **REQ-SEED-002**: The seed MUST promote `registry_id = 'google/gemma-4-31b-it'` to `lifecycle = 'active'` and `license.status = 'approved-commercial'` (per parent spec `spec-architecture-model-selection-centralization.md:COM-001`). The seed MUST then `INSERT INTO registry_config (id=1, product_default_model_id=<gemma.id>)` to establish the singleton default. This Gemma row is the only one that enters `active` at seed time unless REQ-SEED-006 curated licenses are applied; all other non-curated rows remain in `registered`.
- **REQ-SEED-003**: The seed MUST preserve the existing role semantics. A sibling table `model_role_defaults` (also created in migration 020) maps each role (`classifier`, `structured`, `reasoning`, `long-context`, `summarize`, `thinking`) to a registry_id. Seed values are all `google/gemma-4-31b-it`, matching the parent spec's `DefaultModelRegistry` state after REQ-CFG-001. Schema in §4.2.
- **REQ-SEED-004**: The `PRODUCT_DEFAULT_MODEL` Go constant in `infra/openrouter/openrouter.go:27` MUST be retained as a **fallback** returned when the registry lookup fails (e.g. database unavailable at startup). The runtime gate described in REQ-GATE-002 still applies; the constant is used only as a last-resort safety net, and its use MUST emit a WARN-level log `"registry unavailable, falling back to compile-time default"`.
- **REQ-SEED-005**: After the migration runs, the existing `resolveForUser` function (`internal/app/session_service.go`, per parent spec `spec-architecture-model-selection-centralization.md:REQ-CFG-005`) MUST be refactored to resolve the product default from `ModelRegistry.GetProductDefault(ctx)` rather than the Go constant. Compile-time constant use is limited to REQ-SEED-004.

### Domain & ports (REQ-DOM-*)

- **REQ-DOM-001**: A new port `cpn.ModelRegistry` MUST be declared in `back/go-assistant/cpn/ports.go` (or a new `cpn/registry.go` file — implementer's choice, must be co-located with other ports) with the interface defined in §4.3.
- **REQ-DOM-002**: A Postgres adapter `store/postgres/model_registry.go` MUST implement `cpn.ModelRegistry` using the existing `pgxpool.Pool` dependency injection pattern already established by `store/postgres/user.go`.
- **REQ-DOM-003**: The domain layer MUST NOT import `infra/openrouter` for model metadata — the registry is the single source of truth. The `infra/openrouter` package retains only the HTTP client concerns (request building, retry, streaming).

### REST API (REQ-API-*)

- **REQ-API-001**: `GET /api/v1/models` MUST remain backward-compatible. Its existing keys (`default`, `available_models`, `available`, `roles`) MUST continue to work so that `front/react-assistant/src/features/setup/steps/ModelStep.tsx:29-56` and `front/react-assistant/src/features/settings/SettingsPage.tsx:60-77` do not require changes in the same release. New keys (`models[]` — full registry rows with lifecycle/license/capabilities) are **additive**.
- **REQ-API-002**: `GET /api/v1/models` MUST filter the returned `available` / `available_models` arrays to **invokable** models only (lifecycle=active AND license approved). Users must not see a model in the dropdown that they cannot actually use.
- **REQ-API-003**: New endpoints under `/api/v1/admin/models` MUST be mounted behind the existing `AdminMiddleware` defined at `back/go-assistant/internal/driving/httpapi/middleware_admin.go:12`. Admin identity is a case-insensitive email match against the `LIWAISI_ADMIN_EMAILS` env var (comma-separated list, parsed by `ParseAdminEmails` at `middleware_admin.go:45`). Non-admins receive HTTP 403 with body `{"error":"admin access required"}` — matching the existing handler's error shape, not a new `ADMIN_REQUIRED` code. All mutations MUST record `updated_by = <user.Email>` for audit (consistent with `handler_admin.go:53`).
- **REQ-API-004**: `POST /api/v1/admin/models` MUST accept the JSON Schema shape in §4.1 minus server-computed fields (`created_at`, `updated_at`, `source_metadata.fetched_at`). Omitted fields default as specified in §4.1. Response is 201 with the fully hydrated row.
- **REQ-API-005**: `PATCH /api/v1/admin/models/:registry_id` MUST accept a partial update. The handler MUST reject any attempt to change product-default status via PATCH (the `is_product_default` field in the request body MUST return HTTP 400 `USE_SET_DEFAULT_ENDPOINT`) — use the dedicated endpoint below.
- **REQ-API-006**: `DELETE /api/v1/admin/models/:registry_id` MUST refuse (HTTP 409, code `CANNOT_DELETE_DEFAULT`) if the row is referenced by `registry_config.product_default_model_id`. The `ON DELETE RESTRICT` at the DB layer is the last line of defense; the service MUST check and return a structured error first. Deletion is a hard-delete of a non-default, non-active row; active rows MUST first be transitioned to `removed` via PATCH. §4.4 documents the state-machine guard.
- **REQ-API-007**: `POST /api/v1/admin/models/:registry_id/set-default` MUST atomically, inside a single SQL transaction: (a) validate the target row is invokable per REQ-GATE-001, auto-promoting `lifecycle` to `active` if currently `registered|disabled` and the license is approved; (b) `UPDATE registry_config SET product_default_model_id = <target.id>, updated_at = NOW(), updated_by = <admin_email> WHERE id = 1`; (c) reject (HTTP 422, code `DEFAULT_NOT_INVOKABLE`) if the target's license is not approved. No other row is mutated — the singleton pointer swap is the only write.
- **REQ-API-008**: `POST /api/v1/admin/models/:registry_id/license/review` MUST accept `{ status, reviewer_id, notes?, commercial_use?, redistribute?, modify?, attribution_required? }`, update the row's `license.*` fields, set `license.reviewed_at = NOW()`, and return the updated row. If the new status is one of the approved statuses and the row is currently `pending-license-review`, the lifecycle MUST auto-transition to `registered`. Auto-activation does NOT happen — that is an explicit admin step (REQ-API-007 or a PATCH to `lifecycle`).
- **REQ-API-009**: All mutation endpoints MUST include a request-level `If-Match` ETag header check using `updated_at` to prevent lost updates when two admins edit concurrently. Mismatch returns HTTP 412 `PRECONDITION_FAILED`.

### Runtime gate (REQ-GATE-*)

- **REQ-GATE-001**: A model is **invokable** iff `lifecycle = 'active'` AND `license.status ∈ {'approved-commercial', 'approved-non-commercial', 'restricted'}`. `deprecated` models are callable but emit a runtime WARN (see REQ-OBS-002). All other states are non-invokable.
- **REQ-GATE-002**: `internal/app/session_service.go::applyUserModelPreferences` MUST, for every `LLMConfig` on the CPN, resolve the effective `registry_id` via the three-level cascade from parent spec `spec-architecture-model-selection-centralization.md:REQ-CFG-005`, then call **`ModelRegistry.GetInvokable(ctx, registry_id)`** (NOT `GetByID` — defense-in-depth per REQ-GATE-004), and select the highest-priority **enabled** route whose `provider_adapter` the backend has a client for. The selected `routes[i].provider_model_id` is stamped onto `LLMConfig.Model`. If `GetInvokable` returns `ErrModelNotInvokable`, the service MUST fall back to the product default (and log the fallback at WARN with `requested_id`, `fallback_id`, and reason). If the product default is itself not invokable (should be impossible per REQ-REG-008 + REQ-API-007), the service MUST fail the session start with HTTP 503 `REGISTRY_DEGRADED`.
- **REQ-GATE-004**: Defense in depth. The gate MUST be enforced at two layers: (a) the **repo** via `GetInvokable` — no transition-firing code path may read non-invokable rows; and (b) the **service** via a `CanInvoke(entry) bool` helper used by the admin UI's read endpoints to mark rows with an "invokable" flag. A transition that forgets to wire the gate is caught because it cannot get a non-invokable entry out of the repo in the first place. Repo adapter tests MUST include a table-driven matrix of (lifecycle, license_status) covering all 56 combinations; exactly the 3 rows per lifecycle=active (license ∈ approved-commercial, approved-non-commercial, restricted) return a model, all others return `ErrModelNotInvokable`.
- **REQ-GATE-003**: The runtime gate MUST log every invocation with `session_id`, `cpn_id`, `transition_id`, `requested_registry_id`, `resolved_registry_id`, `provider_adapter`, `provider_model_id` at INFO. This extends parent spec `spec-architecture-model-selection-centralization.md:REQ-OBS-001` from the old `resolved_model` string to the full route tuple.

### License enrichment (REQ-LIC-*)

- **REQ-LIC-001**: A new package `back/go-assistant/infra/huggingface/` MUST host a `LicenseEnricher` with the interface in §4.3. Its `Enrich(ctx, huggingFaceID)` method calls `GET https://huggingface.co/api/models/{id}` (see `https://huggingface.co/docs/hub/en/api`) and parses, in order of preference: (1) `cardData.license` (canonical SPDX-like slug), (2) `cardData.license_name` + `cardData.license_link` when `cardData.license == "other"`, (3) any `tags[]` entry starting with `"license:"`. It returns a normalized `License` struct plus the raw response for caching in `source_metadata.huggingface_raw`.
- **REQ-LIC-002**: If `hugging_face_id` is empty / null (Anthropic Claude, OpenAI GPT, Google Gemini proprietary tiers, closed-weight xAI), enrichment MUST NOT run and the license MUST be entered manually. The `POST /api/v1/admin/models` payload accepts a `license.manual_entry: true` marker and requires `license.kind = 'proprietary-api'`, `license.name` (e.g. "Anthropic Commercial Terms"), and `license.url` (link to the vendor's terms page). REQ-LIC-003 governs status.
- **REQ-LIC-003**: A newly registered model's initial `license.status` MUST be `unreviewed`. The row's initial `lifecycle` is `pending-license-review`. An admin MUST explicitly call `POST /api/v1/admin/models/:registry_id/license/review` to transition the license to any other status. No automatic approval path exists — even SPDX `apache-2.0` from Hugging Face arrives as `unreviewed`, because "permissively licensed" does not equal "brae has cleared this for production."
- **REQ-LIC-004**: Enrichment responses MUST be cached by writing the raw response into `source_metadata.huggingface_raw` and setting `source_metadata.huggingface_fetched_at = NOW()`. A **weekly background refresh job** MUST run against every model with `hugging_face_id IS NOT NULL AND license_source = 'huggingface' AND huggingface_fetched_at < NOW() - INTERVAL '7 days'`. The refresh writes a new raw payload, updates `huggingface_fetched_at`, and emits a structured log `license_refresh_completed` with `{registry_id, prior_spdx_id, new_spdx_id, status_changed: bool}`. If the refresh detects a license change (new `cardData.license` differs from cached), the row's `license_status` MUST auto-transition to `review-in-progress` and an admin notification MUST be surfaced (implementation: a row inserted into `admin_notifications` — schema deferred to follow-up spec — OR emission of an event to be subscribed by the admin UI). Admin re-enrichment via `POST /api/v1/admin/models/:id/license/refresh` remains available and overwrites the cache on demand. Proprietary / manual-entry models are skipped by the weekly job.
- **REQ-LIC-005**: The `license` JSON subdocument MUST expose both the raw upstream slug (`license.community_slug` — `llama3.3`, `gemma`, `qwen`, `mrl`, `deepseek`) AND the SPDX id (`license.spdx_id` — `apache-2.0`, `mit`, `cc-by-nc-4.0`) when applicable. Downstream UI and policy code prefer the SPDX id; the community slug survives for auditability.
- **REQ-LIC-006**: The license gating rule (REQ-GATE-001) places `restricted` on the **allowed** side but the runtime MUST require the session's caller to explicitly assert consent (`accept_restricted: true` on session-create) for any transition whose resolved model has `license.status = 'restricted'`. If consent is absent, the service falls back to the product default per REQ-GATE-002. This is reserved for models like Mistral MRL where the restriction is "research only" and the brae feature calling the model has been flagged internally as research-only.

### A2UI surface catalog (REQ-A2UI-*)

- **REQ-A2UI-001**: Five new A2UI surfaces MUST be designed for the in-chat management flow: `ModelCardList`, `RegisterModelForm`, `LicenseReviewCard`, `ConfirmStateChange`, `DefaultSelector`. Exact component trees (using only v0.8 primitives plus the already-extended catalog entries) are specified in §4.6.
- **REQ-A2UI-002**: No new catalog extensions are introduced by this spec. Every surface MUST be buildable from v0.8 primitives (Row, Column, List, Card, Modal, Tabs, Text, Image, Icon, Divider, Button, TextField, CheckBox, Slider, MultipleChoice, DateTimeInput) PLUS the extensions already registered in the repo catalog (see `front/react-assistant/src/features/chat/a2ui/A2UIMessageRenderer.tsx:1189-1202`: `text, button, card, code, progress, form, list, divider, alert, badge, choice, questionnaire`) and the chrome components (`RoundBadge`). §4.6 maps each surface to existing primitives — if an implementer finds they need a new primitive, that is a design failure in this spec and MUST be reported before implementation.
- **REQ-A2UI-003**: Every surface MUST use a stable `surfaceId` of the form `model-registry:<intent>:<opaque>` — e.g. `model-registry:list:2026-04-17T12:00:00Z-7a`. One surface per turn. Stale surfaces MUST be locked on submit by emitting `deleteSurface` followed by a frozen read-only replay `Card` whose Buttons have no `action` (consistent with recent commits `c1e1396` — unstall clarify loop and lock surface on submit — and `d3590bf` — persist messages mid-run and lock stale HITL surfaces).
- **REQ-A2UI-004**: Every surface's action Buttons MUST carry a `context` that includes the session-scoped `management_turn_id` and, where relevant, the `registry_id` being acted upon, resolved via `path` binding against the surface's data model (so the template-rendered `ModelCardList` can correctly identify per-row target). Because v0.8 has no `$index`, per-row targets are bound as `{"path": "/id"}` (see §4.6 listing).
- **REQ-A2UI-005**: Required-field validation lives on the server. If the user submits `RegisterModelForm` with a missing required field, the agent MUST emit a `surfaceUpdate` that re-renders the same `surfaceId` with an inline error `Text` node prepended to the offending field's `Column`. This matches the v0.8 pattern documented at `https://a2ui.org/specification/v0.8-a2ui/` §1.4 and is consistent with how questionnaire re-emission works today.
- **REQ-A2UI-006**: Destructive confirmations (delete, disable, set-default) MUST use `ConfirmStateChange` rendered inside a `Modal` with `entryPointChild` = the triggering Button and `contentChild` = the confirm panel. The confirm panel MUST render a **diff**: current state → proposed state, using `Row` + `Text` with `usageHint=caption`.
- **REQ-A2UI-007**: `ModelCardList` MUST be server-paginated with a fixed page size of **20 rows**. The agent's backing query accepts `page` (1-indexed) and `filter` query params; the surface renders a footer `Row` with prev/next Buttons whose `context` includes `{"page": "<N>", "filter": "<current>"}`. Paging emits a `surfaceUpdate` that replaces `/models` in the data model and leaves the surface id stable. Rationale: filterable `MultipleChoice` with 50+ options degrades scan time (SOTA decision-fatigue research); 20 rows/page keeps the card list scrollable on mobile without virtualization. AC-FE-008 covers the pagination contract.

### Conversational management flow — CPN (REQ-CPN-*)

- **REQ-CPN-001**: A new CPN topology fragment `manage-models-flow` MUST be added to `back/go-assistant/cmd/server/topologies.go` (or a new file `topologies_model_registry.go`). The topology implements the places and transitions documented in §4.5.
- **REQ-CPN-002**: The flow is entered when the classifier recognizes a user intent of `manage-models`. The classifier's prompt (currently in `back/go-assistant/cpn/prompts/`) MUST be extended with this intent label; exact prompt text is an implementation detail but MUST include the sample utterances: "list my models", "register a new model", "disable claude haiku", "set gemini 2.5 pro as default", "review the llama 3.3 license", "quiero cambiar mis modelos", "¿qué modelos tengo disponibles?".
- **REQ-CPN-003**: Every HITL transition in the flow MUST use the existing `A2UIPayloadBuilder` pattern (`back/go-assistant/cpn/hitl.go:111` onward): the builder produces a JSON payload, `fireHITL` emits an `EventStreamChunk` prefixed with `$$a2ui:`, records the message in `c.History`, and blocks on the HITL channel. The channel's token color is `C_ModelMgmtResponse`, defined in §4.5.
- **REQ-CPN-004**: Response persistence MUST use `ParentMessageID` linking per `spec-process-bugfix-a2ui-hitl-response-persistence.md`. The user's submission row has `parent_message_id` set to the A2UI surface's message id so session rehydration can pair them.
- **REQ-CPN-005**: The topology MUST be deadlock-free: there is no combination of guards that traps a token in a place with no enabled outgoing transition. Proof obligation on the CPN mathematician (§4.5 contains the reachability argument).
- **REQ-CPN-006**: The topology MUST be bounded: no place can accumulate unbounded tokens. In particular, the per-management-turn state place `P_MgmtState` is guaranteed to hold exactly one token at a time by transition input-arc weights of 1 and the "single active turn" guard.
- **REQ-CPN-007**: The topology MUST NOT mutate the registry via direct SQL in the handler arcs. It MUST call the `ModelRegistryService` defined in §4.4, which enforces invariants (singleton default, gating, etc.). Handler arcs that bypass the service are a protocol violation.

### Frontend (REQ-FE-*)

- **REQ-FE-001**: `front/react-assistant/src/features/chat/a2ui/A2UIMessageRenderer.tsx` MUST gain no new catalog entries (per REQ-A2UI-002). Every new surface is built from existing primitives via the agent-emitted component tree.
- **REQ-FE-002**: `front/react-assistant/src/features/setup/steps/ModelStep.tsx` MUST be updated to understand the extended `GET /api/v1/models` response — specifically, the `models[]` array — and render capability badges (`vision`, `tools`, `reasoning`) and dual pricing (`$in/M · $out/M`) next to each option when the `models[]` field is present. If only the legacy shape is returned (e.g. during rollback), the step MUST render exactly as it does today (REQ-API-001 guarantees this).
- **REQ-FE-003**: `front/react-assistant/src/features/settings/SettingsPage.tsx` MUST gain an inline link in the "Advanced" section reading "Manage model registry" (i18n key `settings.models.manage_link`). The link opens a new chat with the seed message `"I want to manage the model registry"` (i18n key `settings.models.manage_seed_message` — en / es). It MUST NOT duplicate the registry management UI as a panel — per product direction, the only UI for registry management is conversational (the `AdminSecretsPanel` exception is out of scope and orthogonal).
- **REQ-FE-004**: The assistant message bubble (`front/react-assistant/src/features/chat/MessageBubble.tsx` or equivalent per current naming) MUST render a small responding-model indicator below the message content when `message.metadata.responding_model` is present. The indicator uses `RoundBadge` (existing chrome) with a terse format `gemma-4-31b`. Hover / long-press reveals the full `provider_adapter · provider_model_id · registry_id` via tooltip. This addresses the silent-drift failure documented in the SOTA research (Cursor Auto mode, ChatGPT router).
- **REQ-FE-005**: The SSE `stream_chunk` event payload MUST be extended (additive) with a `responding_model` subfield on the FINAL chunk of a transition's stream (`Done: true`). The frontend ignores earlier chunks' values. Backend: `cpn/fire_llm.go` populates the field from the resolved route.
- **REQ-FE-006**: No existing component may call `/api/v1/admin/models` — those endpoints are exclusively driven by the CPN management flow. The frontend's only exposure to them is through the agent's emitted A2UI surfaces.

### Observability (REQ-OBS-*)

- **REQ-OBS-001**: Every lifecycle / license-status transition on a `models` row MUST emit a structured log at INFO with `{ action, registry_id, from, to, actor_id, reason? }`. Actor is extracted from the JWT in the request context.
- **REQ-OBS-002**: Every LLM invocation on a `deprecated` model MUST emit a WARN log with `session_id, cpn_id, transition_id, registry_id, deprecated_at, replaced_by`. This signals to operations that deprecated models are still in use and a migration is overdue.
- **REQ-OBS-003**: License enrichment failures (HF API 4xx/5xx, parse errors) MUST emit ERROR logs with `registry_id, huggingface_id, status_code?, error`. The model's enrichment status is reflected in `source_metadata.huggingface_fetch_error` with the error message truncated to 1 KB.
- **REQ-OBS-004**: Runtime gate rejections (REQ-GATE-002 fallback path) MUST emit a WARN log with `session_id, requested_registry_id, fallback_registry_id, reason`. Reason is one of `lifecycle_not_active`, `license_not_approved`, `route_missing`, `adapter_missing`.

### Constraints (CON-*)

- **CON-001**: The registry schema MUST remain backward-compatible with the parent spec's three-level cascade. `users.preferred_model` and `users.model_overrides` continue as TEXT — the registry does not introduce a foreign key. A string that no longer resolves to a registry row falls back to product default (REQ-GATE-002).
- **CON-002**: The A2UI surfaces MUST stay on v0.8. The repo's catalog extension URI (per `https://a2ui.org/specification/v0.8-a2ui/` §2.1) remains unchanged; this spec does not bump the catalog version. A future `spec-architecture-a2ui-v0.9-migration.md` will address v0.9.
- **CON-003**: Pricing values are best-effort point-in-time quotes. OpenRouter prices change without notice. Operators MUST refresh pricing manually; the background refresh job is out of scope.
- **CON-004**: HuggingFace license metadata is self-reported by the model publisher. brae does not audit it. The `approved-commercial` license status is an explicit product decision made by the admin, not an inference from `license.spdx_id`.
- **CON-005**: The conversational flow runs at human speed — a registry mutation is expected to take 5–30 seconds end-to-end (classifier → surface emission → user action → confirmation → persist → replay). Bulk admin work (e.g. seeding 50 models) MUST go through the REST endpoints, not the chat.
- **CON-006**: Removing the `PRODUCT_DEFAULT_MODEL` constant would violate REQ-SEED-004. The constant stays as a last-resort fallback for startup-time registry unavailability.

### Guidelines (GUD-*)

- **GUD-001**: The two state machines (lifecycle, license) MUST stay orthogonal in code. An enum-valued column per state with a `state_transitions.go` table of allowed edges is clearer than combining them. Tests that assert forbidden transitions live in `internal/app/model_registry_service_test.go`.
- **GUD-002**: Prefer the Hugging Face `cardData.license` slug when populating `license.spdx_id`. When the slug is non-SPDX (`llama3.3`, `gemma`, `qwen`, `mrl`), store it verbatim in `license.community_slug` and leave `license.spdx_id` NULL.
- **GUD-003**: When composing the `ModelCardList` template, bind per-row IDs via `{"path": "/id"}`, not array indexes. v0.8 has no `$index` (per spec §3.2–3.3). This keeps per-row actions stable across re-ordering.
- **GUD-004**: Any action that mutates the registry MUST produce a terminal `ConfirmStateChange` surface even if the user's free-text intent was unambiguous. Do not silently apply agent-inferred mutations — this preserves the "explicit confirmation before destructive action" rule (SOTA research: Cursor Auto mode silent-switch class of complaint).
- **GUD-005**: Surface JSON payloads SHOULD be produced by small, pure builder functions (one per surface) in `back/go-assistant/cpn/surfaces/model_registry/`. Each builder takes a typed snapshot struct and returns `[]byte` + `error`. This mirrors `buildClarifyA2UIPayload` (see parent spec `spec-architecture-cpn-iterative-clarification-loop.md`).

### Patterns (PAT-*)

- **PAT-001**: Two orthogonal state machines (lifecycle × license) — borrowed from the SOTA research on Open WebUI, which separates `is_active` (backend functional) from `meta.hidden` (access-control visibility). Mixing them causes the class of bugs documented in Open WebUI discussion #14004 (hidden default persisted as default).
- **PAT-002**: Radio-not-checkbox for default selection — SOTA research concludes this is the single cleanest way to enforce "exactly one default" in the UX. Expressed in v0.8 as `MultipleChoice` with `maxAllowedSelections: 1`.
- **PAT-003**: Responding-model indicator — OpenRouter's `response.model` pattern (`https://openrouter.ai/docs/guides/routing/model-fallbacks`). Eliminates silent model drift.
- **PAT-004**: Hugging Face gated-model consent flow — the reference for license acceptance UX (`https://huggingface.co/docs/hub/en/models-gated`). brae's `LicenseReviewCard` follows this pattern: one-time modal, persisted acceptance in `license.reviewed_at` + `license.reviewed_by`, link to canonical terms.
- **PAT-005**: v0.8 `deleteSurface` + frozen read-only replay Card for locking stale HITL surfaces — already used by the repo (commit `c1e1396`), reused here.

## 4. Interfaces & Data Contracts

### 4.1 JSON Schema — ModelRegistryEntry

Canonical external-facing shape returned by `GET /api/v1/admin/models/:registry_id` and accepted (minus server-computed fields) by `POST /api/v1/admin/models`. Inline comments use `// …` prose; the JSON itself is schema-valid.

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://liwaisi.tech/schemas/model-registry-entry.json",
  "title": "ModelRegistryEntry",
  "type": "object",
  "required": [
    "registry_id", "vendor", "family", "display_name",
    "modalities", "context", "pricing", "capabilities",
    "license", "lifecycle", "routes",
    "created_at", "updated_at"
  ],
  "properties": {
    "registry_id": {
      "type": "string",
      "pattern": "^[a-z0-9][a-z0-9._-]*\\/[a-z0-9][a-z0-9._-]*$",
      "examples": ["anthropic/claude-4.7-opus-20260416", "google/gemma-4-31b-it"]
    },
    "vendor":  { "type": "string", "examples": ["anthropic", "openai", "google", "meta", "mistralai", "deepseek", "alibaba-qwen", "xai", "cohere"] },
    "family":  { "type": "string", "examples": ["claude", "gpt", "gemini", "llama", "mistral", "deepseek-v3", "qwen3", "gemma"] },
    "version": { "type": "string", "examples": ["4.7-opus", "4o-2024-11-20", "2.5-pro", "4-31b-it"] },
    "variant": { "type": ["string", "null"], "enum": [null, "free", "fast", "nitro", "floor", "thinking", "instruct", "base"] },
    "display_name": { "type": "string" },
    "description":  { "type": "string" },
    "hugging_face_id": { "type": ["string", "null"] },

    "modalities": {
      "type": "object",
      "required": ["input", "output"],
      "properties": {
        "input":  { "type": "array", "items": { "enum": ["text","image","file","audio","video"] } },
        "output": { "type": "array", "items": { "enum": ["text","image","audio","embeddings","rerank","video"] } },
        "modality_string": { "type": "string" }
      }
    },

    "context": {
      "type": "object",
      "required": ["length"],
      "properties": {
        "length": { "type": "integer" },
        "max_output_tokens": { "type": ["integer", "null"] },
        "tokenizer": { "type": "string" },
        "knowledge_cutoff": { "type": ["string", "null"], "format": "date" }
      }
    },

    "pricing": {
      "type": "object",
      "description": "All per-token values are decimal USD per SINGLE token. Display converts to per-1M at render time.",
      "properties": {
        "input_per_token":          { "type": ["string", "null"] },
        "output_per_token":         { "type": ["string", "null"] },
        "cache_read_per_token":     { "type": ["string", "null"] },
        "cache_write_per_token":    { "type": ["string", "null"] },
        "reasoning_per_token":      { "type": ["string", "null"] },
        "image_per_input":          { "type": ["string", "null"] },
        "image_per_output":         { "type": ["string", "null"] },
        "audio_per_input_unit":     { "type": ["string", "null"] },
        "audio_per_output_unit":    { "type": ["string", "null"] },
        "web_search_per_call":      { "type": ["string", "null"] },
        "request_flat":             { "type": ["string", "null"] },
        "currency":                 { "type": "string", "default": "USD" },
        "last_verified_at":         { "type": "string", "format": "date-time" }
      }
    },

    "capabilities": {
      "type": "object",
      "description": "Derived booleans for UI ergonomics. Source of truth is supported_parameters + modalities.",
      "properties": {
        "text":                      { "type": "boolean" },
        "vision":                    { "type": "boolean" },
        "audio_in":                  { "type": "boolean" },
        "audio_out":                 { "type": "boolean" },
        "video_in":                  { "type": "boolean" },
        "file_in":                   { "type": "boolean" },
        "image_out":                 { "type": "boolean" },
        "tools":                     { "type": "boolean" },
        "parallel_tools":            { "type": "boolean" },
        "structured_output":         { "type": "boolean" },
        "json_mode":                 { "type": "boolean" },
        "reasoning":                 { "type": "boolean" },
        "reasoning_effort_tunable":  { "type": "boolean" },
        "web_search":                { "type": "boolean" },
        "logprobs":                  { "type": "boolean" },
        "streaming":                 { "type": "boolean", "default": true }
      }
    },
    "supported_parameters": { "type": "array", "items": { "type": "string" } },
    "default_parameters":   { "type": "object", "additionalProperties": { "type": ["number", "integer", "null"] } },

    "license": {
      "type": "object",
      "required": ["kind", "status"],
      "properties": {
        "kind":              { "enum": ["spdx", "community", "proprietary-api", "custom", "unknown"] },
        "spdx_id":           { "type": ["string", "null"], "examples": ["apache-2.0","mit","cc-by-nc-4.0"] },
        "community_slug":    { "type": ["string", "null"], "examples": ["llama3.3","gemma","qwen","mrl","deepseek"] },
        "name":              { "type": ["string", "null"] },
        "url":               { "type": ["string", "null"], "format": "uri" },
        "source":            { "enum": ["huggingface", "provider-docs", "manual", "inferred"] },
        "commercial_use":    { "type": ["boolean", "null"] },
        "redistribute":      { "type": ["boolean", "null"] },
        "modify":            { "type": ["boolean", "null"] },
        "attribution_required": { "type": ["boolean", "null"] },
        "status":            { "enum": ["unreviewed","review-in-progress","approved-commercial","approved-non-commercial","restricted","blocked","unknown"] },
        "reviewed_at":       { "type": ["string", "null"], "format": "date-time" },
        "reviewed_by":       { "type": ["string", "null"] },
        "notes":             { "type": ["string", "null"] }
      }
    },

    "lifecycle": {
      "type": "object",
      "required": ["state"],
      "properties": {
        "state":          { "enum": ["discovered","pending-license-review","registered","active","disabled","deprecated","sunset","removed"] },
        "registered_at":  { "type": ["string", "null"], "format": "date-time" },
        "deprecated_at":  { "type": ["string", "null"], "format": "date-time" },
        "sunset_at":      { "type": ["string", "null"], "format": "date-time" },
        "replaced_by":    { "type": ["string", "null"] },
        "reason":         { "type": ["string", "null"] }
      }
    },

    "routes": {
      "type": "array",
      "minItems": 1,
      "items": {
        "type": "object",
        "required": ["provider_adapter", "provider_model_id", "priority", "enabled"],
        "properties": {
          "provider_adapter":   { "enum": ["openrouter","anthropic","openai","google-gemini","vertex","bedrock","azure-openai","groq","together","fireworks","self-hosted"] },
          "provider_model_id":  { "type": "string" },
          "endpoint_base_url":  { "type": ["string", "null"], "format": "uri" },
          "priority":           { "type": "integer" },
          "enabled":            { "type": "boolean" },
          "region":             { "type": ["string", "null"] },
          "is_moderated":       { "type": ["boolean", "null"] }
        }
      }
    },

    "source_metadata": {
      "type": "object",
      "properties": {
        "openrouter_id":             { "type": ["string", "null"] },
        "openrouter_canonical_slug": { "type": ["string", "null"] },
        "openrouter_raw":            { "type": ["object", "null"] },
        "huggingface_raw":           { "type": ["object", "null"] },
        "huggingface_fetched_at":    { "type": ["string", "null"], "format": "date-time" },
        "huggingface_fetch_error":   { "type": ["string", "null"] },
        "fetched_at":                { "type": "string", "format": "date-time" }
      }
    },

    "is_product_default": {
      "type": "boolean",
      "description": "Derived: TRUE iff this row's id == registry_config.product_default_model_id. Read-only in API responses; mutating requires POST /api/v1/admin/models/:id/set-default (REQ-API-007)."
    },
    "created_at":         { "type": "string", "format": "date-time" },
    "updated_at":         { "type": "string", "format": "date-time" }
  }
}
```

### 4.2 Postgres DDL — migration 020

```sql
-- back/go-assistant/store/postgres/migrations/020_model_registry.up.sql

CREATE TYPE model_lifecycle_state AS ENUM (
  'discovered',
  'pending-license-review',
  'registered',
  'active',
  'disabled',
  'deprecated',
  'sunset',
  'removed'
);

CREATE TYPE model_license_status AS ENUM (
  'unreviewed',
  'review-in-progress',
  'approved-commercial',
  'approved-non-commercial',
  'restricted',
  'blocked',
  'unknown'
);

CREATE TABLE IF NOT EXISTS models (
  registry_id         TEXT PRIMARY KEY
                        CHECK (registry_id ~ '^[a-z0-9][a-z0-9._-]*\/[a-z0-9][a-z0-9._-]*$'),
  vendor              TEXT NOT NULL,
  family              TEXT NOT NULL,
  version             TEXT,
  variant             TEXT,
  display_name        TEXT NOT NULL,
  description         TEXT,
  hugging_face_id     TEXT,

  -- Modalities / context
  modalities          JSONB NOT NULL DEFAULT '{"input":["text"],"output":["text"]}'::jsonb,
  context_length      INTEGER NOT NULL DEFAULT 0,
  max_output_tokens   INTEGER,
  tokenizer           TEXT NOT NULL DEFAULT 'Other',
  knowledge_cutoff    DATE,

  -- Pricing (per single token; NULL = not priced)
  pricing_input_per_token         NUMERIC(20,12),
  pricing_output_per_token        NUMERIC(20,12),
  pricing_cache_read_per_token    NUMERIC(20,12),
  pricing_cache_write_per_token   NUMERIC(20,12),
  pricing_reasoning_per_token     NUMERIC(20,12),
  pricing_image_per_input         NUMERIC(20,12),
  pricing_image_per_output        NUMERIC(20,12),
  pricing_audio_per_input_unit    NUMERIC(20,12),
  pricing_audio_per_output_unit   NUMERIC(20,12),
  pricing_web_search_per_call     NUMERIC(20,12),
  pricing_request_flat            NUMERIC(20,12),
  pricing_currency                TEXT NOT NULL DEFAULT 'USD',
  pricing_last_verified_at        TIMESTAMPTZ,

  -- Capabilities (denormalized derived booleans)
  cap_vision                      BOOLEAN NOT NULL DEFAULT FALSE,
  cap_audio_in                    BOOLEAN NOT NULL DEFAULT FALSE,
  cap_audio_out                   BOOLEAN NOT NULL DEFAULT FALSE,
  cap_video_in                    BOOLEAN NOT NULL DEFAULT FALSE,
  cap_file_in                     BOOLEAN NOT NULL DEFAULT FALSE,
  cap_image_out                   BOOLEAN NOT NULL DEFAULT FALSE,
  cap_tools                       BOOLEAN NOT NULL DEFAULT FALSE,
  cap_parallel_tools              BOOLEAN NOT NULL DEFAULT FALSE,
  cap_structured_output           BOOLEAN NOT NULL DEFAULT FALSE,
  cap_json_mode                   BOOLEAN NOT NULL DEFAULT FALSE,
  cap_reasoning                   BOOLEAN NOT NULL DEFAULT FALSE,
  cap_reasoning_effort_tunable    BOOLEAN NOT NULL DEFAULT FALSE,
  cap_web_search                  BOOLEAN NOT NULL DEFAULT FALSE,
  cap_logprobs                    BOOLEAN NOT NULL DEFAULT FALSE,
  cap_streaming                   BOOLEAN NOT NULL DEFAULT TRUE,

  supported_parameters            TEXT[] NOT NULL DEFAULT '{}',
  default_parameters              JSONB NOT NULL DEFAULT '{}'::jsonb,

  -- License
  license_kind            TEXT NOT NULL DEFAULT 'unknown'
                            CHECK (license_kind IN ('spdx','community','proprietary-api','custom','unknown')),
  license_spdx_id         TEXT,
  license_community_slug  TEXT,
  license_name            TEXT,
  license_url             TEXT,
  license_source          TEXT NOT NULL DEFAULT 'inferred'
                            CHECK (license_source IN ('huggingface','provider-docs','manual','inferred')),
  license_commercial_use  BOOLEAN,
  license_redistribute    BOOLEAN,
  license_modify          BOOLEAN,
  license_attribution_required BOOLEAN,
  license_status          model_license_status NOT NULL DEFAULT 'unreviewed',
  license_reviewed_at     TIMESTAMPTZ,
  license_reviewed_by     TEXT,
  license_notes           TEXT,

  -- Lifecycle
  lifecycle_state         model_lifecycle_state NOT NULL DEFAULT 'registered',
  lifecycle_registered_at TIMESTAMPTZ,
  lifecycle_deprecated_at TIMESTAMPTZ,
  lifecycle_sunset_at     TIMESTAMPTZ,
  lifecycle_replaced_by   TEXT REFERENCES models(registry_id) ON DELETE SET NULL,
  lifecycle_reason        TEXT,

  -- Routes (array of route objects; see §4.1)
  routes                  JSONB NOT NULL DEFAULT '[]'::jsonb,

  -- Audit trail
  source_metadata         JSONB NOT NULL DEFAULT '{}'::jsonb,

  created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Singleton invariant (REQ-REG-008): exactly one product default at all times.
-- `id = 1` forces the table to hold a single row; NOT NULL + FK RESTRICT
-- guarantees a non-deletable, always-populated pointer.
CREATE TABLE IF NOT EXISTS registry_config (
  id                        SMALLINT PRIMARY KEY CHECK (id = 1),
  product_default_model_id  UUID NOT NULL REFERENCES models(id) ON DELETE RESTRICT,
  updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_by                TEXT
);

CREATE INDEX models_by_vendor_family  ON models (vendor, family);
CREATE INDEX models_by_lifecycle      ON models (lifecycle_state);
CREATE INDEX models_by_license_status ON models (license_status);
CREATE INDEX models_routes_gin        ON models USING GIN (routes jsonb_path_ops);

-- updated_at trigger
CREATE OR REPLACE FUNCTION trg_models_set_updated_at() RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at := NOW();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_models_updated_at
  BEFORE UPDATE ON models
  FOR EACH ROW
  EXECUTE FUNCTION trg_models_set_updated_at();

-- Role defaults (replaces the in-code DefaultModelRegistry map)
CREATE TABLE IF NOT EXISTS model_role_defaults (
  role           TEXT PRIMARY KEY
                   CHECK (role IN ('classifier','structured','reasoning','long-context','summarize','thinking')),
  registry_id    TEXT NOT NULL REFERENCES models(registry_id) ON DELETE RESTRICT,
  updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TRIGGER trg_role_defaults_updated_at
  BEFORE UPDATE ON model_role_defaults
  FOR EACH ROW
  EXECUTE FUNCTION trg_models_set_updated_at();

-- Seed: every row from openrouter.AvailableModels. Gemma is the default+active.
-- (Full seed INSERTs produced by scripts/seed-registry/gen_seed_sql.go from
--  scripts/seed-registry/seed_data.json; abbreviated example below.)

INSERT INTO models (
  registry_id, vendor, family, version, display_name,
  context_length, tokenizer,
  pricing_input_per_token, pricing_output_per_token,
  license_kind, license_spdx_id, license_source, license_status,
  lifecycle_state, lifecycle_registered_at,
  routes
) VALUES
(
  'google/gemma-4-31b-it', 'google', 'gemma', '4-31b-it', 'Google Gemma 4 31B Instruct',
  262144, 'Gemma',
  0.000000130, 0.000000380,
  'community', 'apache-2.0', 'manual', 'approved-commercial',
  'active', NOW(),
  '[{"provider_adapter":"openrouter","provider_model_id":"google/gemma-4-31b-it","priority":0,"enabled":true}]'::jsonb
);
-- ... further rows for the remaining 8 entries in openrouter.AvailableModels:43-54 ...

-- Initialize the singleton registry_config pointing at Gemma as the product default.
INSERT INTO registry_config (id, product_default_model_id, updated_by)
SELECT 1, m.id, 'seed-migration-014'
FROM models m
WHERE m.registry_id = 'google/gemma-4-31b-it';

INSERT INTO model_role_defaults (role, registry_id) VALUES
  ('classifier',   'google/gemma-4-31b-it'),
  ('structured',   'google/gemma-4-31b-it'),
  ('reasoning',    'google/gemma-4-31b-it'),
  ('long-context', 'google/gemma-4-31b-it'),
  ('summarize',    'google/gemma-4-31b-it'),
  ('thinking',     'google/gemma-4-31b-it');
```

The corresponding `020_model_registry.down.sql` MUST drop, in reverse order: triggers → `registry_config` → `model_role_defaults` → `models` → the two ENUM types.

### 4.3 Go ports & adapters

```go
// back/go-assistant/cpn/registry.go (new file, or append to ports.go)

package cpn

import "context"

// ModelRegistry is the domain port. The implementation lives in store/postgres.
type ModelRegistry interface {
    // GetInvokable is the PRIMARY runtime-path lookup. It returns the entry IFF it
    // satisfies REQ-GATE-001 (lifecycle=active AND license approved). Non-invokable
    // rows return ErrModelNotInvokable — callers MUST fall back to the product default.
    // All transition firing paths MUST use this method, not GetByID, to guarantee the
    // gate cannot be bypassed (defense in depth per D5).
    GetInvokable(ctx context.Context, registryID string) (*ModelRegistryEntry, error)

    // GetByID is the ADMIN-path lookup. Returns any row regardless of invokability.
    // Used by the admin UI and the A2UI management flow when displaying non-invokable
    // rows for review/repair. MUST NOT be used from the transition firing path.
    GetByID(ctx context.Context, registryID string) (*ModelRegistryEntry, error)

    GetProductDefault(ctx context.Context) (*ModelRegistryEntry, error)
    ListInvokable(ctx context.Context) ([]*ModelRegistryEntry, error)
    ListAll(ctx context.Context, filter ModelListFilter) ([]*ModelRegistryEntry, error)
    Insert(ctx context.Context, entry *ModelRegistryEntry) error
    Update(ctx context.Context, entry *ModelRegistryEntry, ifMatch UpdatedAt) error
    Delete(ctx context.Context, registryID string) error
    SetProductDefault(ctx context.Context, registryID string) error // atomic transaction
    SetLicenseReview(ctx context.Context, registryID string, review LicenseReview) error
    GetRoleDefault(ctx context.Context, role string) (string, error)
    SetRoleDefault(ctx context.Context, role, registryID string) error
}

// ErrModelNotInvokable signals the target row exists but fails the REQ-GATE-001 predicate.
// Returned only by GetInvokable. Callers MUST treat it as a fallback-to-default signal,
// not an infrastructure error.
var ErrModelNotInvokable = errors.New("model is not invokable")

type ModelRegistryEntry struct {
    RegistryID       string
    Vendor           string
    Family           string
    Version          string
    Variant          *string
    DisplayName      string
    Description      string
    HuggingFaceID    *string
    Modalities       Modalities
    Context          Context
    Pricing          Pricing
    Capabilities     Capabilities
    SupportedParams  []string
    DefaultParams    map[string]any
    License          License
    Lifecycle        Lifecycle
    Routes           []Route
    SourceMetadata   map[string]any
    IsProductDefault bool // derived from registry_config; set by the repo adapter, never persisted as a column
    CreatedAt        time.Time
    UpdatedAt        time.Time
}

type Route struct {
    ProviderAdapter  string // "openrouter" | "anthropic" | "openai" | ...
    ProviderModelID  string
    EndpointBaseURL  *string
    Priority         int
    Enabled          bool
    Region           *string
    IsModerated      *bool
}

// Invokable returns true iff the entry is callable at runtime.
// Implements REQ-GATE-001.
func (m *ModelRegistryEntry) Invokable() bool {
    if m.Lifecycle.State != "active" {
        return false
    }
    switch m.License.Status {
    case "approved-commercial", "approved-non-commercial", "restricted":
        return true
    }
    return false
}
```

```go
// back/go-assistant/infra/huggingface/license.go (new package)

package huggingface

import "context"

type LicenseEnricher interface {
    Enrich(ctx context.Context, huggingFaceID string) (*EnrichResult, error)
}

type EnrichResult struct {
    SPDXID         *string
    CommunitySlug  *string
    LicenseName    *string
    LicenseURL     *string
    RawResponse    map[string]any
}
```

```go
// internal/app/model_registry_service.go (new file)

package app

type ModelRegistryService struct {
    repo      cpn.ModelRegistry
    enricher  huggingface.LicenseEnricher
    logger    *slog.Logger
}

// Register validates, enriches, and inserts. Default initial state:
// lifecycle=pending-license-review, license.status=unreviewed.
// If hugging_face_id is set, enrichment runs and license.source=huggingface.
func (s *ModelRegistryService) Register(ctx context.Context, req RegisterRequest) (*cpn.ModelRegistryEntry, error) { ... }

// SetDefault atomically reassigns the singleton default and auto-activates the target (REQ-API-007).
func (s *ModelRegistryService) SetDefault(ctx context.Context, registryID, actorID string) (*cpn.ModelRegistryEntry, error) { ... }

// Transition validates the lifecycle or license edge (§4.4 state machines) before persisting.
func (s *ModelRegistryService) Transition(ctx context.Context, req TransitionRequest) error { ... }

// Delete refuses when the row is the default (REQ-API-006) or when lifecycle != 'removed'.
func (s *ModelRegistryService) Delete(ctx context.Context, registryID, actorID string) error { ... }
```

`ModelRegistryService` is injected into `SessionService` alongside the existing `PersistDeps`, and the parent spec's `resolveForUser` is updated to call `ModelRegistryService.GetProductDefault` at the product-default rung.

### 4.4 REST API

| Method | Path | Auth | Body / Response |
|--------|------|------|-----------------|
| `GET`  | `/api/v1/models` | user | **Extended (backward-compat).** Existing keys unchanged; additive `models: ModelRegistryEntry[]` with only invokable models included. |
| `GET`  | `/api/v1/admin/models` | admin | Query: `?lifecycle=`, `?license_status=`, `?vendor=`. Response `{ models: ModelRegistryEntry[] }`. |
| `GET`  | `/api/v1/admin/models/:registry_id` | admin | Full `ModelRegistryEntry`. |
| `POST` | `/api/v1/admin/models` | admin | Body: `ModelRegistryEntry` minus server-computed fields. Server runs license enrichment if `hugging_face_id` is set. 201 → full entry. |
| `PATCH` | `/api/v1/admin/models/:registry_id` | admin | Partial `ModelRegistryEntry`. Rejects `is_product_default` (use dedicated endpoint). Requires `If-Match: <updated_at>`. |
| `DELETE` | `/api/v1/admin/models/:registry_id` | admin | Rejects when default (409 `CANNOT_DELETE_DEFAULT`) or when lifecycle ≠ `removed` (409 `DELETE_REQUIRES_REMOVED`). |
| `POST` | `/api/v1/admin/models/:registry_id/set-default` | admin | Body: `{}`. Atomic: auto-activates target, flips the flag, demotes previous default. Rejects non-invokable target with 422 `DEFAULT_NOT_INVOKABLE`. |
| `POST` | `/api/v1/admin/models/:registry_id/license/review` | admin | Body: `{ status, reviewer_id, notes?, commercial_use?, redistribute?, modify?, attribution_required? }`. If approved, auto-advances lifecycle `pending-license-review → registered`. |
| `POST` | `/api/v1/admin/models/:registry_id/lifecycle` | admin | Body: `{ to, reason?, replaced_by? }`. Validates the transition against the state machine below. |

#### Lifecycle state machine (REQ-REG-007)

Allowed transitions (any other edge returns HTTP 422 `INVALID_LIFECYCLE_TRANSITION`):

```
discovered             → pending-license-review
pending-license-review → registered    (only after license approved)
                       → blocked       (legal reject → removed)
registered             → active        (only if license approved AND ≥1 route enabled)
                       → disabled
                       → deprecated
                       → removed
active                 → disabled       (reversible)
                       → deprecated
                       → removed        (only via deprecated → sunset → removed chain)
disabled               → active         (only if license still approved)
                       → removed
deprecated             → sunset
sunset                 → removed
removed                (terminal)
```

#### License status state machine (REQ-LIC-*)

Allowed transitions:

```
unreviewed              → review-in-progress
                        → blocked
                        → unknown
review-in-progress      → approved-commercial
                        → approved-non-commercial
                        → restricted
                        → blocked
                        → unknown
approved-commercial     → review-in-progress   (re-review)
                        → blocked              (legal revocation)
approved-non-commercial → review-in-progress
                        → blocked
restricted              → review-in-progress
                        → blocked
blocked                 → review-in-progress   (re-review only)
unknown                 → review-in-progress
```

### 4.5 CPN topology fragment — `manage-models-flow`

```
Places:
  P_MgmtEnter         color: C_UserTurn
  P_MgmtIntent        color: C_MgmtIntent   -- { kind: list|register|toggle|set-default|review-license, args: map[string]any }
  P_MgmtState         color: C_MgmtState    -- { turn_id, snapshot, prev_surface_id? }
  P_SurfaceEmitted    color: C_SurfaceRef   -- { surface_id, kind, message_id }
  P_HITLResponse      color: C_ModelMgmtResponse -- { action, payload, surface_id }
  P_RegistryMutation  color: C_Mutation     -- { op, registry_id, fields }
  P_Confirmed         color: C_Confirmation
  P_PersistedOK       color: C_Result
  P_ReplayEmitted     color: C_SurfaceRef
  P_MgmtExit          color: C_AssistantTurn

Transitions:
  t-classify-mgmt  : LLM (reuses classifier) ; IN P_MgmtEnter  → OUT P_MgmtIntent
  t-emit-list      : HITL ; IN P_MgmtIntent(kind=list)         → OUT P_SurfaceEmitted
  t-emit-register  : HITL ; IN P_MgmtIntent(kind=register)     → OUT P_SurfaceEmitted
  t-emit-toggle    : HITL ; IN P_MgmtIntent(kind=toggle)       → OUT P_SurfaceEmitted
  t-emit-setdefault: HITL ; IN P_MgmtIntent(kind=set-default)  → OUT P_SurfaceEmitted
  t-emit-license   : HITL ; IN P_MgmtIntent(kind=review-license)→ OUT P_SurfaceEmitted
  t-recv-response  : HITL channel wait         ; IN P_SurfaceEmitted  → OUT P_HITLResponse
  t-validate       : guard+handler             ; IN P_HITLResponse    → OUT P_RegistryMutation | t-reask (loop back to matching t-emit-*)
  t-emit-confirm   : HITL (ConfirmStateChange) ; IN P_RegistryMutation → OUT P_SurfaceEmitted'
  t-apply          : handler (calls service)   ; IN P_Confirmed       → OUT P_PersistedOK
  t-lock-replay    : handler                   ; IN P_PersistedOK     → OUT P_ReplayEmitted (emits deleteSurface + frozen replay card)
  t-summarize      : LLM (brief reply)         ; IN P_ReplayEmitted   → OUT P_MgmtExit

Guards (selected):
  t-emit-list           : no guard (intent already matched)
  t-validate            : payload.action ∈ {submit, cancel} AND payload.surface_id == latest
  t-emit-confirm        : mutation.op ∈ {register, enable, disable, delete, set-default, license-review}
  t-apply               : confirmation == "accept"
  t-lock-replay         : previous surface_id is non-empty

Token colors:
  C_UserTurn            : existing
  C_MgmtIntent          : { kind, args }
  C_MgmtState           : { turn_id, snapshot_ts, prev_surface_id?: string }
  C_SurfaceRef          : { surface_id, kind, message_id }
  C_ModelMgmtResponse   : { action: "submit"|"cancel", payload: map[string]any, surface_id }
  C_Mutation            : { op: string, registry_id: string, fields: map[string]any }
  C_Confirmation        : { decision: "accept"|"reject", mutation_id: string }
  C_Result              : { registry_id, before, after }
  C_AssistantTurn       : existing
```

**Boundedness (REQ-CPN-006)**: every input arc from `P_MgmtState` has weight 1; the "one active turn" guard on `t-classify-mgmt` (checking no existing token in `P_MgmtState` for the same session) prevents duplicate tokens. Therefore `|P_MgmtState| ≤ 1` at all times. Every surface place holds at most one token per turn by the same reasoning. The HITL channel is unbuffered; blocked reads cannot accumulate tokens.

**Deadlock freedom (REQ-CPN-005)**: from any reachable marking, at least one transition is enabled. In particular: if a token sits in `P_SurfaceEmitted` with no matching response for > HITL timeout, the existing `fireHITL` timeout (`back/go-assistant/cpn/hitl.go:234-245`) fails the session, removing the token. From `P_HITLResponse`, `t-validate` is always enabled; from `P_MgmtIntent`, exactly one of the `t-emit-*` transitions is enabled by intent kind. The `t-reask` loop is bounded by a max-rounds guard (3) mirroring the clarify-loop guard from `spec-architecture-cpn-iterative-clarification-loop.md`.

**Locking stale surfaces (REQ-A2UI-003, consistent with commits `c1e1396` and `d3590bf`)**: `t-lock-replay` emits a `deleteSurface` followed by a frozen replay `Card` on a fresh `surfaceId`. The replay Card's Buttons have no `action` wiring so they are inert if re-hydrated on session reload. The response row's `parent_message_id` (per REQ-CPN-004) links back to the original surface's message for history traversal.

### 4.6 A2UI surface catalog

All five surfaces use v0.8 primitives plus already-registered extensions (`questionnaire`, `alert`, `badge`, `choice` from `front/react-assistant/src/features/chat/a2ui/A2UIMessageRenderer.tsx:1189-1202`). No new catalog entries. The wire format is the same as today's A2UI output: a JSON object whose leading bytes on the stream are `$$a2ui:` (see `cpn/hitl.go:14 A2UIMarker`).

#### 4.6.1 `ModelCardList` — conversational list view

```json
{
  "type": "a2ui.v08",
  "surfaceId": "model-registry:list:2026-04-17T12-00-00Z-7a",
  "catalogId": "https://liwaisi.tech/a2ui/catalogs/v1/liwaisi.json",
  "dataModel": {
    "models": [
      { "id": "google/gemma-4-31b-it", "name": "Gemma 4 31B", "provider": "google",
        "status": "default", "price_in": "0.13", "price_out": "0.38",
        "ctx": "262K", "caps": ["tools","streaming","reasoning"] },
      { "id": "anthropic/claude-opus-4-6", "name": "Claude Opus 4.6", "provider": "anthropic",
        "status": "active", "price_in": "15.00", "price_out": "75.00",
        "ctx": "1M", "caps": ["tools","vision","structured"] }
    ],
    "filter": ""
  },
  "root": "root",
  "components": {
    "root": { "Column": { "children": { "explicitList": ["header","filter","list_models","register_btn"] } } },
    "header": { "Text": { "text": { "literalString": "Modelos registrados" }, "usageHint": "h2" } },
    "filter": { "TextField": { "label": "Buscar", "text": { "path": "/filter" }, "textFieldType": "shortText" } },
    "list_models": {
      "List": {
        "direction": "vertical",
        "children": { "template": { "dataBinding": "/models", "componentId": "model_row" } }
      }
    },
    "model_row": {
      "Card": { "child": "model_row_body" }
    },
    "model_row_body": {
      "Row": { "distribution": "spaceBetween", "alignment": "start",
        "children": { "explicitList": ["model_meta","model_actions"] } }
    },
    "model_meta": {
      "Column": { "children": { "explicitList": ["model_title","model_sub","model_caps"] } }
    },
    "model_title": { "Text": { "text": { "path": "/name" }, "usageHint": "h3" } },
    "model_sub":   { "Text": { "text": { "path": "/provider" }, "usageHint": "caption" } },
    "model_caps":  { "Text": { "text": { "path": "/caps" }, "usageHint": "caption" } },
    "model_actions": {
      "Row": { "distribution": "end",
        "children": { "explicitList": ["btn_inspect","btn_toggle","btn_setdefault","btn_delete"] } }
    },
    "btn_inspect":    { "Button": { "child": "txt_inspect",    "action": { "name": "inspect_model",    "context": [{"key":"registry_id","value":{"path":"/id"}}] } } },
    "btn_toggle":     { "Button": { "child": "txt_toggle",     "action": { "name": "toggle_model",     "context": [{"key":"registry_id","value":{"path":"/id"}}] } } },
    "btn_setdefault": { "Button": { "child": "txt_setdefault", "action": { "name": "set_default",      "context": [{"key":"registry_id","value":{"path":"/id"}}] } } },
    "btn_delete":     { "Button": { "child": "txt_delete",     "action": { "name": "delete_model",     "context": [{"key":"registry_id","value":{"path":"/id"}}] } } },
    "txt_inspect":    { "Text": { "text": { "literalString": "Detalles" } } },
    "txt_toggle":     { "Text": { "text": { "literalString": "Activar/Desactivar" } } },
    "txt_setdefault": { "Text": { "text": { "literalString": "Por defecto" } } },
    "txt_delete":     { "Text": { "text": { "literalString": "Eliminar" } } },
    "register_btn":   { "Button": { "child": "txt_register", "primary": true, "action": { "name": "register_new", "context": [] } } },
    "txt_register":   { "Text": { "text": { "literalString": "Registrar un modelo nuevo" } } }
  }
}
```

Notes: `filter` is a client-side value — because v0.8 has no onChange events (per `https://a2ui.org/specification/v0.8-a2ui/` §5), filtering is done at render time by the renderer, not re-sent to the agent. If the user wants to re-filter from the server, they re-open the list via a new message. This keeps the surface agent-minimal.

#### 4.6.2 `RegisterModelForm`

```json
{
  "type": "a2ui.v08",
  "surfaceId": "model-registry:register:2026-04-17T12-01-12Z-8b",
  "catalogId": "https://liwaisi.tech/a2ui/catalogs/v1/liwaisi.json",
  "dataModel": {
    "form": {
      "registry_id": "", "vendor": "", "family": "", "version": "", "display_name": "",
      "hugging_face_id": "", "context_length": 0,
      "primary_route_adapter": "openrouter", "primary_route_model_id": "",
      "enable_enrichment": true
    }
  },
  "root": "root",
  "components": {
    "root": { "Column": { "children": { "explicitList": [
      "title","intro","fld_registry_id","fld_vendor","fld_family","fld_version","fld_display_name",
      "fld_hf_id","fld_ctx","adapter_pick","fld_route_id","chk_enrich","submit_row"
    ] } } },
    "title": { "Text": { "text": { "literalString": "Registrar un modelo nuevo" }, "usageHint": "h2" } },
    "intro": { "Text": { "text": { "literalString": "Completa la información canónica. La licencia se revisará por separado." }, "usageHint": "body" } },
    "fld_registry_id":  { "TextField": { "label": "registry_id (vendor/family-version)", "text": { "path": "/form/registry_id" }, "textFieldType": "shortText", "validationRegexp": "^[a-z0-9][a-z0-9._-]*\\/[a-z0-9][a-z0-9._-]*$" } },
    "fld_vendor":       { "TextField": { "label": "vendor",       "text": { "path": "/form/vendor" },       "textFieldType": "shortText" } },
    "fld_family":       { "TextField": { "label": "family",       "text": { "path": "/form/family" },       "textFieldType": "shortText" } },
    "fld_version":      { "TextField": { "label": "version",      "text": { "path": "/form/version" },      "textFieldType": "shortText" } },
    "fld_display_name": { "TextField": { "label": "display_name", "text": { "path": "/form/display_name" }, "textFieldType": "shortText" } },
    "fld_hf_id":        { "TextField": { "label": "hugging_face_id (optional)", "text": { "path": "/form/hugging_face_id" }, "textFieldType": "shortText" } },
    "fld_ctx":          { "TextField": { "label": "context_length (tokens)", "text": { "path": "/form/context_length" }, "textFieldType": "number" } },
    "adapter_pick": { "MultipleChoice": {
      "selections": { "path": "/form/primary_route_adapter" }, "maxAllowedSelections": 1, "filterable": false,
      "options": [
        { "label": "OpenRouter",       "value": "openrouter" },
        { "label": "Anthropic direct", "value": "anthropic" },
        { "label": "OpenAI direct",    "value": "openai" },
        { "label": "Google Gemini",    "value": "google-gemini" },
        { "label": "Self-hosted",      "value": "self-hosted" }
      ]
    } },
    "fld_route_id": { "TextField": { "label": "provider_model_id", "text": { "path": "/form/primary_route_model_id" }, "textFieldType": "shortText" } },
    "chk_enrich":   { "CheckBox": { "label": "Enrich license from Hugging Face (if hugging_face_id set)", "value": { "path": "/form/enable_enrichment" } } },
    "submit_row":   { "Row": { "distribution": "end", "children": { "explicitList": ["btn_cancel","btn_submit"] } } },
    "btn_submit":   { "Button": { "child": "txt_submit", "primary": true,
                        "action": { "name": "submit_register", "context": [
                          {"key":"registry_id",             "value":{"path":"/form/registry_id"}},
                          {"key":"vendor",                  "value":{"path":"/form/vendor"}},
                          {"key":"family",                  "value":{"path":"/form/family"}},
                          {"key":"version",                 "value":{"path":"/form/version"}},
                          {"key":"display_name",            "value":{"path":"/form/display_name"}},
                          {"key":"hugging_face_id",         "value":{"path":"/form/hugging_face_id"}},
                          {"key":"context_length",          "value":{"path":"/form/context_length"}},
                          {"key":"primary_route_adapter",   "value":{"path":"/form/primary_route_adapter"}},
                          {"key":"primary_route_model_id",  "value":{"path":"/form/primary_route_model_id"}},
                          {"key":"enable_enrichment",       "value":{"path":"/form/enable_enrichment"}}
                        ] } } },
    "btn_cancel":   { "Button": { "child": "txt_cancel", "action": { "name": "cancel_register", "context": [] } } },
    "txt_submit":   { "Text": { "text": { "literalString": "Registrar" } } },
    "txt_cancel":   { "Text": { "text": { "literalString": "Cancelar" } } }
  }
}
```

Required-field validation happens server-side per REQ-A2UI-005. On missing fields, the agent emits a `surfaceUpdate` that replaces each offending field's parent Column with a version that prepends an error `Text` (via the existing `alert` extension).

#### 4.6.3 `LicenseReviewCard`

Matches the Hugging Face gated-model consent pattern (PAT-004). A `Modal` with an entry-point "Review license" button and a content panel that renders license body in scrollable `Text`, a `CheckBox` "I've read and accept the terms", and an Accept Button that posts `{"name":"accept_license","context":[{"key":"registry_id",...},{"key":"license_status","value":{"literalString":"approved-commercial"}}]}`. Full JSON omitted for brevity; structure mirrors the `ConfirmStateChange` tree below.

#### 4.6.4 `ConfirmStateChange`

Renders a diff panel inside a `Modal`. Example (delete):

```json
{
  "type": "a2ui.v08",
  "surfaceId": "model-registry:confirm:2026-04-17T12-02-30Z-9c",
  "dataModel": {
    "op": "delete",
    "registry_id": "anthropic/claude-haiku-4-5-20251001",
    "before": { "lifecycle": "disabled" },
    "after":  { "lifecycle": "removed" }
  },
  "root": "modal",
  "components": {
    "modal": { "Modal": { "entryPointChild": "btn_entry", "contentChild": "panel" } },
    "btn_entry": { "Button": { "child": "txt_entry", "action": { "name": "open_confirm", "context": [] } } },
    "txt_entry": { "Text": { "text": { "literalString": "Confirmar" } } },
    "panel": { "Column": { "children": { "explicitList": ["title","diff_row","notice","actions"] } } },
    "title": { "Text": { "text": { "literalString": "¿Eliminar este modelo?" }, "usageHint": "h3" } },
    "diff_row": { "Row": { "distribution": "spaceBetween", "children": { "explicitList": ["before_col","after_col"] } } },
    "before_col": { "Column": { "children": { "explicitList": ["before_lbl","before_val"] } } },
    "after_col":  { "Column": { "children": { "explicitList": ["after_lbl","after_val"] } } },
    "before_lbl": { "Text": { "text": { "literalString": "Antes" }, "usageHint": "caption" } },
    "before_val": { "Text": { "text": { "path": "/before/lifecycle" } } },
    "after_lbl":  { "Text": { "text": { "literalString": "Después" }, "usageHint": "caption" } },
    "after_val":  { "Text": { "text": { "path": "/after/lifecycle" } } },
    "notice":     { "Text": { "text": { "literalString": "Esta acción no se puede deshacer." }, "usageHint": "caption" } },
    "actions":    { "Row": { "distribution": "end", "children": { "explicitList": ["btn_cancel","btn_confirm"] } } },
    "btn_cancel": { "Button": { "child": "txt_cancel", "action": { "name": "cancel_confirm", "context": [{"key":"op","value":{"path":"/op"}},{"key":"registry_id","value":{"path":"/registry_id"}}] } } },
    "btn_confirm":{ "Button": { "child": "txt_confirm", "primary": true, "action": { "name": "apply_confirm", "context": [{"key":"op","value":{"path":"/op"}},{"key":"registry_id","value":{"path":"/registry_id"}}] } } },
    "txt_cancel": { "Text": { "text": { "literalString": "Cancelar" } } },
    "txt_confirm":{ "Text": { "text": { "literalString": "Eliminar" } } }
  }
}
```

#### 4.6.5 `DefaultSelector`

A single `MultipleChoice` with `maxAllowedSelections: 1` and `filterable: true` bound to `/selected_default`, plus a submit Button (PAT-002, per the SOTA research "radio not checkbox"). On submit, the agent calls `SetProductDefault(new_id)`. Structural guarantee: the client enforces single-select; the server enforces the singleton invariant via the `registry_config` singleton table + FK RESTRICT (REQ-REG-008).

### 4.7 Frontend impact list

| File | Change |
|------|--------|
| `front/react-assistant/src/services/api.ts` | Add admin endpoints (typed helpers under a feature-flagged `admin` namespace). Extend `ModelsResponse` type to include `models?: ModelRegistryEntry[]` (optional for backward-compat). |
| `front/react-assistant/src/types/setup.ts` | Add `ModelRegistryEntry` + subtypes (License, Lifecycle, Route, etc.). Backward-compat: keep `ModelsResponse` fields as-is. |
| `front/react-assistant/src/features/setup/steps/ModelStep.tsx` | Render capability badges + dual pricing next to each option when `models[]` is present. Fallback render unchanged. |
| `front/react-assistant/src/features/settings/SettingsPage.tsx` | Add "Manage model registry" inline link in the Advanced section. Opens a chat with the seed message. |
| `front/react-assistant/src/features/chat/MessageBubble.tsx` (or current name) | Render responding-model `RoundBadge` when `message.metadata.responding_model` is set. |
| `front/react-assistant/src/types/sse.ts` | Extend `StreamChunkPayload` with optional `responding_model` string. |
| `front/react-assistant/src/hooks/useChat.ts` | Capture `responding_model` on the final chunk and store it on the completed assistant message. |
| `front/react-assistant/src/features/chat/a2ui/A2UIMessageRenderer.tsx` | **No changes.** Every new surface uses existing primitives/extensions (REQ-A2UI-002). |
| `front/react-assistant/src/i18n/locales/en/settings.json` | New keys `settings.models.manage_link`, `settings.models.manage_seed_message`. |
| `front/react-assistant/src/i18n/locales/es/settings.json` | Spanish translations. |
| `front/react-assistant/src/i18n/locales/en/chat.json` | Registry-specific strings used by the agent's Text components (see §5 AC-REG-*). |
| `front/react-assistant/src/i18n/locales/es/chat.json` | Spanish translations. |

## 5. Acceptance Criteria

### Registry storage & seed

- **AC-REG-001**: Given migration `020_model_registry.up.sql` is applied to an empty Postgres, When `SELECT COUNT(*) FROM models` runs, Then the result equals `len(openrouter.AvailableModels)` (9 as of `back/go-assistant/infra/openrouter/openrouter.go:43-54`), `SELECT COUNT(*) FROM registry_config` returns exactly 1, and `SELECT m.registry_id FROM registry_config rc JOIN models m ON m.id = rc.product_default_model_id` returns `'google/gemma-4-31b-it'`.
- **AC-REG-002**: Given an attempt to `DELETE FROM models WHERE id = <default.id>`, When the DELETE executes, Then Postgres rejects it with foreign-key violation `registry_config_product_default_model_id_fkey` (ON DELETE RESTRICT). Given an attempt to `INSERT INTO registry_config (id, product_default_model_id) VALUES (2, ...)`, Then the INSERT fails the `CHECK (id = 1)` constraint.
- **AC-REG-003**: Given `SELECT * FROM model_role_defaults`, Then exactly 6 rows exist, one per canonical role, all pointing to `google/gemma-4-31b-it`.

### Runtime gate

- **AC-GATE-001**: Given a model `x/y` with `lifecycle_state = 'disabled'` and `license_status = 'approved-commercial'`, When a session attempts to stamp `x/y` as an `LLMConfig.Model`, Then `applyUserModelPreferences` MUST fall back to the product default and log a WARN `reason=lifecycle_not_active`.
- **AC-GATE-002**: Given a model `x/y` with `lifecycle_state = 'active'` and `license_status = 'unreviewed'`, When a session attempts to stamp `x/y`, Then the service falls back to the product default with `reason=license_not_approved`.
- **AC-GATE-003**: Given a model `x/y` with `license_status = 'restricted'` and the session-create request omits `accept_restricted`, When resolution runs, Then the service falls back to the product default with `reason=restricted_consent_missing`.

### REST API

- **AC-API-001**: Given `POST /api/v1/admin/models/google/gemma-4-31b-it` is called by a non-admin JWT, Then the response is HTTP 403 with body `{"error":"ADMIN_REQUIRED"}`.
- **AC-API-002**: Given `DELETE /api/v1/admin/models/google/gemma-4-31b-it`, Then the response is HTTP 409 with body `{"error":"CANNOT_DELETE_DEFAULT"}` and the row remains.
- **AC-API-003**: Given `POST /api/v1/admin/models/anthropic/claude-opus-4-6/set-default` where the target has `lifecycle_state = 'registered'` and `license_status = 'approved-commercial'`, Then the server runs a single SQL transaction that promotes the target's `lifecycle_state` to `'active'` and `UPDATE registry_config SET product_default_model_id = <target.id> WHERE id = 1`, and returns HTTP 200 with the updated entry. The previous default's row is untouched (its lifecycle remains whatever it was).
- **AC-API-004**: Given two admins PATCH the same row concurrently with the same `If-Match`, When the second request arrives, Then the response is HTTP 412 `PRECONDITION_FAILED`.
- **AC-API-005**: Given `GET /api/v1/models` called by any authenticated user, Then the response includes a non-empty `default` string AND an `available` array containing only invokable registry IDs AND a `models` array whose entries are all invokable. Non-invokable models do NOT appear in any of the three.

### License enrichment

- **AC-LIC-001**: Given a new registration with `hugging_face_id = "meta-llama/Llama-3.3-70B-Instruct"`, When enrichment runs, Then the stored row has `license_kind = 'community'`, `license_community_slug = 'llama3.3'`, `license_source = 'huggingface'`, and `source_metadata.huggingface_raw` contains the full response. Initial `license_status = 'unreviewed'`.
- **AC-LIC-002**: Given a new registration with `hugging_face_id` empty and `license.manual_entry = true`, When stored, Then the row has `license_source = 'manual'`, `license_kind = 'proprietary-api'`, and `license_url` is non-empty.
- **AC-LIC-003**: Given `POST /api/v1/admin/models/x/y/license/review` with body `{"status":"approved-commercial","reviewer_id":"admin@liwaisi.tech"}`, When the row was previously `pending-license-review`, Then the lifecycle auto-advances to `registered`, `license_reviewed_at` is NOW() ± 1s, `license_reviewed_by = 'admin@liwaisi.tech'`.

### CPN management flow

- **AC-CPN-001**: Given the user types "lista mis modelos" in chat, Then the classifier emits `intent = manage-models` with `kind = list`, and within 3 SSE events a `stream_chunk` arrives whose content starts with `$$a2ui:` and whose parsed JSON has `surfaceId` matching `model-registry:list:*`.
- **AC-CPN-002**: Given a `ModelCardList` is rendered and the user clicks the "Por defecto" Button on `anthropic/claude-opus-4-6`, Then the CPN receives a `userAction` with `name = set_default` and `registry_id = anthropic/claude-opus-4-6`, emits a `ConfirmStateChange` Modal surface showing before/after, and on accept issues `POST /api/v1/admin/models/anthropic/claude-opus-4-6/set-default` internally via the service.
- **AC-CPN-003**: Given the user submits `RegisterModelForm` with `registry_id = ""`, Then the agent emits `surfaceUpdate` that re-renders the same `surfaceId` with an inline `Text` error `"registry_id is required"` prepended above the `fld_registry_id` TextField. The session remains in `running` and the form values are preserved.
- **AC-CPN-004**: Given any management surface has received a user submission, When the mutation is persisted, Then the agent emits a `deleteSurface` for the original surface id and a frozen read-only `Card` on a new surface id whose Buttons have no `action`. On session rehydration, the replay Card renders but submits no actions.
- **AC-CPN-005**: Given 3 consecutive validation failures on `RegisterModelForm`, Then the agent terminates the loop with a summary message "No pude validar el formulario tras 3 intentos. Puedes retomarlo cuando quieras." and the `P_MgmtExit` place receives a token — bounding the loop per REQ-CPN-006.

### Frontend

- **AC-FE-001**: Given `ModelStep.tsx` renders with the new `/api/v1/models` response shape, Then each `<option>` shows the model's display name plus capability badges (`vision`, `tools`, `reasoning`) and dual pricing (`$0.13/M in · $0.38/M out`).
- **AC-FE-002**: Given `GET /api/v1/models` returns ONLY the legacy shape (no `models[]`), Then `ModelStep.tsx` renders exactly as before this spec. No null-reference errors, no missing badges causing crashes.
- **AC-FE-003**: Given an assistant message completes with `responding_model = "google/gemma-4-31b-it"` on the final `stream_chunk`, Then `MessageBubble` renders a `RoundBadge` with text `gemma-4-31b-it` below the content.
- **AC-FE-004**: Given `SettingsPage` is open, When the user clicks "Manage model registry", Then a new chat opens with the first user message pre-filled as `"I want to manage the model registry"` (en) or `"Quiero gestionar mis modelos"` (es).
- **AC-FE-008**: Given the registry contains 47 models and the user asks to list them, When `ModelCardList` renders page 1, Then exactly 20 rows appear and the footer shows a "Siguiente" Button whose `context` is `{"page":"2","filter":""}`. Given the user clicks "Siguiente", Then the agent emits a `surfaceUpdate` on the same `surfaceId` replacing `/models` with rows 21–40 and the footer now shows both "Anterior" and "Siguiente" Buttons. Given the user clicks "Siguiente" a second time, rows 41–47 render (7 rows) and only "Anterior" remains enabled.

### Observability

- **AC-OBS-001**: Given any lifecycle transition, Then the backend emits an INFO log with exactly the fields `{action, registry_id, from, to, actor_id, reason?}` and `action` ∈ `{lifecycle_transition, license_transition, default_reassign, insert, delete}`.
- **AC-OBS-002**: Given a deprecated model is invoked, Then every LLM call emits exactly one WARN log `"deprecated model in use"` with `{registry_id, deprecated_at, replaced_by}`.

## 6. Test Automation Strategy

- **Test Levels**: unit (Go + Vitest), integration (Go with testcontainers-go + real Postgres for migration and service tests), e2e (Playwright for the conversational management flow — optional if an existing e2e harness covers chat).
- **Frameworks**: Go `testing` package + `testify`; `pgxmock` for fast repo tests; `testcontainers-go` for migration replay; React Testing Library + Vitest for component tests; Playwright for e2e.
- **Test Data**: the seed fixture `scripts/seed-registry/seed_data.json` is the single source of truth for both the SQL seed and the Go unit tests. Repo tests load it directly; service tests rely on the migration. No inline model definitions in tests beyond one-off edge cases.
- **CI/CD Integration**: existing GitHub Actions workflow gains:
  1. `migration-replay` job — spins up Postgres, runs all migrations up then down then up, asserts zero errors.
  2. `registry-seed-invariants` job — asserts the singleton partial-index refuses a second `TRUE` row, and asserts `GET /api/v1/models` filters to invokable.
  3. `cpn-management-flow` job — runs an integration test that walks `classify → list → set-default → persist → replay`.
- **Coverage Requirements**: new code in `store/postgres/model_registry.go`, `internal/app/model_registry_service.go`, `infra/huggingface/license.go`, `cmd/server/topologies_model_registry.go` MUST reach 90% line coverage. Each A2UI surface builder (`cpn/surfaces/model_registry/*.go`) MUST have a unit test that marshals a snapshot input and diffs against a golden JSON file committed under `testdata/`.
- **Performance Testing**: registry queries MUST be served from Postgres in under 10ms p95 when the table holds ≤ 500 rows (realistic ceiling for this spec). License enrichment MUST time out at 5s and never block session start — it runs only on admin registration, never on session creation.

### Specific test list (TDD order)

| Test file | Asserts |
|-----------|---------|
| `store/postgres/model_registry_test.go` | Insert, GetByID, singleton-default conflict, Update with ETag, Delete refuses default, SetProductDefault atomic transaction, role-default reassign cascade protection. |
| `internal/app/model_registry_service_test.go` | Lifecycle state-machine: each allowed edge and a sample of forbidden edges. License state-machine likewise. Cascade: deleting the default via the service returns `ErrCannotDeleteDefault`. Set-default of a non-invokable target returns `ErrDefaultNotInvokable`. |
| `internal/app/session_service_registry_gate_test.go` | Gate fallback for each failure mode (lifecycle not active, license not approved, restricted no consent, adapter missing, route missing). |
| `infra/huggingface/license_test.go` | Fixtures: Llama 3.3 (tags + cardData), Mistral MRL (`license=other` + `license_name` + `license_link`), Apache-2.0 Qwen variant, empty cardData case, 404 HF response. |
| `internal/driving/httpapi/handler_admin_models_test.go` | Each REST endpoint: happy path, auth rejection, ETag mismatch, delete-default rejection, set-default auto-activation. |
| `cmd/server/topologies_model_registry_test.go` | Classifier hits `manage-models` intent for each seeded utterance; t-emit-list produces a `$$a2ui:` prefixed chunk with the expected `surfaceId` shape; t-recv-response accepts a well-formed userAction. |
| `cpn/surfaces/model_registry/list_builder_test.go` (and siblings) | Golden-file diff for each of the five surfaces. |
| `front/react-assistant/src/features/setup/steps/ModelStep.test.tsx` | Renders new `models[]` shape with badges + dual pricing; falls back to legacy shape without crashing. |
| `front/react-assistant/src/features/chat/MessageBubble.test.tsx` | Renders responding-model `RoundBadge` when metadata is present; hides it otherwise. |
| `front/react-assistant/e2e/manage-models.spec.ts` | Playwright: user types "list my models" → clicks set-default on a card → confirms modal → sees replay card with the new default visible. |

## 7. Rationale & Context

### 7.1 Why a database-backed registry now

The current compile-time catalog (`openrouter.go:43-54`) was intentional scaffolding for v1. It hit three walls simultaneously: (a) Gemini and direct-provider support requires route metadata (`provider_adapter`, `endpoint_base_url`) that a string array cannot express; (b) license clearance has to be a first-class property of the model, not a CLAUDE.md side-note — operators need to point to a data row when legal asks "did we clear Llama 3.3 for commercial use?"; (c) every model addition today requires a release. The database cut moves the catalog to where it always should have lived and lets product evolve independently of binary rollouts.

### 7.2 Why two orthogonal state machines

Mixing administrative lifecycle with legal clearance produces ambiguous states. "Is this model disabled because it's costly or because legal pulled it?" is the exact failure mode Open WebUI discussion #14004 documents. Separating them gives clean semantics: the SRE team toggles `lifecycle` for operations reasons, the legal team toggles `license_status` for clearance reasons, and the runtime gate is the intersection. This is the pattern the SOTA research identified in Open WebUI's `is_active × meta.hidden` split (PAT-001).

### 7.3 UX: information architecture

The `ModelCardList` arranges each row as `Row{ Column{title, provider, capability badges}, Row{inspect, toggle, set-default, delete} }`. Pricing is dual-display — `$0.13/M in · $0.38/M out` — because output tokens cost 3–10× input and a single number misleads (SOTA: Chuck Learning on decision fatigue, CloudIDR LLM pricing). Capability badges (vision, tools, reasoning) are the minimum set — adding more badges past three reliably degrades scannability.

The `DefaultSelector` is a single-select (`MultipleChoice` with `maxAllowedSelections: 1`) rather than a row of CheckBoxes. A CheckBox-per-row implies "you can have zero defaults or many" — both invalid under REQ-REG-008 (the `registry_config` singleton enforces exactly one at the DB level). The shape of the widget must reflect the invariant.

License review is a one-time Hugging Face-style consent (PAT-004): the model card exposes a "Review license" button, which opens a `Modal` with scrollable terms and an "I accept" CheckBox that gates the Accept Button. Once accepted, the row's `license_reviewed_at` persists the decision; the Modal does not re-open on subsequent uses. This matches the documented HF gated-model UX (`https://huggingface.co/docs/hub/en/models-gated`).

Decision-fatigue mitigations: the product default is pre-selected in every onboarding + settings view (already true per parent spec); the registry surfaces only expose invokable models to users via `GET /api/v1/models` (REQ-API-002); the admin surfaces show all lifecycle states but group by state and lead with `active`. Favorites / pinning is out of scope but recorded as a follow-up.

### 7.4 Why the agent confirms destructive actions even when intent is unambiguous

SOTA research documents a recurring failure mode — Cursor's Auto mode silently switches models mid-project; ChatGPT's router silently picks Instant when the user expected Thinking. The same pattern applied to a CRUD agent would be "I think you want to delete this model, done." The rule in GUD-004 is structural: the agent MUST NEVER apply a registry mutation without a terminal `ConfirmStateChange` surface. Even if the user said "delete Claude Haiku immediately," the agent emits a confirm. This is one of the most important UX guardrails in the spec — more important than any individual component choice.

### 7.5 Why license gating is explicit, not inferred

SPDX `apache-2.0` is permissive. It does not mean "brae has approved this model for production." License approval is a product decision that must be recorded with a human-identifiable reviewer and a timestamp. REQ-LIC-003 codifies this: enrichment surfaces the license text, but status stays `unreviewed` until an admin flips it. This is also why `license.reviewed_by` is a TEXT column tracking who signed off, not an automated label.

### 7.6 Why stay on A2UI v0.8

v0.9 adds `checks[]` / `Checkable` with `required` and `regex`, auto-disabled Buttons on check failure, and a cleaner `action`-key (replacing v0.8's `userAction`). These are all improvements we want — and they are the subject of a separate follow-up spec `spec-architecture-a2ui-v0.9-migration.md`. Mixing a v0.9 migration with this feature would double the risk and slow both.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: OpenRouter HTTPS API — already a dependency (parent spec); unchanged.
- **EXT-002**: Hugging Face Hub API (`https://huggingface.co/api/models/{id}`) — new dependency for license enrichment. Best-effort; brae tolerates 4xx/5xx by logging and leaving `license.status = 'unreviewed'`.
- **EXT-003**: Direct provider APIs (Google Gemini, Anthropic, OpenAI, self-hosted) — enabled by the `routes[]` abstraction. Individual adapter implementations are out of scope for this spec but the registry SHALL not prevent their future addition.

### Third-Party Services
- **SVC-001**: OpenRouter — continues as the default route for seeded models.
- **SVC-002**: Hugging Face — license metadata source. No SLA; enrichment is one-shot at registration and re-triggerable by admin.

### Infrastructure Dependencies
- **INF-001**: Postgres 16+ — extended with two new ENUM types, one new table (`models`), one new table (`model_role_defaults`), and triggers.

### Data Dependencies
- **DAT-001**: `users.preferred_model` and `users.model_overrides` — unchanged; remain TEXT. No FK.
- **DAT-002**: `scripts/seed-registry/seed_data.json` — new committed file, source for the seed migration.

### Technology Platform Dependencies
- **PLT-001**: Go 1.25+ (existing). No new language features required.
- **PLT-002**: React 19 / Vite 6 / TS 5.7 (existing). No new frontend framework dependencies — the A2UI surfaces reuse the existing custom v0.8 renderer.

### Compliance Dependencies
- **COM-001**: The onboarding consent copy (parent spec `spec-architecture-model-selection-centralization.md:REQ-ONB-002`) remains in force: users give informed consent to the default model's data-flow. This spec layers on per-model license review — a second compliance hop orthogonal to user consent.
- **COM-002**: License status transitions are audit-relevant. REQ-OBS-001 ensures every transition is logged with actor_id; the license-acceptance audit trail beyond row-level timestamps is a follow-up spec.

### Open Questions
- **DEP-004** (resolved): Admin identity source is the env-var email allowlist `LIWAISI_ADMIN_EMAILS`, enforced by `AdminMiddleware` at `back/go-assistant/internal/driving/httpapi/middleware_admin.go:12`. Case-insensitive match; trims whitespace; parsed by `ParseAdminEmails` at the same file, line 45. The existing `handler_admin.go` already uses this pattern for `ConfigProvider` endpoints — the model registry endpoints reuse it without introducing any new auth surface.

## 9. Examples & Edge Cases

### 9.1 Example — seed row for Gemma

```json
{
  "registry_id": "google/gemma-4-31b-it",
  "vendor": "google", "family": "gemma", "version": "4-31b-it",
  "display_name": "Google Gemma 4 31B Instruct",
  "modalities": {"input":["text"],"output":["text"]},
  "context": {"length": 262144, "tokenizer": "Gemma"},
  "pricing": {"input_per_token":"0.000000130","output_per_token":"0.000000380","currency":"USD"},
  "capabilities": {"text":true,"tools":true,"streaming":true,"reasoning":true,"structured_output":false},
  "license": {"kind":"community","community_slug":"apache-2.0","spdx_id":"apache-2.0","source":"manual","status":"approved-commercial"},
  "lifecycle": {"state":"active","registered_at":"2026-04-17T12:00:00Z"},
  "routes": [{"provider_adapter":"openrouter","provider_model_id":"google/gemma-4-31b-it","priority":0,"enabled":true}],
  "is_product_default": true
}
```

### 9.2 Edge case — attempted delete of default

```
DELETE /api/v1/admin/models/google/gemma-4-31b-it
→ HTTP 409 {"error":"CANNOT_DELETE_DEFAULT","detail":"Reassign the default before deleting this model."}
```

The admin must first `POST /api/v1/admin/models/<other>/set-default`, then retry the delete.

### 9.3 Edge case — race on set-default

Two admins simultaneously call `set-default` on different targets. The first transaction acquires a row lock via `SELECT product_default_model_id FROM registry_config WHERE id = 1 FOR UPDATE`, validates the target's invokability, updates the pointer, commits. The second transaction waits on the row lock, then re-reads state, sees the new default, validates its own target, and swaps the pointer again. Only the `registry_config` singleton is written in either transaction — no multi-row invariant to maintain. Service-layer code MUST use: `BEGIN; SELECT 1 FROM registry_config WHERE id = 1 FOR UPDATE; UPDATE models SET lifecycle_state = 'active' WHERE registry_id = $1 AND lifecycle_state IN ('registered','disabled') AND license_status IN ('approved-commercial','approved-non-commercial','restricted'); UPDATE registry_config SET product_default_model_id = (SELECT id FROM models WHERE registry_id = $1), updated_at = NOW(), updated_by = $2 WHERE id = 1; COMMIT;`.

### 9.4 Edge case — license enrichment returns 404

Admin registers a model with `hugging_face_id = "typo/does-not-exist"`. The HF API returns 404. The enricher:

1. Logs ERROR `"hf enrichment failed"` with status 404.
2. Writes the error to `source_metadata.huggingface_fetch_error`.
3. Stores `license_kind = 'unknown'`, `license_status = 'unreviewed'`, `license_source = 'inferred'`.
4. Returns the inserted row normally (201). Admin can edit `hugging_face_id` and re-trigger enrichment, or switch to manual entry.

### 9.5 Edge case — user in chat picks a model that was disabled between list-render and confirm

User receives `ModelCardList` at T0. At T1 (before user clicks), admin A disables model `x/y`. At T2 user clicks "Por defecto" on `x/y`. The confirm surface renders — diff shows "before: lifecycle=disabled, after: lifecycle=active AND registry_config.product_default_model_id=<x/y.id>". User accepts at T3. The service runs REQ-API-007: target's lifecycle is `disabled` → auto-activate to `active` if license is approved → swap the `registry_config` pointer. If license is NOT approved, the service rejects with `DEFAULT_NOT_INVOKABLE` and the agent emits an `alert` surface explaining why.

### 9.6 Edge case — responding-model surprise

User has `preferred_model = 'anthropic/claude-opus-4-6'`. At runtime, the route `anthropic` adapter is unreachable; `routes[]` priority 1 is `openrouter/anthropic/claude-opus-4-6`. Resolution picks priority 1. `responding_model` on the final chunk is `anthropic/claude-opus-4-6` via `openrouter`, which surfaces on the assistant bubble as a `RoundBadge` reading `claude-opus-4-6 · openrouter`. The user sees the fallback immediately rather than guessing why the answer smells different.

## 10. Validation Criteria

- `grep -RIn 'AvailableModels\s*=' back/go-assistant/` returns only the slice declaration in `openrouter.go` (kept for REQ-SEED-004 fallback). No call site reads from it at runtime except the fallback branch.
- `psql \c brae -c "SELECT COUNT(*) FROM registry_config"` returns exactly 1 after migration; `psql \c brae -c "SELECT m.registry_id FROM registry_config rc JOIN models m ON m.id = rc.product_default_model_id"` returns `google/gemma-4-31b-it`.
- Running `go test ./...` in `back/go-assistant` passes, including every test listed in §6.
- Running `npm test` in `front/react-assistant` passes, including the new ModelStep + MessageBubble tests.
- Manual: walk through the chat flow — type "list my models" → receive ModelCardList → click "Por defecto" on a non-default row → see ConfirmStateChange modal with correct before/after → accept → see replay card → `GET /api/v1/admin/models/:id` reflects the new default.
- Manual: admin registers a Mistral MRL model with `hugging_face_id = "mistralai/Mistral-Large-Instruct-2407"`. Enrichment populates `license.community_slug = 'mrl'`, `license.url = 'https://mistral.ai/licenses/MRL-0.1.md'`. Status is `unreviewed`. Admin reviews and sets `approved-non-commercial`. The model does NOT appear in `GET /api/v1/models` for ordinary users until it is set to `active` AND the session caller provides `accept_restricted: true` (if status were `restricted` rather than `approved-non-commercial`).

## 11. Related Specifications / Further Reading

### Parent / sibling specs

- [`spec-architecture-model-selection-centralization.md`](spec-architecture-model-selection-centralization.md) — **Parent.** This spec **extends** the three-level cascade REQ-CFG-005 by sourcing the product default from the registry instead of the compile-time constant. REQ-CFG-001 is **superseded** by REQ-SEED-002 + REQ-REG-008: the hardcoded constant remains only as the REQ-SEED-004 fallback. REQ-ONB-* (onboarding wizard) is preserved unchanged. REQ-MIG-* and REQ-PAR-* (Gemma parser hardening) are unaffected.
- [`spec-architecture-a2a-a2ui-protocol-integration.md`](spec-architecture-a2a-a2ui-protocol-integration.md) — A2UI renderer design. This spec reuses the renderer without extending its catalog.
- [`spec-architecture-cpn-iterative-clarification-loop.md`](spec-architecture-cpn-iterative-clarification-loop.md) — The clarify-loop topology this spec's management flow mirrors in shape (HITL surface → response → validate → re-emit-or-apply → lock-replay).
- [`spec-process-bugfix-a2ui-hitl-response-persistence.md`](spec-process-bugfix-a2ui-hitl-response-persistence.md) — ParentMessageID linking pattern reused by REQ-CPN-004.
- [`spec-design-user-preferences-settings-page.md`](spec-design-user-preferences-settings-page.md) — SettingsPage contract; this spec adds a single inline link without structural change.
- [`spec-architecture-block20-llm-streaming.md`](spec-architecture-block20-llm-streaming.md) — SSE stream model; this spec extends the final `stream_chunk` payload with `responding_model` (REQ-FE-005).
- [`spec-architecture-setup-mode-onboarding-wizard.md`](spec-architecture-setup-mode-onboarding-wizard.md) — Onboarding integration; `ModelStep.tsx` change in REQ-FE-002 is additive.

### External references

- A2UI v0.8 specification — `https://a2ui.org/specification/v0.8-a2ui/`
- A2UI standard catalog — `https://raw.githubusercontent.com/google/A2UI/main/specification/v0_8/json/standard_catalog_definition.json`
- A2UI component gallery — `https://a2ui.org/reference/components/`
- A2UI actions + data binding — `https://a2ui.org/concepts/actions/`, `https://a2ui.org/concepts/data-binding/`
- OpenRouter `/api/v1/models` — `https://openrouter.ai/docs/api/api-reference/models/get-models`
- OpenRouter models overview — `https://openrouter.ai/docs/guides/overview/models`
- OpenRouter model-routing fallbacks (responding-model pattern) — `https://openrouter.ai/docs/guides/routing/model-fallbacks`
- Hugging Face Hub API — `https://huggingface.co/docs/hub/en/api`
- Hugging Face gated-model consent UX — `https://huggingface.co/docs/hub/en/models-gated`
- Open WebUI model configuration (is_active × meta.hidden) — `https://docs.openwebui.com/features/workspace/models/`
- Open WebUI hidden-default bug discussion #14004 — `https://github.com/open-webui/open-webui/discussions/14004`
- LibreChat `modelSpecs` (reference for config-driven defaults) — `https://www.librechat.ai/docs/configuration/librechat_yaml/object_structure/model_specs`

### Follow-up specs

- `spec-process-openrouter-registry-sync.md` — scheduled ingestion of OpenRouter's /models feed.
- `spec-architecture-byok-model-credentials.md` — user-supplied API keys.
- `spec-architecture-workspace-registry.md` — per-workspace visibility and overrides.
- `spec-design-model-compare-pane.md` — side-by-side multi-model compare.
- `spec-process-license-acceptance-audit.md` — immutable audit trail for license reviews.
- `spec-architecture-mcp-model-tool-matrix.md` — per-model MCP tool capability.
- `spec-architecture-a2ui-v0.9-migration.md` — catalog + action-key upgrade.
