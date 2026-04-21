-- GAP-7: topology mutation audit log
CREATE TABLE IF NOT EXISTS cpn_mutations (
    id              TEXT        PRIMARY KEY,
    cpn_id          TEXT        NOT NULL,
    session_id      TEXT        NOT NULL,
    mutation_kind   TEXT        NOT NULL,
    requested_by    TEXT        NOT NULL DEFAULT '',
    reason          TEXT        NOT NULL DEFAULT '',
    approved        BOOLEAN     NOT NULL DEFAULT FALSE,
    rejected_reason TEXT        NOT NULL DEFAULT '',
    applied_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS cpn_mutations_cpn_id_idx ON cpn_mutations (cpn_id, applied_at DESC);
