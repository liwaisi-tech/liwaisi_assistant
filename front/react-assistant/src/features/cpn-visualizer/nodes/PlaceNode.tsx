import { memo } from 'react';
import { useTranslation } from 'react-i18next';
import { Handle, Position } from '@xyflow/react';
import type { PlaceTopology } from '../../../types/flow';

const COLOR_MAP: Record<string, string> = {
  STRING: '#0ea5e9',
  JSON: '#a855f7',
  ARTIFACT: '#10b981',
  HUMAN: '#f59e0b',
  SCORE: '#ef4444',
  EVENT: '#6366f1',
  CPN: '#ec4899',
  SCHEMA: '#14b8a6',
  ERROR: '#dc2626',
};

interface PlaceNodeData {
  place: PlaceTopology;
  fired?: boolean;
  direction?: 'LR' | 'TB';
}

export const PlaceNode = memo(function PlaceNode({ data }: { data: PlaceNodeData }) {
  const { t } = useTranslation('flows');
  const { place, fired, direction = 'LR' } = data;
  const color = COLOR_MAP[place.color] || '#64748b';
  const isHorizontal = direction === 'LR';

  const spaceLabel = t(`placeNode.spaceLabels.${place.space}`, { defaultValue: place.space });

  return (
    <>
      <Handle
        type="target"
        position={isHorizontal ? Position.Left : Position.Top}
        style={{ background: color, width: 8, height: 8, border: `2px solid ${color}` }}
      />
      <div
        className="flex items-center justify-center rounded-full transition-all duration-300 cursor-default"
        style={{
          width: 72,
          height: 72,
          backgroundColor: `${color}10`,
          border: `2px solid ${color}60`,
          boxShadow: fired !== false ? `0 0 20px ${color}25, inset 0 0 12px ${color}08` : 'none',
          opacity: fired === false ? 0.25 : 1,
        }}
      >
        <div className="text-center px-1">
          <div
            className="text-[10px] font-bold leading-tight"
            style={{ color, fontFamily: "'JetBrains Mono', monospace" }}
          >
            {place.id.replace('p-', '')}
          </div>
          <div
            className="text-[8px] mt-0.5 opacity-70"
            style={{ color, fontFamily: "'JetBrains Mono', monospace" }}
          >
            {place.color}
          </div>
          <div className="text-[7px] opacity-50" style={{ color: 'var(--text-muted)' }}>
            {spaceLabel}
          </div>
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
