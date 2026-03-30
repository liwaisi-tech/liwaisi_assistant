import { useEffect, useRef } from 'react';
import { MessageBubble } from './MessageBubble';
import { StreamingIndicator } from './StreamingIndicator';
import type { SessionState } from '../../types/api';
import type { ChatMessage } from '../../types/chat';

interface MessageListProps {
  messages: ChatMessage[];
  sessionState: SessionState;
}

export function MessageList({ messages, sessionState }: MessageListProps) {
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
          <div className="flex flex-col items-center justify-center h-full min-h-[40vh] gap-3">
            <p className="text-base font-medium" style={{ color: 'var(--text-secondary)' }}>
              Start a conversation
            </p>
            <p className="text-sm" style={{ color: 'var(--text-muted)' }}>
              Type a message below to begin.
            </p>
          </div>
        )}

        {messages.map((msg) => (
          <MessageBubble
            key={msg.id}
            role={msg.role}
            content={msg.content}
            cpnRole={msg.cpnRole}
            timestamp={msg.timestamp}
            isStreaming={msg.isStreaming}
          />
        ))}

        {showThinking && <StreamingIndicator />}

        <div ref={bottomRef} />
      </div>
    </div>
  );
}
