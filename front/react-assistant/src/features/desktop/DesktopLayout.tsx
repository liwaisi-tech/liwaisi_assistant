import { useState, useCallback, useEffect, useMemo, useRef } from 'react';
import { useChat } from '../../hooks/useChat';
import { useExecutionMonitor } from '../../hooks/useExecutionMonitor';
import { StatusBar } from './StatusBar';
import { Dock } from './Dock';
import { MessageList } from '../chat/MessageList';
import { MessageInput } from '../chat/MessageInput';
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

  const monitor = useExecutionMonitor();

  // Memoize SSE callbacks so useChat's useSSE doesn't reconnect on every render
  const sseCallbacks = useMemo(() => ({
    onTransitionStarted: monitor.handleTransitionStarted,
    onTransitionCompleted: monitor.handleTransitionCompleted,
    onSubNetStarted: monitor.handleSubNetStarted,
    onSubNetCompleted: monitor.handleSubNetCompleted,
  }), [
    monitor.handleTransitionStarted,
    monitor.handleTransitionCompleted,
    monitor.handleSubNetStarted,
    monitor.handleSubNetCompleted,
  ]);

  const { messages, sessionState, sessionId, isConnected, sendMessage, resolveHITL, error } = useChat(userId, sseCallbacks);

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

  // Auto-load topology when switching to monitor (fetch from flows API)
  useEffect(() => {
    if (activeApp !== 'monitor' || monitor.state.topology) return;

    async function loadLatestTopology() {
      try {
        const flowList = await getFlows();
        if (flowList.items.length > 0) {
          const detail = await getFlow(flowList.items[0].hash);
          monitor.loadTopology(detail.topology);
        }
      } catch {
        // Best-effort — monitor will show empty state
      }
    }

    loadLatestTopology();
  }, [activeApp, monitor.state.topology, monitor.loadTopology]);

  // Reset monitor when session changes
  useEffect(() => {
    if (sessionId) {
      monitor.reset();
    }
  }, [sessionId]); // eslint-disable-line react-hooks/exhaustive-deps

  const handleLoadTrace = useCallback(async (sid: string) => {
    await monitor.loadExecutionTrace(sid);
  }, [monitor.loadExecutionTrace]);

  const handleOpenMonitor = useCallback(() => {
    setActiveApp('monitor');
  }, []);

  return (
    <div className="scan-lines flex flex-col h-dvh relative" style={{ backgroundColor: 'var(--bg-deep)' }}>
      <StatusBar sessionState={sessionState} isConnected={isConnected} />

      {activeApp === 'chat' && (
        <>
          <MessageList
            messages={messages}
            sessionState={sessionState}
            onSuggestionClick={sendMessage}
            onHITLAction={resolveHITL}
            onOpenMonitor={handleOpenMonitor}
          />
          <div className="pb-20">
            <MessageInput
              onSend={sendMessage}
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

      <Dock
        activeApp={activeApp}
        onChatClick={() => { setActiveApp('chat'); setSelectedFlowHash(null); }}
        onFlowsClick={() => { setActiveApp('flows'); setSelectedFlowHash(null); }}
        onMonitorClick={() => { setActiveApp('monitor'); }}
      />
    </div>
  );
}
