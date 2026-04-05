import { renderHook, act } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { usePanelManager } from './usePanelManager';

describe('usePanelManager', () => {
  it('should start with panel closed and no content', () => {
    const { result } = renderHook(() => usePanelManager());

    expect(result.current.panelOpen).toBe(false);
    expect(result.current.panelContent).toBeNull();
    expect(result.current.selectedFlowHash).toBeNull();
    expect(result.current.selectedPanelFlowHash).toBeNull();
  });

  it('should open panel with content', () => {
    const { result } = renderHook(() => usePanelManager());

    act(() => {
      result.current.openPanel('flows');
    });

    expect(result.current.panelOpen).toBe(true);
    expect(result.current.panelContent).toBe('flows');
  });

  it('should close panel and clear panel content', () => {
    const { result } = renderHook(() => usePanelManager());

    act(() => {
      result.current.openPanel('monitor');
    });
    expect(result.current.panelOpen).toBe(true);

    act(() => {
      result.current.closePanel();
    });

    expect(result.current.panelOpen).toBe(false);
    expect(result.current.panelContent).toBeNull();
    expect(result.current.selectedPanelFlowHash).toBeNull();
  });

  it('should set flow hash', () => {
    const { result } = renderHook(() => usePanelManager());

    act(() => {
      result.current.setFlowHash('abc123');
    });

    expect(result.current.selectedFlowHash).toBe('abc123');
  });

  it('should set panel flow hash', () => {
    const { result } = renderHook(() => usePanelManager());

    act(() => {
      result.current.setPanelFlowHash('xyz789');
    });

    expect(result.current.selectedPanelFlowHash).toBe('xyz789');
  });

  it('should toggle panel open when content differs', () => {
    const { result } = renderHook(() => usePanelManager());

    act(() => {
      result.current.openPanel('flows');
    });
    expect(result.current.panelContent).toBe('flows');

    // Toggle with different content should switch
    act(() => {
      result.current.togglePanel('monitor');
    });

    expect(result.current.panelOpen).toBe(true);
    expect(result.current.panelContent).toBe('monitor');
  });

  it('should toggle panel closed when content is the same', () => {
    const { result } = renderHook(() => usePanelManager());

    act(() => {
      result.current.openPanel('flows');
    });

    // Toggle with same content should close
    act(() => {
      result.current.togglePanel('flows');
    });

    expect(result.current.panelOpen).toBe(false);
    expect(result.current.panelContent).toBeNull();
  });

  it('should clear all state', () => {
    const { result } = renderHook(() => usePanelManager());

    act(() => {
      result.current.openPanel('monitor');
      result.current.setFlowHash('hash1');
      result.current.setPanelFlowHash('hash2');
    });

    act(() => {
      result.current.clearAll();
    });

    expect(result.current.panelOpen).toBe(false);
    expect(result.current.panelContent).toBeNull();
    expect(result.current.selectedFlowHash).toBeNull();
    expect(result.current.selectedPanelFlowHash).toBeNull();
  });

  it('should clear selectedPanelFlowHash when closing panel', () => {
    const { result } = renderHook(() => usePanelManager());

    act(() => {
      result.current.openPanel('flows');
      result.current.setPanelFlowHash('some-hash');
    });
    expect(result.current.selectedPanelFlowHash).toBe('some-hash');

    act(() => {
      result.current.closePanel();
    });

    expect(result.current.selectedPanelFlowHash).toBeNull();
  });
});
