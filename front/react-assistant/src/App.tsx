import { useState, useEffect } from 'react';
import { AuthProvider, useAuth } from './contexts/AuthContext';
import { DesktopLayout } from './features/desktop/DesktopLayout';
import { LandingPage } from './features/landing/LandingPage';
import { SetupWizard } from './features/setup/SetupWizard';
import { getUserProfile } from './services/api';

const hasGoogleAuth = !!import.meta.env.VITE_GOOGLE_CLIENT_ID;

function AppContent() {
  const { user, isAuthenticated } = useAuth();
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
    return <LandingPage />;
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
