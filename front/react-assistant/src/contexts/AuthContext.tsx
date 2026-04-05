import { createContext, useContext, useState, useEffect, useCallback, useRef } from 'react';
import { setTokenGetter } from '../services/api';

interface User {
  email: string;
  name: string;
  picture: string;
}

interface AuthContextType {
  user: User | null;
  token: string | null;
  isAuthenticated: boolean;
  logout: () => void;
  getToken: () => string | null;
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
  const [token, setToken] = useState<string | null>(() => {
    return localStorage.getItem('liwaisi_token');
  });
  const initRef = useRef(false);
  const tokenRef = useRef<string | null>(token);

  // Keep tokenRef in sync for the getToken callback.
  useEffect(() => {
    tokenRef.current = token;
  }, [token]);

  const getToken = useCallback(() => tokenRef.current, []);

  // Wire token getter into api service immediately so all API calls
  // include the Authorization header from the very first request.
  setTokenGetter(getToken);

  const handleCredentialResponse = useCallback((response: { credential: string }) => {
    const payload = decodeJwtPayload(response.credential);
    const userData: User = {
      email: payload.email as string,
      name: payload.name as string,
      picture: payload.picture as string,
    };
    // Update tokenRef SYNCHRONOUSLY so the token is available for API calls
    // that fire in the same render cycle as the state update.
    tokenRef.current = response.credential;
    setUser(userData);
    setToken(response.credential);
    localStorage.setItem('liwaisi_user', JSON.stringify(userData));
    localStorage.setItem('liwaisi_token', response.credential);
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
        auto_select: false,
      });
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
    setToken(null);
    localStorage.removeItem('liwaisi_user');
    localStorage.removeItem('liwaisi_token');
    window.google?.accounts.id.disableAutoSelect();
  }, [user?.email]);

  // Listen for auth:expired events (fired by api.ts on 401 responses).
  // Re-trigger Google Sign-In prompt to get a fresh token.
  useEffect(() => {
    const handleExpired = () => {
      setToken(null);
      localStorage.removeItem('liwaisi_token');
      // Re-prompt Google Sign-In for fresh credential.
      window.google?.accounts.id.prompt();
    };
    window.addEventListener('auth:expired', handleExpired);
    return () => window.removeEventListener('auth:expired', handleExpired);
  }, []);

  return (
    <AuthContext.Provider value={{ user, token, isAuthenticated: !!user, logout, getToken }}>
      {children}
    </AuthContext.Provider>
  );
}
