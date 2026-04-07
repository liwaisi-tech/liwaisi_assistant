---
title: "User Onboarding Wizard & Platform Secret Management (SOPS + age)"
version: 2.0
date_created: 2026-04-05
last_updated: 2026-04-05
owner: liwaisi
tags: [architecture, design, onboarding, ux, security, encryption, full-stack, devops, sops, age]
---

# Introduction

This specification defines two complementary systems for the Liwaisi Assistant platform:

1. **User Onboarding Wizard** — A post-authentication, multi-step frontend experience that guides new users through initial setup (language preference, AI model selection, personality configuration, and product tour). This runs after Google sign-in and ensures users reach their first AI chat as fast as possible.

2. **Platform Secret Management (SOPS + age)** — An operator-level workflow for the liwaisi team to manage platform secrets (`OPENROUTER_API_KEY`, `GOOGLE_CLIENT_ID`, database credentials) using encrypted files committed to git. This replaces manual `.env` file creation for deployment.

**Key distinction**: Platform secrets (API keys, OAuth credentials) are owned and managed by the liwaisi team — users never provide their own keys. The onboarding wizard is about **user preferences**, not infrastructure configuration.

The goal is to reduce time-to-first-chat to **under 90 seconds** from sign-in to first AI response.

## 1. Purpose & Scope

### Purpose

1. Provide a guided first-run experience for new users that captures preferences and introduces the product
2. Reduce time-to-first-value: sign in → configure → chat in under 90 seconds
3. Persist user preferences (language, model, personality) for a personalized experience from day one
4. Establish a secure, reproducible workflow for the liwaisi team to manage platform secrets via SOPS + age
5. Eliminate manual `.env` file creation from the deployment process

### Scope

- **In scope**: Post-auth onboarding wizard UI, user preferences API, onboarding state tracking, SOPS + age setup scripts, Makefile targets, Docker Compose refinements, i18n for wizard strings, migration for user preferences
- **Out of scope**: Self-hosted operator setup wizard (future), per-user API key provisioning (future), admin panel for managing users, A/B testing of onboarding flows, analytics/telemetry

### Intended Audience

- Frontend engineers building the wizard UI
- Golang engineers implementing preferences API and DB migration
- Solutions architects reviewing the security model
- UX/UI designers reviewing the wizard flow
- AI Product Managers defining the onboarding journey
- AI code generation agents building from this specification

### Assumptions

- liwaisi owns the Google OAuth Client ID and OpenRouter API key (platform secrets)
- Platform secrets are deployed via environment variables managed by the liwaisi team
- Users authenticate via Google OAuth (existing flow, spec-architecture-google-oauth-login.md)
- PostgreSQL persistence is enabled for all production deployments (`LIWAISI_DB_DSN` is set)
- The existing i18n system (`react-i18next`, namespace-based) is operational
- React 19, Tailwind CSS v4, Vite 6, TypeScript 5.7 remain the frontend stack
- The dark glassmorphism design system is preserved
- English and Spanish translations are provided at launch

## 2. Definitions

| Term | Definition |
|------|-----------|
| **Onboarding Wizard** | A multi-step, post-authentication UI flow presented to new users on their first sign-in. Collects preferences and introduces the product. |
| **Platform Secrets** | Credentials owned by the liwaisi team: `OPENROUTER_API_KEY`, `GOOGLE_CLIENT_ID`, `LIWAISI_DB_DSN`, `LIWAISI_REDIS_URL`, `POSTGRES_PASSWORD`. Never user-facing. |
| **User Preferences** | Per-user settings stored in the database: preferred language, default AI model, personality configuration, onboarding completion status. |
| **SOPS** | Secrets OPerationS — a CLI tool for encrypting/decrypting YAML/JSON files while keeping keys in plaintext for readable diffs. Used by the liwaisi team for git-committed secrets. |
| **age** | A modern file encryption tool using X25519 + ChaCha20-Poly1305. Used as the SOPS encryption backend. |
| **CPN** | Coloured Petri Net — the backend's AI agent execution engine. |
| **SSE** | Server-Sent Events — real-time streaming protocol for chat and execution events. |
| **HITL** | Human-In-The-Loop — user approval gates in CPN execution flows. |
| **Personality** | A set of principles (nucleo, conducta, etica), hierarchy, and tension rules that shape the AI agent's behavior for a specific user. |

## 3. Requirements, Constraints & Guidelines

### Onboarding Detection

- **REQ-001**: The backend MUST track whether a user has completed onboarding via an `onboarding_completed_at` column in the `users` table.
- **REQ-002**: The `GET /api/v1/user/profile` endpoint MUST return the user's profile including onboarding status.
- **REQ-003**: The frontend MUST check the user's onboarding status after successful authentication and render the wizard if `onboarding_completed_at` is null.
- **REQ-004**: The onboarding wizard MUST be skippable — users can dismiss it and go directly to the main app. Skipping still marks onboarding as completed.

### User Preferences API

- **REQ-005**: `GET /api/v1/user/profile` MUST return the authenticated user's profile, preferences, and onboarding status.
- **REQ-006**: `PUT /api/v1/user/preferences` MUST accept and persist user preferences (language, default model, optional model overrides).
- **REQ-007**: `POST /api/v1/user/onboarding/complete` MUST mark the user's onboarding as completed and persist any final preferences.
- **REQ-008**: All user preference endpoints MUST require authentication (existing auth middleware).
- **REQ-009**: `GET /api/v1/models` MUST return the available model registry (role → model mapping) for populating the model selection UI. This endpoint requires authentication.

### Wizard Steps

- **REQ-010**: **Step 1 — Welcome**: Animated welcome screen with Liwaisi branding, brief product description, estimated time ("~1 minute"), and "Get Started" button. No inputs.
- **REQ-011**: **Step 2 — Language**: Language selector (English/Spanish) with live preview of a sample greeting in the selected language. Selection persists to `liwaisi_lang` in localStorage and user preferences in DB.
- **REQ-012**: **Step 3 — AI Model Preference** (optional): Default model selector populated from `GET /api/v1/models`. Show model descriptions (speed vs capability). Advanced toggle for per-role overrides (classifier, reasoning, etc.). Pre-populated with platform defaults.
- **REQ-013**: **Step 4 — Personality Quick Setup** (optional): Simplified personality configuration — choose from 2-3 preset personality profiles (e.g., "Balanced", "Creative", "Precise") or "Customize later". Full personality editor is already available in the main app.
- **REQ-014**: **Step 5 — Ready**: Success animation, summary of choices, "Start Chatting" button that opens a new session. Optional: 3-panel product tour highlighting key UI areas (chat, monitor, personality).

### Wizard UX

- **UX-001**: The wizard MUST use the existing dark glassmorphism design system (CSS custom properties from `index.css`).
- **UX-002**: Step transitions MUST be animated (slide or fade, consistent with existing modal patterns in `SignInModal`).
- **UX-003**: A horizontal progress indicator MUST show completed, current, and upcoming steps with distinct visual states.
- **UX-004**: The wizard MUST include a "Skip Setup" link on every step (except the final one) that completes onboarding with defaults.
- **UX-005**: Form inputs MUST provide inline validation and feedback (follow `WaitlistForm` pattern).
- **UX-006**: The wizard MUST be responsive (min-width: 320px) and usable on mobile.
- **UX-007**: All interactive elements MUST be keyboard-navigable with appropriate ARIA attributes.
- **UX-008**: The wizard MUST include the Liwaisi logo at the top for brand recognition.
- **UX-009**: Step navigation MUST support back/forward movement, preserving previously entered values.
- **UX-010**: The wizard card MUST be centered on a full-bleed background with subtle CPN-inspired animation (reuse landing page patterns).

### i18n

- **REQ-015**: All wizard text MUST be translatable via the i18n system using a `setup` namespace.
- **REQ-016**: Language selection in Step 2 MUST immediately apply the chosen language to the wizard itself (live switch).
- **REQ-017**: Translation files (`en/setup.json`, `es/setup.json`) MUST be provided at launch.

### SOPS + age Platform Secret Management

- **REQ-018**: A `scripts/sops-setup.sh` script MUST be provided to initialize age key generation and SOPS configuration for the liwaisi development team.
- **REQ-019**: A `.sops.yaml` configuration file MUST be created at the repository root, defining encryption rules for `.env.sops.yaml`.
- **REQ-020**: A `.env.sops.yaml` file MUST be provided as the encrypted secrets file (committed to git).
- **REQ-021**: The `Makefile` (root level) MUST include targets: `sops-setup`, `sops-encrypt`, `sops-decrypt`, `sops-edit`.
- **REQ-022**: `make sops-decrypt` MUST produce a valid `.env` file that docker-compose can consume.
- **REQ-023**: The `.gitignore` MUST be updated to ignore age private keys (`*.age.key`, `keys.txt`, `~/.config/sops/`) while allowing `.sops.yaml` and `.env.sops.yaml`.

### Docker Compose Refinements

- **REQ-024**: Infrastructure secrets (`LIWAISI_DB_DSN`, `LIWAISI_REDIS_URL`) MUST be moved from `.env` into `docker-compose.yml` `environment:` block with defaults referencing Docker service names.
- **REQ-025**: The `env_file: .env` directive MUST remain for platform secrets (`OPENROUTER_API_KEY`, `GOOGLE_CLIENT_ID`) that come from the SOPS-decrypted file.
- **REQ-026**: A new `make deploy` target MUST chain `sops-decrypt` → `docker-compose up -d` for one-command deployment.

### Security

- **SEC-001**: User preferences MUST NOT contain any secrets or credentials — only non-sensitive settings (language, model name, personality preset).
- **SEC-002**: The SOPS-encrypted file (`.env.sops.yaml`) MUST be the only file containing secrets committed to git.
- **SEC-003**: Decrypted `.env` files MUST be in `.gitignore` and MUST NOT be committed.
- **SEC-004**: Age private keys MUST never be committed to git. The `scripts/sops-setup.sh` MUST warn the user about key backup.
- **SEC-005**: The `POST /api/v1/user/onboarding/complete` endpoint MUST validate that the authenticated user can only complete their own onboarding (enforced by auth context).

### Backward Compatibility

- **REQ-027**: Existing users (who signed in before this feature) MUST have `onboarding_completed_at` backfilled to their `created_at` timestamp via the migration. They should NOT see the wizard.
- **REQ-028**: The new DB migration MUST be additive (new columns, no destructive changes to existing data).
- **REQ-029**: Existing `.env` file workflows MUST continue to work. SOPS + age is an enhancement, not a replacement.

### Constraints

- **CON-001**: The frontend MUST NOT add new npm dependencies for the wizard. Use existing React 19, Tailwind CSS v4, and i18n system.
- **CON-002**: The wizard MUST work without persistence (in-memory mode / dev-mode). When persistence is disabled, preferences are stored only in localStorage.
- **CON-003**: No modifications to existing database migrations. New migration file only.
- **CON-004**: SOPS and age are CLI tools installed on developer workstations — they are NOT runtime dependencies and MUST NOT be included in Docker images.
- **CON-005**: The personality presets in Step 4 MUST reuse the existing `cpn.Personality` structure and `PersonalityRepository` interface.

### Guidelines

- **GUD-001**: Follow the existing hexagonal architecture: user preferences as a driven adapter port, wizard handlers as driving adapter endpoints.
- **GUD-002**: Reuse existing patterns: `WaitlistForm` state machine for form steps, `SignInModal` glassmorphic styling, `useAuth` context for user identity.
- **GUD-003**: Keep the wizard minimal — 5 steps, most optional. The goal is speed, not comprehensiveness.
- **GUD-004**: Personality presets should map to the existing 3-principle system (nucleo, conducta, etica) with predefined rule sets.
- **PAT-001**: Follow the same handler pattern as `handler_personality.go` for the new user profile/preferences endpoints.
- **PAT-002**: Follow the same migration pattern as `009_create_personalities.up.sql` for the new user preferences migration.

## 4. Interfaces & Data Contracts

### 4.1 Database Migration (013_user_preferences)

```sql
-- 013_user_preferences.up.sql

-- Add onboarding tracking and preferences to users table.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS onboarding_completed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS preferred_language       TEXT NOT NULL DEFAULT 'en',
    ADD COLUMN IF NOT EXISTS preferred_model          TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS model_overrides          JSONB NOT NULL DEFAULT '{}';

-- Backfill: existing users are considered onboarded.
UPDATE users SET onboarding_completed_at = created_at WHERE onboarding_completed_at IS NULL;
```

```sql
-- 013_user_preferences.down.sql
ALTER TABLE users
    DROP COLUMN IF EXISTS onboarding_completed_at,
    DROP COLUMN IF EXISTS preferred_language,
    DROP COLUMN IF EXISTS preferred_model,
    DROP COLUMN IF EXISTS model_overrides;
```

### 4.2 Backend: Updated UserRecord

```go
// cpn/persist/types.go — updated UserRecord

type UserRecord struct {
    ID                   string
    Email                string
    Name                 string
    Picture              string
    PreferredLanguage    string            // "en", "es"
    PreferredModel       string            // e.g., "anthropic/claude-sonnet-4-6"
    ModelOverrides       map[string]string // role → model (optional)
    OnboardingCompletedAt *time.Time       // nil = not completed
    CreatedAt            time.Time
    UpdatedAt            time.Time
}
```

### 4.3 Backend: Updated UserRepository Interface

```go
// cpn/persist/interfaces.go — additions to UserRepository

type UserRepository interface {
    // Existing methods
    Upsert(ctx context.Context, user *UserRecord) error
    GetByID(ctx context.Context, id string) (*UserRecord, error)
    GetByEmail(ctx context.Context, email string) (*UserRecord, error)

    // New methods
    UpdatePreferences(ctx context.Context, userID string, prefs *UserPreferences) error
    CompleteOnboarding(ctx context.Context, userID string) error
}

type UserPreferences struct {
    PreferredLanguage string            `json:"preferred_language"`
    PreferredModel    string            `json:"preferred_model"`
    ModelOverrides    map[string]string `json:"model_overrides,omitempty"`
}
```

### 4.4 API Endpoints

#### `GET /api/v1/user/profile`

**Response:**
```json
{
  "id": "google-sub-123",
  "email": "user@example.com",
  "name": "Juan Garcia",
  "picture": "https://lh3.googleusercontent.com/...",
  "preferences": {
    "preferred_language": "es",
    "preferred_model": "anthropic/claude-sonnet-4-6",
    "model_overrides": {}
  },
  "onboarding_completed": false,
  "created_at": "2026-04-05T10:30:00Z"
}
```

#### `PUT /api/v1/user/preferences`

**Request:**
```json
{
  "preferred_language": "es",
  "preferred_model": "anthropic/claude-sonnet-4-6",
  "model_overrides": {
    "classifier": "google/gemini-2.0-flash-001",
    "reasoning": "anthropic/claude-opus-4-6"
  }
}
```

**Response:**
```json
{
  "ok": true
}
```

#### `POST /api/v1/user/onboarding/complete`

**Request:**
```json
{
  "preferred_language": "es",
  "preferred_model": "anthropic/claude-sonnet-4-6",
  "model_overrides": {},
  "personality_preset": "balanced"
}
```

`personality_preset` is one of: `"balanced"`, `"creative"`, `"precise"`, `"custom"` (skip personality setup), or `""` (no preference).

**Response:**
```json
{
  "ok": true,
  "message": "Welcome to Liwaisi!"
}
```

**Backend behavior:**
1. Persist user preferences (`preferred_language`, `preferred_model`, `model_overrides`)
2. If `personality_preset` is not empty or `"custom"`: save the corresponding personality profile to `PersonalityRepository`
3. Set `onboarding_completed_at = now()`

#### `GET /api/v1/models`

**Response:**
```json
{
  "default_model": "anthropic/claude-sonnet-4-6",
  "roles": [
    {
      "key": "classifier",
      "label": "Intent Classifier",
      "description": "Fast model for routing user messages",
      "default_model": "google/gemini-2.0-flash-001"
    },
    {
      "key": "structured",
      "label": "Structured Output",
      "description": "Model for JSON/structured responses",
      "default_model": "anthropic/claude-haiku-4-5-20251001"
    },
    {
      "key": "reasoning",
      "label": "Reasoning",
      "description": "Primary model for complex tasks",
      "default_model": "anthropic/claude-sonnet-4-6"
    },
    {
      "key": "long-context",
      "label": "Long Context",
      "description": "Model for large document processing",
      "default_model": "google/gemini-2.0-pro-001"
    },
    {
      "key": "summarize",
      "label": "Summarization",
      "description": "Lightweight model for summaries",
      "default_model": "meta-llama/llama-3.3-8b-instruct"
    },
    {
      "key": "thinking",
      "label": "Deep Thinking",
      "description": "Most capable model for complex reasoning",
      "default_model": "anthropic/claude-opus-4-6"
    }
  ]
}
```

### 4.5 Frontend: TypeScript Types

```typescript
// src/types/setup.ts

export interface UserProfile {
  id: string;
  email: string;
  name: string;
  picture: string;
  preferences: UserPreferences;
  onboarding_completed: boolean;
  created_at: string;
}

export interface UserPreferences {
  preferred_language: string;
  preferred_model: string;
  model_overrides: Record<string, string>;
}

export interface ModelRole {
  key: string;
  label: string;
  description: string;
  default_model: string;
}

export interface ModelsResponse {
  default_model: string;
  roles: ModelRole[];
}

export type PersonalityPreset = 'balanced' | 'creative' | 'precise' | 'custom' | '';

export interface OnboardingCompleteRequest {
  preferred_language: string;
  preferred_model: string;
  model_overrides: Record<string, string>;
  personality_preset: PersonalityPreset;
}
```

### 4.6 Frontend: API Functions

```typescript
// Added to src/services/api.ts

export async function getUserProfile(): Promise<UserProfile> {
  return request<UserProfile>('/user/profile');
}

export async function updatePreferences(prefs: UserPreferences): Promise<{ ok: boolean }> {
  return request<{ ok: boolean }>('/user/preferences', {
    method: 'PUT',
    body: JSON.stringify(prefs),
  });
}

export async function completeOnboarding(data: OnboardingCompleteRequest): Promise<{ ok: boolean; message: string }> {
  return request<{ ok: boolean; message: string }>('/user/onboarding/complete', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function getModels(): Promise<ModelsResponse> {
  return request<ModelsResponse>('/models');
}
```

### 4.7 Frontend: App.tsx Routing Change

```typescript
// src/App.tsx — updated routing logic

function AppContent() {
  const { user, isAuthenticated } = useAuth();
  const [needsOnboarding, setNeedsOnboarding] = useState<boolean | null>(null);

  useEffect(() => {
    if (isAuthenticated && user) {
      getUserProfile().then(profile => {
        setNeedsOnboarding(!profile.onboarding_completed);
      }).catch(() => setNeedsOnboarding(false)); // fail-open: skip wizard
    }
  }, [isAuthenticated, user]);

  if (hasGoogleAuth && (!isAuthenticated || !user)) {
    return <LandingPage />;
  }

  if (needsOnboarding === true) {
    return <SetupWizard onComplete={() => setNeedsOnboarding(false)} />;
  }

  if (needsOnboarding === null) {
    return <LoadingScreen />; // brief loading while checking profile
  }

  const userId = user?.email ?? 'dev-user';
  return <DesktopLayout userId={userId} />;
}
```

### 4.8 Docker Compose Changes

```yaml
# docker-compose.yml — refined

services:
  backend:
    build:
      context: back/go-assistant
      args:
        VERSION: ${VERSION:-dev}
    container_name: liwaisi-backend
    networks: [liwaisi]
    ports:
      - "8080:8080"
    env_file: .env     # Platform secrets from SOPS-decrypted file
    environment:
      - LISTEN_ADDR=:8080
      - LIWAISI_DB_DSN=postgres://liwaisi:${POSTGRES_PASSWORD:-liwaisi_dev}@postgres:5432/liwaisi?sslmode=disable
      - LIWAISI_REDIS_URL=redis://redis:6379/0
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:8080/api/v1/health"]
      interval: 10s
      timeout: 3s
      start_period: 5s
      retries: 3
    restart: unless-stopped
```

**Key changes:**
- `LIWAISI_DB_DSN` and `LIWAISI_REDIS_URL` moved to `environment:` with Docker service name defaults
- `.env` file now only needs: `OPENROUTER_API_KEY`, `GOOGLE_CLIENT_ID`, `VITE_GOOGLE_CLIENT_ID`
- Infrastructure secrets are no longer user-facing

### 4.9 SOPS + age Configuration

```yaml
# .sops.yaml (repository root)
creation_rules:
  - path_regex: \.env\.sops\.yaml$
    age: >-
      age1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
    # Add team member public keys, one per line
```

```yaml
# .env.sops.yaml (committed to git, values encrypted by SOPS)
# Keys are plaintext, values are encrypted.
OPENROUTER_API_KEY: ENC[AES256_GCM,data:...,type:str]
GOOGLE_CLIENT_ID: ENC[AES256_GCM,data:...,type:str]
VITE_GOOGLE_CLIENT_ID: ENC[AES256_GCM,data:...,type:str]
POSTGRES_PASSWORD: ENC[AES256_GCM,data:...,type:str]
sops:
    # SOPS metadata (auto-generated)
```

### 4.10 Personality Presets

```go
// Defined in cpn/personality_presets.go or similar

var PersonalityPresets = map[string]Personality{
    "balanced": {
        Principles: [3]Principle{
            {Kind: "nucleo", Title: "Clarity & Precision", Rules: [...]},
            {Kind: "conducta", Title: "Collaborative & Adaptive", Rules: [...]},
            {Kind: "etica", Title: "Honest & Transparent", Rules: [...]},
        },
        Hierarchy: [3]PrincipleKind{"nucleo", "conducta", "etica"},
    },
    "creative": {
        Principles: [3]Principle{
            {Kind: "nucleo", Title: "Creative Exploration", Rules: [...]},
            {Kind: "conducta", Title: "Bold & Experimental", Rules: [...]},
            {Kind: "etica", Title: "Originality & Attribution", Rules: [...]},
        },
        Hierarchy: [3]PrincipleKind{"conducta", "nucleo", "etica"},
    },
    "precise": {
        Principles: [3]Principle{
            {Kind: "nucleo", Title: "Technical Rigor", Rules: [...]},
            {Kind: "conducta", Title: "Methodical & Thorough", Rules: [...]},
            {Kind: "etica", Title: "Accuracy & Accountability", Rules: [...]},
        },
        Hierarchy: [3]PrincipleKind{"nucleo", "etica", "conducta"},
    },
}
```

## 5. Acceptance Criteria

### Onboarding Flow

- **AC-001**: Given a new user who just signed in via Google OAuth, When the frontend loads their profile, Then the onboarding wizard is displayed (because `onboarding_completed_at` is null).
- **AC-002**: Given a user on Step 2 (Language), When they select Spanish, Then the wizard immediately re-renders in Spanish.
- **AC-003**: Given a user on Step 3 (Model), When they expand "Advanced" and select a model override for "reasoning", Then the override is included in the final configuration.
- **AC-004**: Given a user on Step 4 (Personality), When they select "Creative" preset, Then the corresponding personality is saved to the `personalities` table.
- **AC-005**: Given a user clicks "Skip Setup" on Step 2, When onboarding completes, Then `onboarding_completed_at` is set and default preferences are saved. The user is redirected to DesktopLayout.
- **AC-006**: Given a user completes the full wizard, When they refresh the page, Then the wizard is NOT shown again (onboarding status persisted in DB).
- **AC-007**: Given an existing user who was created before this feature, When the migration runs, Then their `onboarding_completed_at` is backfilled to `created_at` and they never see the wizard.

### API

- **AC-008**: Given an authenticated user, When `GET /api/v1/user/profile` is called, Then the response includes preferences and `onboarding_completed` boolean.
- **AC-009**: Given an authenticated user, When `PUT /api/v1/user/preferences` is called with valid data, Then preferences are persisted and subsequent GET returns updated values.
- **AC-010**: Given an authenticated user with `onboarding_completed_at = null`, When `POST /api/v1/user/onboarding/complete` is called, Then `onboarding_completed_at` is set to the current timestamp.
- **AC-011**: Given an unauthenticated request, When any `/api/v1/user/*` endpoint is called, Then it returns 401 Unauthorized.

### SOPS + age

- **AC-012**: Given a developer runs `make sops-setup`, Then an age keypair is generated and instructions are printed.
- **AC-013**: Given a configured `.sops.yaml` and `.env.sops.yaml`, When `make sops-decrypt` is run, Then a valid `.env` file is produced containing all platform secrets in plaintext.
- **AC-014**: Given a decrypted `.env` file, When `docker-compose up` is run, Then the backend starts in normal mode with all secrets loaded.
- **AC-015**: Given the `.env.sops.yaml` file is committed to git, When inspected, Then secret values are encrypted and not readable.

### Dev Mode

- **AC-016**: Given no `GOOGLE_CLIENT_ID` env var (dev mode), When the frontend loads, Then the wizard is skipped (dev-user has no profile in DB, fails gracefully).
- **AC-017**: Given persistence is disabled (no `LIWAISI_DB_DSN`), When a user would see the wizard, Then preferences fall back to localStorage only and the wizard works correctly.

## 6. Test Automation Strategy

### Test Levels

| Level | Scope | Framework | Location |
|-------|-------|-----------|----------|
| Unit (Go) | UserRepository methods, preferences handler, personality presets | `testing` + `testify` | `store/postgres/user_test.go`, `internal/driving/httpapi/handler_user_test.go` |
| Integration (Go) | Full onboarding lifecycle with DB | `testing` + test DB | `integration/onboarding_test.go` |
| Unit (Frontend) | SetupWizard component, step components, state management | `vitest` + `@testing-library/react` | `src/features/setup/__tests__/` |
| E2E | Full wizard flow from sign-in to first chat | Manual / Playwright (future) | — |

### Unit Tests (Backend)

```go
// store/postgres/user_test.go — new tests
func TestUserRepository_UpdatePreferences(t *testing.T)
func TestUserRepository_CompleteOnboarding(t *testing.T)
func TestUserRepository_CompleteOnboarding_Idempotent(t *testing.T)
func TestUserRepository_GetByID_IncludesPreferences(t *testing.T)

// internal/driving/httpapi/handler_user_test.go
func TestGetUserProfile_Authenticated(t *testing.T)
func TestGetUserProfile_Unauthenticated(t *testing.T)
func TestUpdatePreferences_ValidData(t *testing.T)
func TestUpdatePreferences_InvalidModel(t *testing.T)
func TestCompleteOnboarding_WithPersonalityPreset(t *testing.T)
func TestCompleteOnboarding_SkipPersonality(t *testing.T)
func TestCompleteOnboarding_AlreadyCompleted(t *testing.T)
func TestGetModels_ReturnsRegistry(t *testing.T)
```

### Unit Tests (Frontend)

```typescript
// src/features/setup/__tests__/SetupWizard.test.tsx
describe('SetupWizard', () => {
  it('renders step 1 (welcome) by default')
  it('navigates forward and backward between steps')
  it('preserves form values when navigating back')
  it('calls language change handler on step 2 selection')
  it('loads models from API on step 3')
  it('shows personality presets on step 4')
  it('masks no secrets in review (none exist)')
  it('calls completeOnboarding API on submit')
  it('calls onComplete callback after successful submit')
  it('handles skip setup correctly')
  it('renders in Spanish when locale is es')
  it('shows loading state during API calls')
  it('handles API errors gracefully')
})
```

### Integration Tests

```go
// integration/onboarding_test.go
func TestOnboardingLifecycle(t *testing.T) {
    // 1. Create user via OAuth flow
    // 2. GET /user/profile → onboarding_completed: false
    // 3. GET /models → returns model registry
    // 4. POST /user/onboarding/complete with preferences + personality preset
    // 5. GET /user/profile → onboarding_completed: true, preferences populated
    // 6. GET /personality → preset personality saved
    // 7. Verify existing user migration backfill
}
```

### Coverage Requirements

- Backend: 85%+ line coverage for new handler and repository code
- Frontend: 80%+ for SetupWizard and step components

## 7. Rationale & Context

### Why a Post-Auth Wizard (Not Infrastructure Setup)?

liwaisi is a **platform product** — the team owns and manages all infrastructure secrets. Users authenticate via liwaisi's Google OAuth app and use liwaisi's OpenRouter API key. The onboarding friction is not "configure your server" but "set your preferences and understand the product." This is the same model as Notion, Linear, or any SaaS product.

### Why SOPS + age (Not a Vault)?

For a small team managing a handful of platform secrets:
- **SOPS + age** is zero-infrastructure (CLI tools only), free, and fits git-based workflows
- **HashiCorp Vault** would be massive overkill (complex operation for 4-5 secrets)
- **Cloud secret managers** create vendor lock-in
- age uses modern crypto (X25519 + ChaCha20-Poly1305) and is created by the former Go security lead (Filippo Valsorda)

### Why Personality Presets?

The existing personality editor is powerful but complex (3 principles, hierarchy, tensions). New users need a simple starting point. Presets provide:
- Instant personalization without cognitive overhead
- Demonstration of the personality system's value
- A bridge to the full editor (available in DesktopLayout)

### Why Backfill Existing Users?

Users created before this feature should not be forced through onboarding. The migration sets `onboarding_completed_at = created_at` for all existing users, ensuring they go straight to the main app.

### Why Infrastructure Secrets in docker-compose.yml?

`LIWAISI_DB_DSN` and `LIWAISI_REDIS_URL` reference Docker service hostnames (`postgres`, `redis`). These are:
- Deterministic (defined in docker-compose.yml)
- Internal to the Docker network
- The same for every deployment
Moving them to `environment:` with defaults eliminates them from the `.env` file, reducing the number of secrets the team must manage.

## 8. Dependencies & External Integrations

### External Systems

- **EXT-001**: OpenRouter Model Registry — The `GET /api/v1/models` endpoint exposes the `defaultModelRegistry` map from `infra/openrouter/openrouter.go:25-42`. No external API call needed; this is static data.

### Infrastructure Dependencies

- **INF-001**: PostgreSQL — New migration (013) adds columns to existing `users` table. Requires `LIWAISI_DB_DSN` to be set.
- **INF-002**: SOPS CLI (v3.9+) and age CLI (v1.2+) — Developer workstation tools. NOT runtime dependencies.

### Technology Platform Dependencies

- **PLT-001**: Go standard library — No new external dependencies for the backend.
- **PLT-002**: React 19 + Tailwind CSS v4 — No new frontend dependencies (CON-001).
- **PLT-003**: i18next + react-i18next — Existing i18n system for wizard translations.

## 9. Examples & Edge Cases

### Example: New User First Sign-In

```
1. User visits liwaisi.app → LandingPage shown
2. User clicks "Sign In with Google" → Google OAuth flow
3. Backend: token verified, user upserted (onboarding_completed_at = null)
4. Frontend: GET /api/v1/user/profile → {onboarding_completed: false}
5. Frontend: renders SetupWizard

   Step 1: Welcome → "Get Started"
   Step 2: Language → selects "Espanol" → wizard switches to Spanish
   Step 3: Model → keeps defaults → "Next"
   Step 4: Personality → selects "Creative" → "Next"
   Step 5: Ready → "Start Chatting"

6. Frontend: POST /api/v1/user/onboarding/complete
   {preferred_language: "es", preferred_model: "", model_overrides: {}, personality_preset: "creative"}
7. Backend: saves preferences, saves creative personality, sets onboarding_completed_at
8. Frontend: renders DesktopLayout
9. User starts first chat (< 90 seconds from sign-in)
```

### Example: User Skips Onboarding

```
1. User signs in → wizard appears
2. User clicks "Skip Setup" on Step 1
3. Frontend: POST /api/v1/user/onboarding/complete
   {preferred_language: "en", preferred_model: "", model_overrides: {}, personality_preset: ""}
4. Backend: saves defaults, sets onboarding_completed_at
5. Frontend: renders DesktopLayout with all defaults
```

### Example: Developer Local Setup with SOPS

```bash
# One-time setup
$ make sops-setup
> Generating age keypair...
> Public key: age1abc123...
> Private key saved to ~/.config/sops/age/keys.txt
> IMPORTANT: Back up your private key!
> Add your public key to .sops.yaml

# Daily workflow
$ make sops-decrypt    # Creates .env from encrypted .env.sops.yaml
$ docker-compose up    # Starts with decrypted secrets
```

### Edge Case: Dev Mode (No Persistence)

```
- GOOGLE_CLIENT_ID not set → dev mode, synthetic user
- LIWAISI_DB_DSN not set → no persistence
- Frontend: GET /api/v1/user/profile → fails (no user repo)
- Frontend: catches error → setNeedsOnboarding(false) → skip wizard
- User goes straight to DesktopLayout with defaults
- Preferences stored in localStorage only
```

### Edge Case: Returning User After Page Refresh

```
- User completed onboarding yesterday
- Refreshes page → token still valid in localStorage
- Frontend: GET /api/v1/user/profile → {onboarding_completed: true}
- Wizard NOT shown → DesktopLayout rendered immediately
```

### Edge Case: Migration for 100 Existing Users

```sql
-- Migration 013 runs:
ALTER TABLE users ADD COLUMN onboarding_completed_at TIMESTAMPTZ;
UPDATE users SET onboarding_completed_at = created_at WHERE onboarding_completed_at IS NULL;
-- Result: all 100 users have onboarding_completed_at set
-- None of them will see the wizard
```

## 10. Validation Criteria

1. A new user signing in for the first time sees the onboarding wizard
2. Completing the wizard persists preferences and personality, then shows DesktopLayout
3. Refreshing the page after completing onboarding does NOT show the wizard again
4. Existing users (pre-migration) never see the wizard
5. "Skip Setup" works on every step and completes onboarding with defaults
6. Language selection in Step 2 immediately switches the wizard language
7. Personality presets correctly map to the existing personality structure
8. Dev mode (no GOOGLE_CLIENT_ID) skips the wizard gracefully
9. `make sops-decrypt && docker-compose up` deploys with secrets from encrypted file
10. `.env.sops.yaml` committed to git contains only encrypted values
11. All wizard text is available in English and Spanish
12. The wizard is responsive and usable on mobile (320px min-width)

## 11. Related Specifications / Further Reading

- [spec-architecture-google-oauth-login.md](spec-architecture-google-oauth-login.md) — Authentication system (prerequisite for onboarding)
- [spec-design-i18n-internationalization.md](spec-design-i18n-internationalization.md) — i18n system used for wizard translations
- [spec-architecture-tools-engine-agent-personality.md](spec-architecture-tools-engine-agent-personality.md) — Personality system that presets map to
- [SOPS documentation](https://github.com/getsops/sops) — Official SOPS repository
- [age documentation](https://github.com/FiloSottile/age) — Official age encryption tool
- [OpenRouter Models](https://openrouter.ai/models) — Available models for the model selector
