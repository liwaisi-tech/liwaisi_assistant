import { render, screen } from '@testing-library/react';
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
});
