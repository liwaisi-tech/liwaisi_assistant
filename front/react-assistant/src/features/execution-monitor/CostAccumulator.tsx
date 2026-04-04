interface CostAccumulatorProps {
  totalCostUSD: number;
  llmCalls: number;
  transitionsFired: number;
}

export function CostAccumulator({ totalCostUSD, llmCalls, transitionsFired }: CostAccumulatorProps) {
  return (
    <div
      className="absolute top-3 right-3 z-20 rounded-lg px-3 py-2"
      style={{
        backgroundColor: 'rgba(10, 10, 15, 0.88)',
        border: '1px solid var(--border-dim)',
        backdropFilter: 'blur(12px)',
        fontFamily: "'JetBrains Mono', monospace",
        minWidth: 140,
      }}
    >
      <div className="text-[8px] uppercase tracking-widest mb-1.5" style={{ color: 'var(--text-muted)' }}>
        Execution Cost
      </div>
      <div className="text-base font-bold tabular-nums" style={{ color: 'var(--accent)' }}>
        ${totalCostUSD.toFixed(4)}
      </div>
      <div className="flex gap-3 mt-1.5">
        <div className="text-[9px]" style={{ color: 'var(--text-secondary)' }}>
          <span style={{ color: 'var(--text-muted)' }}>LLM</span>{' '}
          <span className="font-semibold">{llmCalls}</span>
        </div>
        <div className="text-[9px]" style={{ color: 'var(--text-secondary)' }}>
          <span style={{ color: 'var(--text-muted)' }}>T</span>{' '}
          <span className="font-semibold">{transitionsFired}</span>
        </div>
      </div>
    </div>
  );
}
