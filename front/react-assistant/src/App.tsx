import { AuthProvider, useAuth } from './contexts/AuthContext';
import { ChatContainer } from './features/chat/ChatContainer';
import { LoginScreen } from './features/auth/LoginScreen';

const hasGoogleAuth = !!import.meta.env.VITE_GOOGLE_CLIENT_ID;

function AppContent() {
  const { user, isAuthenticated } = useAuth();

  if (hasGoogleAuth && (!isAuthenticated || !user)) {
    return <LoginScreen />;
  }

  const userId = user?.email ?? 'dev-user';
  return <ChatContainer userId={userId} />;
}

function App() {
  return (
    <AuthProvider>
      <AppContent />
    </AuthProvider>
  );
}

export default App;
