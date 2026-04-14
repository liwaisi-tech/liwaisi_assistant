-- 018_message_parent_id.down.sql
ALTER TABLE messages
  DROP COLUMN IF EXISTS parent_message_id;
