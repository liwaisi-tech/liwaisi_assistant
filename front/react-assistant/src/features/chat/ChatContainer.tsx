import { useState, useCallback } from 'react';
import { useChat } from '../../hooks/useChat';
import { ChatHeader } from './ChatHeader';
import { MessageList } from './MessageList';
import { MessageInput } from './MessageInput';
import { ConfirmClearDialog } from './ConfirmClearDialog';

interface ChatContainerProps {
  sessionId: string | null;
  onClearConversation?: () => void;
}

export function ChatContainer({ sessionId, onClearConversation }: ChatContainerProps) {
  const { messages, sessionState, isConnected, sendMessage, resolveHITL, error } = useChat(sessionId);
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
        isConnected={isConnected}
        onClearConversation={handleClearRequest}
        clearDisabled={!canClear}
      />
      <MessageList
        messages={messages}
        sessionState={sessionState}
        onSuggestionClick={sendMessage}
        onHITLAction={resolveHITL}
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
