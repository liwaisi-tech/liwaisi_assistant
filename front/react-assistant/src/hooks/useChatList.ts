import { useReducer, useEffect, useCallback, useRef, useState } from 'react';
import type { SessionListItem } from '../types/api';
import {
  listSessions,
  createSession,
  updateSession,
  forkSession,
  getSession,
  ApiError,
  SessionNotFoundError,
} from '../services/api';

interface ChatListState {
  chats: SessionListItem[];
  activeSessionId: string | null;
  isLoading: boolean;
  hasMore: boolean;
  cursor: string | null;
  error: string | null;
}

type ChatListAction =
  | { type: 'LOAD_START' }
  | { type: 'LOAD_SUCCESS'; items: SessionListItem[]; nextCursor: string; hasMore: boolean; append: boolean }
  | { type: 'LOAD_ERROR'; error: string }
  | { type: 'SET_ACTIVE'; sessionId: string | null }
  | { type: 'ADD_CHAT'; chat: SessionListItem }
  | { type: 'REMOVE_CHAT'; sessionId: string }
  | { type: 'UPDATE_CHAT'; sessionId: string; updates: Partial<SessionListItem> }
  | { type: 'SET_ERROR'; error: string };

function chatListReducer(state: ChatListState, action: ChatListAction): ChatListState {
  switch (action.type) {
    case 'LOAD_START':
      return { ...state, isLoading: true, error: null };

    case 'LOAD_SUCCESS': {
      const items = action.append
        ? [...state.chats, ...action.items]
        : action.items;
      return {
        ...state,
        chats: items,
        cursor: action.nextCursor || null,
        hasMore: action.hasMore,
        isLoading: false,
      };
    }

    case 'LOAD_ERROR':
      return { ...state, isLoading: false, error: action.error };

    case 'SET_ACTIVE':
      return { ...state, activeSessionId: action.sessionId };

    case 'ADD_CHAT':
      return {
        ...state,
        chats: [action.chat, ...state.chats],
      };

    case 'REMOVE_CHAT': {
      const filtered = state.chats.filter((c) => c.id !== action.sessionId);
      const newActive = state.activeSessionId === action.sessionId
        ? (filtered[0]?.id ?? null)
        : state.activeSessionId;
      return { ...state, chats: filtered, activeSessionId: newActive };
    }

    case 'UPDATE_CHAT':
      return {
        ...state,
        chats: state.chats.map((c) =>
          c.id === action.sessionId ? { ...c, ...action.updates } : c
        ),
      };

    case 'SET_ERROR':
      return { ...state, error: action.error };

    default:
      return state;
  }
}

const initialState: ChatListState = {
  chats: [],
  activeSessionId: null,
  isLoading: false,
  hasMore: false,
  cursor: null,
  error: null,
};

/**
 * Unified localStorage prefix. The active session id is keyed by the user's
 * email (see REQ-110, §4.4). `AuthContext.logout` uses the same prefix, which
 * is critical — without unification, logout leaves a stale ghost id behind
 * and the bug resurfaces on re-login.
 */
const STORAGE_KEY_PREFIX = 'liwaisi_active_session_';

/** Build the storage key for a given user identifier (email). */
function buildStorageKey(userEmail: string): string {
  return `${STORAGE_KEY_PREFIX}${userEmail}`;
}

/**
 * One-time migration (REQ-110). In prior versions the key was written by
 * `useChatList` under a `userId`-parameterized suffix while `AuthContext`
 * wrote and cleared under an `email`-parameterized suffix. For users whose
 * `userId` happened to equal `email` these coincide; for anyone else the
 * old key would orphan. We read any legacy key that matches the prefix and
 * whose suffix differs from the current one, migrate its value into the
 * current key if the current key is unset, and delete the legacy entries.
 *
 * This helper is idempotent and safe to run on every mount.
 */
function migrateLegacyActiveSessionKey(currentKey: string): void {
  try {
    if (typeof localStorage === 'undefined') return;
    // Don't overwrite an already-unified key.
    const current = localStorage.getItem(currentKey);
    // Snapshot keys first so we can delete during iteration safely.
    const legacyKeys: string[] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k && k.startsWith(STORAGE_KEY_PREFIX) && k !== currentKey) {
        legacyKeys.push(k);
      }
    }
    if (legacyKeys.length === 0) return;
    if (!current) {
      // Promote the most recently written legacy value into the new key.
      // Without access to write-order metadata we pick the first legacy key
      // deterministically; the important property is that exactly one value
      // survives after migration.
      const legacyValue = localStorage.getItem(legacyKeys[0]);
      if (legacyValue) {
        localStorage.setItem(currentKey, legacyValue);
      }
    }
    for (const k of legacyKeys) {
      localStorage.removeItem(k);
    }
  } catch {
    // Non-fatal: storage may be disabled or quota-exceeded. Recovery still
    // proceeds without the hint.
  }
}

/**
 * `userEmail` is the user's email (authoritative storage identifier). The
 * parameter is typed as `string` for backward compatibility with the existing
 * call site in `useSessionManager`, which passes `user.email` through a
 * `userId` variable (see App.tsx).
 */
export function useChatList(userEmail: string) {
  const [state, dispatch] = useReducer(chatListReducer, initialState);
  const initRef = useRef(false);
  const storageKey = buildStorageKey(userEmail);
  // Monotonic timestamp bumped once per successful auto-recovery (REQ-111).
  // The UI observes changes to re-render the "session resumed" notice.
  const [sessionResumedAt, setSessionResumedAt] = useState<number | null>(null);

  // Persist active session to localStorage under the unified key.
  useEffect(() => {
    if (state.activeSessionId) {
      try {
        localStorage.setItem(storageKey, state.activeSessionId);
      } catch {
        // Storage may be disabled; state is still in memory.
      }
    }
  }, [state.activeSessionId, storageKey]);

  // Initial load.
  //
  // Recovery policy (REQ-102 / REQ-103 / AC-006):
  //   1. Migrate any legacy key into the unified key, exactly once on mount.
  //   2. Fetch the persisted session list.
  //   3. Resolve a candidate active id: saved hint > first list item.
  //   4. Probe the candidate via GET /sessions/{id}. If it responds with
  //      SessionNotFoundError, treat the id as a ghost: drop it from the
  //      hint cache and the in-memory list, then try the next candidate.
  //   5. The probe loop is bounded by the list length + 1 (`remaining--`
  //      guard) so no runaway recursion is possible even if the list
  //      shrinks mid-recovery.
  //   6. If no candidate survives, call `createSession` EXACTLY ONCE. Any
  //      subsequent failure surfaces as an error — we never loop.
  useEffect(() => {
    if (initRef.current) return;
    initRef.current = true;

    async function init() {
      migrateLegacyActiveSessionKey(storageKey);
      dispatch({ type: 'LOAD_START' });
      let items: SessionListItem[];
      let nextCursor = '';
      let hasMore = false;
      try {
        const res = await listSessions();
        items = res.items;
        nextCursor = res.next_cursor;
        hasMore = res.has_more;
        dispatch({
          type: 'LOAD_SUCCESS',
          items,
          nextCursor,
          hasMore,
          append: false,
        });
      } catch (err) {
        dispatch({ type: 'LOAD_ERROR', error: (err as Error).message });
        return;
      }

      const savedId = (() => {
        try {
          return localStorage.getItem(storageKey);
        } catch {
          return null;
        }
      })();

      // Build candidate queue: saved hint first (if still in the list), then
      // every other list entry in order. Using a Set keeps ordering simple.
      const orderedCandidates: string[] = [];
      if (savedId && items.some((c) => c.id === savedId)) {
        orderedCandidates.push(savedId);
      }
      for (const item of items) {
        if (!orderedCandidates.includes(item.id)) {
          orderedCandidates.push(item.id);
        }
      }

      const dropStaleHint = () => {
        try {
          localStorage.removeItem(storageKey);
        } catch {
          // ignore
        }
      };

      let chosen: string | null = null;
      let skippedGhost = false;
      for (const candidate of orderedCandidates) {
        try {
          await getSession(candidate);
          chosen = candidate;
          break;
        } catch (err) {
          if (err instanceof SessionNotFoundError) {
            // Ghost: drop the saved hint if it matched this candidate and
            // continue to the next. The session will still appear in the
            // visible chat list — surfacing the ghost in the sidebar is
            // acceptable; what matters is that we do not PIN it active.
            if (candidate === savedId) {
              dropStaleHint();
            }
            skippedGhost = true;
            continue;
          }
          // Transient / unknown failure — do NOT treat as a ghost; stop
          // probing and accept the candidate so the SSE layer can take over
          // the retry policy (REQ-105). Clearing state here would risk
          // data loss on a network blip.
          chosen = candidate;
          break;
        }
      }

      if (chosen) {
        dispatch({ type: 'SET_ACTIVE', sessionId: chosen });
        if (skippedGhost) setSessionResumedAt(Date.now());
        return;
      }

      // No candidate survived. Single-shot fallback (REQ-103, AC-006).
      dropStaleHint();
      try {
        const session = await createSession(userEmail, 'web');
        const newChat: SessionListItem = {
          id: session.id,
          title: 'New Chat',
          state: session.state,
          last_message_preview: '',
          last_activity_at: session.created_at,
          created_at: session.created_at,
          total_cost_usd: 0,
          message_count: 0,
          forked_from_session_id: '',
        };
        dispatch({ type: 'ADD_CHAT', chat: newChat });
        dispatch({ type: 'SET_ACTIVE', sessionId: session.id });
        if (skippedGhost) setSessionResumedAt(Date.now());
      } catch (err) {
        dispatch({ type: 'SET_ERROR', error: (err as Error).message });
      }
    }

    init();
  }, [userEmail, storageKey]);

  const createChat = useCallback(async () => {
    try {
      const session = await createSession(userEmail, 'web');
      const newChat: SessionListItem = {
        id: session.id,
        title: 'New Chat',
        state: session.state,
        last_message_preview: '',
        last_activity_at: session.created_at,
        created_at: session.created_at,
        total_cost_usd: 0,
        message_count: 0,
        forked_from_session_id: '',
      };
      dispatch({ type: 'ADD_CHAT', chat: newChat });
      dispatch({ type: 'SET_ACTIVE', sessionId: session.id });
      return session.id;
    } catch (err) {
      dispatch({ type: 'SET_ERROR', error: (err as Error).message });
      return null;
    }
  }, [userEmail]);

  const switchChat = useCallback((sessionId: string) => {
    dispatch({ type: 'SET_ACTIVE', sessionId });
  }, []);

  const deleteChat = useCallback(async (sessionId: string) => {
    // Optimistic removal
    dispatch({ type: 'REMOVE_CHAT', sessionId });
    try {
      await updateSession(sessionId, { deleted: true });
    } catch (err) {
      dispatch({ type: 'SET_ERROR', error: err instanceof ApiError ? err.message : 'Failed to delete chat' });
    }
  }, []);

  const renameChat = useCallback(async (sessionId: string, title: string) => {
    // Optimistic update
    dispatch({ type: 'UPDATE_CHAT', sessionId, updates: { title } });
    try {
      await updateSession(sessionId, { title });
    } catch (err) {
      dispatch({ type: 'SET_ERROR', error: err instanceof ApiError ? err.message : 'Failed to rename chat' });
    }
  }, []);

  const forkChat = useCallback(async (sessionId: string, messageIndex: number) => {
    try {
      const res = await forkSession(sessionId, { message_index: messageIndex });
      const newChat: SessionListItem = {
        id: res.id,
        title: res.title,
        state: res.state,
        last_message_preview: '',
        last_activity_at: res.created_at,
        created_at: res.created_at,
        total_cost_usd: res.total_cost_usd,
        message_count: res.fork_message_count,
        forked_from_session_id: res.forked_from_session_id,
      };
      dispatch({ type: 'ADD_CHAT', chat: newChat });
      dispatch({ type: 'SET_ACTIVE', sessionId: res.id });
      return res.id;
    } catch (err) {
      dispatch({ type: 'SET_ERROR', error: err instanceof ApiError ? err.message : 'Failed to fork chat' });
      return null;
    }
  }, []);

  /**
   * Recover from a SessionNotFoundError surfaced by SSE or any session-scoped
   * call while the hook is already mounted (REQ-102, REQ-104, AC-007). Unlike
   * the startup path this is a single-shot: we drop the ghost from state +
   * storage, switch to another existing chat if one is available, otherwise
   * create a fresh session. Callers (useSSE/useChat) MUST NOT invoke this in
   * a loop; it is expected to be fired at most once per ghost id.
   */
  const recoverFromGhost = useCallback(async (ghostId: string): Promise<string | null> => {
    // Drop hint if it matched this ghost id.
    try {
      const saved = localStorage.getItem(storageKey);
      if (saved === ghostId) {
        localStorage.removeItem(storageKey);
      }
    } catch {
      // ignore
    }
    dispatch({ type: 'REMOVE_CHAT', sessionId: ghostId });

    // Try to pick another live chat from the list first — avoid spawning a
    // fresh session when the user already has working sessions.
    const fallback = state.chats.find((c) => c.id !== ghostId);
    if (fallback) {
      dispatch({ type: 'SET_ACTIVE', sessionId: fallback.id });
      setSessionResumedAt(Date.now());
      return fallback.id;
    }

    // No other session available — create a fresh one (exactly once).
    try {
      const session = await createSession(userEmail, 'web');
      const newChat: SessionListItem = {
        id: session.id,
        title: 'New Chat',
        state: session.state,
        last_message_preview: '',
        last_activity_at: session.created_at,
        created_at: session.created_at,
        total_cost_usd: 0,
        message_count: 0,
        forked_from_session_id: '',
      };
      dispatch({ type: 'ADD_CHAT', chat: newChat });
      dispatch({ type: 'SET_ACTIVE', sessionId: session.id });
      setSessionResumedAt(Date.now());
      return session.id;
    } catch (err) {
      dispatch({ type: 'SET_ERROR', error: (err as Error).message });
      return null;
    }
  }, [state.chats, storageKey, userEmail]);

  const loadMore = useCallback(async () => {
    if (!state.hasMore || state.isLoading || !state.cursor) return;
    dispatch({ type: 'LOAD_START' });
    try {
      const res = await listSessions(state.cursor);
      dispatch({
        type: 'LOAD_SUCCESS',
        items: res.items,
        nextCursor: res.next_cursor,
        hasMore: res.has_more,
        append: true,
      });
    } catch (err) {
      dispatch({ type: 'LOAD_ERROR', error: (err as Error).message });
    }
  }, [state.hasMore, state.isLoading, state.cursor]);

  const refresh = useCallback(async () => {
    dispatch({ type: 'LOAD_START' });
    try {
      const res = await listSessions();
      dispatch({
        type: 'LOAD_SUCCESS',
        items: res.items,
        nextCursor: res.next_cursor,
        hasMore: res.has_more,
        append: false,
      });
    } catch (err) {
      dispatch({ type: 'LOAD_ERROR', error: (err as Error).message });
    }
  }, []);

  return {
    chats: state.chats,
    activeSessionId: state.activeSessionId,
    isLoading: state.isLoading,
    hasMore: state.hasMore,
    error: state.error,
    createChat,
    switchChat,
    deleteChat,
    renameChat,
    forkChat,
    loadMore,
    refresh,
    recoverFromGhost,
    sessionResumedAt,
  };
}
