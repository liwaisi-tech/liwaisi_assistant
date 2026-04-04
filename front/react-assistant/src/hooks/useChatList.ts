import { useReducer, useEffect, useCallback, useRef } from 'react';
import type { SessionListItem } from '../types/api';
import { listSessions, createSession, updateSession, forkSession, ApiError } from '../services/api';

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

const STORAGE_KEY_PREFIX = 'liwaisi_active_session_';

export function useChatList(userId: string) {
  const [state, dispatch] = useReducer(chatListReducer, initialState);
  const initRef = useRef(false);
  const storageKey = `${STORAGE_KEY_PREFIX}${userId}`;

  // Persist active session to localStorage
  useEffect(() => {
    if (state.activeSessionId) {
      localStorage.setItem(storageKey, state.activeSessionId);
    }
  }, [state.activeSessionId, storageKey]);

  // Initial load
  useEffect(() => {
    if (initRef.current) return;
    initRef.current = true;

    async function init() {
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

        const savedId = localStorage.getItem(storageKey);
        if (savedId && res.items.some((c) => c.id === savedId)) {
          dispatch({ type: 'SET_ACTIVE', sessionId: savedId });
        } else if (res.items.length > 0) {
          dispatch({ type: 'SET_ACTIVE', sessionId: res.items[0].id });
        } else {
          // No chats exist — create one
          try {
            const session = await createSession(userId, 'web');
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
          } catch (err) {
            dispatch({ type: 'SET_ERROR', error: (err as Error).message });
          }
        }
      } catch (err) {
        dispatch({ type: 'LOAD_ERROR', error: (err as Error).message });
      }
    }

    init();
  }, [userId, storageKey]);

  const createChat = useCallback(async () => {
    try {
      const session = await createSession(userId, 'web');
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
  }, [userId]);

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
  };
}
