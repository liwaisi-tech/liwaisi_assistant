import { useCallback, useRef, useState } from 'react';
import { adminListModels, adminSetDefaultModel, adminLicenseReview, adminDeleteModel, AdminApiError } from '../../../services/api';
import type { A2UIAction } from '../a2ui/types';
import { buildModelAdminContent, ModelAdminActionTypes } from './buildModelAdminSurface';
import { A2UI_MARKER } from '../a2ui/constants';

const MESSAGE_ID_PREFIX = 'model-admin-surface';

function errorContent(err: unknown): string {
  const message = err instanceof AdminApiError
    ? `${err.status} — ${err.message}`
    : err instanceof Error
      ? err.message
      : 'unknown error';
  const payload = {
    components: [
      { type: 'alert', props: { severity: 'error', title: 'Model admin error', message } },
    ],
  };
  return `${A2UI_MARKER}${JSON.stringify(payload)}`;
}

function loadingContent(): string {
  const payload = {
    components: [
      { type: 'text', props: { content: 'Loading model registry…' } },
    ],
  };
  return `${A2UI_MARKER}${JSON.stringify(payload)}`;
}

export interface UseModelAdminFlowOptions {
  injectLocalMessage: (id: string, content: string, cpnRole?: string) => void;
  updateMessageContent: (id: string, content: string) => void;
}

export interface UseModelAdminFlowReturn {
  /** Returns true if the input was a model-admin slash command and was handled. */
  tryHandleSlashCommand: (content: string) => boolean;
  /** Returns true if the A2UI action was a model-admin action and was handled. */
  tryHandleA2UIAction: (action: A2UIAction, messageId: string) => boolean;
}

/**
 * Intercepts the `/models` slash command and dispatches model-admin A2UI
 * actions against the backend admin endpoints, refreshing the surface in
 * place on every mutation. No CPN / SSE involvement: the surface lives as
 * a local assistant message whose `content` is rewritten after each call.
 */
export function useModelAdminFlow({
  injectLocalMessage,
  updateMessageContent,
}: UseModelAdminFlowOptions): UseModelAdminFlowReturn {
  const [surfaceMessageId, setSurfaceMessageId] = useState<string | null>(null);
  // Keep the most recent surface-id in a ref so async callbacks don't close
  // over a stale value when handling rapid successive actions.
  const currentIdRef = useRef<string | null>(null);

  const refreshSurface = useCallback(
    async (messageId: string) => {
      try {
        const resp = await adminListModels();
        updateMessageContent(messageId, buildModelAdminContent(resp.items));
      } catch (err) {
        updateMessageContent(messageId, errorContent(err));
      }
    },
    [updateMessageContent],
  );

  const openSurface = useCallback(async () => {
    const id = `${MESSAGE_ID_PREFIX}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    setSurfaceMessageId(id);
    currentIdRef.current = id;
    injectLocalMessage(id, loadingContent(), 'models-admin');
    await refreshSurface(id);
  }, [injectLocalMessage, refreshSurface]);

  const tryHandleSlashCommand = useCallback(
    (content: string): boolean => {
      const trimmed = content.trim().toLowerCase();
      if (trimmed === '/models' || trimmed === '/model' || trimmed === '/modelos') {
        void openSurface();
        return true;
      }
      return false;
    },
    [openSurface],
  );

  const tryHandleA2UIAction = useCallback(
    (action: A2UIAction, messageId: string): boolean => {
      if (!action.type.startsWith('model:')) return false;
      if (messageId !== currentIdRef.current && messageId !== surfaceMessageId) {
        // The action came from a stale surface (e.g. an older bubble) — ignore
        // to avoid split-brain state. Users can /models again to get a fresh one.
        return true;
      }
      const payload = (action.payload ?? {}) as { registry_id?: string };
      const registryID = payload.registry_id ?? '';

      (async () => {
        try {
          switch (action.type) {
            case ModelAdminActionTypes.Refresh:
              await refreshSurface(messageId);
              return;
            case ModelAdminActionTypes.SetDefault:
              if (!registryID) return;
              await adminSetDefaultModel(registryID);
              break;
            case ModelAdminActionTypes.LicenseApproveCommercial:
              if (!registryID) return;
              await adminLicenseReview(registryID, 'approved-commercial');
              break;
            case ModelAdminActionTypes.LicenseApproveNonCommercial:
              if (!registryID) return;
              await adminLicenseReview(registryID, 'approved-non-commercial');
              break;
            case ModelAdminActionTypes.LicenseRestrict:
              if (!registryID) return;
              await adminLicenseReview(registryID, 'restricted');
              break;
            case ModelAdminActionTypes.LicenseBlock:
              if (!registryID) return;
              await adminLicenseReview(registryID, 'blocked');
              break;
            case ModelAdminActionTypes.Delete:
              if (!registryID) return;
              await adminDeleteModel(registryID);
              break;
            default:
              return;
          }
          await refreshSurface(messageId);
        } catch (err) {
          updateMessageContent(messageId, errorContent(err));
        }
      })();

      return true;
    },
    [refreshSurface, surfaceMessageId, updateMessageContent],
  );

  return { tryHandleSlashCommand, tryHandleA2UIAction };
}
