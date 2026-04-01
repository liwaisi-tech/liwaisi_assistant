CREATE TABLE IF NOT EXISTS flows (
    hash             TEXT PRIMARY KEY,
    role             TEXT NOT NULL,
    topology_json    JSONB NOT NULL,
    function_mapping JSONB NOT NULL,
    execution_count  BIGINT NOT NULL DEFAULT 0,
    success_rate     DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_cost_usd     DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_duration_ms  BIGINT NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_flows_role ON flows (role) WHERE deleted_at IS NULL;
