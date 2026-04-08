import { useState, useEffect } from 'react';
import type { TFunction } from 'i18next';
import { getModels } from '../../../services/api';
import type { ModelRole } from '../../../types/setup';

interface ModelStepProps {
  t: TFunction;
  selectedModel: string;
  modelOverrides: Record<string, string>;
  onSelectModel: (model: string) => void;
  onSetOverrides: (overrides: Record<string, string>) => void;
}

export function ModelStep({
  t,
  selectedModel,
  modelOverrides,
  onSelectModel,
  onSetOverrides,
}: ModelStepProps) {
  const [roles, setRoles] = useState<ModelRole[]>([]);
  const [defaultModel, setDefaultModel] = useState('');
  const [availableModels, setAvailableModels] = useState<string[]>([]);
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    getModels()
      .then((res) => {
        if (cancelled) return;
        setDefaultModel(res.default_model);
        setRoles(res.roles);
        setAvailableModels(res.available_models ?? [res.default_model]);
        if (!selectedModel) {
          onSelectModel(res.default_model);
        }
        setLoading(false);
      })
      .catch(() => {
        if (cancelled) return;
        setError(true);
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

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
          {t('model.title')}
        </h2>
        <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
          {t('model.description')}
        </p>
      </div>

      {loading && (
        <div className="flex justify-center py-8">
          <svg className="animate-spin h-6 w-6" viewBox="0 0 24 24" fill="none" style={{ color: 'var(--accent)' }}>
            <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" opacity="0.3" />
            <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
          </svg>
        </div>
      )}

      {error && (
        <p className="text-center text-sm" style={{ color: 'var(--text-muted)' }}>
          {t('model.keepDefaults')}
        </p>
      )}

      {!loading && !error && (
        <>
          {/* Default model selection */}
          <div className="flex flex-col gap-2">
            <select
              value={selectedModel || defaultModel}
              onChange={(e) => onSelectModel(e.target.value)}
              className="w-full px-4 py-3 rounded-lg border text-sm outline-none transition-colors duration-200 cursor-pointer"
              style={{
                backgroundColor: 'var(--bg-input)',
                borderColor: 'var(--border-dim)',
                color: 'var(--text-primary)',
                fontFamily: "'JetBrains Mono', monospace",
              }}
            >
              {availableModels.map((model) => (
                <option key={model} value={model}>
                  {model}
                </option>
              ))}
            </select>
            <p className="text-xs" style={{ color: 'var(--text-muted)' }}>
              {t('model.appliesToAll')}
            </p>
          </div>

          {/* Advanced toggle */}
          {roles.length > 0 && (
            <div className="flex flex-col gap-3">
              <button
                onClick={() => setShowAdvanced(!showAdvanced)}
                className="flex items-center gap-2 text-xs cursor-pointer transition-colors duration-200"
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
                {t('model.advanced')}
              </button>

              {showAdvanced && (
                <div className="flex flex-col gap-3 pl-4">
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
                        value={modelOverrides[role.key] ?? role.default_model}
                        onChange={(e) => {
                          const next = { ...modelOverrides };
                          if (e.target.value === role.default_model) {
                            delete next[role.key];
                          } else {
                            next[role.key] = e.target.value;
                          }
                          onSetOverrides(next);
                        }}
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
                            {model}
                          </option>
                        ))}
                      </select>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );
}
