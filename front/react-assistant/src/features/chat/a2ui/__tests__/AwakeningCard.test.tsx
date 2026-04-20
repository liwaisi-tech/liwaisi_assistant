import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { I18nTestWrapper } from '../../../../test/i18n-test-utils';
import { A2UIMessageRenderer } from '../A2UIMessageRenderer';
import type { A2UIPayload } from '../types';

// Canonical awakening payload from spec §4.4.
const AWAKENING_PAYLOAD: A2UIPayload = {
  components: [
    {
      type: 'card',
      props: { variant: 'info', title: 'brae est\u00e1 listo' },
      children: [
        {
          type: 'text',
          props: { content: 'Despert\u00e9 en Alpine 3.21 (aarch64, kernel 6.17). Shell: busybox sh.' },
        },
        { type: 'divider', props: {} },
        {
          type: 'stack',
          props: {},
          children: [
            { type: 'text', props: { content: '**Tengo:** sh, awk, sed, grep, tar, wget' } },
            { type: 'text', props: { content: '**Me falta:** git, python, node, go, gcc' } },
          ],
        },
      ],
    },
    { type: 'text', props: { content: '\u00bfEn qu\u00e9 trabajamos hoy?' } },
  ],
};

describe('Awakening A2UI card (spec §4.4)', () => {
  it('renders the info-variant card with title, body, stack items and follow-up prompt', () => {
    render(
      <A2UIMessageRenderer payload={AWAKENING_PAYLOAD} isStreaming={false} onAction={vi.fn()} />,
      { wrapper: I18nTestWrapper },
    );

    // Distinctive info card container is present with the dedicated test id
    // so the visual treatment is locked by a stable contract.
    expect(screen.getByTestId('awakening-card')).toBeInTheDocument();

    // Title appears, rendered with the JetBrains-Mono display typography.
    expect(screen.getByText('brae est\u00e1 listo')).toBeInTheDocument();

    // Body narrative.
    expect(
      screen.getByText(/Despert\u00e9 en Alpine 3\.21 \(aarch64, kernel 6\.17\)/),
    ).toBeInTheDocument();

    // Stack children render as inline markdown — the bold segments become
    // <strong>Tengo:</strong> / <strong>Me falta:</strong>. Assert the
    // bolded labels survived the markdown pass.
    expect(screen.getByText('Tengo:')).toBeInTheDocument();
    expect(screen.getByText('Me falta:')).toBeInTheDocument();

    // Follow-up prompt (BEH-003 — awakening is not a dead-end monologue).
    expect(screen.getByText('\u00bfEn qu\u00e9 trabajamos hoy?')).toBeInTheDocument();
  });

  it('falls back to the default card chrome for non-info variants', () => {
    render(
      <A2UIMessageRenderer
        payload={{
          components: [
            {
              type: 'card',
              props: { title: 'plain' },
              children: [{ type: 'text', props: { content: 'body' } }],
            },
          ],
        }}
        isStreaming={false}
        onAction={vi.fn()}
      />,
      { wrapper: I18nTestWrapper },
    );
    // The info card emits a data-testid we can negate against to prove the
    // default card stays on its pre-awakening visual path.
    expect(screen.queryByTestId('awakening-card')).not.toBeInTheDocument();
    expect(screen.getByText('plain')).toBeInTheDocument();
  });
});
