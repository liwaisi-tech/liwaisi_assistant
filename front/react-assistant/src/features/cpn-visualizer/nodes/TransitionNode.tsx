import { Handle, Position } from '@xyflow/react';
import type { TransitionTopology } from '../../../types/flow';

const KIND_ICONS: Record<string, string> = {
  llm: '\u2728',    // sparkle
  tool: '\u2699',    // gear
  validate: '\u2713', // check
  subnet: '\u25A3',  // box
  observer: '\u25C9', // circle
  hitl: '\u270B',    // hand
};

const KIND_LABELS: Record<string, string> = {
  llm: 'LLM',
  tool: 'TOOL',
  validate: 'VALIDATE',
  subnet: 'SUBNET',
  observer: 'OBSERVER',
  hitl: 'HITL',
};

const KIND_COLORS: Record<string, string> = {
  llm: '#0ea5e9',
  tool: '#10b981',
  validate: '#a855f7',
  subnet: '#6366f1',
  observer: '#64748b',
  hitl: '#f59e0b',
};

interface TransitionNodeData {
  transition: TransitionTopology;
  fired?: boolean;
  executionOrder?: number;
  direction?: 'LR' | 'TB';
  onSelect?: (transition: TransitionTopology) => void;
}

export function TransitionNode({ data }: { data: TransitionNodeData }) {
  const { transition, fired, executionOrder, direction = 'LR', onSelect } = data;
  const color = KIND_COLORS[transition.kind] || '#64748b';
  const icon = KIND_ICONS[transition.kind] || '?';
  const kindLabel = KIND_LABELS[transition.kind] || transition.kind;
  const model = transition.llmConfig?.model;
  const guard = transition.guardFunc;
  const prompt = transition.systemPrompt;
  const isHorizontal = direction === 'LR';

  const handleClick = (e: React.MouseEvent) => {
    e.stopPropagation();
    onSelect?.(transition);
  };

  return (
    <>
      <Handle
        type="target"
        position={isHorizontal ? Position.Left : Position.Top}
        style={{ background: color, width: 8, height: 8, border: `2px solid ${color}` }}
      />
      <div
        onClick={handleClick}
        className="rounded-xl px-3 py-2.5 transition-all duration-300 cursor-pointer relative"
        style={{
          minWidth: 160,
          maxWidth: 220,
          backgroundColor: `${color}08`,
          border: `1.5px solid ${color}50`,
          boxShadow: fired
            ? `0 0 24px ${color}40, 0 0 8px ${color}20`
            : 'none',
          opacity: fired === false ? 0.2 : 1,
        }}
      >
        {/* Execution order badge */}
        {executionOrder != null && (
          <div
            className="absolute -top-2.5 -right-2.5 w-6 h-6 rounded-full flex items-center justify-center text-[10px] font-black"
            style={{ backgroundColor: color, color: '#0a0a0f', boxShadow: `0 0 8px ${color}60` }}
          >
            {executionOrder}
          </div>
        )}

        {/* Header: icon + kind + id */}
        <div className="flex items-center gap-1.5 mb-1.5">
          <span className="text-sm">{icon}</span>
          <span
            className="text-[9px] font-bold px-1.5 py-0.5 rounded"
            style={{
              backgroundColor: `${color}20`,
              color,
              fontFamily: "'JetBrains Mono', monospace",
              letterSpacing: '0.05em',
            }}
          >
            {kindLabel}
          </span>
        </div>

        {/* Transition ID */}
        <div
          className="text-[11px] font-semibold mb-1"
          style={{ color: 'var(--text-primary)', fontFamily: "'JetBrains Mono', monospace" }}
        >
          {transition.id}
        </div>

        {/* Metadata lines */}
        <div className="space-y-0.5">
          {model && (
            <div className="text-[9px] flex items-center gap-1" style={{ color: 'var(--text-secondary)' }}>
              <span style={{ color: `${color}90` }}>model</span>
              <span style={{ fontFamily: "'JetBrains Mono', monospace" }}>{model}</span>
            </div>
          )}
          {transition.llmConfig && !model && (
            <div className="text-[9px] flex items-center gap-1" style={{ color: 'var(--text-secondary)' }}>
              <span style={{ color: `${color}90` }}>tokens</span>
              <span style={{ fontFamily: "'JetBrains Mono', monospace" }}>
                {transition.llmConfig.maxTokens}
                {transition.llmConfig.streamOutput && ' \u25B6'}
              </span>
            </div>
          )}
          {guard && (
            <div className="text-[9px] flex items-center gap-1" style={{ color: 'var(--text-secondary)' }}>
              <span style={{ color: `${color}90` }}>guard</span>
              <span style={{ fontFamily: "'JetBrains Mono', monospace" }}>{guard.replace('guard-', '')}</span>
            </div>
          )}
          {prompt && (
            <div
              className="text-[8px] mt-1 leading-tight opacity-60 line-clamp-2"
              style={{ color: 'var(--text-muted)', fontFamily: "'DM Sans', sans-serif" }}
            >
              {prompt.slice(0, 60)}{prompt.length > 60 ? '...' : ''}
            </div>
          )}
          {transition.kind === 'hitl' && (
            <div className="text-[9px] mt-1 font-semibold" style={{ color: '#f59e0b' }}>
              \u270B Human Gate
            </div>
          )}
        </div>

        {/* Click hint */}
        <div
          className="text-[7px] mt-1.5 opacity-0 group-hover:opacity-100 transition-opacity"
          style={{ color: 'var(--text-muted)' }}
        >
          click for details
        </div>
      </div>
      <Handle
        type="source"
        position={isHorizontal ? Position.Right : Position.Bottom}
        style={{ background: color, width: 8, height: 8, border: `2px solid ${color}` }}
      />
    </>
  );
}
