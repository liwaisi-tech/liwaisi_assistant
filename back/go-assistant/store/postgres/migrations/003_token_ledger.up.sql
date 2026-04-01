CREATE TABLE IF NOT EXISTS token_ledger (
    session_id      TEXT PRIMARY KEY,
    input_tokens    BIGINT NOT NULL DEFAULT 0,
    output_tokens   BIGINT NOT NULL DEFAULT 0,
    calls           BIGINT NOT NULL DEFAULT 0,
    total_cost_usd  DOUBLE PRECISION NOT NULL DEFAULT 0,
    daily_total_usd DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_updated    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_token_ledger_last_updated ON token_ledger (last_updated);
