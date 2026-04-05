import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { loadNamespace } from '../../i18n/loadNamespace';
import { getFlows } from '../../services/api';
import type { FlowSummary } from '../../types/flow';

interface FlowBrowserProps {
  onSelectFlow: (hash: string) => void;
}

export function FlowBrowser({ onSelectFlow }: FlowBrowserProps) {
  const { t } = useTranslation('flows');
  const [flows, setFlows] = useState<FlowSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => { loadNamespace('flows'); }, []);

  useEffect(() => {
    getFlows()
      .then((res) => setFlows(res.items))
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false));
  }, []);

  if (loading) {
    return (
      <div className="flex-1 flex items-center justify-center" style={{ color: 'var(--text-muted)' }}>
        <div className="text-center">
          <div className="thinking-dots mb-4"><span /><span /><span /></div>
          <p className="text-sm" style={{ fontFamily: "'JetBrains Mono', monospace" }}>{t('flowBrowser.loading')}</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex-1 flex items-center justify-center" style={{ color: 'var(--text-muted)' }}>
        <p className="text-sm">{t('flowBrowser.errorPrefix', { error })}</p>
      </div>
    );
  }

  if (flows.length === 0) {
    return (
      <div className="flex-1 flex items-center justify-center" style={{ color: 'var(--text-muted)' }}>
        <div className="text-center">
          <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1" className="mx-auto mb-4 opacity-40">
            <circle cx="12" cy="12" r="10" />
            <path d="M8 12h8M12 8v8" />
          </svg>
          <p className="text-sm" style={{ fontFamily: "'JetBrains Mono', monospace" }}>{t('flowBrowser.emptyTitle')}</p>
          <p className="text-xs mt-2 opacity-60">{t('flowBrowser.emptyDescription')}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-auto px-6 py-4">
      <h2
        className="text-lg font-semibold mb-4"
        style={{ fontFamily: "'JetBrains Mono', monospace", color: 'var(--text-primary)' }}
      >
        {t('flowBrowser.title')}
      </h2>

      <div className="space-y-2">
        {flows.map((flow) => (
          <button
            key={flow.hash}
            onClick={() => onSelectFlow(flow.hash)}
            className="w-full text-left p-4 rounded-xl transition-all duration-200 hover:scale-[1.01]"
            style={{
              backgroundColor: 'var(--bg-surface)',
              border: '1px solid var(--border-dim)',
            }}
          >
            <div className="flex items-center justify-between mb-2">
              <span
                className="text-xs font-medium px-2 py-0.5 rounded"
                style={{
                  backgroundColor: 'rgba(14, 165, 233, 0.12)',
                  color: 'var(--accent)',
                  fontFamily: "'JetBrains Mono', monospace",
                }}
              >
                {flow.role.toUpperCase()}
              </span>
              <span className="text-[10px]" style={{ color: 'var(--text-muted)' }}>
                {new Date(flow.created_at).toLocaleDateString()}
              </span>
            </div>

            <div className="text-xs font-mono mb-2" style={{ color: 'var(--text-secondary)' }}>
              {flow.hash.slice(0, 16)}...
            </div>

            <div className="flex gap-4 text-[10px]" style={{ color: 'var(--text-muted)' }}>
              <span>{t('flowBrowser.executions', { count: flow.execution_count })}</span>
              <span>{t('flowBrowser.success', { rate: (flow.success_rate * 100).toFixed(0) })}</span>
              <span>{t('flowBrowser.avgCost', { cost: flow.avg_cost_usd.toFixed(4) })}</span>
              <span>{t('flowBrowser.avgTime', { time: flow.avg_duration_ms })}</span>
            </div>
          </button>
        ))}
      </div>
    </div>
  );
}
