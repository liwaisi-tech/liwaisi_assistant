-- 031_pending_tools_and_approvals.up.sql
--
-- Postgres-backed persistence for SC-12 (toolsynth) and SC-13 (toolapproval).
-- Both were in-memory only; promoting them to durable storage lets
-- awakening flows survive restarts and reason about drift across sessions.
--
-- pending_tools        — staged synthesized manifests awaiting HITL gate.
-- tool_approvals       — first-run approvals keyed by
--                        (session_id, tool_name, provenance_sha256).
--
-- Both tables key on the same composite triple so provenance-SHA256 drift
-- invalidates prior state and forces re-prompt (REQ-1303).

CREATE TABLE IF NOT EXISTS pending_tools (
    session_id         TEXT        NOT NULL,
    tool_name          TEXT        NOT NULL,
    provenance_sha256  TEXT        NOT NULL,
    manifest_json      JSONB       NOT NULL,
    source_sha256      TEXT        NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (session_id, tool_name, provenance_sha256)
);

CREATE INDEX IF NOT EXISTS pending_tools_tool_name_idx
    ON pending_tools (tool_name);

CREATE INDEX IF NOT EXISTS pending_tools_session_id_idx
    ON pending_tools (session_id);

CREATE TABLE IF NOT EXISTS tool_approvals (
    session_id         TEXT        NOT NULL,
    tool_name          TEXT        NOT NULL,
    provenance_sha256  TEXT        NOT NULL,
    approved_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (session_id, tool_name, provenance_sha256)
);

CREATE INDEX IF NOT EXISTS tool_approvals_session_id_idx
    ON tool_approvals (session_id);
