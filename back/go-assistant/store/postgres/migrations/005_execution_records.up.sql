CREATE TABLE IF NOT EXISTS execution_records (
    id               TEXT PRIMARY KEY,
    cpn_id           TEXT NOT NULL,
    cpn_role         TEXT NOT NULL,
    cpn_depth        INT NOT NULL DEFAULT 0,
    session_id       TEXT NOT NULL,
    transitions_fired INT NOT NULL DEFAULT 0,
    llm_calls        INT NOT NULL DEFAULT 0,
    tool_calls       INT NOT NULL DEFAULT 0,
    tokens_produced  INT NOT NULL DEFAULT 0,
    total_cost_usd   DOUBLE PRECISION NOT NULL DEFAULT 0,
    duration_ms      BIGINT NOT NULL DEFAULT 0,
    success          BOOLEAN NOT NULL DEFAULT false,
    started_at       TIMESTAMPTZ NOT NULL,
    completed_at     TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_execution_records_cpn_role ON execution_records (cpn_role, completed_at);
CREATE INDEX IF NOT EXISTS idx_execution_records_session_id ON execution_records (session_id);
