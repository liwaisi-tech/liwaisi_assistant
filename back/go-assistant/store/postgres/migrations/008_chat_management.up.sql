-- 008_chat_management: Multi-chat management with conversation forking.
-- Adds title, soft-delete, and fork lineage to sessions.

ALTER TABLE sessions ADD COLUMN IF NOT EXISTS title TEXT;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS forked_from_session_id TEXT;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS fork_message_count INT;

-- Index for listing active chats per user (exclude soft-deleted and expired)
CREATE INDEX IF NOT EXISTS idx_sessions_user_active
  ON sessions (user_id, last_activity_at DESC)
  WHERE deleted_at IS NULL AND state != 'expired';

-- Index for fork lineage queries
CREATE INDEX IF NOT EXISTS idx_sessions_forked_from
  ON sessions (forked_from_session_id)
  WHERE forked_from_session_id IS NOT NULL;
