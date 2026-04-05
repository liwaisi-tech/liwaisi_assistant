DROP INDEX IF EXISTS idx_sessions_forked_from;
DROP INDEX IF EXISTS idx_sessions_user_active;
ALTER TABLE sessions DROP COLUMN IF EXISTS fork_message_count;
ALTER TABLE sessions DROP COLUMN IF EXISTS forked_from_session_id;
ALTER TABLE sessions DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE sessions DROP COLUMN IF EXISTS title;
