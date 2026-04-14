import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { I18nTestWrapper } from '../../test/i18n-test-utils';
import { ChatSidebar } from './ChatSidebar';
import type { SessionListItem } from '../../types/api';

// Mock IntersectionObserver
let intersectionCallback: IntersectionObserverCallback;

const mockObserve = vi.fn();
const mockDisconnect = vi.fn();

beforeEach(() => {
  vi.clearAllMocks();

  // Use a class so `new IntersectionObserver(...)` works
  class MockIntersectionObserver {
    constructor(callback: IntersectionObserverCallback) {
      intersectionCallback = callback;
    }
    observe = mockObserve;
    unobserve = vi.fn();
    disconnect = mockDisconnect;
    root = null;
    rootMargin = '';
    thresholds = [] as number[];
    takeRecords = () => [] as IntersectionObserverEntry[];
  }

  vi.stubGlobal('IntersectionObserver', MockIntersectionObserver);
});

function createChat(overrides: Partial<SessionListItem> = {}): SessionListItem {
  return {
    id: 'chat-1',
    title: 'Test Chat',
    state: 'idle',
    last_message_preview: 'Hello there',
    last_activity_at: new Date().toISOString(),
    created_at: new Date().toISOString(),
    total_cost_usd: 0,
    message_count: 1,
    forked_from_session_id: '',
    ...overrides,
  };
}

const defaultProps = {
  chats: [createChat()],
  activeSessionId: 'chat-1',
  isLoading: false,
  hasMore: true,
  onSelect: vi.fn(),
  onCreate: vi.fn(),
  onRename: vi.fn(),
  onDelete: vi.fn(),
  onFork: vi.fn(),
  onLoadMore: vi.fn(),
  embedded: true, // Use embedded mode to avoid toggle button complexity
};

const wrapper = I18nTestWrapper;

describe('ChatSidebar', () => {
  it('should render chat items', () => {
    render(<ChatSidebar {...defaultProps} />, { wrapper });

    expect(screen.getByText('Test Chat')).toBeInTheDocument();
  });

  it('should call onLoadMore when sentinel becomes visible', () => {
    render(<ChatSidebar {...defaultProps} />, { wrapper });

    // Simulate the sentinel becoming visible
    intersectionCallback(
      [{ isIntersecting: true } as IntersectionObserverEntry],
      {} as IntersectionObserver,
    );

    expect(defaultProps.onLoadMore).toHaveBeenCalled();
  });

  it('should not call onLoadMore when sentinel is not visible', () => {
    const onLoadMore = vi.fn();
    render(<ChatSidebar {...defaultProps} onLoadMore={onLoadMore} />, { wrapper });

    // Simulate the sentinel NOT being visible
    intersectionCallback(
      [{ isIntersecting: false } as IntersectionObserverEntry],
      {} as IntersectionObserver,
    );

    expect(onLoadMore).not.toHaveBeenCalled();
  });

  it('should not call onLoadMore when isLoading is true', () => {
    const onLoadMore = vi.fn();
    render(<ChatSidebar {...defaultProps} isLoading={true} onLoadMore={onLoadMore} />, { wrapper });

    intersectionCallback(
      [{ isIntersecting: true } as IntersectionObserverEntry],
      {} as IntersectionObserver,
    );

    expect(onLoadMore).not.toHaveBeenCalled();
  });

  it('should not call onLoadMore when hasMore is false', () => {
    const onLoadMore = vi.fn();
    render(<ChatSidebar {...defaultProps} hasMore={false} onLoadMore={onLoadMore} />, { wrapper });

    intersectionCallback(
      [{ isIntersecting: true } as IntersectionObserverEntry],
      {} as IntersectionObserver,
    );

    expect(onLoadMore).not.toHaveBeenCalled();
  });

  it('should display empty state when no chats', () => {
    render(<ChatSidebar {...defaultProps} chats={[]} />, { wrapper });

    expect(screen.getByText('No conversations yet')).toBeInTheDocument();
  });

  it('should show loading indicator when isLoading', () => {
    render(<ChatSidebar {...defaultProps} isLoading={true} />, { wrapper });

    expect(screen.getByText('Loading...')).toBeInTheDocument();
  });

  it('should disconnect observer on unmount', () => {
    const { unmount } = render(<ChatSidebar {...defaultProps} />, { wrapper });

    unmount();

    expect(mockDisconnect).toHaveBeenCalled();
  });

  // AC-009 — keyboard activation of the chat-list item (role="button")
  describe('chat-list item keyboard a11y', () => {
    it('exposes each chat item as role="button" with tabIndex=0', () => {
      render(<ChatSidebar {...defaultProps} />, { wrapper });

      const item = screen.getByRole('button', { name: 'Test Chat' });
      expect(item).toBeInTheDocument();
      expect(item).toHaveAttribute('tabIndex', '0');
    });

    it('calls onSelect when user presses Enter on a focused chat item', () => {
      const onSelect = vi.fn();
      render(
        <ChatSidebar {...defaultProps} onSelect={onSelect} />,
        { wrapper },
      );

      const item = screen.getByRole('button', { name: 'Test Chat' });
      fireEvent.keyDown(item, { key: 'Enter' });

      expect(onSelect).toHaveBeenCalledWith('chat-1');
    });

    it('calls onSelect when user presses Space on a focused chat item', () => {
      const onSelect = vi.fn();
      render(
        <ChatSidebar {...defaultProps} onSelect={onSelect} />,
        { wrapper },
      );

      const item = screen.getByRole('button', { name: 'Test Chat' });
      fireEvent.keyDown(item, { key: ' ' });

      expect(onSelect).toHaveBeenCalledWith('chat-1');
    });

    it('ignores other keys (e.g., ArrowDown) on the chat item', () => {
      const onSelect = vi.fn();
      render(
        <ChatSidebar {...defaultProps} onSelect={onSelect} />,
        { wrapper },
      );

      const item = screen.getByRole('button', { name: 'Test Chat' });
      fireEvent.keyDown(item, { key: 'ArrowDown' });

      expect(onSelect).not.toHaveBeenCalled();
    });
  });
});
