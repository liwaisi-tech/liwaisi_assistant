import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { loadNamespace } from '../../i18n/loadNamespace';
import { getUserProfile, getModels, updatePreferences, ApiError } from '../../services/api';
import {
  REGIONAL_VARIANTS,
  defaultVariantForLanguage,
  type ModelRole,
  type ModelRegistryEntry,
} from '../../types/setup';

interface FormState {
  language: string;
  regionalVariant: string;
  model: string;
  modelOverrides: Record<string, string>;
}

const EMPTY_FORM: FormState = {
  language: '',
  regionalVariant: '',
  model: '',
  modelOverrides: {},
};

function formsEqual(a: FormState, b: FormState): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}

function baseLangOf(code: string): 'en' | 'es' {
  return (code || '').split('-')[0] === 'en' ? 'en' : 'es';
}

export function SettingsPage() {
  const { t } = useTranslation(['settings', 'setup', 'common']);

  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [savedFlash, setSavedFlash] = useState(false);

  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [baseline, setBaseline] = useState<FormState>(EMPTY_FORM);

  const [roles, setRoles] = useState<ModelRole[]>([]);
  const [availableModels, setAvailableModels] = useState<string[]>([]);
  const [defaultModel, setDefaultModel] = useState('');
  const [registry, setRegistry] = useState<ModelRegistryEntry[]>([]);
  const [showAdvanced, setShowAdvanced] = useState(false);

  const flashTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    loadNamespace('settings');
    loadNamespace('setup');
  }, []);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setLoadError(false);
    Promise.all([getUserProfile(), getModels()])
      .then(([profile, models]) => {
        if (cancelled) return;
        const prefs = profile.preferences;
        const lang = prefs.preferred_language || 'es';
        const variant = prefs.regional_variant || defaultVariantForLanguage(lang);
        const next: FormState = {
          language: lang,
          regionalVariant: variant,
          model: prefs.preferred_model || '',
          modelOverrides: { ...(prefs.model_overrides || {}) },
        };
        setForm(next);
        setBaseline(next);
        setDefaultModel(models.default_model);
        setAvailableModels(models.available_models ?? [models.default_model]);
        setRoles(models.roles ?? []);
        setRegistry(models.registry ?? []);
        setLoading(false);
      })
      .catch(() => {
        if (cancelled) return;
        setLoadError(true);
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    return () => {
      if (flashTimerRef.current) clearTimeout(flashTimerRef.current);
    };
  }, []);

  const hasOverrides = Object.keys(form.modelOverrides).length > 0;
  const isDirty = !formsEqual(form, baseline);

  const regionGroup = REGIONAL_VARIANTS[baseLangOf(form.language)];

  const modelInList = !form.model || availableModels.includes(form.model);

  const handleLanguageChange = useCallback((lang: string) => {
    setForm((prev) => {
      const base = baseLangOf(lang);
      const currentBase = baseLangOf(prev.regionalVariant || prev.language);
      const nextVariant = base === currentBase
        ? prev.regionalVariant
        : defaultVariantForLanguage(lang);
      return { ...prev, language: lang, regionalVariant: nextVariant };
    });
  }, []);

  const handleVariantChange = useCallback((variant: string) => {
    setForm((prev) => ({ ...prev, regionalVariant: variant }));
  }, []);

  const handleModelChange = useCallback((model: string) => {
    setForm((prev) => ({ ...prev, model }));
  }, []);

  const handleOverrideChange = useCallback((roleKey: string, roleDefault: string, value: string) => {
    setForm((prev) => {
      const next = { ...prev.modelOverrides };
      if (!value || value === roleDefault) {
        delete next[roleKey];
      } else {
        next[roleKey] = value;
      }
      return { ...prev, modelOverrides: next };
    });
  }, []);

  const handleResetOverrides = useCallback(() => {
    setForm((prev) => ({ ...prev, modelOverrides: {} }));
  }, []);

  const handleToggleAdvanced = useCallback(() => {
    setShowAdvanced((p) => !p);
  }, []);

  const handleSave = useCallback(async () => {
    setSaving(true);
    setSaveError(null);
    try {
      await updatePreferences({
        preferred_language: form.language,
        regional_variant: form.regionalVariant,
        preferred_model: form.model,
        model_overrides: form.modelOverrides,
      });
      setBaseline(form);
      setSavedFlash(true);
      if (flashTimerRef.current) clearTimeout(flashTimerRef.current);
      flashTimerRef.current = setTimeout(() => setSavedFlash(false), 3000);
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : t('settings:saveError');
      setSaveError(msg || t('settings:saveError'));
    } finally {
      setSaving(false);
    }
  }, [form, t]);

  const saveDisabled = !isDirty || saving || loading || loadError;

  const modelSelectValue = useMemo(() => {
    // If current model not in available list, we still want to show it selected.
    return form.model;
  }, [form.model]);

  // Registry lookup keyed by registry_id so the pickers can render
  // display_name + vendor + context length instead of raw slugs when the
  // backend exposes the DB-backed registry
  // (spec-architecture-model-registry-and-a2ui-management.md §F2).
  const registryByID = useMemo(() => {
    const map = new Map<string, ModelRegistryEntry>();
    for (const entry of registry) map.set(entry.registry_id, entry);
    return map;
  }, [registry]);

  const formatModelOption = useCallback(
    (registryID: string): string => {
      const entry = registryByID.get(registryID);
      if (!entry) return registryID;
      const ctx = entry.context?.length
        ? ` · ${Math.round(entry.context.length / 1000)}k`
        : '';
      return `${entry.display_name} — ${entry.vendor}${ctx}`;
    },
    [registryByID],
  );

  return (
    <div
      className="flex flex-col h-full overflow-y-auto"
      style={{ backgroundColor: 'var(--bg-deep)' }}
    >
      <div className="w-full max-w-2xl mx-auto px-6 py-8 flex flex-col gap-6">
        <header className="flex flex-col gap-1">
          <h1
            className="text-xl font-semibold tracking-tight"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              color: 'var(--text-primary)',
            }}
          >
            {t('settings:title')}
          </h1>
          <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
            {t('settings:subtitle')}
          </p>
        </header>

        {loading && (
          <div className="flex items-center justify-center py-10">
            <svg className="animate-spin h-6 w-6" viewBox="0 0 24 24" fill="none" style={{ color: 'var(--accent)' }}>
              <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" opacity="0.3" />
              <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
            </svg>
            <span className="ml-3 text-sm" style={{ color: 'var(--text-muted)' }}>
              {t('settings:loadingMessage')}
            </span>
          </div>
        )}

        {!loading && loadError && (
          <div
            className="rounded-lg border px-4 py-3 text-sm"
            style={{
              borderColor: 'var(--border-dim)',
              backgroundColor: 'var(--bg-input)',
              color: 'var(--text-secondary)',
              fontFamily: "'JetBrains Mono', monospace",
            }}
          >
            {t('settings:errorMessage')}
          </div>
        )}

        {!loading && !loadError && (
          <div
            className="glass-surface rounded-xl border p-6 flex flex-col gap-6"
            style={{
              borderColor: 'var(--border-dim)',
              backgroundColor: 'var(--bg-surface)',
            }}
          >
            {/* Language */}
            <div className="flex flex-col gap-2">
              <label
                className="text-xs font-medium uppercase tracking-wider"
                style={{
                  fontFamily: "'JetBrains Mono', monospace",
                  color: 'var(--text-secondary)',
                }}
              >
                {t('settings:languageLabel')}
              </label>
              <select
                value={form.language}
                onChange={(e) => handleLanguageChange(e.target.value)}
                className="w-full px-4 py-3 rounded-lg border text-sm outline-none transition-colors duration-200 cursor-pointer"
                style={{
                  backgroundColor: 'var(--bg-input)',
                  borderColor: 'var(--border-dim)',
                  color: 'var(--text-primary)',
                  fontFamily: "'JetBrains Mono', monospace",
                }}
              >
                <option value="es">{t('settings:languageEs')}</option>
                <option value="en">{t('settings:languageEn')}</option>
              </select>
              <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
                {t('settings:languageHint')}
              </p>
            </div>

            {/* Regional variant */}
            <div className="flex flex-col gap-2">
              <label
                className="text-xs font-medium uppercase tracking-wider"
                style={{
                  fontFamily: "'JetBrains Mono', monospace",
                  color: 'var(--text-secondary)',
                }}
              >
                {t('settings:regionLabel')}
              </label>
              <select
                value={form.regionalVariant}
                onChange={(e) => handleVariantChange(e.target.value)}
                className="w-full px-4 py-3 rounded-lg border text-sm outline-none transition-colors duration-200 cursor-pointer"
                style={{
                  backgroundColor: 'var(--bg-input)',
                  borderColor: 'var(--border-dim)',
                  color: 'var(--text-primary)',
                  fontFamily: "'JetBrains Mono', monospace",
                }}
              >
                {regionGroup.variants.map((v) => (
                  <option key={v.code} value={v.code}>
                    {v.label}
                  </option>
                ))}
              </select>
              <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
                {t('settings:regionHint')}
              </p>
            </div>

            {/* Model picker */}
            <div className="flex flex-col gap-2">
              <div className="flex items-center justify-between gap-2">
                <label
                  className="text-xs font-medium uppercase tracking-wider"
                  style={{
                    fontFamily: "'JetBrains Mono', monospace",
                    color: 'var(--text-secondary)',
                  }}
                >
                  {t('settings:modelLabel')}
                </label>
                <span
                  className="inline-flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] uppercase tracking-wider border"
                  style={{
                    fontFamily: "'JetBrains Mono', monospace",
                    color: hasOverrides ? 'var(--accent)' : 'var(--text-secondary)',
                    borderColor: hasOverrides ? 'var(--accent)' : 'var(--border-dim)',
                    backgroundColor: 'transparent',
                  }}
                >
                  <span
                    className="w-1.5 h-1.5 rounded-full"
                    style={{
                      backgroundColor: hasOverrides ? 'var(--accent)' : 'var(--text-secondary)',
                      boxShadow: hasOverrides ? '0 0 6px var(--accent-glow)' : 'none',
                    }}
                  />
                  {hasOverrides ? t('setup:model.modeAdvanced') : t('setup:model.modeSimple')}
                </span>
              </div>
              <select
                value={modelSelectValue}
                onChange={(e) => handleModelChange(e.target.value)}
                className="w-full px-4 py-3 rounded-lg border text-sm outline-none transition-colors duration-200 cursor-pointer"
                style={{
                  backgroundColor: 'var(--bg-input)',
                  borderColor: 'var(--border-dim)',
                  color: 'var(--text-primary)',
                  fontFamily: "'JetBrains Mono', monospace",
                }}
              >
                <option value="">
                  {t('settings:useSystemDefault')} ({formatModelOption(defaultModel)})
                </option>
                {!modelInList && form.model && (
                  <option value={form.model}>{formatModelOption(form.model)}</option>
                )}
                {availableModels.map((model) => (
                  <option key={model} value={model}>
                    {formatModelOption(model)}
                  </option>
                ))}
              </select>
              <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
                {t('setup:model.appliesToAll')}
              </p>
              {!modelInList && form.model && (
                <p className="text-xs" style={{ color: 'var(--accent)' }}>
                  {t('settings:staleModelWarning')}
                </p>
              )}

              {roles.length > 0 && (
                <div className="flex flex-col gap-3 mt-2">
                  <button
                    type="button"
                    onClick={handleToggleAdvanced}
                    className="flex items-center gap-2 text-xs cursor-pointer transition-colors duration-200 self-start"
                    style={{ color: 'var(--text-muted)' }}
                  >
                    <svg
                      width="12"
                      height="12"
                      viewBox="0 0 12 12"
                      fill="none"
                      style={{
                        transform: showAdvanced ? 'rotate(90deg)' : 'rotate(0deg)',
                        transition: 'transform 0.2s ease',
                      }}
                    >
                      <path d="M4 2l4 4-4 4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
                    </svg>
                    {t('settings:advancedLabel')}
                  </button>

                  {showAdvanced && (
                    <div className="flex flex-col gap-3 pl-4 max-h-[40vh] overflow-y-auto pr-2">
                      {hasOverrides && (
                        <button
                          type="button"
                          onClick={handleResetOverrides}
                          className="self-start px-3 py-1 rounded-lg text-[11px] font-medium transition-colors duration-200 cursor-pointer border"
                          style={{
                            color: 'var(--text-secondary)',
                            borderColor: 'var(--border-dim)',
                            backgroundColor: 'transparent',
                            fontFamily: "'JetBrains Mono', monospace",
                          }}
                        >
                          {t('setup:model.resetToSingle')}
                        </button>
                      )}
                      {roles.map((role) => (
                        <div key={role.key} className="flex flex-col gap-1">
                          <label
                            className="text-xs font-medium"
                            style={{
                              color: 'var(--text-secondary)',
                              fontFamily: "'JetBrains Mono', monospace",
                            }}
                          >
                            {role.label}
                          </label>
                          <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
                            {role.description}
                          </p>
                          <select
                            value={form.modelOverrides[role.key] ?? role.default_model}
                            onChange={(e) => handleOverrideChange(role.key, role.default_model, e.target.value)}
                            className="w-full px-3 py-2 rounded-lg border text-xs outline-none cursor-pointer"
                            style={{
                              backgroundColor: 'var(--bg-input)',
                              borderColor: 'var(--border-dim)',
                              color: 'var(--text-primary)',
                              fontFamily: "'JetBrains Mono', monospace",
                            }}
                          >
                            {availableModels.map((model) => (
                              <option key={model} value={model}>
                                {formatModelOption(model)}
                              </option>
                            ))}
                          </select>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </div>

            {/* Save row */}
            <div className="flex items-center justify-between gap-3 pt-2 border-t" style={{ borderColor: 'var(--border-dim)' }}>
              <div className="flex-1 min-h-[1.25rem] text-xs" aria-live="polite">
                {savedFlash && (
                  <span style={{ color: 'var(--accent)', fontFamily: "'JetBrains Mono', monospace" }}>
                    {t('settings:savedToast')}
                  </span>
                )}
                {saveError && (
                  <span style={{ color: '#f87171', fontFamily: "'JetBrains Mono', monospace" }}>
                    {saveError}
                  </span>
                )}
              </div>
              <button
                type="button"
                onClick={handleSave}
                disabled={saveDisabled}
                className="px-4 py-2 rounded-lg text-xs font-medium transition-colors duration-200 border"
                style={{
                  backgroundColor: saveDisabled ? 'transparent' : 'var(--accent)',
                  color: saveDisabled ? 'var(--text-muted)' : 'var(--bg-deep)',
                  borderColor: saveDisabled ? 'var(--border-dim)' : 'var(--accent)',
                  cursor: saveDisabled ? 'not-allowed' : 'pointer',
                  fontFamily: "'JetBrains Mono', monospace",
                }}
              >
                {saving ? t('settings:savingButton') : t('settings:saveButton')}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
