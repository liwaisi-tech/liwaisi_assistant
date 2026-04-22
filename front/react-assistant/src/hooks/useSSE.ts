import { useEffect, useRef, useState, useCallback } from 'react';
import type { StreamChunkData, CPNEventData } from '../types/sse';
import { getAuthToken, SessionNotFoundError } from '../services/api';

interface UseSSEOptions {
  sessionId: string | null;
  onStreamChunk: (data: StreamChunkData) => void;
  onSessionCompleted: () => void;
  onSessionFailed: () => void;
  onHITLRequested?: (data: CPNEventData) => void;
  onTransitionFired?: (data: CPNEventData) => void;
  onTransitionStarted?: (data: CPNEventData) => void;
  onTransitionCompleted?: (data: CPNEventData) => void;
  onSubNetStarted?: (data: CPNEventData) => void;
  onSubNetCompleted?: (data: CPNEventData) => void;
  onSubNetFailed?: (data: CPNEventData) => void;
  onToolExecuted?: (data: CPNEventData) => void;
  /**
   * Fired when the backend asks the user to approve a freshly synthesized
   * tool before its first run. Payload shape:
   *   { request_id: string, preview: A2UIPayload }
   * Kept as `unknown` here so this hook stays a thin SSE pump — the
   * caller (useChat) parses + validates the bag.
   */
  onToolApprovalRequested?: (data: { request_id: string; preview: unknown }) => void;
  onError?: (error: Event) => void;
  /**
   * Fired when the SSE stream reveals that the current session id no longer
   * exists (e.g. ghost after restart + failed rehydration). The EventSource
   * is closed and no reconnect is attempted for this id (REQ-104, AC-007).
   */
  onSessionNotFound?: (sessionId: string) => void;
}

export type SSEConnectionState = 'connected' | 'reconnecting' | 'disconnected-terminal';

interface UseSSEReturn {
  isConnected: boolean;
  /**
   * True once `onSessionNotFound` has fired for the current session id. The
   * UI uses this to distinguish transient reconnects from a terminal
   * "session is dead" state.
   */
  isSessionDead: boolean;
  /**
   * Typed connection state for UI indicators (REQ-112). Derived from the
   * other two flags: dead → terminal, open → connected, else reconnecting.
   */
  connectionState: SSEConnectionState;
}

/** Initial backoff base in ms. Doubles each retry, capped at MAX_BACKOFF_MS. */
const INITIAL_BACKOFF_MS = 1000;
/** Backoff ceiling per REQ-105. */
const MAX_BACKOFF_MS = 30_000;

/**
 * Parse an `error` SSE event's data blob for a session-not-found signal.
 * The backend may emit either a proper named event (`event: error`) or
 * close the connection with a 404/410 — EventSource surfaces the latter as
 * a plain `onerror` with no data. In both cases we rely on either an in-
 * band JSON payload or a probe request fired by the caller to resolve the
 * ambiguity. Returns true only if the data clearly names the ghost case.
 */
function isSessionNotFoundPayload(raw: unknown): boolean {
  if (typeof raw !== 'string') return false;
  try {
    const parsed = JSON.parse(raw) as { error?: unknown };
    if (typeof parsed.error === 'string') {
      return /^\s*session not found\s*$/i.test(parsed.error);
    }
  } catch {
    // Fall through to plain-string check.
  }
  return /session not found/i.test(raw);
}

export function useSSE({
  sessionId,
  onStreamChunk,
  onSessionCompleted,
  onSessionFailed,
  onHITLRequested,
  onTransitionFired,
  onTransitionStarted,
  onTransitionCompleted,
  onSubNetStarted,
  onSubNetCompleted,
  onSubNetFailed,
  onToolExecuted,
  onToolApprovalRequested,
  onError,
  onSessionNotFound,
}: UseSSEOptions): UseSSEReturn {
  const [isConnected, setIsConnected] = useState(false);
  const [isSessionDead, setIsSessionDead] = useState(false);
  const retryDelayRef = useRef(INITIAL_BACKOFF_MS);
  const retryTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const sessionTerminalRef = useRef(false);

  // Keep the latest callbacks accessible without triggering reconnects when
  // the parent component re-renders (REQ-109). Components MUST still wrap
  // their handlers in `useCallback` to avoid tearing down the SSE connection
  // each render — `connect` only depends on stable values below.
  const callbacksRef = useRef({
    onStreamChunk, onSessionCompleted, onSessionFailed, onHITLRequested, onTransitionFired,
    onTransitionStarted, onTransitionCompleted, onSubNetStarted, onSubNetCompleted, onSubNetFailed, onToolExecuted, onToolApprovalRequested, onError, onSessionNotFound,
  });
  callbacksRef.current = {
    onStreamChunk, onSessionCompleted, onSessionFailed, onHITLRequested, onTransitionFired,
    onTransitionStarted, onTransitionCompleted, onSubNetStarted, onSubNetCompleted, onSubNetFailed, onToolExecuted, onToolApprovalRequested, onError, onSessionNotFound,
  };

  /**
   * Tear down the current EventSource and mark the session id as terminally
   * dead (REQ-104). Caller is responsible for rotating the session id
   * afterwards — this hook will not retry against the dead id.
   */
  const markSessionDead = useCallback((sid: string) => {
    sessionTerminalRef.current = true;
    if (retryTimerRef.current) {
      clearTimeout(retryTimerRef.current);
      retryTimerRef.current = null;
    }
    setIsSessionDead(true);
    setIsConnected(false);
    callbacksRef.current.onSessionNotFound?.(sid);
  }, []);

  const connect = useCallback((sid: string) => {
    if (sessionTerminalRef.current) return undefined;

    const token = getAuthToken();
    const url = token
      ? `/api/v1/sessions/${sid}/events?token=${encodeURIComponent(token)}`
      : `/api/v1/sessions/${sid}/events`;
    const es = new EventSource(url);

    es.onopen = () => {
      setIsConnected(true);
      retryDelayRef.current = INITIAL_BACKOFF_MS;
    };

    es.onerror = (evt) => {
      setIsConnected(false);
      es.close();
      callbacksRef.current.onError?.(evt);

      if (sessionTerminalRef.current) return;

      // Exponential backoff with jitter, capped at MAX_BACKOFF_MS (REQ-105).
      // Jitter is added AFTER the cap so total wait is cap + [0, 1s).
      const base = Math.min(retryDelayRef.current, MAX_BACKOFF_MS);
      const jitter = Math.random() * 1000;
      retryDelayRef.current = Math.min(base * 2, MAX_BACKOFF_MS);
      retryTimerRef.current = setTimeout(() => connect(sid), base + jitter);
    };

    // Some backends emit a typed `error` event to indicate a non-retriable
    // condition (session-not-found, auth failure) while keeping the stream
    // open. We treat any `error` event whose data matches the ghost signal
    // as terminal (REQ-104, AC-007).
    es.addEventListener('error', (evt) => {
      const data = (evt as MessageEvent).data;
      if (isSessionNotFoundPayload(data)) {
        es.close();
        markSessionDead(sid);
      }
      // Otherwise fall through — the default `onerror` path will handle
      // transient connection drops with backoff.
    });

    es.addEventListener('session_not_found', () => {
      es.close();
      markSessionDead(sid);
    });

    es.addEventListener('stream_chunk', (evt) => {
      try {
        const data: StreamChunkData = JSON.parse(evt.data);
        callbacksRef.current.onStreamChunk(data);
      } catch { /* malformed SSE data — skip chunk */ }
    });

    es.addEventListener('session_completed', () => {
      sessionTerminalRef.current = true;
      callbacksRef.current.onSessionCompleted();
      es.close();
      setIsConnected(false);
    });

    es.addEventListener('session_failed', () => {
      callbacksRef.current.onSessionFailed();
      // Do NOT set sessionTerminalRef or close EventSource —
      // the session remains usable after HITL rejection or recoverable errors.
    });

    es.addEventListener('hitl_requested', (evt) => {
      try {
        const data: CPNEventData = JSON.parse(evt.data);
        callbacksRef.current.onHITLRequested?.(data);
      } catch { /* malformed SSE data — skip event */ }
    });

    es.addEventListener('transition_fired', (evt) => {
      try {
        const data: CPNEventData = JSON.parse(evt.data);
        callbacksRef.current.onTransitionFired?.(data);
      } catch { /* malformed SSE data — skip event */ }
    });

    es.addEventListener('transition_started', (evt) => {
      try {
        const data: CPNEventData = JSON.parse(evt.data);
        callbacksRef.current.onTransitionStarted?.(data);
      } catch { /* malformed SSE data — skip event */ }
    });

    es.addEventListener('transition_completed', (evt) => {
      try {
        const data: CPNEventData = JSON.parse(evt.data);
        callbacksRef.current.onTransitionCompleted?.(data);
      } catch { /* malformed SSE data — skip event */ }
    });

    es.addEventListener('subnet_started', (evt) => {
      try {
        const data: CPNEventData = JSON.parse(evt.data);
        callbacksRef.current.onSubNetStarted?.(data);
      } catch { /* malformed SSE data — skip event */ }
    });

    es.addEventListener('subnet_completed', (evt) => {
      try {
        const data: CPNEventData = JSON.parse(evt.data);
        callbacksRef.current.onSubNetCompleted?.(data);
      } catch { /* malformed SSE data — skip event */ }
    });

    es.addEventListener('subnet_failed', (evt) => {
      try {
        const data: CPNEventData = JSON.parse(evt.data);
        callbacksRef.current.onSubNetFailed?.(data);
      } catch { /* malformed SSE data — skip event */ }
    });

    es.addEventListener('tool_executed', (evt) => {
      try {
        const data: CPNEventData = JSON.parse(evt.data);
        callbacksRef.current.onToolExecuted?.(data);
      } catch { /* malformed SSE data — skip event */ }
    });

    // tool_approval_request — first-run HITL gate for a freshly synthesized
    // tool. Payload is `{ request_id, preview }` where preview is an A2UI
    // envelope. Forwarded as-is; useChat is responsible for shape validation.
    es.addEventListener('tool_approval_request', (evt) => {
      try {
        const data = JSON.parse(evt.data);
        if (data && typeof data.request_id === 'string') {
          callbacksRef.current.onToolApprovalRequested?.(data);
        }
      } catch { /* malformed SSE data — skip event */ }
    });

    return es;
  }, [markSessionDead]);

  useEffect(() => {
    if (!sessionId) return;

    sessionTerminalRef.current = false;
    setIsSessionDead(false);
    retryDelayRef.current = INITIAL_BACKOFF_MS;
    const es = connect(sessionId);

    return () => {
      es?.close();
      setIsConnected(false);
      if (retryTimerRef.current) {
        clearTimeout(retryTimerRef.current);
        retryTimerRef.current = null;
      }
    };
  }, [sessionId, connect]);

  const connectionState: SSEConnectionState = isSessionDead
    ? 'disconnected-terminal'
    : isConnected
    ? 'connected'
    : 'reconnecting';

  return { isConnected, isSessionDead, connectionState };
}

/**
 * Re-export so sibling modules (useChat, callers that bridge SSE errors to
 * recovery flows) can do `instanceof SessionNotFoundError` without importing
 * from deeper in the service layer.
 */
export { SessionNotFoundError };
