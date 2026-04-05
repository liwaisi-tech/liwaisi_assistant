import { useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';

interface ConfirmClearDialogProps {
  onConfirm: () => void;
  onCancel: () => void;
}

export function ConfirmClearDialog({ onConfirm, onCancel }: ConfirmClearDialogProps) {
  const { t } = useTranslation(['chat', 'common']);
  const cancelRef = useRef<HTMLButtonElement>(null);

  const dialogRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    cancelRef.current?.focus();

    function handleKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault();
        onCancel();
      } else if (e.key === 'Tab') {
        const focusable = dialogRef.current?.querySelectorAll<HTMLElement>(
          'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
        );
        if (!focusable || focusable.length === 0) return;
        const first = focusable[0];
        const last = focusable[focusable.length - 1];
        if (e.shiftKey) {
          if (document.activeElement === first) {
            e.preventDefault();
            last.focus();
          }
        } else {
          if (document.activeElement === last) {
            e.preventDefault();
            first.focus();
          }
        }
      }
    }

    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [onCancel]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
      onClick={onCancel}
      role="dialog"
      aria-modal="true"
      aria-labelledby="clear-dialog-title"
    >
      <div
        ref={dialogRef}
        className="w-full max-w-sm mx-4 rounded-xl border p-6"
        style={{
          backgroundColor: 'var(--bg-surface)',
          borderColor: 'var(--border-dim)',
          boxShadow: '0 0 40px rgba(14, 165, 233, 0.08)',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        <h3
          id="clear-dialog-title"
          className="text-base font-semibold mb-2"
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            color: 'var(--text-primary)',
          }}
        >
          {t('chat:confirmClear.title')}
        </h3>

        <p
          className="text-sm mb-6"
          style={{
            color: 'var(--text-secondary)',
            fontFamily: "'DM Sans', system-ui, sans-serif",
          }}
        >
          {t('chat:confirmClear.description')}
        </p>

        <div className="flex justify-end gap-3">
          <button
            ref={cancelRef}
            onClick={onCancel}
            className="px-4 py-2 rounded-lg text-sm font-medium transition-colors"
            style={{
              color: 'var(--text-muted)',
              backgroundColor: 'transparent',
              border: '1px solid var(--border-dim)',
            }}
            onMouseEnter={(e) => (e.currentTarget.style.color = 'var(--text-secondary)')}
            onMouseLeave={(e) => (e.currentTarget.style.color = 'var(--text-muted)')}
          >
            {t('common:buttons.cancel')}
          </button>
          <button
            onClick={onConfirm}
            className="px-4 py-2 rounded-lg text-sm font-medium transition-colors"
            style={{
              color: '#fff',
              backgroundColor: 'var(--accent)',
            }}
            onMouseEnter={(e) => (e.currentTarget.style.opacity = '0.85')}
            onMouseLeave={(e) => (e.currentTarget.style.opacity = '1')}
          >
            {t('common:buttons.clear')}
          </button>
        </div>
      </div>
    </div>
  );
}
