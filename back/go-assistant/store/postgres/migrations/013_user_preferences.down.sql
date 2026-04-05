ALTER TABLE users
    DROP COLUMN IF EXISTS onboarding_completed_at,
    DROP COLUMN IF EXISTS preferred_language,
    DROP COLUMN IF EXISTS preferred_model,
    DROP COLUMN IF EXISTS model_overrides;
