import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { loadNamespace } from '../../i18n/loadNamespace';
import { useChat } from '../../hooks/useChat';
import { useSessionManager } from '../../hooks/useSessionManager';
import { useMonitorManager } from '../../hooks/useMonitorManager';
import { usePanelManager } from '../../hooks/usePanelManager';
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
import { SessionResumedNotice } from '../chat/SessionResumedNotice';
import { useModelAdminFlow } from '../chat/modelAdmin/useModelAdminFlow';
import type { A2UIAction } from '../chat/a2ui/types';
import { FlowBrowser } from '../cpn-visualizer/FlowBrowser';
import { FlowDetail } from '../cpn-visualizer/FlowDetail';
import { ExecutionMonitor } from '../execution-monitor/ExecutionMonitor';
import { PersonalityPanel } from '../personality/PersonalityPanel';
import { ToolBrowser } from '../tools/ToolBrowser';
import { AdminSecretsPanel } from '../admin/AdminSecretsPanel';
import { SettingsPage } from '../settings/SettingsPage';
import { SkillsPanel } from '../skills/SkillsPanel';
import { useAuth } from '../../contexts/AuthContext';

interface DesktopLayoutProps {
  userId: string;
}

export function DesktopLayout({ userId }: DesktopLayoutProps) {
  const { t } = useTranslation(['desktop', 'common']);
  const isMobile = useIsMobile();
  const { user, isAdmin, logout } = useAuth();

  // Load namespaces for desktop layout
  useEffect(() => {
    loadNamespace('desktop');
    loadNamespace('chat');
  }, []);

  // ── Session + navigation state ──────────────────────────────────────────
  const session = useSessionManager(userId);
  const {
    activeApp, setActiveApp, isRailExpanded, setIsRailExpanded,
    isPaletteOpen, forkingSessionId, chatList,
    togglePalette, closePalette,
    handleForkRequest, handleForkConfirm, closeForkDialog,
  } = session;

  // ── Panel state ─────────────────────────────────────────────────────────
  const panel = usePanelManager();

  // ── Skills sub-view ─────────────────────────────────────────────────────
  // The Skills panel (GAP-8) is reached at `/settings/skills`. Because the
  // app doesn't use react-router (every view is a key in `activeApp`), we
  // model the route as a sub-view of the `settings` app driven by the URL
  // hash + a command-palette action. Opening Skills forces activeApp into
  // `settings` so the rail stays in the settings context.
  const [skillsOpen, setSkillsOpen] = useState<boolean>(() => {
    if (typeof window === 'undefined') return false;
    return window.location.hash === '#/settings/skills';
  });

  useEffect(() => {
    const readHash = () => {
      const next = window.location.hash === '#/settings/skills';
      setSkillsOpen((prev) => (prev === next ? prev : next));
    };
    window.addEventListener('hashchange', readHash);
    readHash();
    return () => window.removeEventListener('hashchange', readHash);
  }, []);

  const openSkills = useCallback(() => {
    setSkillsOpen(true);
    setActiveApp('settings');
    panel.closePanel();
    setIsRailExpanded(false);
    if (typeof window !== 'undefined' && window.location.hash !== '#/settings/skills') {
      window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}#/settings/skills`);
    }
  }, [setActiveApp, panel.closePanel, setIsRailExpanded]);

  const closeSkills = useCallback(() => {
    setSkillsOpen(false);
    if (typeof window !== 'undefined' && window.location.hash === '#/settings/skills') {
      window.history.replaceState(null, '', `${window.location.pathname}${window.location.search}`);
    }
  }, []);

  // Navigating away from settings (to chat, flows, etc.) MUST clear the
  // skills sub-view so the hash and the visible content stay in sync.
  useEffect(() => {
    if (activeApp !== 'settings' && skillsOpen) {
      closeSkills();
    }
  }, [activeApp, skillsOpen, closeSkills]);

  // ── Monitor + SSE coordination ──────────────────────────────────────────
  const onSessionCompleted = useCallback(() => {
    chatList.refresh();
  }, [chatList.refresh]);

  const monitorMgr = useMonitorManager(onSessionCompleted);

  // Bridge SSE ghost-session detection (REQ-102/104) into the chat-list
  // recovery path so a dead session id is replaced exactly once without
  // requiring a manual refresh or logout.
  const chatOptions = useMemo(
    () => ({
      ...monitorMgr.sseCallbacks,
      onSessionNotFound: chatList.recoverFromGhost,
    }),
    [monitorMgr.sseCallbacks, chatList.recoverFromGhost],
  );

  const {
    messages,
    sessionState,
    sessionId,
    isConnected,
    sendMessage,
    resolveHITL,
    error,
    injectLocalMessage,
    updateMessageContent,
    sendUserAction,
  } = useChat(chatList.activeSessionId, chatOptions);

  const modelAdmin = useModelAdminFlow({ injectLocalMessage, updateMessageContent });

  /**
   * A2UI fallthrough — `model:*` stays client-side, everything else flows
   * back to the CPN via sendUserAction (REQ-GAP-REG-002 / REQ-FE-006).
   */
  const handleA2UIAction = useCallback(
    (action: A2UIAction, messageId: string): boolean => {
      if (modelAdmin.tryHandleA2UIAction(action, messageId)) return true;
      void sendUserAction(action);
      return true;
    },
    [modelAdmin, sendUserAction],
  );

  // ── Coordination effects (bridge chat <-> monitor) ──────────────────────

  // Track message count to detect new user messages -> new execution
  const prevMessageCountRef = useRef(0);
  useEffect(() => {
    const currentCount = messages.length;
    const prev = prevMessageCountRef.current;
    prevMessageCountRef.current = currentCount;

    if (currentCount > prev && currentCount > 0) {
      const lastMsg = messages[currentCount - 1];
      if (lastMsg.role === 'user') {
        monitorMgr.monitor.startNewExecution(lastMsg.content);
      }
    }
  }, [messages.length]); // eslint-disable-line react-hooks/exhaustive-deps

  // Reset monitor and reload execution data when the active session changes
  const prevMonitorSessionRef = useRef<string | null>(null);
  useEffect(() => {
    if (!sessionId || sessionId === prevMonitorSessionRef.current) return;
    prevMonitorSessionRef.current = sessionId;

    monitorMgr.monitor.reset();
    monitorMgr.loadMonitorDataForSession(sessionId);
  }, [sessionId]); // eslint-disable-line react-hooks/exhaustive-deps

  // When session goes from running -> idle, reload monitor data
  const prevSessionStateRef = useRef<string>(sessionState);
  useEffect(() => {
    const wasRunning = prevSessionStateRef.current === 'running';
    prevSessionStateRef.current = sessionState;

    if (wasRunning && sessionState === 'idle' && sessionId) {
      monitorMgr.loadMonitorDataForSession(sessionId);
      chatList.refresh();
    }
  }, [sessionState, sessionId, monitorMgr.loadMonitorDataForSession]);

  // ── Send message handler ────────────────────────────────────────────────
  const handleSendMessage = useCallback(async (content: string) => {
    if (modelAdmin.tryHandleSlashCommand(content)) return;
    if (sessionState === 'completed') {
      const newId = await chatList.createChat();
      if (newId) {
        setTimeout(() => sendMessage(content), 200);
      }
    } else {
      sendMessage(content);
    }
  }, [modelAdmin, sessionState, chatList.createChat, sendMessage]);

  const handleOpenMonitor = useCallback(() => {
    setActiveApp('monitor');
    panel.closePanel();
    setIsRailExpanded(false);
  }, [setActiveApp, panel.closePanel, setIsRailExpanded]);

  // ── Navigation helpers that also clear panel ────────────────────────────
  const navigateAndClearPanel = useCallback((app: typeof activeApp) => {
    setActiveApp(app);
    panel.closePanel();
    setIsRailExpanded(false);
    panel.setFlowHash(null);
  }, [setActiveApp, panel.closePanel, setIsRailExpanded, panel.setFlowHash]);

  const handlePanelPopOut = useCallback(() => {
    if (panel.panelContent) {
      setActiveApp(panel.panelContent);
      panel.closePanel();
      setIsRailExpanded(false);
    }
  }, [panel.panelContent, setActiveApp, panel.closePanel, setIsRailExpanded]);

  const toggleFlowsPanel = useCallback(() => {
    if (activeApp !== 'chat') return;
    panel.togglePanel('flows');
  }, [activeApp, panel.togglePanel]);

  const toggleMonitorPanel = useCallback(() => {
    if (activeApp !== 'chat') return;
    panel.togglePanel('monitor');
  }, [activeApp, panel.togglePanel]);

  // Override rail navigate to also clear panel
  const handleRailNav = useCallback((app: typeof activeApp) => {
    if (app === 'chat' && activeApp === 'chat') {
      setIsRailExpanded((prev) => !prev);
      return;
    }
    navigateAndClearPanel(app);
  }, [activeApp, setIsRailExpanded, navigateAndClearPanel]);

  const handleSettingsNav = useCallback(() => {
    navigateAndClearPanel('personality');
  }, [navigateAndClearPanel]);

  const handleToolsNav = useCallback(() => {
    navigateAndClearPanel('tools');
  }, [navigateAndClearPanel]);

  const handleAdminNav = useCallback(() => {
    navigateAndClearPanel('admin');
  }, [navigateAndClearPanel]);

  const handleUserSettingsNav = useCallback(() => {
    navigateAndClearPanel('settings');
  }, [navigateAndClearPanel]);

  // ── Command Palette actions ─────────────────────────────────────────────

  const paletteActions: PaletteAction[] = useMemo(() => [
    {
      id: 'nav-chat',
      label: t('desktop:commandPalette.actions.goToChat'),
      category: t('desktop:commandPalette.categories.navigation'),
      shortcut: '\u2318 1',
      keywords: ['conversation', 'message'],
      icon: (
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <path d="M21 15a2 2 0 01-2 2H7l-4 4V5a2 2 0 012-2h14a2 2 0 012 2z" />
        </svg>
      ),
      handler: () => navigateAndClearPanel('chat'),
    },
    {
      id: 'nav-flows',
      label: t('desktop:commandPalette.actions.goToFlows'),
      category: t('desktop:commandPalette.categories.navigation'),
      shortcut: '\u2318 2',
      keywords: ['topology', 'cpn', 'petri'],
      icon: (
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <circle cx="12" cy="5" r="2" /><circle cx="6" cy="19" r="2" /><circle cx="18" cy="19" r="2" />
          <path d="M12 7v4M12 11l-6 6M12 11l6 6" />
        </svg>
      ),
      handler: () => navigateAndClearPanel('flows'),
    },
    {
      id: 'nav-monitor',
      label: t('desktop:commandPalette.actions.goToMonitor'),
      category: t('desktop:commandPalette.categories.navigation'),
      shortcut: '\u2318 3',
      keywords: ['execution', 'trace', 'debug'],
      icon: (
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <path d="M22 12h-4l-3 9L9 3l-3 9H2" />
        </svg>
      ),
      handler: () => { setActiveApp('monitor'); panel.closePanel(); setIsRailExpanded(false); },
    },
    {
      id: 'nav-personality',
      label: t('desktop:commandPalette.actions.agentIdentity'),
      category: t('desktop:commandPalette.categories.navigation'),
      shortcut: '\u2318 4',
      keywords: ['personality', 'principles', 'identity', 'settings'],
      icon: (
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <circle cx="12" cy="12" r="3" />
          <path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 010 2.83 2 2 0 01-2.83 0l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-2 2 2 2 0 01-2-2v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83 0 2 2 0 010-2.83l.06-.06A1.65 1.65 0 004.68 15a1.65 1.65 0 00-1.51-1H3a2 2 0 01-2-2 2 2 0 012-2h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 010-2.83 2 2 0 012.83 0l.06.06A1.65 1.65 0 009 4.68a1.65 1.65 0 001-1.51V3a2 2 0 012-2 2 2 0 012 2v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 0 2 2 0 010 2.83l-.06.06A1.65 1.65 0 0019.4 9a1.65 1.65 0 001.51 1H21a2 2 0 012 2 2 2 0 01-2 2h-.09a1.65 1.65 0 00-1.51 1z" />
        </svg>
      ),
      handler: () => navigateAndClearPanel('personality'),
    },
    {
      id: 'nav-tools',
      label: t('desktop:commandPalette.actions.toolBrowser'),
      category: t('desktop:commandPalette.categories.navigation'),
      shortcut: '\u2318 5',
      keywords: ['tools', 'registry', 'wrench', 'browser'],
      icon: (
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z" />
        </svg>
      ),
      handler: () => navigateAndClearPanel('tools'),
    },
    {
      id: 'chat-new',
      label: t('desktop:commandPalette.actions.newChat'),
      category: t('desktop:commandPalette.categories.chat'),
      shortcut: '\u2318 N',
      keywords: ['create', 'start', 'conversation'],
      handler: () => { chatList.createChat(); setActiveApp('chat'); panel.closePanel(); },
    },
    {
      id: 'view-sidebar',
      label: t('desktop:commandPalette.actions.toggleChatSidebar'),
      category: t('desktop:commandPalette.categories.view'),
      shortcut: '\u2318 B',
      keywords: ['sessions', 'history', 'list'],
      handler: () => { if (activeApp === 'chat') setIsRailExpanded((p) => !p); },
    },
    {
      id: 'view-flows-panel',
      label: t('desktop:commandPalette.actions.openFlowsPanel'),
      category: t('desktop:commandPalette.categories.view'),
      shortcut: '\u2318\u21E7F',
      keywords: ['split', 'dual', 'side'],
      handler: toggleFlowsPanel,
    },
    {
      id: 'view-monitor-panel',
      label: t('desktop:commandPalette.actions.openMonitorPanel'),
      category: t('desktop:commandPalette.categories.view'),
      shortcut: '\u2318\u21E7M',
      keywords: ['split', 'dual', 'execution'],
      handler: toggleMonitorPanel,
    },
    {
      id: 'account-settings',
      label: t('desktop:commandPalette.actions.openSettings'),
      category: t('desktop:commandPalette.categories.account'),
      shortcut: '\u2318,',
      keywords: ['preferences', 'ajustes', 'configuración', 'settings'],
      handler: () => { closeSkills(); navigateAndClearPanel('settings'); },
    },
    // Skills (GAP-8) — reached at #/settings/skills. Registered here so
    // Cmd-K → "Habilidades" opens the inspection panel without a rail
    // change. See spec-architecture-skill-manifest.md for the manifest
    // shape.
    {
      id: 'account-skills',
      label: 'Habilidades · Skills',
      category: t('desktop:commandPalette.categories.account'),
      keywords: ['skills', 'habilidades', 'flows', 'tools', 'herramientas', 'capacidades', 'manifest'],
      icon: (
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <path d="M4 4h6v6H4zM14 4h6v6h-6zM4 14h6v6H4zM14 14h6v6h-6z" />
        </svg>
      ),
      handler: openSkills,
    },
    {
      id: 'account-language',
      label: t('desktop:commandPalette.actions.changeLanguage'),
      category: t('desktop:commandPalette.categories.account'),
      keywords: ['idioma', 'language', 'locale'],
      handler: () => window.dispatchEvent(new CustomEvent('liwaisi:openLanguageSwitcher')),
    },
    {
      id: 'account-workspace',
      label: t('desktop:commandPalette.actions.switchWorkspace'),
      category: t('desktop:commandPalette.categories.account'),
      keywords: ['workspace', 'org', 'equipo'],
      handler: () => { /* workspace switching is a stub until multi-workspace lands */ },
    },
    {
      id: 'account-billing',
      label: t('desktop:commandPalette.actions.billing'),
      category: t('desktop:commandPalette.categories.account'),
      keywords: ['billing', 'facturación', 'balance', 'créditos'],
      handler: () => navigateAndClearPanel('settings'),
    },
    {
      id: 'account-logout',
      label: t('desktop:commandPalette.actions.logout'),
      category: t('desktop:commandPalette.categories.account'),
      shortcut: '\u2318\u21E7Q',
      keywords: ['logout', 'salir', 'cerrar sesión', 'sign out'],
      handler: () => logout(),
    },
  ], [t, activeApp, chatList.createChat, navigateAndClearPanel, setActiveApp, panel.closePanel, setIsRailExpanded, toggleFlowsPanel, toggleMonitorPanel, logout, openSkills, closeSkills]);

  // ── Keyboard shortcuts ──────────────────────────────────────────────────

  const shortcuts = useMemo(() => [
    { key: 'k', meta: true, handler: togglePalette, global: true },
    { key: '1', meta: true, handler: () => navigateAndClearPanel('chat') },
    { key: '2', meta: true, handler: () => navigateAndClearPanel('flows') },
    { key: '3', meta: true, handler: () => { setActiveApp('monitor'); panel.closePanel(); setIsRailExpanded(false); } },
    { key: '4', meta: true, handler: () => navigateAndClearPanel('personality') },
    { key: '5', meta: true, handler: () => navigateAndClearPanel('tools') },
    { key: 'n', meta: true, handler: () => { chatList.createChat(); setActiveApp('chat'); panel.closePanel(); } },
    { key: 'b', meta: true, handler: () => { if (activeApp === 'chat') setIsRailExpanded((p) => !p); } },
    { key: 'f', meta: true, shift: true, handler: toggleFlowsPanel },
    { key: 'm', meta: true, shift: true, handler: toggleMonitorPanel },
    { key: ',', meta: true, handler: () => navigateAndClearPanel('settings'), global: true },
    { key: 'q', meta: true, shift: true, handler: () => logout(), global: true },
    { key: 'Escape', handler: () => { if (isPaletteOpen) { closePalette(); } else if (panel.panelContent) { panel.closePanel(); } } },
  ], [activeApp, isPaletteOpen, panel.panelContent, chatList.createChat, navigateAndClearPanel, setActiveApp, panel.closePanel, setIsRailExpanded, togglePalette, closePalette, toggleFlowsPanel, toggleMonitorPanel, logout]);

  useKeyboardShortcuts(shortcuts);

  // ── Sidebar content for rail expansion ──────────────────────────────────

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

  // ── Panel content ───────────────────────────────────────────────────────

  const panelTitle = panel.panelContent === 'flows' ? t('desktop:navigationRail.flows') : panel.panelContent === 'monitor' ? t('desktop:navigationRail.monitor') : '';

  const renderPanelContent = () => {
    if (panel.panelContent === 'flows') {
      if (panel.selectedPanelFlowHash) {
        return <FlowDetail hash={panel.selectedPanelFlowHash} onBack={() => panel.setPanelFlowHash(null)} />;
      }
      return <FlowBrowser onSelectFlow={(hash) => panel.setPanelFlowHash(hash)} />;
    }
    if (panel.panelContent === 'monitor') {
      return (
        <ExecutionMonitor
          state={monitorMgr.monitor.state}
          selectedRun={monitorMgr.monitor.selectedRun}
          onSelectRun={monitorMgr.monitor.selectRun}
          onSelectTransition={monitorMgr.monitor.selectTransition}
          onNavigateCPN={monitorMgr.monitor.navigateCPN}
          onLoadTrace={monitorMgr.handleLoadTrace}
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
        {/* Navigation Rail -- hidden on mobile */}
        {!isMobile && (
          <NavigationRail
            activeApp={activeApp}
            isExpanded={isRailExpanded}
            isAdmin={isAdmin}
            onNavigate={handleRailNav}
            onSettingsClick={handleSettingsNav}
            onToolsClick={handleToolsNav}
            onAdminClick={handleAdminNav}
            sidebarContent={sidebarContent}
            user={user}
            onOpenUserSettings={handleUserSettingsNav}
            onLogout={logout}
          />
        )}

        {/* Main content area */}
        <div className="flex-1 flex min-w-0 relative overflow-hidden">
          {/* Primary content */}
          <div className="flex-1 flex flex-col min-w-0">
            {activeApp === 'chat' && (
              <>
                <SessionResumedNotice resumedAt={chatList.sessionResumedAt} />
                <MessageList
                  messages={messages}
                  sessionState={sessionState}
                  onSuggestionClick={handleSendMessage}
                  onHITLAction={resolveHITL}
                  onA2UIAction={handleA2UIAction}
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

            {activeApp === 'flows' && !panel.selectedFlowHash && (
              <FlowBrowser onSelectFlow={(hash) => panel.setFlowHash(hash)} />
            )}

            {activeApp === 'flows' && panel.selectedFlowHash && (
              <FlowDetail hash={panel.selectedFlowHash} onBack={() => panel.setFlowHash(null)} />
            )}

            {activeApp === 'monitor' && (
              <ExecutionMonitor
                state={monitorMgr.monitor.state}
                selectedRun={monitorMgr.monitor.selectedRun}
                onSelectRun={monitorMgr.monitor.selectRun}
                onSelectTransition={monitorMgr.monitor.selectTransition}
                onNavigateCPN={monitorMgr.monitor.navigateCPN}
                onLoadTrace={monitorMgr.handleLoadTrace}
                sessionId={sessionId}
              />
            )}

            {activeApp === 'personality' && <PersonalityPanel />}

            {activeApp === 'tools' && <ToolBrowser />}

            {activeApp === 'admin' && <AdminSecretsPanel />}

            {activeApp === 'settings' && !skillsOpen && <SettingsPage />}

            {activeApp === 'settings' && skillsOpen && <SkillsPanel />}
          </div>

          {/* Adaptive Right Panel -- only in chat mode, desktop only */}
          {!isMobile && activeApp === 'chat' && (
            <AdaptivePanel
              isOpen={panel.panelContent !== null}
              title={panelTitle}
              onClose={panel.closePanel}
              onPopOut={handlePanelPopOut}
            >
              {renderPanelContent()}
            </AdaptivePanel>
          )}
        </div>
      </div>

      {/* Mobile Tab Bar */}
      {isMobile && (
        <MobileTabBar activeApp={activeApp} onNavigate={handleRailNav} />
      )}

      {/* Command Palette */}
      <CommandPalette
        isOpen={isPaletteOpen}
        onClose={closePalette}
        actions={paletteActions}
      />

      {/* Fork Dialog */}
      {forkingSessionId && (
        <ForkDialog
          messages={messages}
          onFork={handleForkConfirm}
          onClose={closeForkDialog}
        />
      )}
    </div>
  );
}
