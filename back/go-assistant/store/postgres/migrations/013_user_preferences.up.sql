-- Add onboarding tracking and user preferences to users table.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS onboarding_completed_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS preferred_language       TEXT NOT NULL DEFAULT 'en',
    ADD COLUMN IF NOT EXISTS preferred_model          TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS model_overrides          JSONB NOT NULL DEFAULT '{}';

-- Backfill: existing users are considered onboarded (they should not see the wizard).
UPDATE users SET onboarding_completed_at = created_at WHERE onboarding_completed_at IS NULL;
