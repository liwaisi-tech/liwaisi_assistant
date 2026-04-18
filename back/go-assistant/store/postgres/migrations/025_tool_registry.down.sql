-- Reverse of 025_tool_registry.up.sql.

DROP INDEX IF EXISTS idx_tools_deprecated;
DROP INDEX IF EXISTS idx_tools_origin;
DROP INDEX IF EXISTS idx_tools_latest;
DROP TABLE IF EXISTS tools;
