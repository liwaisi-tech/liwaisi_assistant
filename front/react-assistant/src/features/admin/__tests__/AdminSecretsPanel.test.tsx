import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { I18nTestWrapper, testI18n } from '../../../test/i18n-test-utils';
import adminEn from '../../../i18n/locales/en/admin.json';
import { AdminApiError } from '../../../services/api';

// Mock the api module so we can control getAdminConfig per test.
vi.mock('../../../services/api', async () => {
  const actual = await vi.importActual<typeof import('../../../services/api')>(
    '../../../services/api',
  );
  return {
    ...actual,
    getAdminConfig: vi.fn(),
    setAdminConfig: vi.fn(),
    deleteAdminConfig: vi.fn(),
  };
});

// Mock loadNamespace — admin namespace is preloaded into testI18n directly.
vi.mock('../../../i18n/loadNamespace', () => ({
  loadNamespace: vi.fn().mockResolvedValue(undefined),
}));

// Mock useAuth so we can spy on logout.
const logoutSpy = vi.fn();
vi.mock('../../../contexts/AuthContext', () => ({
  useAuth: () => ({
    user: { email: 'admin@example.com', name: 'Admin', picture: '', is_admin: true },
    token: 'tok',
    isAuthenticated: true,
    isAdmin: true,
    logout: logoutSpy,
    getToken: () => 'tok',
  }),
}));

import { AdminSecretsPanel } from '../AdminSecretsPanel';
import { getAdminConfig } from '../../../services/api';

const mockedGetAdminConfig = vi.mocked(getAdminConfig);

beforeEach(() => {
  // Ensure admin namespace is available on the test i18n instance.
  if (!testI18n.hasResourceBundle('en', 'admin')) {
    testI18n.addResourceBundle('en', 'admin', adminEn, true, true);
  }
  logoutSpy.mockClear();
  mockedGetAdminConfig.mockReset();
});

afterEach(() => {
  testI18n.changeLanguage('en');
});

describe('AdminSecretsPanel error handling', () => {
  it('calls auth.logout on 401 and does not render the error state', async () => {
    mockedGetAdminConfig.mockRejectedValueOnce(new AdminApiError(401, 'unauthorized'));

    render(<AdminSecretsPanel />, { wrapper: I18nTestWrapper });

    await waitFor(() => expect(logoutSpy).toHaveBeenCalledTimes(1));
    expect(screen.queryByText(adminEn.errors.forbidden)).not.toBeInTheDocument();
    expect(screen.queryByText(adminEn.errors.unavailable)).not.toBeInTheDocument();
    expect(screen.queryByText(adminEn.errors.generic)).not.toBeInTheDocument();
  });

  it('renders the forbidden string on 403', async () => {
    mockedGetAdminConfig.mockRejectedValueOnce(new AdminApiError(403, 'forbidden'));

    render(<AdminSecretsPanel />, { wrapper: I18nTestWrapper });

    await waitFor(() =>
      expect(screen.getByText(adminEn.errors.forbidden)).toBeInTheDocument(),
    );
    expect(logoutSpy).not.toHaveBeenCalled();
  });

  it('renders the unavailable string on 503', async () => {
    mockedGetAdminConfig.mockRejectedValueOnce(new AdminApiError(503, 'unavailable'));

    render(<AdminSecretsPanel />, { wrapper: I18nTestWrapper });

    await waitFor(() =>
      expect(screen.getByText(adminEn.errors.unavailable)).toBeInTheDocument(),
    );
    expect(logoutSpy).not.toHaveBeenCalled();
  });

  it('renders the generic string on a non-AdminApiError failure', async () => {
    mockedGetAdminConfig.mockRejectedValueOnce(new Error('boom'));

    render(<AdminSecretsPanel />, { wrapper: I18nTestWrapper });

    await waitFor(() =>
      expect(screen.getByText(adminEn.errors.generic)).toBeInTheDocument(),
    );
    expect(logoutSpy).not.toHaveBeenCalled();
  });
});
