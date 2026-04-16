import { renderHook, act, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { SessionListItem } from '../types/api';
import { SessionNotFoundError } from '../services/api';

// ────────────────────────────────────────────────────────────────────────────
// Module mocks. We mock the api layer surgically so the hook's real logic
// (reducer, recovery policy, storage migration) runs against fake RPCs.
// ────────────────────────────────────────────────────────────────────────────

const listSessionsMock = vi.fn();
const createSessionMock = vi.fn();
const getSessionMock = vi.fn();
const updateSessionMock = vi.fn();
const forkSessionMock = vi.fn();

vi.mock('../services/api', async () => {
  const actual = await vi.importActual<typeof import('../services/api')>(
    '../services/api',
  );
  return {
    ...actual,
    listSessions: (...args: unknown[]) => listSessionsMock(...args),
    createSession: (...args: unknown[]) => createSessionMock(...args),
    getSession: (...args: unknown[]) => getSessionMock(...args),
    updateSession: (...args: unknown[]) => updateSessionMock(...args),
    forkSession: (...args: unknown[]) => forkSessionMock(...args),
  };
});

import { useChatList } from './useChatList';

// ────────────────────────────────────────────────────────────────────────────
// Fixtures
// ────────────────────────────────────────────────────────────────────────────

const USER_EMAIL = 'alice@example.com';
const STORAGE_KEY = `liwaisi_active_session_${USER_EMAIL}`;

function makeListItem(id: string, title = 'Chat'): SessionListItem {
  return {
    id,
    title,
    state: 'idle',
    last_message_preview: '',
    last_activity_at: '2026-04-13T00:00:00Z',
    created_at: '2026-04-13T00:00:00Z',
    total_cost_usd: 0,
    message_count: 0,
    forked_from_session_id: '',
  };
}

function listResponse(items: SessionListItem[]) {
  return { items, next_cursor: '', has_more: false };
}

function sessionResponse(id: string) {
  return {
    id,
    user_id: USER_EMAIL,
    channel: 'web' as const,
    state: 'idle' as const,
    created_at: '2026-04-13T00:00:00Z',
    messages: [],
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
});

afterEach(() => {
  localStorage.clear();
});

// ────────────────────────────────────────────────────────────────────────────
// Happy paths
// ────────────────────────────────────────────────────────────────────────────

describe('useChatList — startup (REQ-102)', () => {
  it('respects a saved hint that the backend confirms is live', async () => {
    localStorage.setItem(STORAGE_KEY, 'sess_hint');
    listSessionsMock.mockResolvedValue(
      listResponse([makeListItem('sess_hint'), makeListItem('sess_other')]),
    );
    getSessionMock.mockResolvedValue(sessionResponse('sess_hint'));

    const { result } = renderHook(() => useChatList(USER_EMAIL));

    await waitFor(() => {
      expect(result.current.activeSessionId).toBe('sess_hint');
    });
    expect(getSessionMock).toHaveBeenCalledWith('sess_hint');
    expect(createSessionMock).not.toHaveBeenCalled();
  });

  it('falls through to the next list item when the hint is a ghost (REQ-102)', async () => {
    localStorage.setItem(STORAGE_KEY, 'sess_ghost');
    listSessionsMock.mockResolvedValue(
      listResponse([makeListItem('sess_ghost'), makeListItem('sess_live')]),
    );
    getSessionMock.mockImplementation((id: string) => {
      if (id === 'sess_ghost') return Promise.reject(new SessionNotFoundError('sess_ghost'));
      return Promise.resolve(sessionResponse(id));
    });

    const { result } = renderHook(() => useChatList(USER_EMAIL));

    await waitFor(() => expect(result.current.activeSessionId).toBe('sess_live'));
    // Ghost hint is wiped from storage (REQ-102.b)
    expect(localStorage.getItem(STORAGE_KEY)).toBe('sess_live');
    expect(createSessionMock).not.toHaveBeenCalled();
  });

  it('creates a fresh session exactly once when every candidate is a ghost (REQ-103, AC-006)', async () => {
    localStorage.setItem(STORAGE_KEY, 'sess_ghost_a');
    listSessionsMock.mockResolvedValue(
      listResponse([makeListItem('sess_ghost_a'), makeListItem('sess_ghost_b')]),
    );
    getSessionMock.mockRejectedValue(new SessionNotFoundError('any'));
    createSessionMock.mockResolvedValue({
      id: 'sess_fresh',
      user_id: USER_EMAIL,
      channel: 'web',
      state: 'idle',
      created_at: '2026-04-13T00:00:00Z',
    });

    const { result } = renderHook(() => useChatList(USER_EMAIL));

    await waitFor(() => expect(result.current.activeSessionId).toBe('sess_fresh'));
    // AC-006: max one createSession call per startup.
    expect(createSessionMock).toHaveBeenCalledTimes(1);
    // Stale hint is cleared.
    expect(localStorage.getItem(STORAGE_KEY)).toBe('sess_fresh');
  });

  it('creates a session when the list is empty', async () => {
    listSessionsMock.mockResolvedValue(listResponse([]));
    createSessionMock.mockResolvedValue({
      id: 'sess_new',
      user_id: USER_EMAIL,
      channel: 'web',
      state: 'idle',
      created_at: '2026-04-13T00:00:00Z',
    });

    const { result } = renderHook(() => useChatList(USER_EMAIL));

    await waitFor(() => expect(result.current.activeSessionId).toBe('sess_new'));
    expect(createSessionMock).toHaveBeenCalledTimes(1);
  });

  it('does NOT treat transient (non-ghost) errors as ghost signals', async () => {
    listSessionsMock.mockResolvedValue(listResponse([makeListItem('sess_flaky')]));
    getSessionMock.mockRejectedValue(new Error('network blip'));

    const { result } = renderHook(() => useChatList(USER_EMAIL));

    // On transient error we still pin the candidate as active and let SSE
    // drive the retry policy — we MUST NOT create a fresh session here.
    await waitFor(() => expect(result.current.activeSessionId).toBe('sess_flaky'));
    expect(createSessionMock).not.toHaveBeenCalled();
  });
});

// ────────────────────────────────────────────────────────────────────────────
// Storage-key migration (REQ-110)
// ────────────────────────────────────────────────────────────────────────────

describe('useChatList — storage key migration (REQ-110)', () => {
  it('promotes a legacy key written under a different suffix and removes it', async () => {
    // Legacy key — different suffix from the current user.
    localStorage.setItem('liwaisi_active_session_legacy_user_id', 'sess_legacy');
    listSessionsMock.mockResolvedValue(
      listResponse([makeListItem('sess_legacy'), makeListItem('sess_other')]),
    );
    getSessionMock.mockResolvedValue(sessionResponse('sess_legacy'));

    const { result } = renderHook(() => useChatList(USER_EMAIL));

    await waitFor(() => expect(result.current.activeSessionId).toBe('sess_legacy'));

    // Legacy entry is gone, unified entry is set.
    expect(localStorage.getItem('liwaisi_active_session_legacy_user_id')).toBeNull();
    expect(localStorage.getItem(STORAGE_KEY)).toBe('sess_legacy');
  });

  it('keeps the current key when both legacy and current exist — migration does not overwrite', async () => {
    localStorage.setItem(STORAGE_KEY, 'sess_current');
    localStorage.setItem('liwaisi_active_session_legacy', 'sess_legacy');
    listSessionsMock.mockResolvedValue(
      listResponse([makeListItem('sess_current'), makeListItem('sess_legacy')]),
    );
    getSessionMock.mockResolvedValue(sessionResponse('sess_current'));

    renderHook(() => useChatList(USER_EMAIL));

    await waitFor(() => {
      expect(localStorage.getItem(STORAGE_KEY)).toBe('sess_current');
    });
    expect(localStorage.getItem('liwaisi_active_session_legacy')).toBeNull();
  });
});

// ────────────────────────────────────────────────────────────────────────────
// Bounded recovery (REQ-103, AC-006)
// ────────────────────────────────────────────────────────────────────────────

describe('useChatList — recoverFromGhost', () => {
  it('drops the ghost from state, the hint from storage, and picks a live fallback', async () => {
    localStorage.setItem(STORAGE_KEY, 'sess_ghost');
    listSessionsMock.mockResolvedValue(
      listResponse([makeListItem('sess_ghost'), makeListItem('sess_live')]),
    );
    getSessionMock.mockResolvedValue(sessionResponse('sess_ghost'));

    const { result } = renderHook(() => useChatList(USER_EMAIL));
    await waitFor(() => expect(result.current.activeSessionId).toBe('sess_ghost'));

    await act(async () => {
      await result.current.recoverFromGhost('sess_ghost');
    });

    expect(result.current.activeSessionId).toBe('sess_live');
    expect(result.current.chats.find((c) => c.id === 'sess_ghost')).toBeUndefined();
    expect(localStorage.getItem(STORAGE_KEY)).toBe('sess_live');
    // No new session was created — a live fallback was available.
    expect(createSessionMock).not.toHaveBeenCalled();
  });

  it('creates a single fresh session when no fallback exists', async () => {
    localStorage.setItem(STORAGE_KEY, 'sess_only_ghost');
    listSessionsMock.mockResolvedValue(listResponse([makeListItem('sess_only_ghost')]));
    getSessionMock.mockResolvedValue(sessionResponse('sess_only_ghost'));
    createSessionMock.mockResolvedValue({
      id: 'sess_brand_new',
      user_id: USER_EMAIL,
      channel: 'web',
      state: 'idle',
      created_at: '2026-04-13T00:00:00Z',
    });

    const { result } = renderHook(() => useChatList(USER_EMAIL));
    await waitFor(() => expect(result.current.activeSessionId).toBe('sess_only_ghost'));

    await act(async () => {
      await result.current.recoverFromGhost('sess_only_ghost');
    });

    expect(result.current.activeSessionId).toBe('sess_brand_new');
    expect(createSessionMock).toHaveBeenCalledTimes(1);
  });
});
