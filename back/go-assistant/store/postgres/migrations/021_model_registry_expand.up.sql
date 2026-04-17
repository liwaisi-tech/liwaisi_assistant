-- Expand the model registry with current-generation Anthropic + Google models
-- routed through OpenRouter, and deprecate one stale preview.
--
-- Sourced from https://openrouter.ai/provider/anthropic and
-- https://openrouter.ai/google on 2026-04-17.
--
-- Idempotent: INSERTs use ON CONFLICT (registry_id) DO NOTHING; the
-- deprecation UPDATE re-runs safely because lifecycle_state + replaced_by are
-- both driven by the target registry_id.
--
-- Product default and role defaults are intentionally NOT touched.

INSERT INTO models (
    registry_id, vendor, family, version, display_name, description,
    context_length,
    pricing_input_per_token, pricing_output_per_token,
    license_kind, license_name, license_source, license_status,
    license_reviewed_by, license_reviewed_at,
    lifecycle_state, lifecycle_activated_at,
    routes
) VALUES
-- ── Anthropic ────────────────────────────────────────────────────────────────
(
    'anthropic/claude-opus-4-7', 'anthropic', 'claude', 'opus-4.7',
    'Anthropic Claude Opus 4.7',
    'Flagship Anthropic model with 1M context, long-running agents and extended sessions.',
    1000000,
    0.000005, 0.000025,
    'proprietary-api', 'Anthropic Commercial Terms', 'manual', 'approved-commercial',
    'seed-migration-021', NOW(),
    'active', NOW(),
    '[{"provider_adapter":"openrouter","provider_model_id":"anthropic/claude-opus-4.7","priority":0,"enabled":true}]'::jsonb
),
(
    'anthropic/claude-sonnet-4-5', 'anthropic', 'claude', 'sonnet-4.5',
    'Anthropic Claude Sonnet 4.5',
    '1M-context Sonnet generation tuned for tool orchestration and extended autonomous agents.',
    1000000,
    0.000003, 0.000015,
    'proprietary-api', 'Anthropic Commercial Terms', 'manual', 'approved-commercial',
    'seed-migration-021', NOW(),
    'active', NOW(),
    '[{"provider_adapter":"openrouter","provider_model_id":"anthropic/claude-sonnet-4.5","priority":0,"enabled":true}]'::jsonb
),
(
    'anthropic/claude-haiku-4-5', 'anthropic', 'claude', 'haiku-4.5',
    'Anthropic Claude Haiku 4.5',
    'Latest-alias Haiku: 200K context, extended thinking, web search, computer use.',
    200000,
    0.000001, 0.000005,
    'proprietary-api', 'Anthropic Commercial Terms', 'manual', 'approved-commercial',
    'seed-migration-021', NOW(),
    'active', NOW(),
    '[{"provider_adapter":"openrouter","provider_model_id":"anthropic/claude-haiku-4.5","priority":0,"enabled":true}]'::jsonb
),
-- ── Google Gemini (proprietary) ──────────────────────────────────────────────
(
    'google/gemini-3-pro-preview', 'google', 'gemini', '3-pro-preview',
    'Google Gemini 3 Pro (Preview)',
    'Flagship Gemini 3 preview: multimodal (text, image, video, audio, code), tool calling, 1M context.',
    1048576,
    0, 0,
    'proprietary-api', 'Google Generative AI Additional Terms', 'manual', 'approved-commercial',
    'seed-migration-021', NOW(),
    'active', NOW(),
    '[{"provider_adapter":"openrouter","provider_model_id":"google/gemini-3-pro-preview","priority":0,"enabled":true}]'::jsonb
),
(
    'google/gemini-3-flash-preview', 'google', 'gemini', '3-flash-preview',
    'Google Gemini 3 Flash (Preview)',
    'Balanced Gemini 3 preview: multimodal with configurable thinking and tool use, 1M context.',
    1048576,
    0.0000005, 0.000003,
    'proprietary-api', 'Google Generative AI Additional Terms', 'manual', 'approved-commercial',
    'seed-migration-021', NOW(),
    'active', NOW(),
    '[{"provider_adapter":"openrouter","provider_model_id":"google/gemini-3-flash-preview","priority":0,"enabled":true}]'::jsonb
),
(
    'google/gemini-2.5-pro', 'google', 'gemini', '2.5-pro',
    'Google Gemini 2.5 Pro',
    'GA flagship of the 2.5 family: advanced reasoning, coding, thinking mode, 1M context.',
    1048576,
    0.00000125, 0.00001,
    'proprietary-api', 'Google Generative AI Additional Terms', 'manual', 'approved-commercial',
    'seed-migration-021', NOW(),
    'active', NOW(),
    '[{"provider_adapter":"openrouter","provider_model_id":"google/gemini-2.5-pro","priority":0,"enabled":true}]'::jsonb
),
(
    'google/gemini-2.5-flash', 'google', 'gemini', '2.5-flash',
    'Google Gemini 2.5 Flash',
    'GA 2.5 Flash: reasoning, coding, mathematics with thinking mode, 1M context.',
    1048576,
    0.0000003, 0.0000025,
    'proprietary-api', 'Google Generative AI Additional Terms', 'manual', 'approved-commercial',
    'seed-migration-021', NOW(),
    'active', NOW(),
    '[{"provider_adapter":"openrouter","provider_model_id":"google/gemini-2.5-flash","priority":0,"enabled":true}]'::jsonb
),
-- ── Google Gemma (community) ─────────────────────────────────────────────────
(
    'google/gemma-3-27b-it', 'google', 'gemma', '3-27b-it',
    'Google Gemma 3 27B Instruct',
    'Open-weight multimodal vision-language model with function calling, 131K context.',
    131072,
    0.00000008, 0.00000016,
    'community', 'Gemma Terms of Use', 'manual', 'approved-commercial',
    'seed-migration-021', NOW(),
    'active', NOW(),
    '[{"provider_adapter":"openrouter","provider_model_id":"google/gemma-3-27b-it","priority":0,"enabled":true}]'::jsonb
),
(
    'google/gemma-3-12b-it', 'google', 'gemma', '3-12b-it',
    'Google Gemma 3 12B Instruct',
    'Open-weight mid-tier Gemma 3 with multimodal vision-language and function calling, 131K context.',
    131072,
    0.00000004, 0.00000013,
    'community', 'Gemma Terms of Use', 'manual', 'approved-commercial',
    'seed-migration-021', NOW(),
    'active', NOW(),
    '[{"provider_adapter":"openrouter","provider_model_id":"google/gemma-3-12b-it","priority":0,"enabled":true}]'::jsonb
)
ON CONFLICT (registry_id) DO NOTHING;

-- ── Deprecate: gemini 3.1 flash lite preview is superseded by gemini 3 flash
--               preview (stable context, broader capabilities). Keep the row
--               so audit trails and historical llm_calls references survive.
-- Idempotent by targeting registry_id.
UPDATE models
SET lifecycle_state       = 'deprecated',
    lifecycle_deprecated_at = COALESCE(lifecycle_deprecated_at, NOW()),
    lifecycle_replaced_by = 'google/gemini-3-flash-preview',
    lifecycle_reason      = 'Superseded by google/gemini-3-flash-preview (migration 021).'
WHERE registry_id = 'google/gemini-3.1-flash-lite-preview'
  AND lifecycle_state <> 'deprecated';
