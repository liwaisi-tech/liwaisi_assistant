import { useState, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import type { ConfigItem } from '../../types/admin';

interface SecretFieldProps {
  item: ConfigItem;
  onSave: (key: string, value: string) => Promise<void>;
  onDelete: (key: string) => Promise<void>;
}

const sourceBadgeStyles: Record<string, { bg: string; color: string }> = {
  env: { bg: 'rgba(59, 130, 246, 0.15)', color: '#3b82f6' },
  db: { bg: 'rgba(34, 197, 94, 0.15)', color: '#22c55e' },
  default: { bg: 'rgba(148, 163, 184, 0.15)', color: '#94a3b8' },
};

export function SecretField({ item, onSave, onDelete }: SecretFieldProps) {
  const { t } = useTranslation('admin');
  const [value, setValue] = useState(item.value);
  const [revealed, setRevealed] = useState(false);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [success, setSuccess] = useState<string | null>(null);

  const isModified = value !== item.value;
  const isLocked = item.source === 'env';
  const badge = sourceBadgeStyles[item.source] ?? sourceBadgeStyles.default;

  const handleSave = useCallback(async () => {
    setSaving(true);
    setError(null);
    setSuccess(null);
    try {
      await onSave(item.key, value);
      setSuccess(t('field.saved'));
      setTimeout(() => setSuccess(null), 2000);
    } catch {
      setError(t('field.error'));
    } finally {
      setSaving(false);
    }
  }, [item.key, value, onSave, t]);

  const handleDelete = useCallback(async () => {
    setDeleting(true);
    setError(null);
    setSuccess(null);
    try {
      await onDelete(item.key);
      setSuccess(t('field.deleted'));
      setTimeout(() => setSuccess(null), 2000);
    } catch {
      setError(t('field.error'));
    } finally {
      setDeleting(false);
    }
  }, [item.key, onDelete, t]);

  return (
    <div
      className="rounded-lg p-3 space-y-2"
      style={{
        backgroundColor: 'var(--bg-surface)',
        border: '1px solid var(--border-dim)',
      }}
    >
      {/* Header row: label + source badge */}
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 min-w-0">
          <span
            className="text-xs font-medium truncate"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              color: 'var(--text-primary)',
            }}
          >
            {item.label}
          </span>
          <span
            className="text-[9px] font-semibold px-1.5 py-0.5 rounded-full shrink-0 uppercase tracking-wider"
            style={{
              backgroundColor: badge.bg,
              color: badge.color,
              fontFamily: "'JetBrains Mono', monospace",
            }}
          >
            {t(`source.${item.source}`)}
          </span>
        </div>
        <span
          className="text-[10px] shrink-0"
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            color: 'var(--text-muted)',
          }}
        >
          {item.key}
        </span>
      </div>

      {/* Input row */}
      <div className="flex items-center gap-2">
        <div className="relative flex-1">
          <input
            type={item.is_secret && !revealed ? 'password' : 'text'}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            disabled={isLocked || saving}
            className="w-full rounded-md px-3 py-1.5 text-xs outline-none transition-colors"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              backgroundColor: isLocked ? 'rgba(148, 163, 184, 0.08)' : 'var(--bg-input)',
              border: '1px solid var(--border-dim)',
              color: isLocked ? 'var(--text-muted)' : 'var(--text-primary)',
              cursor: isLocked ? 'not-allowed' : 'text',
            }}
            title={isLocked ? t('field.locked') : undefined}
          />
          {/* Reveal toggle for secrets */}
          {item.is_secret && (
            <button
              onClick={() => setRevealed((r) => !r)}
              className="absolute right-2 top-1/2 -translate-y-1/2 transition-colors"
              style={{ color: 'var(--text-muted)' }}
              aria-label={revealed ? t('field.hide') : t('field.reveal')}
              title={revealed ? t('field.hide') : t('field.reveal')}
            >
              {revealed ? (
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M17.94 17.94A10.07 10.07 0 0112 20c-7 0-11-8-11-8a18.45 18.45 0 015.06-5.94" />
                  <path d="M9.9 4.24A9.12 9.12 0 0112 4c7 0 11 8 11 8a18.5 18.5 0 01-2.16 3.19" />
                  <line x1="1" y1="1" x2="23" y2="23" />
                </svg>
              ) : (
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
                  <circle cx="12" cy="12" r="3" />
                </svg>
              )}
            </button>
          )}
        </div>

        {/* Save button — only when modified */}
        {isModified && !isLocked && (
          <button
            onClick={handleSave}
            disabled={saving}
            className="shrink-0 px-3 py-1.5 rounded-md text-[11px] font-medium transition-colors"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              backgroundColor: saving ? 'rgba(14, 165, 233, 0.1)' : 'rgba(14, 165, 233, 0.15)',
              color: 'var(--accent)',
            }}
          >
            {saving ? t('field.saving') : t('field.save')}
          </button>
        )}

        {/* Delete button — only for db source */}
        {item.source === 'db' && (
          <button
            onClick={handleDelete}
            disabled={deleting}
            className="shrink-0 px-3 py-1.5 rounded-md text-[11px] font-medium transition-colors"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              backgroundColor: deleting ? 'rgba(239, 68, 68, 0.08)' : 'rgba(239, 68, 68, 0.12)',
              color: '#ef4444',
            }}
          >
            {deleting ? '...' : t('field.delete')}
          </button>
        )}
      </div>

      {/* Locked tooltip */}
      {isLocked && (
        <p className="text-[10px]" style={{ color: 'var(--text-muted)' }}>
          {t('field.locked')}
        </p>
      )}

      {/* Inline feedback */}
      {error && (
        <p className="text-[10px]" style={{ color: '#ef4444' }}>
          {error}
        </p>
      )}
      {success && (
        <p className="text-[10px]" style={{ color: '#22c55e' }}>
          {success}
        </p>
      )}
    </div>
  );
}
