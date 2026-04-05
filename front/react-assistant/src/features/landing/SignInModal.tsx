import { useEffect, useRef, useCallback } from 'react';
import { WaitlistForm } from './WaitlistForm';

interface SignInModalProps {
  open: boolean;
  onClose: () => void;
}

export function SignInModal({ open, onClose }: SignInModalProps) {
  const panelRef = useRef<HTMLDivElement>(null);
  const googleBtnRef = useRef<HTMLDivElement>(null);
  const closeBtnRef = useRef<HTMLButtonElement>(null);

  // Render Google Sign-In button when modal opens
  useEffect(() => {
    if (!open) return;

    const renderButton = () => {
      if (googleBtnRef.current && window.google?.accounts) {
        // Clear previous renders
        googleBtnRef.current.innerHTML = '';
        window.google.accounts.id.renderButton(googleBtnRef.current, {
          theme: 'filled_black',
          size: 'large',
          text: 'signin_with',
          width: 300,
        });
      }
    };

    if (window.google?.accounts) {
      renderButton();
    } else {
      const checkInterval = setInterval(() => {
        if (window.google?.accounts) {
          clearInterval(checkInterval);
          renderButton();
        }
      }, 100);
      return () => clearInterval(checkInterval);
    }
  }, [open]);

  // Focus trap + Escape key
  useEffect(() => {
    if (!open) return;

    // Focus the close button on open
    requestAnimationFrame(() => closeBtnRef.current?.focus());

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
        return;
      }

      // Simple focus trap within the panel
      if (e.key === 'Tab' && panelRef.current) {
        const focusable = panelRef.current.querySelectorAll<HTMLElement>(
          'button, [href], input, [tabindex]:not([tabindex="-1"])',
        );
        if (focusable.length === 0) return;

        const first = focusable[0];
        const last = focusable[focusable.length - 1];

        if (e.shiftKey && document.activeElement === first) {
          e.preventDefault();
          last.focus();
        } else if (!e.shiftKey && document.activeElement === last) {
          e.preventDefault();
          first.focus();
        }
      }
    };

    // Lock body scroll
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';

    document.addEventListener('keydown', handleKeyDown);
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      document.body.style.overflow = prevOverflow;
    };
  }, [open, onClose]);

  const handleBackdropClick = useCallback(
    (e: React.MouseEvent) => {
      if (e.target === e.currentTarget) onClose();
    },
    [onClose],
  );

  if (!open) return null;

  return (
    <div
      className="signin-backdrop fixed inset-0 z-50 flex items-center justify-center px-4"
      style={{ backgroundColor: 'rgba(0, 0, 0, 0.7)', backdropFilter: 'blur(8px)' }}
      onClick={handleBackdropClick}
      role="dialog"
      aria-modal="true"
      aria-label="Sign in to Liwaisi Assistant"
    >
      <div
        ref={panelRef}
        className="signin-panel modal-scanline relative w-full max-w-md rounded-2xl border overflow-hidden"
        style={{
          backgroundColor: 'rgba(18, 18, 26, 0.92)',
          backdropFilter: 'blur(24px)',
          borderColor: 'var(--border-dim)',
          boxShadow:
            '0 0 0 1px var(--border-dim), 0 0 48px -12px var(--accent-glow), 0 32px 64px -24px rgba(0,0,0,0.8)',
        }}
      >
        {/* Close button */}
        <button
          ref={closeBtnRef}
          onClick={onClose}
          className="absolute top-4 right-4 z-10 w-8 h-8 rounded-full flex items-center justify-center transition-colors cursor-pointer"
          style={{ color: 'var(--text-muted)', backgroundColor: 'transparent' }}
          onMouseEnter={(e) => {
            e.currentTarget.style.backgroundColor = 'var(--bg-input)';
            e.currentTarget.style.color = 'var(--text-primary)';
          }}
          onMouseLeave={(e) => {
            e.currentTarget.style.backgroundColor = 'transparent';
            e.currentTarget.style.color = 'var(--text-muted)';
          }}
          aria-label="Close sign-in dialog"
        >
          <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
            <path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
          </svg>
        </button>

        {/* Top accent line */}
        <div
          className="h-px w-full"
          style={{
            background: 'linear-gradient(90deg, transparent, var(--accent), transparent)',
          }}
        />

        <div className="relative z-[2] p-8 sm:p-10 flex flex-col items-center gap-6">
          {/* Logo + Header */}
          <div className="flex flex-col items-center gap-4">
            <div
              className="w-14 h-14 rounded-2xl flex items-center justify-center"
              style={{
                backgroundColor: 'rgba(14, 165, 233, 0.08)',
                border: '1px solid rgba(14, 165, 233, 0.15)',
                boxShadow: '0 0 24px -4px var(--accent-glow)',
              }}
            >
              <img src="/liwaisi_logo_dark_bg.svg" alt="" className="h-8 w-auto" aria-hidden="true" />
            </div>

            <div className="text-center">
              <h2
                className="text-lg font-semibold tracking-tight mb-1"
                style={{
                  fontFamily: "'JetBrains Mono', monospace",
                  color: 'var(--text-primary)',
                }}
              >
                Welcome to Liwaisi
              </h2>
              <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
                Sign in to access your AI command center
              </p>
            </div>
          </div>

          {/* Google Sign-In */}
          <div className="w-full flex flex-col items-center gap-3">
            <div ref={googleBtnRef} className="flex justify-center" />

            <p className="text-[10px]" style={{ color: 'var(--text-muted)' }}>
              We only request basic profile info. Your data stays yours.
            </p>
          </div>

          {/* Divider */}
          <div className="w-full flex items-center gap-3">
            <div className="flex-1 h-px" style={{ backgroundColor: 'var(--border-dim)' }} />
            <span
              className="text-[10px] uppercase tracking-widest"
              style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
            >
              or join waitlist
            </span>
            <div className="flex-1 h-px" style={{ backgroundColor: 'var(--border-dim)' }} />
          </div>

          {/* Waitlist alternative */}
          <div className="w-full">
            <WaitlistForm variant="footer" />
            <p className="text-[10px] mt-2 text-center" style={{ color: 'var(--text-muted)' }}>
              No account needed. We'll contact you for B2B access.
            </p>
          </div>
        </div>

        {/* Bottom accent line */}
        <div
          className="h-px w-full"
          style={{
            background: 'linear-gradient(90deg, transparent, rgba(16,185,129,0.4), transparent)',
          }}
        />
      </div>
    </div>
  );
}
