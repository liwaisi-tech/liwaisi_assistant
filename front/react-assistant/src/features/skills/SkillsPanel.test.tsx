import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { SkillsPanel } from './SkillsPanel';
import type { SkillManifest } from './types';
import fixture from './skills.fixture.json';

function buildFixtureLoader(override?: Partial<{
  manifest: SkillManifest;
  source: 'live' | 'fixture';
}>) {
  return vi.fn().mockResolvedValue({
    manifest: override?.manifest ?? (fixture as SkillManifest),
    source: override?.source ?? 'live',
  });
}

describe('SkillsPanel', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders the fixture with sections and counts', async () => {
    const loader = buildFixtureLoader({ source: 'live' });
    render(<SkillsPanel fetchManifest={loader} />);

    await waitFor(() => {
      expect(screen.getByTestId('skills-sections')).toBeInTheDocument();
    });
    // Section headers render for flows, tools, host capabilities.
    expect(screen.getByTestId('skills-section-flow')).toBeInTheDocument();
    expect(screen.getByTestId('skills-section-tool')).toBeInTheDocument();
    expect(screen.getByTestId('skills-section-host_capability')).toBeInTheDocument();
    // Source pill = live.
    expect(screen.getByTestId('skills-source-pill')).toHaveTextContent(/vivo/i);
    // At least one skill row renders under each section.
    expect(screen.getAllByTestId(/^skill-row-/).length).toBeGreaterThan(0);
  });

  it('falls back to fixture when fetch reports fixture source (404 / 5xx path)', async () => {
    const loader = buildFixtureLoader({ source: 'fixture' });
    render(<SkillsPanel fetchManifest={loader} />);

    await waitFor(() => {
      expect(screen.getByTestId('skills-source-pill')).toHaveTextContent(/gap-8/i);
    });
  });

  it('filters by origin using the radio group', async () => {
    const loader = buildFixtureLoader();
    render(<SkillsPanel fetchManifest={loader} />);
    await waitFor(() => expect(screen.getByTestId('skills-sections')).toBeInTheDocument());

    // Only agent-authored entries.
    fireEvent.click(screen.getByTestId('skills-origin-agent-authored'));

    const rows = screen.getAllByTestId(/^skill-row-/);
    // Every remaining row shows the "creado por el agente" pill.
    for (const row of rows) {
      expect(row.textContent ?? '').toMatch(/creado por el agente/i);
    }
  });

  it('filters by text search over name + description', async () => {
    const loader = buildFixtureLoader();
    render(<SkillsPanel fetchManifest={loader} />);
    await waitFor(() => expect(screen.getByTestId('skills-sections')).toBeInTheDocument());

    const input = screen.getByTestId('skills-search');
    fireEvent.change(input, { target: { value: 'compil' } });

    // "can-compile-c" and "markdown-lint" both match via description
    // containing "compil". Assert at least one row remains and the
    // non-matching rows are gone.
    const rows = screen.getAllByTestId(/^skill-row-/);
    expect(rows.length).toBeGreaterThan(0);
    expect(rows.length).toBeLessThan(fixture.skills.length);
  });

  it('hides deprecated entries by default and reveals them when the toggle is flipped', async () => {
    const loader = buildFixtureLoader();
    render(<SkillsPanel fetchManifest={loader} />);
    await waitFor(() => expect(screen.getByTestId('skills-sections')).toBeInTheDocument());

    // The fixture carries one deprecated flow ('cold-email-sweep'). It
    // should be hidden by default.
    expect(screen.queryByTestId(/^skill-row-flow\/agent\/cold-email-sweep/)).not.toBeInTheDocument();
    // Toggle on → the row shows up.
    fireEvent.click(screen.getByTestId('skills-show-deprecated'));
    await waitFor(() => {
      expect(screen.getByTestId('skill-row-flow/agent/cold-email-sweep')).toBeInTheDocument();
    });
  });

  it('collapses and expands a section on header click', async () => {
    const loader = buildFixtureLoader();
    render(<SkillsPanel fetchManifest={loader} />);
    await waitFor(() => expect(screen.getByTestId('skills-sections')).toBeInTheDocument());

    // Initially the Flows section is expanded.
    const section = screen.getByTestId('skills-section-flow');
    const toggle = section.querySelector('button[aria-expanded]') as HTMLButtonElement | null;
    expect(toggle).not.toBeNull();
    expect(toggle!).toHaveAttribute('aria-expanded', 'true');

    fireEvent.click(toggle!);
    expect(toggle!).toHaveAttribute('aria-expanded', 'false');
  });

  it('shows the "no matches" status when every filter excludes everything', async () => {
    const loader = buildFixtureLoader();
    render(<SkillsPanel fetchManifest={loader} />);
    await waitFor(() => expect(screen.getByTestId('skills-sections')).toBeInTheDocument());

    const input = screen.getByTestId('skills-search');
    fireEvent.change(input, { target: { value: 'zzzzzzz-nothing-matches' } });

    await waitFor(() => {
      expect(screen.getByTestId('skills-no-matches')).toBeInTheDocument();
    });
  });

  it('renders a loading state while the manifest fetch is pending', async () => {
    let resolve!: (v: { manifest: SkillManifest; source: 'live' | 'fixture' }) => void;
    const loader = vi.fn(
      () =>
        new Promise<{ manifest: SkillManifest; source: 'live' | 'fixture' }>((r) => {
          resolve = r;
        }),
    );
    render(<SkillsPanel fetchManifest={loader} />);
    expect(screen.getByTestId('skills-loading')).toBeInTheDocument();
    resolve({ manifest: fixture as SkillManifest, source: 'live' });
    await waitFor(() => {
      expect(screen.queryByTestId('skills-loading')).not.toBeInTheDocument();
    });
  });
});
