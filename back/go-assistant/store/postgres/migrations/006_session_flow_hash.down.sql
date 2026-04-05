DROP INDEX IF EXISTS idx_sessions_flow_hash;
ALTER TABLE sessions DROP COLUMN IF EXISTS flow_hash;
