import type { TensionResponse } from '../../types/personality';

interface TensionVisualizerProps {
  tensions: TensionResponse[];
}

const kindColors: Record<string, string> = {
  nucleo: '#38bdf8',
  conducta: '#fbbf24',
  etica: '#34d399',
};

export function TensionVisualizer({ tensions }: TensionVisualizerProps) {
  if (tensions.length === 0) return null;

  // Extract unique principle names from tensions
  const principleNames = Array.from(
    new Set(tensions.flatMap((t) => t.between)),
  );

  // Position nodes in a triangle layout
  // top center, bottom-left, bottom-right
  const positions = [
    { x: 50, y: 8 },
    { x: 12, y: 80 },
    { x: 88, y: 80 },
  ];

  const getColor = (name: string) => {
    const lower = name.toLowerCase();
    for (const [kind, color] of Object.entries(kindColors)) {
      if (lower.includes(kind)) return color;
    }
    return 'var(--accent)';
  };

  return (
    <div
      className="rounded-xl border p-4"
      style={{
        backgroundColor: 'var(--bg-surface)',
        borderColor: 'var(--border-dim)',
      }}
    >
      <span
        className="text-[10px] font-semibold uppercase tracking-wider block mb-3"
        style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
      >
        Tension Map
      </span>

      {/* Triangle diagram */}
      <div className="relative w-full" style={{ height: 200 }}>
        {/* Lines between nodes */}
        <svg
          className="absolute inset-0 w-full h-full pointer-events-none"
          viewBox="0 0 100 100"
          preserveAspectRatio="none"
        >
          {tensions.map((tension, i) => {
            const idxA = principleNames.indexOf(tension.between[0]);
            const idxB = principleNames.indexOf(tension.between[1]);
            if (idxA < 0 || idxB < 0) return null;
            const a = positions[idxA % 3];
            const b = positions[idxB % 3];
            return (
              <line
                key={i}
                x1={`${a.x}%`}
                y1={`${a.y}%`}
                x2={`${b.x}%`}
                y2={`${b.y}%`}
                stroke="var(--border-dim)"
                strokeWidth="0.5"
                strokeDasharray="2,2"
              />
            );
          })}
        </svg>

        {/* Nodes */}
        {principleNames.slice(0, 3).map((name, i) => {
          const pos = positions[i];
          const color = getColor(name);
          return (
            <div
              key={name}
              className="absolute flex flex-col items-center"
              style={{
                left: `${pos.x}%`,
                top: `${pos.y}%`,
                transform: 'translate(-50%, -50%)',
              }}
            >
              <div
                className="w-10 h-10 rounded-full flex items-center justify-center text-[10px] font-bold border-2"
                style={{
                  backgroundColor: `${color}15`,
                  borderColor: `${color}60`,
                  color: color,
                  fontFamily: "'JetBrains Mono', monospace",
                }}
              >
                {name.charAt(0).toUpperCase()}
              </div>
              <span
                className="mt-1 text-[9px] font-medium whitespace-nowrap"
                style={{ color: 'var(--text-secondary)', fontFamily: "'JetBrains Mono', monospace" }}
              >
                {name}
              </span>
            </div>
          );
        })}

        {/* Friction labels on edges */}
        {tensions.map((tension, i) => {
          const idxA = principleNames.indexOf(tension.between[0]);
          const idxB = principleNames.indexOf(tension.between[1]);
          if (idxA < 0 || idxB < 0) return null;
          const a = positions[idxA % 3];
          const b = positions[idxB % 3];
          const mx = (a.x + b.x) / 2;
          const my = (a.y + b.y) / 2;
          return (
            <div
              key={`friction-${i}`}
              className="absolute px-2 py-0.5 rounded text-[8px] max-w-[120px] text-center"
              style={{
                left: `${mx}%`,
                top: `${my}%`,
                transform: 'translate(-50%, -50%)',
                backgroundColor: 'var(--bg-input)',
                color: 'var(--text-muted)',
                border: '1px solid var(--border-dim)',
                fontFamily: "'JetBrains Mono', monospace",
              }}
              title={`Resolution: ${tension.resolution}`}
            >
              {tension.friction}
            </div>
          );
        })}
      </div>

      {/* Resolutions list */}
      {tensions.length > 0 && (
        <div className="mt-3 space-y-2">
          {tensions.map((tension, i) => (
            <div key={i} className="text-[11px]" style={{ color: 'var(--text-secondary)' }}>
              <span style={{ color: 'var(--text-muted)' }}>
                {tension.between[0]} / {tension.between[1]}:
              </span>{' '}
              {tension.resolution}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
