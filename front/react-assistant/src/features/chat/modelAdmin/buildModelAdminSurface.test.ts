import { describe, it, expect } from 'vitest';
import {
  buildModelAdminContent,
  buildLoadingContent,
  buildErrorContent,
  ModelAdminActionTypes,
  type TFunction,
} from './buildModelAdminSurface';
import type { ModelRegistryEntry } from '../../../types/setup';
import { A2UI_MARKER } from '../a2ui/constants';

/**
 * Minimal `t` that echoes the key so assertions can still inspect the
 * surface structure without pulling i18next into the test env. Any
 * interpolation variable is rendered inline so the visibility tests can
 * still read the label back.
 */
const t: TFunction = (key, opts) => {
  if (!opts) return key;
  const parts = Object.entries(opts).map(([k, v]) => `${k}=${String(v)}`);
  return `${key}(${parts.join(',')})`;
};

function makeEntry(overrides: Partial<ModelRegistryEntry>): ModelRegistryEntry {
  return {
    id: overrides.registry_id ?? 'vendor/family',
    registry_id: 'vendor/family',
    vendor: 'vendor',
    family: 'family',
    version: '1',
    display_name: 'Model',
    description: '',
    modalities: { input: ['text'], output: ['text'] },
    capabilities: {
      text: true, tools: false, streaming: true, reasoning: false,
      structured_output: false, vision: false, audio: false,
    },
    context: { length: 128_000, tokenizer: 'gpt' },
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

function parsePayload(content: string) {
  expect(content.startsWith(A2UI_MARKER)).toBe(true);
  return JSON.parse(content.slice(A2UI_MARKER.length));
}

describe('buildModelAdminContent', () => {
  it('prefixes the A2UI marker on the returned string', () => {
    const out = buildModelAdminContent([], t);
    expect(out.startsWith(A2UI_MARKER)).toBe(true);
  });

  it('sorts: product default first, then invokable, then alphabetical', () => {
    const entries: ModelRegistryEntry[] = [
      makeEntry({ registry_id: 'zzz/zeta',    display_name: 'Zeta',    invokable: true,  is_product_default: false }),
      makeEntry({ registry_id: 'aaa/alpha',   display_name: 'Alpha',   invokable: false, is_product_default: false }),
      makeEntry({ registry_id: 'mmm/middle',  display_name: 'Middle',  invokable: true,  is_product_default: false }),
      makeEntry({ registry_id: 'def/default', display_name: 'Default', invokable: true,  is_product_default: true  }),
    ];

    const parsed = parsePayload(buildModelAdminContent(entries, t));
    // Header, summaryAlert, <cards>, divider, refresh-button
    const cards = parsed.components.filter((c: { type: string }) => c.type === 'card');
    const titles = cards.map((c: { props: { title: string } }) => c.props.title);
    expect(titles).toEqual(['Default', 'Middle', 'Zeta', 'Alpha']);
  });

  it('hides set-default + delete on the current product default', () => {
    const entry = makeEntry({ is_product_default: true, invokable: true });
    const parsed = parsePayload(buildModelAdminContent([entry], t));
    const card = parsed.components.find((c: { type: string }) => c.type === 'card');
    const buttons = card.children
      .flatMap((ch: { type: string; children?: Array<{ props: { actionType: string; disabled?: boolean } }> }) =>
        ch.type === 'row' ? ch.children ?? [] : [],
      )
      .filter((b: { type?: string }) => b.type === 'button');

    // set-default button must not be emitted at all on the default row.
    const hasSetDefault = buttons.some((b: { props: { actionType: string } }) =>
      b.props.actionType === ModelAdminActionTypes.SetDefault,
    );
    expect(hasSetDefault).toBe(false);

    // delete button still renders but must be disabled so the UI reflects
    // the FK RESTRICT invariant (cannot drop the current default).
    const del = buttons.find((b: { props: { actionType: string } }) =>
      b.props.actionType === ModelAdminActionTypes.Delete,
    );
    expect(del).toBeTruthy();
    expect(del.props.disabled).toBe(true);
  });

  it('hides license-approve-commercial on an already commercial row', () => {
    const entry = makeEntry({
      license: { kind: 'permissive', source: 'manual', status: 'approved-commercial' },
    });
    const parsed = parsePayload(buildModelAdminContent([entry], t));
    const card = parsed.components.find((c: { type: string }) => c.type === 'card');
    const buttons = card.children
      .flatMap((ch: { type: string; children?: Array<{ props: { actionType: string } }> }) =>
        ch.type === 'row' ? ch.children ?? [] : [],
      )
      .filter((b: { type?: string }) => b.type === 'button');
    const hasApproveCommercial = buttons.some(
      (b: { props: { actionType: string } }) => b.props.actionType === ModelAdminActionTypes.LicenseApproveCommercial,
    );
    expect(hasApproveCommercial).toBe(false);
  });

  it('hides block on an already blocked row', () => {
    const entry = makeEntry({
      invokable: false,
      license: { kind: 'permissive', source: 'manual', status: 'blocked' },
    });
    const parsed = parsePayload(buildModelAdminContent([entry], t));
    const card = parsed.components.find((c: { type: string }) => c.type === 'card');
    const buttons = card.children
      .flatMap((ch: { type: string; children?: Array<{ props: { actionType: string } }> }) =>
        ch.type === 'row' ? ch.children ?? [] : [],
      )
      .filter((b: { type?: string }) => b.type === 'button');
    const hasBlock = buttons.some(
      (b: { props: { actionType: string } }) => b.props.actionType === ModelAdminActionTypes.LicenseBlock,
    );
    expect(hasBlock).toBe(false);
  });

  it('summary alert severity is warn when there is ANY unreviewed row', () => {
    const entries: ModelRegistryEntry[] = [
      makeEntry({ registry_id: 'a/b', license: { kind: 'permissive', source: 'manual', status: 'approved-commercial' } }),
      makeEntry({ registry_id: 'c/d', license: { kind: 'permissive', source: 'manual', status: 'unreviewed' }, invokable: false }),
    ];
    const parsed = parsePayload(buildModelAdminContent(entries, t));
    const alert = parsed.components.find((c: { type: string }) => c.type === 'alert');
    expect(alert.props.severity).toBe('warn');
  });

  it('summary alert severity is info when all rows are approved', () => {
    const entries: ModelRegistryEntry[] = [
      makeEntry({ registry_id: 'a/b', license: { kind: 'permissive', source: 'manual', status: 'approved-commercial' } }),
      makeEntry({ registry_id: 'c/d', license: { kind: 'permissive', source: 'manual', status: 'approved-non-commercial' } }),
    ];
    const parsed = parsePayload(buildModelAdminContent(entries, t));
    const alert = parsed.components.find((c: { type: string }) => c.type === 'alert');
    expect(alert.props.severity).toBe('info');
  });

  it('routes every button label through the translator (AC-I18N-003)', () => {
    const entries: ModelRegistryEntry[] = [
      makeEntry({
        registry_id: 'x/y',
        display_name: 'XY',
        license: { kind: 'permissive', source: 'manual', status: 'unreviewed' },
      }),
    ];
    const parsed = parsePayload(buildModelAdminContent(entries, t));
    const refresh = parsed.components.find(
      (c: { type: string; props: { actionType?: string } }) =>
        c.type === 'button' && c.props.actionType === ModelAdminActionTypes.Refresh,
    );
    // Echo translator returns the key as-is → proves lookup happened.
    expect(refresh.props.label).toBe('models.list.actions.refresh');
  });

  it('passes the current row count into the summary bag', () => {
    const entries: ModelRegistryEntry[] = [
      makeEntry({ registry_id: 'a/b' }),
      makeEntry({ registry_id: 'c/d' }),
    ];
    const parsed = parsePayload(buildModelAdminContent(entries, t));
    const alert = parsed.components.find((c: { type: string }) => c.type === 'alert');
    expect(alert.props.message).toContain('count=2');
  });
});

describe('buildLoadingContent / buildErrorContent', () => {
  it('loading content carries the A2UI marker and the loading i18n key', () => {
    const out = buildLoadingContent(t);
    expect(out.startsWith(A2UI_MARKER)).toBe(true);
    const parsed = JSON.parse(out.slice(A2UI_MARKER.length));
    expect(parsed.components[0].props.content).toBe('models.list.loading');
  });

  it('error content carries the error title + raw message', () => {
    const out = buildErrorContent(t, '500 — kaboom');
    const parsed = JSON.parse(out.slice(A2UI_MARKER.length));
    expect(parsed.components[0].type).toBe('alert');
    expect(parsed.components[0].props.severity).toBe('error');
    expect(parsed.components[0].props.title).toBe('models.list.error.title');
    expect(parsed.components[0].props.message).toBe('500 — kaboom');
  });
});
