---
title: LLM Model Selection Centralization — Gemma-Default + User Preference
version: 1.0
date_created: 2026-04-15
last_updated: 2026-04-15
owner: backend + frontend
tags: [architecture, configuration, llm, openrouter, onboarding]
---

# Introduction

Today every CPN transition picks its LLM through a tangle of four mechanisms: hard-coded role aliases (`"classifier"`, `"structured"`, …), per-role `MODEL_*` environment variables, per-transition literal strings in `topologies.go`, and — only at the user-preference layer — `UserRecord.PreferredModel` / `ModelOverrides`. When the operator flips a `MODEL_*` variable or a user updates their preferences, the topology JSON changes, its SHA-256 hash changes, and a brand-new `flows` row is created for what is semantically the same topology. Worse: the current defaults (Claude Sonnet, Gemini Flash) are expensive, and the JSON-producing transitions (`t-ask`) silently break when a user picks a non-structured model because `firstQuestionnaireFromTokens` cannot decode the response.

This specification replaces the ENV-based defaults with a single hard-coded default model — `google/gemma-4-31b-it` — and makes user preference the **only** legitimate override. It also captures the root-cause analysis of the observed Gemma regression (empty JSON extraction for `t-ask`) and mandates parser hardening so a model change can never again route users into a dead HITL surface.

## 1. Purpose & Scope

### Purpose

1. Remove operator-facing `MODEL_*` / `DEFAULT_MODEL` environment variables from the backend. The concrete model for every CPN transition comes from (in order of precedence):
   1. The user's per-role override (`UserRecord.ModelOverrides[<role>]`).
   2. The user's global preference (`UserRecord.PreferredModel`).
   3. The product default baked into the code: `google/gemma-4-31b-it`.
2. Surface the global preference in the existing onboarding wizard so first-run users see and can accept or change the default before their first session.
3. Harden the `t-ask` / `t-clarify` JSON pipeline so that Gemma's reasoning-style output (and any comparable model) cannot produce an undecodable token, which currently falls back to a generic Approve/Reject card and strands the user.

### In Scope

- `back/go-assistant/infra/openrouter/openrouter.go` (role registry, defaults).
- `back/go-assistant/internal/config/config.go` (ENV bindings).
- `back/go-assistant/cmd/server/topologies.go` (per-transition `Model` literals, `buildClarifyA2UIPayload`, `firstQuestionnaireFromTokens`, `extractJSONObject`).
- `back/go-assistant/internal/app/session_service.go::applyUserModelPreferences`.
- `back/go-assistant/internal/driving/httpapi/handler_models.go` + `handler_user.go`.
- `front/react-assistant/src/features/setup/*` (onboarding wizard) and `features/settings/SettingsPage.tsx`.
- Postgres: `users.preferred_model`, `users.model_overrides`, and the `flows` table life-cycle.

### Out of Scope

- Re-designing the onboarding wizard itself (REQ-ONB-* assume the existing `SetupPage` step chain and only add a step to it).
- A cost-aware routing layer that picks a model per request based on prompt length — that is a separate future spec.
- Removing `PROMPT_*` / `MAX_TOKENS_*` environment variables. Only model-selection ENVs are removed here; prompt and token-limit tuning remain operator-controlled.
- Building a model catalog UI richer than the existing `/api/v1/models` dropdown.

### Audience

Backend and frontend engineers implementing the change. The document is written so an AI code-generation agent can implement each REQ independently and verify it against the acceptance criteria without needing the original conversation transcript.

## 2. Definitions

| Term | Definition |
|------|------------|
| **Transition** | A node in a Coloured Petri Net (CPN) that performs work — in this spec, always an `NodeKindLLM` transition that consults an LLM. |
| **Role** | A logical model class used by the backend: `classifier`, `structured`, `reasoning`, `long-context`, `summarize`, `thinking`, `direct` (implicit). A role is an alias that resolves to a concrete OpenRouter model id. |
| **OpenRouter model id** | The canonical provider/model string, e.g. `google/gemma-4-31b-it`, that OpenRouter's `/chat/completions` endpoint accepts verbatim. |
| **ModelRegistry** | The Go map that resolves role → OpenRouter model id inside `infra/openrouter/openrouter.go`. |
| **Topology hash** | SHA-256 over a canonical serialization of all places and transitions (including each transition's `LLMConfig.Model`), computed by `cpn/persist/topology.go::TopologyHash`. Changing any transition's model string changes the hash and produces a new `flows` row. |
| **A2UI surface** | The interactive JSON payload (prefixed with `$$a2ui:` on the wire) the frontend renders as an interactive component. For `t-clarify` this is a questionnaire; for the fallback path it is a generic Review Required card with Approve/Request Changes/Discard buttons. |
| **HITL** | Human-in-the-loop. A CPN transition of kind `NodeKindHITL` that blocks on a user action before depositing a token downstream. |
| **PRODUCT_DEFAULT_MODEL** | The single compile-time constant introduced by this spec; equals `google/gemma-4-31b-it` until a future spec changes it. |
| **Onboarding** | The `SetupPage` first-run wizard the user walks through after signing in. Completion is recorded via `POST /api/v1/user/onboarding/complete`. |

## 3. Requirements, Constraints & Guidelines

### Configuration surface (REQ-CFG-*)

- **REQ-CFG-001**: `back/go-assistant/infra/openrouter/openrouter.go` MUST define a single exported constant `PRODUCT_DEFAULT_MODEL = "google/gemma-4-31b-it"`. Every entry in `DefaultModelRegistry` MUST map to that constant. No per-role default deviates from the product default.
- **REQ-CFG-002**: The following environment variables MUST be removed from `infra/openrouter/openrouter.go::buildModelRegistry` and `internal/config/config.go`: `DEFAULT_MODEL`, `MODEL_CLASSIFIER`, `MODEL_STRUCTURED`, `MODEL_REASONING`, `MODEL_LONG_CONTEXT`, `MODEL_SUMMARIZE`, `MODEL_THINKING`. Their removal MUST delete the corresponding lines in `.env.example`, the Compose file, and any deployment manifests. Reading an unknown key must not silently fall back — the fields are gone from the `Config` struct.
- **REQ-CFG-003**: `LLMConfig.Model` in `cmd/server/topologies.go` MUST NOT hold role aliases (`"classifier"`, `"structured"`, …). Every literal that previously held a role string is replaced with `""` (empty string), and the call sites rely on `applyUserModelPreferences` + the OpenRouter resolver to substitute the effective model. The role, if semantically relevant for routing, is carried on a new `LLMConfig.Role` field (introduced in REQ-CFG-004).
- **REQ-CFG-004**: `cpn.LLMConfig` MUST gain a new field `Role string` (JSON: `"role,omitempty"`). The topology assigns `Role: "classifier"` to `t-classify`, `Role: "structured"` to `t-ask`, and leaves the rest empty. The role drives per-role user overrides but MUST NOT affect the topology hash differently than the old `Model` string did — migration (REQ-MIG-001) replays all existing `flows` rows through the new canonicalization so the new default produces a stable hash.
- **REQ-CFG-005**: The effective model for a transition MUST be computed by `applyUserModelPreferences` in `internal/app/session_service.go` using this exact precedence (highest wins):
  1. `UserRecord.ModelOverrides[t.LLMConfig.Role]` if `t.LLMConfig.Role != ""` and the override is non-empty.
  2. `UserRecord.PreferredModel` if non-empty.
  3. `openrouter.PRODUCT_DEFAULT_MODEL`.

  No ENV lookup MUST occur inside this function.

### Onboarding & user preference (REQ-ONB-*)

- **REQ-ONB-001**: The onboarding wizard (`front/react-assistant/src/features/setup/SetupPage.tsx`) MUST add a step titled "Model" positioned immediately before the existing `ReadyStep`. The step displays a single `<select>` seeded from `GET /api/v1/models` with the `PRODUCT_DEFAULT_MODEL` pre-selected.
- **REQ-ONB-002**: The "Model" step MUST include a one-paragraph, plain-language explanation that the selected model runs every brae task, that it can be changed later in Settings, and that the default (Gemma) is the product's recommended balance of cost and capability. Copy lives in `i18n/locales/{en,es}/setup.json` under key `model.description`.
- **REQ-ONB-003**: On onboarding submit, the request `POST /api/v1/user/onboarding/complete` MUST include a non-empty `preferred_model`. The handler (`handler_user.go::CompleteOnboarding`) MUST reject a missing or empty value with HTTP 400 and error code `ONBOARDING_MODEL_REQUIRED`.
- **REQ-ONB-004**: If a legacy user (created before this spec) has `preferred_model = ""` AND `model_overrides = {}`, the system MUST treat them as mid-onboarding: `GET /api/v1/user/profile` returns `onboarding_completed: false` and the frontend MUST route them into the onboarding wizard even if they completed onboarding under the old schema. Backfill (REQ-MIG-002) may be used as an alternative and MUST be explicitly chosen at migration time.
- **REQ-ONB-005**: The existing `SettingsPage` MUST keep the global model picker and the per-role override table. The per-role table's keys are sourced from `GET /api/v1/models` (which returns registry roles), so no frontend change is required when new roles are added to the backend registry.

### Migration (REQ-MIG-*)

- **REQ-MIG-001**: A new Postgres migration (`019_flows_recanonicalize.up.sql`) MUST soft-delete (`deleted_at = NOW()`) every row in `flows` whose `topology_json` contains any `LLMConfig.Model` equal to a legacy role alias (`"classifier"`, `"structured"`, `"reasoning"`, `"long-context"`, `"summarize"`, `"thinking"`, `"structured-cheap"`). Soft-delete — never hard-delete — preserves monitoring data. A matching `.down.sql` restores `deleted_at = NULL` for the same rows.
- **REQ-MIG-002 (optional)**: An operator-triggered backfill SQL statement (provided in the PR description, not auto-applied) sets `preferred_model = 'google/gemma-4-31b-it'` for any `users` row where `preferred_model = ''`. If the operator skips the backfill, REQ-ONB-004 catches those users.
- **REQ-MIG-003**: The `internal/version.Version` string MUST be bumped to a release that signals the configuration change. The backend MUST log at startup, at `INFO` level: `"using product default model"`, `model=google/gemma-4-31b-it`. No warning is logged about removed ENVs — they are simply no longer read.

### Parser hardening (REQ-PAR-*)

- **REQ-PAR-001**: `cmd/server/topologies.go::extractJSONObject` MUST, before scanning for braces, strip any leading `<think>...</think>` block (case-insensitive, dot-matches-newline) emitted by reasoning-mode models. It MUST also strip fenced code blocks (```json / ```). The function MUST return the first balanced `{…}` object that successfully `json.Unmarshal`s into `questionnaireSpec` with a non-empty `Questions` slice OR `Assumptions` slice OR `RestatedGoal`, scanning forward when the first candidate fails.
- **REQ-PAR-002**: `buildClarifyA2UIPayload` MUST NOT return `(nil, nil)`. Any unrecoverable decode failure MUST return `(nil, err)`, and `err` MUST wrap the original decode error with the first 512 bytes of the raw LLM output for debugging. The backend logs the error at `slog.LevelError` with `session_id`, `cpn_id`, `transition_id`, and `model` fields.
- **REQ-PAR-003**: When `A2UIPayloadBuilder` returns an error, `fireHITL` MUST continue to emit `EventHITLRequested` with `CustomSurface: false` (preserving today's fallback to the generic Review card) BUT the integration layer (`cmd/server/main.go:250-292`) MUST additionally emit an `EventStreamChunk` whose content is a localised error banner — "Your selected model did not return a valid questionnaire. You can approve the request as-is, reject it, or change the model in Settings." — so the user knows why the questionnaire is missing. The banner uses the plain-text bubble path (no `$$a2ui:` prefix).
- **REQ-PAR-004**: The t-ask transition's `LLMConfig` MUST add a new field `ResponseFmtRequired bool` (JSON: `"response_fmt_required,omitempty"`) set to `true`. When `true`, `fire_llm.go` MUST — after a successful HTTP response — verify that `extractJSONObject(response)` returns a non-empty string; if not, it MUST retry the call once with a terser "JSON only, no thinking, no markdown fences" instruction appended to the system prompt. Cost of the retry is attributed to the same transition.

### Observability (REQ-OBS-*)

- **REQ-OBS-001**: Every LLM call MUST log at `INFO`: `session_id`, `cpn_id`, `transition_id`, `role`, `resolved_model`. This makes "which model ran for this user/session" directly answerable from the log stream.
- **REQ-OBS-002**: The `/api/v1/models` response MUST include a `default` key set to `PRODUCT_DEFAULT_MODEL` so the frontend does not have to re-state it.

### Constraints

- **CON-001**: Breaking the hash stability of `flows` is acceptable once; the migration (REQ-MIG-001) documents the one-shot invalidation. Future changes to `LLMConfig` fields MUST NOT recanonicalize hashes.
- **CON-002**: Removing ENVs is a breaking change for self-hosters who override the defaults today. The PR description MUST include a migration blurb and point to the user settings endpoint as the replacement.
- **CON-003**: `google/gemma-4-31b-it` is the default only while this spec is the most recent model-selection spec. When a future spec changes the default, it MUST edit `PRODUCT_DEFAULT_MODEL` in one place.
- **CON-004**: OpenRouter's `response_format: {"type":"json_object"}` is best-effort for Gemma (REQ-PAR-001/004 compensate). Do not assume it is honoured.

### Guidelines

- **GUD-001**: When a call site needs the effective model for telemetry or auditing, it MUST ask `applyUserModelPreferences` rather than re-resolving — the resolver is the single source of truth.
- **GUD-002**: Prefer narrow, targeted tests over broad e2e in this area. A pure unit test over `extractJSONObject` covers the high-volume regression surface (reasoning preamble, fenced JSON, prose wrappers) at a fraction of the cost.

### Patterns

- **PAT-001**: Three-level preference cascade (override → preference → product default) is the same pattern used by `resolveModel` today; this spec only removes the ENV middle rung.

## 4. Interfaces & Data Contracts

### Go types

```go
// infra/openrouter/openrouter.go
const PRODUCT_DEFAULT_MODEL = "google/gemma-4-31b-it"

// cpn/types.go (LLMConfig extension)
type LLMConfig struct {
    Model               string  `json:"model,omitempty"`
    Role                string  `json:"role,omitempty"`                 // REQ-CFG-004
    MaxTokens           int     `json:"max_tokens,omitempty"`
    Temperature         float64 `json:"temperature,omitempty"`
    StreamOutput        bool    `json:"stream_output,omitempty"`
    RequireJSON         bool    `json:"require_json,omitempty"`
    ResponseFmtRequired bool    `json:"response_fmt_required,omitempty"` // REQ-PAR-004
    // unchanged: SkipHistory, SkipOutputHistory, SkipRegionalPreamble, …
}

// internal/app/session_service.go (signature unchanged; body simplified)
func (s *SessionService) applyUserModelPreferences(user *persist.UserRecord, c *cpn.CPN) {
    for _, t := range c.Transitions {
        if t.Kind != cpn.NodeKindLLM || t.LLMConfig == nil {
            continue
        }
        t.LLMConfig.Model = resolveForUser(user, t.LLMConfig.Role)
    }
}

func resolveForUser(u *persist.UserRecord, role string) string {
    if u != nil && role != "" {
        if m, ok := u.ModelOverrides[role]; ok && m != "" {
            return m
        }
        if u.PreferredModel != "" {
            return u.PreferredModel
        }
    }
    return openrouter.PRODUCT_DEFAULT_MODEL
}
```

### REST API

| Method | Path | Change |
|--------|------|--------|
| `GET`  | `/api/v1/models` | Response gains `default: string` field equal to `PRODUCT_DEFAULT_MODEL`. |
| `POST` | `/api/v1/user/onboarding/complete` | `preferred_model` becomes REQUIRED; empty-string returns 400 `ONBOARDING_MODEL_REQUIRED`. |
| `PUT`  | `/api/v1/user/preferences` | No shape change; clearing `preferred_model` to `""` is allowed and re-routes the user to onboarding per REQ-ONB-004. |

Response example for `GET /api/v1/models`:

```json
{
  "default": "google/gemma-4-31b-it",
  "roles": [
    {"role": "classifier",   "current": "google/gemma-4-31b-it"},
    {"role": "structured",   "current": "google/gemma-4-31b-it"},
    {"role": "reasoning",    "current": "google/gemma-4-31b-it"},
    {"role": "long-context", "current": "google/gemma-4-31b-it"},
    {"role": "summarize",    "current": "google/gemma-4-31b-it"},
    {"role": "thinking",     "current": "google/gemma-4-31b-it"}
  ],
  "available": [
    "google/gemma-4-31b-it",
    "anthropic/claude-sonnet-4-6",
    "anthropic/claude-opus-4-6",
    "google/gemini-2.0-flash-001"
  ]
}
```

### Frontend store shape

```ts
// features/setup/state.ts additions
interface SetupState {
  // …existing fields…
  preferredModel: string; // initialised to backend's default from GET /api/v1/models
}
```

## 5. Acceptance Criteria

- **AC-001**: Given an operator starts the backend with no `MODEL_*` ENV variables set, When the backend boots, Then the startup log MUST include `using product default model model=google/gemma-4-31b-it` exactly once.
- **AC-002**: Given a fresh user with `preferred_model = ""`, When they call any session-creating endpoint, Then every LLM transition in the spawned CPN resolves to `google/gemma-4-31b-it` and `applyUserModelPreferences` MUST be the only function that sets `LLMConfig.Model`.
- **AC-003**: Given a user with `preferred_model = "anthropic/claude-sonnet-4-6"` and `model_overrides = {"classifier": "google/gemini-2.0-flash-001"}`, When they start a session, Then `t-classify` resolves to `google/gemini-2.0-flash-001` and every other LLM transition resolves to `anthropic/claude-sonnet-4-6`.
- **AC-004**: Given an existing flow row whose `topology_json` contains `"model": "classifier"`, When the 019 migration runs, Then that row's `deleted_at` is set and a `SELECT COUNT(*)` on non-deleted flows returns 0 legacy rows.
- **AC-005**: Given `t-ask` returns the string `<think>Let me reason...</think>\n\`\`\`json\n{"restated_goal":"x","questions":[]}\n\`\`\``, When `extractJSONObject` runs, Then it returns `{"restated_goal":"x","questions":[]}` and `firstQuestionnaireFromTokens` returns a `questionnaireSpec` with `RestatedGoal == "x"`.
- **AC-006**: Given `t-ask` returns an empty string, When `buildClarifyA2UIPayload` runs, Then it returns `(nil, err)` with `err.Error()` containing `"empty LLM output"` and the first 512 bytes of the offending response. The backend MUST log the error at `slog.LevelError` with the session id.
- **AC-007**: Given `buildClarifyA2UIPayload` returns an error at runtime, When `t-clarify` fires, Then the user sees BOTH the generic Review Required card AND a plain-text error banner explaining the model did not return a valid questionnaire (REQ-PAR-003).
- **AC-008**: Given the user opens onboarding for the first time, When the "Model" step renders, Then the `<select>` shows `google/gemma-4-31b-it` as the initially-selected option and disables the "Continue" button only when a non-empty value is not selected.
- **AC-009**: Given a user who completed onboarding under the old schema (no `preferred_model`), When `GET /api/v1/user/profile` is called, Then the response contains `"onboarding_completed": false` and the frontend router sends them to `SetupPage`.
- **AC-010**: Given `GET /api/v1/models` is called, When the response body is parsed, Then `default === "google/gemma-4-31b-it"`.

## 6. Test Automation Strategy

- **Test Levels**: unit (Go + Vitest), integration (Go against pgxmock for migration, testcontainers for session-service), e2e (Playwright for onboarding — optional if an existing e2e harness already covers `SetupPage`).
- **Frameworks**: Go `testing` package + `pgxmock`; React Testing Library + Vitest; Playwright for wizard click-through.
- **Test Data Management**: fixtures for `extractJSONObject` live alongside the function in `topologies_parser_test.go`; onboarding fixtures live in `features/setup/__tests__/`. No shared golden files — each test constructs its inputs inline to stay self-contained.
- **CI/CD Integration**: existing GitHub Actions workflow. Add a job `gemma-jsonobject-replay` that feeds a corpus of 20 captured real Gemma responses (redacted) into `extractJSONObject` and asserts each decodes. Corpus lives at `back/go-assistant/cmd/server/testdata/gemma_responses/`.
- **Coverage Requirements**: new code under `topologies.go`, `handler_models.go`, `applyUserModelPreferences` MUST reach 90% line coverage. The onboarding step MUST have component-level tests covering the default-selected and user-overrides-selection branches.
- **Performance Testing**: no new perf bar — model latency is unchanged. The parser's additional strip passes are O(n) over response size and MUST not regress existing `extractJSONObject` benchmarks by more than 10%.

## 7. Rationale & Context

### Why remove the ENV variables

The observed bug had two root causes. (1) The operator or a deployment manifest had set `DEFAULT_MODEL` — or a `MODEL_*` override — to `google/gemma-4-31b-it`. That change flowed through `buildModelRegistry`, into `resolveModel`, into every transition's `Model` field, and from there into the topology JSON. (2) Because the topology hash is deterministic over `LLMConfig.Model`, a new `flows` row was created the moment the ENV changed. Two separate issues, one trigger. Removing the ENVs collapses the surface area to exactly one source of truth per user and makes the hash change intentional (triggered by the user, visible in `users.preferred_model`) rather than environmental.

### Why Gemma broke the questionnaire

OpenRouter's Gemma 4 31B offering enables reasoning mode by default and advertises "thinking" output that prepends `<think>…</think>` to the user-visible answer. The current `extractJSONObject` scans forward for the first `{` — which often lives inside the reasoning block — and then tries to balance braces. The reasoning prose frequently contains JSON-like fragments (`{intent: task}` written as free text), so the extractor returns a syntactically balanced but semantically garbage object that fails `json.Unmarshal` into `questionnaireSpec`. The classifier survives because `classifierResult.parseClassified` is tolerant of unexpected fields; the questionnaire path is strict. REQ-PAR-001 and REQ-PAR-004 are the minimum-viable hardening that keeps Gemma usable without forcing every operator to pay for a structured-output-capable model.

### Why make the default a single constant

Centralizing the default in code (rather than in `.env.example`) means "what does brae use today?" is a question the code answers, not a question the operator's environment answers. This is the same reasoning that led to `braeIdentity` being a compile-time constant.

### Why onboarding, not a silent default

REQ-ONB-001 exists because a default is a **privacy-adjacent** choice — the user's prompts are sent to OpenRouter and routed to Google's servers when Gemma is selected. Surfacing the default in onboarding preserves the "Privacidad radical" principle documented in the agent identity: no surprise data flows, and the user has an explicit moment to pick a different provider. This is also why REQ-ONB-003 fails the request when `preferred_model` is empty — silent acceptance of the default would undermine that.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: OpenRouter HTTPS API — required for model resolution and chat completion. SLA is best-effort; brae tolerates 5xx responses via existing retry logic in `infra/openrouter/client.go`.

### Third-Party Services
- **SVC-001**: `google/gemma-4-31b-it` served via OpenRouter. Input $0.13/M, output $0.38/M, 262K context, reasoning mode enabled by default. No structured-output guarantee.

### Infrastructure Dependencies
- **INF-001**: Postgres 16 for the `users` and `flows` tables and the 019 migration.

### Data Dependencies
- **DAT-001**: `users.preferred_model` (TEXT) and `users.model_overrides` (JSONB) — both already exist per `store/postgres/user.go`. No new columns.

### Technology Platform Dependencies
- **PLT-001**: Go 1.25+ (existing). No new language-level features required.

### Compliance Dependencies
- **COM-001**: The onboarding copy (REQ-ONB-002) MUST state which provider runs the default model so users give informed consent to the cross-border data flow implied by OpenRouter → Google.

## 9. Examples & Edge Cases

### Example — Gemma reasoning output

Raw LLM output that currently breaks `firstQuestionnaireFromTokens`:

```
<think>
The user asked about a pitch deck. I'll structure the questionnaire around timing and audience.
{this is not real JSON, just my working notes}
</think>

```json
{
  "restated_goal": "Prepare an angel-investor pitch for brae tomorrow.",
  "assumptions": ["Investor knows brae exists in broad strokes."],
  "questions": [
    {"id":"q1","prompt":"¿De cuánto tiempo dispones?","quote_from_user":"es mañana","why_it_matters":"Changes scope.","recommended":"opt-a","options":[{"id":"opt-a","label":"20-30 min"},{"id":"opt-b","label":"60 min+"}]}
  ]
}
```

After REQ-PAR-001 the parser strips the `<think>…</think>` block and the fenced code block, then decodes the inner object cleanly.

### Edge case — empty response

LLM returned `""`. `extractJSONObject("")` returns `""`. `firstQuestionnaireFromTokens` returns `(_, err)` wrapping the raw response. `buildClarifyA2UIPayload` returns `(nil, err)`. `fireHITL` proceeds with `CustomSurface: false`. The integration layer emits (a) the generic Review card (existing behaviour) and (b) an explanatory error banner (REQ-PAR-003). The user can still approve/reject; nothing is wedged.

### Edge case — user clears their preference

`PUT /api/v1/user/preferences` with `{"preferred_model": ""}` succeeds. The user's next `GET /api/v1/user/profile` returns `onboarding_completed: false`. The frontend routes to `SetupPage`. The user must re-pick before any session can start. This is intentional (REQ-ONB-004) — an empty preference is always unambiguous.

### Edge case — legacy flow replay after migration

A pre-migration session persisted with `topology_json.transitions["t-ask"].llm_config.model == "structured"`. After the 019 migration the row is soft-deleted. When the session is re-loaded, the system creates a new `flows` row with `model: ""` and `role: "structured"`, then `applyUserModelPreferences` stamps the effective model based on the user. Session history is preserved; only the `flows` row is replaced.

## 10. Validation Criteria

- `grep -RIn 'MODEL_CLASSIFIER\|MODEL_STRUCTURED\|MODEL_REASONING\|MODEL_LONG_CONTEXT\|MODEL_SUMMARIZE\|MODEL_THINKING\|DEFAULT_MODEL' back/go-assistant/` returns zero matches outside of `.md` docs.
- Every `LLMConfig.Model` literal in `cmd/server/topologies.go` is `""` or removed; every role is on `LLMConfig.Role`.
- `SELECT COUNT(*) FROM flows WHERE deleted_at IS NULL AND topology_json::text LIKE '%"model":"classifier"%'` returns 0 after migration.
- Running `go test ./...` in `back/go-assistant` passes, including the new `TestExtractJSONObject_StripsThinkAndFences` and `TestBuildClarifyA2UIPayload_ErrorOnEmpty` tests.
- Running `npm test` in `front/react-assistant` passes, including tests for the new onboarding step and the unchanged `SettingsPage`.
- Manual check: start the backend, create a user, walk through onboarding, create a session, type `"Tengo un angel investor mañana, no tengo nada"`. The response MUST be a rendered questionnaire surface (not the generic Review card). The backend log MUST show `resolved_model=google/gemma-4-31b-it` for every LLM call in that session.

## 11. Related Specifications / Further Reading

- `/spec/spec-architecture-setup-mode-onboarding-wizard.md` — the wizard this spec extends.
- `/spec/spec-process-bugfix-a2ui-hitl-response-persistence.md` — the HITL-persistence fix that neighbours the `t-clarify` code path.
- `/spec/spec-architecture-tools-engine-agent-personality.md` — where the agent identity preamble (`braeIdentity`) is canonicalized, mirroring the centralization pattern used here.
- OpenRouter model card: https://openrouter.ai/google/gemma-4-31b-it
