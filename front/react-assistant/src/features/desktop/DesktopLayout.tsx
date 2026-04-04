import { useState, useCallback, useEffect, useMemo, useRef } from 'react';
import { useChat } from '../../hooks/useChat';
import { useChatList } from '../../hooks/useChatList';
import { useExecutionMonitor } from '../../hooks/useExecutionMonitor';
import { useIsMobile } from '../../hooks/useMediaQuery';
import { useKeyboardShortcuts } from '../../hooks/useKeyboardShortcuts';
import { StatusBar } from './StatusBar';
import { NavigationRail } from './NavigationRail';
import { CommandPalette } from './CommandPalette';
import type { PaletteAction } from './CommandPalette';
import { AdaptivePanel } from './AdaptivePanel';
import { MobileTabBar } from './MobileTabBar';
import { MessageList } from '../chat/MessageList';
import { MessageInput } from '../chat/MessageInput';
import { ChatSidebar } from '../chat/ChatSidebar';
import { ForkDialog } from '../chat/ForkDialog';
import { FlowBrowser } from '../cpn-visualizer/FlowBrowser';
import { FlowDetail } from '../cpn-visualizer/FlowDetail';
import { ExecutionMonitor } from '../execution-monitor/ExecutionMonitor';
import { getFlows, getFlow } from '../../services/api';

type ActiveApp = 'chat' | 'flows' | 'monitor';
type PanelContent = 'flows' | 'monitor' | null;

interface DesktopLayoutProps {
  userId: string;
}

export function DesktopLayout({ userId }: DesktopLayoutProps) {
  const [activeApp, setActiveApp] = useState<ActiveApp>('chat');
  const [panelContent, setPanelContent] = useState<PanelContent>(null);
  const [isRailExpanded, setIsRailExpanded] = useState(false);
  const [isPaletteOpen, setIsPaletteOpen] = useState(false);
  const [selectedFlowHash, setSelectedFlowHash] = useState<string | null>(null);
  const [selectedPanelFlowHash, setSelectedPanelFlowHash] = useState<string | null>(null);
  const [forkingSessionId, setForkingSessionId] = useState<string | null>(null);

  const isMobile = useIsMobile();
  const chatList = useChatList(userId);
  const monitor = useExecutionMonitor();

  // Memoize SSE callbacks so useChat's useSSE doesn't reconnect on every render
  const sseCallbacks = useMemo(() => ({
    onTransitionStarted: monitor.handleTransitionStarted,
    onTransitionCompleted: monitor.handleTransitionCompleted,
    onSubNetStarted: monitor.handleSubNetStarted,
    onSubNetCompleted: monitor.handleSubNetCompleted,
    onSessionCompleted: () => {
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

  // When session goes from running → idle, reload monitor data
  const prevSessionStateRef = useRef<string>(sessionState);
  useEffect(() => {
    const wasRunning = prevSessionStateRef.current === 'running';
    prevSessionStateRef.current = sessionState;

    if (wasRunning && sessionState === 'idle' && sessionId) {
      loadMonitorDataForSession(sessionId);
      chatList.refresh();
    }
  }, [sessionState, sessionId, loadMonitorDataForSession]);

  const handleLoadTrace = useCallback(async (sid: string) => {
    await monitor.loadExecutionTrace(sid);
  }, [monitor.loadExecutionTrace]);

  const handleOpenMonitor = useCallback(() => {
    setActiveApp('monitor');
    setPanelContent(null);
    setIsRailExpanded(false);
  }, []);

  // When session is completed and user sends a message, create a new chat
  const handleSendMessage = useCallback(async (content: string) => {
    if (sessionState === 'completed') {
      const newId = await chatList.createChat();
      if (newId) {
        setTimeout(() => sendMessage(content), 200);
      }
    } else {
      sendMessage(content);
    }
  }, [sessionState, chatList.createChat, sendMessage]);

  // Fork handler
  const handleForkRequest = useCallback((chatId: string) => {
    setForkingSessionId(chatId);
  }, []);

  const handleForkConfirm = useCallback((messageIndex: number) => {
    if (forkingSessionId) {
      chatList.forkChat(forkingSessionId, messageIndex);
    }
    setForkingSessionId(null);
  }, [forkingSessionId, chatList.forkChat]);

  // ── Navigation handlers ──────────────────────────────────────────────────

  const handleRailNavigate = useCallback((app: ActiveApp) => {
    if (app === 'chat' && activeApp === 'chat') {
      // Already in chat — toggle rail expansion
      setIsRailExpanded((prev) => !prev);
      return;
    }
    setActiveApp(app);
    setPanelContent(null);
    setIsRailExpanded(false);
    setSelectedFlowHash(null);
  }, [activeApp]);

  const handleSettingsClick = useCallback(() => {
    // Settings panel placeholder — no-op for now
  }, []);

  const handlePanelClose = useCallback(() => {
    setPanelContent(null);
    setSelectedPanelFlowHash(null);
  }, []);

  const handlePanelPopOut = useCallback(() => {
    if (panelContent) {
      setActiveApp(panelContent);
      setPanelContent(null);
      setIsRailExpanded(false);
      setSelectedPanelFlowHash(null);
    }
  }, [panelContent]);

  const toggleFlowsPanel = useCallback(() => {
    if (activeApp !== 'chat') return;
    setPanelContent((prev) => prev === 'flows' ? null : 'flows');
    setSelectedPanelFlowHash(null);
  }, [activeApp]);

  const toggleMonitorPanel = useCallback(() => {
    if (activeApp !== 'chat') return;
    setPanelContent((prev) => prev === 'monitor' ? null : 'monitor');
  }, [activeApp]);

  // ── Command Palette actions ──────────────────────────────────────────────

  const paletteActions: PaletteAction[] = useMemo(() => [
    {
      id: 'nav-chat',
      label: 'Go to Chat',
      category: 'Navigation',
      shortcut: '⌘1',
      keywords: ['conversation', 'message'],
      icon: (
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z" />
        </svg>
      ),
      handler: () => { setActiveApp('chat'); setPanelContent(null); setIsRailExpanded(false); setSelectedFlowHash(null); },
    },
    {
      id: 'nav-flows',
      label: 'Go to Flows',
      category: 'Navigation',
      shortcut: '⌘2',
      keywords: ['topology', 'cpn', 'petri'],
      icon: (
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <circle cx="12" cy="5" r="2" /><circle cx="6" cy="19" r="2" /><circle cx="18" cy="19" r="2" />
          <path d="M12 7v4M12 11l-6 6M12 11l6 6" />
        </svg>
      ),
      handler: () => { setActiveApp('flows'); setPanelContent(null); setIsRailExpanded(false); setSelectedFlowHash(null); },
    },
    {
      id: 'nav-monitor',
      label: 'Go to Monitor',
      category: 'Navigation',
      shortcut: '⌘3',
      keywords: ['execution', 'trace', 'debug'],
      icon: (
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
        </svg>
      ),
      handler: () => { setActiveApp('monitor'); setPanelContent(null); setIsRailExpanded(false); },
    },
    {
      id: 'chat-new',
      label: 'New Chat',
      category: 'Chat',
      shortcut: '⌘N',
      keywords: ['create', 'start', 'conversation'],
      handler: () => { chatList.createChat(); setActiveApp('chat'); setPanelContent(null); },
    },
    {
      id: 'view-sidebar',
      label: 'Toggle Chat Sidebar',
      category: 'View',
      shortcut: '⌘B',
      keywords: ['sessions', 'history', 'list'],
      handler: () => { if (activeApp === 'chat') setIsRailExpanded((p) => !p); },
    },
    {
      id: 'view-flows-panel',
      label: 'Open Flows Panel',
      category: 'View',
      shortcut: '⌘⇧F',
      keywords: ['split', 'dual', 'side'],
      handler: toggleFlowsPanel,
    },
    {
      id: 'view-monitor-panel',
      label: 'Open Monitor Panel',
      category: 'View',
      shortcut: '⌘⇧M',
      keywords: ['split', 'dual', 'execution'],
      handler: toggleMonitorPanel,
    },
  ], [activeApp, chatList.createChat, toggleFlowsPanel, toggleMonitorPanel]);

  // ── Keyboard shortcuts ───────────────────────────────────────────────────

  const shortcuts = useMemo(() => [
    { key: 'k', meta: true, handler: () => setIsPaletteOpen((p) => !p), global: true },
    { key: '1', meta: true, handler: () => { setActiveApp('chat'); setPanelContent(null); setIsRailExpanded(false); setSelectedFlowHash(null); } },
    { key: '2', meta: true, handler: () => { setActiveApp('flows'); setPanelContent(null); setIsRailExpanded(false); setSelectedFlowHash(null); } },
    { key: '3', meta: true, handler: () => { setActiveApp('monitor'); setPanelContent(null); setIsRailExpanded(false); } },
    { key: 'n', meta: true, handler: () => { chatList.createChat(); setActiveApp('chat'); setPanelContent(null); } },
    { key: 'b', meta: true, handler: () => { if (activeApp === 'chat') setIsRailExpanded((p) => !p); } },
    { key: 'f', meta: true, shift: true, handler: toggleFlowsPanel },
    { key: 'm', meta: true, shift: true, handler: toggleMonitorPanel },
    { key: 'Escape', handler: () => { if (isPaletteOpen) { setIsPaletteOpen(false); } else if (panelContent) { setPanelContent(null); } } },
  ], [activeApp, isPaletteOpen, panelContent, chatList.createChat, toggleFlowsPanel, toggleMonitorPanel]);

  useKeyboardShortcuts(shortcuts);

  // ── Sidebar content for rail expansion ────────────────────────────────────

  const sidebarContent = activeApp === 'chat' ? (
    <ChatSidebar
      embedded
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
  ) : undefined;

  // ── Panel content ─────────────────────────────────────────────────────────

  const panelTitle = panelContent === 'flows' ? 'Flows' : panelContent === 'monitor' ? 'Monitor' : '';

  const renderPanelContent = () => {
    if (panelContent === 'flows') {
      if (selectedPanelFlowHash) {
        return <FlowDetail hash={selectedPanelFlowHash} onBack={() => setSelectedPanelFlowHash(null)} />;
      }
      return <FlowBrowser onSelectFlow={(hash) => setSelectedPanelFlowHash(hash)} />;
    }
    if (panelContent === 'monitor') {
      return (
        <ExecutionMonitor
          state={monitor.state}
          selectedRun={monitor.selectedRun}
          onSelectRun={monitor.selectRun}
          onSelectTransition={monitor.selectTransition}
          onNavigateCPN={monitor.navigateCPN}
          onLoadTrace={handleLoadTrace}
          sessionId={sessionId}
        />
      );
    }
    return null;
  };

  return (
    <div className="scan-lines flex flex-col h-dvh relative" style={{ backgroundColor: 'var(--bg-deep)' }}>
      <StatusBar sessionState={sessionState} isConnected={isConnected} activeApp={activeApp} />

      <div className="flex flex-1 min-h-0">
        {/* Navigation Rail — hidden on mobile */}
        {!isMobile && (
          <NavigationRail
            activeApp={activeApp}
            isExpanded={isRailExpanded}
            onNavigate={handleRailNavigate}
            onSettingsClick={handleSettingsClick}
            sidebarContent={sidebarContent}
          />
        )}

        {/* Main content area */}
        <div className="flex-1 flex min-w-0 relative overflow-hidden">
          {/* Primary content */}
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
                <div className={isMobile ? 'pb-16' : ''}>
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

          {/* Adaptive Right Panel — only in chat mode, desktop only */}
          {!isMobile && activeApp === 'chat' && (
            <AdaptivePanel
              isOpen={panelContent !== null}
              title={panelTitle}
              onClose={handlePanelClose}
              onPopOut={handlePanelPopOut}
            >
              {renderPanelContent()}
            </AdaptivePanel>
          )}
        </div>
      </div>

      {/* Mobile Tab Bar */}
      {isMobile && (
        <MobileTabBar activeApp={activeApp} onNavigate={handleRailNavigate} />
      )}

      {/* Command Palette */}
      <CommandPalette
        isOpen={isPaletteOpen}
        onClose={() => setIsPaletteOpen(false)}
        actions={paletteActions}
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
