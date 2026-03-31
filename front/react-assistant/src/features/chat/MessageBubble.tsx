import { MarkdownContent } from './MarkdownContent.tsx';

interface MessageBubbleProps {
  role: 'user' | 'assistant';
  content: string;
  cpnRole?: string;
  timestamp: Date;
  isStreaming?: boolean;
}

export function MessageBubble({ role, content, cpnRole, timestamp, isStreaming }: MessageBubbleProps) {
  const isUser = role === 'user';

  return (
    <div className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}>
      <div
        className={`relative max-w-[85%] sm:max-w-[75%] rounded-2xl px-4 py-3 ${
          isUser ? 'rounded-br-md' : 'rounded-bl-md'
        }`}
        style={{
          backgroundColor: isUser ? 'var(--bg-user)' : 'var(--bg-assistant)',
          border: isUser ? 'none' : '1px solid var(--border-dim)',
        }}
      >
        {!isUser && cpnRole && (
          <span className="block text-[10px] font-medium uppercase tracking-widest mb-1.5"
                style={{ color: 'var(--accent)', fontFamily: "'JetBrains Mono', monospace" }}>
            {cpnRole}
          </span>
        )}

        {isUser ? (
          <p className="text-sm leading-relaxed whitespace-pre-wrap break-words"
             style={{ color: 'var(--text-primary)', margin: 0 }}>
            {content}
          </p>
        ) : (
          <MarkdownContent content={content} isStreaming={isStreaming ?? false} />
        )}

        <time className="block text-[10px] mt-2 tabular-nums"
              style={{ color: 'var(--text-muted)' }}
              dateTime={timestamp.toISOString()}>
          {formatTime(timestamp)}
        </time>
      </div>
    </div>
  );
}

function formatTime(date: Date): string {
  try {
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  } catch {
    return '';
  }
}
