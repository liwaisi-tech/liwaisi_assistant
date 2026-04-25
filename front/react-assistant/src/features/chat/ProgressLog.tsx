import type { ProgressStep } from '../../hooks/useChat';

interface ProgressLogProps {
  steps: ProgressStep[];
}

// ProgressLog renders a vertical, persistent stack of agent progress
// steps for the active turn. Complements ActivityBubble: the bubble shows
// the latest verb transiently, this list shows the full trail of what
// the agent has done since the user's last message. Cleared by useChat
// on the next USER_MESSAGE / RESET.
//
// Visual: a small left-aligned card list, one row per transition. Running
// steps animate a pulse; completed steps render a check + duration.
export function ProgressLog({ steps }: ProgressLogProps) {
  if (steps.length === 0) return null;

  return (
    <div
      data-testid="progress-log"
      className="rounded-lg px-3 py-2 text-xs space-y-1"
      style={{
        backgroundColor: 'var(--bg-surface)',
        border: '1px solid var(--border-dim)',
        color: 'var(--text-secondary)',
        fontFamily: "'JetBrains Mono', monospace",
      }}
    >
      {steps.map((s) => (
        <ProgressRow key={s.transitionId} step={s} />
      ))}
    </div>
  );
}

function ProgressRow({ step }: { step: ProgressStep }) {
  const isDone = step.status === 'done';
  const glyph = isDone ? '✓' : '⋯';
  const duration = isDone && step.durationMs != null ? formatDuration(step.durationMs) : null;
  return (
    <div className="flex items-center gap-2">
      <span
        aria-hidden="true"
        className={isDone ? '' : 'activity-pulse'}
        style={{
          display: 'inline-block',
          width: 12,
          textAlign: 'center',
          color: isDone ? 'var(--accent)' : 'var(--text-secondary)',
        }}
      >
        {glyph}
      </span>
      <span style={{ color: isDone ? 'var(--text-primary)' : 'var(--text-secondary)' }}>
        {step.verb}
      </span>
      {step.detail && (
        <span style={{ opacity: 0.7 }}>· {step.detail}</span>
      )}
      {duration && (
        <span style={{ marginLeft: 'auto', opacity: 0.6 }}>{duration}</span>
      )}
    </div>
  );
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  const rs = Math.round(s - m * 60);
  return `${m}m${rs}s`;
}
