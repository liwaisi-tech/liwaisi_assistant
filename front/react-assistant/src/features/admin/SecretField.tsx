import { useState, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import type { ConfigItem } from '../../types/admin';

interface SecretFieldProps {
  item: ConfigItem;
  onSave: (key: string, value: string) => Promise<void>;
  onDelete: (key: string) => Promise<void>;
}

const sourceBadgeStyles: Record<string, { bg: string; text: string; label: string }> = {
  env: { bg: 'rgba(100, 116, 139, 0.2)', text: 'var(--text-muted)', label: 'ENV' },
  db: { bg: 'rgba(34, 197, 94, 0.15)', text: '#22c55e', label: 'DB' },
  default: { bg: 'rgba(100, 116, 139, 0.1)', text: 'var(--text-muted)', label: 'DEFAULT' },
};

export function SecretField({ item, onSave, onDelete }: SecretFieldProps) {
  const { t } = useTranslation('admin');
  const [value, setValue] = useState('');
  const [revealed, setRevealed] = useState(false);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const isLocked = item.source === 'env';
  const hasDbValue = item.source === 'db';
  const badge = sourceBadgeStyles[item.source] ?? sourceBadgeStyles.default;

  const handleSave = useCallback(async () => {
    if (!value.trim()) return;
    setSaving(true);
    try {
      await onSave(item.key, value);
      setValue('');
    } finally {
      setSaving(false);
    }
  }, [item.key, value, onSave]);

  const handleDelete = useCallback(async () => {
    setDeleting(true);
    try {
      await onDelete(item.key);
    } finally {
      setDeleting(false);
    }
  }, [item.key, onDelete]);

  return (
    <div
      className="rounded-lg p-3"
      style={{
        backgroundColor: 'var(--bg-surface)',
        border: '1px solid var(--border-dim)',
      }}
    >
      {/* Header row */}
      <div className="flex items-center gap-2 mb-2">
        <span
          className="text-xs font-medium"
          style={{ fontFamily: "'JetBrains Mono', monospace", color: 'var(--text-primary)' }}
        >
          {item.label}
        </span>
        {item.required && (
          <span className="text-[10px]" style={{ color: 'var(--accent)' }}>
            {t('field.required')}
          </span>
        )}
        <span
          className="text-[9px] px-1.5 py-0.5 rounded font-medium"
          style={{ backgroundColor: badge.bg, color: badge.text }}
        >
          {badge.label}
        </span>
        {isLocked && (
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" style={{ color: 'var(--text-muted)' }}>
            <rect x="3" y="11" width="18" height="11" rx="2" ry="2" />
            <path d="M7 11V7a5 5 0 0110 0v4" />
          </svg>
        )}
      </div>

      {/* Current value display */}
      {item.value && (
        <div className="mb-2">
          <span
            className="text-[11px] break-all"
            style={{ fontFamily: "'JetBrains Mono', monospace", color: 'var(--text-secondary)' }}
          >
            {item.is_secret && !revealed ? item.value : item.value}
          </span>
          {item.is_secret && item.source !== 'default' && (
            <button
              onClick={() => setRevealed((r) => !r)}
              className="ml-2 text-[10px] underline"
              style={{ color: 'var(--accent)' }}
            >
              {revealed ? t('field.hide') : t('field.reveal')}
            </button>
          )}
        </div>
      )}

      {/* Input + actions (disabled when locked by env) */}
      {!isLocked && (
        <div className="flex gap-2 items-center">
          <input
            type={item.is_secret ? 'password' : 'text'}
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder={t('field.placeholder')}
            className="flex-1 text-xs px-2 py-1.5 rounded outline-none"
            style={{
              fontFamily: "'JetBrains Mono', monospace",
              backgroundColor: 'var(--bg-deep)',
              border: '1px solid var(--border-dim)',
              color: 'var(--text-primary)',
            }}
            onKeyDown={(e) => { if (e.key === 'Enter') handleSave(); }}
          />
          <button
            onClick={handleSave}
            disabled={saving || !value.trim()}
            className="text-[11px] px-3 py-1.5 rounded font-medium transition-colors"
            style={{
              backgroundColor: saving || !value.trim() ? 'var(--bg-surface)' : 'var(--accent)',
              color: saving || !value.trim() ? 'var(--text-muted)' : '#fff',
              cursor: saving || !value.trim() ? 'not-allowed' : 'pointer',
            }}
          >
            {saving ? t('field.saving') : t('field.save')}
          </button>
          {hasDbValue && (
            <button
              onClick={handleDelete}
              disabled={deleting}
              className="text-[11px] px-2 py-1.5 rounded transition-colors"
              style={{
                color: 'var(--text-muted)',
                cursor: deleting ? 'not-allowed' : 'pointer',
              }}
            >
              {deleting ? '...' : t('field.reset')}
            </button>
          )}
        </div>
      )}

      {isLocked && (
        <span className="text-[10px]" style={{ color: 'var(--text-muted)' }}>
          {t('field.lockedByEnv')}
        </span>
      )}

      {/* Metadata */}
      {item.updated_at && (
        <div className="mt-1 text-[9px]" style={{ color: 'var(--text-muted)' }}>
          {t('field.updatedBy', { by: item.updated_by || 'system', at: new Date(item.updated_at).toLocaleString() })}
        </div>
      )}
    </div>
  );
}
