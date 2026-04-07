CREATE TABLE IF NOT EXISTS admin_audit_log (
    id          BIGSERIAL PRIMARY KEY,
    key         TEXT NOT NULL,
    action      TEXT NOT NULL CHECK (action IN ('set','delete')),
    updated_by  TEXT NOT NULL,
    ts          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_admin_audit_log_ts ON admin_audit_log(ts DESC);
