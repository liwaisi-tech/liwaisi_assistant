import { AuthProvider, useAuth } from './contexts/AuthContext';
import { ChatContainer } from './features/chat/ChatContainer';
import { LoginScreen } from './features/auth/LoginScreen';

function AppContent() {
  const { user, isAuthenticated } = useAuth();

  if (!isAuthenticated || !user) {
    return <LoginScreen />;
  }

  return <ChatContainer userId={user.email} />;
}

function App() {
  return (
    <AuthProvider>
      <AppContent />
    </AuthProvider>
  );
}

export default App;
