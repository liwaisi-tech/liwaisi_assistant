-- 030_tool_registry_taxonomy.up.sql
--
-- Brae Toolbox Taxonomy (spec-architecture-brae-toolbox-taxonomy.md) REQ-001,
-- REQ-002, REQ-009, AC-001.
--
-- Add `hashtags` (normalised capability list) and `toolbox` (named domain
-- grouping) columns to the dynamic tool registry. The registry table is
-- `tools` (created by migration 025); the spec refers to it by its logical
-- name "tool_registry". Existing rows default to an empty hashtag set and
-- an empty toolbox string so AC-001 reads cleanly.
--
-- GIN index powers hashtag-filtered lookups (REQ-005 JIT builder path);
-- btree on toolbox powers toolbox listing.

ALTER TABLE tools
    ADD COLUMN IF NOT EXISTS hashtags text[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS toolbox  text   NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS tools_hashtags_gin
    ON tools USING GIN (hashtags);

CREATE INDEX IF NOT EXISTS tools_toolbox_idx
    ON tools (toolbox);
