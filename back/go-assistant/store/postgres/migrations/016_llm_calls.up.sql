-- Per-call audit log for every LLM invocation.
-- Complements token_ledger (which is a per-session aggregate) with one row per call,
-- capturing the request payload, response text, model, tokens, and cost.
CREATE TABLE IF NOT EXISTS llm_calls (
    id                    TEXT PRIMARY KEY,
    session_id            TEXT,
    transition_id         TEXT,
    cpn_id                TEXT,
    model_requested       TEXT NOT NULL,
    model_resolved        TEXT NOT NULL,
    endpoint              TEXT NOT NULL,
    streamed              BOOLEAN NOT NULL DEFAULT FALSE,
    input_tokens          INTEGER NOT NULL DEFAULT 0,
    output_tokens         INTEGER NOT NULL DEFAULT 0,
    cache_read_tokens     INTEGER NOT NULL DEFAULT 0,
    cache_creation_tokens INTEGER NOT NULL DEFAULT 0,
    reasoning_tokens      INTEGER NOT NULL DEFAULT 0,
    cost_usd              DOUBLE PRECISION NOT NULL DEFAULT 0,
    request_messages      JSONB NOT NULL,
    response_text         TEXT NOT NULL DEFAULT '',
    finish_reason         TEXT,
    error                 TEXT,
    duration_ms           BIGINT NOT NULL DEFAULT 0,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_llm_calls_session_id ON llm_calls (session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_llm_calls_created_at ON llm_calls (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_llm_calls_model ON llm_calls (model_resolved);
