import { useTranslation } from 'react-i18next';
import { useInView } from './useInView';

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

export function WhatIsSection() {
  const { ref, inView } = useInView();
  const { t } = useTranslation('landing');

  return (
    <section
      id="what-is"
      ref={ref as React.RefObject<HTMLElement>}
      className={`py-24 sm:py-32 px-4 sm:px-8 landing-reveal ${inView ? 'in-view' : ''}`}
      style={{ backgroundColor: 'var(--bg-deep)' }}
    >
      <div className="max-w-4xl mx-auto text-center">
        <p
          className="text-xs uppercase tracking-[0.2em] mb-4"
          style={{ color: 'var(--accent)', fontFamily: "'JetBrains Mono', monospace" }}
        >
          {t('whatIs.eyebrow')}
        </p>

        <h2
          className="text-2xl sm:text-3xl lg:text-4xl font-semibold tracking-tight mb-8"
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            color: 'var(--text-primary)',
          }}
        >
          {renderAccentText(t('whatIs.title'))}
        </h2>

        <p
          className="text-base sm:text-lg leading-relaxed max-w-3xl mx-auto mb-12"
          style={{ color: 'var(--text-secondary)' }}
        >
          {t('whatIs.description')}
        </p>

        {/* Three pillars */}
        <div className={`grid grid-cols-1 sm:grid-cols-3 gap-6 stagger-children ${inView ? 'in-view' : ''}`}>
          <div
            className="rounded-xl p-6 border"
            style={{ backgroundColor: 'var(--bg-surface)', borderColor: 'var(--border-dim)' }}
          >
            <div className="w-10 h-10 rounded-lg flex items-center justify-center mb-4 mx-auto" style={{ backgroundColor: 'rgba(14,165,233,0.1)' }}>
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" style={{ color: 'var(--accent)' }}>
                <circle cx="12" cy="12" r="3" fill="currentColor" />
                <path d="M12 2v4M12 18v4M2 12h4M18 12h4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              </svg>
            </div>
            <h3 className="font-semibold text-sm mb-2" style={{ fontFamily: "'JetBrains Mono', monospace", color: 'var(--text-primary)' }}>
              {t('whatIs.pillars.transparent.title')}
            </h3>
            <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
              {t('whatIs.pillars.transparent.description')}
            </p>
          </div>

          <div
            className="rounded-xl p-6 border"
            style={{ backgroundColor: 'var(--bg-surface)', borderColor: 'var(--border-dim)' }}
          >
            <div className="w-10 h-10 rounded-lg flex items-center justify-center mb-4 mx-auto" style={{ backgroundColor: 'rgba(167,139,250,0.1)' }}>
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" style={{ color: '#a78bfa' }}>
                <path d="M9 12l2 2 4-4" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="1.5" />
              </svg>
            </div>
            <h3 className="font-semibold text-sm mb-2" style={{ fontFamily: "'JetBrains Mono', monospace", color: 'var(--text-primary)' }}>
              {t('whatIs.pillars.auditable.title')}
            </h3>
            <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
              {t('whatIs.pillars.auditable.description')}
            </p>
          </div>

          <div
            className="rounded-xl p-6 border"
            style={{ backgroundColor: 'var(--bg-surface)', borderColor: 'var(--border-dim)' }}
          >
            <div className="w-10 h-10 rounded-lg flex items-center justify-center mb-4 mx-auto" style={{ backgroundColor: 'rgba(16,185,129,0.1)' }}>
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" style={{ color: '#10b981' }}>
                <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
                <circle cx="9" cy="7" r="4" stroke="currentColor" strokeWidth="1.5" />
                <path d="M22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              </svg>
            </div>
            <h3 className="font-semibold text-sm mb-2" style={{ fontFamily: "'JetBrains Mono', monospace", color: 'var(--text-primary)' }}>
              {t('whatIs.pillars.humanFirst.title')}
            </h3>
            <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
              {t('whatIs.pillars.humanFirst.description')}
            </p>
          </div>
        </div>
      </div>
    </section>
  );
}
