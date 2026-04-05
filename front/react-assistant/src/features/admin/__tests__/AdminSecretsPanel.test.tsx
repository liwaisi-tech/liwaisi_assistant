import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { AdminSecretsPanel } from '../AdminSecretsPanel';
import { I18nTestWrapper } from '../../../test/i18n-test-utils';
import type { AdminConfigResponse } from '../../../types/admin';

const mockConfigResponse: AdminConfigResponse = {
  items: [
    { key: 'openrouter_api_key', value: 'sk-o********', is_secret: true, source: 'db', label: 'OpenRouter API Key', category: 'llm', required: true, updated_at: '2026-01-01T00:00:00Z', updated_by: 'admin@test.com' },
    { key: 'default_model', value: 'anthropic/claude-sonnet-4-6', is_secret: false, source: 'default', label: 'Default Model', category: 'llm', required: false, updated_at: '', updated_by: '' },
    { key: 'google_client_id', value: '123.apps.googleusercontent.com', is_secret: false, source: 'env', label: 'Google Client ID', category: 'auth', required: false, updated_at: '', updated_by: '' },
  ],
  setup_required: false,
};

vi.mock('../../../services/api', () => ({
  getAdminConfig: vi.fn(() => Promise.resolve(mockConfigResponse)),
  setAdminConfig: vi.fn(() => Promise.resolve({ ok: true })),
  deleteAdminConfig: vi.fn(() => Promise.resolve({ ok: true })),
}));

vi.mock('../../../i18n/loadNamespace', () => ({
  loadNamespace: vi.fn(),
}));

function renderPanel() {
  return render(
    <I18nTestWrapper>
      <AdminSecretsPanel />
    </I18nTestWrapper>,
  );
}

describe('AdminSecretsPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders config items grouped by category', async () => {
    renderPanel();

    await waitFor(() => {
      expect(screen.getByText('OpenRouter API Key')).toBeInTheDocument();
    });

    expect(screen.getByText('Default Model')).toBeInTheDocument();
    expect(screen.getByText('Google Client ID')).toBeInTheDocument();
  });

  it('shows source badges', async () => {
    renderPanel();

    await waitFor(() => {
      expect(screen.getByText('DB')).toBeInTheDocument();
    });

    expect(screen.getByText('DEFAULT')).toBeInTheDocument();
    expect(screen.getByText('ENV')).toBeInTheDocument();
  });

  it('shows locked state for env-sourced items', async () => {
    renderPanel();

    await waitFor(() => {
      expect(screen.getByText('Set via environment variable (read-only)')).toBeInTheDocument();
    });
  });

  it('shows required badge for required items', async () => {
    renderPanel();

    await waitFor(() => {
      expect(screen.getByText('required')).toBeInTheDocument();
    });
  });
});
