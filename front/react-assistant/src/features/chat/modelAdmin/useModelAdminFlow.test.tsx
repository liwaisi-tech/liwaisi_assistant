import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { I18nTestWrapper } from '../../../test/i18n-test-utils';
import { useModelAdminFlow } from './useModelAdminFlow';
import { ModelAdminActionTypes } from './buildModelAdminSurface';
import { AdminApiError } from '../../../services/api';
import type { ModelRegistryEntry } from '../../../types/setup';

// Mock the admin API surface so tests never touch real HTTP.
vi.mock('../../../services/api', async () => {
  const actual = await vi.importActual<typeof import('../../../services/api')>(
    '../../../services/api',
  );
  return {
    ...actual,
    adminListModels: vi.fn(),
    adminSetDefaultModel: vi.fn(),
    adminLicenseReview: vi.fn(),
    adminDeleteModel: vi.fn(),
  };
});

const api = await import('../../../services/api');
const adminListModels = vi.mocked(api.adminListModels);
const adminSetDefaultModel = vi.mocked(api.adminSetDefaultModel);
const adminLicenseReview = vi.mocked(api.adminLicenseReview);
const adminDeleteModel = vi.mocked(api.adminDeleteModel);

function makeEntry(partial: Partial<ModelRegistryEntry>): ModelRegistryEntry {
  return {
    id: partial.registry_id ?? 'a/b',
    registry_id: 'a/b',
    vendor: 'a',
    family: 'b',
    version: '1',
    display_name: 'A/B',
    description: '',
    modalities: { input: ['text'], output: ['text'] },
    capabilities: { text: true, tools: false, streaming: true, reasoning: false, structured_output: false, vision: false, audio: false },
    context: { length: 100, tokenizer: '' },
    pricing: { input_per_token: 0, output_per_token: 0, currency: 'USD' },
    supported_params: [],
    license: { kind: '', source: '', status: 'approved-commercial' },
    lifecycle: { state: 'active', registered_at: '' },
    routes: [],
    is_product_default: false,
    invokable: true,
    created_at: '',
    updated_at: '',
    ...partial,
  };
}

describe('useModelAdminFlow', () => {
  let injectLocalMessage: ReturnType<typeof vi.fn>;
  let updateMessageContent: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.clearAllMocks();
    injectLocalMessage = vi.fn();
    updateMessageContent = vi.fn();
    adminListModels.mockResolvedValue({ items: [makeEntry({})], total: 1, page: 1, size: 1 });
    adminSetDefaultModel.mockResolvedValue(makeEntry({ is_product_default: true }));
    adminLicenseReview.mockResolvedValue(makeEntry({}));
    adminDeleteModel.mockResolvedValue({ ok: true, registry_id: 'a/b' });
  });

  const renderFlow = () =>
    renderHook(
      () => useModelAdminFlow({ injectLocalMessage, updateMessageContent }),
      { wrapper: I18nTestWrapper },
    );

  it('handles the /models slash command: injects loading + lists models', async () => {
    const { result } = renderFlow();
    await act(async () => {
      const claimed = result.current.tryHandleSlashCommand('/models');
      expect(claimed).toBe(true);
    });

    await waitFor(() => {
      expect(adminListModels).toHaveBeenCalledTimes(1);
    });
    expect(injectLocalMessage).toHaveBeenCalledTimes(1);
    // The injected content is the loading bubble; the refresh then
    // rewrites it with the full list via updateMessageContent.
    expect(updateMessageContent).toHaveBeenCalled();
    const lastCall = updateMessageContent.mock.calls.at(-1)!;
    expect(typeof lastCall[1]).toBe('string');
    expect(lastCall[1]).toContain('$$a2ui:');
  });

  it('accepts localized + aliased slash commands and rejects unknown ones', () => {
    const { result } = renderFlow();
    expect(result.current.tryHandleSlashCommand('/MODEL')).toBe(true);
    expect(result.current.tryHandleSlashCommand('  /modelos  ')).toBe(true);
    expect(result.current.tryHandleSlashCommand('/model')).toBe(true);
    expect(result.current.tryHandleSlashCommand('/help')).toBe(false);
    expect(result.current.tryHandleSlashCommand('models')).toBe(false);
  });

  it('refuses to handle unprefixed CPN actions (REQ-GAP-REG-005)', () => {
    const { result } = renderFlow();
    const claimed = result.current.tryHandleA2UIAction(
      { type: 'submit_register', componentId: 'btn', payload: {} },
      'msg-id',
    );
    expect(claimed).toBe(false);
    expect(adminSetDefaultModel).not.toHaveBeenCalled();
  });

  it('dispatches model:set-default against the admin endpoint and refreshes', async () => {
    const { result } = renderFlow();
    // Seed a surface id by opening the list first.
    await act(async () => {
      result.current.tryHandleSlashCommand('/models');
    });
    await waitFor(() => expect(injectLocalMessage).toHaveBeenCalledTimes(1));
    const surfaceId = injectLocalMessage.mock.calls[0][0] as string;

    adminListModels.mockClear();
    await act(async () => {
      const claimed = result.current.tryHandleA2UIAction(
        {
          type: ModelAdminActionTypes.SetDefault,
          componentId: 'btn-x',
          payload: { registry_id: 'a/b' },
        },
        surfaceId,
      );
      expect(claimed).toBe(true);
    });

    await waitFor(() => {
      expect(adminSetDefaultModel).toHaveBeenCalledWith('a/b');
    });
    // After the mutation, the hook re-lists to refresh the surface.
    expect(adminListModels).toHaveBeenCalled();
  });

  it('swallows actions dispatched against a stale surface id (returns true, no API call)', async () => {
    const { result } = renderFlow();
    await act(async () => {
      result.current.tryHandleSlashCommand('/models');
    });
    await waitFor(() => expect(injectLocalMessage).toHaveBeenCalled());

    const staleId = 'surface-from-an-earlier-turn';
    adminSetDefaultModel.mockClear();
    const claimed = result.current.tryHandleA2UIAction(
      {
        type: ModelAdminActionTypes.SetDefault,
        componentId: 'btn',
        payload: { registry_id: 'a/b' },
      },
      staleId,
    );
    expect(claimed).toBe(true);
    // No API hit because the surface id does not match the hook's active surface.
    expect(adminSetDefaultModel).not.toHaveBeenCalled();
  });

  it('renders an error-surface when adminListModels rejects with AdminApiError', async () => {
    adminListModels.mockRejectedValueOnce(new AdminApiError(500, 'kaboom'));
    const { result } = renderFlow();
    await act(async () => {
      result.current.tryHandleSlashCommand('/models');
    });
    await waitFor(() => {
      expect(updateMessageContent).toHaveBeenCalled();
    });
    const lastCall = updateMessageContent.mock.calls.at(-1)!;
    const content = lastCall[1] as string;
    expect(content).toContain('$$a2ui:');
    expect(content).toContain('kaboom');
    expect(content).toContain('"severity":"error"');
  });

  it('triggers license-block and delete action dispatches with the registry id', async () => {
    const { result } = renderFlow();
    await act(async () => {
      result.current.tryHandleSlashCommand('/models');
    });
    await waitFor(() => expect(injectLocalMessage).toHaveBeenCalled());
    const surfaceId = injectLocalMessage.mock.calls[0][0] as string;

    await act(async () => {
      result.current.tryHandleA2UIAction(
        {
          type: ModelAdminActionTypes.LicenseBlock,
          componentId: 'btn-block',
          payload: { registry_id: 'v/f' },
        },
        surfaceId,
      );
    });
    await waitFor(() => expect(adminLicenseReview).toHaveBeenCalledWith('v/f', 'blocked'));

    await act(async () => {
      result.current.tryHandleA2UIAction(
        {
          type: ModelAdminActionTypes.Delete,
          componentId: 'btn-del',
          payload: { registry_id: 'v/f' },
        },
        surfaceId,
      );
    });
    await waitFor(() => expect(adminDeleteModel).toHaveBeenCalledWith('v/f'));
  });

  it('dispatches license approve-commercial / approve-non-commercial / restrict via the review endpoint', async () => {
    const { result } = renderFlow();
    await act(async () => {
      result.current.tryHandleSlashCommand('/models');
    });
    await waitFor(() => expect(injectLocalMessage).toHaveBeenCalled());
    const surfaceId = injectLocalMessage.mock.calls[0][0] as string;

    await act(async () => {
      result.current.tryHandleA2UIAction(
        { type: ModelAdminActionTypes.LicenseApproveCommercial, componentId: 'x', payload: { registry_id: 'a/1' } },
        surfaceId,
      );
    });
    await waitFor(() => expect(adminLicenseReview).toHaveBeenCalledWith('a/1', 'approved-commercial'));

    await act(async () => {
      result.current.tryHandleA2UIAction(
        { type: ModelAdminActionTypes.LicenseApproveNonCommercial, componentId: 'x', payload: { registry_id: 'a/2' } },
        surfaceId,
      );
    });
    await waitFor(() => expect(adminLicenseReview).toHaveBeenCalledWith('a/2', 'approved-non-commercial'));

    await act(async () => {
      result.current.tryHandleA2UIAction(
        { type: ModelAdminActionTypes.LicenseRestrict, componentId: 'x', payload: { registry_id: 'a/3' } },
        surfaceId,
      );
    });
    await waitFor(() => expect(adminLicenseReview).toHaveBeenCalledWith('a/3', 'restricted'));
  });

  it('swallows model:* actions with an empty registry_id payload (early return)', async () => {
    const { result } = renderFlow();
    await act(async () => {
      result.current.tryHandleSlashCommand('/models');
    });
    await waitFor(() => expect(injectLocalMessage).toHaveBeenCalled());
    const surfaceId = injectLocalMessage.mock.calls[0][0] as string;

    adminSetDefaultModel.mockClear();
    adminDeleteModel.mockClear();
    adminLicenseReview.mockClear();
    await act(async () => {
      result.current.tryHandleA2UIAction(
        { type: ModelAdminActionTypes.SetDefault, componentId: 'x', payload: {} },
        surfaceId,
      );
      result.current.tryHandleA2UIAction(
        { type: ModelAdminActionTypes.Delete, componentId: 'x', payload: null },
        surfaceId,
      );
      result.current.tryHandleA2UIAction(
        { type: ModelAdminActionTypes.LicenseBlock, componentId: 'x', payload: {} },
        surfaceId,
      );
    });
    expect(adminSetDefaultModel).not.toHaveBeenCalled();
    expect(adminDeleteModel).not.toHaveBeenCalled();
    expect(adminLicenseReview).not.toHaveBeenCalled();
  });

  it('ignores an unrecognised model:* action (default branch is a no-op)', async () => {
    const { result } = renderFlow();
    await act(async () => {
      result.current.tryHandleSlashCommand('/models');
    });
    await waitFor(() => expect(injectLocalMessage).toHaveBeenCalled());
    const surfaceId = injectLocalMessage.mock.calls[0][0] as string;

    const claimed = result.current.tryHandleA2UIAction(
      { type: 'model:unknown-op', componentId: 'x', payload: { registry_id: 'a/b' } },
      surfaceId,
    );
    // The hook still claims the action (it starts with `model:`) but
    // takes no concrete action beyond the default switch branch.
    expect(claimed).toBe(true);
  });

  it('surfaces non-AdminApiError errors through the error bubble', async () => {
    adminListModels.mockRejectedValueOnce(new Error('boom'));
    const { result } = renderFlow();
    await act(async () => {
      result.current.tryHandleSlashCommand('/models');
    });
    await waitFor(() => expect(updateMessageContent).toHaveBeenCalled());
    const content = updateMessageContent.mock.calls.at(-1)![1] as string;
    expect(content).toContain('boom');
  });

  it('surfaces a mutation error onto the active surface without crashing the hook', async () => {
    const { result } = renderFlow();
    await act(async () => {
      result.current.tryHandleSlashCommand('/models');
    });
    await waitFor(() => expect(injectLocalMessage).toHaveBeenCalled());
    const surfaceId = injectLocalMessage.mock.calls[0][0] as string;

    adminSetDefaultModel.mockRejectedValueOnce(new AdminApiError(409, 'CANNOT_DELETE_DEFAULT'));
    await act(async () => {
      result.current.tryHandleA2UIAction(
        { type: ModelAdminActionTypes.SetDefault, componentId: 'x', payload: { registry_id: 'a/b' } },
        surfaceId,
      );
    });
    await waitFor(() => {
      const lastContent = updateMessageContent.mock.calls.at(-1)![1] as string;
      expect(lastContent).toContain('CANNOT_DELETE_DEFAULT');
    });
  });

  it('treats refresh as a direct re-list with no mutation call', async () => {
    const { result } = renderFlow();
    await act(async () => {
      result.current.tryHandleSlashCommand('/models');
    });
    await waitFor(() => expect(injectLocalMessage).toHaveBeenCalled());
    const surfaceId = injectLocalMessage.mock.calls[0][0] as string;

    adminListModels.mockClear();
    await act(async () => {
      result.current.tryHandleA2UIAction(
        { type: ModelAdminActionTypes.Refresh, componentId: 'btn-r', payload: null },
        surfaceId,
      );
    });
    await waitFor(() => expect(adminListModels).toHaveBeenCalledTimes(1));
    expect(adminSetDefaultModel).not.toHaveBeenCalled();
    expect(adminLicenseReview).not.toHaveBeenCalled();
  });
});
