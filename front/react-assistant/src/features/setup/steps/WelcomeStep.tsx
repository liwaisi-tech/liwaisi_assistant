import type { TFunction } from 'i18next';

interface WelcomeStepProps {
  t: TFunction;
  onNext: () => void;
}

export function WelcomeStep({ t, onNext }: WelcomeStepProps) {
  return (
    <div
      className="flex flex-col items-center gap-8 py-6"
      style={{ animation: 'welcome-fade 0.4s ease-out forwards' }}
    >
      {/* Logo */}
      <div
        className="w-16 h-16 rounded-2xl flex items-center justify-center"
        style={{
          backgroundColor: 'rgba(14, 165, 233, 0.08)',
          border: '1px solid rgba(14, 165, 233, 0.15)',
          boxShadow: '0 0 24px -4px var(--accent-glow)',
        }}
      >
        <span
          className="text-xl font-bold"
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            color: 'var(--accent)',
          }}
        >
          brae
        </span>
      </div>

      {/* Text */}
      <div className="text-center flex flex-col gap-3">
        <h2
          className="text-2xl font-semibold tracking-tight"
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            color: 'var(--text-primary)',
          }}
        >
          {t('welcome.title')}
        </h2>
        <p
          className="text-sm max-w-sm mx-auto"
          style={{ color: 'var(--text-secondary)' }}
        >
          {t('welcome.description')}
        </p>
      </div>

      {/* CTA */}
      <button
        onClick={onNext}
        className="px-8 py-3 rounded-lg font-medium text-sm transition-all duration-200 cursor-pointer hover:shadow-lg"
        style={{
          backgroundColor: 'var(--accent)',
          color: '#0a0a0f',
          boxShadow: '0 0 16px var(--accent-glow)',
        }}
      >
        {t('welcome.cta')}
      </button>
    </div>
  );
}
