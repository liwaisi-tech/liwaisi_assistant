import { useInView } from './useInView';

const STEPS = [
  {
    number: '01',
    icon: (
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
        <circle cx="12" cy="12" r="9" stroke="#10b981" strokeWidth="1.5" />
        <circle cx="12" cy="12" r="3" fill="#10b981" />
      </svg>
    ),
    title: 'You make a request',
    subtitle: 'Place: Surface Space',
    description:
      'Your input enters the system as a colored token in the Surface space — the human-facing boundary where you maintain full control.',
    color: '#10b981',
    cpnLabel: 'Token deposited in Input Place',
  },
  {
    number: '02',
    icon: (
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
        <rect x="3" y="6" width="18" height="12" rx="2" stroke="#0ea5e9" strokeWidth="1.5" />
        <path d="M8 12h8M12 9l3 3-3 3" stroke="#0ea5e9" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    ),
    title: 'AI agents analyze and draft',
    subtitle: 'Transition: Computation Space',
    description:
      'LLM transitions fire in the Computation space — consuming your input token, calling AI models, executing tools, and producing a structured draft.',
    color: '#0ea5e9',
    cpnLabel: 'Transition fires, LLM produces output tokens',
  },
  {
    number: '03',
    icon: (
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
        <circle cx="12" cy="12" r="9" stroke="#a78bfa" strokeWidth="1.5" />
        <path d="M9 9v6M15 9v6" stroke="#a78bfa" strokeWidth="2" strokeLinecap="round" />
      </svg>
    ),
    title: 'You review and decide',
    subtitle: 'HITL Transition: Space Bridge',
    description:
      'The Human-in-the-Loop transition pauses the net and presents the draft. You approve, reject, or request revisions — your voice crosses from Surface to Computation.',
    color: '#a78bfa',
    cpnLabel: 'HITL bridges Surface ↔ Computation spaces',
  },
  {
    number: '04',
    icon: (
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
        <circle cx="12" cy="12" r="9" stroke="#10b981" strokeWidth="1.5" />
        <path d="M8 12l3 3 5-5" stroke="#10b981" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    ),
    title: 'Result delivered',
    subtitle: 'Place: Output',
    description:
      'The approved result lands in the Output place — auditable, cost-tracked, and ready. Every step recorded for future optimization.',
    color: '#10b981',
    cpnLabel: 'Token deposited in Output Place, execution logged',
  },
];

export function HowItWorksSection() {
  const { ref, inView } = useInView();

  return (
    <section
      id="how-it-works"
      ref={ref as React.RefObject<HTMLElement>}
      className={`py-24 sm:py-32 px-4 sm:px-8 landing-reveal ${inView ? 'in-view' : ''}`}
      style={{ backgroundColor: 'var(--bg-surface)' }}
    >
      <div className="max-w-4xl mx-auto">
        <div className="text-center mb-16">
          <p
            className="text-xs uppercase tracking-[0.2em] mb-4"
            style={{ color: 'var(--accent)', fontFamily: "'JetBrains Mono', monospace" }}
          >
            How it works
          </p>
          <h2
            className="text-2xl sm:text-3xl lg:text-4xl font-semibold tracking-tight"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              color: 'var(--text-primary)',
            }}
          >
            From request to result,{' '}
            <span className="gradient-text">every step visible</span>
          </h2>
        </div>

        {/* Steps timeline */}
        <div className={`relative stagger-children ${inView ? 'in-view' : ''}`}>
          {/* Vertical line */}
          <div
            className="absolute left-6 sm:left-8 top-0 bottom-0 w-px hidden sm:block"
            style={{ background: 'linear-gradient(to bottom, var(--accent), #a78bfa, #10b981)' }}
          />

          {STEPS.map((step) => (
            <div key={step.number} className="relative flex gap-4 sm:gap-8 mb-12 last:mb-0">
              {/* Step number circle */}
              <div
                className="shrink-0 w-12 h-12 sm:w-16 sm:h-16 rounded-full border-2 flex items-center justify-center z-10"
                style={{
                  borderColor: step.color,
                  backgroundColor: `${step.color}10`,
                }}
              >
                {step.icon}
              </div>

              {/* Content */}
              <div className="flex-1 pt-1">
                <div className="flex items-baseline gap-3 mb-1">
                  <span
                    className="text-xs font-mono"
                    style={{ color: step.color, fontFamily: "'JetBrains Mono', monospace" }}
                  >
                    {step.number}
                  </span>
                  <h3
                    className="text-base sm:text-lg font-semibold"
                    style={{
                      fontFamily: "'JetBrains Mono', monospace",
                      color: 'var(--text-primary)',
                    }}
                  >
                    {step.title}
                  </h3>
                </div>
                <p
                  className="text-xs mb-2"
                  style={{
                    fontFamily: "'JetBrains Mono', monospace",
                    color: step.color,
                    opacity: 0.7,
                  }}
                >
                  {step.subtitle}
                </p>
                <p className="text-sm leading-relaxed mb-2" style={{ color: 'var(--text-secondary)' }}>
                  {step.description}
                </p>
                <p
                  className="text-[10px] px-2 py-1 rounded inline-block"
                  style={{
                    fontFamily: "'JetBrains Mono', monospace",
                    backgroundColor: `${step.color}10`,
                    color: step.color,
                    border: `1px solid ${step.color}30`,
                  }}
                >
                  {step.cpnLabel}
                </p>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
