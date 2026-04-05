import { useTranslation } from 'react-i18next';
import { useInView } from './useInView';

const PRINCIPLE_DEFS = [
  { emoji: '\u{1F9E0}', key: 'humanFirst' as const },
  { emoji: '\u{1F41A}', key: 'territorialKnowledge' as const },
  { emoji: '\u{1F30D}', key: 'positiveImpact' as const },
  { emoji: '\u{1F932}', key: 'coCreation' as const },
  { emoji: '\u{1F331}', key: 'techSovereignty' as const },
  { emoji: '\u{2764}\u{FE0F}', key: 'socialJustice' as const },
];

function renderAccentText(raw: string) {
  const match = raw.match(/^(.*)<accent>(.*)<\/accent>(.*)$/);
  if (!match) return <>{raw}</>;
  return (
    <>
      {match[1]}
      <span style={{ color: '#10b981' }}>{match[2]}</span>
      {match[3]}
    </>
  );
}

export function MissionSection() {
  const { ref, inView } = useInView();
  const { t } = useTranslation('landing');

  return (
    <section
      id="mission"
      ref={ref as React.RefObject<HTMLElement>}
      className={`py-24 sm:py-32 px-4 sm:px-8 landing-reveal ${inView ? 'in-view' : ''}`}
      style={{ backgroundColor: 'var(--bg-surface)' }}
    >
      <div className="max-w-5xl mx-auto">
        <div className="text-center mb-16">
          <p
            className="text-xs uppercase tracking-[0.2em] mb-4"
            style={{ color: '#10b981', fontFamily: "'JetBrains Mono', monospace" }}
          >
            {t('mission.eyebrow')}
          </p>
          <h2
            className="text-2xl sm:text-3xl lg:text-4xl font-semibold tracking-tight mb-6"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              color: 'var(--text-primary)',
            }}
          >
            {renderAccentText(t('mission.title'))}
          </h2>
          <p
            className="text-base sm:text-lg leading-relaxed max-w-3xl mx-auto"
            style={{ color: 'var(--text-secondary)' }}
          >
            {t('mission.description')}
          </p>
        </div>

        {/* Quote block */}
        <div
          className="rounded-xl p-8 mb-16 border-l-4 max-w-3xl mx-auto"
          style={{
            backgroundColor: 'rgba(16,185,129,0.05)',
            borderLeftColor: '#10b981',
          }}
        >
          <blockquote
            className="text-sm sm:text-base italic leading-relaxed"
            style={{ color: 'var(--text-secondary)' }}
          >
            {t('mission.quote')}
          </blockquote>
          <p
            className="mt-4 text-xs"
            style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
          >
            &mdash; {t('mission.quoteAttribution')}
          </p>
        </div>

        {/* Principles grid */}
        <div className={`grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 stagger-children ${inView ? 'in-view' : ''}`}>
          {PRINCIPLE_DEFS.map((p) => (
            <div
              key={p.key}
              className="rounded-lg p-4 border flex gap-3"
              style={{
                backgroundColor: 'var(--bg-deep)',
                borderColor: 'var(--border-dim)',
              }}
            >
              <span className="text-xl shrink-0" role="img" aria-hidden="true">
                {p.emoji}
              </span>
              <div>
                <h3
                  className="font-semibold text-sm mb-1"
                  style={{
                    fontFamily: "'JetBrains Mono', monospace",
                    color: 'var(--text-primary)',
                  }}
                >
                  {t(`mission.principles.${p.key}.title`)}
                </h3>
                <p className="text-xs leading-relaxed" style={{ color: 'var(--text-secondary)' }}>
                  {t(`mission.principles.${p.key}.description`)}
                </p>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
