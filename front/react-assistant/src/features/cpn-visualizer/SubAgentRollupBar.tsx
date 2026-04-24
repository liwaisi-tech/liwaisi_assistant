import type { ProfileRollup } from '../../hooks/useSubAgentRollup';

const PROFILE_COLORS: Record<string, string> = {
  pm: '#f472b6',
  arch: '#a78bfa',
  qa: '#34d399',
  devops: '#fb923c',
  'ai-eng': '#22d3ee',
  'go-eng': '#60a5fa',
  security: '#f87171',
};

const ICON_GLYPHS: Record<string, string> = {
  'clipboard-check': '📋',
  layers: '🧱',
  'flask-conical': '🧪',
  container: '📦',
  bot: '🤖',
  gopher: '🐹',
  'shield-check': '🛡️',
};

interface SubAgentRollupBarProps {
  byProfile: ProfileRollup[];
  totalCostUSD: number;
  totalFirings: number;
  failedFirings: number;
}

// SubAgentRollupBar renders per-profile pills next to the existing
// DISPAROS / LLM / COSTO / EVENTOS header. Empty when no sub-agent
// firings have been observed — the bar simply does not render.
export function SubAgentRollupBar({ byProfile, totalCostUSD, totalFirings, failedFirings }: SubAgentRollupBarProps) {
  if (byProfile.length === 0) return null;

  return (
    <div className="flex items-center gap-2 flex-wrap">
      <div
        className="text-[9px] font-bold px-2 py-1 rounded uppercase tracking-wider"
        style={{
          backgroundColor: 'rgba(255,255,255,0.05)',
          color: 'var(--text-secondary)',
          fontFamily: "'JetBrains Mono', monospace",
        }}
      >
        sub-agents · {totalFirings}{failedFirings > 0 ? ` · ${failedFirings} failed` : ''} · ${totalCostUSD.toFixed(4)}
      </div>
      {byProfile.map((p) => {
        const color = PROFILE_COLORS[p.profileId] ?? '#94a3b8';
        const glyph = p.iconKey ? ICON_GLYPHS[p.iconKey] ?? '●' : '●';
        return (
          <div
            key={p.profileId}
            className="flex items-center gap-1 px-2 py-1 rounded"
            style={{
              backgroundColor: `${color}15`,
              border: `1px solid ${color}40`,
              fontFamily: "'JetBrains Mono', monospace",
            }}
            title={`${p.profileId}: ${p.firings} firings, ${p.failed} failed, $${p.costUSD.toFixed(4)}, ${p.totalDurationMs}ms total`}
          >
            <span className="text-sm leading-none">{glyph}</span>
            <span className="text-[10px] font-bold" style={{ color }}>
              {p.profileId}
            </span>
            <span className="text-[10px]" style={{ color: 'var(--text-secondary)' }}>
              ×{p.firings}
            </span>
            {p.failed > 0 && (
              <span className="text-[9px] font-bold" style={{ color: '#f87171' }}>
                !{p.failed}
              </span>
            )}
            {p.costUSD > 0 && (
              <span className="text-[9px]" style={{ color: 'var(--text-muted)' }}>
                ${p.costUSD.toFixed(4)}
              </span>
            )}
          </div>
        );
      })}
    </div>
  );
}
