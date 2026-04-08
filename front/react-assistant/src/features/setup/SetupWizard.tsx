import { useState, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { useNamespace } from '../../i18n/useNamespace';
import {
  defaultVariantForLanguage,
  type PersonalityPreset,
  type RegionalVariant,
} from '../../types/setup';
import { WelcomeStep } from './steps/WelcomeStep';
import { LanguageStep } from './steps/LanguageStep';
import { RegionStep } from './steps/RegionStep';
import { ModelStep } from './steps/ModelStep';
import { PersonalityStep } from './steps/PersonalityStep';
import { ReadyStep } from './steps/ReadyStep';

interface SetupWizardProps {
  onComplete: () => void;
}

interface WizardState {
  language: string;
  regionalVariant: RegionalVariant;
  model: string;
  modelOverrides: Record<string, string>;
  personalityPreset: PersonalityPreset;
}

const TOTAL_STEPS = 6;
const READY_STEP = 5;

const STEP_KEYS = [
  'step1',
  'step2',
  'step3',
  'step4',
  'step5',
  'step6',
] as const;

export function SetupWizard({ onComplete }: SetupWizardProps) {
  const { t } = useNamespace('setup');
  const { i18n } = useTranslation();
  const [step, setStep] = useState(0);
  const [state, setState] = useState<WizardState>(() => {
    const lang = (i18n.resolvedLanguage || i18n.language || 'es').split('-')[0];
    return {
      language: lang,
      regionalVariant: defaultVariantForLanguage(lang),
      model: '',
      modelOverrides: {},
      personalityPreset: '',
    };
  });

  const goNext = useCallback(() => setStep((s) => Math.min(s + 1, TOTAL_STEPS - 1)), []);
  const goBack = useCallback(() => setStep((s) => Math.max(s - 1, 0)), []);

  const handleSkip = useCallback(() => {
    onComplete();
  }, [onComplete]);

  const handleLanguageSelect = useCallback((lang: string) => {
    setState((prev) =>
      prev.language === lang
        ? prev
        : {
            ...prev,
            language: lang,
            regionalVariant: defaultVariantForLanguage(lang),
          },
    );
  }, []);

  const handleVariantSelect = useCallback((variant: RegionalVariant) => {
    setState((prev) =>
      prev.regionalVariant === variant
        ? prev
        : { ...prev, regionalVariant: variant },
    );
  }, []);

  const handleModelSelect = useCallback((model: string) => {
    setState((prev) => ({ ...prev, model }));
  }, []);

  const handleOverridesSet = useCallback((overrides: Record<string, string>) => {
    setState((prev) => ({ ...prev, modelOverrides: overrides }));
  }, []);

  const handlePersonalitySelect = useCallback((preset: PersonalityPreset) => {
    setState((prev) => ({ ...prev, personalityPreset: preset }));
  }, []);

  return (
    <div
      className="fixed inset-0 flex items-center justify-center px-4"
      style={{ backgroundColor: 'var(--bg-deep)' }}
    >
      <div
        className="w-full relative rounded-2xl border overflow-hidden"
        style={{
          maxWidth: 600,
          backgroundColor: 'rgba(18, 18, 26, 0.92)',
          backdropFilter: 'blur(20px)',
          WebkitBackdropFilter: 'blur(20px)',
          borderColor: 'var(--border-dim)',
          boxShadow:
            '0 0 0 1px var(--border-dim), 0 0 48px -12px var(--accent-glow), 0 32px 64px -24px rgba(0,0,0,0.8)',
        }}
      >
        {/* Top accent line */}
        <div
          className="h-px w-full"
          style={{
            background: 'linear-gradient(90deg, transparent, var(--accent), transparent)',
          }}
        />

        {/* Progress indicator */}
        <div className="flex items-center justify-center gap-2 pt-6 px-6">
          {STEP_KEYS.map((key, i) => (
            <div key={key} className="flex items-center gap-2">
              <div className="flex flex-col items-center gap-1">
                <div
                  className="w-2.5 h-2.5 rounded-full transition-all duration-300"
                  style={{
                    backgroundColor:
                      i < step
                        ? 'var(--accent)'
                        : i === step
                          ? 'var(--accent)'
                          : 'var(--border-dim)',
                    boxShadow:
                      i === step ? '0 0 8px var(--accent-glow)' : 'none',
                    opacity: i <= step ? 1 : 0.4,
                  }}
                />
                <span
                  className="text-[9px] uppercase tracking-wider hidden sm:block"
                  style={{
                    fontFamily: "'JetBrains Mono', monospace",
                    color:
                      i <= step ? 'var(--text-secondary)' : 'var(--text-muted)',
                  }}
                >
                  {t(`progress.${key}`)}
                </span>
              </div>
              {i < TOTAL_STEPS - 1 && (
                <div
                  className="w-8 h-px hidden sm:block"
                  style={{
                    backgroundColor:
                      i < step ? 'var(--accent)' : 'var(--border-dim)',
                    opacity: i < step ? 0.6 : 0.3,
                  }}
                />
              )}
            </div>
          ))}
        </div>

        {/* Step content */}
        <div className="px-6 sm:px-10 pb-2">
          {step === 0 && <WelcomeStep t={t} onNext={goNext} />}
          {step === 1 && (
            <LanguageStep
              t={t}
              selectedLanguage={state.language}
              onSelect={handleLanguageSelect}
            />
          )}
          {step === 2 && (
            <RegionStep
              t={t}
              language={state.language}
              selectedVariant={state.regionalVariant}
              onSelect={handleVariantSelect}
            />
          )}
          {step === 3 && (
            <ModelStep
              t={t}
              selectedModel={state.model}
              modelOverrides={state.modelOverrides}
              onSelectModel={handleModelSelect}
              onSetOverrides={handleOverridesSet}
            />
          )}
          {step === 4 && (
            <PersonalityStep
              t={t}
              selected={state.personalityPreset}
              onSelect={handlePersonalitySelect}
            />
          )}
          {step === 5 && (
            <ReadyStep
              t={t}
              language={state.language}
              regionalVariant={state.regionalVariant}
              model={state.model}
              modelOverrides={state.modelOverrides}
              personalityPreset={state.personalityPreset}
              onComplete={onComplete}
            />
          )}
        </div>

        {/* Navigation */}
        <div className="flex items-center justify-between px-6 sm:px-10 pb-6 pt-2">
          <div>
            {step > 0 && step < READY_STEP && (
              <button
                onClick={goBack}
                className="px-4 py-2 rounded-lg text-xs font-medium transition-colors duration-200 cursor-pointer"
                style={{
                  color: 'var(--text-secondary)',
                  backgroundColor: 'transparent',
                  border: '1px solid var(--border-dim)',
                }}
              >
                {t('back')}
              </button>
            )}
          </div>

          <div className="flex items-center gap-4">
            {step < READY_STEP && (
              <button
                onClick={handleSkip}
                className="text-xs cursor-pointer transition-colors duration-200"
                style={{ color: 'var(--text-muted)' }}
              >
                {t('skip')}
              </button>
            )}
            {step > 0 && step < READY_STEP && (
              <button
                onClick={goNext}
                className="px-6 py-2 rounded-lg text-xs font-medium transition-all duration-200 cursor-pointer hover:shadow-lg"
                style={{
                  backgroundColor: 'var(--accent)',
                  color: '#0a0a0f',
                  boxShadow: '0 0 12px var(--accent-glow)',
                }}
              >
                {t('next')}
              </button>
            )}
          </div>
        </div>

        {/* Bottom accent line */}
        <div
          className="h-px w-full"
          style={{
            background:
              'linear-gradient(90deg, transparent, rgba(16,185,129,0.4), transparent)',
          }}
        />
      </div>
    </div>
  );
}
