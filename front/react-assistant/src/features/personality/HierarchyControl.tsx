import { useState, useCallback } from 'react';
import { useTranslation } from 'react-i18next';

interface HierarchyControlProps {
  hierarchy: string[];
  onSave: (hierarchy: [string, string, string]) => Promise<void>;
  saving: boolean;
}

const POSITION_KEYS = ['hierarchyControl.positionLabels.highest', 'hierarchyControl.positionLabels.secondary', 'hierarchyControl.positionLabels.foundation'] as const;

const kindColors: Record<string, string> = {
  nucleo: '#38bdf8',
  conducta: '#fbbf24',
  etica: '#34d399',
};

const getColor = (name: string) => {
  const lower = name.toLowerCase();
  for (const [kind, color] of Object.entries(kindColors)) {
    if (lower.includes(kind)) return color;
  }
  return 'var(--accent)';
};

export function HierarchyControl({ hierarchy, onSave, saving }: HierarchyControlProps) {
  const { t } = useTranslation('personality');
  const [items, setItems] = useState<string[]>(hierarchy);
  const [warning, setWarning] = useState<string | null>(null);
  const [hasChanges, setHasChanges] = useState(false);

  const swap = useCallback((index: number, direction: 'up' | 'down') => {
    setItems((prev) => {
      const next = [...prev];
      const targetIndex = direction === 'up' ? index - 1 : index + 1;
      if (targetIndex < 0 || targetIndex >= next.length) return prev;

      [next[index], next[targetIndex]] = [next[targetIndex], next[index]];

      // Check if etica is at position 3 (foundation / last position)
      const eticaItem = next.find((item) => item.toLowerCase().includes('etica'));
      if (eticaItem && next.indexOf(eticaItem) === next.length - 1) {
        setWarning(t('hierarchyControl.eticaWarning'));
      } else {
        setWarning(null);
      }

      setHasChanges(true);
      return next;
    });
  }, [t]);

  const handleSave = useCallback(async () => {
    try {
      await onSave(items as [string, string, string]);
      setHasChanges(false);
      setWarning(null);
    } catch {
      // Error handled by parent
    }
  }, [items, onSave]);

  const handleReset = useCallback(() => {
    setItems(hierarchy);
    setHasChanges(false);
    setWarning(null);
  }, [hierarchy]);

  return (
    <div
      className="rounded-xl border p-4"
      style={{
        backgroundColor: 'var(--bg-surface)',
        borderColor: 'var(--border-dim)',
      }}
    >
      <span
        className="text-[10px] font-semibold uppercase tracking-wider block mb-3"
        style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
      >
        {t('hierarchyControl.title')}
      </span>

      <div className="space-y-2">
        {items.map((item, index) => {
          const color = getColor(item);
          return (
            <div
              key={item}
              className="flex items-center gap-3 px-3 py-2 rounded-lg border transition-colors"
              style={{
                backgroundColor: `${color}08`,
                borderColor: `${color}30`,
              }}
            >
              {/* Position number */}
              <span
                className="shrink-0 w-5 h-5 rounded-full flex items-center justify-center text-[10px] font-bold"
                style={{
                  backgroundColor: `${color}20`,
                  color: color,
                  fontFamily: "'JetBrains Mono', monospace",
                }}
              >
                {index + 1}
              </span>

              {/* Name + position label */}
              <div className="flex-1 min-w-0">
                <span
                  className="text-xs font-medium"
                  style={{ color: color, fontFamily: "'JetBrains Mono', monospace" }}
                >
                  {item}
                </span>
                <span className="text-[9px] ml-2" style={{ color: 'var(--text-muted)' }}>
                  {t(POSITION_KEYS[index])}
                </span>
              </div>

              {/* Up/down arrows */}
              <div className="flex flex-col gap-0.5">
                <button
                  onClick={() => swap(index, 'up')}
                  disabled={index === 0}
                  className="w-5 h-4 flex items-center justify-center rounded transition-colors"
                  style={{
                    color: index === 0 ? 'var(--border-dim)' : 'var(--text-muted)',
                    cursor: index === 0 ? 'default' : 'pointer',
                  }}
                  onMouseEnter={(e) => { if (index !== 0) e.currentTarget.style.color = 'var(--text-primary)'; }}
                  onMouseLeave={(e) => { if (index !== 0) e.currentTarget.style.color = 'var(--text-muted)'; }}
                  aria-label={t('hierarchyControl.moveUpAriaLabel', { item })}
                >
                  <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                    <polyline points="18 15 12 9 6 15" />
                  </svg>
                </button>
                <button
                  onClick={() => swap(index, 'down')}
                  disabled={index === items.length - 1}
                  className="w-5 h-4 flex items-center justify-center rounded transition-colors"
                  style={{
                    color: index === items.length - 1 ? 'var(--border-dim)' : 'var(--text-muted)',
                    cursor: index === items.length - 1 ? 'default' : 'pointer',
                  }}
                  onMouseEnter={(e) => { if (index !== items.length - 1) e.currentTarget.style.color = 'var(--text-primary)'; }}
                  onMouseLeave={(e) => { if (index !== items.length - 1) e.currentTarget.style.color = 'var(--text-muted)'; }}
                  aria-label={t('hierarchyControl.moveDownAriaLabel', { item })}
                >
                  <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                    <polyline points="6 9 12 15 18 9" />
                  </svg>
                </button>
              </div>
            </div>
          );
        })}
      </div>

      {/* Warning */}
      {warning && (
        <div
          className="mt-3 px-3 py-2 rounded-lg text-[11px] border"
          style={{
            backgroundColor: 'rgba(245, 158, 11, 0.08)',
            borderColor: 'rgba(245, 158, 11, 0.3)',
            color: '#fbbf24',
          }}
        >
          {warning}
        </div>
      )}

      {/* Save/Reset buttons */}
      {hasChanges && (
        <div className="mt-3 flex items-center gap-2">
          <button
            onClick={handleSave}
            disabled={saving}
            className="px-3 py-1.5 rounded-lg text-[11px] font-medium transition-all duration-150"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              backgroundColor: 'rgba(14, 165, 233, 0.2)',
              color: 'var(--accent)',
              opacity: saving ? 0.5 : 1,
            }}
            onMouseEnter={(e) => { if (!saving) e.currentTarget.style.boxShadow = '0 0 12px -2px var(--accent-glow)'; }}
            onMouseLeave={(e) => { e.currentTarget.style.boxShadow = 'none'; }}
          >
            {saving ? t('hierarchyControl.saving') : t('hierarchyControl.saveOrder')}
          </button>
          <button
            onClick={handleReset}
            className="px-3 py-1.5 rounded-lg text-[11px] font-medium transition-colors"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              color: 'var(--text-muted)',
            }}
          >
            {t('hierarchyControl.reset')}
          </button>
        </div>
      )}
    </div>
  );
}
