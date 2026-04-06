import { useReducer, useEffect, useCallback, useRef } from 'react';
import type { SessionState } from '../types/api';
import type { StreamChunkData, CPNEventData } from '../types/sse';
import type { ChatMessage, HITLAction } from '../types/chat';
import { getSession, sendMessage as apiSendMessage, resolveHITL as apiResolveHITL, ApiError } from '../services/api';
import { useSSE } from './useSSE';

interface ChatState {
  messages: ChatMessage[];
  sessionState: SessionState;
  error: string | null;
}

type ChatAction =
  | { type: 'SESSION_LOADED'; messages: ChatMessage[]; state: SessionState }
  | { type: 'STREAM_CHUNK'; data: StreamChunkData }
  | { type: 'USER_MESSAGE'; content: string; id: string }
  | { type: 'SESSION_COMPLETED' }
  | { type: 'SESSION_FAILED' }
  | { type: 'SET_SENDING' }
  | { type: 'SET_ERROR'; error: string }
  | { type: 'CLEAR_ERROR' }
  | { type: 'HITL_REQUESTED'; transitionId: string; prompt: string; cpnId: string; cpnRole: string; suppressBubble?: boolean }
  | { type: 'HITL_RESOLVED'; transitionId: string; action: HITLAction }
  | { type: 'RESET' };

function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case 'SESSION_LOADED':
      return {
        ...state,
        messages: action.messages,
        sessionState: action.state,
        error: null,
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
        messages: action.suppressBubble
          ? state.messages
          : [
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

    case 'RESET':
      return { ...initialState };

    default:
      return state;
  }
}

const initialState: ChatState = {
  messages: [],
  sessionState: 'idle',
  error: null,
};

export interface UseChatOptions {
  onTransitionStarted?: (data: CPNEventData) => void;
  onTransitionCompleted?: (data: CPNEventData) => void;
  onSubNetStarted?: (data: CPNEventData) => void;
  onSubNetCompleted?: (data: CPNEventData) => void;
  onSubNetFailed?: (data: CPNEventData) => void;
  onSessionCompleted?: () => void;
}

export interface UseChatReturn {
  messages: ChatMessage[];
  sessionState: SessionState;
  sessionId: string | null;
  isConnected: boolean;
  sendMessage: (content: string) => Promise<void>;
  resolveHITL: (transitionId: string, action: HITLAction) => Promise<void>;
  error: string | null;
}

export function useChat(sessionId: string | null, options?: UseChatOptions): UseChatReturn {
  const [state, dispatch] = useReducer(chatReducer, initialState);
  const prevSessionIdRef = useRef<string | null>(null);

  // Load session messages when sessionId changes
  useEffect(() => {
    if (sessionId === prevSessionIdRef.current) return;
    prevSessionIdRef.current = sessionId;

    if (!sessionId) {
      dispatch({ type: 'RESET' });
      return;
    }

    let cancelled = false;

    async function loadSession() {
      try {
        const session = await getSession(sessionId!);
        if (cancelled) return;
        const messages: ChatMessage[] = session.messages.map((m) => ({
          id: m.id,
          role: m.role as 'user' | 'assistant',
          content: m.content,
          isStreaming: false,
          cpnId: m.cpn_id,
          timestamp: new Date(m.timestamp),
        }));
        dispatch({ type: 'SESSION_LOADED', messages, state: session.state });
      } catch (err) {
        if (cancelled) return;
        // New session with no messages yet is fine
        if (err instanceof ApiError && err.status === 404) {
          dispatch({ type: 'SESSION_LOADED', messages: [], state: 'idle' });
        } else {
          dispatch({ type: 'SET_ERROR', error: (err as Error).message });
        }
      }
    }

    dispatch({ type: 'RESET' });
    loadSession();

    return () => { cancelled = true; };
  }, [sessionId]);

  const onStreamChunk = useCallback((data: StreamChunkData) => {
    dispatch({ type: 'STREAM_CHUNK', data });
  }, []);

  const onSessionCompleted = useCallback(() => {
    dispatch({ type: 'SESSION_COMPLETED' });
    options?.onSessionCompleted?.();
  }, [options?.onSessionCompleted]);

  const onSessionFailed = useCallback(() => {
    dispatch({ type: 'SESSION_FAILED' });
  }, []);

  const onHITLRequested = useCallback((data: CPNEventData) => {
    let planContent = '';
    let prompt = 'Please review and confirm.';
    let customSurface = false;

    if (typeof data.Payload === 'string') {
      prompt = data.Payload;
    } else if (data.Payload && typeof data.Payload === 'object') {
      const p = data.Payload as Record<string, unknown>;
      if (typeof p.prompt === 'string') prompt = p.prompt;
      if (typeof p.content === 'string') planContent = p.content;
      // Backend signals via HITLRequestedPayload.CustomSurface that the
      // transition already pushed its own A2UI surface (e.g. questionnaire)
      // through the stream_chunk path. In that case we must NOT create an
      // extra prompt bubble — the surface already contains everything the
      // user needs to see and act on.
      if (p.custom_surface === true) customSurface = true;
    }

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
      suppressBubble: customSurface,
    });
  }, []);

  const { isConnected } = useSSE({
    sessionId,
    onStreamChunk,
    onSessionCompleted,
    onSessionFailed,
    onHITLRequested,
    onTransitionStarted: options?.onTransitionStarted,
    onTransitionCompleted: options?.onTransitionCompleted,
    onSubNetStarted: options?.onSubNetStarted,
    onSubNetCompleted: options?.onSubNetCompleted,
    onSubNetFailed: options?.onSubNetFailed,
  });

  const handleResolveHITL = useCallback(
    async (transitionId: string, action: HITLAction, content?: string) => {
      if (!sessionId) return;
      dispatch({ type: 'HITL_RESOLVED', transitionId, action });
      try {
        await apiResolveHITL(sessionId, transitionId, {
          action,
          ...((action === 'revise' || action === 'submit') && content ? { content } : {}),
        });
      } catch (err) {
        dispatch({ type: 'SET_ERROR', error: err instanceof ApiError ? err.message : 'Failed to respond' });
      }
    },
    [sessionId]
  );

  const sendMessage = useCallback(
    async (content: string) => {
      if (!content.trim() || !sessionId) return;

      const trimmed = content.trim();
      const id = `user-${Date.now()}`;

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
    [sessionId]
  );

  return {
    messages: state.messages,
    sessionState: state.sessionState,
    sessionId,
    isConnected,
    sendMessage,
    resolveHITL: handleResolveHITL,
    error: state.error,
  };
}
