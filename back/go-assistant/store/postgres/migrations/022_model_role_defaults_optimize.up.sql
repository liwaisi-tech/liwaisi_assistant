-- Repoint model_role_defaults at the most cost-effective model per role.
-- Decisions documented in-line; all targets are invokable rows seeded by 020/021.
--
--   classifier   → gemini 2.5 flash-lite (ultra-low latency tier, cheapest viable
--                  routing model; $0.10/$0.40 per M tokens)
--   structured   → gemini 2.5 flash (reliable JSON adherence + thinking mode)
--   reasoning    → gemini 2.5 pro (1M ctx + native thinking; beats Sonnet 4.6
--                  on $/capability at $1.25/$10 per M)
--   long-context → gemini 2.5 flash (1M ctx, cheapest for large-doc passes)
--   summarize    → gemini 2.5 flash-lite (cheapest + fast, sufficient)
--   thinking     → claude opus 4.7 (reserved Anthropic slot for max reasoning)
--
-- Simple-mode product default (registry_config) is intentionally NOT changed:
-- onboarding copy "El predeterminado es Gemma 4 31B ..." is still accurate.

UPDATE model_role_defaults SET registry_id = 'google/gemini-2.5-flash-lite' WHERE role = 'classifier';
UPDATE model_role_defaults SET registry_id = 'google/gemini-2.5-flash'      WHERE role = 'structured';
UPDATE model_role_defaults SET registry_id = 'google/gemini-2.5-pro'        WHERE role = 'reasoning';
UPDATE model_role_defaults SET registry_id = 'google/gemini-2.5-flash'      WHERE role = 'long-context';
UPDATE model_role_defaults SET registry_id = 'google/gemini-2.5-flash-lite' WHERE role = 'summarize';
UPDATE model_role_defaults SET registry_id = 'anthropic/claude-opus-4-7'    WHERE role = 'thinking';
