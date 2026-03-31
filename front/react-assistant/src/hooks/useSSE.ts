import { useEffect, useRef, useState, useCallback } from 'react';
import type { StreamChunkData, CPNEventData } from '../types/sse';

interface UseSSEOptions {
  sessionId: string | null;
  onStreamChunk: (data: StreamChunkData) => void;
  onSessionCompleted: () => void;
  onSessionFailed: () => void;
  onHITLRequested?: (data: CPNEventData) => void;
  onTransitionFired?: (data: CPNEventData) => void;
  onError?: (error: Event) => void;
}

interface UseSSEReturn {
  isConnected: boolean;
}

export function useSSE({
  sessionId,
  onStreamChunk,
  onSessionCompleted,
  onSessionFailed,
  onHITLRequested,
  onTransitionFired,
  onError,
}: UseSSEOptions): UseSSEReturn {
  const [isConnected, setIsConnected] = useState(false);
  const retryDelayRef = useRef(3000);
  const retryTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const sessionTerminalRef = useRef(false);

  // Use refs for callbacks to avoid reconnection on callback changes
  const callbacksRef = useRef({ onStreamChunk, onSessionCompleted, onSessionFailed, onHITLRequested, onTransitionFired, onError });
  callbacksRef.current = { onStreamChunk, onSessionCompleted, onSessionFailed, onHITLRequested, onTransitionFired, onError };

  const connect = useCallback((sid: string) => {
    if (sessionTerminalRef.current) return;

    const url = `/api/v1/sessions/${sid}/events`;
    const es = new EventSource(url);

    es.onopen = () => {
      setIsConnected(true);
      retryDelayRef.current = 3000; // Reset backoff
    };

    es.onerror = (evt) => {
      setIsConnected(false);
      es.close();
      callbacksRef.current.onError?.(evt);

      if (!sessionTerminalRef.current) {
        // Exponential backoff with jitter
        const delay = Math.min(retryDelayRef.current, 30000);
        retryDelayRef.current = delay * 2;
        retryTimerRef.current = setTimeout(() => connect(sid), delay + Math.random() * 1000);
      }
    };

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

    return es;
  }, []);

  useEffect(() => {
    if (!sessionId) return;

    sessionTerminalRef.current = false;
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

  return { isConnected };
}
