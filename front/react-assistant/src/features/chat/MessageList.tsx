import { useEffect, useRef } from 'react';
import { MessageBubble } from './MessageBubble';
import { StreamingIndicator } from './StreamingIndicator';
import type { SessionState } from '../../types/api';
import type { ChatMessage, HITLAction } from '../../types/chat';

const SUGGESTIONS = [
  'Explain CPN architecture',
  'Help me write Go code',
  'Analyze my data',
  'Summarize a document',
];

interface MessageListProps {
  messages: ChatMessage[];
  sessionState: SessionState;
  onSuggestionClick?: (prompt: string) => void;
  onHITLAction?: (transitionId: string, action: HITLAction) => void;
  onOpenMonitor?: () => void;
}

export function MessageList({ messages, sessionState, onSuggestionClick, onHITLAction, onOpenMonitor }: MessageListProps) {
  const bottomRef = useRef<HTMLDivElement>(null);

  const lastMessage = messages.at(-1);
  const hasStreamingMessage = lastMessage?.isStreaming === true;

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages.length, lastMessage?.content]);

  const showThinking =
    sessionState === 'running' &&
    !hasStreamingMessage &&
    lastMessage?.role === 'user';

  return (
    <div className="flex-1 overflow-y-auto chat-scroll">
      <div className="mx-auto w-full max-w-3xl px-4 py-6 space-y-4">
        {messages.length === 0 && sessionState === 'idle' && (
          <div className="flex flex-col items-center justify-center min-h-[50vh] gap-6 welcome-fade-in">
            {/* Sparkle icon */}
            <svg width="40" height="40" viewBox="0 0 24 24" fill="none" aria-hidden="true" className="welcome-sparkle">
              <path
                d="M12 2L14.5 9.5L22 12L14.5 14.5L12 22L9.5 14.5L2 12L9.5 9.5L12 2Z"
                fill="var(--accent)"
                opacity="0.8"
              />
              <path
                d="M12 2L14.5 9.5L22 12L14.5 14.5L12 22L9.5 14.5L2 12L9.5 9.5L12 2Z"
                fill="none"
                stroke="var(--accent)"
                strokeWidth="0.5"
                opacity="0.3"
              />
            </svg>

            {/* Wordmark */}
            <div className="text-center">
              <h2
                className="text-3xl font-bold tracking-tight"
                style={{
                  fontFamily: "'JetBrains Mono', monospace",
                  color: 'var(--accent)',
                  textShadow: '0 0 24px var(--accent-glow)',
                }}
              >
                Liwaisi
              </h2>
              <p className="text-sm mt-2" style={{ color: 'var(--text-secondary)' }}>
                Your AI-powered workspace
              </p>
            </div>

            {/* Suggestion chips */}
            {onSuggestionClick && (
              <div className="flex flex-wrap justify-center gap-2 max-w-md">
                {SUGGESTIONS.map((suggestion) => (
                  <button
                    key={suggestion}
                    onClick={() => onSuggestionClick(suggestion)}
                    className="suggestion-chip px-4 py-2 rounded-xl text-xs font-medium transition-all duration-200"
                    style={{
                      backgroundColor: 'var(--bg-surface)',
                      border: '1px solid var(--border-dim)',
                      color: 'var(--text-secondary)',
                      fontFamily: "'DM Sans', system-ui, sans-serif",
                    }}
                    aria-label={`Send: ${suggestion}`}
                  >
                    {suggestion}
                  </button>
                ))}
              </div>
            )}
          </div>
        )}

        {messages.map((msg) => (
          <MessageBubble
            key={msg.id}
            role={msg.role}
            content={msg.content}
            cpnId={msg.cpnId}
            cpnRole={msg.cpnRole}
            timestamp={msg.timestamp}
            isStreaming={msg.isStreaming}
            hitlTransitionId={msg.hitlTransitionId}
            hitlActions={msg.hitlActions}
            hitlResolved={msg.hitlResolved}
            onHITLAction={onHITLAction}
            onOpenMonitor={onOpenMonitor}
          />
        ))}

        {showThinking && <StreamingIndicator />}

        <div ref={bottomRef} />
      </div>
    </div>
  );
}
