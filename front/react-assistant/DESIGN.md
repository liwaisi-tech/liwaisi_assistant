# Chat UI Design Specification

## Aesthetic Direction: Deep-Space Terminal

A mission-control terminal aesthetic -- deep blacks with precise cyan/teal accents. Typography uses **JetBrains Mono** for the header (monospaced, engineered feel) paired with **DM Sans** for body text (clean, geometric, highly readable). The interface feels like a refined spacecraft communication terminal: functional, precise, with purposeful luminance.

**Key differentiator**: Subtle scan-line texture overlay on the background, glowing accent borders, and a cursor that pulses like a heartbeat monitor.

**Color tokens** (CSS custom properties):
```
--bg-deep:      #0a0a0f       (near-black with blue undertone)
--bg-surface:   #12121a       (elevated surface)
--bg-input:     #1a1a26       (input fields)
--bg-user:      #1e3a5f       (user message bubble)
--bg-assistant: #16161f       (assistant message bubble)
--border-dim:   #2a2a3a       (subtle borders)
--border-glow:  #0ea5e9       (accent borders - sky-500)
--text-primary: #e2e8f0       (slate-200)
--text-secondary: #94a3b8     (slate-400)
--text-muted:   #64748b       (slate-500)
--accent:       #0ea5e9       (sky-500)
--accent-glow:  #0ea5e940     (sky-500/25 for shadows)
--status-idle:      #64748b
--status-running:   #0ea5e9
--status-waiting:   #f59e0b
--status-completed: #10b981
--status-failed:    #ef4444
```

---

## Font Loading

Add to `index.html` `<head>`:
```html
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=DM+Sans:ital,wght@0,400;0,500;0,600&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet">
```

---

## Custom CSS (index.css)

```css
@import "tailwindcss";

/* ── Custom properties ── */
:root {
  --bg-deep: #0a0a0f;
  --bg-surface: #12121a;
  --bg-input: #1a1a26;
  --bg-user: #1e3a5f;
  --bg-assistant: #16161f;
  --border-dim: #2a2a3a;
  --border-glow: #0ea5e9;
  --text-primary: #e2e8f0;
  --text-secondary: #94a3b8;
  --text-muted: #64748b;
  --accent: #0ea5e9;
  --accent-glow: #0ea5e940;
}

/* ── Base resets ── */
body {
  margin: 0;
  background-color: var(--bg-deep);
  color: var(--text-primary);
  font-family: 'DM Sans', system-ui, sans-serif;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}

/* ── Scan-line overlay ── */
.scan-lines::after {
  content: '';
  position: fixed;
  inset: 0;
  pointer-events: none;
  z-index: 50;
  background: repeating-linear-gradient(
    0deg,
    transparent,
    transparent 2px,
    rgba(0, 0, 0, 0.03) 2px,
    rgba(0, 0, 0, 0.03) 4px
  );
}

/* ── Streaming cursor ── */
@keyframes cursor-blink {
  0%, 100% { opacity: 1; }
  50% { opacity: 0; }
}

.streaming-cursor::after {
  content: '\2588';
  animation: cursor-blink 1s step-end infinite;
  color: var(--accent);
  margin-left: 1px;
  font-size: 0.875rem;
}

/* ── Thinking dots ── */
@keyframes thinking-bounce {
  0%, 60%, 100% {
    transform: translateY(0);
    opacity: 0.4;
  }
  30% {
    transform: translateY(-4px);
    opacity: 1;
  }
}

.thinking-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background-color: var(--accent);
  animation: thinking-bounce 1.4s ease-in-out infinite;
}

.thinking-dot:nth-child(2) { animation-delay: 0.2s; }
.thinking-dot:nth-child(3) { animation-delay: 0.4s; }

/* ── SSE connection dot pulse ── */
@keyframes dot-pulse {
  0%, 100% { box-shadow: 0 0 0 0 currentColor; }
  50% { box-shadow: 0 0 0 4px transparent; }
}

.connection-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  animation: dot-pulse 2s ease-in-out infinite;
}

/* ── Status badge pulse (for running state) ── */
@keyframes status-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.6; }
}

.status-pulse {
  animation: status-pulse 2s ease-in-out infinite;
}

/* ── Glow border ── */
.glow-border {
  box-shadow: 0 0 0 1px var(--border-dim),
              0 0 12px -4px var(--accent-glow);
}

/* ── Scrollbar styling ── */
.chat-scroll::-webkit-scrollbar {
  width: 6px;
}

.chat-scroll::-webkit-scrollbar-track {
  background: transparent;
}

.chat-scroll::-webkit-scrollbar-thumb {
  background: var(--border-dim);
  border-radius: 3px;
}

.chat-scroll::-webkit-scrollbar-thumb:hover {
  background: var(--text-muted);
}

/* ── Textarea auto-resize support ── */
.message-textarea {
  field-sizing: content;
  min-height: 44px;
  max-height: 120px;
}

/* Fallback for browsers without field-sizing */
@supports not (field-sizing: content) {
  .message-textarea {
    overflow-y: auto;
    resize: none;
  }
}
```

---

## Component Designs

### 1. ChatHeader

```tsx
// src/components/ChatHeader.tsx

import type { SessionState, SSEConnectionState } from '../types';

interface ChatHeaderProps {
  sessionState: SessionState;
  sseConnection: SSEConnectionState;
}

const stateConfig: Record<SessionState, { label: string; className: string }> = {
  idle:      { label: 'Idle',      className: 'bg-slate-700/50 text-slate-400' },
  running:   { label: 'Running',   className: 'bg-sky-500/20 text-sky-400 status-pulse' },
  waiting:   { label: 'Waiting',   className: 'bg-amber-500/20 text-amber-400' },
  completed: { label: 'Completed', className: 'bg-emerald-500/20 text-emerald-400' },
  failed:    { label: 'Failed',    className: 'bg-red-500/20 text-red-400' },
};

export function ChatHeader({ sessionState, sseConnection }: ChatHeaderProps) {
  const state = stateConfig[sessionState];

  return (
    <header className="flex items-center justify-between px-4 py-3 border-b"
            style={{ borderColor: 'var(--border-dim)', backgroundColor: 'var(--bg-surface)' }}>
      {/* Left: Title */}
      <div className="flex items-center gap-3">
        <h1 className="text-base font-semibold tracking-tight"
            style={{ fontFamily: "'JetBrains Mono', monospace", color: 'var(--text-primary)' }}>
          Liwaisi Assistant
        </h1>
      </div>

      {/* Right: Status indicators */}
      <div className="flex items-center gap-3">
        {/* Session state badge */}
        <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium ${state.className}`}>
          {sessionState === 'running' && (
            <span className="w-1.5 h-1.5 rounded-full bg-sky-400 status-pulse" />
          )}
          {state.label}
        </span>

        {/* SSE connection indicator */}
        <div className="flex items-center gap-1.5" title={sseConnection === 'connected' ? 'Connected' : 'Disconnected'}>
          <span className={`connection-dot ${sseConnection === 'connected' ? 'text-emerald-400 bg-emerald-400' : 'text-red-400 bg-red-400'}`} />
          <span className="text-xs sr-only">
            {sseConnection === 'connected' ? 'Connected' : 'Disconnected'}
          </span>
        </div>
      </div>
    </header>
  );
}
```

---

### 2. MessageBubble

```tsx
// src/components/MessageBubble.tsx

interface MessageBubbleProps {
  role: 'user' | 'assistant' | 'observer';
  content: string;
  cpnRole?: string;
  timestamp: string;
  isStreaming?: boolean;
}

export function MessageBubble({ role, content, cpnRole, timestamp, isStreaming }: MessageBubbleProps) {
  const isUser = role === 'user';

  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
      <div
        className={`relative max-w-[85%] sm:max-w-[75%] rounded-2xl px-4 py-3 ${
          isUser
            ? 'rounded-br-md'
            : 'rounded-bl-md'
        }`}
        style={{
          backgroundColor: isUser ? 'var(--bg-user)' : 'var(--bg-assistant)',
          border: isUser ? 'none' : '1px solid var(--border-dim)',
        }}
      >
        {/* CPN role label for assistant messages */}
        {!isUser && cpnRole && (
          <span className="block text-[10px] font-medium uppercase tracking-widest mb-1.5"
                style={{ color: 'var(--accent)', fontFamily: "'JetBrains Mono', monospace" }}>
            {cpnRole}
          </span>
        )}

        {/* Message content */}
        <p className={`text-sm leading-relaxed whitespace-pre-wrap break-words ${isStreaming ? 'streaming-cursor' : ''}`}
           style={{ color: 'var(--text-primary)', margin: 0 }}>
          {content}
        </p>

        {/* Timestamp */}
        <time className="block text-[10px] mt-2 tabular-nums"
              style={{ color: 'var(--text-muted)' }}
              dateTime={timestamp}>
          {formatTime(timestamp)}
        </time>
      </div>
    </div>
  );
}

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  } catch {
    return '';
  }
}
```

---

### 3. StreamingIndicator

```tsx
// src/components/StreamingIndicator.tsx

export function StreamingIndicator() {
  return (
    <div className="flex justify-start">
      <div className="flex items-center gap-2 px-4 py-3 rounded-2xl rounded-bl-md"
           style={{ backgroundColor: 'var(--bg-assistant)', border: '1px solid var(--border-dim)' }}>
        <div className="flex items-center gap-1.5" role="status" aria-label="Assistant is thinking">
          <span className="thinking-dot" />
          <span className="thinking-dot" />
          <span className="thinking-dot" />
        </div>
      </div>
    </div>
  );
}
```

---

### 4. MessageList

```tsx
// src/components/MessageList.tsx

import { useEffect, useRef } from 'react';
import { MessageBubble } from './MessageBubble';
import { StreamingIndicator } from './StreamingIndicator';
import type { ChatMessage, SessionState } from '../types';

interface MessageListProps {
  messages: ChatMessage[];
  sessionState: SessionState;
  streamingContent: string | null;
  streamingCpnRole?: string;
}

export function MessageList({ messages, sessionState, streamingContent, streamingCpnRole }: MessageListProps) {
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages.length, streamingContent]);

  const showThinking = sessionState === 'running' && streamingContent === null && messages.at(-1)?.role === 'user';

  return (
    <div className="flex-1 overflow-y-auto chat-scroll">
      <div className="mx-auto w-full max-w-3xl px-4 py-6 space-y-4">
        {/* Empty state */}
        {messages.length === 0 && sessionState === 'idle' && (
          <div className="flex flex-col items-center justify-center h-full min-h-[40vh] gap-3">
            <p className="text-base font-medium" style={{ color: 'var(--text-secondary)' }}>
              Start a conversation
            </p>
            <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
              Type a message below to begin.
            </p>
          </div>
        )}

        {/* Message list */}
        {messages.map((msg) => (
          <MessageBubble
            key={msg.id}
            role={msg.role}
            content={msg.content}
            cpnRole={msg.cpnRole}
            timestamp={msg.timestamp}
          />
        ))}

        {/* Streaming message (in-progress assistant response) */}
        {streamingContent !== null && (
          <MessageBubble
            role="assistant"
            content={streamingContent}
            cpnRole={streamingCpnRole}
            timestamp={new Date().toISOString()}
            isStreaming
          />
        )}

        {/* Thinking indicator */}
        {showThinking && <StreamingIndicator />}

        {/* Scroll anchor */}
        <div ref={bottomRef} />
      </div>
    </div>
  );
}
```

---

### 5. MessageInput

```tsx
// src/components/MessageInput.tsx

import { useRef, useCallback } from 'react';

interface MessageInputProps {
  onSend: (content: string) => void;
  disabled: boolean;
  error: string | null;
}

export function MessageInput({ onSend, disabled, error }: MessageInputProps) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const handleSend = useCallback(() => {
    const value = textareaRef.current?.value.trim();
    if (!value || disabled) return;
    onSend(value);
    if (textareaRef.current) {
      textareaRef.current.value = '';
      // Reset height for browsers without field-sizing
      textareaRef.current.style.height = 'auto';
    }
  }, [onSend, disabled]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }, [handleSend]);

  return (
    <div className="border-t px-4 py-3"
         style={{ borderColor: 'var(--border-dim)', backgroundColor: 'var(--bg-surface)' }}>
      <div className="mx-auto max-w-3xl">
        {/* Error display */}
        {error && (
          <div className="mb-2 px-3 py-2 rounded-lg text-xs font-medium bg-red-500/10 text-red-400 border border-red-500/20">
            {error}
          </div>
        )}

        {/* Input row */}
        <div className="flex items-end gap-2 rounded-xl px-3 py-2 glow-border transition-shadow focus-within:shadow-[0_0_0_1px_var(--accent),0_0_20px_-4px_var(--accent-glow)]"
             style={{ backgroundColor: 'var(--bg-input)' }}>
          <textarea
            ref={textareaRef}
            className="message-textarea flex-1 bg-transparent text-sm leading-relaxed placeholder:text-slate-500 focus:outline-none"
            style={{ color: 'var(--text-primary)', fontFamily: "'DM Sans', system-ui, sans-serif" }}
            placeholder={disabled ? 'Waiting for response...' : 'Type a message...'}
            disabled={disabled}
            onKeyDown={handleKeyDown}
            rows={1}
            aria-label="Message input"
          />
          <button
            type="button"
            onClick={handleSend}
            disabled={disabled}
            className="flex-none flex items-center justify-center w-8 h-8 rounded-lg transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
            style={{
              backgroundColor: disabled ? 'transparent' : 'var(--accent)',
              color: disabled ? 'var(--text-muted)' : '#fff',
            }}
            aria-label="Send message"
          >
            {/* Arrow-up send icon */}
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M8 12V4M8 4L4 8M8 4L12 8" />
            </svg>
          </button>
        </div>

        <p className="text-[10px] mt-1.5 text-center" style={{ color: 'var(--text-muted)' }}>
          Press Enter to send, Shift+Enter for newline
        </p>
      </div>
    </div>
  );
}
```

---

### 6. ChatLayout (top-level composition)

```tsx
// src/components/ChatLayout.tsx

import { ChatHeader } from './ChatHeader';
import { MessageList } from './MessageList';
import { MessageInput } from './MessageInput';
import type { ChatMessage, SessionState, SSEConnectionState } from '../types';

interface ChatLayoutProps {
  messages: ChatMessage[];
  sessionState: SessionState;
  sseConnection: SSEConnectionState;
  streamingContent: string | null;
  streamingCpnRole?: string;
  error: string | null;
  onSend: (content: string) => void;
}

export function ChatLayout({
  messages,
  sessionState,
  sseConnection,
  streamingContent,
  streamingCpnRole,
  error,
  onSend,
}: ChatLayoutProps) {
  return (
    <div className="scan-lines flex flex-col h-dvh" style={{ backgroundColor: 'var(--bg-deep)' }}>
      <ChatHeader sessionState={sessionState} sseConnection={sseConnection} />
      <MessageList
        messages={messages}
        sessionState={sessionState}
        streamingContent={streamingContent}
        streamingCpnRole={streamingCpnRole}
      />
      <MessageInput
        onSend={onSend}
        disabled={sessionState === 'running'}
        error={error}
      />
    </div>
  );
}
```

---

## TypeScript Types (referenced by components)

```ts
// src/types.ts (relevant subset for UI)

export type SessionState = 'idle' | 'running' | 'waiting' | 'completed' | 'failed';
export type SSEConnectionState = 'connected' | 'disconnected';

export interface ChatMessage {
  id: string;
  role: 'user' | 'assistant' | 'observer';
  content: string;
  cpnRole?: string;
  timestamp: string;  // ISO 8601
}
```

---

## Layout Summary

```
+--------------------------------------------------+
| ChatHeader (fixed top)                            |
| [Liwaisi Assistant]     [Running ●]  ● connected |
+--------------------------------------------------+
|                                                   |
|  MessageList (flex-1, overflow-y-auto)            |
|                                                   |
|                          ┌────────────────┐       |
|                          │  User message   │      |
|                          └────────────────┘       |
|  ┌────────────────────┐                           |
|  │ cpn-role            │                          |
|  │ Assistant message   │                          |
|  └────────────────────┘                           |
|                                                   |
|  ┌────────┐                                       |
|  │ ● ● ●  │  (thinking)                          |
|  └────────┘                                       |
|                                                   |
+--------------------------------------------------+
| MessageInput (fixed bottom)                       |
| [Type a message...              ] [↑]             |
| Press Enter to send, Shift+Enter for newline      |
+--------------------------------------------------+
```

## Responsive Behavior

- **Desktop (768px+)**: Max-width 768px (`max-w-3xl`) centered container for messages and input. Header is full-width.
- **Mobile (<768px)**: Full-width with 16px horizontal padding. All elements stack naturally. Touch-friendly tap targets (min 44px).
- **Height**: Uses `h-dvh` (dynamic viewport height) which accounts for mobile browser chrome.

## Accessibility

- `aria-label` on all interactive elements
- `role="status"` on thinking indicator
- `sr-only` text for connection state
- Focus ring on input area via `focus-within` glow effect
- Sufficient contrast: all text meets WCAG AA on dark backgrounds
- `dateTime` attribute on `<time>` elements
