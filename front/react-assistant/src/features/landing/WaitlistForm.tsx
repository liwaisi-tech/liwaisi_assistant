import { useState, type FormEvent } from 'react';
import { joinWaitlist, ApiError } from '../../services/api';

type FormState = 'idle' | 'loading' | 'success' | 'error';

const EMAIL_RE = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

interface WaitlistFormProps {
  variant: 'hero' | 'footer';
  className?: string;
}

export function WaitlistForm({ variant, className = '' }: WaitlistFormProps) {
  const [email, setEmail] = useState('');
  const [state, setState] = useState<FormState>('idle');
  const [errorMsg, setErrorMsg] = useState('');

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();

    const trimmed = email.trim().toLowerCase();
    if (!EMAIL_RE.test(trimmed)) {
      setState('error');
      setErrorMsg('Please enter a valid email address');
      return;
    }

    setState('loading');
    try {
      await joinWaitlist(trimmed);
      setState('success');
    } catch (err) {
      setState('error');
      if (err instanceof ApiError && err.status === 429) {
        setErrorMsg('Too many requests. Please try again in a moment.');
      } else {
        setErrorMsg('Something went wrong. Please try again.');
      }
    }
  }

  if (state === 'success') {
    return (
      <div className={`flex items-center gap-3 ${className}`}>
        <svg width="24" height="24" viewBox="0 0 24 24" fill="none" className="text-emerald-400 shrink-0">
          <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="2" opacity="0.3" />
          <path d="M8 12.5l2.5 2.5 5.5-5.5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="check-animate" />
        </svg>
        <p className="text-sm" style={{ color: '#10b981' }}>
          You're on the list! We'll reach out soon.
        </p>
      </div>
    );
  }

  const isHero = variant === 'hero';

  return (
    <form onSubmit={handleSubmit} className={`flex flex-col gap-2 ${className}`} noValidate>
      <div className={`flex ${isHero ? 'flex-col sm:flex-row' : 'flex-row'} gap-2`}>
        <label htmlFor={`waitlist-email-${variant}`} className="sr-only">
          Email address
        </label>
        <input
          id={`waitlist-email-${variant}`}
          type="email"
          placeholder="your@email.com"
          value={email}
          onChange={(e) => {
            setEmail(e.target.value);
            if (state === 'error') {
              setState('idle');
              setErrorMsg('');
            }
          }}
          disabled={state === 'loading'}
          className={`
            flex-1 px-4 rounded-lg border text-sm outline-none
            transition-colors duration-200
            disabled:opacity-50
            ${isHero ? 'py-3' : 'py-2.5'}
          `}
          style={{
            backgroundColor: 'var(--bg-input)',
            borderColor: state === 'error' ? '#ef4444' : 'var(--border-dim)',
            color: 'var(--text-primary)',
          }}
          onFocus={(e) => {
            if (state !== 'error') {
              e.currentTarget.style.borderColor = 'var(--accent)';
              e.currentTarget.style.boxShadow = '0 0 0 2px var(--accent-glow)';
            }
          }}
          onBlur={(e) => {
            if (state !== 'error') {
              e.currentTarget.style.borderColor = 'var(--border-dim)';
              e.currentTarget.style.boxShadow = 'none';
            }
          }}
        />
        <button
          type="submit"
          disabled={state === 'loading'}
          className={`
            px-6 rounded-lg font-medium text-sm
            transition-all duration-200 cursor-pointer
            disabled:opacity-50 disabled:cursor-not-allowed
            hover:shadow-lg shrink-0
            ${isHero ? 'py-3' : 'py-2.5'}
          `}
          style={{
            backgroundColor: 'var(--accent)',
            color: '#0a0a0f',
            boxShadow: '0 0 16px var(--accent-glow)',
          }}
        >
          {state === 'loading' ? (
            <span className="flex items-center gap-2">
              <svg className="animate-spin h-4 w-4" viewBox="0 0 24 24" fill="none">
                <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" opacity="0.3" />
                <path d="M12 2a10 10 0 0 1 10 10" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
              </svg>
              Joining...
            </span>
          ) : (
            'Request Early Access'
          )}
        </button>
      </div>
      {state === 'error' && errorMsg && (
        <p className="text-xs" style={{ color: '#ef4444' }}>{errorMsg}</p>
      )}
    </form>
  );
}
