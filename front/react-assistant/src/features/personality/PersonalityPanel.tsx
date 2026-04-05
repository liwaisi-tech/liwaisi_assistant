import { usePersonality } from '../../hooks/usePersonality';
import { PrincipleEditor } from './PrincipleEditor';
import { TensionVisualizer } from './TensionVisualizer';
import { HierarchyControl } from './HierarchyControl';
import type { UpdatePrincipleRequest } from '../../types/personality';
import { useState, useCallback } from 'react';

const kindColorMap: Record<string, string> = {
  nucleo: 'sky-500',
  conducta: 'amber-500',
  etica: 'emerald-500',
};

export function PersonalityPanel() {
  const { personality, loading, error, saving, updatePrinciple, setHierarchy, reset, reload } = usePersonality();
  const [resetConfirm, setResetConfirm] = useState(false);

  const handleReset = useCallback(async () => {
    try {
      await reset();
      setResetConfirm(false);
    } catch {
      // Error shown via hook
    }
  }, [reset]);

  const handleSavePrinciple = useCallback((kind: string) => {
    return async (data: UpdatePrincipleRequest) => {
      await updatePrinciple(kind, data);
    };
  }, [updatePrinciple]);

  if (loading) {
    return (
      <div className="flex-1 flex items-center justify-center p-8">
        <div className="flex flex-col items-center gap-3">
          <div className="flex gap-1.5">
            <span className="thinking-dot" />
            <span className="thinking-dot" />
            <span className="thinking-dot" />
          </div>
          <span
            className="text-xs"
            style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
          >
            Loading agent identity...
          </span>
        </div>
      </div>
    );
  }

  if (error && !personality) {
    return (
      <div className="flex-1 flex items-center justify-center p-8">
        <div className="flex flex-col items-center gap-3 text-center">
          <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="#ef4444" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
            <circle cx="12" cy="12" r="10" />
            <line x1="12" y1="8" x2="12" y2="12" />
            <line x1="12" y1="16" x2="12.01" y2="16" />
          </svg>
          <span className="text-xs" style={{ color: '#ef4444' }}>{error}</span>
          <button
            onClick={reload}
            className="px-3 py-1.5 rounded-lg text-[11px] font-medium transition-colors"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              backgroundColor: 'rgba(14, 165, 233, 0.15)',
              color: 'var(--accent)',
            }}
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  if (!personality) return null;

  return (
    <div className="flex-1 flex flex-col min-h-0 overflow-y-auto chat-scroll">
      <div className="p-4 md:p-6 max-w-2xl mx-auto w-full space-y-5">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div>
            <h2
              className="text-base font-semibold"
              style={{ color: 'var(--text-primary)', fontFamily: "'JetBrains Mono', monospace" }}
            >
              Agent Identity
            </h2>
            <p className="text-[11px] mt-0.5" style={{ color: 'var(--text-muted)' }}>
              v{personality.version} &middot; {new Date(personality.updated_at).toLocaleDateString()}
            </p>
          </div>

          <div className="flex items-center gap-2">
            {!resetConfirm ? (
              <button
                onClick={() => setResetConfirm(true)}
                className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-[11px] font-medium transition-colors"
                style={{
                  fontFamily: "'JetBrains Mono', monospace",
                  color: 'var(--text-muted)',
                  border: '1px solid var(--border-dim)',
                }}
                onMouseEnter={(e) => { e.currentTarget.style.borderColor = '#ef4444'; e.currentTarget.style.color = '#ef4444'; }}
                onMouseLeave={(e) => { e.currentTarget.style.borderColor = 'var(--border-dim)'; e.currentTarget.style.color = 'var(--text-muted)'; }}
              >
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <polyline points="1 4 1 10 7 10" /><path d="M3.51 15a9 9 0 102.13-9.36L1 10" />
                </svg>
                Reset
              </button>
            ) : (
              <div className="flex items-center gap-2">
                <span className="text-[11px]" style={{ color: 'var(--text-muted)' }}>Reset to defaults?</span>
                <button
                  onClick={handleReset}
                  disabled={saving}
                  className="px-2.5 py-1 rounded-lg text-[11px] font-medium"
                  style={{
                    fontFamily: "'JetBrains Mono', monospace",
                    backgroundColor: 'rgba(239, 68, 68, 0.15)',
                    color: '#ef4444',
                  }}
                >
                  {saving ? 'Resetting...' : 'Confirm'}
                </button>
                <button
                  onClick={() => setResetConfirm(false)}
                  className="px-2.5 py-1 rounded-lg text-[11px] font-medium"
                  style={{
                    fontFamily: "'JetBrains Mono', monospace",
                    color: 'var(--text-muted)',
                  }}
                >
                  Cancel
                </button>
              </div>
            )}
          </div>
        </div>

        {/* Error banner */}
        {error && (
          <div
            className="px-3 py-2 rounded-lg text-[11px] border"
            style={{
              backgroundColor: 'rgba(239, 68, 68, 0.08)',
              borderColor: 'rgba(239, 68, 68, 0.3)',
              color: '#ef4444',
            }}
          >
            {error}
          </div>
        )}

        {/* Hierarchy Control */}
        <HierarchyControl
          hierarchy={personality.hierarchy}
          onSave={setHierarchy}
          saving={saving}
        />

        {/* Principle Editors */}
        {personality.principles.map((principle) => (
          <PrincipleEditor
            key={principle.kind}
            principle={principle}
            color={kindColorMap[principle.kind] ?? 'sky-500'}
            onSave={handleSavePrinciple(principle.kind)}
            saving={saving}
          />
        ))}

        {/* Tension Visualizer */}
        <TensionVisualizer tensions={personality.tensions} />
      </div>
    </div>
  );
}
