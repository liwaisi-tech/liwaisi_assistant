import { useEffect } from 'react';
import type { ExecutionState, ExecutionRun } from '../../hooks/useExecutionMonitor';
import { LiveGraph } from './LiveGraph';
import { TransitionInspector } from './TransitionInspector';
import { CostAccumulator } from './CostAccumulator';
import { MetricsSummary } from './MetricsSummary';
import { CPNBreadcrumb } from './CPNBreadcrumb';
import { TimelineScrubber } from './TimelineScrubber';
import { ExecutionList } from './ExecutionList';

interface ExecutionMonitorProps {
  state: ExecutionState;
  selectedRun: ExecutionRun | null;
  onSelectRun: (index: number) => void;
  onSelectTransition: (transitionId: string | null) => void;
  onNavigateCPN: (cpnId: string) => void;
  onLoadTrace?: (sessionId: string) => Promise<void> | void;
  sessionId: string | null;
}

export function ExecutionMonitor({
  state,
  selectedRun,
  onSelectRun,
  onSelectTransition,
  onNavigateCPN,
  onLoadTrace,
  sessionId,
}: ExecutionMonitorProps) {
  // Auto-load trace when entering monitor with a session
  useEffect(() => {
    if (sessionId && onLoadTrace && state.runs.length === 0) {
      const result = onLoadTrace(sessionId);
      if (result && typeof result.catch === 'function') {
        result.catch(() => {/* best-effort */});
      }
    }
  }, [sessionId]); // eslint-disable-line react-hooks/exhaustive-deps

  if (!state.topology) {
    return <EmptyState sessionId={sessionId} />;
  }

  const selectedId = state.selectedTransitionId;
  const startedPayload = selectedId && selectedRun ? selectedRun.activeTransitions.get(selectedId) : undefined;
  const completedPayload = selectedId && selectedRun ? selectedRun.completedTransitions.get(selectedId) : undefined;

  return (
    <div className="flex flex-1 overflow-hidden" style={{ backgroundColor: 'var(--bg-deep)' }}>
      {/* Left: Execution list */}
      <div
        className="shrink-0 flex flex-col"
        style={{
          width: 220,
          borderRight: '1px solid var(--border-dim)',
          backgroundColor: 'var(--bg-surface)',
        }}
      >
        <ExecutionList
          runs={state.runs}
          selectedIndex={state.selectedRunIndex}
          onSelect={onSelectRun}
        />
      </div>

      {/* Center + Right */}
      <div className="flex flex-col flex-1 overflow-hidden">
        {/* Top bar */}
        <div
          className="flex items-center justify-between px-4 py-2 shrink-0"
          style={{ borderBottom: '1px solid var(--border-dim)', backgroundColor: 'var(--bg-surface)' }}
        >
          <CPNBreadcrumb
            hierarchy={state.cpnHierarchy}
            activeCPNId={state.activeCPNId}
            onNavigate={onNavigateCPN}
          />
          {selectedRun && (
            <MetricsSummary
              transitionsFired={selectedRun.transitionsFired}
              llmCalls={selectedRun.llmCalls}
              totalCostUSD={selectedRun.totalCostUSD}
              eventCount={selectedRun.events.length}
            />
          )}
        </div>

        {/* Main area */}
        <div className="flex flex-1 overflow-hidden relative">
          {/* Graph */}
          <div className="flex-1 relative">
            {selectedRun ? (
              <>
                <LiveGraph
                  topology={state.topology}
                  activeTransitions={selectedRun.activeTransitions}
                  completedTransitions={selectedRun.completedTransitions}
                  onSelectTransition={(id) => onSelectTransition(id)}
                />
                <CostAccumulator
                  totalCostUSD={selectedRun.totalCostUSD}
                  llmCalls={selectedRun.llmCalls}
                  transitionsFired={selectedRun.transitionsFired}
                />
              </>
            ) : (
              <div className="flex-1 flex items-center justify-center h-full">
                <div className="text-[10px]" style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}>
                  Select an execution from the list
                </div>
              </div>
            )}
          </div>

          {/* Inspector panel */}
          {selectedId && state.topology && (
            <TransitionInspector
              transitionId={selectedId}
              topology={state.topology}
              startedPayload={startedPayload}
              completedPayload={completedPayload}
              onClose={() => onSelectTransition(null)}
            />
          )}
        </div>

        {/* Timeline */}
        <div
          className="shrink-0"
          style={{
            height: 48,
            borderTop: '1px solid var(--border-dim)',
            backgroundColor: 'var(--bg-surface)',
          }}
        >
          <TimelineScrubber events={selectedRun?.events ?? []} />
        </div>
      </div>
    </div>
  );
}

function EmptyState({ sessionId }: { sessionId: string | null }) {
  return (
    <div className="flex-1 flex flex-col items-center justify-center gap-3" style={{ backgroundColor: 'var(--bg-deep)' }}>
      <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="var(--text-muted)" strokeWidth="1" strokeLinecap="round" strokeLinejoin="round">
        <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
      </svg>
      <div className="text-center" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
        <div className="text-[11px] font-semibold mb-1" style={{ color: 'var(--text-secondary)' }}>
          Execution Monitor
        </div>
        <div className="text-[10px]" style={{ color: 'var(--text-muted)' }}>
          {sessionId
            ? 'Send a message to start monitoring CPN execution'
            : 'No active session'
          }
        </div>
      </div>
    </div>
  );
}
