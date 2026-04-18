-- Dynamic ToolRegistry persistence (GAP-3).
-- Spec: spec/spec-architecture-dynamic-tool-registry.md §3 REQ-011.
--
-- The row is the durable twin of cpn/tools.ToolEntry. Uniqueness is on
-- (namespace, name, version) so the registry can store multiple versions of
-- the same anchor (ns/name). The lookup index covers the "latest non-
-- deprecated" resolve path that happens on every NodeKindTool firing.

CREATE TABLE IF NOT EXISTS tools (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    namespace           TEXT        NOT NULL,
    name                TEXT        NOT NULL,
    version             TEXT        NOT NULL,
    schema              JSONB       NOT NULL DEFAULT '{}'::jsonb,
    help_text           TEXT        NOT NULL DEFAULT '',
    man_page            TEXT        NOT NULL DEFAULT '',
    binary_path         TEXT        NOT NULL DEFAULT '',
    binary_sha256       TEXT        NOT NULL DEFAULT '',
    origin              TEXT        NOT NULL,
    provenance          JSONB       NOT NULL DEFAULT '{}'::jsonb,
    registered_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    registered_by       TEXT        NOT NULL DEFAULT '',
    deprecated          BOOLEAN     NOT NULL DEFAULT FALSE,
    deprecated_at       TIMESTAMPTZ,
    deprecation_reason  TEXT,
    UNIQUE (namespace, name, version)
);

CREATE INDEX IF NOT EXISTS idx_tools_latest    ON tools(namespace, name, deprecated);
CREATE INDEX IF NOT EXISTS idx_tools_origin    ON tools(origin);
CREATE INDEX IF NOT EXISTS idx_tools_deprecated ON tools(deprecated);
