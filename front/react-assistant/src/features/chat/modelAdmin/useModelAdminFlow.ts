import { useCallback, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { adminListModels, adminSetDefaultModel, adminLicenseReview, adminDeleteModel, AdminApiError } from '../../../services/api';
import type { A2UIAction } from '../a2ui/types';
import {
  buildModelAdminContent,
  buildLoadingContent,
  buildErrorContent,
  ModelAdminActionTypes,
  type TFunction,
} from './buildModelAdminSurface';

const MESSAGE_ID_PREFIX = 'model-admin-surface';

function errorMessage(err: unknown): string {
  if (err instanceof AdminApiError) {
    return `${err.status} — ${err.message}`;
  }
  if (err instanceof Error) {
    return err.message;
  }
  return 'unknown error';
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
 *
 * Per REQ-GAP-REG-005 this hook ONLY handles actions whose type starts with
 * `model:`. Unprefixed CPN-emitted actions (e.g. `submit_register`) fall
 * through so the parent can route them via the standard SSE `userAction`
 * pipeline — preserving REQ-FE-006 (no direct `fetch(/admin/models)` from
 * the CPN-driven submit path).
 */
export function useModelAdminFlow({
  injectLocalMessage,
  updateMessageContent,
}: UseModelAdminFlowOptions): UseModelAdminFlowReturn {
  const { t } = useTranslation('chat');
  // Cast once — react-i18next's `t` signature is intentionally loose; the
  // pure builder only needs (key, bag) → string so we pin that shape at
  // the seam instead of leaking i18next's union into the builder module.
  const tFn = t as unknown as TFunction;

  const [surfaceMessageId, setSurfaceMessageId] = useState<string | null>(null);
  // Keep the most recent surface-id in a ref so async callbacks don't close
  // over a stale value when handling rapid successive actions.
  const currentIdRef = useRef<string | null>(null);

  const refreshSurface = useCallback(
    async (messageId: string) => {
      try {
        const resp = await adminListModels();
        updateMessageContent(messageId, buildModelAdminContent(resp.items, tFn));
      } catch (err) {
        updateMessageContent(messageId, buildErrorContent(tFn, errorMessage(err)));
      }
    },
    [updateMessageContent, tFn],
  );

  const openSurface = useCallback(async () => {
    const id = `${MESSAGE_ID_PREFIX}-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
    setSurfaceMessageId(id);
    currentIdRef.current = id;
    injectLocalMessage(id, buildLoadingContent(tFn), 'models-admin');
    await refreshSurface(id);
  }, [injectLocalMessage, refreshSurface, tFn]);

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
      // REQ-GAP-REG-005: only this hook's `model:` namespace. CPN-emitted
      // form actions (`submit_register`, `apply_confirm`, …) fall through
      // so the parent router can ship them through the SSE pipeline.
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
          updateMessageContent(messageId, buildErrorContent(tFn, errorMessage(err)));
        }
      })();

      return true;
    },
    [refreshSurface, surfaceMessageId, updateMessageContent, tFn],
  );

  return { tryHandleSlashCommand, tryHandleA2UIAction };
}
