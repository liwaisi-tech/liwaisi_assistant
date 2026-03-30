import type { SessionState } from '../../types/api';

interface ChatHeaderProps {
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

export function ChatHeader({ sessionState, isConnected }: ChatHeaderProps) {
  const state = stateConfig[sessionState];

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
