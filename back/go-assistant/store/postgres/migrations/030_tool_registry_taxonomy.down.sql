-- 030_tool_registry_taxonomy.down.sql
--
-- Reverse of 030_tool_registry_taxonomy.up.sql. Drops indexes first so the
-- column removal does not trip dependency errors, then drops both columns.

DROP INDEX IF EXISTS tools_toolbox_idx;
DROP INDEX IF EXISTS tools_hashtags_gin;

ALTER TABLE tools
    DROP COLUMN IF EXISTS toolbox,
    DROP COLUMN IF EXISTS hashtags;
