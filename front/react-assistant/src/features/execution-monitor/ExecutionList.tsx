import { useTranslation } from 'react-i18next';
import type { ExecutionRun } from '../../hooks/useExecutionMonitor';

interface ExecutionListProps {
  runs: ExecutionRun[];
  selectedIndex: number | null;
  onSelect: (index: number) => void;
}

const ROUTE_COLORS: Record<string, string> = {
  't-direct': '#0ea5e9',
  't-plan': '#a855f7',
  't-execute': '#10b981',
};

const ROUTE_KEYS: Record<string, string> = {
  't-direct': 'routeLabels.direct',
  't-plan': 'routeLabels.plan',
  't-execute': 'routeLabels.execute',
};

export function ExecutionList({ runs, selectedIndex, onSelect }: ExecutionListProps) {
  const { t } = useTranslation('monitor');

  if (runs.length === 0) {
    return (
      <div className="h-full flex items-center justify-center px-4">
        <div className="text-center" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
          <div className="text-[10px]" style={{ color: 'var(--text-muted)' }}>
            {t('executionList.noExecutions')}
          </div>
          <div className="text-[9px] mt-1" style={{ color: 'var(--text-muted)' }}>
            {t('executionList.sendToStart')}
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto monitor-scroll" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
      <div className="px-2 py-2 text-[8px] uppercase tracking-widest font-bold" style={{ color: 'var(--text-muted)' }}>
        {t('executionList.title', { count: runs.length })}
      </div>
      <div className="space-y-0.5 px-1 pb-2">
        {runs.map((run, i) => {
          const isSelected = i === selectedIndex;
          const route = detectRoute(run, t);
          const duration = computeDuration(run, t);

          return (
            <button
              key={run.id}
              onClick={() => onSelect(i)}
              className="w-full text-left rounded-lg px-2.5 py-2 transition-all"
              style={{
                backgroundColor: isSelected ? 'rgba(14, 165, 233, 0.1)' : 'transparent',
                border: isSelected ? '1px solid rgba(14, 165, 233, 0.3)' : '1px solid transparent',
                cursor: 'pointer',
              }}
            >
              {/* Run number + route badge */}
              <div className="flex items-center gap-1.5 mb-1">
                <span className="text-[9px] font-bold tabular-nums" style={{ color: 'var(--text-secondary)' }}>
                  #{i + 1}
                </span>
                {route && (
                  <span
                    className="text-[7px] font-bold px-1 py-0.5 rounded uppercase"
                    style={{ backgroundColor: `${route.color}20`, color: route.color }}
                  >
                    {route.label}
                  </span>
                )}
                {run.isRunning && (
                  <div className="w-1.5 h-1.5 rounded-full status-pulse" style={{ backgroundColor: 'var(--accent)' }} />
                )}
              </div>

              {/* Input message preview */}
              <div
                className="text-[10px] leading-tight mb-1 line-clamp-2"
                style={{ color: isSelected ? 'var(--text-primary)' : 'var(--text-secondary)' }}
              >
                {run.inputMessage || t('executionList.loadingInput')}
              </div>

              {/* Stats row */}
              <div className="flex items-center gap-2 text-[8px]" style={{ color: 'var(--text-muted)' }}>
                <span>T:{run.transitionsFired}</span>
                <span>LLM:{run.llmCalls}</span>
                <span style={{ color: 'var(--accent)' }}>${run.totalCostUSD.toFixed(4)}</span>
                {duration && <span>{duration}</span>}
              </div>
            </button>
          );
        })}
      </div>
    </div>
  );
}

function detectRoute(run: ExecutionRun, t: (key: string) => string): { label: string; color: string } | null {
  for (const [id] of run.completedTransitions) {
    if (ROUTE_KEYS[id]) {
      return { label: t(ROUTE_KEYS[id]), color: ROUTE_COLORS[id] };
    }
  }
  return null;
}

function computeDuration(run: ExecutionRun, t: (key: string) => string): string | null {
  if (!run.endTime) return run.isRunning ? t('executionList.runningDuration') : null;
  const ms = new Date(run.endTime).getTime() - new Date(run.startTime).getTime();
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}
