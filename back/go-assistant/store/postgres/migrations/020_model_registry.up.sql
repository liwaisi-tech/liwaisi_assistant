-- Database-backed LLM model registry.
-- Spec: spec/spec-architecture-model-registry-and-a2ui-management.md
--
-- Introduces three tables:
--   models                — one row per registered model, with lifecycle + license fields.
--   registry_config       — singleton (id = 1) pointing at the product default.
--                           NOT NULL + FK RESTRICT guarantees "exactly one, never deletable"
--                           (REQ-REG-008).
--   model_role_defaults   — role → registry_id mapping replacing the Go-constant map.
--
-- Seed data mirrors back/go-assistant/infra/openrouter/openrouter.go AvailableModels,
-- with hand-curated licenses per REQ-SEED-006.

-- ── ENUMs ────────────────────────────────────────────────────────────────────
DO $$ BEGIN
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
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    CREATE TYPE model_license_status AS ENUM (
        'unreviewed',
        'review-in-progress',
        'approved-commercial',
        'approved-non-commercial',
        'restricted',
        'blocked',
        'unknown'
    );
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- ── models ───────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS models (
    id                        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    registry_id               TEXT NOT NULL UNIQUE,

    -- Identity
    vendor                    TEXT NOT NULL,
    family                    TEXT NOT NULL,
    version                   TEXT NOT NULL,
    variant                   TEXT,
    display_name              TEXT NOT NULL,
    description               TEXT NOT NULL DEFAULT '',
    hugging_face_id           TEXT,

    -- Capabilities / context
    modalities                JSONB NOT NULL DEFAULT '{"input":["text"],"output":["text"]}'::jsonb,
    capabilities              JSONB NOT NULL DEFAULT '{}'::jsonb,
    context_length            INTEGER NOT NULL DEFAULT 0,
    tokenizer                 TEXT NOT NULL DEFAULT '',
    supported_parameters      JSONB NOT NULL DEFAULT '[]'::jsonb,
    default_parameters        JSONB NOT NULL DEFAULT '{}'::jsonb,

    -- Pricing (per-token in USD)
    pricing_input_per_token   NUMERIC(20, 12) NOT NULL DEFAULT 0,
    pricing_output_per_token  NUMERIC(20, 12) NOT NULL DEFAULT 0,
    pricing_currency          TEXT NOT NULL DEFAULT 'USD',

    -- License (flattened; §4.1 JSON Schema rehydrates via JOIN)
    license_kind              TEXT NOT NULL DEFAULT 'unknown',
    license_spdx_id           TEXT,
    license_community_slug    TEXT,
    license_name              TEXT,
    license_url               TEXT,
    license_source            TEXT NOT NULL DEFAULT 'manual',
    license_status            model_license_status NOT NULL DEFAULT 'unreviewed',
    license_reviewed_by       TEXT,
    license_reviewed_at       TIMESTAMPTZ,

    -- Lifecycle
    lifecycle_state           model_lifecycle_state NOT NULL DEFAULT 'registered',
    lifecycle_registered_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    lifecycle_activated_at    TIMESTAMPTZ,
    lifecycle_deprecated_at   TIMESTAMPTZ,
    lifecycle_sunset_at       TIMESTAMPTZ,
    lifecycle_replaced_by     TEXT,
    lifecycle_reason          TEXT,

    -- Routes + audit trail
    routes                    JSONB NOT NULL DEFAULT '[]'::jsonb,
    source_metadata           JSONB NOT NULL DEFAULT '{}'::jsonb,

    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Self-referencing "replaced by" FK is ON DELETE SET NULL so deprecation chains
-- survive the removal of a replacement row.
DO $$ BEGIN
    ALTER TABLE models
        ADD CONSTRAINT models_replaced_by_fk
        FOREIGN KEY (lifecycle_replaced_by)
        REFERENCES models(registry_id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE INDEX IF NOT EXISTS models_by_vendor_family  ON models (vendor, family);
CREATE INDEX IF NOT EXISTS models_by_lifecycle      ON models (lifecycle_state);
CREATE INDEX IF NOT EXISTS models_by_license_status ON models (license_status);
CREATE INDEX IF NOT EXISTS models_routes_gin        ON models USING GIN (routes jsonb_path_ops);

-- ── updated_at trigger ───────────────────────────────────────────────────────
CREATE OR REPLACE FUNCTION trg_models_set_updated_at() RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at := NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_models_updated_at ON models;
CREATE TRIGGER trg_models_updated_at
    BEFORE UPDATE ON models
    FOR EACH ROW
    EXECUTE FUNCTION trg_models_set_updated_at();

-- ── registry_config (REQ-REG-008 singleton) ──────────────────────────────────
CREATE TABLE IF NOT EXISTS registry_config (
    id                        SMALLINT PRIMARY KEY CHECK (id = 1),
    product_default_model_id  UUID NOT NULL REFERENCES models(id) ON DELETE RESTRICT,
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by                TEXT
);

-- ── model_role_defaults ──────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS model_role_defaults (
    role        TEXT PRIMARY KEY
                CHECK (role IN (
                    'classifier','structured','reasoning',
                    'long-context','summarize','thinking'
                )),
    registry_id TEXT NOT NULL REFERENCES models(registry_id) ON DELETE RESTRICT,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DROP TRIGGER IF EXISTS trg_role_defaults_updated_at ON model_role_defaults;
CREATE TRIGGER trg_role_defaults_updated_at
    BEFORE UPDATE ON model_role_defaults
    FOR EACH ROW
    EXECUTE FUNCTION trg_models_set_updated_at();

-- ─────────────────────────────────────────────────────────────────────────────
-- Seed (REQ-SEED-001..006)
-- Mirrors AvailableModels in back/go-assistant/infra/openrouter/openrouter.go:52-65.
-- Prices from modelCostTable (converted from $/1M to $/token). Unknown prices = 0.
-- Idempotent via ON CONFLICT (registry_id) DO NOTHING.
-- ─────────────────────────────────────────────────────────────────────────────

INSERT INTO models (
    registry_id, vendor, family, version, display_name,
    pricing_input_per_token, pricing_output_per_token,
    license_kind, license_name, license_source, license_status,
    license_reviewed_by, license_reviewed_at,
    lifecycle_state, lifecycle_activated_at,
    routes
) VALUES
-- Anthropic — commercial API terms, approved
(
    'anthropic/claude-opus-4-6', 'anthropic', 'claude', 'opus-4.6',
    'Anthropic Claude Opus 4.6',
    0.000015, 0.000075,
    'proprietary-api', 'Anthropic Commercial Terms', 'manual', 'approved-commercial',
    'seed-migration-020', NOW(),
    'active', NOW(),
    '[{"provider_adapter":"openrouter","provider_model_id":"anthropic/claude-opus-4-6","priority":0,"enabled":true}]'::jsonb
),
(
    'anthropic/claude-sonnet-4-6', 'anthropic', 'claude', 'sonnet-4.6',
    'Anthropic Claude Sonnet 4.6',
    0.000003, 0.000015,
    'proprietary-api', 'Anthropic Commercial Terms', 'manual', 'approved-commercial',
    'seed-migration-020', NOW(),
    'active', NOW(),
    '[{"provider_adapter":"openrouter","provider_model_id":"anthropic/claude-sonnet-4-6","priority":0,"enabled":true}]'::jsonb
),
(
    'anthropic/claude-haiku-4-5-20251001', 'anthropic', 'claude', 'haiku-4.5-20251001',
    'Anthropic Claude Haiku 4.5 (2025-10-01)',
    0.00000080, 0.0000040,
    'proprietary-api', 'Anthropic Commercial Terms', 'manual', 'approved-commercial',
    'seed-migration-020', NOW(),
    'active', NOW(),
    '[{"provider_adapter":"openrouter","provider_model_id":"anthropic/claude-haiku-4-5-20251001","priority":0,"enabled":true}]'::jsonb
),
-- Google Gemma — community, Gemma Terms of Use, approved-commercial
(
    'google/gemma-4-31b-it', 'google', 'gemma', '4-31b-it',
    'Google Gemma 4 31B Instruct',
    0, 0,
    'community', 'Gemma Terms of Use', 'manual', 'approved-commercial',
    'seed-migration-020', NOW(),
    'active', NOW(),
    '[{"provider_adapter":"openrouter","provider_model_id":"google/gemma-4-31b-it","priority":0,"enabled":true}]'::jsonb
),
(
    'google/gemma-4-26b-a4b-it', 'google', 'gemma', '4-26b-a4b-it',
    'Google Gemma 4 26B A4B Instruct',
    0, 0,
    'community', 'Gemma Terms of Use', 'manual', 'approved-commercial',
    'seed-migration-020', NOW(),
    'active', NOW(),
    '[{"provider_adapter":"openrouter","provider_model_id":"google/gemma-4-26b-a4b-it","priority":0,"enabled":true}]'::jsonb
),
-- Google Gemini — commercial API terms, approved
(
    'google/gemini-3.1-flash-lite-preview', 'google', 'gemini', '3.1-flash-lite-preview',
    'Google Gemini 3.1 Flash Lite (Preview)',
    0, 0,
    'proprietary-api', 'Google Generative AI Additional Terms', 'manual', 'approved-commercial',
    'seed-migration-020', NOW(),
    'active', NOW(),
    '[{"provider_adapter":"openrouter","provider_model_id":"google/gemini-3.1-flash-lite-preview","priority":0,"enabled":true}]'::jsonb
),
(
    'google/gemini-2.5-flash-lite', 'google', 'gemini', '2.5-flash-lite',
    'Google Gemini 2.5 Flash Lite',
    0, 0,
    'proprietary-api', 'Google Generative AI Additional Terms', 'manual', 'approved-commercial',
    'seed-migration-020', NOW(),
    'active', NOW(),
    '[{"provider_adapter":"openrouter","provider_model_id":"google/gemini-2.5-flash-lite","priority":0,"enabled":true}]'::jsonb
),
(
    'google/gemini-2.0-flash-001', 'google', 'gemini', '2.0-flash-001',
    'Google Gemini 2.0 Flash',
    0.00000010, 0.00000040,
    'proprietary-api', 'Google Generative AI Additional Terms', 'manual', 'approved-commercial',
    'seed-migration-020', NOW(),
    'active', NOW(),
    '[{"provider_adapter":"openrouter","provider_model_id":"google/gemini-2.0-flash-001","priority":0,"enabled":true}]'::jsonb
),
-- Z.ai GLM — community license, unreviewed (requires admin sign-off before invokable)
(
    'z-ai/glm-5.1', 'z-ai', 'glm', '5.1',
    'Z.ai GLM 5.1',
    0, 0,
    'community', 'GLM License', 'manual', 'unreviewed',
    NULL, NULL,
    'registered', NULL,
    '[{"provider_adapter":"openrouter","provider_model_id":"z-ai/glm-5.1","priority":0,"enabled":true}]'::jsonb
)
ON CONFLICT (registry_id) DO NOTHING;

-- Initialize the singleton pointer to Gemma.
INSERT INTO registry_config (id, product_default_model_id, updated_by)
SELECT 1, m.id, 'seed-migration-020'
FROM models m
WHERE m.registry_id = 'google/gemma-4-31b-it'
ON CONFLICT (id) DO NOTHING;

-- Role defaults all point to Gemma (parent spec parity).
INSERT INTO model_role_defaults (role, registry_id) VALUES
    ('classifier',   'google/gemma-4-31b-it'),
    ('structured',   'google/gemma-4-31b-it'),
    ('reasoning',    'google/gemma-4-31b-it'),
    ('long-context', 'google/gemma-4-31b-it'),
    ('summarize',    'google/gemma-4-31b-it'),
    ('thinking',     'google/gemma-4-31b-it')
ON CONFLICT (role) DO NOTHING;
