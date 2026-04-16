import type { TFunction } from 'i18next';

import { A2UI_MARKER } from './constants';

/**
 * Defence-in-depth sanitizer for the chat sidebar's `last_message_preview`
 * field. The backend already strips the A2UI marker server-side via
 * `persist.RenderablePreview` (see REQ-201/202 of
 * spec-process-bugfix-a2ui-rehydration-completion.md), but if any future
 * backend or migration leaves a raw marker or routing-JSON envelope in the
 * preview slot, this helper guarantees it never reaches the DOM.
 *
 * Returns `t('chat:sidebar.pendingForm')` for any input that looks like an
 * A2UI surface or a bare JSON object; otherwise returns the input unchanged.
 *
 * REQ-405, AC-405, INV-302.
 */
export function displayPreview(raw: string, t: TFunction): string {
  const trimmed = raw.trimStart();
  if (trimmed.startsWith(A2UI_MARKER)) {
    return t('chat:sidebar.pendingForm');
  }
  if (trimmed.startsWith('{"')) {
    return t('chat:sidebar.pendingForm');
  }
  return raw;
}
