-- Migration 0026: agent-authored topology provenance (GAP-4).
-- Spec: spec/spec-architecture-cpn-synthesis-instantiate.md §4 REQ-041.
--
-- Adds the provenance + safety columns FlowRepository.SaveAuthored writes
-- on every synth run. The flows table already exists (migration 011);
-- every pre-existing row is a platform-authored flow so the defaults
-- leave its behaviour unchanged (safe_lint_passed defaults to false,
-- which the instantiate path reads as "this is not an agent-authored
-- flow" — existing SubNetFactory paths skip the safe-only gate).
ALTER TABLE flows
    ADD COLUMN IF NOT EXISTS authored_by_cpn_id        TEXT,
    ADD COLUMN IF NOT EXISTS authored_from_prompt_digest TEXT,
    ADD COLUMN IF NOT EXISTS safe_lint_passed          BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS size_places               INT     NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS size_transitions          INT     NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rejected                  BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS rejected_reason           TEXT,
    ADD COLUMN IF NOT EXISTS origin                    TEXT    NOT NULL DEFAULT 'platform',
    ADD COLUMN IF NOT EXISTS summary                   TEXT    NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS referenced_primitives     JSONB   NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS session_id                TEXT;

CREATE INDEX IF NOT EXISTS idx_flows_origin      ON flows(origin);
CREATE INDEX IF NOT EXISTS idx_flows_rejected    ON flows(rejected);
CREATE INDEX IF NOT EXISTS idx_flows_session_id  ON flows(session_id);
