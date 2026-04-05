import { useTranslation } from 'react-i18next';
import { useInView } from './useInView';

const STEP_DEFS = [
  {
    number: '01',
    key: 'step01' as const,
    icon: (
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
        <circle cx="12" cy="12" r="9" stroke="#10b981" strokeWidth="1.5" />
        <circle cx="12" cy="12" r="3" fill="#10b981" />
      </svg>
    ),
    color: '#10b981',
  },
  {
    number: '02',
    key: 'step02' as const,
    icon: (
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
        <rect x="3" y="6" width="18" height="12" rx="2" stroke="#0ea5e9" strokeWidth="1.5" />
        <path d="M8 12h8M12 9l3 3-3 3" stroke="#0ea5e9" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    ),
    color: '#0ea5e9',
  },
  {
    number: '03',
    key: 'step03' as const,
    icon: (
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
        <circle cx="12" cy="12" r="9" stroke="#a78bfa" strokeWidth="1.5" />
        <path d="M9 9v6M15 9v6" stroke="#a78bfa" strokeWidth="2" strokeLinecap="round" />
      </svg>
    ),
    color: '#a78bfa',
  },
  {
    number: '04',
    key: 'step04' as const,
    icon: (
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
        <circle cx="12" cy="12" r="9" stroke="#10b981" strokeWidth="1.5" />
        <path d="M8 12l3 3 5-5" stroke="#10b981" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    ),
    color: '#10b981',
  },
];

function renderAccentText(raw: string) {
  const match = raw.match(/^(.*)<accent>(.*)<\/accent>(.*)$/);
  if (!match) return <>{raw}</>;
  return (
    <>
      {match[1]}
      <span className="gradient-text">{match[2]}</span>
      {match[3]}
    </>
  );
}

export function HowItWorksSection() {
  const { ref, inView } = useInView();
  const { t } = useTranslation('landing');

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
            {t('howItWorks.eyebrow')}
          </p>
          <h2
            className="text-2xl sm:text-3xl lg:text-4xl font-semibold tracking-tight"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              color: 'var(--text-primary)',
            }}
          >
            {renderAccentText(t('howItWorks.title'))}
          </h2>
        </div>

        {/* Steps timeline */}
        <div className={`relative stagger-children ${inView ? 'in-view' : ''}`}>
          {/* Vertical line */}
          <div
            className="absolute left-6 sm:left-8 top-0 bottom-0 w-px hidden sm:block"
            style={{ background: 'linear-gradient(to bottom, var(--accent), #a78bfa, #10b981)' }}
          />

          {STEP_DEFS.map((step) => (
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
                    {t(`howItWorks.steps.${step.key}.title` as const)}
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
                  {t(`howItWorks.steps.${step.key}.subtitle` as const)}
                </p>
                <p className="text-sm leading-relaxed mb-2" style={{ color: 'var(--text-secondary)' }}>
                  {t(`howItWorks.steps.${step.key}.description` as const)}
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
                  {t(`howItWorks.steps.${step.key}.cpnLabel` as const)}
                </p>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
