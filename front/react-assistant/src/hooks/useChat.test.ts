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
