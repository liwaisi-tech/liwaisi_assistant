import { useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { LANGUAGE_STORAGE_KEY } from '../../../i18n/config';

interface LanguageStepProps {
  t: TFunction;
  selectedLanguage: string;
  onSelect: (lang: string) => void;
}

const LANGUAGES = [
  { code: 'en', flag: 'EN' },
  { code: 'es', flag: 'ES' },
] as const;

export function LanguageStep({ t, selectedLanguage, onSelect }: LanguageStepProps) {
  const { i18n } = useTranslation();

  const handleSelect = useCallback(
    (code: string) => {
      onSelect(code);
      i18n.changeLanguage(code);
      localStorage.setItem(LANGUAGE_STORAGE_KEY, code);
    },
    [onSelect, i18n],
  );

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
          {t('language.title')}
        </h2>
        <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
          {t('language.description')}
        </p>
      </div>

      <div className="grid grid-cols-2 gap-4">
        {LANGUAGES.map(({ code, flag }) => {
          const isSelected = selectedLanguage === code;
          return (
            <button
              key={code}
              onClick={() => handleSelect(code)}
              className="flex flex-col items-center gap-3 p-5 rounded-xl border transition-all duration-200 cursor-pointer text-left"
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
                className="text-lg font-bold tracking-wider"
                style={{
                  fontFamily: "'JetBrains Mono', monospace",
                  color: isSelected ? 'var(--accent)' : 'var(--text-muted)',
                }}
              >
                {flag}
              </span>
              <span
                className="text-sm font-medium"
                style={{ color: 'var(--text-primary)' }}
              >
                {t(`language.${code}.name`)}
              </span>
              <span
                className="text-xs text-center"
                style={{ color: 'var(--text-secondary)' }}
              >
                {t(`language.${code}.greeting`)}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}
