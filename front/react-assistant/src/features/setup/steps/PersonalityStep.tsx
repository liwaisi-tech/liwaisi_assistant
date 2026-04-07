import type { TFunction } from 'i18next';
import type { PersonalityPreset } from '../../../types/setup';

interface PersonalityStepProps {
  t: TFunction;
  selected: PersonalityPreset;
  onSelect: (preset: PersonalityPreset) => void;
}

const PRESETS: { key: 'balanced' | 'creative' | 'precise'; indicator: string }[] = [
  { key: 'balanced', indicator: '=' },
  { key: 'creative', indicator: '~' },
  { key: 'precise', indicator: '#' },
];

export function PersonalityStep({ t, selected, onSelect }: PersonalityStepProps) {
  return (
    <div
      className="flex flex-col gap-6 py-4"
      style={{ animation: 'welcome-fade 0.4s ease-out forwards' }}
    >
      <div className="text-center flex flex-col gap-2">
        <h2
          className="text-xl font-semibold tracking-tight"
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            color: 'var(--text-primary)',
          }}
        >
          {t('personality.title')}
        </h2>
        <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
          {t('personality.description')}
        </p>
      </div>

      <div className="flex flex-col gap-3">
        {PRESETS.map(({ key, indicator }) => {
          const isSelected = selected === key;
          return (
            <button
              key={key}
              onClick={() => onSelect(key)}
              className="flex items-start gap-4 p-4 rounded-xl border transition-all duration-200 cursor-pointer text-left"
              style={{
                backgroundColor: isSelected
                  ? 'rgba(14, 165, 233, 0.06)'
                  : 'var(--bg-input)',
                borderColor: isSelected ? 'var(--accent)' : 'var(--border-dim)',
                boxShadow: isSelected
                  ? '0 0 20px -4px var(--accent-glow)'
                  : 'none',
              }}
            >
              <span
                className="w-8 h-8 rounded-lg flex items-center justify-center shrink-0 text-sm font-bold"
                style={{
                  fontFamily: "'JetBrains Mono', monospace",
                  backgroundColor: isSelected
                    ? 'rgba(14, 165, 233, 0.15)'
                    : 'rgba(255, 255, 255, 0.04)',
                  color: isSelected ? 'var(--accent)' : 'var(--text-muted)',
                  border: `1px solid ${isSelected ? 'rgba(14, 165, 233, 0.3)' : 'var(--border-dim)'}`,
                }}
              >
                {indicator}
              </span>
              <div className="flex flex-col gap-1">
                <span
                  className="text-sm font-medium"
                  style={{ color: 'var(--text-primary)' }}
                >
                  {t(`personality.${key}.name`)}
                </span>
                <span className="text-xs" style={{ color: 'var(--text-secondary)' }}>
                  {t(`personality.${key}.description`)}
                </span>
              </div>
            </button>
          );
        })}
      </div>

      {/* Customize later option */}
      <button
        onClick={() => onSelect('custom')}
        className="text-xs cursor-pointer transition-colors duration-200 self-center"
        style={{
          color: selected === 'custom' ? 'var(--accent)' : 'var(--text-muted)',
          textDecoration: selected === 'custom' ? 'underline' : 'none',
        }}
      >
        {t('personality.custom')}
      </button>
    </div>
  );
}
