import { useState, useCallback } from 'react';
import type { TFunction } from 'i18next';
import { completeOnboarding } from '../../../services/api';
import type { PersonalityPreset } from '../../../types/setup';

interface ReadyStepProps {
  t: TFunction;
  language: string;
  model: string;
  modelOverrides: Record<string, string>;
  personalityPreset: PersonalityPreset;
  onComplete: () => void;
}

export function ReadyStep({
  t,
  language,
  model,
  modelOverrides,
  personalityPreset,
  onComplete,
}: ReadyStepProps) {
  const [saving, setSaving] = useState(false);

  const handleFinish = useCallback(async () => {
    setSaving(true);
    try {
      await completeOnboarding({
        preferred_language: language,
        preferred_model: model,
        model_overrides: modelOverrides,
        personality_preset: personalityPreset,
      });
      onComplete();
    } catch {
      // If saving fails, still let the user proceed
      onComplete();
    }
  }, [language, model, modelOverrides, personalityPreset, onComplete]);

  const personalityLabel =
    personalityPreset && personalityPreset !== 'custom'
      ? t(`personality.${personalityPreset}.name`)
      : personalityPreset === 'custom'
        ? t('personality.custom')
        : '--';

  return (
    <div
      className="flex flex-col items-center gap-6 py-6"
      style={{ animation: 'welcome-fade 0.4s ease-out forwards' }}
    >
      {/* Checkmark */}
      <div
        className="w-16 h-16 rounded-full flex items-center justify-center"
        style={{
          backgroundColor: 'rgba(16, 185, 129, 0.1)',
          border: '2px solid rgba(16, 185, 129, 0.4)',
          boxShadow: '0 0 24px -4px rgba(16, 185, 129, 0.3)',
        }}
      >
        <svg width="32" height="32" viewBox="0 0 32 32" fill="none">
          <path
            d="M10 16.5l4 4 8-8"
            stroke="#10b981"
            strokeWidth="2.5"
            strokeLinecap="round"
            strokeLinejoin="round"
            className="check-animate"
          />
        </svg>
      </div>

      <div className="text-center flex flex-col gap-2">
        <h2
          className="text-xl font-semibold tracking-tight"
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            color: 'var(--text-primary)',
          }}
        >
          {t('ready.title')}
        </h2>
        <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
          {t('ready.description')}
        </p>
      </div>

      {/* Summary */}
      <div
        className="w-full rounded-xl border p-4 flex flex-col gap-3"
        style={{
          backgroundColor: 'var(--bg-input)',
          borderColor: 'var(--border-dim)',
        }}
      >
        <p
          className="text-xs font-medium uppercase tracking-wider"
          style={{
            color: 'var(--text-muted)',
            fontFamily: "'JetBrains Mono', monospace",
          }}
        >
          {t('ready.summary')}
        </p>
        <div className="flex flex-col gap-2">
          <div className="flex justify-between items-center">
            <span className="text-xs" style={{ color: 'var(--text-secondary)' }}>
              {t('ready.language_label')}
            </span>
            <span
              className="text-xs font-medium"
              style={{
                color: 'var(--text-primary)',
                fontFamily: "'JetBrains Mono', monospace",
              }}
            >
              {language === 'es' ? 'Espanol' : 'English'}
            </span>
          </div>
          <div
            className="h-px w-full"
            style={{ backgroundColor: 'var(--border-dim)' }}
          />
          <div className="flex justify-between items-center">
            <span className="text-xs" style={{ color: 'var(--text-secondary)' }}>
              {t('ready.model_label')}
            </span>
            <span
              className="text-xs font-medium"
              style={{
                color: 'var(--text-primary)',
                fontFamily: "'JetBrains Mono', monospace",
              }}
            >
              {model || '--'}
            </span>
          </div>
          <div
            className="h-px w-full"
            style={{ backgroundColor: 'var(--border-dim)' }}
          />
          <div className="flex justify-between items-center">
            <span className="text-xs" style={{ color: 'var(--text-secondary)' }}>
              {t('ready.personality_label')}
            </span>
            <span
              className="text-xs font-medium"
              style={{
                color: 'var(--text-primary)',
                fontFamily: "'JetBrains Mono', monospace",
              }}
            >
              {personalityLabel}
            </span>
          </div>
        </div>
      </div>

      {/* CTA */}
      <button
        onClick={handleFinish}
        disabled={saving}
        className="px-8 py-3 rounded-lg font-medium text-sm transition-all duration-200 cursor-pointer hover:shadow-lg disabled:opacity-50 disabled:cursor-not-allowed"
        style={{
          backgroundColor: 'var(--accent)',
          color: '#0a0a0f',
          boxShadow: '0 0 16px var(--accent-glow)',
        }}
      >
        {saving ? (
          <span className="flex items-center gap-2">
            <svg className="animate-spin h-4 w-4" viewBox="0 0 24 24" fill="none">
              <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" opacity="0.3" />
              <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
            </svg>
            {t('ready.saving')}
          </span>
        ) : (
          t('ready.cta')
        )}
      </button>
    </div>
  );
}
