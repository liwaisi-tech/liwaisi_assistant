import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { I18nTestWrapper } from '../../../test/i18n-test-utils';
import { useModelAdminFlow } from '../modelAdmin/useModelAdminFlow';
import { ModelAdminActionTypes } from '../modelAdmin/buildModelAdminSurface';
import { chatReducer, initialState } from '../../../hooks/useChat';
import type { ChatState } from '../../../hooks/useChat';
import type { ModelRegistryEntry } from '../../../types/setup';
import { A2UI_MARKER } from '../a2ui/constants';

// Mock the admin API surface — tests simulate the full client loop
// without a real network round-trip.
vi.mock('../../../services/api', async () => {
  const actual = await vi.importActual<typeof import('../../../services/api')>(
    '../../../services/api',
  );
  return {
    ...actual,
    adminListModels: vi.fn(),
    adminSetDefaultModel: vi.fn(),
  };
});

const api = await import('../../../services/api');
const adminListModels = vi.mocked(api.adminListModels);
const adminSetDefaultModel = vi.mocked(api.adminSetDefaultModel);

function entry(
  registryId: string,
  overrides: Partial<ModelRegistryEntry> = {},
): ModelRegistryEntry {
  return {
    id: registryId,
    registry_id: registryId,
    vendor: registryId.split('/')[0] ?? '',
    family: registryId.split('/')[1] ?? '',
    version: '1',
    display_name: registryId,
    description: '',
    modalities: { input: ['text'], output: ['text'] },
    capabilities: {
      text: true, tools: false, streaming: true, reasoning: false,
      structured_output: false, vision: false, audio: false,
    },
    context: { length: 128_000, tokenizer: '' },
    pricing: { input_per_token: 0.000001, output_per_token: 0.000003, currency: 'USD' },
    supported_params: [],
    license: { kind: 'permissive', source: 'manual', status: 'approved-commercial' },
    lifecycle: { state: 'active', registered_at: '' },
    routes: [],
    is_product_default: false,
    invokable: true,
    created_at: '',
    updated_at: '',
    ...overrides,
  };
}

function decodeSurface(content: string) {
  expect(content.startsWith(A2UI_MARKER)).toBe(true);
  return JSON.parse(content.slice(A2UI_MARKER.length));
}

describe('model-admin integration — slash → card list → set-default → refresh', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('drives the full loop: /models → loading → list → set-default → refreshed list', async () => {
    // Reducer state simulates the chat message list; the hook's side-effects
    // go through the `injectLocalMessage` / `updateMessageContent` bridges
    // which in production go via useChat.dispatch — here we just dispatch
    // the same actions by hand so the integration asserts the reducer +
    // hook together without mounting the full app.
    let state: ChatState = { ...initialState };
    const injectLocalMessage = vi.fn((id: string, content: string, cpnRole?: string) => {
      state = chatReducer(state, { type: 'INJECT_LOCAL_MESSAGE', id, content, cpnRole });
    });
    const updateMessageContent = vi.fn((id: string, content: string) => {
      state = chatReducer(state, { type: 'UPDATE_MESSAGE_CONTENT', id, content });
    });

    // Seed backend fixtures.
    const before: ModelRegistryEntry[] = [
      entry('anthropic/claude-opus-4-6', { display_name: 'Claude Opus 4.6' }),
      entry('google/gemma-4-31b-it', { display_name: 'Gemma 4 31B', is_product_default: true }),
    ];
    const after: ModelRegistryEntry[] = [
      entry('anthropic/claude-opus-4-6', { display_name: 'Claude Opus 4.6', is_product_default: true }),
      entry('google/gemma-4-31b-it', { display_name: 'Gemma 4 31B', is_product_default: false }),
    ];
    adminListModels.mockResolvedValueOnce({ items: before, total: before.length, page: 1, size: before.length });
    adminListModels.mockResolvedValueOnce({ items: after, total: after.length, page: 1, size: after.length });
    adminSetDefaultModel.mockResolvedValue(entry('anthropic/claude-opus-4-6', { is_product_default: true }));

    const { result } = renderHook(
      () => useModelAdminFlow({ injectLocalMessage, updateMessageContent }),
      { wrapper: I18nTestWrapper },
    );

    // 1) Trigger the slash command.
    await act(async () => {
      expect(result.current.tryHandleSlashCommand('/models')).toBe(true);
    });

    // 2) A loading bubble should have been injected.
    await waitFor(() => expect(injectLocalMessage).toHaveBeenCalled());
    const surfaceId = injectLocalMessage.mock.calls[0][0] as string;
    const loadingContent = injectLocalMessage.mock.calls[0][1] as string;
    const loading = decodeSurface(loadingContent);
    expect(loading.components[0].type).toBe('text');

    // 3) updateMessageContent should replace the loading bubble with the
    //    full card list once adminListModels resolves.
    await waitFor(() => expect(updateMessageContent).toHaveBeenCalled());
    const firstUpdate = decodeSurface(
      (updateMessageContent.mock.calls.at(-1) as [string, string])[1],
    );
    const cards = firstUpdate.components.filter((c: { type: string }) => c.type === 'card');
    expect(cards).toHaveLength(2);
    // Default-first sort — Gemma is the current default so it should lead.
    expect(cards[0].props.title).toBe('Gemma 4 31B');
    expect(cards[1].props.title).toBe('Claude Opus 4.6');

    // 4) Dispatch set-default on the non-default row against the active surface.
    updateMessageContent.mockClear();
    await act(async () => {
      const claimed = result.current.tryHandleA2UIAction(
        {
          type: ModelAdminActionTypes.SetDefault,
          componentId: 'btn-setdefault',
          payload: { registry_id: 'anthropic/claude-opus-4-6' },
        },
        surfaceId,
      );
      expect(claimed).toBe(true);
    });

    // 5) The mutation goes through adminSetDefaultModel, then a refresh.
    await waitFor(() =>
      expect(adminSetDefaultModel).toHaveBeenCalledWith('anthropic/claude-opus-4-6'),
    );
    await waitFor(() => expect(updateMessageContent).toHaveBeenCalled());
    const refreshed = decodeSurface(
      (updateMessageContent.mock.calls.at(-1) as [string, string])[1],
    );
    const refreshedCards = refreshed.components.filter((c: { type: string }) => c.type === 'card');
    // Claude is now the default → it leads after the refresh.
    expect(refreshedCards[0].props.title).toBe('Claude Opus 4.6');
    // The surface lives on the same local bubble — the reducer shows a
    // single assistant message whose content has been rewritten twice.
    const assistantBubbles = state.messages.filter((m) => m.role === 'assistant');
    expect(assistantBubbles).toHaveLength(1);
    expect(assistantBubbles[0].id).toBe(surfaceId);
  });

  it('ignores actions against a stale surface id (REQ-GAP-TEST-002c)', async () => {
    adminListModels.mockResolvedValue({ items: [entry('a/b')], total: 1, page: 1, size: 1 });
    const injectLocalMessage = vi.fn();
    const updateMessageContent = vi.fn();
    const { result } = renderHook(
      () => useModelAdminFlow({ injectLocalMessage, updateMessageContent }),
      { wrapper: I18nTestWrapper },
    );

    // Without /models having been called, currentIdRef is null so any id
    // is stale. The call returns true (claimed) but no API is hit.
    const claimed = result.current.tryHandleA2UIAction(
      {
        type: ModelAdminActionTypes.SetDefault,
        componentId: 'btn',
        payload: { registry_id: 'a/b' },
      },
      'stale-id',
    );
    expect(claimed).toBe(true);
    expect(adminSetDefaultModel).not.toHaveBeenCalled();
  });
});
