import { useCallback, useEffect, useMemo, useState } from 'react';
import { fetchSkillManifest } from './fetchSkills';
import type { Skill, SkillManifest } from './types';

// ── SkillsPanel ────────────────────────────────────────────────────────────
//
// Read-only "what does the agent know how to do?" surface. The companion to
// spec-architecture-skill-manifest.md §1 — consumed by `GET /api/skills`
// (GAP-8, backend in flight). v1 ships against the bundled fixture so the
// UI is designable + ship-testable before the endpoint lands. When the
// manifest returns {source:'fixture'} we surface a small pill in the
// header so operators reading the page know it's not live data.
//
// Copy: Spanish-LATAM, per the rest of the chat-side UI and the brief. A
// future pass should migrate the literals to the `settings` i18n namespace.
//
// Design constraints:
// • No new deps — uses only the existing token set (DESIGN.md).
// • Read-only; no mutation paths.
// • Desktop + mobile; collapsing sections keep density manageable on phones.

interface SkillsPanelProps {
  /**
   * When set, overrides the initial fetch path. Kept for tests + for a
   * future "show this agent's manifest" deep-link where the id lands in
   * the URL. Unused in the DesktopLayout call-site.
   */
  fetchManifest?: typeof fetchSkillManifest;
}

// Section typing — we explicitly DO NOT merge `builtin` into `flow` or
// `tool` because the manifest keeps them distinct (REQ-002 in the skill
// manifest spec). For the UI, builtins land under whichever section their
// underlying shape serves (a flow-kind builtin goes in Flows, tool-kind in
// Tools). The manifest sends a `kind` that tells us which bucket.
type SectionKey = 'flow' | 'tool' | 'host_capability';

const SECTION_ORDER: SectionKey[] = ['flow', 'tool', 'host_capability'];

const SECTION_COPY: Record<SectionKey, { label: string; empty: string; empty_hint: string }> = {
  flow: {
    label: 'Flujos',
    empty: 'Aún no hay flujos disponibles.',
    empty_hint: 'Los flujos son topologías CPN que brae puede ejecutar end-to-end. Pídele al agente que construya uno cuando necesites una pieza nueva.',
  },
  tool: {
    label: 'Herramientas',
    empty: 'Aún no hay herramientas registradas.',
    empty_hint: 'Las herramientas son acciones atómicas reutilizables. Pídele al forge que cree una cuando necesites llamar algo específico.',
  },
  host_capability: {
    label: 'Capacidades del host',
    empty: 'Aún no hay capacidades detectadas.',
    empty_hint: 'Las capacidades surgen automáticamente del probe de descubrimiento. Vuelve a ejecutarlo si instalaste algo nuevo en la máquina.',
  },
};

const ORIGIN_COPY: Record<string, string> = {
  builtin: 'nativo',
  user: 'creado por ti',
  'agent-authored': 'creado por el agente',
};

const ORIGIN_FILTER_LABEL: Record<'all' | 'builtin' | 'user' | 'agent-authored', string> = {
  all: 'Todos',
  builtin: 'Nativos',
  user: 'Tuyos',
  'agent-authored': 'Del agente',
};

const COPY = {
  title: 'Habilidades',
  subtitle:
    'Inspección en solo lectura de todo lo que brae sabe hacer ahora mismo: flujos, herramientas y capacidades del host.',
  loading: 'Consultando el manifiesto\u2026',
  fixtureBadge: 'Datos de ejemplo · pendiente GAP-8',
  liveBadge: 'En vivo',
  searchPlaceholder: 'Buscar por nombre o descripción\u2026',
  showDeprecated: 'Mostrar descontinuados',
  origin: 'Origen',
  deprecatedPill: 'descontinuado',
  versionPrefix: 'v',
  countSuffix: (n: number) => (n === 1 ? '1 entrada' : `${n} entradas`),
  noMatches: 'Nada coincide con esos filtros.',
  manifestBuiltAt: 'Manifiesto compilado',
};

function bucketForSkill(s: Skill): SectionKey | null {
  if (s.kind === 'host_capability') return 'host_capability';
  if (s.kind === 'tool') return 'tool';
  // Both 'flow' and 'builtin' go in the Flows bucket — REQ-002 (aggregation
  // order) treats builtins as first-class flows for presentation.
  if (s.kind === 'flow' || s.kind === 'builtin') return 'flow';
  return null;
}

export function SkillsPanel({ fetchManifest = fetchSkillManifest }: SkillsPanelProps = {}) {
  const [manifest, setManifest] = useState<SkillManifest | null>(null);
  const [source, setSource] = useState<'live' | 'fixture' | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const [search, setSearch] = useState('');
  const [originFilter, setOriginFilter] = useState<'all' | 'builtin' | 'user' | 'agent-authored'>('all');
  const [showDeprecated, setShowDeprecated] = useState(false);
  const [collapsed, setCollapsed] = useState<Record<SectionKey, boolean>>({
    flow: false,
    tool: false,
    host_capability: false,
  });

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetchManifest()
      .then(({ manifest: m, source: s }) => {
        if (cancelled) return;
        setManifest(m);
        setSource(s);
        setLoading(false);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : 'Error de carga');
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [fetchManifest]);

  const filtered = useMemo(() => {
    if (!manifest) return {} as Record<SectionKey, Skill[]>;
    const q = search.trim().toLowerCase();
    const out: Record<SectionKey, Skill[]> = {
      flow: [],
      tool: [],
      host_capability: [],
    };
    for (const skill of manifest.skills) {
      if (!showDeprecated && skill.deprecated) continue;
      if (originFilter !== 'all' && skill.origin !== originFilter) continue;
      if (q) {
        const hay = `${skill.name} ${skill.description} ${skill.provenance_summary ?? ''}`.toLowerCase();
        if (!hay.includes(q)) continue;
      }
      const bucket = bucketForSkill(skill);
      if (bucket) out[bucket].push(skill);
    }
    return out;
  }, [manifest, search, originFilter, showDeprecated]);

  const totalVisible = useMemo(() => {
    return SECTION_ORDER.reduce((sum, key) => sum + (filtered[key]?.length ?? 0), 0);
  }, [filtered]);

  const toggleSection = useCallback((key: SectionKey) => {
    setCollapsed((prev) => ({ ...prev, [key]: !prev[key] }));
  }, []);

  const builtAtLabel = useMemo(() => {
    if (!manifest?.built_at) return '';
    try {
      return new Date(manifest.built_at).toLocaleString([], {
        dateStyle: 'medium',
        timeStyle: 'short',
      });
    } catch {
      return manifest.built_at;
    }
  }, [manifest?.built_at]);

  return (
    <div
      className="flex flex-col h-full overflow-y-auto chat-scroll"
      style={{ backgroundColor: 'var(--bg-deep)' }}
    >
      <div className="w-full max-w-3xl mx-auto px-6 py-8 flex flex-col gap-6">
        {/* ── Header ── */}
        <header className="flex flex-col gap-2">
          <div className="flex items-center gap-3 flex-wrap">
            <h1
              className="text-xl font-semibold tracking-tight"
              style={{
                fontFamily: "'JetBrains Mono', monospace",
                color: 'var(--text-primary)',
              }}
            >
              {COPY.title}
            </h1>
            {source && (
              <span
                className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-medium uppercase tracking-widest"
                style={{
                  color: source === 'fixture' ? '#fbbf24' : 'var(--accent)',
                  border: `1px solid ${source === 'fixture' ? 'rgba(245, 158, 11, 0.4)' : 'var(--accent)'}`,
                  background:
                    source === 'fixture'
                      ? 'rgba(245, 158, 11, 0.1)'
                      : 'rgba(14, 165, 233, 0.08)',
                  fontFamily: "'JetBrains Mono', monospace",
                }}
                data-testid="skills-source-pill"
              >
                <span
                  aria-hidden="true"
                  className="w-1.5 h-1.5 rounded-full"
                  style={{
                    backgroundColor: source === 'fixture' ? '#fbbf24' : 'var(--accent)',
                    boxShadow:
                      source === 'fixture'
                        ? 'none'
                        : '0 0 6px var(--accent-glow)',
                  }}
                />
                {source === 'fixture' ? COPY.fixtureBadge : COPY.liveBadge}
              </span>
            )}
          </div>
          <p className="text-sm leading-snug" style={{ color: 'var(--text-secondary)' }}>
            {COPY.subtitle}
          </p>
          {builtAtLabel && (
            <p
              className="text-[11px]"
              style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
            >
              {COPY.manifestBuiltAt}: {builtAtLabel}
            </p>
          )}
        </header>

        {/* ── Filters ── */}
        {!loading && !error && manifest && (
          <section
            className="rounded-2xl border p-4 flex flex-col gap-3"
            style={{
              backgroundColor: 'var(--bg-surface)',
              borderColor: 'var(--border-dim)',
            }}
          >
            <input
              type="text"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder={COPY.searchPlaceholder}
              aria-label={COPY.searchPlaceholder}
              data-testid="skills-search"
              className="w-full px-3 py-2 rounded-lg text-sm outline-none transition-colors"
              style={{
                backgroundColor: 'var(--bg-input)',
                color: 'var(--text-primary)',
                border: '1px solid var(--border-dim)',
                fontFamily: "'DM Sans', system-ui, sans-serif",
              }}
            />

            <div className="flex items-center justify-between flex-wrap gap-3">
              <div className="flex items-center gap-1.5 flex-wrap" role="radiogroup" aria-label={COPY.origin}>
                <span
                  className="text-[10px] font-semibold uppercase tracking-widest mr-1"
                  style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
                >
                  {COPY.origin}
                </span>
                {(Object.keys(ORIGIN_FILTER_LABEL) as (keyof typeof ORIGIN_FILTER_LABEL)[]).map(
                  (key) => {
                    const active = originFilter === key;
                    return (
                      <button
                        key={key}
                        type="button"
                        role="radio"
                        aria-checked={active}
                        data-testid={`skills-origin-${key}`}
                        onClick={() => setOriginFilter(key)}
                        className="px-2.5 py-1 rounded-full text-[11px] font-medium transition-all"
                        style={{
                          fontFamily: "'JetBrains Mono', monospace",
                          backgroundColor: active
                            ? 'rgba(14, 165, 233, 0.12)'
                            : 'transparent',
                          color: active ? 'var(--accent)' : 'var(--text-secondary)',
                          border: `1px solid ${active ? 'var(--accent)' : 'var(--border-dim)'}`,
                          cursor: 'pointer',
                        }}
                      >
                        {ORIGIN_FILTER_LABEL[key]}
                      </button>
                    );
                  },
                )}
              </div>

              <label
                className="inline-flex items-center gap-2 text-[11px] cursor-pointer"
                style={{ color: 'var(--text-secondary)' }}
              >
                <input
                  type="checkbox"
                  data-testid="skills-show-deprecated"
                  checked={showDeprecated}
                  onChange={(e) => setShowDeprecated(e.target.checked)}
                  style={{ accentColor: 'var(--accent)' }}
                />
                {COPY.showDeprecated}
              </label>
            </div>
          </section>
        )}

        {/* ── Loading ── */}
        {loading && (
          <div
            className="flex items-center justify-center py-12"
            data-testid="skills-loading"
          >
            <div className="flex items-center gap-2">
              <span className="thinking-dot" />
              <span className="thinking-dot" />
              <span className="thinking-dot" />
              <span
                className="ml-2 text-xs"
                style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
              >
                {COPY.loading}
              </span>
            </div>
          </div>
        )}

        {/* ── Error ── */}
        {!loading && error && (
          <div
            role="alert"
            className="rounded-2xl border px-4 py-3 text-sm"
            style={{
              borderColor: 'rgba(239, 68, 68, 0.3)',
              backgroundColor: 'rgba(239, 68, 68, 0.08)',
              color: '#fca5a5',
            }}
          >
            {error}
          </div>
        )}

        {/* ── Sections ── */}
        {!loading && !error && manifest && (
          <div className="flex flex-col gap-4" data-testid="skills-sections">
            {SECTION_ORDER.map((key) => {
              const entries = filtered[key] ?? [];
              const isCollapsed = collapsed[key];
              return (
                <section
                  key={key}
                  data-testid={`skills-section-${key}`}
                  className="rounded-2xl border overflow-hidden"
                  style={{
                    backgroundColor: 'var(--bg-surface)',
                    borderColor: 'var(--border-dim)',
                  }}
                >
                  <button
                    type="button"
                    onClick={() => toggleSection(key)}
                    aria-expanded={!isCollapsed}
                    className="w-full flex items-center justify-between gap-3 px-4 py-3 transition-colors"
                    style={{
                      backgroundColor: 'transparent',
                      cursor: 'pointer',
                      borderBottom: isCollapsed ? 'none' : '1px solid var(--border-dim)',
                    }}
                  >
                    <div className="flex items-center gap-2">
                      <span
                        aria-hidden="true"
                        className="inline-block transition-transform duration-150"
                        style={{
                          color: 'var(--text-muted)',
                          transform: isCollapsed ? 'rotate(0deg)' : 'rotate(90deg)',
                          fontFamily: "'JetBrains Mono', monospace",
                        }}
                      >
                        {'\u25B8'}
                      </span>
                      <h2
                        className="text-sm font-semibold"
                        style={{
                          color: 'var(--text-primary)',
                          fontFamily: "'JetBrains Mono', monospace",
                        }}
                      >
                        {SECTION_COPY[key].label}
                      </h2>
                      <span
                        className="inline-flex items-center px-1.5 py-0.5 rounded text-[10px]"
                        style={{
                          backgroundColor: 'rgba(14, 165, 233, 0.1)',
                          color: 'var(--accent)',
                          fontFamily: "'JetBrains Mono', monospace",
                        }}
                      >
                        {entries.length}
                      </span>
                    </div>
                  </button>

                  {!isCollapsed && (
                    <div className="flex flex-col divide-y" style={{ borderColor: 'var(--border-dim)' }}>
                      {entries.length === 0 ? (
                        <div className="px-4 py-5 flex flex-col gap-1">
                          <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
                            {SECTION_COPY[key].empty}
                          </p>
                          <p className="text-[11px]" style={{ color: 'var(--text-muted)' }}>
                            {SECTION_COPY[key].empty_hint}
                          </p>
                        </div>
                      ) : (
                        entries.map((skill) => (
                          <SkillRow key={skill.id} skill={skill} />
                        ))
                      )}
                    </div>
                  )}
                </section>
              );
            })}

            {totalVisible === 0 && (
              <p
                role="status"
                data-testid="skills-no-matches"
                className="text-sm text-center py-4"
                style={{ color: 'var(--text-muted)' }}
              >
                {COPY.noMatches}
              </p>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

// ── SkillRow ───────────────────────────────────────────────────────────────

interface SkillRowProps {
  skill: Skill;
}

function SkillRow({ skill }: SkillRowProps) {
  const originLabel = ORIGIN_COPY[skill.origin] ?? skill.origin;
  const isBuiltin = skill.origin === 'builtin';
  const pillTone = isBuiltin
    ? {
        bg: 'rgba(14, 165, 233, 0.1)',
        color: 'var(--accent)',
        border: 'rgba(14, 165, 233, 0.35)',
      }
    : skill.origin === 'agent-authored'
      ? {
          bg: 'rgba(167, 139, 250, 0.12)',
          color: '#c4b5fd',
          border: 'rgba(167, 139, 250, 0.4)',
        }
      : {
          bg: 'rgba(52, 211, 153, 0.1)',
          color: '#6ee7b7',
          border: 'rgba(52, 211, 153, 0.35)',
        };

  return (
    <div
      className="px-4 py-3 flex flex-col gap-1"
      data-testid={`skill-row-${skill.id}`}
      data-deprecated={skill.deprecated ? 'true' : 'false'}
    >
      <div className="flex items-center flex-wrap gap-2 min-w-0">
        <span
          className="text-sm font-medium"
          style={{
            color: 'var(--text-primary)',
            fontFamily: "'JetBrains Mono', monospace",
            textDecoration: skill.deprecated ? 'line-through' : 'none',
            opacity: skill.deprecated ? 0.65 : 1,
          }}
        >
          {skill.name}
        </span>
        {skill.version && (
          <span
            className="text-[10px]"
            style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
          >
            v{skill.version}
          </span>
        )}
        <span
          className="inline-block px-1.5 py-0.5 rounded-full text-[9px] font-semibold uppercase tracking-widest"
          style={{
            backgroundColor: pillTone.bg,
            color: pillTone.color,
            border: `1px solid ${pillTone.border}`,
            fontFamily: "'JetBrains Mono', monospace",
          }}
        >
          {originLabel}
        </span>
        {skill.deprecated && (
          <span
            className="inline-block px-1.5 py-0.5 rounded-full text-[9px] font-semibold uppercase tracking-widest"
            style={{
              backgroundColor: 'rgba(244, 63, 94, 0.1)',
              color: '#fb7185',
              border: '1px solid rgba(244, 63, 94, 0.4)',
              fontFamily: "'JetBrains Mono', monospace",
            }}
          >
            {COPY.deprecatedPill}
          </span>
        )}
      </div>
      <p
        className="text-xs leading-snug"
        style={{
          color: 'var(--text-secondary)',
          opacity: skill.deprecated ? 0.7 : 1,
        }}
      >
        {skill.description}
      </p>
      {skill.provenance_summary && (
        <p
          className="text-[10px]"
          style={{
            color: 'var(--text-muted)',
            fontFamily: "'JetBrains Mono', monospace",
          }}
        >
          {skill.provenance_summary}
        </p>
      )}
    </div>
  );
}
