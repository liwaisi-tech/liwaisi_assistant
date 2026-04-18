-- Authored-artefact provenance ledger (GAP-10).
-- Spec: spec-architecture-authored-artifact-provenance.md §3 REQ-001.
--
-- Every file that brae writes under the path jail ($HOME/.local/brae/)
-- lands a row here; rollback flips state="quarantined" + quarantined_at,
-- restore flips state="restored" + restored_at, and the purge job flips
-- state="purged" + purged_at once the grace window expires.
--
-- The set_id groups artefacts that must be rolled back atomically (spec
-- §4). artefact_events is the append-only audit trail per REQ-008.

CREATE TABLE IF NOT EXISTS authored_artefacts (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    path               TEXT        NOT NULL,
    classification     TEXT        NOT NULL,
    set_id             TEXT        NOT NULL,
    forge_run_id       TEXT        NOT NULL DEFAULT '',
    host_id            TEXT        NOT NULL DEFAULT '',
    flow_hash          TEXT        NOT NULL DEFAULT '',
    authoring_cpn_id   TEXT        NOT NULL DEFAULT '',
    transition_id      TEXT        NOT NULL DEFAULT '',
    session_id         TEXT        NOT NULL DEFAULT '',
    sha256             TEXT        NOT NULL DEFAULT '',
    size_bytes         BIGINT      NOT NULL DEFAULT 0,
    mime               TEXT        NOT NULL DEFAULT '',
    mode               INTEGER     NOT NULL DEFAULT 0,
    state              TEXT        NOT NULL DEFAULT 'pending',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    quarantined_at     TIMESTAMPTZ,
    restored_at        TIMESTAMPTZ,
    purged_at          TIMESTAMPTZ,
    quarantine_path    TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_authored_artefacts_set        ON authored_artefacts(set_id);
CREATE INDEX IF NOT EXISTS idx_authored_artefacts_forge_run  ON authored_artefacts(forge_run_id);
CREATE INDEX IF NOT EXISTS idx_authored_artefacts_host       ON authored_artefacts(host_id);
CREATE INDEX IF NOT EXISTS idx_authored_artefacts_state      ON authored_artefacts(state);
CREATE INDEX IF NOT EXISTS idx_authored_artefacts_quarantine ON authored_artefacts(state, quarantined_at)
    WHERE state = 'quarantined';

CREATE TABLE IF NOT EXISTS artefact_events (
    event_id UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    set_id   TEXT        NOT NULL,
    kind     TEXT        NOT NULL,
    actor    TEXT        NOT NULL DEFAULT '',
    at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_artefact_events_set ON artefact_events(set_id, at);
