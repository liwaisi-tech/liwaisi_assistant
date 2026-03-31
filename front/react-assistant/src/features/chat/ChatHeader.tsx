import type { SessionState } from '../../types/api';
import { useAuth } from '../../contexts/AuthContext';

interface ChatHeaderProps {
  sessionState: SessionState;
  isConnected: boolean;
  onClearConversation: () => void;
  clearDisabled: boolean;
}

const stateConfig: Record<SessionState, { label: string; className: string }> = {
  idle:      { label: 'Idle',      className: 'bg-slate-700/50 text-slate-400' },
  running:   { label: 'Running',   className: 'bg-sky-500/20 text-sky-400 status-pulse' },
  waiting:   { label: 'Waiting',   className: 'bg-amber-500/20 text-amber-400' },
  completed: { label: 'Completed', className: 'bg-emerald-500/20 text-emerald-400' },
  failed:    { label: 'Failed',    className: 'bg-red-500/20 text-red-400' },
};

export function ChatHeader({ sessionState, isConnected, onClearConversation, clearDisabled }: ChatHeaderProps) {
  const state = stateConfig[sessionState];
  const { user, logout } = useAuth();

  return (
    <header className="flex items-center justify-between px-4 py-3 border-b"
            style={{ borderColor: 'var(--border-dim)', backgroundColor: 'var(--bg-surface)' }}>
      <div className="flex items-center gap-3">
        <h1 className="text-base font-semibold tracking-tight"
            style={{ fontFamily: "'JetBrains Mono', monospace", color: 'var(--text-primary)' }}>
          Liwaisi Assistant
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
          aria-label="New conversation"
          title="New conversation"
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
              aria-label="Sign out"
            >
              Sign out
            </button>
          </div>
        )}

        <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium ${state.className}`}>
          {sessionState === 'running' && (
            <span className="w-1.5 h-1.5 rounded-full bg-sky-400 status-pulse" />
          )}
          {state.label}
        </span>

        <div className="flex items-center gap-1.5" title={isConnected ? 'Connected' : 'Disconnected'}>
          <span className={`connection-dot ${isConnected ? 'text-emerald-400 bg-emerald-400' : 'text-red-400 bg-red-400'}`} />
          <span className="text-xs sr-only">
            {isConnected ? 'Connected' : 'Disconnected'}
          </span>
        </div>
      </div>
    </header>
  );
}
