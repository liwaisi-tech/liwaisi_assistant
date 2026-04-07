import { useState, useEffect, useCallback, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { loadNamespace } from '../../i18n/loadNamespace';
import { getAdminConfig, setAdminConfig, deleteAdminConfig, AdminApiError } from '../../services/api';
import type { ConfigItem } from '../../types/admin';
import { SecretField } from './SecretField';
import { useAuth } from '../../contexts/AuthContext';

const CATEGORY_ORDER = ['llm', 'auth', 'server', 'models'];

const categoryLabelKeys: Record<string, string> = {
  llm: 'categories.llm',
  auth: 'categories.auth',
  server: 'categories.server',
  models: 'categories.models',
};

export function AdminSecretsPanel() {
  const { t } = useTranslation('admin');
  const auth = useAuth();
  const [items, setItems] = useState<ConfigItem[]>([]);
  const [setupRequired, setSetupRequired] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => { loadNamespace('admin'); }, []);

  const fetchConfig = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await getAdminConfig();
      setItems(data.items);
      setSetupRequired(data.setup_required);
    } catch (e) {
      if (e instanceof AdminApiError) {
        if (e.status === 401) {
          auth.logout();
          return;
        }
        if (e.status === 403) {
          setError(t('errors.forbidden'));
          return;
        }
        if (e.status === 503) {
          setError(t('errors.unavailable'));
          return;
        }
      }
      setError(t('errors.generic'));
    } finally {
      setLoading(false);
    }
  }, [t, auth]);

  useEffect(() => { fetchConfig(); }, [fetchConfig]);

  const handleSave = useCallback(async (key: string, value: string) => {
    await setAdminConfig(key, value);
    await fetchConfig();
  }, [fetchConfig]);

  const handleDelete = useCallback(async (key: string) => {
    await deleteAdminConfig(key);
    await fetchConfig();
  }, [fetchConfig]);

  const grouped = useMemo(() => {
    const map = new Map<string, ConfigItem[]>();
    for (const item of items) {
      const cat = item.category.toLowerCase();
      if (!map.has(cat)) map.set(cat, []);
      map.get(cat)!.push(item);
    }
    // Sort categories by defined order, then remaining alphabetically
    const sorted: [string, ConfigItem[]][] = [];
    for (const cat of CATEGORY_ORDER) {
      const group = map.get(cat);
      if (group) {
        sorted.push([cat, group]);
        map.delete(cat);
      }
    }
    for (const [cat, group] of map.entries()) {
      sorted.push([cat, group]);
    }
    return sorted;
  }, [items]);

  // ── Loading state ──────────────────────────────────────────────────────
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
            {t('loading')}
          </span>
        </div>
      </div>
    );
  }

  // ── Error state ────────────────────────────────────────────────────────
  if (error && items.length === 0) {
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
            onClick={fetchConfig}
            className="px-3 py-1.5 rounded-lg text-[11px] font-medium transition-colors"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              backgroundColor: 'rgba(14, 165, 233, 0.15)',
              color: 'var(--accent)',
            }}
          >
            {t('retry')}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex-1 flex flex-col min-h-0 overflow-y-auto chat-scroll">
      <div className="p-4 md:p-6 max-w-2xl mx-auto w-full space-y-5">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div>
            <h2
              className="text-base font-semibold"
              style={{ color: 'var(--text-primary)', fontFamily: "'JetBrains Mono', monospace" }}
            >
              {t('title')}
            </h2>
            <p className="text-[11px] mt-0.5" style={{ color: 'var(--text-muted)' }}>
              {t('subtitle')}
            </p>
          </div>
          <button
            onClick={fetchConfig}
            className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-[11px] font-medium transition-colors"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              color: 'var(--text-muted)',
              border: '1px solid var(--border-dim)',
            }}
            onMouseEnter={(e) => { e.currentTarget.style.borderColor = 'var(--accent)'; e.currentTarget.style.color = 'var(--accent)'; }}
            onMouseLeave={(e) => { e.currentTarget.style.borderColor = 'var(--border-dim)'; e.currentTarget.style.color = 'var(--text-muted)'; }}
          >
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <polyline points="1 4 1 10 7 10" /><path d="M3.51 15a9 9 0 102.13-9.36L1 10" />
            </svg>
            {t('refresh')}
          </button>
        </div>

        {/* Setup required warning */}
        {setupRequired && (
          <div
            className="px-3 py-2 rounded-lg text-[11px] border"
            style={{
              backgroundColor: 'rgba(245, 158, 11, 0.08)',
              borderColor: 'rgba(245, 158, 11, 0.3)',
              color: '#f59e0b',
            }}
          >
            {t('setupRequired')}
          </div>
        )}

        {/* Error banner (non-fatal) */}
        {error && items.length > 0 && (
          <div
            className="px-3 py-2 rounded-lg text-[11px] border"
            style={{
              backgroundColor: 'rgba(239, 68, 68, 0.08)',
              borderColor: 'rgba(239, 68, 68, 0.3)',
              color: '#ef4444',
            }}
          >
            {error}
          </div>
        )}

        {/* Config sections grouped by category */}
        {grouped.map(([category, categoryItems]) => (
          <div key={category} className="space-y-2">
            <h3
              className="text-[11px] font-semibold uppercase tracking-wider"
              style={{
                fontFamily: "'JetBrains Mono', monospace",
                color: 'var(--text-muted)',
              }}
            >
              {t(categoryLabelKeys[category] ?? category)}
            </h3>
            <div className="space-y-2">
              {categoryItems.map((item) => (
                <SecretField
                  key={item.key}
                  item={item}
                  onSave={handleSave}
                  onDelete={handleDelete}
                />
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
