/**
 * CPNAnimation — CSS-only animated Coloured Petri Net diagram.
 *
 * Shows a simplified CPN with Places (circles), Transitions (rectangles),
 * and animated Tokens (dots) flowing along arcs.
 *
 * CPN Mathematician Expert notes:
 * - Token conservation: tokens disappear from input, appear at output
 * - Color semantics: blue=data, green=human, amber=AI
 * - Space separation: left (Surface/human) vs right (Computation/AI)
 */

const PLACES = [
  { id: 'p-input', label: 'Your Request', x: 8, y: 50, space: 'surface' },
  { id: 'p-draft', label: 'AI Draft', x: 50, y: 25, space: 'computation' },
  { id: 'p-output', label: 'Final Result', x: 92, y: 50, space: 'surface' },
  { id: 'p-revision', label: 'Revision', x: 50, y: 75, space: 'computation' },
] as const;

const TRANSITIONS = [
  { id: 't-analyze', label: 'AI Analysis', x: 30, y: 38, kind: 'llm' },
  { id: 't-approve', label: 'Your Review', x: 70, y: 38, kind: 'hitl' },
  { id: 't-revise', label: 'AI Revision', x: 70, y: 75, kind: 'llm' },
] as const;

// Token animation paths (keyframe positions as % along an arc)
const TOKEN_PATHS = [
  // Path 1: Input → AI Analysis → Draft (main happy path)
  { from: 'p-input', to: 't-analyze', color: '#0ea5e9', delay: 0, duration: 3 },
  { from: 't-analyze', to: 'p-draft', color: '#f59e0b', delay: 1.5, duration: 3 },
  // Path 2: Draft → Your Review → Output
  { from: 'p-draft', to: 't-approve', color: '#0ea5e9', delay: 3, duration: 3 },
  { from: 't-approve', to: 'p-output', color: '#10b981', delay: 4.5, duration: 3 },
  // Path 3: Revision loop
  { from: 't-approve', to: 'p-revision', color: '#ef4444', delay: 6, duration: 4 },
  { from: 'p-revision', to: 't-revise', color: '#f59e0b', delay: 8, duration: 4 },
  { from: 't-revise', to: 'p-draft', color: '#0ea5e9', delay: 10, duration: 4 },
];

function PlaceNode({ label, x, y, space }: { label: string; x: number; y: number; space: string }) {
  const isSurface = space === 'surface';
  return (
    <div
      className="absolute flex flex-col items-center gap-1"
      style={{ left: `${x}%`, top: `${y}%`, transform: 'translate(-50%, -50%)' }}
    >
      <div
        className="cpn-place w-12 h-12 sm:w-16 sm:h-16 rounded-full border-2 flex items-center justify-center"
        style={{
          borderColor: isSurface ? '#10b981' : 'var(--accent)',
          backgroundColor: isSurface ? 'rgba(16,185,129,0.08)' : 'rgba(14,165,233,0.08)',
        }}
      >
        <div
          className="w-2 h-2 sm:w-3 sm:h-3 rounded-full"
          style={{ backgroundColor: isSurface ? '#10b981' : 'var(--accent)' }}
        />
      </div>
      <span
        className="text-[9px] sm:text-[10px] font-medium text-center whitespace-nowrap"
        style={{
          fontFamily: "'JetBrains Mono', monospace",
          color: 'var(--text-muted)',
        }}
      >
        {label}
      </span>
    </div>
  );
}

function TransitionNode({ label, x, y, kind }: { label: string; x: number; y: number; kind: string }) {
  const isHITL = kind === 'hitl';
  return (
    <div
      className="absolute flex flex-col items-center gap-1"
      style={{ left: `${x}%`, top: `${y}%`, transform: 'translate(-50%, -50%)' }}
    >
      <div
        className={`w-16 h-8 sm:w-20 sm:h-10 rounded-md border flex items-center justify-center gap-1 ${!isHITL ? 'cpn-transition-fire' : ''}`}
        style={{
          borderColor: isHITL ? '#a78bfa' : 'var(--accent)',
          backgroundColor: isHITL ? 'rgba(167,139,250,0.1)' : 'var(--bg-surface)',
        }}
      >
        {isHITL && (
          <svg width="12" height="12" viewBox="0 0 16 16" fill="none" className="hitl-indicator shrink-0">
            <circle cx="8" cy="8" r="6" stroke="#a78bfa" strokeWidth="1.5" />
            <path d="M6 5v6M10 5v6" stroke="#a78bfa" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
        )}
        {!isHITL && (
          <svg width="12" height="12" viewBox="0 0 16 16" fill="none" className="shrink-0">
            <path d="M3 8h10M8 3l5 5-5 5" stroke="var(--accent)" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
        )}
      </div>
      <span
        className="text-[9px] sm:text-[10px] font-medium text-center whitespace-nowrap"
        style={{
          fontFamily: "'JetBrains Mono', monospace",
          color: isHITL ? '#a78bfa' : 'var(--text-muted)',
        }}
      >
        {label}
      </span>
    </div>
  );
}

function AnimatedToken({ color, delay, duration }: { color: string; delay: number; duration: number }) {
  return (
    <div
      className="cpn-token"
      style={{
        backgroundColor: color,
        color,
        animationDelay: `${delay}s`,
        animationDuration: `${duration}s`,
        opacity: 0,
        // Use blinking visibility since we don't have offset-path
        animation: `token-pulse ${duration}s ease-in-out ${delay}s infinite`,
      }}
    />
  );
}

// SVG arcs connecting places and transitions
function CPNArcs() {
  return (
    <svg
      className="absolute inset-0 w-full h-full"
      viewBox="0 0 100 100"
      preserveAspectRatio="none"
      style={{ opacity: 0.2 }}
    >
      {/* Input → AI Analysis */}
      <line x1="14" y1="50" x2="24" y2="38" stroke="var(--accent)" strokeWidth="0.3" strokeDasharray="1 1" />
      {/* AI Analysis → Draft */}
      <line x1="36" y1="38" x2="44" y2="25" stroke="var(--accent)" strokeWidth="0.3" strokeDasharray="1 1" />
      {/* Draft → Your Review */}
      <line x1="56" y1="25" x2="64" y2="38" stroke="var(--accent)" strokeWidth="0.3" strokeDasharray="1 1" />
      {/* Your Review → Output */}
      <line x1="76" y1="38" x2="86" y2="50" stroke="#10b981" strokeWidth="0.3" strokeDasharray="1 1" />
      {/* Your Review → Revision (reject path) */}
      <line x1="70" y1="45" x2="70" y2="70" stroke="#ef4444" strokeWidth="0.2" strokeDasharray="1 1" />
      {/* Revision → AI Revision */}
      <line x1="56" y1="75" x2="64" y2="75" stroke="#f59e0b" strokeWidth="0.2" strokeDasharray="1 1" />
      {/* AI Revision → Draft (loop back) */}
      <path d="M 76 75 Q 80 50 56 25" fill="none" stroke="#f59e0b" strokeWidth="0.2" strokeDasharray="1 1" />
    </svg>
  );
}

export function CPNAnimation() {
  return (
    <div
      className="relative w-full aspect-[2/1] max-w-xl mx-auto"
      role="img"
      aria-label="Animated Coloured Petri Net diagram showing AI workflow: Your Request flows through AI Analysis to create a Draft, then through Your Review to reach the Final Result, with a revision loop for rejected drafts"
    >
      {/* Space labels */}
      <div
        className="absolute top-1 left-2 text-[8px] sm:text-[9px] uppercase tracking-widest"
        style={{ color: '#10b981', fontFamily: "'JetBrains Mono', monospace", opacity: 0.5 }}
      >
        Surface
      </div>
      <div
        className="absolute top-1 right-2 text-[8px] sm:text-[9px] uppercase tracking-widest text-right"
        style={{ color: 'var(--accent)', fontFamily: "'JetBrains Mono', monospace", opacity: 0.5 }}
      >
        Computation
      </div>

      {/* Vertical space divider */}
      <div
        className="absolute top-0 bottom-0 left-1/2 w-px"
        style={{ background: 'linear-gradient(to bottom, transparent, var(--border-dim), transparent)', opacity: 0.3 }}
      />

      <CPNArcs />

      {PLACES.map((p) => (
        <PlaceNode key={p.id} label={p.label} x={p.x} y={p.y} space={p.space} />
      ))}

      {TRANSITIONS.map((t) => (
        <TransitionNode key={t.id} label={t.label} x={t.x} y={t.y} kind={t.kind} />
      ))}

      {/* Animated tokens scattered at arc midpoints */}
      {TOKEN_PATHS.map((tp, i) => (
        <AnimatedToken key={i} color={tp.color} delay={tp.delay} duration={tp.duration} />
      ))}
    </div>
  );
}
