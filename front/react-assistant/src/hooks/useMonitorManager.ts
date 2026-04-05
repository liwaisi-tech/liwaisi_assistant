import { useCallback, useMemo } from 'react';
import { useExecutionMonitor } from './useExecutionMonitor';
import { getFlows, getFlow } from '../services/api';

/**
 * Wraps useExecutionMonitor with session-level coordination helpers.
 * Provides stable SSE callbacks and a helper to load monitor data.
 *
 * Coordination effects (message tracking, session change detection) remain
 * in the parent component since they bridge multiple hooks.
 */
export function useMonitorManager(onSessionCompleted: () => void) {
  const monitor = useExecutionMonitor();

  // Memoize SSE callbacks so useChat's useSSE doesn't reconnect on every render
  const sseCallbacks = useMemo(() => ({
    onTransitionStarted: monitor.handleTransitionStarted,
    onTransitionCompleted: monitor.handleTransitionCompleted,
    onSubNetStarted: monitor.handleSubNetStarted,
    onSubNetCompleted: monitor.handleSubNetCompleted,
    onSessionCompleted,
  }), [
    monitor.handleTransitionStarted,
    monitor.handleTransitionCompleted,
    monitor.handleSubNetStarted,
    monitor.handleSubNetCompleted,
    onSessionCompleted,
  ]);

  // Helper: load topology and execution trace for a session
  const loadMonitorDataForSession = useCallback(async (sid: string) => {
    try {
      const flowList = await getFlows();
      if (flowList.items.length > 0) {
        const detail = await getFlow(flowList.items[0].hash);
        monitor.loadTopology(detail.topology);
      }
    } catch {
      // Best-effort — no flows crystallized yet
    }

    try {
      await monitor.loadExecutionTrace(sid);
    } catch {
      // No events yet for this session
    }
  }, [monitor.loadTopology, monitor.loadExecutionTrace]);

  const handleLoadTrace = useCallback(async (sid: string) => {
    await monitor.loadExecutionTrace(sid);
  }, [monitor.loadExecutionTrace]);

  return {
    monitor,
    sseCallbacks,
    handleLoadTrace,
    loadMonitorDataForSession,
  } as const;
}
