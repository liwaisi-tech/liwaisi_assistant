import type { SessionState } from '../../types/api';
import { useAuth } from '../../contexts/AuthContext';
import { useTranslation } from 'react-i18next';
import { useEffect } from 'react';
import { loadNamespace } from '../../i18n/loadNamespace';

/**
 * Three-state SSE/session connection indicator per spec REQ-112:
 *   - 'connected'            → green, steady pulse
 *   - 'reconnecting'         → amber, animated (subtle pulse), during transient
 *                              errors / exponential backoff retries
 *   - 'disconnected-terminal'→ red, used only when SessionNotFoundError has
 *                              fired AND auto-recovery has also failed
 */
export type ConnectionState = 'connected' | 'reconnecting' | 'disconnected-terminal';

interface ChatHeaderProps {
  sessionState: SessionState;
  /**
   * Preferred: explicit three-state indicator (REQ-112). If provided, takes
   * precedence over `isConnected`.
   */
  connectionState?: ConnectionState;
  /**
   * Back-compat: boolean connection flag. Maps to `connected` / `reconnecting`
   * until Agent 2 wires the typed value through `useSSE`. See spec §4.3.
   */
  isConnected?: boolean;
  onClearConversation: () => void;
  clearDisabled: boolean;
}

const stateConfig: Record<SessionState, { labelKey: string; className: string }> = {
  idle:      { labelKey: 'common:status.idle',      className: 'bg-slate-700/50 text-slate-400' },
  running:   { labelKey: 'common:status.running',   className: 'bg-sky-500/20 text-sky-400 status-pulse' },
  waiting:   { labelKey: 'common:status.waiting',   className: 'bg-amber-500/20 text-amber-400' },
  completed: { labelKey: 'common:status.completed', className: 'bg-emerald-500/20 text-emerald-400' },
  failed:    { labelKey: 'common:status.failed',    className: 'bg-red-500/20 text-red-400' },
};

const connectionConfig: Record<ConnectionState, { className: string; labelKey: string }> = {
  connected: {
    // Green: steady, no extra pulse beyond the base `.connection-dot` CSS.
    className: 'text-emerald-400 bg-emerald-400',
    labelKey: 'connection.connected',
  },
  reconnecting: {
    // Amber + status-pulse opacity animation so it visually differs from both
    // steady-green and solid-red (AC-010).
    className: 'text-amber-400 bg-amber-400 status-pulse',
    labelKey: 'connection.reconnecting',
  },
  'disconnected-terminal': {
    // Red, no pulse, conveys "final" / non-recoverable state.
    className: 'text-red-500 bg-red-500',
    labelKey: 'connection.disconnected',
  },
};

function resolveConnectionState(
  explicit: ConnectionState | undefined,
  isConnected: boolean | undefined
): ConnectionState {
  if (explicit) return explicit;
  if (isConnected) return 'connected';
  // Without the typed signal, treat "disconnected" as a transient reconnecting
  // state — `disconnected-terminal` must ONLY be used after a confirmed
  // SessionNotFoundError + recovery failure (REQ-112).
  return 'reconnecting';
}

export function ChatHeader({
  sessionState,
  connectionState,
  isConnected,
  onClearConversation,
  clearDisabled,
}: ChatHeaderProps) {
  const { t } = useTranslation(['chat', 'common']);
  const state = stateConfig[sessionState];
  const { user, logout } = useAuth();

  useEffect(() => { loadNamespace('chat'); }, []);

  const effectiveConnection = resolveConnectionState(connectionState, isConnected);
  const connection = connectionConfig[effectiveConnection];
  const connectionLabel = t(connection.labelKey);

  return (
    <header className="flex items-center justify-between px-4 py-3 border-b"
            style={{ borderColor: 'var(--border-dim)', backgroundColor: 'var(--bg-surface)' }}>
      <div className="flex items-center gap-3">
        <h1 className="text-base font-semibold tracking-tight"
            style={{ fontFamily: "'JetBrains Mono', monospace", color: 'var(--text-primary)' }}>
          {t('header.title')}
        </h1>
      </div>

      <div className="flex items-center gap-3">
        <button
          onClick={onClearConversation}
          disabled={clearDisabled}
          className="p-1.5 rounded-md transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
          style={{ color: 'var(--text-muted)' }}
          onMouseEnter={(e) => { if (!e.currentTarget.disabled) e.currentTarget.style.color = 'var(--accent)'; }}
          onMouseLeave={(e) => { e.currentTarget.style.color = 'var(--text-muted)'; }}
          aria-label={t('header.newConversation')}
          title={t('header.newConversation')}
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
            <path d="M12 5v14M5 12h14" />
          </svg>
        </button>

        {user && (
          <div className="flex items-center gap-2">
            <img
              src={user.picture}
              alt={user.name}
              className="w-6 h-6 rounded-full"
              referrerPolicy="no-referrer"
            />
            <span className="text-xs hidden sm:inline" style={{ color: 'var(--text-secondary)' }}>
              {user.name}
            </span>
            <button
              onClick={logout}
              className="text-xs px-2 py-1 rounded transition-colors"
              style={{ color: 'var(--text-muted)' }}
              onMouseEnter={(e) => (e.currentTarget.style.color = 'var(--text-secondary)')}
              onMouseLeave={(e) => (e.currentTarget.style.color = 'var(--text-muted)')}
              aria-label={t('header.signOut')}
            >
              {t('header.signOut')}
            </button>
          </div>
        )}

        <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium ${state.className}`}>
          {sessionState === 'running' && (
            <span className="w-1.5 h-1.5 rounded-full bg-sky-400 status-pulse" />
          )}
          {t(state.labelKey)}
        </span>

        {/*
          Connection indicator: role="status" + aria-live="polite" so screen
          readers get an update on transitions (e.g., reconnecting → connected)
          without the indicator stealing focus. The visible dot uses colors +
          animation so the state is perceivable without relying on color alone
          (the sr-only label gives the textual fallback). REQ-112, AC-010.
        */}
        <div
          className="flex items-center gap-1.5"
          title={connectionLabel}
          role="status"
          aria-live="polite"
          data-testid="chat-header-connection"
          data-state={effectiveConnection}
        >
          <span className={`connection-dot ${connection.className}`} />
          <span className="text-xs sr-only">{connectionLabel}</span>
        </div>
      </div>
    </header>
  );
}
