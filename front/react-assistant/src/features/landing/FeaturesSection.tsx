import { useTranslation } from 'react-i18next';
import { useInView } from './useInView';

const FEATURE_DEFS = [
  {
    key: 'cpnEngine',
    icon: (
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
        <circle cx="5" cy="12" r="3" stroke="currentColor" strokeWidth="1.5" />
        <circle cx="19" cy="6" r="3" stroke="currentColor" strokeWidth="1.5" />
        <circle cx="19" cy="18" r="3" stroke="currentColor" strokeWidth="1.5" />
        <path d="M7.5 10.5L16.5 7M7.5 13.5L16.5 17" stroke="currentColor" strokeWidth="1.5" />
      </svg>
    ),
    color: 'var(--accent)',
  },
  {
    key: 'hitl',
    icon: (
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
        <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="1.5" />
        <path d="M9 9v6M15 9v6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" />
      </svg>
    ),
    color: '#a78bfa',
  },
  {
    key: 'personality',
    icon: (
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
        <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2z" stroke="currentColor" strokeWidth="1.5" />
        <path d="M12 8v4l3 3" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
        <circle cx="12" cy="12" r="2" fill="currentColor" />
      </svg>
    ),
    color: '#f59e0b',
  },
  {
    key: 'monitoring',
    icon: (
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
        <rect x="3" y="3" width="18" height="18" rx="2" stroke="currentColor" strokeWidth="1.5" />
        <path d="M3 9h18M9 3v18" stroke="currentColor" strokeWidth="1.5" />
        <circle cx="15" cy="15" r="2" fill="currentColor" />
      </svg>
    ),
    color: '#10b981',
  },
  {
    key: 'tools',
    icon: (
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
        <path d="M14.7 6.3a1 1 0 000 1.4l1.6 1.6a1 1 0 001.4 0l3.77-3.77a6 6 0 01-7.94 7.94l-6.91 6.91a2.12 2.12 0 01-3-3l6.91-6.91a6 6 0 017.94-7.94l-3.76 3.76z" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    ),
    color: '#ec4899',
  },
  {
    key: 'flowIntelligence',
    icon: (
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
        <path d="M12 2l3.09 6.26L22 9.27l-5 4.87L18.18 21 12 17.27 5.82 21 7 14.14l-5-4.87 6.91-1.01L12 2z" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
    ),
    color: '#06b6d4',
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

export function FeaturesSection() {
  const { ref, inView } = useInView();
  const { t } = useTranslation('landing');

  return (
    <section
      id="features"
      ref={ref as React.RefObject<HTMLElement>}
      className={`py-24 sm:py-32 px-4 sm:px-8 landing-reveal ${inView ? 'in-view' : ''}`}
      style={{ backgroundColor: 'var(--bg-deep)' }}
    >
      <div className="max-w-6xl mx-auto">
        <div className="text-center mb-16">
          <p
            className="text-xs uppercase tracking-[0.2em] mb-4"
            style={{ color: 'var(--accent)', fontFamily: "'JetBrains Mono', monospace" }}
          >
            {t('features.eyebrow')}
          </p>
          <h2
            className="text-2xl sm:text-3xl lg:text-4xl font-semibold tracking-tight"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              color: 'var(--text-primary)',
            }}
          >
            {renderAccentText(t('features.title'))}
          </h2>
        </div>

        <div className={`grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 sm:gap-6 stagger-children ${inView ? 'in-view' : ''}`}>
          {FEATURE_DEFS.map((feature) => (
            <div
              key={feature.key}
              className="feature-card rounded-xl p-6 border cursor-default"
              style={{
                backgroundColor: 'var(--bg-surface)',
                borderColor: 'var(--border-dim)',
              }}
            >
              <div
                className="w-10 h-10 rounded-lg flex items-center justify-center mb-4"
                style={{
                  backgroundColor: `${feature.color}15`,
                  color: feature.color,
                }}
              >
                {feature.icon}
              </div>
              <h3
                className="font-semibold text-sm mb-2"
                style={{
                  fontFamily: "'JetBrains Mono', monospace",
                  color: 'var(--text-primary)',
                }}
              >
                {t(`features.${feature.key}.title`)}
              </h3>
              <p className="text-sm leading-relaxed" style={{ color: 'var(--text-secondary)' }}>
                {t(`features.${feature.key}.description`)}
              </p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
