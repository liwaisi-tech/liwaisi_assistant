import { useState, useEffect } from 'react';
import { getFlow, getSessionExecution } from '../../services/api';
import { TopologyGraph } from './TopologyGraph';
import type { FlowDetail as FlowDetailType, ExecutionEvent, TransitionTopology } from '../../types/flow';

interface FlowDetailProps {
  hash: string;
  onBack: () => void;
}

export function FlowDetail({ hash, onBack }: FlowDetailProps) {
  const [flow, setFlow] = useState<FlowDetailType | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [firedTransitions, setFiredTransitions] = useState<Set<string> | undefined>();
  const [events, setEvents] = useState<ExecutionEvent[]>([]);
  const [sessionId, setSessionId] = useState('');
  const [selectedTransition, setSelectedTransition] = useState<TransitionTopology | null>(null);

  useEffect(() => {
    setLoading(true);
    getFlow(hash)
      .then((f) => setFlow(f))
      .catch((err) => setError(err.message))
      .finally(() => setLoading(false));
  }, [hash]);

  const loadExecution = () => {
    if (!sessionId.trim()) return;
    getSessionExecution(sessionId.trim())
      .then((exec) => {
        setEvents(exec.events);
        const fired = new Set(exec.events.map((e) => e.transition_id).filter(Boolean));
        setFiredTransitions(fired);
      })
      .catch(() => setEvents([]));
  };

  if (loading) {
    return (
      <div className="flex-1 flex items-center justify-center" style={{ color: 'var(--text-muted)' }}>
        <div className="thinking-dots"><span /><span /><span /></div>
      </div>
    );
  }

  if (error || !flow) {
    return (
      <div className="flex-1 flex items-center justify-center" style={{ color: 'var(--text-muted)' }}>
        <p>Flow not found: {error}</p>
      </div>
    );
  }

  const formatMs = (ms: number) => {
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(1)}s`;
  };

  return (
    <div className="flex-1 flex flex-col overflow-hidden">
      {/* Header */}
      <div
        className="flex items-center justify-between px-4 py-2.5"
        style={{ borderBottom: '1px solid var(--border-dim)', backgroundColor: 'var(--bg-surface)' }}
      >
        <div className="flex items-center gap-3">
          <button
            onClick={onBack}
            className="text-xs px-2.5 py-1 rounded-lg transition-all hover:scale-105"
            style={{ color: 'var(--accent)', border: '1px solid var(--border-dim)', fontFamily: "'JetBrains Mono', monospace" }}
          >
            \u2190 Back
          </button>
          <div>
            <div className="flex items-center gap-2">
              <span
                className="text-[9px] font-bold px-1.5 py-0.5 rounded"
                style={{ backgroundColor: 'rgba(14,165,233,0.15)', color: 'var(--accent)', fontFamily: "'JetBrains Mono', monospace" }}
              >
                {flow.role.toUpperCase()}
              </span>
              <span
                className="text-xs font-semibold"
                style={{ fontFamily: "'JetBrains Mono', monospace", color: 'var(--text-primary)' }}
              >
                {hash.slice(0, 12)}...
              </span>
            </div>
            <div className="text-[9px] mt-0.5" style={{ color: 'var(--text-muted)' }}>
              {Object.keys(flow.topology.places).length} places \u00B7 {Object.keys(flow.topology.transitions).length} transitions
            </div>
          </div>
        </div>

        {/* Stats */}
        <div
          className="flex gap-4 text-[10px]"
          style={{ fontFamily: "'JetBrains Mono', monospace", color: 'var(--text-secondary)' }}
        >
          <span><b style={{ color: 'var(--accent)' }}>{flow.stats.execution_count}</b> runs</span>
          <span><b style={{ color: '#10b981' }}>{(flow.stats.success_rate * 100).toFixed(0)}%</b> success</span>
          <span><b style={{ color: '#f59e0b' }}>${flow.stats.avg_cost_usd.toFixed(4)}</b> avg</span>
          <span><b>{formatMs(flow.stats.avg_duration_ms)}</b> avg</span>
        </div>
      </div>

      {/* Execution loader bar */}
      <div
        className="flex items-center gap-2 px-4 py-1.5"
        style={{ borderBottom: '1px solid var(--border-dim)' }}
      >
        <span className="text-[9px]" style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}>
          REPLAY
        </span>
        <input
          type="text"
          placeholder="session ID..."
          value={sessionId}
          onChange={(e) => setSessionId(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && loadExecution()}
          className="flex-1 text-[10px] px-2.5 py-1 rounded-md outline-none"
          style={{
            backgroundColor: 'var(--bg-input)',
            border: '1px solid var(--border-dim)',
            color: 'var(--text-primary)',
            fontFamily: "'JetBrains Mono', monospace",
            maxWidth: 300,
          }}
        />
        <button
          onClick={loadExecution}
          className="text-[9px] px-2.5 py-1 rounded-md font-semibold"
          style={{ backgroundColor: 'rgba(14,165,233,0.12)', color: 'var(--accent)', border: '1px solid rgba(14,165,233,0.25)' }}
        >
          Load
        </button>
        {firedTransitions && (
          <>
            <span className="text-[9px]" style={{ color: 'var(--accent)', fontFamily: "'JetBrains Mono', monospace" }}>
              {firedTransitions.size} fired \u00B7 {events.length} events
            </span>
            <button
              onClick={() => { setFiredTransitions(undefined); setEvents([]); }}
              className="text-[9px] px-1.5 py-0.5 rounded"
              style={{ color: 'var(--text-muted)', border: '1px solid var(--border-dim)' }}
            >
              \u2715
            </button>
          </>
        )}
      </div>

      {/* Main area: graph + optional side panel */}
      <div className="flex-1 flex overflow-hidden">
        {/* Graph */}
        <div className="flex-1">
          <TopologyGraph
            topology={flow.topology}
            firedTransitions={firedTransitions}
            onSelectTransition={setSelectedTransition}
          />
        </div>

        {/* Side panel — transition detail */}
        {selectedTransition && (
          <TransitionPanel
            transition={selectedTransition}
            onClose={() => setSelectedTransition(null)}
          />
        )}
      </div>
    </div>
  );
}

// ── Transition Detail Side Panel ───────────────────────────────────────────

function TransitionPanel({ transition, onClose }: { transition: TransitionTopology; onClose: () => void }) {
  const color = {
    llm: '#0ea5e9', tool: '#10b981', validate: '#a855f7',
    subnet: '#6366f1', observer: '#64748b', hitl: '#f59e0b',
  }[transition.kind] || '#64748b';

  return (
    <div
      className="w-80 overflow-y-auto flex-shrink-0"
      style={{
        backgroundColor: 'var(--bg-surface)',
        borderLeft: `1px solid var(--border-dim)`,
      }}
    >
      {/* Panel header */}
      <div
        className="flex items-center justify-between px-4 py-3"
        style={{ borderBottom: `2px solid ${color}30` }}
      >
        <div>
          <div className="text-[9px] font-bold mb-0.5" style={{ color, fontFamily: "'JetBrains Mono', monospace" }}>
            {transition.kind.toUpperCase()} TRANSITION
          </div>
          <div className="text-sm font-semibold" style={{ color: 'var(--text-primary)', fontFamily: "'JetBrains Mono', monospace" }}>
            {transition.id}
          </div>
        </div>
        <button
          onClick={onClose}
          className="text-xs px-1.5 py-0.5 rounded"
          style={{ color: 'var(--text-muted)', border: '1px solid var(--border-dim)' }}
        >
          \u2715
        </button>
      </div>

      <div className="px-4 py-3 space-y-4">
        {/* Connections */}
        <Section title="Connections">
          <Field label="Inputs" value={transition.inputPlaces.join(', ')} />
          <Field label="Outputs" value={transition.outputPlaces.join(', ')} />
          {transition.errorPlace && <Field label="Error" value={transition.errorPlace} />}
        </Section>

        {/* Guard */}
        {transition.guardFunc && (
          <Section title="Guard Function">
            <code
              className="text-[10px] block px-2 py-1.5 rounded"
              style={{ backgroundColor: 'var(--bg-input)', color, fontFamily: "'JetBrains Mono', monospace" }}
            >
              {transition.guardFunc}
            </code>
          </Section>
        )}

        {/* LLM Config */}
        {transition.llmConfig && (
          <Section title="LLM Configuration">
            {transition.llmConfig.model && <Field label="Model" value={transition.llmConfig.model} />}
            {transition.llmConfig.maxTokens && <Field label="Max Tokens" value={String(transition.llmConfig.maxTokens)} />}
            {transition.llmConfig.temperature != null && <Field label="Temperature" value={String(transition.llmConfig.temperature)} />}
            <Field label="Stream" value={transition.llmConfig.streamOutput ? 'Yes' : 'No'} />
            {transition.llmConfig.requireJSON && <Field label="JSON Mode" value="Required" />}
            {transition.llmConfig.skipHistory && <Field label="History" value="Skipped" />}
          </Section>
        )}

        {/* HITL Config */}
        {transition.hitlConfig && (
          <Section title="HITL Configuration">
            <Field label="Prompt" value={transition.hitlConfig.prompt || 'None'} />
            {transition.hitlConfig.revisionLoop && <Field label="Revision" value="Enabled" />}
          </Section>
        )}

        {/* System Prompt */}
        {transition.systemPrompt && (
          <Section title="System Prompt">
            <pre
              className="text-[9px] leading-relaxed whitespace-pre-wrap px-2.5 py-2 rounded-lg max-h-64 overflow-y-auto"
              style={{
                backgroundColor: 'var(--bg-input)',
                color: 'var(--text-secondary)',
                fontFamily: "'JetBrains Mono', monospace",
                border: '1px solid var(--border-dim)',
              }}
            >
              {transition.systemPrompt}
            </pre>
          </Section>
        )}
      </div>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div>
      <div
        className="text-[8px] font-bold uppercase tracking-widest mb-1.5"
        style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
      >
        {title}
      </div>
      <div className="space-y-1">{children}</div>
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-start gap-2 text-[10px]">
      <span className="flex-shrink-0" style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace", minWidth: 70 }}>
        {label}
      </span>
      <span style={{ color: 'var(--text-primary)', fontFamily: "'JetBrains Mono', monospace", wordBreak: 'break-word' }}>
        {value}
      </span>
    </div>
  );
}
