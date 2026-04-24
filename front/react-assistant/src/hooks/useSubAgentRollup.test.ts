import { describe, it, expect } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useSubAgentRollup } from './useSubAgentRollup';
import type { CPNEventData } from '../types/sse';

function startedEvent(firingId: string, profile: string, action: string, model = 'haiku'): CPNEventData {
  return {
    ID: `id-${firingId}`,
    Type: 'subagent_started',
    SessionID: 's',
    CPNID: 'c',
    CPNDepth: 0,
    CPNRole: 'r',
    TransitionID: `t-${action}-${profile}`,
    TransitionKind: 'llm',
    Payload: {
      profile_id: profile,
      action_id: action,
      firing_id: firingId,
      model,
    },
    Timestamp: '2026-04-24T00:00:00Z',
  };
}

function finishedEvent(
  firingId: string,
  profile: string,
  action: string,
  ok: boolean,
  costUSD = 0.0001,
  durationMs = 1234,
): CPNEventData {
  return {
    ID: `id-${firingId}-done`,
    Type: 'subagent_finished',
    SessionID: 's',
    CPNID: 'c',
    CPNDepth: 0,
    CPNRole: 'r',
    TransitionID: `t-${action}-${profile}`,
    TransitionKind: 'llm',
    Payload: {
      profile_id: profile,
      action_id: action,
      firing_id: firingId,
      duration_ms: durationMs,
      ok,
      cost_usd: costUSD,
      stage: ok ? '' : 'gate',
      error: ok ? '' : 'forbidden',
    },
    Timestamp: '2026-04-24T00:00:01Z',
  };
}

describe('useSubAgentRollup', () => {
  it('aggregates firings by profile and action', () => {
    const { result } = renderHook(() => useSubAgentRollup());
    act(() => {
      result.current.handleStarted(startedEvent('f1', 'qa', 'review-spec'));
      result.current.handleFinished(finishedEvent('f1', 'qa', 'review-spec', true));
      result.current.handleStarted(startedEvent('f2', 'go-eng', 'review-spec'));
      result.current.handleFinished(finishedEvent('f2', 'go-eng', 'review-spec', true));
    });
    expect(result.current.totalFirings).toBe(2);
    expect(result.current.failedFirings).toBe(0);
    expect(result.current.byProfile.map((p) => p.profileId).sort()).toEqual(['go-eng', 'qa']);
    expect(result.current.byAction).toHaveLength(1);
    expect(result.current.byAction[0].firings).toBe(2);
    expect(result.current.totalCostUSD).toBeCloseTo(0.0002, 6);
  });

  it('dedupes by firing_id (idempotent on duplicate started)', () => {
    const { result } = renderHook(() => useSubAgentRollup());
    act(() => {
      result.current.handleStarted(startedEvent('f1', 'qa', 'review-spec'));
      result.current.handleStarted(startedEvent('f1', 'qa', 'review-spec')); // dup
    });
    expect(result.current.totalFirings).toBe(1);
  });

  it('records failed firings with stage', () => {
    const { result } = renderHook(() => useSubAgentRollup());
    act(() => {
      result.current.handleStarted(startedEvent('f1', 'go-eng', 'review-code'));
      result.current.handleFinished(finishedEvent('f1', 'go-eng', 'review-code', false, 0));
    });
    expect(result.current.failedFirings).toBe(1);
    const profile = result.current.byProfile.find((p) => p.profileId === 'go-eng');
    expect(profile?.failed).toBe(1);
    expect(result.current.firings[0].stage).toBe('gate');
  });

  it('handles finished arriving before started (out-of-order)', () => {
    const { result } = renderHook(() => useSubAgentRollup());
    act(() => {
      result.current.handleFinished(finishedEvent('f1', 'qa', 'review-spec', true));
      result.current.handleStarted(startedEvent('f1', 'qa', 'review-spec'));
    });
    expect(result.current.totalFirings).toBe(1);
    expect(result.current.firings[0].profileId).toBe('qa');
    expect(result.current.firings[0].finishedAt).toBeDefined();
  });

  it('reset clears all state', () => {
    const { result } = renderHook(() => useSubAgentRollup());
    act(() => {
      result.current.handleStarted(startedEvent('f1', 'qa', 'review-spec'));
      result.current.reset();
    });
    expect(result.current.totalFirings).toBe(0);
    expect(result.current.byProfile).toHaveLength(0);
  });
});
