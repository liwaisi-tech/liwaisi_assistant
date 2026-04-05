import { useState, useEffect, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { loadNamespace } from '../../i18n/loadNamespace';
import { getAdminConfig, setAdminConfig, deleteAdminConfig } from '../../services/api';
import { SecretField } from './SecretField';
import type { ConfigItem } from '../../types/admin';

const categoryOrder = ['llm', 'auth', 'server', 'models'];

export function AdminSecretsPanel() {
  const { t } = useTranslation('admin');
  const [items, setItems] = useState<ConfigItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [setupRequired, setSetupRequired] = useState(false);

  useEffect(() => { loadNamespace('admin'); }, []);

  const loadConfig = useCallback(async () => {
    try {
      setLoading(true);
      setError(null);
      const resp = await getAdminConfig();
      setItems(resp.items);
      setSetupRequired(resp.setup_required);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load config');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { loadConfig(); }, [loadConfig]);

  const handleSave = useCallback(async (key: string, value: string) => {
    await setAdminConfig(key, value);
    await loadConfig();
  }, [loadConfig]);

  const handleDelete = useCallback(async (key: string) => {
    await deleteAdminConfig(key);
    await loadConfig();
  }, [loadConfig]);

  // Group items by category
  const grouped = categoryOrder.map((cat) => ({
    category: cat,
    items: items.filter((item) => item.category === cat),
  })).filter((g) => g.items.length > 0);

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
            {t('panel.loading')}
          </span>
        </div>
      </div>
    );
  }

  if (error && items.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center p-8">
        <div className="text-center">
          <p className="text-sm mb-2" style={{ color: 'var(--error)' }}>{error}</p>
          <button
            onClick={loadConfig}
            className="text-xs px-3 py-1.5 rounded"
            style={{ backgroundColor: 'var(--bg-surface)', color: 'var(--text-secondary)', border: '1px solid var(--border-dim)' }}
          >
            {t('panel.retry')}
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="max-w-2xl mx-auto p-6">
        {/* Header */}
        <div className="mb-6">
          <h2
            className="text-base font-semibold mb-1"
            style={{ fontFamily: "'JetBrains Mono', monospace", color: 'var(--text-primary)' }}
          >
            {t('panel.title')}
          </h2>
          <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
            {t('panel.description')}
          </p>
        </div>

        {/* Setup required banner */}
        {setupRequired && (
          <div
            className="mb-4 p-3 rounded-lg text-xs"
            style={{
              backgroundColor: 'rgba(245, 158, 11, 0.1)',
              border: '1px solid rgba(245, 158, 11, 0.3)',
              color: '#f59e0b',
            }}
          >
            {t('panel.setupRequired')}
          </div>
        )}

        {/* Config groups */}
        {grouped.map(({ category, items: catItems }) => (
          <div key={category} className="mb-6">
            <h3
              className="text-[11px] font-medium uppercase tracking-wider mb-3"
              style={{ fontFamily: "'JetBrains Mono', monospace", color: 'var(--text-muted)' }}
            >
              {t(`categories.${category}`)}
            </h3>
            <div className="flex flex-col gap-2">
              {catItems.map((item) => (
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
