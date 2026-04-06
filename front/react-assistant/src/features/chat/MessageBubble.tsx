import { memo, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { MarkdownContent } from './MarkdownContent.tsx';
import { useA2UIAdapter } from '../../hooks/useA2UIAdapter.ts';
import { A2UIProviderWrapper } from './a2ui/A2UIProviderWrapper.tsx';
import type { A2UIAction } from './a2ui/types.ts';
import type { HITLAction } from '../../types/chat';

interface MessageBubbleProps {
  role: 'user' | 'assistant';
  content: string;
  cpnId?: string;
  cpnRole?: string;
  timestamp: Date;
  isStreaming?: boolean;
  hitlTransitionId?: string;
  hitlActions?: HITLAction[];
  hitlResolved?: HITLAction;
  onHITLAction?: (transitionId: string, action: HITLAction) => void;
  onA2UIAction?: (action: A2UIAction) => void;
  onOpenMonitor?: () => void;
}

export const MessageBubble = memo(function MessageBubble({
  role, content, cpnId, cpnRole, timestamp, isStreaming,
  hitlTransitionId, hitlActions, hitlResolved, onHITLAction, onA2UIAction, onOpenMonitor,
}: MessageBubbleProps) {
  const { t } = useTranslation('chat');
  const isUser = role === 'user';
  const { detect, parse } = useA2UIAdapter();
  const a2uiPayload = !isUser ? parse(content) : null;
  const isA2UI = !isUser && detect(content) && a2uiPayload !== null;

  const handleA2UIAction = useCallback(
    (action: A2UIAction) => {
      onA2UIAction?.(action);
    },
    [onA2UIAction],
  );

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
          <div className="flex items-center gap-2 mb-1.5">
            <span className="text-[10px] font-medium uppercase tracking-widest"
                  style={{ color: 'var(--accent)', fontFamily: "'JetBrains Mono', monospace" }}>
              {cpnRole}
            </span>
            {cpnId && !isStreaming && onOpenMonitor && (
              <button
                onClick={onOpenMonitor}
                className="text-[9px] font-semibold px-1.5 py-0.5 rounded-full transition-all"
                style={{
                  color: 'var(--accent)',
                  border: '1px solid var(--accent)',
                  background: 'rgba(14, 165, 233, 0.08)',
                  fontFamily: "'JetBrains Mono', monospace",
                  cursor: 'pointer',
                  letterSpacing: '0.03em',
                }}
                title={t('messageBubble.openCpnMonitor')}
              >
                {t('messageBubble.cpnButton')}
              </button>
            )}
          </div>
        )}

        {isUser ? (
          <p className="text-sm leading-relaxed whitespace-pre-wrap break-words"
             style={{ color: 'var(--text-primary)', margin: 0 }}>
            {content}
          </p>
        ) : isA2UI && a2uiPayload ? (
          <A2UIProviderWrapper
            payload={a2uiPayload}
            isStreaming={isStreaming ?? false}
            onAction={handleA2UIAction}
          />
        ) : (
          <MarkdownContent content={content} isStreaming={isStreaming ?? false} />
        )}

        {hitlTransitionId && hitlActions && !hitlResolved && (
          <div className="flex gap-2 mt-3 pt-3" style={{ borderTop: '1px solid var(--border-dim)' }}>
            <button
              onClick={() => onHITLAction?.(hitlTransitionId, 'approve')}
              className="px-4 py-1.5 rounded-lg text-xs font-medium transition-colors"
              style={{
                backgroundColor: 'rgba(16, 185, 129, 0.15)',
                color: '#34d399',
                border: '1px solid rgba(16, 185, 129, 0.3)',
              }}
            >
              {t('messageBubble.looksGood')}
            </button>
            <button
              onClick={() => onHITLAction?.(hitlTransitionId, 'reject')}
              className="px-4 py-1.5 rounded-lg text-xs font-medium transition-colors"
              style={{
                backgroundColor: 'rgba(239, 68, 68, 0.1)',
                color: '#f87171',
                border: '1px solid rgba(239, 68, 68, 0.2)',
              }}
            >
              {t('messageBubble.cancel')}
            </button>
          </div>
        )}

        {hitlTransitionId && hitlResolved && (
          <div className="mt-3 pt-3" style={{ borderTop: '1px solid var(--border-dim)' }}>
            <span className="text-[11px] font-medium" style={{
              color: hitlResolved === 'approve' ? '#34d399' : '#f87171',
            }}>
              {hitlResolved === 'approve' ? t('messageBubble.youApproved') : t('messageBubble.youCancelled')}
            </span>
          </div>
        )}

        <time className="block text-[10px] mt-2 tabular-nums"
              style={{ color: 'var(--text-muted)' }}
              dateTime={timestamp.toISOString()}>
          {formatTime(timestamp)}
        </time>
      </div>
    </div>
  );
});

function formatTime(date: Date): string {
  try {
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  } catch {
    return '';
  }
}
