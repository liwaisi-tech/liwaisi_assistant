import { renderHook, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { useTooltip } from './useTooltip';

describe('useTooltip', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('should start with visible=false', () => {
    const { result } = renderHook(() => useTooltip());
    expect(result.current.visible).toBe(false);
  });

  it('should show tooltip after delay on mouseEnter', () => {
    const { result } = renderHook(() => useTooltip(600));

    act(() => {
      result.current.onMouseEnter();
    });

    // Not yet visible before delay elapses
    expect(result.current.visible).toBe(false);

    act(() => {
      vi.advanceTimersByTime(600);
    });

    expect(result.current.visible).toBe(true);
  });

  it('should hide tooltip immediately on mouseLeave', () => {
    const { result } = renderHook(() => useTooltip(600));

    // Show the tooltip first
    act(() => {
      result.current.onMouseEnter();
    });
    act(() => {
      vi.advanceTimersByTime(600);
    });
    expect(result.current.visible).toBe(true);

    // Now leave
    act(() => {
      result.current.onMouseLeave();
    });

    expect(result.current.visible).toBe(false);
  });

  it('should cancel pending show on mouseLeave before delay', () => {
    const { result } = renderHook(() => useTooltip(600));

    act(() => {
      result.current.onMouseEnter();
    });

    // Leave before delay completes
    act(() => {
      vi.advanceTimersByTime(300);
    });

    act(() => {
      result.current.onMouseLeave();
    });

    // Advance past the original delay — tooltip should NOT appear
    act(() => {
      vi.advanceTimersByTime(600);
    });

    expect(result.current.visible).toBe(false);
  });

  it('should clean up timer on unmount', () => {
    const clearTimeoutSpy = vi.spyOn(global, 'clearTimeout');
    const { result, unmount } = renderHook(() => useTooltip(600));

    act(() => {
      result.current.onMouseEnter();
    });

    unmount();

    // clearTimeout should have been called during cleanup
    expect(clearTimeoutSpy).toHaveBeenCalled();
    clearTimeoutSpy.mockRestore();
  });

  it('should respect custom delay', () => {
    const { result } = renderHook(() => useTooltip(200));

    act(() => {
      result.current.onMouseEnter();
    });

    act(() => {
      vi.advanceTimersByTime(199);
    });
    expect(result.current.visible).toBe(false);

    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(result.current.visible).toBe(true);
  });
});
