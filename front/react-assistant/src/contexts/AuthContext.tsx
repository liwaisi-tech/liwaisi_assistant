import { createContext, useContext, useState, useEffect, useCallback, useRef } from 'react';
import { setTokenGetter, getUserProfile, ApiError } from '../services/api';

interface User {
  email: string;
  name: string;
  picture: string;
  is_admin: boolean;
}

interface AuthContextType {
  user: User | null;
  token: string | null;
  isAuthenticated: boolean;
  isAdmin: boolean;
  notAllowed: boolean;
  dismissNotAllowed: () => void;
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
    if (stored) {
      const parsed = JSON.parse(stored);
      return { ...parsed, is_admin: parsed.is_admin ?? false };
    }
    return null;
  });
  const [token, setToken] = useState<string | null>(() => {
    return localStorage.getItem('liwaisi_token');
  });
  const [notAllowed, setNotAllowed] = useState(false);
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

  // Fetch admin status from backend profile endpoint.
  const fetchAdminStatus = useCallback(async () => {
    try {
      const profile = await getUserProfile();
      const isAdmin = (profile as unknown as Record<string, unknown>).is_admin === true;
      setUser((prev) => {
        if (!prev) return prev;
        const updated = { ...prev, is_admin: isAdmin };
        localStorage.setItem('liwaisi_user', JSON.stringify(updated));
        return updated;
      });
    } catch (err) {
      // 403 from the backend means the email is not in the allowlist.
      // Force logout and surface an "invitation required" message.
      if (err instanceof ApiError && err.status === 403) {
        setNotAllowed(true);
        if (user?.email) {
          localStorage.removeItem(`liwaisi_session_${user.email}`);
          localStorage.removeItem(`liwaisi_active_session_${user.email}`);
        }
        setUser(null);
        setToken(null);
        tokenRef.current = null;
        localStorage.removeItem('liwaisi_user');
        localStorage.removeItem('liwaisi_token');
        window.google?.accounts.id.disableAutoSelect();
        return;
      }
      // Other failures are non-fatal for admin status.
    }
  }, [user?.email]);

  // On mount, if we already have a token, refresh admin status.
  useEffect(() => {
    if (token) {
      fetchAdminStatus();
    }
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  const handleCredentialResponse = useCallback((response: { credential: string }) => {
    const payload = decodeJwtPayload(response.credential);
    const userData: User = {
      email: payload.email as string,
      name: payload.name as string,
      picture: payload.picture as string,
      is_admin: false,
    };
    // Update tokenRef SYNCHRONOUSLY so the token is available for API calls
    // that fire in the same render cycle as the state update.
    tokenRef.current = response.credential;
    setUser(userData);
    setToken(response.credential);
    localStorage.setItem('liwaisi_user', JSON.stringify(userData));
    localStorage.setItem('liwaisi_token', response.credential);

    // Validate against backend allowlist + fetch admin status.
    setNotAllowed(false);
    setTimeout(() => fetchAdminStatus(), 100);
  }, [fetchAdminStatus]);

  useEffect(() => {
    // Gate on actual completion, NOT on effect entry. The previous version
    // set `initRef.current = true` before polling for window.google, which
    // meant a re-render during the polling window cleared the interval AND
    // skipped re-init on the next effect call — leaving GSI uninitialized
    // and SignInModal.renderButton() failing with "client_id is missing".
    if (initRef.current) return;

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
      initRef.current = true; // mark complete only after successful init
    };

    if (window.google?.accounts) {
      initGoogle();
      return;
    }

    const checkInterval = setInterval(() => {
      if (window.google?.accounts) {
        clearInterval(checkInterval);
        initGoogle();
      }
    }, 100);
    return () => clearInterval(checkInterval);
  }, [handleCredentialResponse, user]);

  const logout = useCallback(() => {
    if (user?.email) {
      // Clear the unified active-session key (REQ-106, §4.4). The prefix
      // `liwaisi_active_session_` MUST match `STORAGE_KEY_PREFIX` in
      // `useChatList.ts` — keeping them in sync is what prevents the
      // ghost-session bug from resurfacing across logout/login.
      localStorage.removeItem(`liwaisi_session_${user.email}`);
      localStorage.removeItem(`liwaisi_active_session_${user.email}`);
    }
    setUser(null);
    setToken(null);
    localStorage.removeItem('liwaisi_user');
    localStorage.removeItem('liwaisi_token');
    window.google?.accounts.id.disableAutoSelect();
  }, [user?.email]);

  // Listen for auth:expired events (fired by api.ts on 401 responses).
  // Perform a full logout so the app re-renders to the landing page and the
  // user can sign in again. Clearing only the token is not enough: App.tsx
  // gates the landing view on `!user`, so leaving `user` populated keeps the
  // authed UI mounted while every API call keeps 401-ing.
  const logoutRef = useRef(logout);
  useEffect(() => {
    logoutRef.current = logout;
  }, [logout]);
  useEffect(() => {
    const handleExpired = () => {
      logoutRef.current();
      window.google?.accounts.id.prompt();
    };
    window.addEventListener('auth:expired', handleExpired);
    return () => window.removeEventListener('auth:expired', handleExpired);
  }, []);

  return (
    <AuthContext.Provider value={{ user, token, isAuthenticated: !!user, isAdmin: user?.is_admin ?? false, notAllowed, dismissNotAllowed: () => setNotAllowed(false), logout, getToken }}>
      {children}
    </AuthContext.Provider>
  );
}
