import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { createFlow } from '../../services/api';
import type { CreateFlowRequest, FlowDraftResponse, FlowStrategy } from '../../types/flow';

interface CreateFlowDialogProps {
  onClose: () => void;
  onOpenFlow: (hash: string) => void;
  onStartChat?: (prefill: string) => void;
}

function splitCSV(s: string): string[] {
  return s
    .split(',')
    .map((x) => x.trim().replace(/^#/, ''))
    .filter(Boolean);
}

function strategyLabel(strategy: FlowStrategy, t: (k: string) => string): string {
  switch (strategy) {
    case 'reuse':
      return t('createFlow.strategyReuse');
    case 'compose':
      return t('createFlow.strategyCompose');
    case 'toolforge':
      return t('createFlow.strategyToolForge');
    case 'reject':
      return t('createFlow.strategyReject');
    case 'extend':
      return t('createFlow.strategyExtend');
    default:
      return strategy;
  }
}

function strategyAccent(strategy: FlowStrategy): string {
  switch (strategy) {
    case 'reuse':
    case 'compose':
      return 'var(--accent)';
    case 'toolforge':
      return '#f59e0b';
    case 'reject':
      return '#ef4444';
    default:
      return 'var(--text-muted)';
  }
}

export function CreateFlowDialog({ onClose, onOpenFlow, onStartChat }: CreateFlowDialogProps) {
  const { t } = useTranslation('flows');
  const [intent, setIntent] = useState('');
  const [hashtagsRaw, setHashtagsRaw] = useState('');
  const [capsRaw, setCapsRaw] = useState('');
  const [parallelism, setParallelism] = useState<'' | 'fanout' | 'sequence'>('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [draft, setDraft] = useState<FlowDraftResponse | null>(null);

  const handleSubmit = async () => {
    const trimmedIntent = intent.trim();
    const hashtags = splitCSV(hashtagsRaw);
    const caps = splitCSV(capsRaw);
    if (!trimmedIntent && hashtags.length === 0 && caps.length === 0) {
      setError(t('createFlow.requireInput'));
      return;
    }
    setError(null);
    setSubmitting(true);
    try {
      const req: CreateFlowRequest = {
        intent: trimmedIntent,
        hashtags,
        required_caps: caps,
        parallelism_hint: parallelism,
      };
      const res = await createFlow(req);
      setDraft(res);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center p-4"
      style={{ backgroundColor: 'rgba(0,0,0,0.6)' }}
      onClick={onClose}
    >
      <div
        className="w-full max-w-xl rounded-xl overflow-hidden flex flex-col"
        style={{
          backgroundColor: 'var(--bg-surface)',
          border: '1px solid var(--border-dim)',
          maxHeight: '90vh',
          fontFamily: "'JetBrains Mono', monospace",
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <div
          className="px-6 py-4"
          style={{ borderBottom: '1px solid var(--border-dim)', color: 'var(--text-primary)' }}
        >
          <h2 className="text-base font-semibold">{t('createFlow.title')}</h2>
          <p className="text-xs mt-1 opacity-70" style={{ color: 'var(--text-muted)' }}>
            {t('createFlow.subtitle')}
          </p>
        </div>

        <div className="flex-1 overflow-auto px-6 py-4 space-y-4" style={{ color: 'var(--text-primary)' }}>
          {!draft && (
            <>
              <div>
                <label className="text-xs mb-1 block" style={{ color: 'var(--text-muted)' }}>
                  {t('createFlow.intentLabel')}
                </label>
                <textarea
                  value={intent}
                  onChange={(e) => setIntent(e.target.value)}
                  placeholder={t('createFlow.intentPlaceholder')}
                  rows={3}
                  className="w-full text-xs p-2 rounded resize-none"
                  style={{
                    backgroundColor: 'var(--bg-base, #0a0a0a)',
                    border: '1px solid var(--border-dim)',
                    color: 'var(--text-primary)',
                    fontFamily: "'JetBrains Mono', monospace",
                  }}
                />
              </div>

              <div>
                <label className="text-xs mb-1 block" style={{ color: 'var(--text-muted)' }}>
                  {t('createFlow.hashtagsLabel')}
                </label>
                <input
                  value={hashtagsRaw}
                  onChange={(e) => setHashtagsRaw(e.target.value)}
                  placeholder={t('createFlow.hashtagsPlaceholder')}
                  className="w-full text-xs p-2 rounded"
                  style={{
                    backgroundColor: 'var(--bg-base, #0a0a0a)',
                    border: '1px solid var(--border-dim)',
                    color: 'var(--text-primary)',
                    fontFamily: "'JetBrains Mono', monospace",
                  }}
                />
              </div>

              <div>
                <label className="text-xs mb-1 block" style={{ color: 'var(--text-muted)' }}>
                  {t('createFlow.capsLabel')}
                </label>
                <input
                  value={capsRaw}
                  onChange={(e) => setCapsRaw(e.target.value)}
                  placeholder={t('createFlow.capsPlaceholder')}
                  className="w-full text-xs p-2 rounded"
                  style={{
                    backgroundColor: 'var(--bg-base, #0a0a0a)',
                    border: '1px solid var(--border-dim)',
                    color: 'var(--text-primary)',
                    fontFamily: "'JetBrains Mono', monospace",
                  }}
                />
              </div>

              <div>
                <label className="text-xs mb-1 block" style={{ color: 'var(--text-muted)' }}>
                  {t('createFlow.parallelismLabel')}
                </label>
                <div className="flex gap-2">
                  {([
                    ['', 'createFlow.parallelismAuto'],
                    ['fanout', 'createFlow.parallelismFanout'],
                    ['sequence', 'createFlow.parallelismSequence'],
                  ] as const).map(([val, key]) => (
                    <button
                      key={val || 'auto'}
                      onClick={() => setParallelism(val)}
                      className="flex-1 text-xs py-1.5 rounded transition-colors"
                      style={{
                        backgroundColor:
                          parallelism === val ? 'rgba(14, 165, 233, 0.15)' : 'transparent',
                        border: '1px solid var(--border-dim)',
                        color: parallelism === val ? 'var(--accent)' : 'var(--text-muted)',
                      }}
                    >
                      {t(key)}
                    </button>
                  ))}
                </div>
              </div>
            </>
          )}

          {error && (
            <div className="text-xs p-2 rounded" style={{ color: '#ef4444', border: '1px solid #ef4444' }}>
              {error}
            </div>
          )}

          {draft && (
            <div className="space-y-4">
              <div
                className="p-3 rounded"
                style={{ border: `1px solid ${strategyAccent(draft.strategy)}` }}
              >
                <div
                  className="text-xs font-semibold uppercase mb-1"
                  style={{ color: strategyAccent(draft.strategy) }}
                >
                  {strategyLabel(draft.strategy, t)}
                </div>
                <div className="text-xs" style={{ color: 'var(--text-primary)' }}>
                  {draft.reason}
                </div>
                {draft.confidence > 0 && (
                  <div className="text-[10px] mt-1" style={{ color: 'var(--text-muted)' }}>
                    {t('createFlow.confidence', { pct: (draft.confidence * 100).toFixed(0) })}
                  </div>
                )}
              </div>

              {draft.matches && draft.matches.length > 0 && (
                <div>
                  <div className="text-xs font-semibold mb-1" style={{ color: 'var(--text-muted)' }}>
                    {t('createFlow.matchesTitle')}
                  </div>
                  <ul className="space-y-1">
                    {draft.matches.slice(0, 8).map((m) => (
                      <li
                        key={m.qualified_name}
                        className="text-[11px] px-2 py-1 rounded flex justify-between"
                        style={{ border: '1px solid var(--border-dim)' }}
                      >
                        <span>{m.qualified_name}</span>
                        <span style={{ color: 'var(--text-muted)' }}>{m.score.toFixed(2)}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              )}

              {draft.missing_caps && draft.missing_caps.length > 0 && (
                <div>
                  <div className="text-xs font-semibold mb-1" style={{ color: '#f59e0b' }}>
                    {t('createFlow.missingCapsTitle')}
                  </div>
                  <div className="flex gap-1 flex-wrap">
                    {draft.missing_caps.map((c) => (
                      <span
                        key={c}
                        className="text-[10px] px-2 py-0.5 rounded"
                        style={{ backgroundColor: 'rgba(245,158,11,0.15)', color: '#f59e0b' }}
                      >
                        {c}
                      </span>
                    ))}
                  </div>
                </div>
              )}

              {draft.candidates && draft.candidates.length > 0 && (
                <div>
                  <div className="text-xs font-semibold mb-1" style={{ color: 'var(--text-muted)' }}>
                    {t('createFlow.candidatesTitle')}
                  </div>
                  <ul className="space-y-1">
                    {draft.candidates.slice(0, 5).map((c) => (
                      <li
                        key={c.hash}
                        className="text-[11px] px-2 py-1 rounded cursor-pointer"
                        style={{ border: '1px solid var(--border-dim)' }}
                        onClick={() => onOpenFlow(c.hash)}
                      >
                        {c.role || c.hash.slice(0, 12)}
                      </li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}
        </div>

        <div
          className="px-6 py-3 flex justify-end gap-2"
          style={{ borderTop: '1px solid var(--border-dim)' }}
        >
          {!draft && (
            <>
              <button
                onClick={onClose}
                className="text-xs px-3 py-1.5 rounded"
                style={{ color: 'var(--text-muted)' }}
              >
                {t('createFlow.cancel')}
              </button>
              <button
                onClick={handleSubmit}
                disabled={submitting}
                className="text-xs px-3 py-1.5 rounded transition-colors"
                style={{
                  backgroundColor: 'rgba(14, 165, 233, 0.15)',
                  border: '1px solid var(--accent)',
                  color: 'var(--accent)',
                  opacity: submitting ? 0.6 : 1,
                }}
              >
                {submitting ? t('createFlow.submitting') : t('createFlow.submit')}
              </button>
            </>
          )}
          {draft && (
            <>
              <button
                onClick={onClose}
                className="text-xs px-3 py-1.5 rounded"
                style={{ color: 'var(--text-muted)' }}
              >
                {t('createFlow.dismiss')}
              </button>
              {draft.strategy === 'reuse' && draft.base_flow_id && (
                <button
                  onClick={() => onOpenFlow(draft.base_flow_id!)}
                  className="text-xs px-3 py-1.5 rounded"
                  style={{
                    backgroundColor: 'rgba(14, 165, 233, 0.15)',
                    border: '1px solid var(--accent)',
                    color: 'var(--accent)',
                  }}
                >
                  {t('createFlow.openFlow')}
                </button>
              )}
              {onStartChat && (draft.strategy === 'compose' || draft.strategy === 'reuse') && (
                <button
                  onClick={() => {
                    onStartChat(intent);
                    onClose();
                  }}
                  className="text-xs px-3 py-1.5 rounded"
                  style={{
                    backgroundColor: 'rgba(14, 165, 233, 0.15)',
                    border: '1px solid var(--accent)',
                    color: 'var(--accent)',
                  }}
                >
                  {t('createFlow.goToChat')}
                </button>
              )}
            </>
          )}
        </div>
      </div>
    </div>
  );
}
