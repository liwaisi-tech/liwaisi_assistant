-- Reverse of 021_model_registry_expand.up.sql.
--
-- Un-deprecate gemini-3.1-flash-lite-preview first so its self-reference to
-- gemini-3-flash-preview is cleared before we delete the replacement row
-- (models.lifecycle_replaced_by is ON DELETE SET NULL, but being explicit is
-- clearer at rollback time).

UPDATE models
SET lifecycle_state        = 'active',
    lifecycle_deprecated_at = NULL,
    lifecycle_replaced_by   = NULL,
    lifecycle_reason        = NULL
WHERE registry_id = 'google/gemini-3.1-flash-lite-preview';

DELETE FROM models WHERE registry_id IN (
    'anthropic/claude-opus-4-7',
    'anthropic/claude-sonnet-4-5',
    'anthropic/claude-haiku-4-5',
    'google/gemini-3-pro-preview',
    'google/gemini-3-flash-preview',
    'google/gemini-2.5-pro',
    'google/gemini-2.5-flash',
    'google/gemma-3-27b-it',
    'google/gemma-3-12b-it'
);
