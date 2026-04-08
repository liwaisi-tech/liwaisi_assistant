import { useRef, useCallback, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import type { SessionState } from '../../types/api';

interface MessageInputProps {
  onSend: (content: string) => void;
  disabled: boolean;
  sessionState?: SessionState;
  error: string | null;
}

export function MessageInput({ onSend, disabled, sessionState, error }: MessageInputProps) {
  const { t } = useTranslation('chat');
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (!disabled) {
      textareaRef.current?.focus();
    }
  }, [disabled]);

  const handleSend = useCallback(() => {
    const value = textareaRef.current?.value.trim();
    if (!value || disabled) return;
    onSend(value);
    if (textareaRef.current) {
      textareaRef.current.value = '';
      textareaRef.current.style.height = 'auto';
    }
  }, [onSend, disabled]);

  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }, [handleSend]);

  return (
    <div className="border-t px-4 py-3"
         style={{ borderColor: 'var(--border-dim)', backgroundColor: 'var(--bg-surface)' }}>
      <div className="mx-auto max-w-3xl">
        {error && (
          <div className="mb-2 px-3 py-2 rounded-lg text-xs font-medium bg-red-500/10 text-red-400 border border-red-500/20">
            {error}
          </div>
        )}

        <div className="flex items-end gap-2 rounded-xl px-3 py-2 glow-border transition-shadow focus-within:shadow-[0_0_0_1px_var(--accent),0_0_20px_-4px_var(--accent-glow)]"
             style={{ backgroundColor: 'var(--bg-input)' }}>
          <textarea
            ref={textareaRef}
            className="message-textarea flex-1 bg-transparent text-sm leading-relaxed placeholder:text-slate-500 focus:outline-none"
            style={{ color: 'var(--text-primary)', fontFamily: "'DM Sans', system-ui, sans-serif" }}
            placeholder={disabled
              ? (sessionState === 'waiting' ? t('messageInput.placeholderWaiting') : t('messageInput.placeholderDisabled'))
              : t('messageInput.placeholder')}
            disabled={disabled}
            onKeyDown={handleKeyDown}
            rows={1}
            aria-label={t('messageInput.inputAriaLabel')}
          />
          <button
            type="button"
            onClick={handleSend}
            disabled={disabled}
            className="flex-none flex items-center justify-center w-8 h-8 rounded-lg transition-colors disabled:opacity-30 disabled:cursor-not-allowed"
            style={{
              backgroundColor: disabled ? 'transparent' : 'var(--accent)',
              color: disabled ? 'var(--text-muted)' : '#fff',
            }}
            aria-label={t('messageInput.sendAriaLabel')}
          >
            <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d="M8 12V4M8 4L4 8M8 4L12 8" />
            </svg>
          </button>
        </div>

        <p className="text-[10px] mt-1.5 text-center" style={{ color: 'var(--text-muted)' }}>
          {t('messageInput.hint')}
        </p>
      </div>
    </div>
  );
}
