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
