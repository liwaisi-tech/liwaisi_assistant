import { useReducer, useEffect, useCallback, useRef } from 'react';
import type { SessionState } from '../types/api';
import type { StreamChunkData } from '../types/sse';
import type { ChatMessage } from '../types/chat';
import { createSession, sendMessage as apiSendMessage, ApiError } from '../services/api';
import { useSSE } from './useSSE';

interface ChatState {
  sessionId: string | null;
  messages: ChatMessage[];
  sessionState: SessionState;
  error: string | null;
}

type ChatAction =
  | { type: 'SESSION_CREATED'; sessionId: string }
  | { type: 'STREAM_CHUNK'; data: StreamChunkData }
  | { type: 'USER_MESSAGE'; content: string; id: string }
  | { type: 'SESSION_COMPLETED' }
  | { type: 'SESSION_FAILED' }
  | { type: 'SET_SENDING' }
  | { type: 'SET_ERROR'; error: string }
  | { type: 'CLEAR_ERROR' };

function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case 'SESSION_CREATED':
      return { ...state, sessionId: action.sessionId, sessionState: 'idle' };

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
        return { ...state, messages: updated, sessionState: 'running' };
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
        sessionState: 'running',
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
        sessionState: 'failed',
        messages: state.messages.map((m) =>
          m.isStreaming ? { ...m, isStreaming: false } : m
        ),
      };

    case 'SET_ERROR':
      return { ...state, error: action.error };

    case 'CLEAR_ERROR':
      return { ...state, error: null };

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

  useEffect(() => {
    if (initRef.current) return;
    initRef.current = true;

    createSession(userId, 'web')
      .then((session) => dispatch({ type: 'SESSION_CREATED', sessionId: session.id }))
      .catch((err) => dispatch({ type: 'SET_ERROR', error: err.message }));
  }, [userId]);

  const onStreamChunk = useCallback((data: StreamChunkData) => {
    dispatch({ type: 'STREAM_CHUNK', data });
  }, []);

  const onSessionCompleted = useCallback(() => {
    dispatch({ type: 'SESSION_COMPLETED' });
  }, []);

  const onSessionFailed = useCallback(() => {
    dispatch({ type: 'SESSION_FAILED' });
  }, []);

  const { isConnected } = useSSE({
    sessionId: state.sessionId,
    onStreamChunk,
    onSessionCompleted,
    onSessionFailed,
  });

  const sendMessage = useCallback(
    async (content: string) => {
      if (!state.sessionId || !content.trim()) return;

      const id = `user-${Date.now()}`;
      dispatch({ type: 'USER_MESSAGE', content: content.trim(), id });
      dispatch({ type: 'SET_SENDING' });

      try {
        await apiSendMessage(state.sessionId, content.trim());
      } catch (err) {
        if (err instanceof ApiError) {
          dispatch({ type: 'SET_ERROR', error: err.message });
        } else {
          dispatch({ type: 'SET_ERROR', error: 'Failed to send message' });
        }
      }
    },
    [state.sessionId]
  );

  return {
    messages: state.messages,
    sessionState: state.sessionState,
    sessionId: state.sessionId,
    isConnected,
    sendMessage,
    error: state.error,
  };
}
