import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { I18nTestWrapper } from '../../../test/i18n-test-utils';
import { HostApprovalCard } from './HostApprovalCard';
import type { HostApprovalPayload, HostApprovalRiskBand } from '../a2ui/types';

const wrapper = I18nTestWrapper;

function buildPayload(overrides: Partial<HostApprovalPayload> = {}): HostApprovalPayload {
  return {
    operation: 'exec',
    command: 'gcc hello.c -o hello',
    risk_band: 'caution',
    rationale: 'El forge quiere compilar el binario que acaba de generar.',
    alternatives: ['usar /usr/bin/tcc', 'correr la versión precompilada del caché'],
    ...overrides,
  };
}

describe('HostApprovalCard', () => {
  it('renders the command, rationale, operation pill and risk badge (caution)', () => {
    render(
      <HostApprovalCard payload={buildPayload()} onRespond={vi.fn()} />,
      { wrapper },
    );

    expect(screen.getByTestId('host-approval-card')).toBeInTheDocument();
    expect(screen.getByTestId('host-approval-card')).toHaveAttribute(
      'data-risk-band',
      'caution',
    );
    expect(screen.getByTestId('host-approval-command')).toHaveTextContent(
      'gcc hello.c -o hello',
    );
    // Operation pill shows the Spanish label for 'exec'.
    expect(screen.getByTestId('host-approval-operation-pill')).toHaveTextContent(
      /ejecutar comando/i,
    );
    // Risk badge shows the Spanish risk label — 'Precaución'.
    expect(screen.getByTestId('host-approval-risk-badge')).toHaveTextContent(/precauci/i);
    // Rationale is rendered verbatim.
    expect(screen.getByText(/El forge quiere compilar/)).toBeInTheDocument();
  });

  it('renders risk-band styling per band (REQ-041)', () => {
    const bands: HostApprovalRiskBand[] = ['safe', 'caution', 'dangerous'];
    for (const band of bands) {
      const { unmount } = render(
        <HostApprovalCard payload={buildPayload({ risk_band: band })} onRespond={vi.fn()} />,
        { wrapper },
      );
      const card = screen.getByTestId('host-approval-card');
      expect(card).toHaveAttribute('data-risk-band', band);
      // Risk pill text changes per band.
      const badge = screen.getByTestId('host-approval-risk-badge');
      if (band === 'safe') expect(badge).toHaveTextContent(/seguro/i);
      if (band === 'caution') expect(badge).toHaveTextContent(/precauci/i);
      if (band === 'dangerous') expect(badge).toHaveTextContent(/peligroso/i);
      unmount();
    }
  });

  it('dangerous band disables "Aprobar y recordar" (never sticks) per AC-007', () => {
    render(
      <HostApprovalCard payload={buildPayload({ risk_band: 'dangerous' })} onRespond={vi.fn()} />,
      { wrapper },
    );
    const rememberBtn = screen.getByTestId('host-approval-approve-remember') as HTMLButtonElement;
    expect(rememberBtn).toBeDisabled();
    // The one-shot approve is still live because dangerous ops still let
    // the user authorise the single invocation.
    expect(screen.getByTestId('host-approval-approve-once')).not.toBeDisabled();
  });

  it('clicks map to correct action strings (REQ-042)', () => {
    const onRespond = vi.fn();
    render(
      <HostApprovalCard payload={buildPayload()} onRespond={onRespond} />,
      { wrapper },
    );

    fireEvent.click(screen.getByTestId('host-approval-approve-once'));
    expect(onRespond).toHaveBeenLastCalledWith('approve-once');

    fireEvent.click(screen.getByTestId('host-approval-approve-remember'));
    expect(onRespond).toHaveBeenLastCalledWith('approve-and-remember');

    fireEvent.click(screen.getByTestId('host-approval-deny'));
    expect(onRespond).toHaveBeenLastCalledWith('deny');

    fireEvent.click(screen.getByTestId('host-approval-deny-blacklist'));
    expect(onRespond).toHaveBeenLastCalledWith('deny-and-blacklist');
  });

  it('renders alternatives list when provided, hidden inside a collapsible', () => {
    render(
      <HostApprovalCard payload={buildPayload()} onRespond={vi.fn()} />,
      { wrapper },
    );
    // The <details> summary shows the count; individual alternatives
    // render eagerly but may be hidden by CSS until expand. We assert
    // that the text is in the DOM for AT access.
    expect(screen.getByText(/alternativas/i)).toBeInTheDocument();
    expect(screen.getByText('usar /usr/bin/tcc')).toBeInTheDocument();
  });

  it('locks the card when resolvedAction is set — rehydrates identically to live submit', () => {
    const onRespond = vi.fn();
    render(
      <HostApprovalCard
        payload={buildPayload()}
        resolvedAction="approve-once"
        resolvedAt={new Date('2026-04-17T14:30:00Z')}
        onRespond={onRespond}
      />,
      { wrapper },
    );
    // Buttons are all disabled.
    expect(screen.getByTestId('host-approval-approve-once')).toBeDisabled();
    expect(screen.getByTestId('host-approval-deny')).toBeDisabled();
    // The chosen one shows the check glyph + label.
    expect(screen.getByTestId('host-approval-approve-once')).toHaveTextContent(/\u2713.*Aprobar solo esta vez/);
    // "Respondido" line is present.
    expect(screen.getByTestId('host-approval-resolved-state')).toHaveTextContent(/Respondido/);
    // Clicking does NOT re-dispatch onRespond.
    fireEvent.click(screen.getByTestId('host-approval-deny'));
    expect(onRespond).not.toHaveBeenCalled();
  });

  it('shows a "Respondido" state with the chosen action — tests all four actions for rehydration parity', () => {
    const actions: Array<{
      action: Parameters<typeof HostApprovalCard>[0]['resolvedAction'];
      match: RegExp;
    }> = [
      { action: 'approve-once', match: /Aprobaste esta vez/ },
      { action: 'approve-and-remember', match: /Aprobado y recordado/ },
      { action: 'deny', match: /Rechazaste/ },
      { action: 'deny-and-blacklist', match: /bloqueado/i },
    ];
    for (const { action, match } of actions) {
      const { unmount } = render(
        <HostApprovalCard
          payload={buildPayload()}
          resolvedAction={action}
          onRespond={vi.fn()}
        />,
        { wrapper },
      );
      expect(screen.getByTestId('host-approval-resolved-state')).toHaveTextContent(match);
      unmount();
    }
  });

  it('renders an error row with optional retry when `error` is set', () => {
    const onRespond = vi.fn();
    const onRetry = vi.fn();
    render(
      <HostApprovalCard
        payload={buildPayload()}
        onRespond={onRespond}
        error="Falló el envío — reintenta"
        onRetry={onRetry}
      />,
      { wrapper },
    );
    const err = screen.getByTestId('host-approval-error');
    expect(err).toHaveTextContent('Falló el envío — reintenta');
    const retry = screen.getByRole('button', { name: /reintentar/i });
    fireEvent.click(retry);
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it('renders a defensive "forbidden" banner and disables approve buttons when band=forbidden', () => {
    render(
      <HostApprovalCard
        payload={buildPayload({ risk_band: 'forbidden', command: 'rm -rf /' })}
        onRespond={vi.fn()}
      />,
      { wrapper },
    );
    expect(
      screen.getByText(/lista de prohibidos/i),
    ).toBeInTheDocument();
    expect(screen.getByTestId('host-approval-approve-once')).toBeDisabled();
    expect(screen.getByTestId('host-approval-approve-remember')).toBeDisabled();
    // Deny paths still allowed so the user can ACK/discard the UI state.
    expect(screen.getByTestId('host-approval-deny')).not.toBeDisabled();
  });

  it('truncates long commands and expands them on click', () => {
    const long =
      'for f in $(find /var/log -type f -name "*.log" -mtime -1); do grep -E "ERROR|FATAL" "$f"; done';
    render(
      <HostApprovalCard payload={buildPayload({ command: long })} onRespond={vi.fn()} />,
      { wrapper },
    );
    const cmd = screen.getByTestId('host-approval-command');
    // Before click the aria-expanded flag is false.
    expect(cmd).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(cmd);
    expect(cmd).toHaveAttribute('aria-expanded', 'true');
    // And the full command is present in the DOM regardless of display
    // wrapping so screen readers get the whole thing.
    expect(cmd.textContent).toContain('for f in');
  });

  // ── Single-gate spec additions (spec-process-bugfix-tool-hitl-single-gate) ──

  it('AC-002 — renders clean parsed command and collapses raw invocation JSON', () => {
    render(
      <HostApprovalCard
        payload={buildPayload({
          command: 'uname -a && whoami',
          invocation: {
            tool: 'bash_exec',
            args: { command: 'bash', args: ['-c', 'uname -a && whoami'] },
          },
        })}
        onRespond={vi.fn()}
      />,
      { wrapper },
    );
    // Primary COMANDO block shows the parsed shell command only — never
    // the raw JSON wrapper.
    const cmdBlock = screen.getByTestId('host-approval-command');
    expect(cmdBlock).toHaveTextContent('uname -a && whoami');
    expect(cmdBlock.textContent).not.toContain('bash_exec');
    // The disclosure is rendered but collapsed by default.
    const details = screen.getByTestId('host-approval-technical-details') as HTMLDetailsElement;
    expect(details).toBeInTheDocument();
    expect(details.open).toBe(false);
    // The raw JSON block lives inside the disclosure and carries the
    // tool + args payload verbatim.
    const pre = screen.getByTestId('host-approval-invocation-json');
    expect(pre.textContent).toContain('"tool": "bash_exec"');
    expect(pre.textContent).toContain('"uname -a && whoami"');
  });

  it('toggles the technical-details disclosure on click', () => {
    render(
      <HostApprovalCard
        payload={buildPayload({
          invocation: { tool: 'bash_exec', args: { command: 'bash', args: ['-c', 'ls'] } },
        })}
        onRespond={vi.fn()}
      />,
      { wrapper },
    );
    const details = screen.getByTestId('host-approval-technical-details') as HTMLDetailsElement;
    expect(details.open).toBe(false);
    // Native <details> flips `open` on summary click and dispatches `toggle`
    // on the element. jsdom supports both; we drive the summary click so the
    // render path (including the `onToggle` handler) exercises end-to-end.
    const summary = details.querySelector('summary')!;
    fireEvent.click(summary);
    details.dispatchEvent(new Event('toggle'));
    expect(details.open).toBe(true);
  });

  it('does not render the disclosure when invocation is absent (legacy payload)', () => {
    render(
      <HostApprovalCard payload={buildPayload()} onRespond={vi.fn()} />,
      { wrapper },
    );
    expect(screen.queryByTestId('host-approval-technical-details')).toBeNull();
  });

  it('AC-008 — hides "Aprobar y recordar" when rememberAvailable=false', () => {
    render(
      <HostApprovalCard
        payload={buildPayload({ rememberAvailable: false })}
        onRespond={vi.fn()}
      />,
      { wrapper },
    );
    expect(screen.queryByTestId('host-approval-approve-remember')).toBeNull();
    // Other buttons stay in place.
    expect(screen.getByTestId('host-approval-approve-once')).toBeInTheDocument();
    expect(screen.getByTestId('host-approval-deny')).toBeInTheDocument();
    expect(screen.getByTestId('host-approval-deny-blacklist')).toBeInTheDocument();
  });

  it('renders the remember button when rememberAvailable is undefined or true', () => {
    const { unmount } = render(
      <HostApprovalCard payload={buildPayload()} onRespond={vi.fn()} />,
      { wrapper },
    );
    expect(screen.getByTestId('host-approval-approve-remember')).toBeInTheDocument();
    unmount();
    render(
      <HostApprovalCard
        payload={buildPayload({ rememberAvailable: true })}
        onRespond={vi.fn()}
      />,
      { wrapper },
    );
    expect(screen.getByTestId('host-approval-approve-remember')).toBeInTheDocument();
  });

  it('maps the new `risk` field — `danger` → dangerous band', () => {
    render(
      <HostApprovalCard
        payload={buildPayload({ risk: 'danger', risk_band: undefined })}
        onRespond={vi.fn()}
      />,
      { wrapper },
    );
    expect(screen.getByTestId('host-approval-card')).toHaveAttribute(
      'data-risk-band',
      'dangerous',
    );
  });

  it('AC-005 — renders "Aprobación obsoleta" when obsolete=true and all actions are disabled', () => {
    const onRespond = vi.fn();
    render(
      <HostApprovalCard
        payload={buildPayload()}
        obsolete
        onRespond={onRespond}
      />,
      { wrapper },
    );
    expect(screen.getByTestId('host-approval-obsolete-notice')).toHaveTextContent(
      /Obsolete approval|Aprobación obsoleta/,
    );
    // Buttons lock so the user cannot retry against a dead transition.
    expect(screen.getByTestId('host-approval-approve-once')).toBeDisabled();
    expect(screen.getByTestId('host-approval-deny')).toBeDisabled();
    fireEvent.click(screen.getByTestId('host-approval-deny'));
    expect(onRespond).not.toHaveBeenCalled();
  });

  it('prefers payload.title over the default when provided', () => {
    render(
      <HostApprovalCard
        payload={buildPayload({ title: 'Compilación solicitada' })}
        onRespond={vi.fn()}
      />,
      { wrapper },
    );
    expect(screen.getByText('Compilación solicitada')).toBeInTheDocument();
  });
});
