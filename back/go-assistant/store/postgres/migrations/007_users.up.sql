-- 007_users: User management for Google OAuth authentication.
-- Users are identified by Google's 'sub' claim (immutable, unique per account).

CREATE TABLE users (
    id         TEXT PRIMARY KEY,               -- Google 'sub' claim
    email      TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    picture    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_users_email ON users (email);

-- Add google_sub column to sessions for gradual migration.
-- NOT a foreign key to avoid breaking existing sessions with legacy user_id values.
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS google_sub TEXT;
CREATE INDEX IF NOT EXISTS idx_sessions_google_sub ON sessions (google_sub);
