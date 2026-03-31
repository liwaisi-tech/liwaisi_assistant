import { useChat } from '../../hooks/useChat';
import { StatusBar } from './StatusBar';
import { Dock } from './Dock';
import { MessageList } from '../chat/MessageList';
import { MessageInput } from '../chat/MessageInput';

interface DesktopLayoutProps {
  userId: string;
}

export function DesktopLayout({ userId }: DesktopLayoutProps) {
  const { messages, sessionState, isConnected, sendMessage, resolveHITL, error } = useChat(userId);

  return (
    <div className="scan-lines flex flex-col h-dvh relative" style={{ backgroundColor: 'var(--bg-deep)' }}>
      <StatusBar sessionState={sessionState} isConnected={isConnected} />

      <MessageList
        messages={messages}
        sessionState={sessionState}
        onSuggestionClick={sendMessage}
        onHITLAction={resolveHITL}
      />

      <div className="pb-20">
        <MessageInput
          onSend={sendMessage}
          disabled={sessionState === 'running' || sessionState === 'waiting'}
          sessionState={sessionState}
          error={error}
        />
      </div>

      <Dock />
    </div>
  );
}
