import { useReducer, useCallback } from 'react';
import type { CPNEventData, TransitionStartedPayload, TransitionCompletedPayload } from '../types/sse';
import type { CPNTopology } from '../types/flow';
import { getSessionExecution } from '../services/api';

// ── Types ─────────────────────────────────────────────────────────────────

export interface MonitorEvent {
  id: string;
  type: string;
  transitionId: string;
  transitionKind: string;
  cpnId: string;
  cpnDepth: number;
  cpnRole: string;
  payload: unknown;
  timestamp: string;
}

export interface ExecutionRun {
  id: string;
  inputMessage: string;
  startTime: string;
  endTime?: string;
  events: MonitorEvent[];
  activeTransitions: Map<string, TransitionStartedPayload>;
  completedTransitions: Map<string, TransitionCompletedPayload>;
  totalCostUSD: number;
  transitionsFired: number;
  llmCalls: number;
  isRunning: boolean;
}

export interface CPNHierarchyNode {
  cpnId: string;
  cpnRole: string;
  cpnDepth: number;
  parentCPNId: string | null;
  children: CPNHierarchyNode[];
}

export interface ExecutionState {
  runs: ExecutionRun[];
  selectedRunIndex: number | null;
  topology: CPNTopology | null;
  selectedTransitionId: string | null;
  cpnHierarchy: CPNHierarchyNode[];
  activeCPNId: string | null;
}

// ── Actions ───────────────────────────────────────────────────────────────

type MonitorAction =
  | { type: 'NEW_EXECUTION'; inputMessage: string }
  | { type: 'TRANSITION_STARTED'; event: MonitorEvent }
  | { type: 'TRANSITION_COMPLETED'; event: MonitorEvent }
  | { type: 'SUBNET_STARTED'; event: MonitorEvent }
  | { type: 'SUBNET_COMPLETED'; event: MonitorEvent }
  | { type: 'LOAD_TOPOLOGY'; topology: CPNTopology }
  | { type: 'LOAD_TRACE'; runs: ExecutionRun[] }
  | { type: 'SELECT_RUN'; index: number }
  | { type: 'SELECT_TRANSITION'; transitionId: string | null }
  | { type: 'NAVIGATE_CPN'; cpnId: string }
  | { type: 'RESET' };

// ── Initial State ─────────────────────────────────────────────────────────

const initialState: ExecutionState = {
  runs: [],
  selectedRunIndex: null,
  topology: null,
  selectedTransitionId: null,
  cpnHierarchy: [],
  activeCPNId: null,
};

// ── Helpers ───────────────────────────────────────────────────────────────

function createEmptyRun(inputMessage: string, startTime: string): ExecutionRun {
  return {
    id: `run-${Date.now()}`,
    inputMessage,
    startTime,
    events: [],
    activeTransitions: new Map(),
    completedTransitions: new Map(),
    totalCostUSD: 0,
    transitionsFired: 0,
    llmCalls: 0,
    isRunning: true,
  };
}

function updateCurrentRun(state: ExecutionState, updater: (run: ExecutionRun) => ExecutionRun): ExecutionState {
  if (state.runs.length === 0) return state;
  const idx = state.runs.length - 1;
  const updated = [...state.runs];
  updated[idx] = updater(updated[idx]);
  // Auto-select current run if none selected
  return { ...state, runs: updated, selectedRunIndex: state.selectedRunIndex ?? idx };
}

// ── Reducer ───────────────────────────────────────────────────────────────

function monitorReducer(state: ExecutionState, action: MonitorAction): ExecutionState {
  switch (action.type) {
    case 'NEW_EXECUTION': {
      // Mark previous run as done
      const runs = state.runs.map((r) =>
        r.isRunning ? { ...r, isRunning: false, endTime: new Date().toISOString() } : r
      );
      const newRun = createEmptyRun(action.inputMessage, new Date().toISOString());
      return {
        ...state,
        runs: [...runs, newRun],
        selectedRunIndex: runs.length, // Select the new run
        selectedTransitionId: null,
      };
    }

    case 'TRANSITION_STARTED':
      return updateCurrentRun(state, (run) => {
        const newActive = new Map(run.activeTransitions);
        const payload = action.event.payload as TransitionStartedPayload | undefined;
        if (payload) newActive.set(action.event.transitionId, payload);
        return {
          ...run,
          events: [...run.events, action.event],
          activeTransitions: newActive,
        };
      });

    case 'TRANSITION_COMPLETED':
      return updateCurrentRun(state, (run) => {
        const newActive = new Map(run.activeTransitions);
        newActive.delete(action.event.transitionId);
        const newCompleted = new Map(run.completedTransitions);
        const payload = action.event.payload as TransitionCompletedPayload | undefined;
        if (payload) newCompleted.set(action.event.transitionId, payload);
        const cost = payload?.cost_usd ?? 0;
        const isLLM = action.event.transitionKind === 'llm';
        const allDone = newActive.size === 0;
        return {
          ...run,
          events: [...run.events, action.event],
          activeTransitions: newActive,
          completedTransitions: newCompleted,
          totalCostUSD: run.totalCostUSD + cost,
          transitionsFired: run.transitionsFired + 1,
          llmCalls: run.llmCalls + (isLLM ? 1 : 0),
          isRunning: !allDone,
          endTime: allDone ? action.event.timestamp : run.endTime,
        };
      });

    case 'SUBNET_STARTED': {
      const node: CPNHierarchyNode = {
        cpnId: action.event.cpnId,
        cpnRole: action.event.cpnRole,
        cpnDepth: action.event.cpnDepth,
        parentCPNId: null,
        children: [],
      };
      const updated = updateCurrentRun(state, (run) => ({
        ...run,
        events: [...run.events, action.event],
      }));
      return { ...updated, cpnHierarchy: [...updated.cpnHierarchy, node] };
    }

    case 'SUBNET_COMPLETED':
      return updateCurrentRun(state, (run) => ({
        ...run,
        events: [...run.events, action.event],
      }));

    case 'LOAD_TOPOLOGY':
      return { ...state, topology: action.topology, activeCPNId: action.topology.id };

    case 'LOAD_TRACE':
      return {
        ...state,
        runs: action.runs,
        selectedRunIndex: action.runs.length > 0 ? action.runs.length - 1 : null,
        selectedTransitionId: null,
      };

    case 'SELECT_RUN':
      return { ...state, selectedRunIndex: action.index, selectedTransitionId: null };

    case 'SELECT_TRANSITION':
      return { ...state, selectedTransitionId: action.transitionId };

    case 'NAVIGATE_CPN':
      return { ...state, activeCPNId: action.cpnId };

    case 'RESET':
      return { ...initialState };

    default:
      return state;
  }
}

// ── Hook ──────────────────────────────────────────────────────────────────

export function useExecutionMonitor() {
  const [state, dispatch] = useReducer(monitorReducer, initialState);

  const startNewExecution = useCallback((inputMessage: string) => {
    dispatch({ type: 'NEW_EXECUTION', inputMessage });
  }, []);

  const handleTransitionStarted = useCallback((data: CPNEventData) => {
    dispatch({ type: 'TRANSITION_STARTED', event: cpnEventToMonitorEvent(data) });
  }, []);

  const handleTransitionCompleted = useCallback((data: CPNEventData) => {
    dispatch({ type: 'TRANSITION_COMPLETED', event: cpnEventToMonitorEvent(data) });
  }, []);

  const handleSubNetStarted = useCallback((data: CPNEventData) => {
    dispatch({ type: 'SUBNET_STARTED', event: cpnEventToMonitorEvent(data) });
  }, []);

  const handleSubNetCompleted = useCallback((data: CPNEventData) => {
    dispatch({ type: 'SUBNET_COMPLETED', event: cpnEventToMonitorEvent(data) });
  }, []);

  const loadTopology = useCallback((topology: CPNTopology) => {
    dispatch({ type: 'LOAD_TOPOLOGY', topology });
  }, []);

  const loadExecutionTrace = useCallback(async (sessionId: string) => {
    const resp = await getSessionExecution(sessionId);
    const allEvents: MonitorEvent[] = resp.events.map((e) => ({
      id: e.id,
      type: e.type,
      transitionId: e.transition_id,
      transitionKind: e.transition_kind,
      cpnId: e.cpn_id,
      cpnDepth: e.cpn_depth ?? 0,
      cpnRole: e.cpn_role ?? '',
      payload: e.payload,
      timestamp: e.timestamp,
    }));

    // Group events into runs by detecting execution boundaries.
    // A new run starts when we see transition_started and no transitions are active.
    const runs: ExecutionRun[] = [];
    let currentRun: ExecutionRun | null = null;
    let activeCount = 0;

    for (const evt of allEvents) {
      if (evt.type === 'transition_started') {
        if (!currentRun || activeCount === 0) {
          // Start a new run
          if (currentRun) {
            currentRun.isRunning = false;
            currentRun.endTime = evt.timestamp;
          }
          currentRun = createEmptyRun('', evt.timestamp);
          runs.push(currentRun);
          activeCount = 0;
        }
        activeCount++;
        const payload = evt.payload as TransitionStartedPayload | undefined;
        if (payload) currentRun.activeTransitions.set(evt.transitionId, payload);
        currentRun.events.push(evt);
      } else if (evt.type === 'transition_completed') {
        if (!currentRun) {
          currentRun = createEmptyRun('', evt.timestamp);
          runs.push(currentRun);
        }
        activeCount = Math.max(0, activeCount - 1);
        currentRun.activeTransitions.delete(evt.transitionId);
        const payload = evt.payload as TransitionCompletedPayload | undefined;
        if (payload) {
          currentRun.completedTransitions.set(evt.transitionId, payload);
          currentRun.totalCostUSD += payload.cost_usd ?? 0;
        }
        currentRun.transitionsFired++;
        if (evt.transitionKind === 'llm') currentRun.llmCalls++;
        currentRun.events.push(evt);
        if (activeCount === 0) {
          currentRun.isRunning = false;
          currentRun.endTime = evt.timestamp;
        }
      } else if (evt.type === 'stream_chunk' || evt.type === 'hitl_requested' || evt.type === 'hitl_resolved' || evt.type === 'mode_switch') {
        if (currentRun) currentRun.events.push(evt);
      }
    }

    // Try to label runs with input messages from output tokens
    for (const run of runs) {
      const firstStarted = run.events.find((e) => e.type === 'transition_started');
      if (firstStarted) {
        const payload = firstStarted.payload as TransitionStartedPayload | undefined;
        if (payload?.input_tokens?.[0]?.payload_preview) {
          run.inputMessage = payload.input_tokens[0].payload_preview;
        }
      }
    }

    dispatch({ type: 'LOAD_TRACE', runs });
  }, []);

  const selectRun = useCallback((index: number) => {
    dispatch({ type: 'SELECT_RUN', index });
  }, []);

  const selectTransition = useCallback((transitionId: string | null) => {
    dispatch({ type: 'SELECT_TRANSITION', transitionId });
  }, []);

  const navigateCPN = useCallback((cpnId: string) => {
    dispatch({ type: 'NAVIGATE_CPN', cpnId });
  }, []);

  const reset = useCallback(() => {
    dispatch({ type: 'RESET' });
  }, []);

  // Derived: selected run
  const selectedRun = state.selectedRunIndex != null ? state.runs[state.selectedRunIndex] ?? null : null;

  return {
    state,
    selectedRun,
    startNewExecution,
    handleTransitionStarted,
    handleTransitionCompleted,
    handleSubNetStarted,
    handleSubNetCompleted,
    loadTopology,
    loadExecutionTrace,
    selectRun,
    selectTransition,
    navigateCPN,
    reset,
  };
}

// ── Helpers ───────────────────────────────────────────────────────────────

function cpnEventToMonitorEvent(data: CPNEventData): MonitorEvent {
  return {
    id: data.ID,
    type: data.Type,
    transitionId: data.TransitionID,
    transitionKind: data.TransitionKind,
    cpnId: data.CPNID,
    cpnDepth: data.CPNDepth,
    cpnRole: data.CPNRole,
    payload: data.Payload,
    timestamp: data.Timestamp,
  };
}
