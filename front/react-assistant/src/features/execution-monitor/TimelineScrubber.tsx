import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { MonitorEvent } from '../../hooks/useExecutionMonitor';

interface TimelineScrubberProps {
  events: MonitorEvent[];
}

const EVENT_COLORS: Record<string, string> = {
  transition_started: '#0ea5e9',
  transition_completed: '#10b981',
  transition_fired: '#64748b',
  subnet_started: '#6366f1',
  subnet_completed: '#6366f1',
  subnet_failed: '#ef4444',
  hitl_requested: '#f59e0b',
  hitl_resolved: '#f59e0b',
};

export function TimelineScrubber({ events }: TimelineScrubberProps) {
  const { t } = useTranslation('monitor');

  const { positions, timeRange } = useMemo(() => {
    if (events.length === 0) return { positions: [], timeRange: '' };

    const timestamps = events.map((e) => new Date(e.timestamp).getTime());
    const minT = Math.min(...timestamps);
    const maxT = Math.max(...timestamps);
    const range = maxT - minT || 1;

    const pos = events.map((e, i) => ({
      event: e,
      pct: ((timestamps[i] - minT) / range) * 100,
      color: EVENT_COLORS[e.type] || '#64748b',
    }));

    const duration = ((maxT - minT) / 1000).toFixed(1);
    return { positions: pos, timeRange: `${duration}s` };
  }, [events]);

  if (events.length === 0) {
    return (
      <div
        className="h-full flex items-center justify-center text-[9px]"
        style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
      >
        {t('timelineScrubber.waiting')}
      </div>
    );
  }

  return (
    <div className="h-full flex items-center px-4 gap-3">
      <span
        className="text-[8px] uppercase tracking-wider shrink-0"
        style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
      >
        {t('timelineScrubber.label')}
      </span>

      <div className="relative flex-1 h-2 rounded-full" style={{ backgroundColor: 'var(--bg-input)' }}>
        {positions.map((p) => (
          <div
            key={p.event.id}
            className="timeline-dot absolute top-1/2 -translate-y-1/2 w-2 h-2 rounded-full cursor-default"
            style={{
              left: `${p.pct}%`,
              backgroundColor: p.color,
              boxShadow: `0 0 6px ${p.color}60`,
            }}
            title={`${p.event.type} | ${p.event.transitionId} | ${new Date(p.event.timestamp).toISOString().slice(11, 23)}`}
          />
        ))}
      </div>

      <span
        className="text-[9px] tabular-nums shrink-0"
        style={{ color: 'var(--text-secondary)', fontFamily: "'JetBrains Mono', monospace" }}
      >
        {timeRange}
      </span>
    </div>
  );
}
