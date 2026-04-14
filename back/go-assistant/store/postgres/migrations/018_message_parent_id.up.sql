-- 018_message_parent_id.up.sql
-- Adds parent_message_id to support HITL response → A2UI surface linkage.
-- NULL means "no parent" — the row is not a response to an earlier message.
-- See spec-process-bugfix-a2ui-hitl-response-persistence.md REQ-007.
ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS parent_message_id TEXT;
