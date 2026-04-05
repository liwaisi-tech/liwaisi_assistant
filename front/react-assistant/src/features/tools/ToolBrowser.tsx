import { useState, useEffect, useMemo, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { loadNamespace } from '../../i18n/loadNamespace';
import { listTools } from '../../services/api';
import type { ToolSummary } from '../../types/personality';

export function ToolBrowser() {
  const { t } = useTranslation('tools');
  const [tools, setTools] = useState<ToolSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [search, setSearch] = useState('');

  useEffect(() => { loadNamespace('tools'); }, []);

  const loadTools = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await listTools();
      setTools(response.tools);
    } catch (err) {
      setError(err instanceof Error ? err.message : t('browser.failedToLoad'));
    } finally {
      setLoading(false);
    }
  }, [t]);

  useEffect(() => {
    loadTools();
  }, [loadTools]);

  const filtered = useMemo(() => {
    const q = search.toLowerCase().trim();
    if (!q) return tools;
    return tools.filter(
      (t) =>
        t.name.toLowerCase().includes(q) ||
        t.namespace.toLowerCase().includes(q) ||
        t.description.toLowerCase().includes(q),
    );
  }, [tools, search]);

  const grouped = useMemo(() => {
    const map = new Map<string, ToolSummary[]>();
    for (const tool of filtered) {
      const list = map.get(tool.namespace) ?? [];
      list.push(tool);
      map.set(tool.namespace, list);
    }
    // Sort namespaces alphabetically
    return Array.from(map.entries()).sort(([a], [b]) => a.localeCompare(b));
  }, [filtered]);

  if (loading) {
    return (
      <div className="flex-1 flex items-center justify-center p-8">
        <div className="flex flex-col items-center gap-3">
          <div className="flex gap-1.5">
            <span className="thinking-dot" />
            <span className="thinking-dot" />
            <span className="thinking-dot" />
          </div>
          <span
            className="text-xs"
            style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
          >
            {t('browser.loading')}
          </span>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex-1 flex items-center justify-center p-8">
        <div className="flex flex-col items-center gap-3 text-center">
          <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="#ef4444" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="12" cy="12" r="10" />
            <line x1="12" y1="8" x2="12" y2="12" />
            <line x1="12" y1="16" x2="12.01" y2="16" />
          </svg>
          <span className="text-xs" style={{ color: '#ef4444' }}>{error}</span>
          <button
            onClick={loadTools}
            className="px-3 py-1.5 rounded-lg text-[11px] font-medium transition-colors"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              backgroundColor: 'rgba(14, 165, 233, 0.15)',
              color: 'var(--accent)',
            }}
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex-1 flex flex-col min-h-0">
      {/* Header + search */}
      <div className="p-4 md:p-6 pb-0">
        <div className="max-w-2xl mx-auto w-full">
          <h2
            className="text-base font-semibold mb-3"
            style={{ color: 'var(--text-primary)', fontFamily: "'JetBrains Mono', monospace" }}
          >
            {t('browser.title')}
          </h2>

          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t('browser.searchPlaceholder')}
            className="w-full px-3 py-2 rounded-lg text-sm outline-none transition-colors mb-4"
            style={{
              backgroundColor: 'var(--bg-input)',
              color: 'var(--text-primary)',
              border: '1px solid var(--border-dim)',
            }}
            onFocus={(e) => { e.currentTarget.style.borderColor = 'var(--border-glow)'; }}
            onBlur={(e) => { e.currentTarget.style.borderColor = 'var(--border-dim)'; }}
          />

          <span className="text-[10px]" style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}>
            {t('browser.toolsFound', { count: filtered.length })}
          </span>
        </div>
      </div>

      {/* Tool list */}
      <div className="flex-1 overflow-y-auto chat-scroll p-4 md:p-6 pt-3">
        <div className="max-w-2xl mx-auto w-full space-y-5">
          {grouped.length === 0 && (
            <p className="text-sm text-center py-8" style={{ color: 'var(--text-muted)' }}>
              {t('browser.noToolsFound')}
            </p>
          )}

          {grouped.map(([namespace, nsTools]) => (
            <div key={namespace}>
              {/* Namespace header */}
              <div className="flex items-center gap-2 mb-2">
                <span
                  className="text-[10px] font-semibold uppercase tracking-wider"
                  style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
                >
                  {namespace}
                </span>
                <span
                  className="px-1.5 py-0.5 rounded text-[9px]"
                  style={{
                    backgroundColor: 'rgba(255,255,255,0.05)',
                    color: 'var(--text-muted)',
                    fontFamily: "'JetBrains Mono', monospace",
                  }}
                >
                  {nsTools.length}
                </span>
              </div>

              {/* Tool cards */}
              <div className="space-y-2">
                {nsTools.map((tool) => (
                  <div
                    key={`${tool.namespace}-${tool.name}`}
                    className="rounded-lg border p-3 transition-colors duration-150"
                    style={{
                      backgroundColor: 'var(--bg-surface)',
                      borderColor: 'var(--border-dim)',
                    }}
                    onMouseEnter={(e) => { e.currentTarget.style.borderColor = 'rgba(14, 165, 233, 0.3)'; }}
                    onMouseLeave={(e) => { e.currentTarget.style.borderColor = 'var(--border-dim)'; }}
                  >
                    <div className="flex items-start justify-between gap-2">
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2 flex-wrap">
                          <span
                            className="text-xs font-medium"
                            style={{ color: 'var(--text-primary)', fontFamily: "'JetBrains Mono', monospace" }}
                          >
                            {tool.name}
                          </span>

                          {/* Namespace badge */}
                          <span
                            className="px-1.5 py-0.5 rounded text-[9px]"
                            style={{
                              backgroundColor: 'rgba(14, 165, 233, 0.1)',
                              color: 'var(--accent)',
                              fontFamily: "'JetBrains Mono', monospace",
                            }}
                          >
                            {tool.namespace}
                          </span>

                          {/* HITL badge */}
                          {tool.requires_hitl && (
                            <span
                              className="px-1.5 py-0.5 rounded text-[9px]"
                              style={{
                                backgroundColor: 'rgba(245, 158, 11, 0.15)',
                                color: '#fbbf24',
                                fontFamily: "'JetBrains Mono', monospace",
                              }}
                            >
                              {t('browser.hitlBadge')}
                            </span>
                          )}

                          {/* Version */}
                          <span
                            className="text-[9px]"
                            style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
                          >
                            v{tool.version}
                          </span>
                        </div>

                        <p className="text-[11px] mt-1" style={{ color: 'var(--text-secondary)' }}>
                          {tool.description}
                        </p>
                      </div>

                      {/* Color dots (input/output) */}
                      <div className="flex items-center gap-1 shrink-0 mt-1">
                        <span
                          className="w-2.5 h-2.5 rounded-full"
                          style={{ backgroundColor: tool.input_color }}
                          title={t('browser.inputColorTitle', { color: tool.input_color })}
                        />
                        <svg width="8" height="8" viewBox="0 0 24 24" fill="none" stroke="var(--text-muted)" strokeWidth="2">
                          <polyline points="9 18 15 12 9 6" />
                        </svg>
                        <span
                          className="w-2.5 h-2.5 rounded-full"
                          style={{ backgroundColor: tool.output_color }}
                          title={t('browser.outputColorTitle', { color: tool.output_color })}
                        />
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
