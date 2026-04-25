import { useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { loadNamespace } from '../../i18n/loadNamespace';
import { MessageBubble } from './MessageBubble';
import { ActivityBubble } from './ActivityBubble';
import { ProgressLog } from './ProgressLog';
import type { SessionState } from '../../types/api';
import type { ChatMessage, HITLAction } from '../../types/chat';
import type {
  AwakeningPhase,
  CurrentActivity,
  PendingToolApproval,
  ProgressStep,
  RecentReceipt,
} from '../../hooks/useChat';
import type { A2UIAction, A2UIPayload } from './a2ui/types';
import { ToolApprovalPrompt } from './ToolApprovalPrompt';

const SUGGESTION_KEYS = [
  'messageList.suggestions.explainCpn',
  'messageList.suggestions.helpGoCode',
  'messageList.suggestions.analyzeData',
  'messageList.suggestions.summarizeDoc',
] as const;

interface MessageListProps {
  messages: ChatMessage[];
  sessionState: SessionState;
  sessionId?: string | null;
  awakeningPhase?: AwakeningPhase;
  onSuggestionClick?: (prompt: string) => void;
  onHITLAction?: (transitionId: string, action: HITLAction, content?: string) => void;
  onA2UIAction?: (action: A2UIAction, messageId: string) => boolean;
  onOpenMonitor?: () => void;
  currentActivity?: CurrentActivity | null;
  recentReceipt?: RecentReceipt | null;
  onReceiptDismiss?: () => void;
  /** Outstanding first-run tool approval prompts (task #9). */
  pendingToolApprovals?: PendingToolApproval[];
  /** Called once a tool approval POST resolves successfully. */
  onToolApprovalResolved?: (requestId: string) => void;
  /** Persistent per-turn progress log (rendered above ActivityBubble). */
  progressSteps?: ProgressStep[];
}

export function MessageList({
  messages,
  sessionState,
  sessionId = null,
  awakeningPhase = 'complete',
  onSuggestionClick,
  onHITLAction,
  onA2UIAction,
  onOpenMonitor,
  currentActivity = null,
  recentReceipt = null,
  onReceiptDismiss,
  pendingToolApprovals = [],
  onToolApprovalResolved,
  progressSteps = [],
}: MessageListProps) {
  const { t } = useTranslation('chat');
  const bottomRef = useRef<HTMLDivElement>(null);

  useEffect(() => { loadNamespace('chat'); }, []);

  const lastMessage = messages.at(-1);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages.length, lastMessage?.content]);

  // Empty-state routing:
  //   • No session yet → generic welcome + suggestion chips (landing view).
  //   • Session active, awakening still running → "waking up" home card; the
  //     actual awakening A2UI card from the backend will replace this view
  //     as soon as it lands (becomes the first message and thus the home).
  //   • Session active, awakening complete but no messages → render nothing;
  //     the agent has spoken at least once and there's nothing useful to say.
  const showLandingWelcome = messages.length === 0 && !sessionId && sessionState === 'idle';
  const showWakingHome =
    messages.length === 0 && !!sessionId && awakeningPhase === 'pending';

  return (
    <div className="flex-1 overflow-y-auto chat-scroll">
      <div className="mx-auto w-full max-w-3xl px-4 py-6 space-y-4">
        {showWakingHome && (
          <div
            data-testid="awakening-home"
            className="flex flex-col items-center justify-center min-h-[50vh] gap-4 welcome-fade-in"
          >
            <span
              aria-hidden="true"
              className="activity-pulse"
              style={{
                display: 'inline-block',
                width: 14,
                height: 14,
                borderRadius: 9999,
                background: 'var(--accent)',
                boxShadow: '0 0 16px var(--accent-glow)',
              }}
            />
            <div className="text-center">
              <h2
                className="text-2xl font-semibold tracking-tight"
                style={{
                  fontFamily: "'JetBrains Mono', monospace",
                  color: 'var(--text-primary)',
                  letterSpacing: '-0.01em',
                }}
              >
                {t('messageList.wakingTitle')}
              </h2>
              <p className="text-sm mt-2" style={{ color: 'var(--text-secondary)' }}>
                {t('messageList.wakingSubtitle')}
              </p>
            </div>
          </div>
        )}

        {showLandingWelcome && (
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
                {t('messageList.welcomeTitle')}
              </h2>
              <p className="text-sm mt-2" style={{ color: 'var(--text-secondary)' }}>
                {t('messageList.welcomeSubtitle')}
              </p>
            </div>

            {/* Suggestion chips */}
            {onSuggestionClick && (
              <div className="flex flex-wrap justify-center gap-2 max-w-md">
                {SUGGESTION_KEYS.map((key) => {
                  const suggestion = t(key);
                  return (
                    <button
                      key={key}
                      onClick={() => onSuggestionClick(suggestion)}
                      className="suggestion-chip px-4 py-2 rounded-xl text-xs font-medium transition-all duration-200"
                      style={{
                        backgroundColor: 'var(--bg-surface)',
                        border: '1px solid var(--border-dim)',
                        color: 'var(--text-secondary)',
                        fontFamily: "'DM Sans', system-ui, sans-serif",
                      }}
                      aria-label={t('messageList.suggestionAriaLabel', { suggestion })}
                    >
                      {suggestion}
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        )}

        {messages.map((msg) => (
          <MessageBubble
            key={msg.id}
            id={msg.id}
            role={msg.role}
            content={msg.content}
            cpnId={msg.cpnId}
            cpnRole={msg.cpnRole}
            timestamp={msg.timestamp}
            isStreaming={msg.isStreaming}
            hitlTransitionId={msg.hitlTransitionId}
            hitlActions={msg.hitlActions}
            hitlResolved={msg.hitlResolved}
            resolvedPayload={msg.resolvedPayload}
            resolvedAt={msg.resolvedAt}
            metadata={msg.metadata}
            toolExecutions={msg.toolExecutions}
            onHITLAction={onHITLAction}
            onA2UIAction={onA2UIAction}
            onOpenMonitor={onOpenMonitor}
          />
        ))}

        {sessionId && pendingToolApprovals.length > 0 &&
          pendingToolApprovals.map((approval) => (
            <ToolApprovalPrompt
              key={approval.requestId}
              requestId={approval.requestId}
              preview={approval.preview as A2UIPayload}
              sessionId={sessionId}
              onResolved={() => onToolApprovalResolved?.(approval.requestId)}
            />
          ))}

        <ProgressLog steps={progressSteps} />

        <ActivityBubble
          activity={currentActivity}
          receipt={recentReceipt}
          onReceiptDismiss={onReceiptDismiss}
        />

        <div ref={bottomRef} />
      </div>
    </div>
  );
}
