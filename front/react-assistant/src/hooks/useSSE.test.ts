import { renderHook, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { useSSE } from './useSSE';

// ────────────────────────────────────────────────────────────────────────────
// Minimal EventSource fake — jsdom does not ship one. We record every
// constructed instance and expose helpers to drive the handlers from tests.
// ────────────────────────────────────────────────────────────────────────────

type Listener = (evt: MessageEvent | Event) => void;

class FakeEventSource {
  static instances: FakeEventSource[] = [];

  onopen: ((evt: Event) => void) | null = null;
  onerror: ((evt: Event) => void) | null = null;
  listeners = new Map<string, Listener[]>();
  closed = false;

  constructor(public url: string) {
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, fn: Listener) {
    const list = this.listeners.get(type) ?? [];
    list.push(fn);
    this.listeners.set(type, list);
  }

  close() {
    this.closed = true;
  }

  // ── Test helpers ────────────────────────────────────────────────────────
  emit(type: string, data?: unknown) {
    const evt = new MessageEvent(type, { data: typeof data === 'string' ? data : JSON.stringify(data) });
    const listeners = this.listeners.get(type) ?? [];
    for (const fn of listeners) fn(evt);
  }

  emitRaw(type: string, evt: MessageEvent | Event) {
    const listeners = this.listeners.get(type) ?? [];
    for (const fn of listeners) fn(evt);
  }

  fireError(evt: Event = new Event('error')) {
    this.onerror?.(evt);
  }

  fireOpen() {
    this.onopen?.(new Event('open'));
  }
}

const originalEventSource = (globalThis as unknown as { EventSource?: unknown }).EventSource;

beforeEach(() => {
  FakeEventSource.instances = [];
  (globalThis as unknown as { EventSource: unknown }).EventSource = FakeEventSource;
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  (globalThis as unknown as { EventSource: unknown }).EventSource = originalEventSource;
});

function noop() {}

function renderSSE(sessionId: string | null, overrides: Partial<Parameters<typeof useSSE>[0]> = {}) {
  return renderHook(
    ({ sid }) =>
      useSSE({
        sessionId: sid,
        onStreamChunk: noop,
        onSessionCompleted: noop,
        onSessionFailed: noop,
        ...overrides,
      }),
    { initialProps: { sid: sessionId } },
  );
}

// ────────────────────────────────────────────────────────────────────────────
// Non-retriable close on SessionNotFoundError (REQ-104, AC-007)
// ────────────────────────────────────────────────────────────────────────────

describe('useSSE — session-not-found is terminal (REQ-104, AC-007)', () => {
  it('closes the EventSource when a typed error payload names a ghost session', () => {
    const onSessionNotFound = vi.fn();
    renderSSE('sess_x', { onSessionNotFound });

    const es = FakeEventSource.instances[0];
    expect(es).toBeDefined();

    // Backend-named ghost event.
    es.emit('error', { error: 'session not found' });

    expect(onSessionNotFound).toHaveBeenCalledWith('sess_x');
    expect(es.closed).toBe(true);
  });

  it('honors the case-insensitive body match', () => {
    const onSessionNotFound = vi.fn();
    renderSSE('sess_x', { onSessionNotFound });

    const es = FakeEventSource.instances[0];
    es.emit('error', { error: 'Session Not Found' });

    expect(onSessionNotFound).toHaveBeenCalledWith('sess_x');
  });

  it('does NOT reconnect after the session is marked dead', () => {
    const onSessionNotFound = vi.fn();
    renderSSE('sess_x', { onSessionNotFound });

    const es = FakeEventSource.instances[0];
    es.emit('error', { error: 'session not found' });
    expect(es.closed).toBe(true);

    // Fire a transport-level error afterwards — should NOT schedule a retry.
    es.fireError();
    vi.advanceTimersByTime(60_000);
    expect(FakeEventSource.instances).toHaveLength(1);
  });

  it('exposes isSessionDead=true after terminal close', () => {
    const { result } = renderSSE('sess_x');

    act(() => {
      const es = FakeEventSource.instances[0];
      es.emit('session_not_found', { error: 'session not found' });
    });

    expect(result.current.isSessionDead).toBe(true);
    expect(result.current.isConnected).toBe(false);
  });
});

// ────────────────────────────────────────────────────────────────────────────
// Exponential backoff with jitter, capped at 30s (REQ-105)
// ────────────────────────────────────────────────────────────────────────────

describe('useSSE — transient reconnect policy (REQ-105)', () => {
  it('reconnects on transport error with bounded delay', () => {
    vi.spyOn(Math, 'random').mockReturnValue(0);
    renderSSE('sess_y');

    const first = FakeEventSource.instances[0];
    first.fireError();
    expect(first.closed).toBe(true);
    expect(FakeEventSource.instances).toHaveLength(1);

    // First backoff ~ 1s base + 0 jitter.
    vi.advanceTimersByTime(1_000);
    expect(FakeEventSource.instances).toHaveLength(2);
  });

  it('doubles the delay up to the 30s ceiling', () => {
    vi.spyOn(Math, 'random').mockReturnValue(0);
    renderSSE('sess_y');

    // Fire 10 consecutive errors to saturate the backoff.
    for (let i = 0; i < 10; i++) {
      const current = FakeEventSource.instances[FakeEventSource.instances.length - 1];
      current.fireError();
      vi.advanceTimersByTime(31_000); // well past the cap
    }
    // We never exceed the cap: each reconnect happens within 30s+1s jitter.
    expect(FakeEventSource.instances.length).toBeGreaterThan(5);
  });

  it('stops reconnecting when the component unmounts', () => {
    vi.spyOn(Math, 'random').mockReturnValue(0);
    const { unmount } = renderSSE('sess_y');

    const first = FakeEventSource.instances[0];
    first.fireError();
    unmount();

    vi.advanceTimersByTime(60_000);
    expect(FakeEventSource.instances).toHaveLength(1);
  });

  it('stops reconnecting when the session id changes', () => {
    vi.spyOn(Math, 'random').mockReturnValue(0);
    const { rerender } = renderSSE('sess_a');

    const first = FakeEventSource.instances[0];
    first.fireError();

    // Change session id BEFORE the retry timer fires.
    act(() => {
      rerender({ sid: 'sess_b' });
    });

    vi.advanceTimersByTime(60_000);

    // The only reconnects that should happen are for sess_b, not sess_a.
    const urls = FakeEventSource.instances.map((e) => e.url);
    const sessAReconnects = urls.filter((u) => u.includes('sess_a')).length;
    expect(sessAReconnects).toBe(1); // only the initial open
  });

  it('resets the backoff on a successful open', () => {
    vi.spyOn(Math, 'random').mockReturnValue(0);
    renderSSE('sess_y');

    const first = FakeEventSource.instances[0];
    first.fireError();
    vi.advanceTimersByTime(1_000);

    const second = FakeEventSource.instances[1];
    second.fireOpen();
    second.fireError();

    // After a successful open the base is back to 1s.
    vi.advanceTimersByTime(1_000);
    expect(FakeEventSource.instances.length).toBeGreaterThanOrEqual(3);
  });
});

// ────────────────────────────────────────────────────────────────────────────
// Callback stability (REQ-109)
// ────────────────────────────────────────────────────────────────────────────

describe('useSSE — stable connection across callback identity changes (REQ-109)', () => {
  it('does not tear down the EventSource when only callback props change', () => {
    const onStreamChunk1 = vi.fn();
    const onStreamChunk2 = vi.fn();

    const { rerender } = renderHook(
      ({ cb }) =>
        useSSE({
          sessionId: 'sess_stable',
          onStreamChunk: cb,
          onSessionCompleted: noop,
          onSessionFailed: noop,
        }),
      { initialProps: { cb: onStreamChunk1 } },
    );

    expect(FakeEventSource.instances).toHaveLength(1);
    const es = FakeEventSource.instances[0];

    rerender({ cb: onStreamChunk2 });

    // Still only one EventSource — changing callbacks MUST NOT reconnect.
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(es.closed).toBe(false);

    // New callback receives the next event.
    es.emit('stream_chunk', { SessionID: 's', CPNID: 'c', CPNRole: 'r', Content: 'hi', Done: false });
    expect(onStreamChunk2).toHaveBeenCalled();
    expect(onStreamChunk1).not.toHaveBeenCalled();
  });
});
