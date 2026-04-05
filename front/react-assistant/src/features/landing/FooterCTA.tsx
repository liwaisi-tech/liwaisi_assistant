import { useTranslation } from 'react-i18next';
import { WaitlistForm } from './WaitlistForm';
import { useInView } from './useInView';

interface FooterCTAProps {
  onSignIn: () => void;
}

/* ── Trust stats (social proof) ─────────────────────────────────── */

const STAT_KEYS = [
  { key: 'cpn' as const, accent: 'var(--accent)' },
  { key: 'blocks' as const, accent: '#a78bfa' },
  { key: 'hitl' as const, accent: '#10b981' },
  { key: 'b2b' as const, accent: '#f59e0b' },
];

/* ── Footer link columns ────────────────────────────────────────── */

const FOOTER_COL_DEFS = [
  {
    titleKey: 'footer.columns.product.title' as const,
    links: [
      { labelKey: 'footer.columns.product.about' as const, href: '#what-is' },
      { labelKey: 'footer.columns.product.howItWorks' as const, href: '#how-it-works' },
      { labelKey: 'footer.columns.product.features' as const, href: '#features' },
      { labelKey: 'footer.columns.product.ourMission' as const, href: '#mission' },
    ],
  },
  {
    titleKey: 'footer.columns.community.title' as const,
    links: [
      { labelKey: 'footer.columns.community.liwaisiTech' as const, href: 'https://liwaisi.tech', external: true },
      { labelKey: 'footer.columns.community.youtubeChannel' as const, href: 'https://www.youtube.com/@LiwaisiTech', external: true },
    ],
  },
  {
    titleKey: 'footer.columns.resources.title' as const,
    links: [
      { labelKey: 'footer.columns.resources.edtechWorkshops' as const, href: 'https://liwaisi.tech', external: true },
      { labelKey: 'footer.columns.resources.videoTutorials' as const, href: 'https://www.youtube.com/@LiwaisiTech', external: true },
      { labelKey: 'footer.columns.resources.softwareDevelopment' as const, href: 'https://www.youtube.com/@LiwaisiTech', external: true },
    ],
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

export function FooterCTA({ onSignIn }: FooterCTAProps) {
  const { t } = useTranslation('landing');
  const { ref: ctaRef, inView: ctaInView } = useInView();
  const { ref: footerRef, inView: footerInView } = useInView(0.1);

  return (
    <>
      {/* ── CTA Section ───────────────────────────────────────────── */}
      <section
        id="get-access"
        ref={ctaRef as React.RefObject<HTMLElement>}
        className={`relative py-24 sm:py-32 px-4 sm:px-8 overflow-hidden landing-reveal ${ctaInView ? 'in-view' : ''}`}
        style={{ backgroundColor: 'var(--bg-deep)' }}
      >
        {/* Background radial gradient */}
        <div
          className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[800px] h-[600px] pointer-events-none"
          style={{
            background: 'radial-gradient(ellipse, var(--accent-glow) 0%, transparent 60%)',
            opacity: 0.15,
          }}
        />

        <div className="relative z-10 max-w-4xl mx-auto">
          {/* Headline */}
          <div className="text-center mb-12">
            <p
              className="text-xs uppercase tracking-[0.2em] mb-4"
              style={{ color: 'var(--accent)', fontFamily: "'JetBrains Mono', monospace" }}
            >
              {t('footerCta.eyebrow')}
            </p>
            <h2
              className="text-2xl sm:text-3xl lg:text-4xl font-semibold tracking-tight mb-4"
              style={{
                fontFamily: "'JetBrains Mono', monospace",
                color: 'var(--text-primary)',
              }}
            >
              {renderAccentText(t('footerCta.title'))}
            </h2>
            <p
              className="text-base sm:text-lg leading-relaxed max-w-2xl mx-auto"
              style={{ color: 'var(--text-secondary)' }}
            >
              {t('footerCta.description')}
            </p>
          </div>

          {/* Waitlist form — centered, prominent */}
          <div className="max-w-md mx-auto mb-6">
            <WaitlistForm variant="hero" />
          </div>

          <p className="text-center text-xs mb-16" style={{ color: 'var(--text-muted)' }}>
            {t('footerCta.alreadyHaveAccess')}{' '}
            <button
              onClick={onSignIn}
              className="cursor-pointer underline underline-offset-2 transition-colors"
              style={{ color: 'var(--accent)', background: 'none', border: 'none', font: 'inherit' }}
            >
              {t('footerCta.signIn')}
            </button>
          </p>

          {/* Trust stats */}
          <div
            className="rounded-xl border p-6 sm:p-8"
            style={{
              backgroundColor: 'var(--bg-surface)',
              borderColor: 'var(--border-dim)',
            }}
          >
            <div className={`grid grid-cols-2 sm:grid-cols-4 gap-6 sm:gap-8 ${ctaInView ? '' : ''}`}>
              {STAT_KEYS.map((stat) => (
                <div key={stat.key} className="stat-item text-center">
                  <p
                    className="text-2xl sm:text-3xl font-bold tracking-tight mb-1"
                    style={{
                      fontFamily: "'JetBrains Mono', monospace",
                      color: stat.accent,
                    }}
                  >
                    {t(`footerCta.stats.${stat.key}.value`)}
                  </p>
                  <p
                    className="text-[10px] sm:text-xs uppercase tracking-wider"
                    style={{
                      fontFamily: "'JetBrains Mono', monospace",
                      color: 'var(--text-muted)',
                    }}
                  >
                    {t(`footerCta.stats.${stat.key}.label`)}
                  </p>
                </div>
              ))}
            </div>
          </div>
        </div>
      </section>

      {/* ── Footer ────────────────────────────────────────────────── */}
      <footer
        ref={footerRef as React.RefObject<HTMLElement>}
        className={`px-4 sm:px-8 pt-16 pb-8 border-t landing-reveal ${footerInView ? 'in-view' : ''}`}
        style={{
          backgroundColor: 'var(--bg-surface)',
          borderColor: 'var(--border-dim)',
        }}
      >
        <div className="max-w-6xl mx-auto">
          {/* Top: Logo column + Link columns */}
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-5 gap-10 sm:gap-8 mb-12">
            {/* Brand column — spans 2 on lg */}
            <div className="lg:col-span-2">
              <div className="flex items-center gap-3 mb-4">
                <img
                  src="/liwaisi_logo_dark_bg.svg"
                  alt="Liwaisi"
                  className="h-8 w-auto"
                />
                <span
                  className="text-sm font-semibold"
                  style={{
                    fontFamily: "'JetBrains Mono', monospace",
                    color: 'var(--text-primary)',
                  }}
                >
                  Liwaisi Tech
                </span>
              </div>
              <p
                className="text-sm leading-relaxed mb-6 max-w-xs"
                style={{ color: 'var(--text-secondary)' }}
              >
                {t('footer.brandDescription')}
              </p>

              {/* Social links */}
              <div className="flex items-center gap-3">
                <a
                  href="https://liwaisi.tech"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="w-9 h-9 rounded-lg border flex items-center justify-center transition-all duration-200"
                  style={{
                    borderColor: 'var(--border-dim)',
                    color: 'var(--text-muted)',
                  }}
                  onMouseEnter={(e) => {
                    e.currentTarget.style.borderColor = 'var(--accent)';
                    e.currentTarget.style.color = 'var(--accent)';
                    e.currentTarget.style.boxShadow = '0 0 12px -2px var(--accent-glow)';
                  }}
                  onMouseLeave={(e) => {
                    e.currentTarget.style.borderColor = 'var(--border-dim)';
                    e.currentTarget.style.color = 'var(--text-muted)';
                    e.currentTarget.style.boxShadow = 'none';
                  }}
                  aria-label={t('footer.websiteAriaLabel')}
                >
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none">
                    <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="1.5" />
                    <path d="M2 12h20M12 2c2.5 2.5 4 5.5 4 10s-1.5 7.5-4 10c-2.5-2.5-4-5.5-4-10s1.5-7.5 4-10z" stroke="currentColor" strokeWidth="1.5" />
                  </svg>
                </a>
                <a
                  href="https://www.youtube.com/@LiwaisiTech"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="w-9 h-9 rounded-lg border flex items-center justify-center transition-all duration-200"
                  style={{
                    borderColor: 'var(--border-dim)',
                    color: 'var(--text-muted)',
                  }}
                  onMouseEnter={(e) => {
                    e.currentTarget.style.borderColor = '#ef4444';
                    e.currentTarget.style.color = '#ef4444';
                    e.currentTarget.style.boxShadow = '0 0 12px -2px rgba(239,68,68,0.3)';
                  }}
                  onMouseLeave={(e) => {
                    e.currentTarget.style.borderColor = 'var(--border-dim)';
                    e.currentTarget.style.color = 'var(--text-muted)';
                    e.currentTarget.style.boxShadow = 'none';
                  }}
                  aria-label={t('footer.youtubeAriaLabel')}
                >
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none">
                    <rect x="2" y="4" width="20" height="16" rx="4" stroke="currentColor" strokeWidth="1.5" />
                    <path d="M10 9l5 3-5 3V9z" fill="currentColor" />
                  </svg>
                </a>
              </div>
            </div>

            {/* Link columns */}
            {FOOTER_COL_DEFS.map((col) => (
              <div key={col.titleKey}>
                <h3
                  className="text-[10px] uppercase tracking-[0.15em] mb-4"
                  style={{
                    fontFamily: "'JetBrains Mono', monospace",
                    color: 'var(--text-muted)',
                  }}
                >
                  {t(col.titleKey)}
                </h3>
                <ul className="flex flex-col gap-2.5">
                  {col.links.map((link) => (
                    <li key={link.labelKey}>
                      <a
                        href={link.href}
                        {...('external' in link && link.external
                          ? { target: '_blank', rel: 'noopener noreferrer' }
                          : {})}
                        className="footer-link text-sm"
                        style={{ color: 'var(--text-secondary)' }}
                        onMouseEnter={(e) => (e.currentTarget.style.color = 'var(--accent)')}
                        onMouseLeave={(e) => (e.currentTarget.style.color = 'var(--text-secondary)')}
                      >
                        {t(link.labelKey)}
                        {'external' in link && link.external && (
                          <svg
                            width="10"
                            height="10"
                            viewBox="0 0 12 12"
                            fill="none"
                            className="inline-block ml-1 opacity-40"
                            style={{ verticalAlign: 'baseline' }}
                          >
                            <path d="M5 1h6v6M11 1L4.5 7.5" stroke="currentColor" strokeWidth="1.2" strokeLinecap="round" strokeLinejoin="round" />
                          </svg>
                        )}
                      </a>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>

          {/* Bottom bar */}
          <div
            className="pt-6 border-t flex flex-col sm:flex-row items-center justify-between gap-4"
            style={{ borderColor: 'var(--border-dim)' }}
          >
            <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
              &copy; {new Date().getFullYear()} {t('footer.copyright')}
            </p>

            {/* "Built with CPN" badge */}
            <div
              className="flex items-center gap-2 px-3 py-1.5 rounded-full border"
              style={{
                borderColor: 'var(--border-dim)',
                backgroundColor: 'rgba(14, 165, 233, 0.04)',
              }}
            >
              {/* Mini CPN icon: place → transition → place */}
              <svg width="36" height="12" viewBox="0 0 36 12" fill="none" aria-hidden="true">
                <circle cx="4" cy="6" r="3" stroke="var(--accent)" strokeWidth="1" opacity="0.6" />
                <circle cx="4" cy="6" r="1" fill="var(--accent)" opacity="0.8" />
                <line x1="8" y1="6" x2="13" y2="6" stroke="var(--accent)" strokeWidth="0.7" opacity="0.4" />
                <rect x="13" y="3" width="10" height="6" rx="1" stroke="var(--accent)" strokeWidth="1" opacity="0.6" />
                <line x1="23" y1="6" x2="28" y2="6" stroke="var(--accent)" strokeWidth="0.7" opacity="0.4" />
                <circle cx="32" cy="6" r="3" stroke="#10b981" strokeWidth="1" opacity="0.6" />
                <circle cx="32" cy="6" r="1" fill="#10b981" opacity="0.8" />
              </svg>
              <span
                className="text-[9px] uppercase tracking-wider"
                style={{
                  fontFamily: "'JetBrains Mono', monospace",
                  color: 'var(--text-muted)',
                }}
              >
                {t('footer.builtWith')}
              </span>
            </div>
          </div>
        </div>
      </footer>
    </>
  );
}
