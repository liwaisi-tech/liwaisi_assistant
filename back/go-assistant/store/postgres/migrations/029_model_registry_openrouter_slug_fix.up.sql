-- 029_model_registry_openrouter_slug_fix.up.sql
--
-- DAT-FIX-005 + REQ-FIX-012 (PR #95 review sweep).
--
-- (1) Migration 021 seeded Anthropic models with dot-versioned
--     provider_model_id slugs ("anthropic/claude-opus-4.7"), but OpenRouter's
--     canonical ids use dashes ("anthropic/claude-opus-4-7"). Requests with
--     the dot form 404 at the provider. Correct every affected row in-place.
--
-- (2) Migration 020 pointed the product default + every role default at
--     "google/gemma-4-31b-it", a slug that does not exist on OpenRouter.
--     Repoint both at "google/gemini-2.5-flash" (confirmed available).
--
-- All UPDATEs are WHERE-guarded so re-running is a no-op.

BEGIN;

-- (1) Anthropic dot → dash slug corrections. routes is a JSONB array; the
-- seed rows from 021 place the openrouter route at index 0.

UPDATE models
SET routes = jsonb_set(
    routes,
    '{0,provider_model_id}',
    to_jsonb('anthropic/claude-opus-4-7'::text),
    false
)
WHERE registry_id = 'anthropic/claude-opus-4-7'
  AND routes->0->>'provider_model_id' = 'anthropic/claude-opus-4.7';

UPDATE models
SET routes = jsonb_set(
    routes,
    '{0,provider_model_id}',
    to_jsonb('anthropic/claude-sonnet-4-5'::text),
    false
)
WHERE registry_id = 'anthropic/claude-sonnet-4-5'
  AND routes->0->>'provider_model_id' = 'anthropic/claude-sonnet-4.5';

UPDATE models
SET routes = jsonb_set(
    routes,
    '{0,provider_model_id}',
    to_jsonb('anthropic/claude-haiku-4-5'::text),
    false
)
WHERE registry_id = 'anthropic/claude-haiku-4-5'
  AND routes->0->>'provider_model_id' = 'anthropic/claude-haiku-4.5';

-- (2) Repoint product default from the non-existent gemma-4-31b-it slug
-- to the verified gemini-2.5-flash row.

UPDATE registry_config
SET product_default_model_id = (
        SELECT id FROM models WHERE registry_id = 'google/gemini-2.5-flash'
    ),
    updated_by = 'migration-029',
    updated_at = NOW()
WHERE id = 1
  AND product_default_model_id = (
        SELECT id FROM models WHERE registry_id = 'google/gemma-4-31b-it'
    );

-- Rebind every role default that still points at gemma-4-31b-it. Idempotent
-- — rows already pointing elsewhere are untouched.
UPDATE model_role_defaults
SET registry_id = 'google/gemini-2.5-flash'
WHERE registry_id = 'google/gemma-4-31b-it';

COMMIT;
