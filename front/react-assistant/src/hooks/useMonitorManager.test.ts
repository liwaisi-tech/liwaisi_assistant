import { renderHook } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useMonitorManager } from './useMonitorManager';

// Mock the useExecutionMonitor dependency
const mockHandleTransitionStarted = vi.fn();
const mockHandleTransitionCompleted = vi.fn();
const mockHandleSubNetStarted = vi.fn();
const mockHandleSubNetCompleted = vi.fn();
const mockLoadTopology = vi.fn();
const mockLoadExecutionTrace = vi.fn().mockResolvedValue(undefined);

vi.mock('./useExecutionMonitor', () => ({
  useExecutionMonitor: vi.fn(() => ({
    state: {
      runs: [],
      selectedRunIndex: null,
      topology: null,
      selectedTransitionId: null,
      cpnHierarchy: [],
      activeCPNId: null,
    },
    selectedRun: null,
    startNewExecution: vi.fn(),
    handleTransitionStarted: mockHandleTransitionStarted,
    handleTransitionCompleted: mockHandleTransitionCompleted,
    handleSubNetStarted: mockHandleSubNetStarted,
    handleSubNetCompleted: mockHandleSubNetCompleted,
    loadTopology: mockLoadTopology,
    loadExecutionTrace: mockLoadExecutionTrace,
    selectRun: vi.fn(),
    selectTransition: vi.fn(),
    navigateCPN: vi.fn(),
    reset: vi.fn(),
  })),
}));

// Mock the API service
vi.mock('../services/api', () => ({
  getFlows: vi.fn().mockResolvedValue({ items: [] }),
  getFlow: vi.fn().mockResolvedValue({ topology: {} }),
}));

describe('useMonitorManager', () => {
  const onSessionCompleted = vi.fn();

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('should expose monitor state from useExecutionMonitor', () => {
    const { result } = renderHook(() => useMonitorManager(onSessionCompleted));

    expect(result.current.monitor).toBeDefined();
    expect(result.current.monitor.state.runs).toEqual([]);
    expect(result.current.monitor.selectedRun).toBeNull();
  });

  it('should provide stable sseCallbacks object', () => {
    const { result } = renderHook(() => useMonitorManager(onSessionCompleted));

    expect(result.current.sseCallbacks).toBeDefined();
    expect(result.current.sseCallbacks.onTransitionStarted).toBe(mockHandleTransitionStarted);
    expect(result.current.sseCallbacks.onTransitionCompleted).toBe(mockHandleTransitionCompleted);
    expect(result.current.sseCallbacks.onSubNetStarted).toBe(mockHandleSubNetStarted);
    expect(result.current.sseCallbacks.onSubNetCompleted).toBe(mockHandleSubNetCompleted);
    expect(result.current.sseCallbacks.onSessionCompleted).toBe(onSessionCompleted);
  });

  it('should provide handleLoadTrace that delegates to monitor', async () => {
    const { result } = renderHook(() => useMonitorManager(onSessionCompleted));

    await result.current.handleLoadTrace('session-123');

    expect(mockLoadExecutionTrace).toHaveBeenCalledWith('session-123');
  });

  it('should provide loadMonitorDataForSession', () => {
    const { result } = renderHook(() => useMonitorManager(onSessionCompleted));

    expect(result.current.loadMonitorDataForSession).toBeDefined();
    expect(typeof result.current.loadMonitorDataForSession).toBe('function');
  });

  it('should memoize sseCallbacks across renders', () => {
    const { result, rerender } = renderHook(() => useMonitorManager(onSessionCompleted));

    const firstCallbacks = result.current.sseCallbacks;
    rerender();
    const secondCallbacks = result.current.sseCallbacks;

    expect(firstCallbacks).toBe(secondCallbacks);
  });
});
