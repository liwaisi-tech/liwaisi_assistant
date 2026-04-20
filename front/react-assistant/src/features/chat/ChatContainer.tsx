import { useState, useCallback } from 'react';
import { useChat } from '../../hooks/useChat';
import { ChatHeader, type ConnectionState } from './ChatHeader';
import { MessageList } from './MessageList';
import { MessageInput } from './MessageInput';
import { ConfirmClearDialog } from './ConfirmClearDialog';
import { SessionResumedNotice } from './SessionResumedNotice';
import { useModelAdminFlow } from './modelAdmin/useModelAdminFlow';
import type { A2UIAction } from './a2ui/types';

interface ChatContainerProps {
  sessionId: string | null;
  onClearConversation?: () => void;
  /**
   * When auto-recovery (REQ-102/REQ-111) picks a fresh session id after a
   * ghost-session failure, `useChatList` should bump this counter / timestamp
   * so the inline notice surfaces. `null` / `0` means "never resumed".
   * Wiring happens in Agent 2's `useChatList` change.
   */
  resumedAt?: number | null;
  /**
   * Three-state indicator (REQ-112). When Agent 2 exposes this through
   * `useSSE` / `useChat`, pass it through; otherwise ChatContainer falls back
   * to the boolean `isConnected`.
   */
  connectionState?: ConnectionState;
}

export function ChatContainer({
  sessionId,
  onClearConversation,
  resumedAt = null,
  connectionState,
}: ChatContainerProps) {
  const {
    messages,
    sessionState,
    isConnected,
    sendMessage,
    resolveHITL,
    error,
    notice,
    clearNotice,
    currentActivity,
    recentReceipt,
    dismissReceipt,
    injectLocalMessage,
    updateMessageContent,
    sendUserAction,
  } = useChat(sessionId);
  const [showClearDialog, setShowClearDialog] = useState(false);

  const modelAdmin = useModelAdminFlow({ injectLocalMessage, updateMessageContent });

  // Intercept model-admin slash commands before hitting the backend. A
  // recognized command is handled entirely client-side — we never call
  // sendMessage, so no user bubble is emitted for the `/models` token.
  const handleSend = useCallback(
    (content: string) => {
      if (modelAdmin.tryHandleSlashCommand(content)) return;
      void sendMessage(content);
    },
    [modelAdmin, sendMessage],
  );

  /**
   * handleA2UIAction is the MessageList → MessageBubble catch-all for A2UI
   * buttons that don't match the HITL branches. `model:*` actions stay on
   * the client (useModelAdminFlow's admin-REST shortcut). Every other
   * action — e.g. the CPN-emitted `submit_register`, `apply_confirm` —
   * becomes a v0.8 userAction envelope sent through the chat wire
   * (REQ-GAP-REG-002 / REQ-FE-006). Always returns true once the router
   * has handled the action so MessageBubble does not double-dispatch.
   */
  const handleA2UIAction = useCallback(
    (action: A2UIAction, messageId: string): boolean => {
      if (modelAdmin.tryHandleA2UIAction(action, messageId)) return true;
      void sendUserAction(action);
      return true;
    },
    [modelAdmin, sendUserAction],
  );

  const canClear = sessionState !== 'running' && sessionState !== 'waiting';

  const handleClearRequest = useCallback(() => {
    if (!onClearConversation) return;
    if (messages.length === 0) {
      onClearConversation();
      return;
    }
    setShowClearDialog(true);
  }, [messages.length, onClearConversation]);

  const handleConfirmClear = useCallback(() => {
    setShowClearDialog(false);
    onClearConversation?.();
  }, [onClearConversation]);

  return (
    <div className="scan-lines flex flex-col h-dvh" style={{ backgroundColor: 'var(--bg-deep)' }}>
      <ChatHeader
        sessionState={sessionState}
        connectionState={connectionState}
        isConnected={isConnected}
        onClearConversation={handleClearRequest}
        clearDisabled={!canClear}
      />
      <SessionResumedNotice resumedAt={resumedAt} />
      <MessageList
        messages={messages}
        sessionState={sessionState}
        onSuggestionClick={handleSend}
        onHITLAction={resolveHITL}
        onA2UIAction={handleA2UIAction}
        currentActivity={currentActivity}
        recentReceipt={recentReceipt}
        onReceiptDismiss={dismissReceipt}
      />
      {notice && (
        <div className="px-4 pt-2">
          <div
            role="status"
            data-testid="chat-notice"
            className="mx-auto max-w-3xl flex items-center justify-between gap-3 px-3 py-2 rounded-lg text-xs font-medium"
            style={{
              backgroundColor: 'var(--bg-input)',
              color: 'var(--text-secondary)',
              border: '1px solid var(--border-dim)',
              fontFamily: "'DM Sans', system-ui, sans-serif",
            }}
          >
            <span>{notice}</span>
            <button
              type="button"
              onClick={clearNotice}
              aria-label="Dismiss"
              className="text-[11px] px-2 py-0.5 rounded-md transition-colors cursor-pointer"
              style={{
                color: 'var(--text-muted)',
                border: '1px solid var(--border-dim)',
                backgroundColor: 'transparent',
              }}
            >
              {'\u2715'}
            </button>
          </div>
        </div>
      )}
      <MessageInput
        onSend={handleSend}
        disabled={sessionState === 'running' || sessionState === 'waiting'}
        sessionState={sessionState}
        error={error}
      />
      {showClearDialog && (
        <ConfirmClearDialog
          onConfirm={handleConfirmClear}
          onCancel={() => setShowClearDialog(false)}
        />
      )}
    </div>
  );
}
