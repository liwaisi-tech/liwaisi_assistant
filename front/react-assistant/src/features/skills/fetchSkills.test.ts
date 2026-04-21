import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { fetchSkillManifest } from './fetchSkills';
import fixture from './skills.fixture.json';

describe('fetchSkillManifest', () => {
  const originalFetch = globalThis.fetch;

  beforeEach(() => {
    vi.restoreAllMocks();
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it('returns the live payload when the endpoint responds 200', async () => {
    const manifest = { ...(fixture as unknown as object), __marker: 'live-one' };
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => manifest,
    }) as unknown as typeof fetch;

    const { manifest: m, source } = await fetchSkillManifest();
    expect(source).toBe('live');
    expect((m as { __marker?: string }).__marker).toBe('live-one');
  });

  it('falls back to the fixture on 404', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 404,
      json: async () => ({}),
    }) as unknown as typeof fetch;

    const { manifest, source } = await fetchSkillManifest();
    expect(source).toBe('fixture');
    // Fixture is the bundled sample payload.
    expect(manifest.skills.length).toBe(fixture.skills.length);
  });

  it('falls back to the fixture on 5xx', async () => {
    globalThis.fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 503,
      json: async () => ({ error: 'not ready' }),
    }) as unknown as typeof fetch;

    const { source } = await fetchSkillManifest();
    expect(source).toBe('fixture');
  });

  it('falls back to the fixture on network error', async () => {
    globalThis.fetch = vi.fn().mockRejectedValue(new Error('offline')) as unknown as typeof fetch;

    const { source } = await fetchSkillManifest();
    expect(source).toBe('fixture');
  });
});
