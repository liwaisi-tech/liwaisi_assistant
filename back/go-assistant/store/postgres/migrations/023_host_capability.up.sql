-- Host-discovery snapshots.
-- Spec: spec/spec-architecture-host-discovery-capability-registry.md §4 (REQ-012).
--
-- Append-only: every run of host-discovery-cpn writes a new row; the latest
-- snapshot per host_id is served to downstream CPNs via the repository's
-- 60 s cache.

CREATE TABLE IF NOT EXISTS host_capability_snapshots (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    host_id      TEXT NOT NULL,
    captured_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    source       TEXT NOT NULL,
    identity     JSONB NOT NULL,
    kernel       JSONB NOT NULL,
    binaries     JSONB NOT NULL,
    capabilities JSONB NOT NULL,
    raw_probes   JSONB
);

CREATE INDEX IF NOT EXISTS idx_hcs_host_captured
    ON host_capability_snapshots (host_id, captured_at DESC);
