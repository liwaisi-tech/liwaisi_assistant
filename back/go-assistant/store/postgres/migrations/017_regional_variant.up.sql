-- 017_regional_variant.up.sql
-- Adds a per-user BCP-47 regional variant used to augment LLM system prompts.
-- NULL means "not set" — application defaults to language-default at read time.
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS regional_variant TEXT;
