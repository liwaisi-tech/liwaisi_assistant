import { describe, it, expect } from 'vitest';
import { chatReducer, initialState, type ChatState } from './useChat';
import type { ChatMessage } from '../types/chat';
import type { StreamChunkData } from '../types/sse';
import { A2UI_MARKER } from '../features/chat/a2ui/constants';

// Helper: build a canned streaming assistant bubble.
function streamingBubble(overrides: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: 'assistant-1',
    role: 'assistant',
    content: '',
    isStreaming: true,
    cpnId: 'cpn-root',
    cpnRole: 'planner',
    timestamp: new Date('2026-04-13T00:00:00Z'),
    ...overrides,
  };
}

function chunk(overrides: Partial<StreamChunkData> = {}): StreamChunkData {
  return {
    SessionID: 'sess-1',
    CPNID: 'cpn-root',
    CPNRole: 'planner',
    Content: '',
    Done: false,
    ...overrides,
  };
}

const a2uiJson = '{"components":[{"type":"text","props":{"content":"hi"}}]}';
const a2uiContent = A2UI_MARKER + a2uiJson;

describe('chatReducer — STREAM_CHUNK', () => {
  it('closes every streaming assistant message on Done-sentinel chunk', () => {
    const state: ChatState = {
      ...initialState,
      sessionState: 'running',
      messages: [
        streamingBubble({ id: 'a', content: 'hello', isStreaming: true }),
        streamingBubble({ id: 'b', content: 'world', isStreaming: true, cpnId: 'cpn-other' }),
      ],
    };
    const next = chatReducer(state, {
      type: 'STREAM_CHUNK',
      data: chunk({ Content: '', Done: true }),
    });
    expect(next.sessionState).toBe('idle');
    expect(next.messages.every((m) => !m.isStreaming)).toBe(true);
  });

  it('creates a NEW assistant bubble when the first chunk carries the A2UI marker', () => {
    const next = chatReducer(initialState, {
      type: 'STREAM_CHUNK',
      data: chunk({ Content: a2uiContent, Done: false }),
    });
    expect(next.messages).toHaveLength(1);
    expect(next.messages[0].role).toBe('assistant');
    expect(next.messages[0].content.startsWith(A2UI_MARKER)).toBe(true);
    expect(next.messages[0].content).toBe(a2uiContent);
    expect(next.messages[0].isStreaming).toBe(true);
    expect(next.messages[0].cpnId).toBe('cpn-root');
  });

  it('closes prior streaming bubble and opens a NEW bubble on A2UI marker (AC-006 / REQ-008/009)', () => {
    const state: ChatState = {
      ...initialState,
      sessionState: 'running',
      messages: [streamingBubble({ content: 'thinking about your request...' })],
    };
    const next = chatReducer(state, {
      type: 'STREAM_CHUNK',
      data: chunk({ Content: a2uiContent, Done: false }),
    });
    expect(next.messages).toHaveLength(2);
    // Prior bubble: closed, content preserved (NOT concatenated with marker)
    expect(next.messages[0].content).toBe('thinking about your request...');
    expect(next.messages[0].isStreaming).toBe(false);
    // New bubble: starts with marker
    expect(next.messages[1].content).toBe(a2uiContent);
    expect(next.messages[1].isStreaming).toBe(true);
    expect(next.messages[1].role).toBe('assistant');
  });

  it('closes an A2UI bubble and opens a NEW bubble when a non-marker chunk follows (REQ-010 / AC-007)', () => {
    const state: ChatState = {
      ...initialState,
      sessionState: 'running',
      messages: [streamingBubble({ id: 'a2ui-1', content: a2uiContent })],
    };
    const next = chatReducer(state, {
      type: 'STREAM_CHUNK',
      data: chunk({ Content: 'follow-on plain text', Done: false }),
    });
    expect(next.messages).toHaveLength(2);
    // A2UI bubble preserved byte-identical
    expect(next.messages[0].content).toBe(a2uiContent);
    expect(next.messages[0].isStreaming).toBe(false);
    // Fresh plain bubble
    expect(next.messages[1].content).toBe('follow-on plain text');
    expect(next.messages[1].isStreaming).toBe(true);
  });

  it('concatenates plain text onto an existing matching-CPNID streaming bubble (regression baseline)', () => {
    const state: ChatState = {
      ...initialState,
      sessionState: 'running',
      messages: [streamingBubble({ content: 'Hello ' })],
    };
    const next = chatReducer(state, {
      type: 'STREAM_CHUNK',
      data: chunk({ Content: 'world', Done: false }),
    });
    expect(next.messages).toHaveLength(1);
    expect(next.messages[0].content).toBe('Hello world');
    expect(next.messages[0].isStreaming).toBe(true);
  });

  it('creates a fresh bubble when no matching streaming bubble exists (regression baseline)', () => {
    const next = chatReducer(initialState, {
      type: 'STREAM_CHUNK',
      data: chunk({ Content: 'first token', Done: false }),
    });
    expect(next.messages).toHaveLength(1);
    expect(next.messages[0].content).toBe('first token');
    expect(next.messages[0].isStreaming).toBe(true);
    expect(next.messages[0].role).toBe('assistant');
  });
});

// ── Session-tagged STREAM_CHUNK / rehydration mapping tests ──────────────────
// See spec-process-bugfix-a2ui-rehydration-completion.md REQ-302/303,
// REQ-401..404, AC-301/302/303/401/403/404. The originally-proposed
// 'generating' sessionState was collapsed onto the existing 'running' value
// (changelog 1.1) — visual behavior is identical because MessageList already
// shows the "thinking…" indicator whenever sessionState='running' and no
// streaming bubble exists yet.

describe('chatReducer — SESSION_LOADED hardening', () => {
  it('clears isStreaming on every rehydrated message (REQ-303 / AC-303)', () => {
    const messages: ChatMessage[] = [
      { id: 'u1', role: 'user', content: 'Hola', isStreaming: false, timestamp: new Date() },
      // Backend MUST never return isStreaming=true, but if a stale fixture leaks one through,
      // the reducer MUST normalize it to false before the message reaches the DOM.
      { id: 'a1', role: 'assistant', content: 'Hi', isStreaming: true as unknown as false, timestamp: new Date() },
    ];
    const next = chatReducer(initialState, {
      type: 'SESSION_LOADED',
      sessionId: 'sess-B',
      messages,
      state: 'idle',
    });
    expect(next.messages.every((m) => m.isStreaming === false)).toBe(true);
  });

  it('records the loaded sessionId on state', () => {
    const next = chatReducer(initialState, {
      type: 'SESSION_LOADED',
      sessionId: 'sess-B',
      messages: [],
      state: 'idle',
    });
    expect(next.sessionId).toBe('sess-B');
  });

  it('maps backend state=running to sessionState=running (REQ-401 / AC-401)', () => {
    const next = chatReducer(initialState, {
      type: 'SESSION_LOADED',
      sessionId: 'sess-B',
      messages: [{ id: 'u1', role: 'user', content: 'Hola', isStreaming: false, timestamp: new Date() }],
      state: 'running',
    });
    expect(next.sessionState).toBe('running');
  });

  it('maps backend state=hitl_pending to sessionState=idle so HITL renders without spinner (REQ-403 / AC-403)', () => {
    const next = chatReducer(initialState, {
      type: 'SESSION_LOADED',
      sessionId: 'sess-B',
      messages: [
        { id: 'u1', role: 'user', content: 'Hola', isStreaming: false, timestamp: new Date() },
        { id: 'a1', role: 'assistant', content: a2uiContent, isStreaming: false, timestamp: new Date() },
      ],
      state: 'hitl_pending',
    });
    // hitl_pending → idle so MessageList does not render the "thinking…"
    // spinner; the A2UI surface IS the affordance.
    expect(next.sessionState).toBe('idle');
  });

  it('maps backend state=terminal to sessionState=idle (REQ-404)', () => {
    const next = chatReducer(initialState, {
      type: 'SESSION_LOADED',
      sessionId: 'sess-B',
      messages: [],
      state: 'terminal',
    });
    expect(next.sessionState).toBe('idle');
  });
});

describe('chatReducer — STREAM_CHUNK session-id race defence', () => {
  it('drops a STREAM_CHUNK whose SessionID does not match state.sessionId (REQ-302 / AC-302)', () => {
    const state: ChatState = {
      ...initialState,
      sessionId: 'sess-B',
      messages: [{ id: 'u1', role: 'user', content: 'Hola', isStreaming: false, timestamp: new Date() }],
      sessionState: 'running',
    };
    const next = chatReducer(state, {
      type: 'STREAM_CHUNK',
      data: chunk({ SessionID: 'sess-A', Content: 'leaked from chat A', Done: false }),
    });
    // State unchanged — no leak from sess-A into sess-B's DOM.
    expect(next.messages).toHaveLength(1);
    expect(next.messages[0].content).toBe('Hola');
    expect(next.sessionState).toBe('running');
  });

  it('accepts a STREAM_CHUNK whose SessionID matches state.sessionId', () => {
    const state: ChatState = {
      ...initialState,
      sessionId: 'sess-B',
      sessionState: 'running',
    };
    const next = chatReducer(state, {
      type: 'STREAM_CHUNK',
      data: chunk({ SessionID: 'sess-B', Content: 'ok', Done: false }),
    });
    expect(next.messages).toHaveLength(1);
    expect(next.messages[0].content).toBe('ok');
  });

  it('still drops sessionId-mismatched chunks even when content is A2UI marker', () => {
    const state: ChatState = {
      ...initialState,
      sessionId: 'sess-B',
      messages: [],
      sessionState: 'idle',
    };
    const next = chatReducer(state, {
      type: 'STREAM_CHUNK',
      data: chunk({ SessionID: 'sess-A', Content: a2uiContent, Done: false }),
    });
    expect(next.messages).toHaveLength(0);
  });

  it('accepts chunks when state.sessionId is null (legacy / first-load tolerance)', () => {
    // When the reducer has not yet been told about a session id (sessionId is null),
    // the defence MUST NOT silently swallow chunks — that would regress the
    // baseline case where useChat hadn't yet dispatched SESSION_LOADED.
    const next = chatReducer(initialState, {
      type: 'STREAM_CHUNK',
      data: chunk({ SessionID: 'sess-A', Content: 'arrived early', Done: false }),
    });
    expect(next.messages).toHaveLength(1);
  });
});

describe('chatReducer — HITL custom-surface stamping', () => {
  // Regression: when t-clarify pushes its A2UI questionnaire via STREAM_CHUNK
  // and then signals HITL_REQUESTED with custom_surface=true, the reducer
  // must stamp hitlTransitionId onto that bubble. Otherwise HITL_RESOLVED
  // can't match it, the questionnaire never locks, and a second submit
  // hits the backend as 409 cpn.ErrNoHITLWaiting.
  it('stamps hitlTransitionId on the latest A2UI bubble when suppressBubble=true', () => {
    const afterStream = chatReducer(initialState, {
      type: 'STREAM_CHUNK',
      data: chunk({ Content: a2uiContent, Done: true }),
    });
    expect(afterStream.messages).toHaveLength(1);
    expect(afterStream.messages[0].hitlTransitionId).toBeUndefined();

    const afterRequest = chatReducer(afterStream, {
      type: 'HITL_REQUESTED',
      transitionId: 't-clarify',
      prompt: 'ignored',
      cpnId: 'cpn-root',
      cpnRole: 'planner',
      suppressBubble: true,
    });
    expect(afterRequest.messages).toHaveLength(1);
    expect(afterRequest.messages[0].hitlTransitionId).toBe('t-clarify');
    expect(afterRequest.sessionState).toBe('waiting');

    const afterResolve = chatReducer(afterRequest, {
      type: 'HITL_RESOLVED',
      transitionId: 't-clarify',
      action: 'submit',
      resolvedPayload: '{"q1":"a","q2":"b"}',
    });
    expect(afterResolve.messages[0].resolvedPayload).toBe('{"q1":"a","q2":"b"}');
    expect(afterResolve.messages[0].resolvedAt).toBeInstanceOf(Date);
    expect(afterResolve.sessionState).toBe('running');
  });

  it('only stamps the most recent matching A2UI bubble for the cpnId', () => {
    const state: ChatState = {
      ...initialState,
      messages: [
        // Older A2UI bubble for a different cpn — must NOT be stamped
        { id: 'a-old', role: 'assistant', content: a2uiContent, isStreaming: false,
          cpnId: 'cpn-other', timestamp: new Date() },
        // Most recent A2UI bubble for the target cpn — gets stamped
        { id: 'a-target', role: 'assistant', content: a2uiContent, isStreaming: false,
          cpnId: 'cpn-root', timestamp: new Date() },
      ],
    };
    const next = chatReducer(state, {
      type: 'HITL_REQUESTED',
      transitionId: 't-clarify',
      prompt: '',
      cpnId: 'cpn-root',
      cpnRole: 'planner',
      suppressBubble: true,
    });
    expect(next.messages[0].hitlTransitionId).toBeUndefined();
    expect(next.messages[1].hitlTransitionId).toBe('t-clarify');
  });

  it('falls back gracefully when no A2UI bubble exists yet', () => {
    const next = chatReducer(initialState, {
      type: 'HITL_REQUESTED',
      transitionId: 't-clarify',
      prompt: '',
      cpnId: 'cpn-root',
      cpnRole: 'planner',
      suppressBubble: true,
    });
    expect(next.messages).toHaveLength(0);
    expect(next.sessionState).toBe('waiting');
  });
});

describe('chatReducer — RESET hygiene', () => {
  it('clears sessionId, messages, and sessionState (REQ-301 / REQ-304 / AC-301)', () => {
    const state: ChatState = {
      ...initialState,
      sessionId: 'sess-A',
      messages: [{ id: 'a1', role: 'assistant', content: 'streaming...', isStreaming: true, timestamp: new Date() }],
      sessionState: 'running',
      error: 'old error',
    };
    const next = chatReducer(state, { type: 'RESET' });
    expect(next.sessionId).toBeNull();
    expect(next.messages).toHaveLength(0);
    expect(next.sessionState).toBe('idle');
    expect(next.error).toBeNull();
  });
});

describe('chatReducer — ACTIVITY_*', () => {
  const startBase = {
    type: 'ACTIVITY_START' as const,
    transitionId: 't-1',
    cpnId: 'cpn-root',
    sessionId: 'sess-1',
    verb: 'Thinking',
  };

  it('ACTIVITY_START sets currentActivity from a non-null DisplayLabel', () => {
    const state: ChatState = { ...initialState, sessionId: 'sess-1' };
    const next = chatReducer(state, startBase);
    expect(next.currentActivity).not.toBeNull();
    expect(next.currentActivity?.verb).toBe('Thinking');
    expect(next.currentActivity?.transitionId).toBe('t-1');
    expect(next.currentActivity?.cpnId).toBe('cpn-root');
  });

  it('ACTIVITY_START is idempotent — re-dispatch with same transitionId is a no-op', () => {
    const state: ChatState = { ...initialState, sessionId: 'sess-1' };
    const first = chatReducer(state, startBase);
    const second = chatReducer(first, startBase);
    expect(second).toBe(first);
  });

  it('ACTIVITY_START with detail keeps the detail on currentActivity', () => {
    const state: ChatState = { ...initialState, sessionId: 'sess-1' };
    const next = chatReducer(state, { ...startBase, verb: 'Calling tool', detail: 'web_search' });
    expect(next.currentActivity?.detail).toBe('web_search');
  });

  it('ACTIVITY_START drops events whose sessionId does not match active session (REQ-020)', () => {
    const state: ChatState = { ...initialState, sessionId: 'sess-A' };
    const next = chatReducer(state, { ...startBase, sessionId: 'sess-B' });
    expect(next).toBe(state);
  });

  it('ACTIVITY_START is accepted while sessionId is null (pre-load grace window)', () => {
    const state: ChatState = { ...initialState, sessionId: null };
    const next = chatReducer(state, startBase);
    expect(next.currentActivity).not.toBeNull();
  });

  it('ACTIVITY_END clears currentActivity and shows a receipt when totals are present', () => {
    const startState: ChatState = {
      ...initialState,
      sessionId: 'sess-1',
      currentActivity: {
        verb: 'Thinking',
        transitionId: 't-1',
        cpnId: 'cpn-root',
        startedAt: 1000,
      },
    };
    const next = chatReducer(startState, {
      type: 'ACTIVITY_END',
      transitionId: 't-1',
      sessionId: 'sess-1',
      durationMs: 2310,
      costUsd: 0.0041,
    });
    expect(next.currentActivity).toBeNull();
    expect(next.recentReceipt).not.toBeNull();
    expect(next.recentReceipt?.durationMs).toBe(2310);
    expect(next.recentReceipt?.costUsd).toBe(0.0041);
  });

  it('ACTIVITY_END clears currentActivity even when totals are absent (no receipt)', () => {
    const startState: ChatState = {
      ...initialState,
      sessionId: 'sess-1',
      currentActivity: {
        verb: 'Working',
        transitionId: 't-2',
        cpnId: 'cpn-root',
        startedAt: 1000,
      },
    };
    const next = chatReducer(startState, {
      type: 'ACTIVITY_END',
      transitionId: 't-2',
      sessionId: 'sess-1',
    });
    expect(next.currentActivity).toBeNull();
    expect(next.recentReceipt).toBeNull();
  });

  it('ACTIVITY_END drops events whose sessionId does not match (REQ-020)', () => {
    const startState: ChatState = {
      ...initialState,
      sessionId: 'sess-A',
      currentActivity: {
        verb: 'Thinking',
        transitionId: 't-1',
        cpnId: 'cpn-root',
        startedAt: 1000,
      },
    };
    const next = chatReducer(startState, {
      type: 'ACTIVITY_END',
      transitionId: 't-1',
      sessionId: 'sess-B',
    });
    expect(next).toBe(startState);
  });

  it('ACTIVITY_RECEIPT_DISMISS clears recentReceipt', () => {
    const startState: ChatState = {
      ...initialState,
      recentReceipt: { durationMs: 100, costUsd: 0, shownAt: 5000 },
    };
    const next = chatReducer(startState, { type: 'ACTIVITY_RECEIPT_DISMISS' });
    expect(next.recentReceipt).toBeNull();
  });

  it('SESSION_COMPLETED clears any in-flight currentActivity', () => {
    const startState: ChatState = {
      ...initialState,
      sessionState: 'running',
      currentActivity: {
        verb: 'Thinking',
        transitionId: 't-1',
        cpnId: 'cpn-root',
        startedAt: 1000,
      },
    };
    const next = chatReducer(startState, { type: 'SESSION_COMPLETED' });
    expect(next.currentActivity).toBeNull();
  });

  it('SESSION_FAILED clears any in-flight currentActivity (no receipt either)', () => {
    const startState: ChatState = {
      ...initialState,
      currentActivity: {
        verb: 'Working',
        transitionId: 't-1',
        cpnId: 'cpn-root',
        startedAt: 1000,
      },
    };
    const next = chatReducer(startState, { type: 'SESSION_FAILED' });
    expect(next.currentActivity).toBeNull();
    expect(next.recentReceipt).toBeNull();
  });

  it('HITL_REQUESTED clears the "Waiting for you" currentActivity (REQ-032)', () => {
    const startState: ChatState = {
      ...initialState,
      sessionId: 'sess-1',
      currentActivity: {
        verb: 'Waiting for you',
        transitionId: 't-hitl',
        cpnId: 'cpn-root',
        startedAt: 1000,
      },
    };
    const next = chatReducer(startState, {
      type: 'HITL_REQUESTED',
      transitionId: 't-hitl',
      prompt: 'Approve?',
      cpnId: 'cpn-root',
      cpnRole: 'planner',
    });
    expect(next.currentActivity).toBeNull();
    expect(next.sessionState).toBe('waiting');
  });

  it('HITL_REQUESTED with suppressBubble (custom-surface path) also clears currentActivity', () => {
    const startState: ChatState = {
      ...initialState,
      sessionId: 'sess-1',
      currentActivity: {
        verb: 'Waiting for you',
        transitionId: 't-hitl',
        cpnId: 'cpn-root',
        startedAt: 1000,
      },
      messages: [
        {
          id: 'a2ui-1',
          role: 'assistant',
          content: '$$a2ui:{"components":[]}',
          isStreaming: false,
          cpnId: 'cpn-root',
          timestamp: new Date(),
        },
      ],
    };
    const next = chatReducer(startState, {
      type: 'HITL_REQUESTED',
      transitionId: 't-hitl',
      prompt: '',
      cpnId: 'cpn-root',
      cpnRole: 'planner',
      suppressBubble: true,
    });
    expect(next.currentActivity).toBeNull();
  });

  it('RESET clears activity + receipt slices', () => {
    const startState: ChatState = {
      ...initialState,
      currentActivity: {
        verb: 'Thinking',
        transitionId: 't-1',
        cpnId: 'cpn-root',
        startedAt: 1000,
      },
      recentReceipt: { durationMs: 100, costUsd: 0, shownAt: 5000 },
    };
    const next = chatReducer(startState, { type: 'RESET' });
    expect(next.currentActivity).toBeNull();
    expect(next.recentReceipt).toBeNull();
  });
});

// ── Local message injection (REQ-GAP-TEST-003) ──────────────────────────────
// The model-admin `/models` shortcut relies on these two reducer actions to
// inject a synthetic assistant bubble and rewrite its content without ever
// hitting the SSE stream. Coverage here guards against either action
// regressing into a full-list re-render.

describe('chatReducer — INJECT_LOCAL_MESSAGE', () => {
  it('appends an assistant bubble with the given id and content', () => {
    const next = chatReducer(initialState, {
      type: 'INJECT_LOCAL_MESSAGE',
      id: 'local-1',
      content: 'synthetic body',
      cpnRole: 'models-admin',
    });
    expect(next.messages).toHaveLength(1);
    const bubble = next.messages[0];
    expect(bubble.id).toBe('local-1');
    expect(bubble.role).toBe('assistant');
    expect(bubble.content).toBe('synthetic body');
    expect(bubble.cpnRole).toBe('models-admin');
    expect(bubble.isStreaming).toBe(false);
  });

  it('preserves pre-existing messages', () => {
    const prior: ChatMessage = {
      id: 'u-1',
      role: 'user',
      content: 'hi',
      isStreaming: false,
      timestamp: new Date(),
    };
    const state: ChatState = { ...initialState, messages: [prior] };
    const next = chatReducer(state, {
      type: 'INJECT_LOCAL_MESSAGE',
      id: 'local-2',
      content: '$$a2ui:{}',
    });
    expect(next.messages).toHaveLength(2);
    expect(next.messages[0]).toBe(prior); // identity preserved → no re-render
  });
});

describe('chatReducer — UPDATE_MESSAGE_CONTENT', () => {
  it('rewrites the content of the matching id only', () => {
    const state: ChatState = {
      ...initialState,
      messages: [
        { id: 'a', role: 'assistant', content: 'x', isStreaming: false, timestamp: new Date() },
        { id: 'b', role: 'assistant', content: 'y', isStreaming: false, timestamp: new Date() },
      ],
    };
    const next = chatReducer(state, { type: 'UPDATE_MESSAGE_CONTENT', id: 'b', content: 'Y2' });
    expect(next.messages[0].content).toBe('x');
    expect(next.messages[0]).toBe(state.messages[0]); // untouched reference
    expect(next.messages[1].content).toBe('Y2');
  });

  it('is a no-op for an unknown id (returns structurally equivalent state)', () => {
    const state: ChatState = {
      ...initialState,
      messages: [
        { id: 'a', role: 'assistant', content: 'x', isStreaming: false, timestamp: new Date() },
      ],
    };
    const next = chatReducer(state, { type: 'UPDATE_MESSAGE_CONTENT', id: 'zzz', content: 'new' });
    expect(next.messages).toHaveLength(1);
    expect(next.messages[0].content).toBe('x');
  });
});

// ── Responding-model capture (REQ-GAP-IND-003) ─────────────────────────────
// The final stream_chunk for an LLM transition carries the resolved route's
// registry_id + adapter on `responding_model`. The reducer must persist it
// onto the matching bubble's `metadata.responding_model` so MessageBubble
// can render the RoundBadge without re-reading the SSE stream.

describe('chatReducer — STREAM_CHUNK responding_model capture', () => {
  it('stamps responding_model onto the completed bubble when append-done fires', () => {
    const state: ChatState = {
      ...initialState,
      messages: [streamingBubble({ id: 'a', content: 'partial ', isStreaming: true })],
    };
    const next = chatReducer(state, {
      type: 'STREAM_CHUNK',
      data: chunk({
        Content: 'final token',
        Done: true,
        responding_model: 'anthropic/claude-opus-4-6 · openrouter',
      }),
    });
    const bubble = next.messages[0];
    expect(bubble.isStreaming).toBe(false);
    expect(bubble.content).toBe('partial final token');
    expect(bubble.metadata?.responding_model).toBe('anthropic/claude-opus-4-6 · openrouter');
  });

  it('stamps responding_model onto a done-sentinel close-out', () => {
    const state: ChatState = {
      ...initialState,
      messages: [streamingBubble({ id: 'a', content: 'hello', isStreaming: true })],
    };
    const next = chatReducer(state, {
      type: 'STREAM_CHUNK',
      data: chunk({
        Content: '',
        Done: true,
        responding_model: 'google/gemma-4-31b-it · openrouter',
      }),
    });
    expect(next.messages[0].metadata?.responding_model).toBe('google/gemma-4-31b-it · openrouter');
    expect(next.messages[0].isStreaming).toBe(false);
  });

  it('leaves metadata empty when the final chunk omits responding_model', () => {
    const state: ChatState = {
      ...initialState,
      messages: [streamingBubble({ id: 'a', content: 'hi', isStreaming: true })],
    };
    const next = chatReducer(state, {
      type: 'STREAM_CHUNK',
      data: chunk({ Content: 'end', Done: true }),
    });
    expect(next.messages[0].metadata).toBeUndefined();
  });

  it('treats empty-string responding_model as absent (no metadata stamp)', () => {
    const next = chatReducer(initialState, {
      type: 'STREAM_CHUNK',
      data: chunk({ Content: 'solo', Done: true, responding_model: '' }),
    });
    expect(next.messages[0].metadata).toBeUndefined();
  });
});
