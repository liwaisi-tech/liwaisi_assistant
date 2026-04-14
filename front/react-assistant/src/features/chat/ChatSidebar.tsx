import { useState, useCallback, useRef, useEffect, memo } from 'react';
import type { SessionListItem } from '../../types/api';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { loadNamespace } from '../../i18n/loadNamespace';

function timeAgo(dateStr: string, t: TFunction): string {
  const now = Date.now();
  const then = new Date(dateStr).getTime();
  const diffMs = now - then;
  const diffSec = Math.floor(diffMs / 1000);

  if (diffSec < 60) return t('common:timeAgo.justNow');
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return t('common:timeAgo.minutesAgo', { count: diffMin });
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return t('common:timeAgo.hoursAgo', { count: diffHr });
  const diffDay = Math.floor(diffHr / 24);
  if (diffDay === 1) return t('common:timeAgo.yesterday');
  if (diffDay < 7) return t('common:timeAgo.daysAgo', { count: diffDay });
  const diffWeek = Math.floor(diffDay / 7);
  if (diffWeek < 5) return t('common:timeAgo.weeksAgo', { count: diffWeek });
  const diffMonth = Math.floor(diffDay / 30);
  if (diffMonth < 12) return t('common:timeAgo.monthsAgo', { count: diffMonth });
  return t('common:timeAgo.yearsAgo', { count: Math.floor(diffMonth / 12) });
}

function formatCost(usd: number, t: TFunction): string {
  if (usd === 0) return t('common:costFormat.zero');
  if (usd < 0.01) return t('common:costFormat.lessThanCent');
  return `$${usd.toFixed(2)}`;
}

// ── ChatListItem ────────────────────────────────────────────────────────────

interface ChatListItemProps {
  chat: SessionListItem;
  isActive: boolean;
  onSelect: (id: string) => void;
  onRename: (id: string, title: string) => void;
  onDelete: (id: string) => void;
  onFork: (id: string) => void;
}

const ChatListItem = memo(function ChatListItem({ chat, isActive, onSelect, onRename, onDelete, onFork }: ChatListItemProps) {
  const { t } = useTranslation(['chat', 'common']);
  const [showMenu, setShowMenu] = useState(false);
  const [isRenaming, setIsRenaming] = useState(false);
  const [renameValue, setRenameValue] = useState(chat.title);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (isRenaming && inputRef.current) {
      inputRef.current.focus();
      inputRef.current.select();
    }
  }, [isRenaming]);

  // Close menu on outside click
  useEffect(() => {
    if (!showMenu) return;
    const handler = (e: MouseEvent) => {
      if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
        setShowMenu(false);
        setConfirmDelete(false);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [showMenu]);

  const handleRenameSubmit = useCallback(() => {
    const trimmed = renameValue.trim();
    if (trimmed && trimmed !== chat.title) {
      onRename(chat.id, trimmed);
    }
    setIsRenaming(false);
  }, [renameValue, chat.id, chat.title, onRename]);

  const handleActivate = useCallback(() => {
    if (!isRenaming) onSelect(chat.id);
  }, [isRenaming, onSelect, chat.id]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLDivElement>) => {
    if (isRenaming) return;
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onSelect(chat.id);
    }
  }, [isRenaming, onSelect, chat.id]);

  return (
    <div
      role="button"
      tabIndex={isRenaming ? -1 : 0}
      aria-current={isActive ? 'true' : undefined}
      aria-label={chat.title || t('chat:sidebar.defaultTitle')}
      className="group relative flex flex-col gap-0.5 px-3 py-2.5 transition-colors duration-150 rounded-lg mx-1.5"
      style={{
        backgroundColor: isActive ? 'rgba(14, 165, 233, 0.08)' : 'transparent',
        borderLeft: isActive ? '2px solid var(--accent)' : '2px solid transparent',
      }}
      onClick={handleActivate}
      onKeyDown={handleKeyDown}
    >
      {/* Title row */}
      <div className="flex items-center gap-1.5 min-w-0">
        {chat.forked_from_session_id && (
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="var(--accent)" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="flex-none opacity-60">
            <circle cx="12" cy="18" r="3" />
            <circle cx="6" cy="6" r="3" />
            <circle cx="18" cy="6" r="3" />
            <path d="M18 9v2c0 .6-.4 1-1 1H7c-.6 0-1-.4-1-1V9" />
            <path d="M12 12v3" />
          </svg>
        )}
        {isRenaming ? (
          <input
            ref={inputRef}
            value={renameValue}
            onChange={(e) => setRenameValue(e.target.value)}
            onBlur={handleRenameSubmit}
            onKeyDown={(e) => {
              if (e.key === 'Enter') handleRenameSubmit();
              if (e.key === 'Escape') setIsRenaming(false);
            }}
            onClick={(e) => e.stopPropagation()}
            className="flex-1 min-w-0 bg-transparent text-xs font-semibold outline-none border-b"
            style={{
              color: 'var(--text-primary)',
              borderColor: 'var(--accent)',
              fontFamily: "var(--font-mono, 'JetBrains Mono', monospace)",
            }}
          />
        ) : (
          <span
            className="flex-1 min-w-0 truncate text-xs font-semibold"
            style={{
              color: isActive ? 'var(--text-primary)' : 'var(--text-secondary)',
              fontFamily: "var(--font-mono, 'JetBrains Mono', monospace)",
            }}
          >
            {chat.title || t('chat:sidebar.defaultTitle')}
          </span>
        )}

        {/* Cost badge */}
        {chat.total_cost_usd > 0 && (
          <span
            className="flex-none text-[10px] px-1.5 py-0.5 rounded"
            style={{
              backgroundColor: 'rgba(14, 165, 233, 0.1)',
              color: 'var(--accent)',
              fontFamily: "var(--font-mono, 'JetBrains Mono', monospace)",
            }}
          >
            {formatCost(chat.total_cost_usd, t)}
          </span>
        )}

        {/* Actions button */}
        <button
          onClick={(e) => {
            e.stopPropagation();
            setShowMenu(!showMenu);
            setConfirmDelete(false);
          }}
          className="flex-none opacity-0 group-hover:opacity-100 transition-opacity p-0.5 rounded hover:bg-white/5"
          aria-label={t('chat:sidebar.chatActions')}
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="var(--text-muted)">
            <circle cx="12" cy="5" r="2" />
            <circle cx="12" cy="12" r="2" />
            <circle cx="12" cy="19" r="2" />
          </svg>
        </button>
      </div>

      {/* Preview row */}
      <div className="flex items-center gap-2 min-w-0">
        <span
          className="flex-1 min-w-0 truncate text-[11px]"
          style={{ color: 'var(--text-muted)' }}
        >
          {chat.last_message_preview || t('chat:sidebar.noMessagesYet')}
        </span>
        <span
          className="flex-none text-[10px]"
          style={{ color: 'var(--text-muted)' }}
        >
          {timeAgo(chat.last_activity_at || chat.created_at, t)}
        </span>
      </div>

      {/* Context menu */}
      {showMenu && (
        <div
          ref={menuRef}
          className="absolute right-2 top-full z-20 mt-1 py-1 rounded-lg border shadow-xl"
          style={{
            backgroundColor: 'var(--bg-surface)',
            borderColor: 'var(--border-dim)',
            minWidth: '120px',
          }}
          onClick={(e) => e.stopPropagation()}
        >
          <button
            onClick={() => {
              setIsRenaming(true);
              setRenameValue(chat.title || t('chat:sidebar.defaultTitle'));
              setShowMenu(false);
            }}
            className="w-full text-left px-3 py-1.5 text-xs transition-colors hover:bg-white/5"
            style={{ color: 'var(--text-secondary)' }}
          >
            {t('chat:sidebar.rename')}
          </button>
          <button
            onClick={() => {
              onFork(chat.id);
              setShowMenu(false);
            }}
            className="w-full text-left px-3 py-1.5 text-xs transition-colors hover:bg-white/5"
            style={{ color: 'var(--text-secondary)' }}
          >
            {t('chat:sidebar.fork')}
          </button>
          {confirmDelete ? (
            <button
              onClick={() => {
                onDelete(chat.id);
                setShowMenu(false);
                setConfirmDelete(false);
              }}
              className="w-full text-left px-3 py-1.5 text-xs font-semibold transition-colors hover:bg-red-500/10"
              style={{ color: '#ef4444' }}
            >
              {t('chat:sidebar.confirmDelete')}
            </button>
          ) : (
            <button
              onClick={() => setConfirmDelete(true)}
              className="w-full text-left px-3 py-1.5 text-xs transition-colors hover:bg-red-500/10"
              style={{ color: '#ef4444' }}
            >
              {t('chat:sidebar.delete')}
            </button>
          )}
        </div>
      )}
    </div>
  );
});

// ── ChatSidebar ─────────────────────────────────────────────────────────────

interface ChatSidebarProps {
  chats: SessionListItem[];
  activeSessionId: string | null;
  isLoading: boolean;
  hasMore: boolean;
  onSelect: (id: string) => void;
  onCreate: () => void;
  onRename: (id: string, title: string) => void;
  onDelete: (id: string) => void;
  onFork: (id: string) => void;
  onLoadMore: () => void;
  /** When true, renders only the header + list without the outer shell (for embedding in NavigationRail) */
  embedded?: boolean;
}

/** Inner content: header + scrollable chat list. Used standalone or embedded. */
function ChatSidebarContent({
  chats,
  activeSessionId,
  isLoading,
  hasMore,
  onSelect,
  onCreate,
  onRename,
  onDelete,
  onFork,
  onLoadMore,
}: Omit<ChatSidebarProps, 'embedded'>) {
  const { t } = useTranslation(['chat', 'common']);
  const sentinelRef = useRef<HTMLDivElement>(null);

  // Use IntersectionObserver instead of scroll handler for infinite loading
  useEffect(() => {
    const sentinel = sentinelRef.current;
    if (!sentinel) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && hasMore && !isLoading) {
          onLoadMore();
        }
      },
      { threshold: 0.1 },
    );

    observer.observe(sentinel);
    return () => observer.disconnect();
  }, [hasMore, isLoading, onLoadMore]);

  return (
    <>
      {/* Header */}
      <div
        className="flex items-center justify-between px-4 py-3 border-b flex-none"
        style={{ borderColor: 'var(--border-dim)' }}
      >
        <h2
          className="text-sm font-semibold tracking-wide"
          style={{
            color: 'var(--text-primary)',
            fontFamily: "var(--font-mono, 'JetBrains Mono', monospace)",
          }}
        >
          {t('chat:sidebar.title')}
        </h2>
        <button
          onClick={onCreate}
          className="flex items-center gap-1 px-2 py-1 rounded-md text-xs font-medium transition-colors hover:bg-white/5"
          style={{ color: 'var(--accent)' }}
          aria-label={t('chat:sidebar.newChat')}
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <line x1="12" y1="5" x2="12" y2="19" />
            <line x1="5" y1="12" x2="19" y2="12" />
          </svg>
          {t('chat:sidebar.newButton')}
        </button>
      </div>

      {/* Chat list */}
      <div
        className="flex-1 overflow-y-auto chat-scroll py-1.5"
      >
        {chats.length === 0 && !isLoading && (
          <div className="px-4 py-8 text-center">
            <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
              {t('chat:sidebar.noConversations')}
            </p>
          </div>
        )}

        {chats.map((chat) => (
          <ChatListItem
            key={chat.id}
            chat={chat}
            isActive={chat.id === activeSessionId}
            onSelect={onSelect}
            onRename={onRename}
            onDelete={onDelete}
            onFork={onFork}
          />
        ))}

        {isLoading && (
          <div className="px-4 py-3 text-center">
            <span className="text-xs" style={{ color: 'var(--text-muted)' }}>
              Loading...
            </span>
          </div>
        )}

        {/* Sentinel for infinite scroll via IntersectionObserver */}
        <div ref={sentinelRef} className="h-1" />
      </div>
    </>
  );
}

export function ChatSidebar(props: ChatSidebarProps) {
  const { t } = useTranslation(['chat', 'common']);
  const { embedded = false, ...contentProps } = props;
  const [isOpen, setIsOpen] = useState(true);

  useEffect(() => { loadNamespace('chat'); loadNamespace('common'); }, []);

  // Embedded mode: just render content, no outer shell
  if (embedded) {
    return <ChatSidebarContent {...contentProps} />;
  }

  return (
    <>
      {/* Toggle button — always visible */}
      <button
        onClick={() => setIsOpen(!isOpen)}
        className="absolute left-0 top-1/2 -translate-y-1/2 z-30 flex items-center justify-center w-5 h-10 rounded-r-md transition-all duration-200 hover:bg-white/5"
        style={{
          backgroundColor: 'var(--bg-surface)',
          borderRight: '1px solid var(--border-dim)',
          borderTop: '1px solid var(--border-dim)',
          borderBottom: '1px solid var(--border-dim)',
          left: isOpen ? '280px' : '0px',
        }}
        aria-label={isOpen ? t('chat:sidebar.collapseSidebar') : t('chat:sidebar.expandSidebar')}
      >
        <svg
          width="12"
          height="12"
          viewBox="0 0 24 24"
          fill="none"
          stroke="var(--text-muted)"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          style={{ transform: isOpen ? 'rotate(180deg)' : 'none', transition: 'transform 200ms' }}
        >
          <polyline points="9 18 15 12 9 6" />
        </svg>
      </button>

      {/* Sidebar panel */}
      <div
        className="flex-none flex flex-col border-r transition-all duration-200 overflow-hidden"
        style={{
          width: isOpen ? '280px' : '0px',
          backgroundColor: 'var(--bg-surface)',
          borderColor: 'var(--border-dim)',
        }}
      >
        <ChatSidebarContent {...contentProps} />
      </div>
    </>
  );
}
