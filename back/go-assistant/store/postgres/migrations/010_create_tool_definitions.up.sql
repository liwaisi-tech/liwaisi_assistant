CREATE TABLE IF NOT EXISTS tool_definitions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    namespace    TEXT NOT NULL DEFAULT 'user',
    description  TEXT NOT NULL,
    input_color  TEXT NOT NULL,
    output_color TEXT NOT NULL,
    parameters   JSONB,
    version      TEXT NOT NULL DEFAULT '1.0',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, namespace, name)
);

CREATE INDEX IF NOT EXISTS idx_tool_definitions_user_id ON tool_definitions(user_id);
