import { useState, useCallback } from 'react';
import { useChatList } from './useChatList';

type ActiveApp = 'chat' | 'flows' | 'monitor' | 'personality' | 'tools' | 'admin';

/**
 * Manages navigation state and coordinates chat session lifecycle.
 * Encapsulates activeApp, rail expansion, palette, and forking state.
 */
export function useSessionManager(userId: string) {
  const [activeApp, setActiveApp] = useState<ActiveApp>('chat');
  const [isRailExpanded, setIsRailExpanded] = useState(false);
  const [isPaletteOpen, setIsPaletteOpen] = useState(false);
  const [forkingSessionId, setForkingSessionId] = useState<string | null>(null);

  const chatList = useChatList(userId);

  // ── Navigation handlers ──────────────────────────────────────────────────

  const navigateTo = useCallback((app: ActiveApp) => {
    setActiveApp(app);
    setIsRailExpanded(false);
  }, []);

  const handleRailNavigate = useCallback((app: ActiveApp) => {
    if (app === 'chat' && activeApp === 'chat') {
      setIsRailExpanded((prev) => !prev);
      return;
    }
    setActiveApp(app);
    setIsRailExpanded(false);
  }, [activeApp]);

  const handleSettingsClick = useCallback(() => {
    setActiveApp('personality');
    setIsRailExpanded(false);
  }, []);

  const handleToolsClick = useCallback(() => {
    setActiveApp('tools');
    setIsRailExpanded(false);
  }, []);

  const togglePalette = useCallback(() => {
    setIsPaletteOpen((p) => !p);
  }, []);

  const closePalette = useCallback(() => {
    setIsPaletteOpen(false);
  }, []);

  // ── Send message (handles completed sessions) ───────────────────────────

  const createSendHandler = useCallback(
    (sessionState: string, sendMessage: (content: string) => Promise<void>) => {
      return async (content: string) => {
        if (sessionState === 'completed') {
          const newId = await chatList.createChat();
          if (newId) {
            setTimeout(() => sendMessage(content), 200);
          }
        } else {
          sendMessage(content);
        }
      };
    },
    [chatList.createChat],
  );

  // ── Fork handlers ───────────────────────────────────────────────────────

  const handleForkRequest = useCallback((chatId: string) => {
    setForkingSessionId(chatId);
  }, []);

  const handleForkConfirm = useCallback((messageIndex: number) => {
    if (forkingSessionId) {
      chatList.forkChat(forkingSessionId, messageIndex);
    }
    setForkingSessionId(null);
  }, [forkingSessionId, chatList.forkChat]);

  const closeForkDialog = useCallback(() => {
    setForkingSessionId(null);
  }, []);

  return {
    activeApp,
    setActiveApp,
    isRailExpanded,
    setIsRailExpanded,
    isPaletteOpen,
    forkingSessionId,
    chatList,
    navigateTo,
    handleRailNavigate,
    handleSettingsClick,
    handleToolsClick,
    togglePalette,
    closePalette,
    createSendHandler,
    handleForkRequest,
    handleForkConfirm,
    closeForkDialog,
  } as const;
}
