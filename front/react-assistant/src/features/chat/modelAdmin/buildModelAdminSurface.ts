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

const LICENSE_LABELS: Record<string, string> = {
  'approved-commercial': 'Commercial',
  'approved-non-commercial': 'Non-commercial',
  restricted: 'Restricted',
  blocked: 'Blocked',
  unreviewed: 'Unreviewed',
  'review-in-progress': 'In review',
  unknown: 'Unknown',
};

const LIFECYCLE_LABELS: Record<string, string> = {
  discovered: 'Discovered',
  'pending-license-review': 'Pending review',
  registered: 'Registered',
  active: 'Active',
  disabled: 'Disabled',
  deprecated: 'Deprecated',
  sunset: 'Sunset',
  removed: 'Removed',
};

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

function modelCard(entry: ModelRegistryEntry): A2UIComponent {
  const { registry_id, display_name, vendor, lifecycle, license, context, pricing, is_product_default, invokable } = entry;

  const subtitleParts = [vendor, formatContextLength(context?.length ?? 0)].filter(Boolean);
  const pricingLine = `${formatPricePerM(pricing?.input_per_token ?? 0)} in · ${formatPricePerM(pricing?.output_per_token ?? 0)} out`;
  const lifecycleLabel = LIFECYCLE_LABELS[lifecycle?.state] ?? lifecycle?.state ?? '—';
  const licenseLabel = LICENSE_LABELS[license?.status] ?? license?.status ?? '—';

  const statusChips: A2UIComponent[] = [
    {
      type: 'badge',
      props: {
        label: is_product_default ? '★ Default' : lifecycleLabel,
        variant: is_product_default ? 'success' : invokable ? 'info' : 'warn',
      },
    },
    {
      type: 'badge',
      props: {
        label: `License: ${licenseLabel}`,
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
        label: 'Set as default',
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
        label: 'Approve commercial',
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
        label: 'Approve non-commercial',
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
        label: 'Block',
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
      label: 'Delete',
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

function summaryAlert(entries: ModelRegistryEntry[]): A2UIComponent {
  const total = entries.length;
  const invokable = entries.filter((m) => m.invokable).length;
  const needsReview = entries.filter((m) => m.license?.status === 'unreviewed' || m.license?.status === 'review-in-progress').length;
  const defaultEntry = entries.find((m) => m.is_product_default);

  const parts: string[] = [];
  parts.push(`${total} registered`);
  parts.push(`${invokable} invokable`);
  if (needsReview > 0) parts.push(`${needsReview} awaiting license review`);
  if (defaultEntry) parts.push(`default: **${defaultEntry.display_name}**`);

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
 */
export function buildModelAdminContent(entries: ModelRegistryEntry[]): string {
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
      content: '### Model registry\n\nPick an action. Only models with an approved license can become the product default.',
    },
  };

  const refreshButton: A2UIComponent = {
    type: 'button',
    props: {
      id: 'models-refresh',
      label: 'Refresh',
      actionType: ModelAdminActionTypes.Refresh,
      variant: 'secondary',
    },
  };

  const cards = sorted.map(modelCard);

  const alertSeverities = sorted.map((m) => statusAlertSeverity(m.license?.status ?? ''));
  void alertSeverities; // severities consumed only through summaryAlert()

  const payload: A2UIPayload = {
    components: [
      header,
      summaryAlert(sorted),
      ...cards,
      { type: 'divider', props: {} },
      refreshButton,
    ],
  };

  return `${A2UI_MARKER}${JSON.stringify(payload)}`;
}
