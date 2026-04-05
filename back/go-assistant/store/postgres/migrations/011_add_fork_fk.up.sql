-- 011_add_fork_fk: Add foreign key constraint on forked_from_session_id.
-- Uses ON DELETE SET NULL so deleting a parent session does not cascade-delete children.

ALTER TABLE sessions
  ADD CONSTRAINT fk_sessions_forked_from
  FOREIGN KEY (forked_from_session_id)
  REFERENCES sessions(id)
  ON DELETE SET NULL;
