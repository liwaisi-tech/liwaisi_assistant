import { renderHook, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useSessionManager } from './useSessionManager';

// Mock the useChatList dependency since it calls APIs
vi.mock('./useChatList', () => ({
  useChatList: vi.fn(() => ({
    chats: [],
    activeSessionId: null,
    isLoading: false,
    hasMore: false,
    error: null,
    createChat: vi.fn().mockResolvedValue('new-session-id'),
    switchChat: vi.fn(),
    deleteChat: vi.fn(),
    renameChat: vi.fn(),
    forkChat: vi.fn(),
    loadMore: vi.fn(),
    refresh: vi.fn(),
  })),
}));

describe('useSessionManager', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('should start with default navigation state', () => {
    const { result } = renderHook(() => useSessionManager('user-1'));

    expect(result.current.activeApp).toBe('chat');
    expect(result.current.isRailExpanded).toBe(false);
    expect(result.current.isPaletteOpen).toBe(false);
    expect(result.current.forkingSessionId).toBeNull();
  });

  it('should navigate to a different app and collapse rail', () => {
    const { result } = renderHook(() => useSessionManager('user-1'));

    act(() => {
      result.current.setIsRailExpanded(true);
    });
    expect(result.current.isRailExpanded).toBe(true);

    act(() => {
      result.current.navigateTo('flows');
    });

    expect(result.current.activeApp).toBe('flows');
    expect(result.current.isRailExpanded).toBe(false);
  });

  it('should toggle rail when navigating to same app (chat)', () => {
    const { result } = renderHook(() => useSessionManager('user-1'));

    // activeApp is already 'chat', so handleRailNavigate('chat') should toggle rail
    act(() => {
      result.current.handleRailNavigate('chat');
    });

    expect(result.current.isRailExpanded).toBe(true);
    expect(result.current.activeApp).toBe('chat');

    // Toggle again
    act(() => {
      result.current.handleRailNavigate('chat');
    });

    expect(result.current.isRailExpanded).toBe(false);
  });

  it('should switch app when rail navigating to different app', () => {
    const { result } = renderHook(() => useSessionManager('user-1'));

    act(() => {
      result.current.handleRailNavigate('monitor');
    });

    expect(result.current.activeApp).toBe('monitor');
    expect(result.current.isRailExpanded).toBe(false);
  });

  it('should toggle palette open and closed', () => {
    const { result } = renderHook(() => useSessionManager('user-1'));

    act(() => {
      result.current.togglePalette();
    });
    expect(result.current.isPaletteOpen).toBe(true);

    act(() => {
      result.current.togglePalette();
    });
    expect(result.current.isPaletteOpen).toBe(false);
  });

  it('should close palette', () => {
    const { result } = renderHook(() => useSessionManager('user-1'));

    act(() => {
      result.current.togglePalette();
    });
    expect(result.current.isPaletteOpen).toBe(true);

    act(() => {
      result.current.closePalette();
    });
    expect(result.current.isPaletteOpen).toBe(false);
  });

  it('should handle settings click by switching to personality app', () => {
    const { result } = renderHook(() => useSessionManager('user-1'));

    act(() => {
      result.current.handleSettingsClick();
    });

    expect(result.current.activeApp).toBe('personality');
    expect(result.current.isRailExpanded).toBe(false);
  });

  it('should handle tools click by switching to tools app', () => {
    const { result } = renderHook(() => useSessionManager('user-1'));

    act(() => {
      result.current.handleToolsClick();
    });

    expect(result.current.activeApp).toBe('tools');
    expect(result.current.isRailExpanded).toBe(false);
  });

  it('should set forking session id on fork request', () => {
    const { result } = renderHook(() => useSessionManager('user-1'));

    act(() => {
      result.current.handleForkRequest('session-abc');
    });

    expect(result.current.forkingSessionId).toBe('session-abc');
  });

  it('should clear forking session id on close fork dialog', () => {
    const { result } = renderHook(() => useSessionManager('user-1'));

    act(() => {
      result.current.handleForkRequest('session-abc');
    });

    act(() => {
      result.current.closeForkDialog();
    });

    expect(result.current.forkingSessionId).toBeNull();
  });

  it('should expose chatList from useChatList', () => {
    const { result } = renderHook(() => useSessionManager('user-1'));

    expect(result.current.chatList).toBeDefined();
    expect(result.current.chatList.chats).toEqual([]);
  });
});
