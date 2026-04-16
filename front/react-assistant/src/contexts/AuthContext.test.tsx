import { renderHook, act, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import type { ReactNode } from 'react';

// Stub the api module so `fetchAdminStatus` doesn't hit the network during
// mount. We only care about logout cleanup in this suite.
vi.mock('../services/api', async () => {
  const actual = await vi.importActual<typeof import('../services/api')>(
    '../services/api',
  );
  return {
    ...actual,
    setTokenGetter: vi.fn(),
    getUserProfile: vi.fn().mockResolvedValue({ is_admin: false }),
  };
});

import { AuthProvider, useAuth } from './AuthContext';

const EMAIL = 'alice@example.com';
const TOKEN_PAYLOAD = btoa(
  JSON.stringify({ email: EMAIL, name: 'Alice', picture: '' }),
);
const FAKE_JWT = `hdr.${TOKEN_PAYLOAD}.sig`;

function wrapper({ children }: { children: ReactNode }) {
  return <AuthProvider>{children}</AuthProvider>;
}

beforeEach(() => {
  localStorage.clear();
  // Pre-seed localStorage to simulate a logged-in user.
  localStorage.setItem(
    'liwaisi_user',
    JSON.stringify({ email: EMAIL, name: 'Alice', picture: '', is_admin: false }),
  );
  localStorage.setItem('liwaisi_token', FAKE_JWT);
  // Google client shim — logout calls it during cleanup.
  (globalThis as unknown as { google: unknown }).google = {
    accounts: {
      id: {
        initialize: vi.fn(),
        disableAutoSelect: vi.fn(),
        prompt: vi.fn(),
      },
    },
  };
});

afterEach(() => {
  localStorage.clear();
  vi.restoreAllMocks();
});

describe('AuthContext.logout — unified storage cleanup (REQ-106, §4.4)', () => {
  it('clears the unified liwaisi_active_session_{email} key on logout', async () => {
    // Active session hint written by useChatList under the unified key.
    localStorage.setItem(`liwaisi_active_session_${EMAIL}`, 'sess_xyz');

    const { result } = renderHook(() => useAuth(), { wrapper });
    await waitFor(() => expect(result.current.user?.email).toBe(EMAIL));

    act(() => {
      result.current.logout();
    });

    expect(localStorage.getItem(`liwaisi_active_session_${EMAIL}`)).toBeNull();
    expect(localStorage.getItem('liwaisi_user')).toBeNull();
    expect(localStorage.getItem('liwaisi_token')).toBeNull();
  });

  it('uses exactly the same key prefix as useChatList (guards against REQ-106 drift)', async () => {
    localStorage.setItem(`liwaisi_active_session_${EMAIL}`, 'sess_abc');

    const { result } = renderHook(() => useAuth(), { wrapper });
    await waitFor(() => expect(result.current.user).not.toBeNull());

    act(() => {
      result.current.logout();
    });

    // Scan the keyspace — NO key with the active-session prefix should survive
    // for this user after logout. This is the exact invariant that, when
    // violated, let the ghost-session bug persist across login cycles.
    const surviving: string[] = [];
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (k && k.startsWith('liwaisi_active_session_')) surviving.push(k);
    }
    expect(surviving).toEqual([]);
  });
});
