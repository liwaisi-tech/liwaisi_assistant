CREATE TABLE IF NOT EXISTS events (
    id              TEXT NOT NULL,
    type            TEXT NOT NULL,
    session_id      TEXT NOT NULL,
    cpn_id          TEXT NOT NULL,
    cpn_role        TEXT,
    cpn_depth       INT DEFAULT 0,
    transition_id   TEXT,
    transition_kind TEXT,
    token_snapshot  JSONB,
    payload         JSONB,
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, timestamp)
) PARTITION BY RANGE (timestamp);

CREATE TABLE IF NOT EXISTS events_default PARTITION OF events DEFAULT;

CREATE INDEX IF NOT EXISTS idx_events_session_id ON events (session_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_events_cpn_id ON events (cpn_id, timestamp);
CREATE INDEX IF NOT EXISTS idx_events_type ON events (type, timestamp);
