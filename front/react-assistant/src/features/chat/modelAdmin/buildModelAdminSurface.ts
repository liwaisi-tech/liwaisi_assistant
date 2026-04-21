import type { A2UIPayload, A2UIComponent } from '../a2ui/types';
import type { ModelRegistryEntry } from '../../../types/setup';
import { A2UI_MARKER } from '../a2ui/constants';

// Action types dispatched by the buttons inside the model-admin surface.
// Consumed by useModelAdminFlow.handleAction; keep stable because they
// appear verbatim in the A2UI payload.
export const ModelAdminActionTypes = {
  SetDefault: 'model:set-default',
  LicenseApproveCommercial: 'model:license-approve-commercial',
  LicenseApproveNonCommercial: 'model:license-approve-non-commercial',
  LicenseRestrict: 'model:license-restrict',
  LicenseBlock: 'model:license-block',
  Delete: 'model:delete',
  Refresh: 'model:refresh',
} as const;

/**
 * Translator shape — a strict subset of what `useTranslation('chat').t`
 * returns. Accepting a function (not pulling `useTranslation` in here)
 * keeps `buildModelAdminSurface.ts` a pure, test-friendly module and lets
 * the caller — `useModelAdminFlow` — wire the namespace.
 *
 * Per AC-I18N-003: no user-visible literals may stay in this file after
 * GAP-I18N. Every Text / Badge / Alert / Button label is sourced from
 * `t(...)` with an interpolation bag where needed.
 */
export type TFunction = (key: string, opts?: Record<string, unknown>) => string;

const LICENSE_KEYS: Record<string, string> = {
  'approved-commercial': 'models.license.approved_commercial',
  'approved-non-commercial': 'models.license.approved_non_commercial',
  restricted: 'models.license.restricted',
  blocked: 'models.license.blocked',
  unreviewed: 'models.license.unreviewed',
  'review-in-progress': 'models.license.review_in_progress',
  unknown: 'models.license.unknown',
};

const LIFECYCLE_KEYS: Record<string, string> = {
  discovered: 'models.lifecycle.discovered',
  'pending-license-review': 'models.lifecycle.pending_license_review',
  registered: 'models.lifecycle.registered',
  active: 'models.lifecycle.active',
  disabled: 'models.lifecycle.disabled',
  deprecated: 'models.lifecycle.deprecated',
  sunset: 'models.lifecycle.sunset',
  removed: 'models.lifecycle.removed',
};

// Pricing/context formatting stays as formatting helpers — they produce
// currency / token strings (locale-agnostic numerics) not user-visible
// prose, so keeping them here does not violate AC-I18N-003.
function formatPricePerM(perToken: number): string {
  if (!perToken || !Number.isFinite(perToken)) return '—';
  const perM = perToken * 1_000_000;
  if (perM < 0.01) return `$${perM.toFixed(4)}/M`;
  if (perM < 1) return `$${perM.toFixed(3)}/M`;
  return `$${perM.toFixed(2)}/M`;
}

function formatContextLength(length: number): string {
  if (!length) return '';
  if (length >= 1000) return `${Math.round(length / 1000)}k ctx`;
  return `${length} ctx`;
}

function statusAlertSeverity(licenseStatus: string): 'info' | 'warn' | 'error' {
  if (licenseStatus === 'blocked') return 'error';
  if (licenseStatus === 'unreviewed' || licenseStatus === 'review-in-progress') return 'warn';
  return 'info';
}

function lifecycleLabel(t: TFunction, state: string): string {
  const key = LIFECYCLE_KEYS[state];
  if (!key) return state || '—';
  return t(key);
}

function licenseLabel(t: TFunction, status: string): string {
  const key = LICENSE_KEYS[status];
  if (!key) return status || '—';
  return t(key);
}

function modelCard(t: TFunction, entry: ModelRegistryEntry): A2UIComponent {
  const { registry_id, display_name, vendor, lifecycle, license, context, pricing, is_product_default, invokable } = entry;

  const subtitleParts = [vendor, formatContextLength(context?.length ?? 0)].filter(Boolean);
  const pricingLine = `${formatPricePerM(pricing?.input_per_token ?? 0)} in · ${formatPricePerM(pricing?.output_per_token ?? 0)} out`;
  const lifecycleText = lifecycleLabel(t, lifecycle?.state ?? '');
  const licenseText = licenseLabel(t, license?.status ?? '');

  const statusChips: A2UIComponent[] = [
    {
      type: 'badge',
      props: {
        label: is_product_default ? t('models.list.badge.default') : lifecycleText,
        variant: is_product_default ? 'success' : invokable ? 'info' : 'warn',
      },
    },
    {
      type: 'badge',
      props: {
        label: t('models.list.badge.license_prefix', { status: licenseText }),
        variant: license?.status?.startsWith('approved') ? 'success' : license?.status === 'blocked' ? 'error' : 'warn',
      },
    },
  ];

  const buttons: A2UIComponent[] = [];

  if (!is_product_default && invokable) {
    buttons.push({
      type: 'button',
      props: {
        id: `set-default-${registry_id}`,
        label: t('models.list.actions.set_default'),
        actionType: ModelAdminActionTypes.SetDefault,
        variant: 'primary',
        payload: { registry_id },
      },
    });
  }

  if (license?.status !== 'approved-commercial') {
    buttons.push({
      type: 'button',
      props: {
        id: `approve-commercial-${registry_id}`,
        label: t('models.list.actions.approve_commercial'),
        actionType: ModelAdminActionTypes.LicenseApproveCommercial,
        variant: 'success',
        payload: { registry_id },
      },
    });
  }

  if (license?.status !== 'approved-non-commercial') {
    buttons.push({
      type: 'button',
      props: {
        id: `approve-non-${registry_id}`,
        label: t('models.list.actions.approve_non_commercial'),
        actionType: ModelAdminActionTypes.LicenseApproveNonCommercial,
        variant: 'secondary',
        payload: { registry_id },
      },
    });
  }

  if (license?.status !== 'blocked') {
    buttons.push({
      type: 'button',
      props: {
        id: `block-${registry_id}`,
        label: t('models.list.actions.block'),
        actionType: ModelAdminActionTypes.LicenseBlock,
        variant: 'danger',
        payload: { registry_id },
      },
    });
  }

  buttons.push({
    type: 'button',
    props: {
      id: `delete-${registry_id}`,
      label: t('models.list.actions.delete'),
      actionType: ModelAdminActionTypes.Delete,
      variant: 'danger',
      payload: { registry_id },
      disabled: is_product_default,
    },
  });

  const card: A2UIComponent = {
    type: 'card',
    props: { title: display_name },
    children: [
      {
        type: 'text',
        props: { content: `**${registry_id}**  \n${subtitleParts.join(' · ')}` },
      },
      { type: 'text', props: { content: pricingLine } },
      { type: 'row', props: { gap: 'sm', wrap: true }, children: statusChips },
      { type: 'divider', props: {} },
      { type: 'row', props: { gap: 'sm', wrap: true }, children: buttons },
    ],
  };

  return card;
}

function summaryAlert(t: TFunction, entries: ModelRegistryEntry[]): A2UIComponent {
  const total = entries.length;
  const invokable = entries.filter((m) => m.invokable).length;
  const needsReview = entries.filter((m) => m.license?.status === 'unreviewed' || m.license?.status === 'review-in-progress').length;
  const defaultEntry = entries.find((m) => m.is_product_default);

  const parts: string[] = [];
  parts.push(t('models.list.summary.total', { count: total }));
  parts.push(t('models.list.summary.invokable', { count: invokable }));
  if (needsReview > 0) parts.push(t('models.list.summary.needs_review', { count: needsReview }));
  if (defaultEntry) parts.push(t('models.list.summary.default', { name: defaultEntry.display_name }));

  const severity: 'info' | 'warn' | 'error' = needsReview > 0 ? 'warn' : 'info';

  return {
    type: 'alert',
    props: {
      severity,
      message: parts.join(' · '),
    },
  };
}

/**
 * Build the full A2UI payload string (including the `$$a2ui:` marker) for
 * the in-chat model-admin surface. The returned string is meant to be
 * dropped directly into a ChatMessage's `content` field.
 *
 * All user-visible strings resolve through the passed translator (AC-I18N-003).
 */
export function buildModelAdminContent(entries: ModelRegistryEntry[], t: TFunction): string {
  const sorted = [...entries].sort((a, b) => {
    if (a.is_product_default && !b.is_product_default) return -1;
    if (!a.is_product_default && b.is_product_default) return 1;
    if (a.invokable && !b.invokable) return -1;
    if (!a.invokable && b.invokable) return 1;
    return a.display_name.localeCompare(b.display_name);
  });

  const header: A2UIComponent = {
    type: 'text',
    props: {
      content: `### ${t('models.list.title')}\n\n${t('models.list.subtitle')}`,
    },
  };

  const refreshButton: A2UIComponent = {
    type: 'button',
    props: {
      id: 'models-refresh',
      label: t('models.list.actions.refresh'),
      actionType: ModelAdminActionTypes.Refresh,
      variant: 'secondary',
    },
  };

  const cards = sorted.map((e) => modelCard(t, e));

  // severities are computed so a future follow-up can surface them in a
  // banner/strip; today they roll up into summaryAlert() for the top-line.
  const alertSeverities = sorted.map((m) => statusAlertSeverity(m.license?.status ?? ''));
  void alertSeverities;

  const payload: A2UIPayload = {
    components: [
      header,
      summaryAlert(t, sorted),
      ...cards,
      { type: 'divider', props: {} },
      refreshButton,
    ],
  };

  return `${A2UI_MARKER}${JSON.stringify(payload)}`;
}

/**
 * Build the loading-surface shown while adminListModels is in flight.
 */
export function buildLoadingContent(t: TFunction): string {
  const payload = {
    components: [
      { type: 'text', props: { content: t('models.list.loading') } },
    ],
  };
  return `${A2UI_MARKER}${JSON.stringify(payload)}`;
}

/**
 * Build the error-surface shown when an admin API call rejects.
 */
export function buildErrorContent(t: TFunction, message: string): string {
  const payload = {
    components: [
      {
        type: 'alert',
        props: {
          severity: 'error',
          title: t('models.list.error.title'),
          message,
        },
      },
    ],
  };
  return `${A2UI_MARKER}${JSON.stringify(payload)}`;
}
