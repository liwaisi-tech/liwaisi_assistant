CREATE TABLE IF NOT EXISTS personalities (
    user_id     TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    principles  JSONB NOT NULL,
    hierarchy   JSONB NOT NULL,
    tensions    JSONB NOT NULL,
    version     INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_personalities_updated_at ON personalities(updated_at);
