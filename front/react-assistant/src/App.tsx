import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { AuthProvider, useAuth } from './contexts/AuthContext';
import { DesktopLayout } from './features/desktop/DesktopLayout';
import { LandingPage } from './features/landing/LandingPage';
import { SetupWizard } from './features/setup/SetupWizard';
import { getUserProfile } from './services/api';

const hasGoogleAuth = !!import.meta.env.VITE_GOOGLE_CLIENT_ID;

function AppContent() {
  const { user, isAuthenticated, notAllowed, dismissNotAllowed } = useAuth();
  const { t } = useTranslation('auth');
  const [needsOnboarding, setNeedsOnboarding] = useState<boolean | null>(null);

  useEffect(() => {
    if (isAuthenticated && user) {
      getUserProfile()
        .then((profile) => {
          setNeedsOnboarding(!profile.onboarding_completed);
        })
        .catch(() => setNeedsOnboarding(false));
    }
  }, [isAuthenticated, user]);

  if (hasGoogleAuth && (!isAuthenticated || !user)) {
    return (
      <>
        <LandingPage />
        {notAllowed && (
          <div
            role="alertdialog"
            aria-modal="true"
            style={{
              position: 'fixed',
              inset: 0,
              backgroundColor: 'rgba(0,0,0,0.75)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              zIndex: 9999,
              padding: '1rem',
            }}
          >
            <div
              style={{
                background: 'var(--bg-deep, #111)',
                color: 'var(--text-primary, #fff)',
                border: '1px solid var(--border, #333)',
                borderRadius: 12,
                maxWidth: 440,
                padding: '1.75rem',
                textAlign: 'center',
                boxShadow: '0 20px 60px rgba(0,0,0,0.5)',
              }}
            >
              <h2 style={{ margin: '0 0 0.75rem', fontSize: '1.25rem' }}>
                {t('notAllowed.title')}
              </h2>
              <p style={{ margin: '0 0 1.5rem', color: 'var(--text-secondary, #aaa)', lineHeight: 1.5 }}>
                {t('notAllowed.message')}
              </p>
              <button
                type="button"
                onClick={dismissNotAllowed}
                style={{
                  background: 'var(--accent, #4f46e5)',
                  color: '#fff',
                  border: 'none',
                  borderRadius: 8,
                  padding: '0.65rem 1.25rem',
                  fontSize: '0.95rem',
                  cursor: 'pointer',
                }}
              >
                {t('notAllowed.dismiss')}
              </button>
            </div>
          </div>
        )}
      </>
    );
  }

  if (needsOnboarding === true) {
    return <SetupWizard onComplete={() => setNeedsOnboarding(false)} />;
  }

  if (needsOnboarding === null && isAuthenticated) {
    return (
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          height: '100vh',
          backgroundColor: 'var(--bg-deep)',
          color: 'var(--text-secondary)',
        }}
      >
        Loading...
      </div>
    );
  }

  const userId = user?.email ?? 'dev-user';
  return <DesktopLayout userId={userId} />;
}

function App() {
  return (
    <AuthProvider>
      <AppContent />
    </AuthProvider>
  );
}

export default App;
