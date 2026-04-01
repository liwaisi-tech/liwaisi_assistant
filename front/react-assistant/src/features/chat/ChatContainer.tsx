import { useState, useCallback } from 'react';
import { useChat } from '../../hooks/useChat';
import { ChatHeader } from './ChatHeader';
import { MessageList } from './MessageList';
import { MessageInput } from './MessageInput';
import { ConfirmClearDialog } from './ConfirmClearDialog';

interface ChatContainerProps {
  userId: string;
}

export function ChatContainer({ userId }: ChatContainerProps) {
  const { messages, sessionState, isConnected, sendMessage, clearConversation, resolveHITL, error } = useChat(userId);
  const [showClearDialog, setShowClearDialog] = useState(false);

  const canClear = sessionState !== 'running' && sessionState !== 'waiting';

  const handleClearRequest = useCallback(() => {
    if (messages.length === 0) {
      clearConversation();
      return;
    }
    setShowClearDialog(true);
  }, [messages.length, clearConversation]);

  const handleConfirmClear = useCallback(() => {
    setShowClearDialog(false);
    clearConversation();
  }, [clearConversation]);

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
