import { CPNAnimation } from './CPNAnimation';
import { WaitlistForm } from './WaitlistForm';

export function HeroSection() {
  return (
    <section
      id="hero"
      className="relative min-h-dvh flex flex-col items-center justify-center px-4 sm:px-8 overflow-hidden hero-grid"
      style={{ backgroundColor: 'var(--bg-deep)' }}
    >
      {/* Scan-line overlay */}
      <div className="scan-lines absolute inset-0 pointer-events-none" />

      {/* Radial glow behind logo */}
      <div
        className="absolute top-1/4 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[600px] h-[600px] rounded-full pointer-events-none"
        style={{
          background: 'radial-gradient(circle, var(--accent-glow) 0%, transparent 70%)',
          opacity: 0.3,
        }}
      />

      <div className="relative z-10 flex flex-col items-center gap-8 max-w-4xl mx-auto text-center">
        {/* Logo */}
        <img
          src="/liwaisi_logo_dark_bg.svg"
          alt="Liwaisi"
          className="h-16 sm:h-20 w-auto"
        />

        {/* Tagline */}
        <div className="flex flex-col gap-3">
          <h1
            className="text-3xl sm:text-4xl lg:text-5xl font-semibold tracking-tight leading-tight"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              color: 'var(--text-primary)',
            }}
          >
            AI that works like{' '}
            <span className="gradient-text">your best team</span>
          </h1>
          <p
            className="text-base sm:text-lg max-w-2xl mx-auto leading-relaxed"
            style={{ color: 'var(--text-secondary)' }}
          >
            Transparent, auditable, and always under your control.
            Powered by Coloured Petri Nets. Built for rural innovation.
          </p>
        </div>

        {/* CPN Animation */}
        <div className="w-full max-w-xl">
          <CPNAnimation />
        </div>

        {/* Waitlist CTA */}
        <div className="w-full max-w-md">
          <WaitlistForm variant="hero" />
          <p className="mt-2 text-xs" style={{ color: 'var(--text-muted)' }}>
            Join our early access program. B2B only — we'll reach out personally.
          </p>
        </div>
      </div>

      {/* Scroll indicator */}
      <div className="absolute bottom-8 left-1/2 -translate-x-1/2 scroll-indicator">
        <svg width="24" height="24" viewBox="0 0 24 24" fill="none" style={{ color: 'var(--text-muted)' }}>
          <path d="M12 5v14M5 12l7 7 7-7" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
      </div>
    </section>
  );
}
