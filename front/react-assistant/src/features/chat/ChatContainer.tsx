import { useChat } from '../../hooks/useChat';
import { ChatHeader } from './ChatHeader';
import { MessageList } from './MessageList';
import { MessageInput } from './MessageInput';

interface ChatContainerProps {
  userId: string;
}

export function ChatContainer({ userId }: ChatContainerProps) {
  const { messages, sessionState, isConnected, sendMessage, error } = useChat(userId);

  return (
    <div className="scan-lines flex flex-col h-dvh" style={{ backgroundColor: 'var(--bg-deep)' }}>
      <ChatHeader sessionState={sessionState} isConnected={isConnected} />
      <MessageList messages={messages} sessionState={sessionState} />
      <MessageInput
        onSend={sendMessage}
        disabled={sessionState === 'running'}
        error={error}
      />
    </div>
  );
}
