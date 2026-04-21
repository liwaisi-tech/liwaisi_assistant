-- Reverse of 022_model_role_defaults_optimize.up.sql: reset every role default
-- back to google/gemma-4-31b-it (the 020 baseline).

UPDATE model_role_defaults SET registry_id = 'google/gemma-4-31b-it'
WHERE role IN ('classifier','structured','reasoning','long-context','summarize','thinking');
