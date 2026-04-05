import { useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { loadNamespace } from '../../i18n/loadNamespace';

export function LoginScreen() {
  const { t } = useTranslation('auth');
  const buttonRef = useRef<HTMLDivElement>(null);

  useEffect(() => { loadNamespace('auth'); }, []);

  useEffect(() => {
    const renderButton = () => {
      if (buttonRef.current && window.google?.accounts) {
        window.google.accounts.id.renderButton(buttonRef.current, {
          theme: 'filled_black',
          size: 'large',
          text: 'signin_with',
          width: 280,
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
  }, []);

  return (
    <div
      className="flex flex-col items-center justify-center h-dvh gap-6"
      style={{ backgroundColor: 'var(--bg-deep)' }}
    >
      <div className="flex flex-col items-center gap-2">
        <h1
          className="text-2xl font-semibold tracking-tight"
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            color: 'var(--text-primary)',
          }}
        >
          {t('login.title')}
        </h1>
        <p className="text-sm" style={{ color: 'var(--text-secondary)' }}>
          {t('login.subtitle')}
        </p>
      </div>

      <div ref={buttonRef} />
    </div>
  );
}
