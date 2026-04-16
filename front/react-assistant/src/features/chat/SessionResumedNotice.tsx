import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';

export interface SessionResumedNoticeProps {
  /**
   * When this value changes to a new truthy value, the notice becomes visible.
   * Use a monotonically-incrementing counter or a timestamp so repeated
   * recoveries re-trigger the notice. `null` / `0` means "hidden".
   */
  resumedAt: number | null;
  /** Milliseconds before auto-dismiss. Defaults to 4000 per REQ-111 / AC-008. */
  durationMs?: number;
  /** Optional manual dismiss hook for tests. */
  onDismiss?: () => void;
}

/**
 * Non-blocking, low-visual-weight banner shown when `useChatList` auto-recovers
 * from a ghost session. Does not steal focus (no autofocus, no modal), uses
 * `aria-live="polite"` so it is announced but not intrusive, and auto-dismisses
 * within `durationMs` (default 4s). See spec REQ-111, AC-008, AC-009.
 */
export function SessionResumedNotice({
  resumedAt,
  durationMs = 4000,
  onDismiss,
}: SessionResumedNoticeProps) {
  const { t } = useTranslation('chat');
  const [visible, setVisible] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (!resumedAt) return;
    setVisible(true);

    if (timerRef.current) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => {
      setVisible(false);
      onDismiss?.();
    }, durationMs);

    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
    };
  }, [resumedAt, durationMs, onDismiss]);

  if (!visible) return null;

  return (
    <div
      // role=status + aria-live=polite: announced to AT without interrupting.
      // Not focusable: the composer keeps focus (AC-008, AC-009).
      role="status"
      aria-live="polite"
      className="session-notice-in mx-3 mt-2 flex items-start gap-2 rounded-md border px-3 py-2 text-xs shadow-sm"
      style={{
        borderColor: 'var(--border-dim)',
        backgroundColor: 'var(--bg-surface)',
        color: 'var(--text-secondary)',
      }}
      data-testid="session-resumed-notice"
    >
      <svg
        width="14"
        height="14"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
        aria-hidden="true"
        style={{ color: 'var(--accent)', flexShrink: 0, marginTop: '1px' }}
      >
        <path d="M21 12a9 9 0 1 1-6.2-8.55" />
        <polyline points="21 4 21 10 15 10" />
      </svg>
      <div className="flex-1 leading-snug">
        <div
          className="font-medium"
          style={{ color: 'var(--text-primary)', fontFamily: "'JetBrains Mono', monospace" }}
        >
          {t('resumedNotice.title')}
        </div>
        <div style={{ color: 'var(--text-muted)' }}>
          {t('resumedNotice.message')}
        </div>
      </div>
      <button
        type="button"
        onClick={() => {
          setVisible(false);
          onDismiss?.();
          if (timerRef.current) {
            clearTimeout(timerRef.current);
            timerRef.current = null;
          }
        }}
        // tabIndex -1 would hide from keyboard users; keep focusable so keyboard
        // users can dismiss, but NOT autofocused so typing is preserved.
        className="rounded p-0.5 transition-colors"
        style={{ color: 'var(--text-muted)' }}
        onMouseEnter={(e) => (e.currentTarget.style.color = 'var(--text-secondary)')}
        onMouseLeave={(e) => (e.currentTarget.style.color = 'var(--text-muted)')}
        aria-label={t('resumedNotice.dismiss')}
      >
        <svg
          width="12"
          height="12"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M18 6L6 18M6 6l12 12" />
        </svg>
      </button>
    </div>
  );
}
