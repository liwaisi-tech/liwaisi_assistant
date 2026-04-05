ALTER TABLE sessions ADD COLUMN IF NOT EXISTS flow_hash TEXT;
CREATE INDEX IF NOT EXISTS idx_sessions_flow_hash ON sessions (flow_hash) WHERE flow_hash IS NOT NULL;
