-- GAP-6: HostGate first-run ledger + gate-decision audit.
-- Spec: spec/spec-architecture-host-gate-security-policy.md §3 REQ-010/REQ-003.

-- ── first_run_ledger ────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS first_run_ledger (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    host_id              TEXT NOT NULL,
    binary_path          TEXT NOT NULL,
    binary_sha256        TEXT NOT NULL,
    first_seen           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    first_approved_by    TEXT,
    first_approved_at    TIMESTAMPTZ,
    revoked              BOOLEAN NOT NULL DEFAULT FALSE,
    UNIQUE (host_id, binary_sha256)
);

CREATE INDEX IF NOT EXISTS first_run_ledger_by_host   ON first_run_ledger (host_id, first_seen DESC);
CREATE INDEX IF NOT EXISTS first_run_ledger_by_status ON first_run_ledger (host_id, revoked, first_approved_at);

-- ── host_gate_decisions ─────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS host_gate_decisions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id          TEXT NOT NULL,
    op_kind             TEXT NOT NULL,
    command_hash        TEXT NOT NULL,
    path                TEXT,
    sandbox             TEXT,
    decision            TEXT NOT NULL,
    reason              TEXT,
    risk_band           TEXT,
    decided_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    hitl_response_id    TEXT
);

CREATE INDEX IF NOT EXISTS host_gate_decisions_by_session ON host_gate_decisions (session_id, decided_at DESC);
CREATE INDEX IF NOT EXISTS host_gate_decisions_by_time    ON host_gate_decisions (decided_at DESC);
