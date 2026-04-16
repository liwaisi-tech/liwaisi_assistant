import { useReducer, useEffect, useCallback, useRef } from 'react';
import type { BackendSessionState, SessionState } from '../types/api';
import type { StreamChunkData, CPNEventData } from '../types/sse';
import type { ChatMessage, HITLAction } from '../types/chat';
import { getSession, sendMessage as apiSendMessage, resolveHITL as apiResolveHITL, ApiError } from '../services/api';
import { useSSE, type SSEConnectionState } from './useSSE';
import { A2UI_MARKER } from '../features/chat/a2ui/constants';

export interface ChatState {
  // Tracks the session whose stream we are willing to apply. STREAM_CHUNK
  // actions whose SessionID does not match are dropped so a late chunk from
  // a previous chat cannot bleed into the current chat's DOM (REQ-302/AC-302).
  // Null means we have not yet been told about a session — chunks are still
  // accepted in that window for backwards-compat.
  sessionId: string | null;
  messages: ChatMessage[];
  sessionState: SessionState;
  error: string | null;
}

export type ChatAction =
  // SESSION_LOADED carries the sessionId so the reducer can begin filtering
  // STREAM_CHUNK actions for that session (REQ-302). The state field carries
  // the backend rehydration vocabulary; the reducer maps it onto the
  // reducer-internal SessionState via mapBackendStateToReducerState.
  | { type: 'SESSION_LOADED'; sessionId: string; messages: ChatMessage[]; state: BackendSessionState }
  | { type: 'STREAM_CHUNK'; data: StreamChunkData }
  | { type: 'USER_MESSAGE'; content: string; id: string }
  | { type: 'SESSION_COMPLETED' }
  | { type: 'SESSION_FAILED' }
  | { type: 'SET_SENDING' }
  | { type: 'SET_ERROR'; error: string }
  | { type: 'CLEAR_ERROR' }
  | { type: 'HITL_REQUESTED'; transitionId: string; prompt: string; cpnId: string; cpnRole: string; suppressBubble?: boolean }
  | { type: 'HITL_RESOLVED'; transitionId: string; action: HITLAction; resolvedPayload?: string }
  | { type: 'RESET' };

// mapBackendStateToReducerState translates the rehydration-oriented vocabulary
// returned by GET /api/v1/sessions/{id} into the reducer-internal SessionState.
// Visual behavior is identical to a fresh 'running' state — the existing
// "thinking…" indicator (MessageList.showThinking) covers both the live and
// rehydrated cases without needing a dedicated 'generating' value.
//   running       → 'running'   (REQ-401: CPN running, indicator visible while no chunks)
//   hitl_pending  → 'idle'      (REQ-403: A2UI surface IS the affordance — no spinner)
//   terminal      → 'idle'      (REQ-404: completed/failed are terminal, composer enabled)
//   idle          → 'idle'
// See spec-process-bugfix-a2ui-rehydration-completion.md changelog 1.1
// for the rationale on collapsing the originally-proposed 'generating' state.
function mapBackendStateToReducerState(s: BackendSessionState): SessionState {
  if (s === 'running') return 'running';
  return 'idle';
}

// narrowToBackendState collapses the wire-level SessionState | BackendSessionState
// union onto the reducer's expected BackendSessionState. Any legacy/list
// vocabulary (waiting/completed/failed) is treated as 'idle' for rehydration —
// the affordance the user needs is read from the persisted message list, not
// from the legacy state value.
// enrichWithResolutions pairs each A2UI assistant row with a later HITL
// response row (linked via parentMessageId) so the questionnaire component
// can render in a locked/read-only state showing the submitted answers
// (REQ-102..105 — spec-process-bugfix-a2ui-hitl-response-persistence.md).
// For action-envelope responses ({"action":"approve|revise|reject"}), it
// also restores hitlResolved so the existing "You approved" badge and the
// hitl:* button-lock pattern reappear after rehydration. Iterating once
// over the array keeps this O(n).
export function enrichWithResolutions(messages: ChatMessage[]): ChatMessage[] {
  const resolutions = new Map<string, { payload: string; at: Date; action: HITLAction | null }>();
  for (const m of messages) {
    if (m.role === 'user' && m.parentMessageId) {
      resolutions.set(m.parentMessageId, {
        payload: m.content,
        at: m.timestamp,
        action: extractEnvelopeAction(m.content),
      });
    }
  }
  if (resolutions.size === 0) return messages;
  const result: ChatMessage[] = [];
  for (const m of messages) {
    // Drop HITL response rows — their content is surfaced via the parent
    // A2UI row's resolvedPayload, not as a standalone user bubble.
    if (m.role === 'user' && m.parentMessageId) continue;
    const hit = resolutions.get(m.id);
    if (!hit) {
      result.push(m);
      continue;
    }
    result.push({
      ...m,
      resolvedPayload: hit.payload,
      resolvedAt: hit.at,
      ...(hit.action ? { hitlResolved: hit.action } : {}),
    });
  }
  return result;
}

// extractEnvelopeAction returns the HITL action keyword embedded in an
// action-envelope response ({"action":"approve|revise|reject|submit"}).
// Returns null for questionnaire answer maps (flat or wrapped), for
// malformed JSON, and for any value that is not one of the known actions.
function extractEnvelopeAction(content: string): HITLAction | null {
  try {
    const parsed = JSON.parse(content) as unknown;
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null;
    const a = (parsed as Record<string, unknown>).action;
    if (a === 'approve' || a === 'revise' || a === 'reject' || a === 'submit') {
      return a;
    }
  } catch {
    // fall through
  }
  return null;
}

function narrowToBackendState(s: SessionState | BackendSessionState): BackendSessionState {
  if (s === 'running' || s === 'hitl_pending' || s === 'terminal' || s === 'idle') {
    return s;
  }
  return 'idle';
}

export function chatReducer(state: ChatState, action: ChatAction): ChatState {
  switch (action.type) {
    case 'SESSION_LOADED':
      return {
        ...state,
        sessionId: action.sessionId,
        // REQ-303: rehydrated messages MUST never carry a stale streaming
        // flag into the DOM. Force isStreaming=false on every entry.
        messages: action.messages.map((m) =>
          m.isStreaming ? { ...m, isStreaming: false } : m
        ),
        sessionState: mapBackendStateToReducerState(action.state),
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

      // 0. Session-id race defence (REQ-302 / AC-302). Drop chunks that
      // belong to a session other than the one the user is viewing. This
      // closes the window between chat-switch dispatch and SSE abort. The
      // null-sessionId branch keeps backwards-compat for callers that
      // dispatch chunks before SESSION_LOADED has set the id.
      if (state.sessionId !== null && data.SessionID !== state.sessionId) {
        return state;
      }

      // 1. Done sentinel — no content, just signals response complete
      if (data.Done && !data.Content) {
        return {
          ...state,
          sessionState: 'idle',
          messages: state.messages.map((m) =>
            m.isStreaming ? { ...m, isStreaming: false } : m
          ),
        };
      }

      const newBubble = (): ChatMessage => ({
        id: `assistant-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
        role: 'assistant',
        content: data.Content,
        isStreaming: !data.Done,
        cpnId: data.CPNID,
        cpnRole: data.CPNRole,
        timestamp: new Date(),
      });

      // 2. A2UI boundary (REQ-008/009): any chunk starting with the marker
      // opens a brand-new assistant bubble. Every currently-streaming
      // assistant message is closed first so a mid-flight LLM bubble does
      // not swallow the marker.
      if (data.Content.startsWith(A2UI_MARKER)) {
        return {
          ...state,
          sessionState: data.Done ? 'idle' : 'running',
          messages: [
            ...state.messages.map((m) =>
              m.role === 'assistant' && m.isStreaming
                ? { ...m, isStreaming: false }
                : m
            ),
            newBubble(),
          ],
        };
      }

      // 3. Post-A2UI guard (REQ-010): if a streaming assistant bubble on the
      // same CPNID is already holding a complete A2UI payload, a follow-on
      // non-marker chunk MUST NOT be concatenated onto it. Close the A2UI
      // bubble and start a fresh plain bubble.
      const a2uiIdx = state.messages.findIndex(
        (m) =>
          m.role === 'assistant' &&
          m.isStreaming &&
          m.cpnId === data.CPNID &&
          m.content.startsWith(A2UI_MARKER)
      );
      if (a2uiIdx >= 0) {
        const updated = [...state.messages];
        updated[a2uiIdx] = { ...updated[a2uiIdx], isStreaming: false };
        updated.push(newBubble());
        return {
          ...state,
          messages: updated,
          sessionState: data.Done ? 'idle' : 'running',
        };
      }

      // 4. Default append — existing behaviour.
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
        messages: [...state.messages, newBubble()],
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

    case 'HITL_REQUESTED': {
      if (!action.suppressBubble) {
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
      }
      // Custom-surface path: the transition already pushed an A2UI bubble
      // through STREAM_CHUNK before this HITL_REQUESTED arrived. Stamp the
      // transition id onto the most-recent A2UI assistant bubble for this
      // cpnId so HITL_RESOLVED can match it and propagate resolvedPayload
      // → ResolutionContext → QuestionnaireLocked. Without this stamp the
      // submit button never locks, allowing a double-submit that returns
      // 409 ErrNoHITLWaiting on the second click.
      let stampIdx = -1;
      for (let i = state.messages.length - 1; i >= 0; i--) {
        const m = state.messages[i];
        if (
          m.role === 'assistant' &&
          m.cpnId === action.cpnId &&
          m.content.startsWith(A2UI_MARKER) &&
          !m.hitlTransitionId
        ) {
          stampIdx = i;
          break;
        }
      }
      if (stampIdx === -1) {
        return { ...state, sessionState: 'waiting' };
      }
      const stamped = [...state.messages];
      stamped[stampIdx] = {
        ...stamped[stampIdx],
        hitlTransitionId: action.transitionId,
      };
      return { ...state, sessionState: 'waiting', messages: stamped };
    }

    case 'HITL_RESOLVED':
      return {
        ...state,
        sessionState: 'running',
        messages: state.messages.map((m) =>
          m.hitlTransitionId === action.transitionId
            ? {
                ...m,
                hitlResolved: action.action,
                hitlActions: undefined,
                // Lock immediately on live submission so the questionnaire
                // renders read-only without waiting for a reload (REQ-105).
                ...(action.resolvedPayload
                  ? { resolvedPayload: action.resolvedPayload, resolvedAt: new Date() }
                  : {}),
              }
            : m
        ),
      };

    case 'RESET':
      return { ...initialState };

    default:
      return state;
  }
}

export const initialState: ChatState = {
  sessionId: null,
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
  /**
   * Fired when SSE reveals a ghost session id (REQ-102, AC-007). The caller
   * is expected to delegate to `useChatList.recoverFromGhost` so recovery
   * runs exactly once per ghost id.
   */
  onSessionNotFound?: (sessionId: string) => void;
}

export interface UseChatReturn {
  messages: ChatMessage[];
  sessionState: SessionState;
  sessionId: string | null;
  isConnected: boolean;
  connectionState: SSEConnectionState;
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
          parentMessageId: m.parent_message_id,
        }));
        // REQ-102..104: pair each A2UI surface with its HITL response row
        // (linked via parent_message_id) so the questionnaire can render
        // locked with the answers the user submitted
        // (spec-process-bugfix-a2ui-hitl-response-persistence.md).
        const enriched = enrichWithResolutions(messages);
        dispatch({
          type: 'SESSION_LOADED',
          sessionId: sessionId!,
          messages: enriched,
          // GET /sessions/{id} returns BackendSessionState (REQ-404), but the
          // shared SessionResponse interface widens it to SessionState |
          // BackendSessionState. Narrow defensively at the seam: any legacy
          // SessionState value (waiting/completed/failed) maps to 'idle' from
          // the reducer's perspective.
          state: narrowToBackendState(session.state),
        });
      } catch (err) {
        if (cancelled) return;
        // New session with no messages yet is fine
        if (err instanceof ApiError && err.status === 404) {
          dispatch({ type: 'SESSION_LOADED', sessionId: sessionId!, messages: [], state: 'idle' });
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

  const { isConnected, connectionState } = useSSE({
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
    onSessionNotFound: options?.onSessionNotFound,
  });

  const handleResolveHITL = useCallback(
    async (transitionId: string, action: HITLAction, content?: string) => {
      if (!sessionId) return;
      // Encode resolvedPayload for the reducer using the same rules the
      // backend uses when appending the HITL response row (REQ-001/002):
      //   submit       → raw content (questionnaire answers JSON)
      //   revise       → {"action":"revise","content":"..."}
      //   approve      → {"action":"approve"} or include content if present
      //   reject       → no lock (backend does not persist a response)
      let resolvedPayload: string | undefined;
      if (action === 'submit') {
        resolvedPayload = content ?? '';
      } else if (action === 'revise') {
        resolvedPayload = JSON.stringify({ action: 'revise', content: content ?? '' });
      } else if (action === 'approve') {
        resolvedPayload = content
          ? JSON.stringify({ action: 'approve', content })
          : JSON.stringify({ action: 'approve' });
      }
      dispatch({ type: 'HITL_RESOLVED', transitionId, action, resolvedPayload });
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
    connectionState,
    sendMessage,
    resolveHITL: handleResolveHITL,
    error: state.error,
  };
}
