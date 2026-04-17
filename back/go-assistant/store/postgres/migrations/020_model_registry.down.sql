-- Reverse of 020_model_registry.up.sql.

DROP TRIGGER IF EXISTS trg_role_defaults_updated_at ON model_role_defaults;
DROP TRIGGER IF EXISTS trg_models_updated_at ON models;

DROP TABLE IF EXISTS model_role_defaults;
DROP TABLE IF EXISTS registry_config;
DROP TABLE IF EXISTS models;

DROP FUNCTION IF EXISTS trg_models_set_updated_at();

DROP TYPE IF EXISTS model_license_status;
DROP TYPE IF EXISTS model_lifecycle_state;
