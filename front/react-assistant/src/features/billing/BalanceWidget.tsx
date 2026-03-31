import { useBalance } from '../../hooks/useBalance';

export function BalanceWidget() {
  const { balance, isLoading, error } = useBalance();

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 px-3 py-1.5 rounded-lg" style={{ backgroundColor: 'var(--bg-input)' }}>
        <div className="w-16 h-3 rounded animate-pulse" style={{ backgroundColor: 'var(--border-dim)' }} />
        <div className="w-8 h-3 rounded animate-pulse" style={{ backgroundColor: 'var(--border-dim)' }} />
      </div>
    );
  }

  if (error || !balance) {
    return (
      <div
        className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-[10px]"
        style={{ backgroundColor: 'var(--bg-input)', color: 'var(--text-muted)' }}
        title={error ?? 'Balance unavailable'}
      >
        <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
          <circle cx="8" cy="8" r="7" />
          <path d="M8 5v3M8 11h.01" />
        </svg>
        <span>--</span>
      </div>
    );
  }

  const hasLimit = balance.limit_remaining !== null;
  const remaining = balance.limit_remaining ?? 0;
  const usagePercent = hasLimit && (remaining + balance.usage) > 0
    ? (balance.usage / (remaining + balance.usage)) * 100
    : 0;

  return (
    <div
      className="flex items-center gap-2.5 px-3 py-1.5 rounded-lg"
      style={{ backgroundColor: 'var(--bg-input)', border: '1px solid var(--border-dim)' }}
      title={`Total usage: $${balance.usage.toFixed(2)} | Daily: $${balance.usage_daily.toFixed(2)} | Weekly: $${balance.usage_weekly.toFixed(2)}`}
    >
      {balance.is_free_tier && (
        <span
          className="text-[9px] font-semibold uppercase tracking-wider px-1.5 py-0.5 rounded"
          style={{
            backgroundColor: 'rgba(14, 165, 233, 0.15)',
            color: 'var(--accent)',
            fontFamily: "'JetBrains Mono', monospace",
          }}
        >
          Free
        </span>
      )}

      <div className="flex flex-col gap-0.5">
        <div className="flex items-baseline gap-1.5">
          <span
            className="text-xs font-semibold tabular-nums"
            style={{ fontFamily: "'JetBrains Mono', monospace", color: 'var(--text-primary)' }}
          >
            {hasLimit ? `$${remaining.toFixed(2)}` : `$${balance.usage.toFixed(2)}`}
          </span>
          <span className="text-[9px]" style={{ color: 'var(--text-muted)' }}>
            {hasLimit ? 'left' : 'used'}
          </span>
        </div>

        {hasLimit && (
          <div className="w-16 h-1 rounded-full overflow-hidden" style={{ backgroundColor: 'var(--border-dim)' }}>
            <div
              className="h-full rounded-full transition-all duration-500"
              style={{
                width: `${Math.min(usagePercent, 100)}%`,
                backgroundColor: usagePercent > 80 ? '#ef4444' : usagePercent > 50 ? '#f59e0b' : 'var(--accent)',
              }}
            />
          </div>
        )}
      </div>

      <div className="flex flex-col items-end">
        <span
          className="text-[9px] tabular-nums"
          style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
        >
          ${balance.usage_daily.toFixed(2)}/d
        </span>
      </div>
    </div>
  );
}
