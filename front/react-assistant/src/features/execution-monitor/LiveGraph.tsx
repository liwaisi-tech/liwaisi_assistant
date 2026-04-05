import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { TopologyGraph } from '../cpn-visualizer/TopologyGraph';
import type { CPNTopology } from '../../types/flow';
import type { TransitionStartedPayload, TransitionCompletedPayload } from '../../types/sse';

interface LiveGraphProps {
  topology: CPNTopology;
  activeTransitions: Map<string, TransitionStartedPayload>;
  completedTransitions: Map<string, TransitionCompletedPayload>;
  onSelectTransition: (transitionId: string) => void;
}

export function LiveGraph({
  topology,
  activeTransitions,
  completedTransitions,
  onSelectTransition,
}: LiveGraphProps) {
  const { t } = useTranslation('monitor');

  // Build the set of fired transitions for TopologyGraph
  const firedTransitions = useMemo(() => {
    const fired = new Set<string>();
    for (const [id] of completedTransitions) {
      fired.add(id);
    }
    for (const [id] of activeTransitions) {
      fired.add(id);
    }
    return fired;
  }, [activeTransitions, completedTransitions]);

  // Enrich the topology with live state data for nodes
  const enrichedTopology = useMemo(() => {
    const enrichedTransitions = { ...topology.transitions };
    for (const [id, transition] of Object.entries(enrichedTransitions)) {
      const isFiring = activeTransitions.has(id);
      const completed = completedTransitions.get(id);
      enrichedTransitions[id] = {
        ...transition,
        // Pass live state via a convention field that PlaceNode/TransitionNode can read
        // We encode state in the existing structure without breaking types
        _liveState: {
          isFiring,
          isCompleted: !!completed,
          hasError: !!completed?.error,
          costUSD: completed?.cost_usd ?? 0,
          durationMs: completed?.duration_ms ?? 0,
        },
      } as typeof transition;
    }
    return { ...topology, transitions: enrichedTransitions };
  }, [topology, activeTransitions, completedTransitions]);

  return (
    <div className="w-full h-full relative">
      <TopologyGraph
        topology={enrichedTopology}
        firedTransitions={firedTransitions}
        onSelectTransition={(t) => onSelectTransition(t.id)}
      />

      {/* Firing overlay indicators */}
      {activeTransitions.size > 0 && (
        <div
          className="absolute bottom-3 right-3 z-10 flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg"
          style={{
            backgroundColor: 'rgba(10, 10, 15, 0.85)',
            border: '1px solid var(--border-dim)',
            fontFamily: "'JetBrains Mono', monospace",
          }}
        >
          <div className="w-2 h-2 rounded-full status-pulse" style={{ backgroundColor: 'var(--accent)' }} />
          <span className="text-[9px]" style={{ color: 'var(--accent)' }}>
            {t('liveGraph.firingCount', { count: activeTransitions.size })}
          </span>
        </div>
      )}
    </div>
  );
}
