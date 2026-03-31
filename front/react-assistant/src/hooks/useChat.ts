import { useReducer, useEffect, useCallback, useRef } from 'react';
import type { SessionState } from '../types/api';
import type { StreamChunkData, CPNEventData } from '../types/sse';
import type { ChatMessage, HITLAction } from '../types/chat';
import { createSession, getSession, sendMessage as apiSendMessage, resolveHITL as apiResolveHITL, ApiError } from '../services/api';
import { useSSE } from './useSSE';

interface ChatState {
  sessionId: string | null;
  messages: ChatMessage[];
  sessionState: SessionState;
  error: string | null;
}

type ChatAction =
  | { type: 'SESSION_CREATED'; sessionId: string }
  | { type: 'SESSION_RESTORED'; sessionId: string; messages: ChatMessage[]; state: SessionState }
  | { type: 'STREAM_CHUNK'; data: StreamChunkData }
  | { type: 'USER_MESSAGE'; content: string; id: string }
  | { type: 'SESSION_COMPLETED' }
  | { type: 'SESSION_FAILED' }
  | { type: 'SET_SENDING' }
  | { type: 'SET_ERROR'; error: string }
  | { type: 'CLEAR_ERROR' }
  | { type: 'HITL_REQUESTED'; transitionId: string; prompt: string; cpnId: string; cpnRole: string }
  | { type: 'HITL_RESOLVED'; transitionId: string; action: HITLAction };

function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case 'SESSION_CREATED':
      return { ...state, sessionId: action.sessionId, sessionState: 'idle' };

    case 'SESSION_RESTORED':
      return {
        ...state,
        sessionId: action.sessionId,
        messages: action.messages,
        sessionState: action.state,
      };

    case 'USER_MESSAGE':
      return {
        ...state,
        messages: [
          ...state.messages,
          {
            id: action.id,
            role: 'user',
            content: action.content,
            isStreaming: false,
            timestamp: new Date(),
          },
        ],
        error: null,
      };

    case 'SET_SENDING':
      return { ...state, sessionState: 'running' };

    case 'STREAM_CHUNK': {
      const { data } = action;

      // Done sentinel — no content, just signals response complete
      if (data.Done && !data.Content) {
        return {
          ...state,
          sessionState: 'idle',
          messages: state.messages.map((m) =>
            m.isStreaming ? { ...m, isStreaming: false } : m
          ),
        };
      }

      const existingIdx = state.messages.findIndex(
        (m) => m.role === 'assistant' && m.isStreaming && m.cpnId === data.CPNID
      );

      if (existingIdx >= 0) {
        const updated = [...state.messages];
        updated[existingIdx] = {
          ...updated[existingIdx],
          content: updated[existingIdx].content + data.Content,
          isStreaming: !data.Done,
        };
        return { ...state, messages: updated, sessionState: data.Done ? 'idle' : 'running' };
      }

      return {
        ...state,
        messages: [
          ...state.messages,
          {
            id: `assistant-${Date.now()}`,
            role: 'assistant',
            content: data.Content,
            isStreaming: !data.Done,
            cpnId: data.CPNID,
            cpnRole: data.CPNRole,
            timestamp: new Date(),
          },
        ],
        sessionState: data.Done ? 'idle' : 'running',
      };
    }

    case 'SESSION_COMPLETED':
      return {
        ...state,
        sessionState: 'completed',
        messages: state.messages.map((m) =>
          m.isStreaming ? { ...m, isStreaming: false } : m
        ),
      };

    case 'SESSION_FAILED':
      return {
        ...state,
        sessionState: 'idle',
        messages: state.messages.map((m) =>
          m.isStreaming ? { ...m, isStreaming: false } : m
        ),
      };

    case 'SET_ERROR':
      return { ...state, error: action.error };

    case 'CLEAR_ERROR':
      return { ...state, error: null };

    case 'HITL_REQUESTED':
      return {
        ...state,
        sessionState: 'waiting',
        messages: [
          ...state.messages,
          {
            id: `hitl-${action.transitionId}-${Date.now()}`,
            role: 'assistant',
            content: action.prompt,
            isStreaming: false,
            cpnId: action.cpnId,
            cpnRole: action.cpnRole,
            timestamp: new Date(),
            hitlTransitionId: action.transitionId,
            hitlActions: ['approve', 'reject'],
          },
        ],
      };

    case 'HITL_RESOLVED':
      return {
        ...state,
        sessionState: 'running',
        messages: state.messages.map((m) =>
          m.hitlTransitionId === action.transitionId
            ? { ...m, hitlResolved: action.action, hitlActions: undefined }
            : m
        ),
      };

    default:
      return state;
  }
}

const initialState: ChatState = {
  sessionId: null,
  messages: [],
  sessionState: 'idle',
  error: null,
};

export function useChat(userId: string) {
  const [state, dispatch] = useReducer(chatReducer, initialState);
  const initRef = useRef(false);

  const storageKey = `liwaisi_session_${userId}`;

  useEffect(() => {
    if (initRef.current) return;
    initRef.current = true;

    async function initSession() {
      const savedSessionId = localStorage.getItem(storageKey);

      if (savedSessionId) {
        try {
          const session = await getSession(savedSessionId);
          if (session.state !== 'failed') {
            const messages: ChatMessage[] = session.messages.map((m) => ({
              id: m.id,
              role: m.role as 'user' | 'assistant',
              content: m.content,
              isStreaming: false,
              cpnId: m.cpn_id,
              timestamp: new Date(m.timestamp),
            }));
            dispatch({ type: 'SESSION_RESTORED', sessionId: session.id, messages, state: session.state });
            return;
          }
        } catch {
          // Session not found or invalid, create new one
        }
      }

      try {
        const session = await createSession(userId, 'web');
        localStorage.setItem(storageKey, session.id);
        dispatch({ type: 'SESSION_CREATED', sessionId: session.id });
      } catch (err) {
        dispatch({ type: 'SET_ERROR', error: (err as Error).message });
      }
    }

    initSession();
  }, [userId, storageKey]);

  const onStreamChunk = useCallback((data: StreamChunkData) => {
    dispatch({ type: 'STREAM_CHUNK', data });
  }, []);

  const onSessionCompleted = useCallback(() => {
    dispatch({ type: 'SESSION_COMPLETED' });
  }, []);

  const onSessionFailed = useCallback(() => {
    dispatch({ type: 'SESSION_FAILED' });
  }, []);

  const onHITLRequested = useCallback((data: CPNEventData) => {
    // Payload can be a string (legacy) or {prompt, content} (with intermediate output).
    let planContent = '';
    let prompt = 'Please review and confirm.';

    if (typeof data.Payload === 'string') {
      prompt = data.Payload;
    } else if (data.Payload && typeof data.Payload === 'object') {
      const p = data.Payload as Record<string, unknown>;
      if (typeof p.prompt === 'string') prompt = p.prompt;
      if (typeof p.content === 'string') planContent = p.content;
    }

    // If there's intermediate content (e.g. a plan), show it as a regular message first.
    if (planContent) {
      dispatch({
        type: 'STREAM_CHUNK',
        data: {
          SessionID: data.SessionID,
          CPNID: data.CPNID,
          CPNRole: data.CPNRole,
          Content: planContent,
          Done: true,
        },
      });
    }

    dispatch({
      type: 'HITL_REQUESTED',
      transitionId: data.TransitionID,
      prompt,
      cpnId: data.CPNID,
      cpnRole: data.CPNRole,
    });
  }, []);

  const { isConnected } = useSSE({
    sessionId: state.sessionId,
    onStreamChunk,
    onSessionCompleted,
    onSessionFailed,
    onHITLRequested,
  });

  const handleResolveHITL = useCallback(
    async (transitionId: string, action: HITLAction) => {
      if (!state.sessionId) return;
      dispatch({ type: 'HITL_RESOLVED', transitionId, action });
      try {
        await apiResolveHITL(state.sessionId, transitionId, { action });
      } catch (err) {
        dispatch({ type: 'SET_ERROR', error: err instanceof ApiError ? err.message : 'Failed to respond' });
      }
    },
    [state.sessionId]
  );

  const sendMessage = useCallback(
    async (content: string) => {
      if (!content.trim()) return;

      const trimmed = content.trim();
      const id = `user-${Date.now()}`;

      // If session is terminal (completed/failed), create a new session first
      let sessionId = state.sessionId;
      if (!sessionId || state.sessionState === 'completed') {
        try {
          const session = await createSession(userId, 'web');
          sessionId = session.id;
          localStorage.setItem(storageKey, session.id);
          dispatch({ type: 'SESSION_CREATED', sessionId: session.id });
        } catch (err) {
          dispatch({ type: 'SET_ERROR', error: err instanceof ApiError ? err.message : 'Failed to create session' });
          return;
        }
      }

      dispatch({ type: 'USER_MESSAGE', content: trimmed, id });
      dispatch({ type: 'SET_SENDING' });

      try {
        await apiSendMessage(sessionId, trimmed);
      } catch (err) {
        if (err instanceof ApiError) {
          dispatch({ type: 'SET_ERROR', error: err.message });
        } else {
          dispatch({ type: 'SET_ERROR', error: 'Failed to send message' });
        }
      }
    },
    [state.sessionId, state.sessionState, userId, storageKey]
  );

  return {
    messages: state.messages,
    sessionState: state.sessionState,
    sessionId: state.sessionId,
    isConnected,
    sendMessage,
    resolveHITL: handleResolveHITL,
    error: state.error,
  };
}
