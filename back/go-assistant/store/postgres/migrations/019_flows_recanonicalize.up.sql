-- Migration 019: soft-delete legacy flow rows whose topology_json still
-- carries role-alias strings in LLMConfig.Model. Required by
-- spec-architecture-model-selection-centralization.md REQ-MIG-001.
--
-- Rationale: before the spec, topology authors encoded intent as
-- LLMConfig.Model="classifier" (or "structured", etc.). That string entered
-- the SHA-256 topology hash, which is the PK of the flows row. With
-- REQ-CFG-003 the authored Model is always "" and Role carries the intent,
-- so every flow row with a legacy-alias Model is now un-replayable. We
-- soft-delete (deleted_at = NOW()) rather than hard-delete to preserve
-- monitoring data (CON-001).
--
-- The regex matches every canonical alias; the JSONB cast to text is
-- necessary because the alias lives inside a nested transition object.
UPDATE flows
SET deleted_at = NOW()
WHERE deleted_at IS NULL
  AND topology_json::text ~ '"model"\s*:\s*"(classifier|structured|reasoning|long-context|summarize|thinking|structured-cheap)"';
