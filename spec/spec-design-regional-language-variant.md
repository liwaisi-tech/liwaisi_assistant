---
title: Regional Language Variant Preference for LLM Prompt Augmentation
version: 1.0
date_created: 2026-04-07
last_updated: 2026-04-07
owner: liwaisi-tech
tags: [design, app, frontend, backend, i18n, llm, prompts]
---

# Introduction

This specification defines a user-level **regional language variant** preference, collected during onboarding and persisted server-side, that augments backend LLM system prompts so the classifier and conversational transitions interpret regional idioms correctly. The motivating defect: a Colombian Spanish speaker writing "cuenta del uno al veinte" was misclassified as a `task` (because the classifier read `cuenta` as the imperative "count"); in Colombian Spanish `cuenta` is also frequently a request for gossip ("cuéntame el cuento"). A single global Spanish prompt cannot disambiguate these registers without a regional hint.

## 1. Purpose & Scope

**Purpose.** Add a per-user regional variant (BCP-47 code) selected at onboarding, persisted in the user record, and injected as a short preamble into the system prompt of every LLM transition for sessions belonging to that user. The variant influences only LLM context — it does **not** change UI i18n (which remains binary `es` / `en`).

**In scope:**
- New `regional_variant` column on the `users` table (BCP-47 code).
- Onboarding UI step to select/confirm the variant after the language step, with sensible regional defaults.
- Backend onboarding handler validation, repository update, and DTO surface.
- A per-session prompt augmentation hook in the CPN engine that prepends a "user context" preamble to the SystemPrompt of LLM transitions.
- Refinement of the classifier prompt to defer regional disambiguation to the injected preamble.

**Out of scope:**
- Adding new UI translation namespaces (e.g., a separate `es-CO` translation file).
- Per-message variant override.
- Locale-aware date/number formatting in the frontend.
- Migrations that backfill historical sessions.

**Audience.** Engineers implementing this feature on `back/go-assistant` (Go) and `front/react-assistant` (React/TypeScript), and reviewers verifying acceptance.

## 2. Definitions

| Term | Definition |
|---|---|
| **BCP-47** | IETF language tag standard (e.g., `es-CO` = Spanish, Colombia). |
| **Regional variant** | A BCP-47 tag from the supported set, identifying both base language and region. |
| **UI language** | The base language used by frontend i18n (`es` or `en`). |
| **CPN** | Coloured Petri Net — the topology engine that executes LLM transitions. |
| **Transition** | A node in the CPN topology; an LLM transition has a `SystemPrompt` and an `LLMConfig`. |
| **t-classify** | LLM transition that classifies user intent as `conversation` or `task`. |
| **t-direct** | LLM transition that streams a conversational reply. |
| **Prompt preamble** | A short text block prepended to a transition's `SystemPrompt` at invocation time, providing per-session user context. |
| **Onboarding** | The first-run wizard collecting language, model, personality, and (now) regional variant. |

## 3. Requirements, Constraints & Guidelines

### Functional requirements

- **REQ-001**: The `users` table SHALL gain a nullable `regional_variant TEXT` column. NULL means "not set" and the system MUST treat it as the language-default variant (`es` → `es-CO`, `en` → `en-GB`).
- **REQ-002**: The supported variant set is exactly: `es-CO`, `es-MX`, `es-AR`, `es-ES`, `en-GB`, `en-US`, `en-AU`. Any other value SHALL be rejected by the onboarding handler with HTTP 400.
- **REQ-003**: Default-by-language MUST be `es-CO` for `preferred_language="es"` and `en-GB` for `preferred_language="en"`.
- **REQ-004**: The SetupWizard SHALL include a step that lets the user confirm or change the variant after the language step. The default selection MUST follow REQ-003.
- **REQ-005**: The onboarding completion request SHALL carry a `regional_variant` field. The handler SHALL validate it against REQ-002 and persist it via the user repository.
- **REQ-006**: A per-session prompt augmentation hook SHALL be added to the CPN engine. When an LLM transition is invoked, the engine SHALL look up the session's user, fetch their `regional_variant`, render a fixed-format preamble, and prepend it to the transition's static `SystemPrompt` before the LLM call.
- **REQ-007**: The preamble SHALL be applied to at least `t-classify` and `t-direct`. Other LLM transitions MAY opt in via a per-transition flag; default opt-in is `true`.
- **REQ-008**: When `regional_variant` is unset on the user (anonymous, pre-onboarding, or NULL), the engine SHALL still inject a preamble using the language-default variant computed from `preferred_language`, falling back to `es-CO` if both are unset.
- **REQ-009**: The classifier prompt SHALL be refined so that disambiguation of regional idioms is delegated to the injected preamble, not hard-coded.

### Security requirements

- **SEC-001**: The `regional_variant` field SHALL be treated as plain user preference data, not PII. No additional authorization beyond existing onboarding auth is required.
- **SEC-002**: The handler MUST reject any value not in the REQ-002 whitelist; arbitrary strings MUST NOT reach the LLM preamble (prompt-injection guard).

### Constraints

- **CON-001**: Frontend i18n MUST remain binary (`es` / `en`). No new locale namespaces.
- **CON-002**: No new database table — `regional_variant` is a column on `users`.
- **CON-003**: The augmentation hook MUST NOT mutate `Transition.SystemPrompt` in place (transitions are shared across sessions). It MUST produce a per-invocation rendered prompt.
- **CON-004**: Token budget impact MUST be ≤ 80 tokens per call (preamble length cap).
- **CON-005**: The hook MUST be a no-op for non-LLM transitions and MUST NOT affect HITL or guard transitions.

### Guidelines

- **GUD-001**: Variant preamble SHOULD be written as **descriptive context**, not as instructions ("The user writes in Colombian Spanish; in this register `cuenta` often means `tell me the story`"), so the LLM uses it for interpretation rather than mechanical rules.
- **GUD-002**: Preambles SHOULD list only **2–4** of the most disambiguating idioms per variant — broad coverage is the LLM's job, the preamble is the hint.
- **GUD-003**: The variant SHOULD be loaded once per session and cached on the session struct, not fetched per LLM call.

### Patterns

- **PAT-001**: The augmentation hook follows a "decorator" pattern around the existing `BuildContext()` step — given `(systemPrompt, sessionCtx)`, return `(preamble + "\n\n" + systemPrompt, messages)`.
- **PAT-002**: Variant preambles live in a single Go map keyed by BCP-47 code, in `cpn/prompts/regional.go` (new file). One source of truth, easy to extend.

## 4. Interfaces & Data Contracts

### 4.1 Database

```sql
-- migration 017_regional_variant.up.sql
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS regional_variant TEXT;

-- migration 017_regional_variant.down.sql
ALTER TABLE users DROP COLUMN IF EXISTS regional_variant;
```

### 4.2 Backend DTO additions

```go
// internal/driving/httpapi/handler_user.go
type CompleteOnboardingRequest struct {
    PreferredLanguage string            `json:"preferred_language"`
    RegionalVariant   string            `json:"regional_variant"` // NEW; BCP-47, optional
    PreferredModel    string            `json:"preferred_model"`
    ModelOverrides    map[string]string `json:"model_overrides"`
    PersonalityPreset string            `json:"personality_preset"`
}

// cpn/persist/types.go
type UserRecord struct {
    // ... existing fields ...
    RegionalVariant string // "" = unset; one of REQ-002 set
}
```

### 4.3 Variant registry

```go
// cpn/prompts/regional.go
var SupportedVariants = map[string]VariantInfo{
    "es-CO": {Lang: "es", Label: "Español (Colombia)", Preamble: preambleEsCO},
    "es-MX": {Lang: "es", Label: "Español (México)",   Preamble: preambleEsMX},
    "es-AR": {Lang: "es", Label: "Español (Argentina)",Preamble: preambleEsAR},
    "es-ES": {Lang: "es", Label: "Español (España)",   Preamble: preambleEsES},
    "en-GB": {Lang: "en", Label: "English (UK)",       Preamble: preambleEnGB},
    "en-US": {Lang: "en", Label: "English (US)",       Preamble: preambleEnUS},
    "en-AU": {Lang: "en", Label: "English (Australia)",Preamble: preambleEnAU},
}

func DefaultVariant(lang string) string // "es" → "es-CO", "en" → "en-GB", else "es-CO"
func PreambleFor(variant string) string  // returns rendered preamble or default
```

### 4.4 Frontend types

```typescript
// src/types/setup.ts
export type RegionalVariant =
  | 'es-CO' | 'es-MX' | 'es-AR' | 'es-ES'
  | 'en-GB' | 'en-US' | 'en-AU';

export interface OnboardingCompleteRequest {
  preferred_language: string;
  regional_variant: RegionalVariant; // NEW
  preferred_model: string;
  model_overrides: Record<string, string>;
  personality_preset: PersonalityPreset;
}
```

### 4.5 Prompt preamble (canonical format)

```
USER CONTEXT — REGIONAL REGISTER
The user writes in {label}. In this register, treat the following as
conversational unless the broader sentence clearly demands otherwise:
- {idiom_1}: {gloss_1}
- {idiom_2}: {gloss_2}
- {idiom_3}: {gloss_3}
Use this only to interpret intent and tone, not to imitate the dialect.
```

Example (es-CO):
```
USER CONTEXT — REGIONAL REGISTER
The user writes in Español (Colombia). In this register, treat the
following as conversational unless the broader sentence clearly
demands otherwise:
- "cuenta / cuéntame": often "tell me the story / spill it", not "count".
- "regálame X": polite "please pass me X", not literal gifting.
- "ahorita": vague near-future, not "right now".
Use this only to interpret intent and tone, not to imitate the dialect.
```

## 5. Acceptance Criteria

- **AC-001**: Given a fresh DB, when migration `017_regional_variant.up.sql` runs, then the `users` table has a nullable `regional_variant TEXT` column.
- **AC-002**: Given the user picks `es` in onboarding, when the variant step renders, then `es-CO` is preselected and the user sees a labelled list of all four `es-*` variants.
- **AC-003**: Given the user picks `en` in onboarding, when the variant step renders, then `en-GB` is preselected and the user sees all three `en-*` variants.
- **AC-004**: Given a valid `regional_variant` in the onboarding request, when `POST /api/v1/user/onboarding/complete` is called, then the value is persisted and the response is 200 OK.
- **AC-005**: Given an invalid `regional_variant` (e.g., `pt-BR`, `xx-YY`, `'); DROP TABLE users;--`), when the handler is called, then it returns 400 with an explanatory error and no database mutation occurs.
- **AC-006**: Given a Colombian user (`regional_variant=es-CO`), when they send `"cuenta del uno al veinte en palabras únicamente"`, then the classifier returns `intent="conversation"` and `t-direct` streams the answer; no `t-review` HITL gate fires.
- **AC-007**: Given the same exact message from a user with `regional_variant=es-ES`, when classified, then it MAY be classified as `task` or `conversation` depending on the LLM, but `t-review` MUST NOT fire if confidence is high enough — i.e., the variant influences the classifier, and the test asserts the preamble was injected, not a specific outcome.
- **AC-008**: Given any LLM transition invocation, when the engine builds the context, then the rendered system prompt starts with the preamble for the session's variant followed by `\n\n` followed by the original transition `SystemPrompt`.
- **AC-009**: Given a user with `regional_variant=NULL` and `preferred_language=es`, when an LLM call is made for their session, then the engine injects the `es-CO` preamble.
- **AC-010**: Given two concurrent sessions for two users with different variants, when both invoke the same transition simultaneously, then each sees its own preamble — `Transition.SystemPrompt` MUST NOT be mutated.

## 6. Test Automation Strategy

- **Test Levels**: Unit (Go: variant registry, default-resolver, preamble renderer; TS: SetupWizard step + reducer); Integration (Go: handler validation table-test; classifier transition with mock LLM verifying injected preamble); E2E (Playwright: full onboarding → first message → assert no review card).
- **Frameworks**: Go `testing` + `testify`; Vitest + React Testing Library; Playwright.
- **Test Data**: Table-driven tests over the seven variants; one negative case per invalid input class (unsupported tag, empty string, oversized string, injection payload).
- **CI/CD Integration**: Existing GitHub Actions pipelines run unit + integration; E2E runs against the docker-compose stack on PRs touching this feature.
- **Coverage**: ≥ 90% on `cpn/prompts/regional.go` and the augmentation hook; existing thresholds elsewhere.
- **Performance**: Preamble rendering MUST add < 1 ms per LLM call (microbenchmark in Go).

## 7. Rationale & Context

The classifier failure that motivated this spec is structural: a single Spanish prompt cannot resolve idioms that depend on the speaker's region. Three alternatives were considered:

1. **Heuristic in the prompt itself** — list every regional idiom for every variant in the global classifier prompt. **Rejected**: prompt size explodes, dilutes other instructions, and the LLM has no signal about which list applies to the current user.
2. **Per-variant prompt files loaded by env var at startup** — multiplies the number of topologies and forbids mixed-language deployments. **Rejected**: classifier prompts are already env-overridable, but the dispatch needs to be per-session, not per-process.
3. **Per-session preamble injection** (chosen). Cleanly separates global instructions from user context; cap on token cost; one source of truth for variant data; backwards compatible (NULL → language default).

The decoupling of `preferred_language` (UI) from `regional_variant` (LLM context) is intentional: UI translation files are heavy and we have no business reason to maintain `es-CO` strings, but the LLM gets a meaningful disambiguation signal at near-zero cost.

GUD-001's "describe, don't instruct" framing is critical: telling the LLM `"NEVER classify cuenta as count for Colombian users"` brittlely overrides Step 1; describing the register lets the model do its job.

## 8. Dependencies & External Integrations

### External Systems
- **EXT-001**: OpenRouter — receives the augmented system prompt via the existing httpclient. No API change.

### Infrastructure Dependencies
- **INF-001**: Postgres (existing `liwaisi-postgres` container) — schema migration only.

### Data Dependencies
- **DAT-001**: None. Variant registry is static, in code.

### Technology Platform Dependencies
- **PLT-001**: Go ≥ 1.22 (existing); React 18 + i18next (existing).

### Compliance Dependencies
- **COM-001**: None. Variant is a non-PII preference.

## 9. Examples & Edge Cases

```jsonc
// Valid onboarding payload
{
  "preferred_language": "es",
  "regional_variant": "es-CO",
  "preferred_model": "anthropic/claude-sonnet-4-6",
  "model_overrides": {},
  "personality_preset": "balanced"
}
```

```jsonc
// Invalid — rejected with 400
{ "preferred_language": "es", "regional_variant": "pt-BR", ... }
{ "preferred_language": "en", "regional_variant": "",      ... } // empty: handler treats as default
{ "preferred_language": "es", "regional_variant": "es-co", ... } // case-sensitive: rejected
```

```go
// Edge case: legacy user with no variant
user.RegionalVariant = ""        // NULL in DB
user.PreferredLanguage = "es"
prompts.PreambleFor(prompts.DefaultVariant(user.PreferredLanguage))
// → es-CO preamble
```

```go
// Edge case: legacy user with neither field
user.RegionalVariant = ""
user.PreferredLanguage = ""
prompts.PreambleFor(prompts.DefaultVariant(""))
// → es-CO preamble (global default)
```

## 10. Validation Criteria

1. All seven variants in REQ-002 round-trip cleanly through the onboarding handler and repository.
2. The Colombian counting message (AC-006) reproducibly classifies as `conversation` against the live OpenRouter classifier model.
3. Concurrent-session test (AC-010) passes under `-race`.
4. No mutation of any package-level `Transition.SystemPrompt` after server start (verified by an init-time snapshot + post-test diff).
5. Preamble token budget ≤ 80 tokens for every variant (verified by tokenizer test).
6. Frontend Lighthouse perf score on `/setup` does not regress more than 2 points.

## 11. Related Specifications / Further Reading

- [spec-architecture-setup-mode-onboarding-wizard.md](./spec-architecture-setup-mode-onboarding-wizard.md)
- [spec-architecture-block20-llm-streaming.md](./spec-architecture-block20-llm-streaming.md)
- BCP-47 — https://www.rfc-editor.org/info/bcp47
