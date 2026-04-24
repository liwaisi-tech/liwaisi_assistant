import { useCallback, useMemo, useReducer } from 'react';
import type { CPNEventData, SubAgentStartedPayload, SubAgentFinishedPayload } from '../types/sse';

// SubAgentFiringRecord captures a single firing of a sub-agent. The
// FE pairs subagent_started + subagent_finished by FiringID; consumers
// dedupe so reconnects + replay don't double-count.
export interface SubAgentFiringRecord {
  firingId: string;
  profileId: string;
  actionId: string;
  subagentLabel?: string;
  iconKey?: string;
  model?: string;
  startedAt: string;
  finishedAt?: string;
  durationMs?: number;
  ok?: boolean;
  stage?: string;
  error?: string;
  costUSD: number;
}

export interface ProfileRollup {
  profileId: string;
  iconKey?: string;
  firings: number;
  finished: number;
  failed: number;
  costUSD: number;
  totalDurationMs: number;
}

export interface ActionRollup {
  actionId: string;
  firings: number;
  finished: number;
  failed: number;
  costUSD: number;
}

interface RollupState {
  byFiring: Map<string, SubAgentFiringRecord>;
}

type RollupAction =
  | { kind: 'started'; payload: SubAgentStartedPayload; ts: string }
  | { kind: 'finished'; payload: SubAgentFinishedPayload; ts: string }
  | { kind: 'reset' };

function reducer(state: RollupState, action: RollupAction): RollupState {
  switch (action.kind) {
    case 'reset':
      return { byFiring: new Map() };
    case 'started': {
      const { firing_id, profile_id, action_id, subagent_label, icon_key, model } = action.payload;
      if (!firing_id) return state;
      // Idempotent: if we already saw this firing_id, keep the existing
      // record (a finished may have arrived first on a reorder; never
      // overwrite it with a new started).
      if (state.byFiring.has(firing_id)) return state;
      const next = new Map(state.byFiring);
      next.set(firing_id, {
        firingId: firing_id,
        profileId: profile_id,
        actionId: action_id,
        subagentLabel: subagent_label,
        iconKey: icon_key,
        model,
        startedAt: action.ts,
        costUSD: 0,
      });
      return { byFiring: next };
    }
    case 'finished': {
      const { firing_id, profile_id, action_id, subagent_label, icon_key, model, duration_ms, ok, stage, error, cost_usd } = action.payload;
      if (!firing_id) return state;
      const next = new Map(state.byFiring);
      const existing = next.get(firing_id);
      next.set(firing_id, {
        firingId: firing_id,
        profileId: existing?.profileId ?? profile_id,
        actionId: existing?.actionId ?? action_id,
        subagentLabel: existing?.subagentLabel ?? subagent_label,
        iconKey: existing?.iconKey ?? icon_key,
        model: existing?.model ?? model,
        startedAt: existing?.startedAt ?? action.ts,
        finishedAt: action.ts,
        durationMs: duration_ms,
        ok,
        stage,
        error,
        costUSD: cost_usd ?? 0,
      });
      return { byFiring: next };
    }
  }
}

export interface UseSubAgentRollupReturn {
  firings: SubAgentFiringRecord[];
  byProfile: ProfileRollup[];
  byAction: ActionRollup[];
  totalCostUSD: number;
  totalFirings: number;
  failedFirings: number;
  handleStarted: (e: CPNEventData) => void;
  handleFinished: (e: CPNEventData) => void;
  reset: () => void;
}

// useSubAgentRollup reduces subagent_started / subagent_finished
// events into per-profile and per-action rollups for the visualizer
// header pills, the timeline, and any future drill-down view.
//
// FiringID is the dedupe key: a reconnect or duplicate event SHOULD
// not double-count.
export function useSubAgentRollup(): UseSubAgentRollupReturn {
  const [state, dispatch] = useReducer(reducer, { byFiring: new Map() });

  const handleStarted = useCallback((e: CPNEventData) => {
    const p = e.Payload as SubAgentStartedPayload | undefined;
    if (!p || !p.firing_id) return;
    dispatch({ kind: 'started', payload: p, ts: e.Timestamp });
  }, []);

  const handleFinished = useCallback((e: CPNEventData) => {
    const p = e.Payload as SubAgentFinishedPayload | undefined;
    if (!p || !p.firing_id) return;
    dispatch({ kind: 'finished', payload: p, ts: e.Timestamp });
  }, []);

  const reset = useCallback(() => dispatch({ kind: 'reset' }), []);

  const firings = useMemo(() => Array.from(state.byFiring.values()), [state]);

  const byProfile = useMemo(() => {
    const m = new Map<string, ProfileRollup>();
    for (const f of firings) {
      const cur = m.get(f.profileId) ?? {
        profileId: f.profileId,
        iconKey: f.iconKey,
        firings: 0,
        finished: 0,
        failed: 0,
        costUSD: 0,
        totalDurationMs: 0,
      };
      cur.firings += 1;
      if (f.finishedAt) cur.finished += 1;
      if (f.ok === false) cur.failed += 1;
      cur.costUSD += f.costUSD ?? 0;
      cur.totalDurationMs += f.durationMs ?? 0;
      m.set(f.profileId, cur);
    }
    return Array.from(m.values()).sort((a, b) => b.firings - a.firings);
  }, [firings]);

  const byAction = useMemo(() => {
    const m = new Map<string, ActionRollup>();
    for (const f of firings) {
      const cur = m.get(f.actionId) ?? {
        actionId: f.actionId,
        firings: 0,
        finished: 0,
        failed: 0,
        costUSD: 0,
      };
      cur.firings += 1;
      if (f.finishedAt) cur.finished += 1;
      if (f.ok === false) cur.failed += 1;
      cur.costUSD += f.costUSD ?? 0;
      m.set(f.actionId, cur);
    }
    return Array.from(m.values()).sort((a, b) => b.firings - a.firings);
  }, [firings]);

  const totalCostUSD = useMemo(() => firings.reduce((acc, f) => acc + (f.costUSD ?? 0), 0), [firings]);
  const totalFirings = firings.length;
  const failedFirings = useMemo(() => firings.filter((f) => f.ok === false).length, [firings]);

  return {
    firings,
    byProfile,
    byAction,
    totalCostUSD,
    totalFirings,
    failedFirings,
    handleStarted,
    handleFinished,
    reset,
  };
}
