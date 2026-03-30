import { createContext, useContext, useState, useEffect, useCallback, useRef } from 'react';

interface User {
  email: string;
  name: string;
  picture: string;
}

interface AuthContextType {
  user: User | null;
  isAuthenticated: boolean;
  logout: () => void;
}

const AuthContext = createContext<AuthContextType | null>(null);

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}

function decodeJwtPayload(token: string): Record<string, unknown> {
  const payload = token.split('.')[1];
  const decoded = atob(payload.replace(/-/g, '+').replace(/_/g, '/'));
  return JSON.parse(decoded);
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(() => {
    const stored = localStorage.getItem('liwaisi_user');
    return stored ? JSON.parse(stored) : null;
  });
  const initRef = useRef(false);

  const handleCredentialResponse = useCallback((response: { credential: string }) => {
    const payload = decodeJwtPayload(response.credential);
    const userData: User = {
      email: payload.email as string,
      name: payload.name as string,
      picture: payload.picture as string,
    };
    setUser(userData);
    localStorage.setItem('liwaisi_user', JSON.stringify(userData));
  }, []);

  useEffect(() => {
    if (initRef.current) return;
    initRef.current = true;

    const clientId = import.meta.env.VITE_GOOGLE_CLIENT_ID;
    if (!clientId) {
      console.error('VITE_GOOGLE_CLIENT_ID is not set');
      return;
    }

    const initGoogle = () => {
      window.google?.accounts.id.initialize({
        client_id: clientId,
        callback: handleCredentialResponse,
        auto_select: true,
      });

      if (!user) {
        window.google?.accounts.id.prompt();
      }
    };

    if (window.google?.accounts) {
      initGoogle();
    } else {
      const checkInterval = setInterval(() => {
        if (window.google?.accounts) {
          clearInterval(checkInterval);
          initGoogle();
        }
      }, 100);
      return () => clearInterval(checkInterval);
    }
  }, [handleCredentialResponse, user]);

  const logout = useCallback(() => {
    if (user?.email) {
      localStorage.removeItem(`liwaisi_session_${user.email}`);
    }
    setUser(null);
    localStorage.removeItem('liwaisi_user');
    window.google?.accounts.id.disableAutoSelect();
  }, [user?.email]);

  return (
    <AuthContext.Provider value={{ user, isAuthenticated: !!user, logout }}>
      {children}
    </AuthContext.Provider>
  );
}
