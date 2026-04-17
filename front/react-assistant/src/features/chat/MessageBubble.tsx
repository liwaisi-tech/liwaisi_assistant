import { memo, useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { A2UIProviderWrapper } from './a2ui/A2UIProviderWrapper.tsx';
import type { A2UIPayload, A2UIAction } from './a2ui/types.ts';
import { A2UI_MARKER } from './a2ui/constants.ts';
import type { HITLAction } from '../../types/chat';

interface MessageBubbleProps {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  cpnId?: string;
  cpnRole?: string;
  timestamp: Date;
  isStreaming?: boolean;
  hitlTransitionId?: string;
  hitlActions?: HITLAction[];
  hitlResolved?: HITLAction;
  // Resolution props that lock an A2UI surface (e.g. the questionnaire)
  // into a read-only state showing what the user submitted. Populated by
  // the chat reducer on SESSION_LOADED rehydration and on live HITL
  // submission (REQ-102..105).
  resolvedPayload?: string;
  resolvedAt?: Date;
  /**
   * Transport metadata bag surfaced from SSE payloads. Today the only
   * consumer is `metadata.responding_model`, rendered as a RoundBadge
   * below assistant bubbles (REQ-GAP-IND-004). Absent → no badge; the
   * badge is also suppressed for user messages and HITL surfaces
   * (REQ-GAP-IND-005).
   */
  metadata?: {
    responding_model?: string;
  };
  onHITLAction?: (transitionId: string, action: HITLAction, content?: string) => void;
  // Catch-all for non-HITL A2UI actions (e.g. the `model:*` action family
  // emitted by the in-chat model-admin surface). Called only when the
  // HITL branches don't match; the messageId lets the handler update the
  // same surface in place.
  onA2UIAction?: (action: A2UIAction, messageId: string) => boolean;
  onOpenMonitor?: () => void;
}

/**
 * formatRespondingModel strips the vendor prefix from a registry id so the
 * badge reads "claude-opus-4-6 · openrouter" instead of the full
 * "anthropic/claude-opus-4-6 · openrouter". Returns the raw string back if
 * no slash is present so we never swallow the adapter hint.
 * REQ-GAP-IND-004.
 */
function formatRespondingModel(raw: string): string {
  const slash = raw.indexOf('/');
  if (slash === -1) return raw;
  return raw.slice(slash + 1);
}

/**
 * Parse A2UI payload from message content.
 * Returns the parsed payload if content (after trimming leading ASCII
 * whitespace) starts with $$a2ui:, otherwise builds a default text
 * component payload for markdown rendering. REQ-011 / REQ-012.
 *
 * Fall-through preserves the ORIGINAL (untrimmed) content so that plain
 * messages which legitimately begin with whitespace render unchanged.
 */
function parsePayload(content: string, isStreaming: boolean): A2UIPayload {
  const trimmed = content.replace(/^\s+/, '');
  if (trimmed.startsWith(A2UI_MARKER)) {
    try {
      return JSON.parse(trimmed.slice(A2UI_MARKER.length));
    } catch {
      // Malformed or partial A2UI JSON — fall through to text rendering
    }
  }

  // Default: wrap plain content in a text component (renders via MarkdownContent)
  return {
    components: content
      ? [{ type: 'text', props: { content, isStreaming } }]
      : [],
  };
}

export const MessageBubble = memo(function MessageBubble({
  id, role, content, cpnId, cpnRole, timestamp, isStreaming,
  hitlTransitionId, hitlResolved, resolvedPayload, resolvedAt,
  metadata,
  onHITLAction, onA2UIAction, onOpenMonitor,
}: MessageBubbleProps) {
  const { t } = useTranslation('chat');
  const isUser = role === 'user';
  const [reviseMode, setReviseMode] = useState(false);
  const [reviseText, setReviseText] = useState('');
  // Transition id captured from the A2UI "Request Changes" button. Needed
  // because A2UI-rendered review cards carry the id on the button component,
  // not on the MessageBubble's hitlTransitionId prop.
  const [reviseTransitionId, setReviseTransitionId] = useState<string | null>(null);

  // Build A2UI payload — either from backend A2UI content or wrapping text
  const payload = useMemo<A2UIPayload | null>(() => {
    if (isUser) return null;
    return parsePayload(content, isStreaming ?? false);
  }, [isUser, content, isStreaming]);

  // Responding-model badge (REQ-GAP-IND-004/005):
  //   • only for assistant messages
  //   • suppressed while streaming (value is final-chunk-only) so the badge
  //     does not flicker in during token-by-token rendering
  //   • suppressed on HITL surfaces — the user is reading an affordance,
  //     not an LLM answer
  //   • suppressed when the bubble carries a $$a2ui: payload whose
  //     componentCatalog entries cover interactive management surfaces
  //   • absent metadata → no badge (backward-compat for rehydrated rows)
  const respondingModelLabel = useMemo<string | null>(() => {
    if (isUser) return null;
    if (isStreaming) return null;
    if (hitlTransitionId || hitlResolved) return null;
    const raw = metadata?.responding_model?.trim();
    if (!raw) return null;
    return formatRespondingModel(raw);
  }, [isUser, isStreaming, hitlTransitionId, hitlResolved, metadata?.responding_model]);

  // Route A2UI actions — HITL actions dispatch to the resolver
  const handleA2UIAction = useCallback(
    (action: A2UIAction) => {
      if (action.type === 'hitl:revise') {
        setReviseTransitionId(action.componentId ?? hitlTransitionId ?? null);
        setReviseMode(true);
        return;
      }
      if (action.type === 'hitl:submit' && action.componentId) {
        const answers = (action.payload as { answers?: Record<string, string> } | null)?.answers ?? {};
        onHITLAction?.(action.componentId, 'submit', JSON.stringify(answers));
        return;
      }
      if (action.type.startsWith('hitl:') && action.componentId) {
        const hitlAction = action.type.replace('hitl:', '') as HITLAction;
        onHITLAction?.(action.componentId, hitlAction);
        return;
      }
      // Fallback for non-HITL A2UI actions (e.g. `model:*`). If no handler
      // claims the action, it is silently dropped — consistent with the
      // existing HITL branch's no-op when hitlTransitionId is missing.
      onA2UIAction?.(action, id);
    },
    [onHITLAction, hitlTransitionId, onA2UIAction, id],
  );

  const handleReviseSubmit = useCallback(() => {
    const transitionId = reviseTransitionId ?? hitlTransitionId;
    if (!reviseText.trim() || !transitionId) return;
    onHITLAction?.(transitionId, 'revise', reviseText.trim());
    setReviseMode(false);
    setReviseText('');
    setReviseTransitionId(null);
  }, [reviseText, reviseTransitionId, hitlTransitionId, onHITLAction]);

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
        {/* CPN role badge */}
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

        {/* Content — user: plain text, assistant: A2UI native */}
        {isUser ? (
          <p className="text-sm leading-relaxed whitespace-pre-wrap break-words"
             style={{ color: 'var(--text-primary)', margin: 0 }}>
            {content}
          </p>
        ) : payload ? (
          <A2UIProviderWrapper
            payload={payload}
            isStreaming={isStreaming ?? false}
            onAction={handleA2UIAction}
            resolvedPayload={resolvedPayload}
            resolvedAt={resolvedAt}
          />
        ) : null}

        {/* Revise mode: text input for feedback */}
        {reviseMode && (
          <div className="flex flex-col gap-2 mt-3 pt-3" style={{ borderTop: '1px solid var(--border-dim)' }}>
            <textarea
              value={reviseText}
              onChange={(e) => setReviseText(e.target.value)}
              placeholder={t('messageBubble.reviseplaceholder', 'Describe what changes you want...')}
              rows={3}
              className="w-full px-3 py-2 rounded-lg text-sm outline-none resize-none"
              style={{
                backgroundColor: 'var(--bg-input)',
                color: 'var(--text-primary)',
                border: '1px solid var(--border-dim)',
                fontFamily: "'DM Sans', system-ui, sans-serif",
              }}
              autoFocus
            />
            <div className="flex gap-2">
              <button
                onClick={handleReviseSubmit}
                disabled={!reviseText.trim()}
                className="px-4 py-1.5 rounded-lg text-xs font-medium transition-all cursor-pointer"
                style={{
                  backgroundColor: 'rgba(14, 165, 233, 0.15)',
                  color: 'var(--accent)',
                  border: '1px solid rgba(14, 165, 233, 0.3)',
                  opacity: reviseText.trim() ? 1 : 0.5,
                  cursor: reviseText.trim() ? 'pointer' : 'not-allowed',
                }}
              >
                {t('messageBubble.sendChanges', 'Send Changes')}
              </button>
              <button
                onClick={() => { setReviseMode(false); setReviseText(''); setReviseTransitionId(null); }}
                className="px-4 py-1.5 rounded-lg text-xs font-medium transition-all cursor-pointer"
                style={{
                  backgroundColor: 'rgba(255, 255, 255, 0.05)',
                  color: 'var(--text-secondary)',
                  border: '1px solid var(--border-dim)',
                }}
              >
                {t('messageBubble.cancelRevise', 'Cancel')}
              </button>
            </div>
          </div>
        )}

        {/* HITL resolved badge — only for review-card actions. 'submit'
            (questionnaire answers) is already narrated by the locked
            questionnaire surface itself ("Respondido a las HH:MM" plus
            the answers), so a second badge here would contradict it —
            the reject fallback color/copy made a submit read as cancelled. */}
        {hitlResolved && hitlResolved !== 'submit' && (
          <div className="mt-3 pt-3" style={{ borderTop: '1px solid var(--border-dim)' }}>
            <span className="text-[11px] font-medium" style={{
              color: hitlResolved === 'approve' ? '#34d399' : hitlResolved === 'revise' ? 'var(--accent)' : '#f87171',
            }}>
              {hitlResolved === 'approve'
                ? t('messageBubble.youApproved')
                : hitlResolved === 'revise'
                  ? t('messageBubble.youRequestedChanges', 'You requested changes')
                  : t('messageBubble.youCancelled')}
            </span>
          </div>
        )}

        {/* Responding-model RoundBadge — REQ-GAP-IND-004.
            Strips the vendor prefix for scannability (e.g. "claude-opus-4-6
            · openrouter") while preserving the full registry id on hover.
            Rendered bottom-right of the bubble content area with caption
            styling, using only existing design tokens (GUD-IND-001). */}
        {respondingModelLabel && (
          <div className="mt-2 flex justify-end">
            <span
              data-testid="responding-model-badge"
              title={t('models.badge.responding_model', {
                defaultValue: 'Responding: {{model}}',
                model: metadata?.responding_model,
              })}
              className="inline-flex items-center gap-1 text-[10px] font-medium uppercase tracking-widest px-2 py-0.5 rounded-full"
              style={{
                color: 'var(--text-muted)',
                border: '1px solid var(--border-dim)',
                background: 'var(--bg-input)',
                fontFamily: "'JetBrains Mono', monospace",
              }}
            >
              <span aria-hidden="true" style={{ color: 'var(--accent)' }}>{'\u25C9'}</span>
              <span>{respondingModelLabel}</span>
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
