-- Encrypted platform configuration key-value store.
CREATE TABLE IF NOT EXISTS platform_config (
    key        TEXT PRIMARY KEY,
    value      BYTEA NOT NULL,
    nonce      BYTEA NOT NULL,
    is_secret  BOOLEAN NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT NOT NULL DEFAULT ''
);

COMMENT ON TABLE platform_config IS 'Encrypted platform configuration key-value store';
COMMENT ON COLUMN platform_config.value IS 'AES-256-GCM encrypted value';
COMMENT ON COLUMN platform_config.nonce IS 'AES-256-GCM nonce (unique per write)';
