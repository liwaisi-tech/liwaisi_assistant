import { useTranslation } from 'react-i18next';
import type { TransitionStartedPayload, TransitionCompletedPayload, TokenSnapshotData } from '../../types/sse';
import type { CPNTopology } from '../../types/flow';

interface TransitionInspectorProps {
  transitionId: string;
  topology: CPNTopology;
  startedPayload?: TransitionStartedPayload;
  completedPayload?: TransitionCompletedPayload;
  onClose: () => void;
}

const KIND_COLORS: Record<string, string> = {
  llm: '#0ea5e9',
  tool: '#10b981',
  validate: '#a855f7',
  subnet: '#6366f1',
  observer: '#64748b',
  hitl: '#f59e0b',
};

export function TransitionInspector({
  transitionId,
  topology,
  startedPayload,
  completedPayload,
  onClose,
}: TransitionInspectorProps) {
  const { t } = useTranslation('monitor');
  const transition = topology.transitions[transitionId];
  if (!transition) return null;

  const color = KIND_COLORS[transition.kind] || '#64748b';
  const isFiring = !!startedPayload && !completedPayload;
  const hasError = !!completedPayload?.error;

  return (
    <div
      className="inspector-enter h-full flex flex-col monitor-scroll overflow-y-auto"
      style={{
        width: 320,
        backgroundColor: 'var(--bg-surface)',
        borderLeft: '1px solid var(--border-dim)',
        fontFamily: "'JetBrains Mono', monospace",
      }}
    >
      {/* Header */}
      <div className="flex items-center justify-between px-3 py-2.5" style={{ borderBottom: '1px solid var(--border-dim)' }}>
        <div className="flex items-center gap-2">
          <span
            className="text-[9px] font-bold px-1.5 py-0.5 rounded uppercase"
            style={{ backgroundColor: `${color}20`, color }}
          >
            {transition.kind}
          </span>
          <span className="text-[11px] font-semibold" style={{ color: 'var(--text-primary)' }}>
            {transitionId}
          </span>
        </div>
        <button
          onClick={onClose}
          className="text-[14px] w-6 h-6 flex items-center justify-center rounded transition-colors"
          style={{ color: 'var(--text-muted)', background: 'none', border: 'none', cursor: 'pointer' }}
          aria-label={t('transitionInspector.closeAriaLabel')}
        >
          {'\u2715'}
        </button>
      </div>

      {/* Status */}
      {isFiring && (
        <div className="px-3 py-2 flex items-center gap-2" style={{ backgroundColor: `${color}08` }}>
          <div className="w-2 h-2 rounded-full status-pulse" style={{ backgroundColor: color }} />
          <span className="text-[10px] font-semibold" style={{ color }}>{t('transitionInspector.firing')}</span>
        </div>
      )}

      {hasError && (
        <div className="px-3 py-2" style={{ backgroundColor: '#ef444410', borderBottom: '1px solid #ef444430' }}>
          <div className="text-[9px] font-bold mb-1" style={{ color: '#ef4444' }}>{t('transitionInspector.error')}</div>
          <div className="text-[10px] leading-relaxed" style={{ color: '#fca5a5' }}>
            {completedPayload.error}
          </div>
        </div>
      )}

      {/* Cost & Duration */}
      {completedPayload && (
        <div className="px-3 py-2.5 flex gap-4" style={{ borderBottom: '1px solid var(--border-dim)' }}>
          <div>
            <div className="text-[8px] uppercase tracking-wider mb-0.5" style={{ color: 'var(--text-muted)' }}>{t('transitionInspector.cost')}</div>
            <div className="text-sm font-bold" style={{ color: 'var(--accent)' }}>
              ${completedPayload.cost_usd.toFixed(4)}
            </div>
          </div>
          <div>
            <div className="text-[8px] uppercase tracking-wider mb-0.5" style={{ color: 'var(--text-muted)' }}>{t('transitionInspector.duration')}</div>
            <div className="text-sm font-bold" style={{ color: 'var(--text-primary)' }}>
              {completedPayload.duration_ms}ms
            </div>
          </div>
        </div>
      )}

      {/* Input Tokens */}
      {startedPayload && startedPayload.input_tokens.length > 0 && (
        <Section title={t('transitionInspector.inputTokens')} color={color}>
          {startedPayload.input_tokens.map((tok, i) => (
            <TokenCard key={i} token={tok} />
          ))}
        </Section>
      )}

      {/* Output Tokens */}
      {completedPayload && completedPayload.output_tokens.length > 0 && (
        <Section title={t('transitionInspector.outputTokens')} color="#10b981">
          {completedPayload.output_tokens.map((tok, i) => (
            <TokenCard key={i} token={tok} />
          ))}
        </Section>
      )}

      {/* Config */}
      {transition.llmConfig && (
        <Section title={t('transitionInspector.llmConfig')} color="var(--text-muted)">
          <ConfigRow label={t('transitionInspector.model')} value={transition.llmConfig.model || t('transitionInspector.defaultModel')} />
          {transition.llmConfig.maxTokens && <ConfigRow label={t('transitionInspector.maxTokens')} value={String(transition.llmConfig.maxTokens)} />}
          {transition.llmConfig.temperature != null && <ConfigRow label={t('transitionInspector.temperature')} value={String(transition.llmConfig.temperature)} />}
          {transition.llmConfig.streamOutput && <ConfigRow label={t('transitionInspector.stream')} value="true" />}
          {transition.llmConfig.requireJSON && <ConfigRow label={t('transitionInspector.jsonMode')} value="true" />}
        </Section>
      )}

      {/* System Prompt */}
      {transition.systemPrompt && (
        <Section title={t('transitionInspector.systemPrompt')} color="var(--text-muted)">
          <div
            className="text-[10px] leading-relaxed max-h-32 overflow-y-auto monitor-scroll"
            style={{ color: 'var(--text-secondary)' }}
          >
            {transition.systemPrompt}
          </div>
        </Section>
      )}
    </div>
  );
}

function Section({ title, color, children }: { title: string; color: string; children: React.ReactNode }) {
  return (
    <div className="px-3 py-2.5" style={{ borderBottom: '1px solid var(--border-dim)' }}>
      <div className="text-[8px] uppercase tracking-wider font-bold mb-2" style={{ color }}>
        {title}
      </div>
      <div className="space-y-1.5">{children}</div>
    </div>
  );
}

function TokenCard({ token }: { token: TokenSnapshotData }) {
  const COLOR_MAP: Record<string, string> = {
    STRING: '#0ea5e9', JSON: '#a855f7', ARTIFACT: '#10b981', HUMAN: '#f59e0b',
    SCORE: '#ef4444', EVENT: '#6366f1', CPN: '#ec4899', SCHEMA: '#14b8a6', ERROR: '#dc2626',
  };
  const dotColor = COLOR_MAP[token.color] || '#64748b';

  return (
    <div className="rounded-md px-2 py-1.5" style={{ backgroundColor: 'var(--bg-deep)', border: '1px solid var(--border-dim)' }}>
      <div className="flex items-center gap-1.5 mb-1">
        <div className="w-2 h-2 rounded-full" style={{ backgroundColor: dotColor }} />
        <span className="text-[9px] font-semibold" style={{ color: dotColor }}>{token.color}</span>
        <span className="text-[8px]" style={{ color: 'var(--text-muted)' }}>{token.space}</span>
      </div>
      {token.payload_preview && (
        <div
          className="text-[9px] leading-relaxed max-h-16 overflow-y-auto monitor-scroll"
          style={{ color: 'var(--text-secondary)', wordBreak: 'break-all' }}
        >
          {token.payload_preview}
        </div>
      )}
    </div>
  );
}

function ConfigRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between text-[9px]">
      <span style={{ color: 'var(--text-muted)' }}>{label}</span>
      <span style={{ color: 'var(--text-primary)' }}>{value}</span>
    </div>
  );
}
