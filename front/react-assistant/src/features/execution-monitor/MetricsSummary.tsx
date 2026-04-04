interface MetricsSummaryProps {
  transitionsFired: number;
  llmCalls: number;
  totalCostUSD: number;
  eventCount: number;
}

export function MetricsSummary({ transitionsFired, llmCalls, totalCostUSD, eventCount }: MetricsSummaryProps) {
  return (
    <div className="flex items-center gap-4" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
      <Metric label="FIRED" value={transitionsFired} />
      <Metric label="LLM" value={llmCalls} />
      <Metric label="COST" value={`$${totalCostUSD.toFixed(4)}`} accent />
      <Metric label="EVENTS" value={eventCount} />
    </div>
  );
}

function Metric({ label, value, accent }: { label: string; value: string | number; accent?: boolean }) {
  return (
    <div className="flex items-baseline gap-1.5">
      <span className="text-[8px] uppercase tracking-wider" style={{ color: 'var(--text-muted)' }}>
        {label}
      </span>
      <span
        className="text-[11px] font-bold tabular-nums"
        style={{ color: accent ? 'var(--accent)' : 'var(--text-primary)' }}
      >
        {value}
      </span>
    </div>
  );
}
