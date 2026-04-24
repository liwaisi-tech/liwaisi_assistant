import { useEffect, useState } from 'react';
import { getFlows } from '../../services/api';
import type { FlowSummary } from '../../types/flow';

interface FlowHashtagChipsProps {
  onInsert: (marker: string) => void;
  disabled?: boolean;
}

/**
 * Horizontal row of clickable hashtag chips — one per registered flow.
 * Clicking a chip inserts `#<role> ` into the composer, which the backend
 * `SendMessage` dispatcher (internal/app/session_service.go dispatchRole)
 * uses to swap the session's Root CPN to the matching topology.
 */
export function FlowHashtagChips({ onInsert, disabled }: FlowHashtagChipsProps) {
  const [flows, setFlows] = useState<FlowSummary[]>([]);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    let cancelled = false;
    getFlows()
      .then((resp) => {
        if (cancelled) return;
        setFlows(resp.items ?? []);
        setLoaded(true);
      })
      .catch(() => {
        if (cancelled) return;
        setLoaded(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  if (!loaded || flows.length === 0) return null;

  return (
    <div
      className="flex flex-wrap items-center gap-1.5 mb-2"
      aria-label="Flow dispatch shortcuts"
    >
      <span
        className="text-[10px] uppercase tracking-wide mr-1"
        style={{ color: 'var(--text-muted)' }}
      >
        Flujos:
      </span>
      {flows.map((f) => (
        <button
          key={f.hash}
          type="button"
          disabled={disabled}
          onClick={() => onInsert(`#${f.role} `)}
          className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium transition-all disabled:opacity-40 disabled:cursor-not-allowed enabled:hover:brightness-125 enabled:hover:shadow-[0_0_8px_-2px_var(--accent-glow)]"
          style={{
            backgroundColor: 'var(--bg-input)',
            border: '1px solid var(--border-dim)',
            color: 'var(--accent)',
          }}
          title={`Insertar #${f.role} y rutear la conversación a este flujo`}
        >
          <span style={{ opacity: 0.6 }}>#</span>
          {f.role}
        </button>
      ))}
    </div>
  );
}
