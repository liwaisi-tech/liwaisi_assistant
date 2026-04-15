-- Reverse of 019_flows_recanonicalize.up.sql. Restores deleted_at = NULL for
-- every flow row whose topology_json still carries a legacy role-alias
-- Model string. Idempotent: running this after an already-reverted DB
-- simply finds no matching rows.
UPDATE flows
SET deleted_at = NULL
WHERE deleted_at IS NOT NULL
  AND topology_json::text ~ '"model"\s*:\s*"(classifier|structured|reasoning|long-context|summarize|thinking|structured-cheap)"';
