import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import type { TransitionTopology } from '../../../types/flow';
import { subAgentMeta } from '../../../types/flow';

// Map BE icon_key to a glyph (emoji) since the project doesn't pull in
// an icon library. Falls back to a generic dot for unknown profiles so
// new profiles render without an FE-side change.
const ICON_GLYPHS: Record<string, string> = {
  'clipboard-check': '📋', // PM
  'layers': '🧱',           // arch
  'flask-conical': '🧪',    // qa
  'container': '📦',        // devops
  'bot': '🤖',              // ai-eng
  'gopher': '🐹',            // go-eng (hamster — closest emoji)
  'shield-check': '🛡️', // security
};

// Per-profile color so the visualizer renders sub-agents distinct from
// plain LLM transitions (which use a single sky-blue). Profiles share
// a palette family so the eye groups them at a glance.
const PROFILE_COLORS: Record<string, string> = {
  pm: '#f472b6',
  arch: '#a78bfa',
  qa: '#34d399',
  devops: '#fb923c',
  'ai-eng': '#22d3ee',
  'go-eng': '#60a5fa',
  security: '#f87171',
};

interface SubAgentNodeData {
  transition: TransitionTopology;
  fired?: boolean;
  executionOrder?: number;
  direction?: 'LR' | 'TB';
  onSelect?: (transition: TransitionTopology) => void;
}

export const SubAgentNode = memo(function SubAgentNode({ data }: { data: SubAgentNodeData }) {
  const { transition, fired, executionOrder, direction = 'LR', onSelect } = data;
  const meta = subAgentMeta(transition);
  // Defensive: caller is expected to pre-filter on subAgentMeta != null,
  // but if a non-sub-agent slips through, render nothing rather than
  // misrepresent the transition.
  if (!meta) return null;

  const color = PROFILE_COLORS[meta.profileId] ?? '#94a3b8';
  const glyph = ICON_GLYPHS[meta.iconKey] ?? '●';
  const isHorizontal = direction === 'LR';
  const model = meta.modelHint ?? transition.llmConfig?.model;

  return (
    <>
      <Handle
        type="target"
        position={isHorizontal ? Position.Left : Position.Top}
        style={{ background: color, width: 8, height: 8, border: `2px solid ${color}` }}
      />
      <div
        onClick={(e) => {
          e.stopPropagation();
          onSelect?.(transition);
        }}
        className="rounded-xl px-3 py-2.5 transition-all duration-300 cursor-pointer relative group"
        style={{
          minWidth: 170,
          maxWidth: 230,
          backgroundColor: `${color}10`,
          border: `1.5px solid ${color}60`,
          boxShadow: fired ? `0 0 24px ${color}50, 0 0 8px ${color}30` : 'none',
          opacity: fired === false ? 0.25 : 1,
        }}
      >
        {executionOrder != null && (
          <div
            className="absolute -top-2.5 -right-2.5 w-6 h-6 rounded-full flex items-center justify-center text-[10px] font-black"
            style={{ backgroundColor: color, color: '#0a0a0f', boxShadow: `0 0 8px ${color}60` }}
          >
            {executionOrder}
          </div>
        )}

        {/* Header: profile glyph + profile id chip */}
        <div className="flex items-center gap-1.5 mb-1.5">
          <span className="text-base leading-none">{glyph}</span>
          <span
            className="text-[9px] font-bold px-1.5 py-0.5 rounded uppercase tracking-wider"
            style={{
              backgroundColor: `${color}25`,
              color,
              fontFamily: "'JetBrains Mono', monospace",
            }}
          >
            {meta.profileId}
          </span>
          <span
            className="text-[8px] font-medium px-1 py-0.5 rounded ml-auto"
            style={{
              backgroundColor: 'rgba(255,255,255,0.05)',
              color: 'var(--text-secondary)',
              fontFamily: "'JetBrains Mono', monospace",
            }}
          >
            sub-agent
          </span>
        </div>

        {/* Action verb — the load-bearing label */}
        <div
          className="text-[12px] font-semibold mb-1"
          style={{ color: 'var(--text-primary)', fontFamily: "'JetBrains Mono', monospace" }}
        >
          {meta.actionId}
        </div>

        {/* Subagent label (precomposed by BE) */}
        <div
          className="text-[10px] mb-1 leading-tight"
          style={{ color: 'var(--text-secondary)' }}
        >
          {meta.subagentLabel}
        </div>

        {model && (
          <div className="text-[9px] flex items-center gap-1" style={{ color: 'var(--text-secondary)' }}>
            <span style={{ color: `${color}90` }}>model</span>
            <span style={{ fontFamily: "'JetBrains Mono', monospace" }}>{model}</span>
          </div>
        )}

        <div
          className="text-[7px] mt-1.5 opacity-0 group-hover:opacity-100 transition-opacity"
          style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
        >
          {transition.id}
        </div>
      </div>
      <Handle
        type="source"
        position={isHorizontal ? Position.Right : Position.Bottom}
        style={{ background: color, width: 8, height: 8, border: `2px solid ${color}` }}
      />
    </>
  );
});
