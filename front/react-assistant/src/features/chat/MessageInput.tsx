import { useRef, useCallback, useEffect, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { SessionState } from '../../types/api';
import type { AwakeningPhase } from '../../hooks/useChat';

interface MessageInputProps {
  onSend: (content: string) => void;
  disabled: boolean;
  sessionState?: SessionState;
  /**
   * Awakening gate (spec §4.4 / REQ-008). When `pending`, the composer
   * shows a distinctive "brae is waking up…" placeholder instead of the
   * generic "waiting for response…" so the user understands this is the
   * one-shot self-discovery turn, not an arbitrary backend delay.
   */
  awakeningPhase?: AwakeningPhase;
  error: string | null;
}

export function MessageInput({ onSend, disabled, sessionState, awakeningPhase, error }: MessageInputProps) {
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

  // Placeholder copy is derived at render — cheap string choice, no effect
  // needed (vercel rerender-derived-state-no-effect). The awakening branch
  // wins over the generic waiting/disabled branches so users understand
  // the first-boot discovery turn is distinct from ordinary latency.
  const placeholder = useMemo(() => {
    if (awakeningPhase === 'pending') return t('messageInput.placeholderAwakening');
    if (!disabled) return t('messageInput.placeholder');
    if (sessionState === 'waiting') return t('messageInput.placeholderWaiting');
    return t('messageInput.placeholderDisabled');
  }, [awakeningPhase, disabled, sessionState, t]);

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
            placeholder={placeholder}
            disabled={disabled}
            onKeyDown={handleKeyDown}
            rows={1}
            aria-label={t('messageInput.inputAriaLabel')}
            data-awakening={awakeningPhase === 'pending' ? 'true' : undefined}
          />
          <button
            type="button"
            onClick={handleSend}
            disabled={disabled}
            className="flex-none flex items-center justify-center w-8 h-8 rounded-lg transition-all disabled:opacity-30 disabled:cursor-not-allowed enabled:hover:shadow-[0_0_12px_-2px_var(--accent-glow)] enabled:hover:brightness-110"
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
