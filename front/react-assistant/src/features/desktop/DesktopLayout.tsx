import { useState, useCallback, useEffect, useMemo, useRef } from 'react';
import { useChat } from '../../hooks/useChat';
import { useChatList } from '../../hooks/useChatList';
import { useExecutionMonitor } from '../../hooks/useExecutionMonitor';
import { StatusBar } from './StatusBar';
import { Dock } from './Dock';
import { MessageList } from '../chat/MessageList';
import { MessageInput } from '../chat/MessageInput';
import { ChatSidebar } from '../chat/ChatSidebar';
import { ForkDialog } from '../chat/ForkDialog';
import { FlowBrowser } from '../cpn-visualizer/FlowBrowser';
import { FlowDetail } from '../cpn-visualizer/FlowDetail';
import { ExecutionMonitor } from '../execution-monitor/ExecutionMonitor';
import { getFlows, getFlow } from '../../services/api';

type ActiveApp = 'chat' | 'flows' | 'monitor';

interface DesktopLayoutProps {
  userId: string;
}

export function DesktopLayout({ userId }: DesktopLayoutProps) {
  const [activeApp, setActiveApp] = useState<ActiveApp>('chat');
  const [selectedFlowHash, setSelectedFlowHash] = useState<string | null>(null);
  const [forkingSessionId, setForkingSessionId] = useState<string | null>(null);

  const chatList = useChatList(userId);
  const monitor = useExecutionMonitor();

  // Memoize SSE callbacks so useChat's useSSE doesn't reconnect on every render
  const sseCallbacks = useMemo(() => ({
    onTransitionStarted: monitor.handleTransitionStarted,
    onTransitionCompleted: monitor.handleTransitionCompleted,
    onSubNetStarted: monitor.handleSubNetStarted,
    onSubNetCompleted: monitor.handleSubNetCompleted,
    onSessionCompleted: () => {
      // When session completes, refresh the chat list to get updated state
      chatList.refresh();
    },
  }), [
    monitor.handleTransitionStarted,
    monitor.handleTransitionCompleted,
    monitor.handleSubNetStarted,
    monitor.handleSubNetCompleted,
    chatList.refresh,
  ]);

  const { messages, sessionState, sessionId, isConnected, sendMessage, resolveHITL, error } = useChat(
    chatList.activeSessionId,
    sseCallbacks,
  );

  // Track message count to detect new user messages -> new execution
  const prevMessageCountRef = useRef(0);
  useEffect(() => {
    const currentCount = messages.length;
    const prev = prevMessageCountRef.current;
    prevMessageCountRef.current = currentCount;

    if (currentCount > prev && currentCount > 0) {
      const lastMsg = messages[currentCount - 1];
      if (lastMsg.role === 'user') {
        // User sent a new message -> start a new execution run
        monitor.startNewExecution(lastMsg.content);
      }
    }
  }, [messages.length]); // eslint-disable-line react-hooks/exhaustive-deps

  // Helper: load topology and execution trace for a session
  const loadMonitorDataForSession = useCallback(async (sid: string) => {
    try {
      const flowList = await getFlows();
      if (flowList.items.length > 0) {
        const detail = await getFlow(flowList.items[0].hash);
        monitor.loadTopology(detail.topology);
      }
    } catch {
      // Best-effort — no flows crystallized yet
    }

    try {
      await monitor.loadExecutionTrace(sid);
    } catch {
      // No events yet for this session
    }
  }, [monitor.loadTopology, monitor.loadExecutionTrace]);

  // Reset monitor and reload execution data when the active session changes
  const prevMonitorSessionRef = useRef<string | null>(null);
  useEffect(() => {
    if (!sessionId || sessionId === prevMonitorSessionRef.current) return;
    prevMonitorSessionRef.current = sessionId;

    monitor.reset();
    loadMonitorDataForSession(sessionId);
  }, [sessionId]); // eslint-disable-line react-hooks/exhaustive-deps

  // When session goes from running → idle and we still have no topology,
  // the CPN run just completed and persistAfterRun crystallized the flow.
  // Retry loading the topology and execution trace.
  const prevSessionStateRef = useRef<string>(sessionState);
  useEffect(() => {
    const wasRunning = prevSessionStateRef.current === 'running';
    prevSessionStateRef.current = sessionState;

    if (wasRunning && sessionState === 'idle' && sessionId) {
      // CPN run just finished — flow is now persisted, reload monitor data
      loadMonitorDataForSession(sessionId);
      // Refresh chat list to pick up auto-generated title and updated cost
      chatList.refresh();
    }
  }, [sessionState, sessionId, loadMonitorDataForSession]);

  const handleLoadTrace = useCallback(async (sid: string) => {
    await monitor.loadExecutionTrace(sid);
  }, [monitor.loadExecutionTrace]);

  const handleOpenMonitor = useCallback(() => {
    setActiveApp('monitor');
  }, []);

  // When session is completed and user sends a message, create a new chat
  const handleSendMessage = useCallback(async (content: string) => {
    if (sessionState === 'completed') {
      const newId = await chatList.createChat();
      if (newId) {
        // Small delay to allow SSE to connect before sending
        setTimeout(() => sendMessage(content), 200);
      }
    } else {
      sendMessage(content);
    }
  }, [sessionState, chatList.createChat, sendMessage]);

  // Fork handler — opens the fork dialog
  const handleForkRequest = useCallback((chatId: string) => {
    setForkingSessionId(chatId);
  }, []);

  // Fork confirm
  const handleForkConfirm = useCallback((messageIndex: number) => {
    if (forkingSessionId) {
      chatList.forkChat(forkingSessionId, messageIndex);
    }
    setForkingSessionId(null);
  }, [forkingSessionId, chatList.forkChat]);

  return (
    <div className="scan-lines flex flex-col h-dvh relative" style={{ backgroundColor: 'var(--bg-deep)' }}>
      <StatusBar sessionState={sessionState} isConnected={isConnected} />

      <div className="flex flex-1 min-h-0 relative">
        {/* Chat Sidebar — only visible in chat mode */}
        {activeApp === 'chat' && (
          <ChatSidebar
            chats={chatList.chats}
            activeSessionId={chatList.activeSessionId}
            isLoading={chatList.isLoading}
            hasMore={chatList.hasMore}
            onSelect={chatList.switchChat}
            onCreate={chatList.createChat}
            onRename={chatList.renameChat}
            onDelete={chatList.deleteChat}
            onFork={handleForkRequest}
            onLoadMore={chatList.loadMore}
          />
        )}

        {/* Main content area */}
        <div className="flex-1 flex flex-col min-w-0">
          {activeApp === 'chat' && (
            <>
              <MessageList
                messages={messages}
                sessionState={sessionState}
                onSuggestionClick={handleSendMessage}
                onHITLAction={resolveHITL}
                onOpenMonitor={handleOpenMonitor}
              />
              <div className="pb-20">
                <MessageInput
                  onSend={handleSendMessage}
                  disabled={sessionState === 'running' || sessionState === 'waiting'}
                  sessionState={sessionState}
                  error={error}
                />
              </div>
            </>
          )}

          {activeApp === 'flows' && !selectedFlowHash && (
            <FlowBrowser onSelectFlow={(hash) => setSelectedFlowHash(hash)} />
          )}

          {activeApp === 'flows' && selectedFlowHash && (
            <FlowDetail hash={selectedFlowHash} onBack={() => setSelectedFlowHash(null)} />
          )}

          {activeApp === 'monitor' && (
            <ExecutionMonitor
              state={monitor.state}
              selectedRun={monitor.selectedRun}
              onSelectRun={monitor.selectRun}
              onSelectTransition={monitor.selectTransition}
              onNavigateCPN={monitor.navigateCPN}
              onLoadTrace={handleLoadTrace}
              sessionId={sessionId}
            />
          )}
        </div>
      </div>

      <Dock
        activeApp={activeApp}
        onChatClick={() => { setActiveApp('chat'); setSelectedFlowHash(null); }}
        onFlowsClick={() => { setActiveApp('flows'); setSelectedFlowHash(null); }}
        onMonitorClick={() => { setActiveApp('monitor'); }}
      />

      {/* Fork Dialog */}
      {forkingSessionId && (
        <ForkDialog
          messages={messages}
          onFork={handleForkConfirm}
          onClose={() => setForkingSessionId(null)}
        />
      )}
    </div>
  );
}
