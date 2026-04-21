DROP INDEX IF EXISTS idx_flows_session_id;
DROP INDEX IF EXISTS idx_flows_rejected;
DROP INDEX IF EXISTS idx_flows_origin;

ALTER TABLE flows
    DROP COLUMN IF EXISTS session_id,
    DROP COLUMN IF EXISTS referenced_primitives,
    DROP COLUMN IF EXISTS summary,
    DROP COLUMN IF EXISTS origin,
    DROP COLUMN IF EXISTS rejected_reason,
    DROP COLUMN IF EXISTS rejected,
    DROP COLUMN IF EXISTS size_transitions,
    DROP COLUMN IF EXISTS size_places,
    DROP COLUMN IF EXISTS safe_lint_passed,
    DROP COLUMN IF EXISTS authored_from_prompt_digest,
    DROP COLUMN IF EXISTS authored_by_cpn_id;
