import { useInView } from './useInView';

const PRINCIPLES = [
  { emoji: '\u{1F9E0}', title: 'Human First', description: 'Technology at the service of people, not the other way around.' },
  { emoji: '\u{1F41A}', title: 'Territorial Knowledge', description: 'We value traditional wisdom and build from it, not over it.' },
  { emoji: '\u{1F30D}', title: 'Positive Impact', description: 'Tech solutions that improve lives, care for the planet, and generate income.' },
  { emoji: '\u{1F932}', title: 'Co-creation', description: 'Everything built in dialogue with people and communities.' },
  { emoji: '\u{1F331}', title: 'Tech Sovereignty', description: 'Supporting autonomy to cultivate and to create your own technology.' },
  { emoji: '\u{2764}\u{FE0F}', title: 'Social Justice', description: 'Businesses that reduce inequalities, not deepen them.' },
];

export function MissionSection() {
  const { ref, inView } = useInView();

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
            Our Mission
          </p>
          <h2
            className="text-2xl sm:text-3xl lg:text-4xl font-semibold tracking-tight mb-6"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              color: 'var(--text-primary)',
            }}
          >
            Technology as a{' '}
            <span style={{ color: '#10b981' }}>seed of justice</span>
          </h2>
          <p
            className="text-base sm:text-lg leading-relaxed max-w-3xl mx-auto"
            style={{ color: 'var(--text-secondary)' }}
          >
            We bring technology to the field as a tool to sow opportunities,
            strengthen community knowledge, and care for the land while growing together.
            We create and teach technology made with communities, so that rural people
            can live well, pursue their ideas, and defend their right to stay on their
            land with pride, dignity, and progress.
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
            "We imagine a territory where learning technology is as common as farming,
            and where our communities are leaders of a new rurality — prosperous,
            creative, and just."
          </blockquote>
          <p
            className="mt-4 text-xs"
            style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
          >
            &mdash; Liwaisi 10-Year Vision
          </p>
        </div>

        {/* Principles grid */}
        <div className={`grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 stagger-children ${inView ? 'in-view' : ''}`}>
          {PRINCIPLES.map((p) => (
            <div
              key={p.title}
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
                  {p.title}
                </h3>
                <p className="text-xs leading-relaxed" style={{ color: 'var(--text-secondary)' }}>
                  {p.description}
                </p>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
