import type { SessionState } from '../../types/api';
import { useAuth } from '../../contexts/AuthContext';
import { BalanceWidget } from '../billing/BalanceWidget';

interface StatusBarProps {
  sessionState: SessionState;
  isConnected: boolean;
}

const stateConfig: Record<SessionState, { label: string; className: string }> = {
  idle:      { label: 'Idle',      className: 'bg-slate-700/50 text-slate-400' },
  running:   { label: 'Running',   className: 'bg-sky-500/20 text-sky-400 status-pulse' },
  waiting:   { label: 'Waiting',   className: 'bg-amber-500/20 text-amber-400' },
  completed: { label: 'Completed', className: 'bg-emerald-500/20 text-emerald-400' },
  failed:    { label: 'Failed',    className: 'bg-red-500/20 text-red-400' },
};

export function StatusBar({ sessionState, isConnected }: StatusBarProps) {
  const state = stateConfig[sessionState];
  const { user, logout } = useAuth();

  return (
    <header
      className="glass-surface flex items-center justify-between px-4 py-2 border-b relative z-10"
      style={{ borderColor: 'var(--border-dim)' }}
    >
      {/* Left: OS Branding */}
      <div className="flex items-center gap-2.5">
        <div className="flex items-center gap-2">
          <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path
              d="M12 2L14.5 9.5L22 12L14.5 14.5L12 22L9.5 14.5L2 12L9.5 9.5L12 2Z"
              fill="var(--accent)"
              opacity="0.9"
            />
            <path
              d="M12 2L14.5 9.5L22 12L14.5 14.5L12 22L9.5 14.5L2 12L9.5 9.5L12 2Z"
              fill="none"
              stroke="var(--accent)"
              strokeWidth="0.5"
              opacity="0.4"
            />
          </svg>
          <h1
            className="text-sm font-semibold tracking-tight"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              color: 'var(--text-primary)',
            }}
          >
            Liwaisi<span style={{ color: 'var(--accent)', marginLeft: '4px' }}>OS</span>
          </h1>
        </div>
      </div>

      {/* Center: Status indicators */}
      <div className="absolute left-1/2 -translate-x-1/2 flex items-center gap-3">
        <span className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[10px] font-medium ${state.className}`}>
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

      {/* Right: User + Balance */}
      <div className="flex items-center gap-3">
        <BalanceWidget />

        {user && (
          <div className="flex items-center gap-2">
            <img
              src={user.picture}
              alt={user.name}
              className="w-6 h-6 rounded-full ring-1 ring-[var(--border-dim)]"
              referrerPolicy="no-referrer"
            />
            <span className="text-[11px] hidden sm:inline" style={{ color: 'var(--text-secondary)' }}>
              {user.name}
            </span>
            <button
              onClick={logout}
              className="text-[10px] px-2 py-1 rounded transition-colors"
              style={{ color: 'var(--text-muted)' }}
              onMouseEnter={(e) => (e.currentTarget.style.color = 'var(--text-secondary)')}
              onMouseLeave={(e) => (e.currentTarget.style.color = 'var(--text-muted)')}
              aria-label="Sign out"
            >
              Sign out
            </button>
          </div>
        )}
      </div>
    </header>
  );
}
