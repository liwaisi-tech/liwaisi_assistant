import { AuthProvider, useAuth } from './contexts/AuthContext';
import { DesktopLayout } from './features/desktop/DesktopLayout';
import { LandingPage } from './features/landing/LandingPage';

const hasGoogleAuth = !!import.meta.env.VITE_GOOGLE_CLIENT_ID;

function AppContent() {
  const { user, isAuthenticated } = useAuth();

  if (hasGoogleAuth && (!isAuthenticated || !user)) {
    return <LandingPage />;
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
