// spec-architecture-skill-manifest.md §4 — the JSON contract returned by
// GET /api/skills. Mirrored verbatim in TS so the UI can consume the live
// endpoint once GAP-8 ships without a separate mapping step.

export type SkillKind = 'flow' | 'tool' | 'host_capability' | 'builtin';

/**
 * Origin buckets the skill manifest uses to answer "who put this here?".
 * `builtin` covers anything shipped by brae core; `user` is hand-authored
 * by the operator; `agent-authored` is produced by the tool-forge/flow
 * synth pipelines. Kept a string union so a server-side addition (e.g.
 * `"marketplace"`) round-trips as text without a UI change.
 */
export type SkillOrigin = 'builtin' | 'user' | 'agent-authored' | string;

export interface Skill {
  id: string;
  kind: SkillKind;
  name: string;
  description: string;
  version?: string;
  origin: SkillOrigin;
  deprecated: boolean;
  provenance_summary?: string;
}

export interface SkillManifest {
  built_at: string;
  skills: Skill[];
  counts: Record<string, number>;
}
