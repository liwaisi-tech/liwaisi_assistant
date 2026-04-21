-- 029_model_registry_openrouter_slug_fix.down.sql
--
-- Reverse DAT-FIX-005 + REQ-FIX-012 corrections. Restores the original
-- (broken) state migrations 020/021 left behind so the forward/reverse
-- pair round-trips cleanly. The restored slugs do NOT resolve at the
-- provider — that is the point of 029 forward; only revert if you
-- explicitly intend to re-seed the bug.

BEGIN;

UPDATE models
SET routes = jsonb_set(
    routes,
    '{0,provider_model_id}',
    to_jsonb('anthropic/claude-opus-4.7'::text),
    false
)
WHERE registry_id = 'anthropic/claude-opus-4-7'
  AND routes->0->>'provider_model_id' = 'anthropic/claude-opus-4-7';

UPDATE models
SET routes = jsonb_set(
    routes,
    '{0,provider_model_id}',
    to_jsonb('anthropic/claude-sonnet-4.5'::text),
    false
)
WHERE registry_id = 'anthropic/claude-sonnet-4-5'
  AND routes->0->>'provider_model_id' = 'anthropic/claude-sonnet-4-5';

UPDATE models
SET routes = jsonb_set(
    routes,
    '{0,provider_model_id}',
    to_jsonb('anthropic/claude-haiku-4.5'::text),
    false
)
WHERE registry_id = 'anthropic/claude-haiku-4-5'
  AND routes->0->>'provider_model_id' = 'anthropic/claude-haiku-4-5';

-- Role + product default repoint is only reversible when the gemma row
-- still exists. Skip cleanly otherwise.
UPDATE model_role_defaults
SET registry_id = 'google/gemma-4-31b-it'
WHERE registry_id = 'google/gemini-2.5-flash'
  AND EXISTS (SELECT 1 FROM models WHERE registry_id = 'google/gemma-4-31b-it');

UPDATE registry_config
SET product_default_model_id = (
        SELECT id FROM models WHERE registry_id = 'google/gemma-4-31b-it'
    ),
    updated_by = 'migration-029-down',
    updated_at = NOW()
WHERE id = 1
  AND EXISTS (SELECT 1 FROM models WHERE registry_id = 'google/gemma-4-31b-it')
  AND product_default_model_id = (
        SELECT id FROM models WHERE registry_id = 'google/gemini-2.5-flash'
    );

COMMIT;
