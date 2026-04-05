DROP INDEX IF EXISTS idx_sessions_google_sub;
ALTER TABLE sessions DROP COLUMN IF EXISTS google_sub;
DROP TABLE IF EXISTS users;
