import { useState, useCallback } from 'react';
import { useChat } from '../../hooks/useChat';
import { ChatHeader, type ConnectionState } from './ChatHeader';
import { MessageList } from './MessageList';
import { MessageInput } from './MessageInput';
import { ConfirmClearDialog } from './ConfirmClearDialog';
import { SessionResumedNotice } from './SessionResumedNotice';

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
    currentActivity,
    recentReceipt,
    dismissReceipt,
  } = useChat(sessionId);
  const [showClearDialog, setShowClearDialog] = useState(false);

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
        onSuggestionClick={sendMessage}
        onHITLAction={resolveHITL}
        currentActivity={currentActivity}
        recentReceipt={recentReceipt}
        onReceiptDismiss={dismissReceipt}
      />
      <MessageInput
        onSend={sendMessage}
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
