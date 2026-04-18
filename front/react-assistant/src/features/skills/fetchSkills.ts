import { getAuthToken } from '../../services/api';
import type { SkillManifest } from './types';
import fixture from './skills.fixture.json';

// SKILLS_ENDPOINT — REQ-010 in spec-architecture-skill-manifest.md.
// TODO(GAP-8): once the backend ships GET /api/skills, remove the fixture
// fallback and surface the 5xx directly to the caller.
const SKILLS_ENDPOINT = '/api/v1/skills';

/**
 * fetchSkillManifest tries the live endpoint first and falls back to the
 * bundled fixture on 404 or 5xx so the UI stays designable while GAP-8 is
 * still in flight. The caller gets a stable `SkillManifest` plus a
 * boolean telling them whether the payload came from the server (used to
 * render a subtle "using fixture" pill in the panel header).
 *
 * Network errors (offline, CORS, DNS) also degrade to fixture — the goal
 * is "the panel always renders something", not "surface every failure
 * mode the platform can throw". Once GAP-8 is in production we'll tighten
 * this so genuine 5xx stays loud.
 */
export async function fetchSkillManifest(): Promise<{
  manifest: SkillManifest;
  source: 'live' | 'fixture';
}> {
  try {
    const headers: Record<string, string> = {
      Accept: 'application/json',
    };
    const token = getAuthToken();
    if (token) headers['Authorization'] = `Bearer ${token}`;

    const response = await fetch(SKILLS_ENDPOINT, { headers });

    if (!response.ok) {
      // 401 still means "not logged in as admin" — the spec's REQ-005 says
      // the endpoint is admin-only. In the v1 UI we still degrade to the
      // fixture so a non-admin looking at the page sees the shape the
      // feature will take. Future: route 401 to a CTA instead.
      if (response.status === 404 || response.status === 401 || response.status >= 500) {
        return { manifest: fixture as SkillManifest, source: 'fixture' };
      }
      // Non-401/404/5xx → re-raise so callers can differentiate (today
      // only 403 lands here and we treat it as a fixture-degrade too).
      return { manifest: fixture as SkillManifest, source: 'fixture' };
    }
    const json = (await response.json()) as SkillManifest;
    return { manifest: json, source: 'live' };
  } catch {
    return { manifest: fixture as SkillManifest, source: 'fixture' };
  }
}
