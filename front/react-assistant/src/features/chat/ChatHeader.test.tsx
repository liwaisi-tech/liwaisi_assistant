import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { I18nTestWrapper } from '../../test/i18n-test-utils';
import { ChatHeader } from './ChatHeader';

// Mock useAuth so we don't need a real AuthProvider in a pure UI test.
vi.mock('../../contexts/AuthContext', () => ({
  useAuth: () => ({
    user: null,
    token: null,
    isAuthenticated: false,
    isAdmin: false,
    notAllowed: false,
    dismissNotAllowed: () => {},
    logout: () => {},
    getToken: () => null,
  }),
}));

// loadNamespace is an async side-effect; no-op it in tests.
vi.mock('../../i18n/loadNamespace', () => ({
  loadNamespace: () => Promise.resolve(),
}));

function renderHeader(props: Partial<React.ComponentProps<typeof ChatHeader>> = {}) {
  return render(
    <I18nTestWrapper>
      <ChatHeader
        sessionState="idle"
        onClearConversation={() => {}}
        clearDisabled={false}
        {...props}
      />
    </I18nTestWrapper>
  );
}

describe('ChatHeader connection indicator (REQ-112)', () => {
  it('renders green steady dot when connectionState is connected', () => {
    renderHeader({ connectionState: 'connected' });
    const indicator = screen.getByTestId('chat-header-connection');
    expect(indicator).toHaveAttribute('data-state', 'connected');
    expect(indicator).toHaveAttribute('role', 'status');
    expect(indicator).toHaveAttribute('aria-live', 'polite');
    const dot = indicator.querySelector('.connection-dot');
    expect(dot?.className).toMatch(/emerald/);
    expect(dot?.className).not.toMatch(/status-pulse/);
  });

  it('renders amber animated dot when reconnecting', () => {
    renderHeader({ connectionState: 'reconnecting' });
    const indicator = screen.getByTestId('chat-header-connection');
    expect(indicator).toHaveAttribute('data-state', 'reconnecting');
    const dot = indicator.querySelector('.connection-dot');
    expect(dot?.className).toMatch(/amber/);
    // AC-010: must animate (status-pulse opacity keyframe) and clearly differ
    // from the red terminal state.
    expect(dot?.className).toMatch(/status-pulse/);
  });

  it('renders red dot when disconnected-terminal', () => {
    renderHeader({ connectionState: 'disconnected-terminal' });
    const indicator = screen.getByTestId('chat-header-connection');
    expect(indicator).toHaveAttribute('data-state', 'disconnected-terminal');
    const dot = indicator.querySelector('.connection-dot');
    expect(dot?.className).toMatch(/red/);
    // Terminal state is static — no reconnecting pulse.
    expect(dot?.className).not.toMatch(/status-pulse/);
  });

  it('falls back to reconnecting (not terminal) when only isConnected=false is provided', () => {
    // Back-compat path: until Agent 2 wires the typed signal, a transient
    // disconnect must degrade to the amber reconnecting state, NOT red.
    renderHeader({ isConnected: false });
    const indicator = screen.getByTestId('chat-header-connection');
    expect(indicator).toHaveAttribute('data-state', 'reconnecting');
  });

  it('maps isConnected=true to connected when typed prop is absent', () => {
    renderHeader({ isConnected: true });
    const indicator = screen.getByTestId('chat-header-connection');
    expect(indicator).toHaveAttribute('data-state', 'connected');
  });

  it('prefers explicit connectionState over isConnected', () => {
    renderHeader({ connectionState: 'disconnected-terminal', isConnected: true });
    const indicator = screen.getByTestId('chat-header-connection');
    expect(indicator).toHaveAttribute('data-state', 'disconnected-terminal');
  });
});

// AC-011 — pointer / focus affordance relies on the global CSS rule in
// index.css (Tailwind v4 Preflight restore). These tests guard the semantic
// contract the rule relies on: header controls must be native <button>
// elements with no inline `cursor` override that would beat the global rule.
describe('ChatHeader interactive affordance (AC-011)', () => {
  it('renders the new-conversation control as a native <button> with type="button"', () => {
    renderHeader();
    const btn = screen.getByRole('button', { name: /new conversation/i });
    expect(btn.tagName).toBe('BUTTON');
    expect(btn).toHaveAttribute('type', 'button');
  });

  it('does not set an inline cursor override on the new-conversation button', () => {
    renderHeader();
    const btn = screen.getByRole('button', { name: /new conversation/i });
    // No inline `style.cursor` means the global CSS rule decides — which
    // resolves to `pointer` for enabled buttons per spec REQ-001.
    expect((btn as HTMLButtonElement).style.cursor).toBe('');
  });

  it('keeps disabled:cursor-not-allowed utility on the new-conversation button', () => {
    renderHeader({ clearDisabled: true });
    const btn = screen.getByRole('button', { name: /new conversation/i });
    expect(btn).toBeDisabled();
    // Tailwind utility ensures REQ-004 (disabled → not-allowed) even with
    // the global pointer rule active.
    expect(btn.className).toMatch(/disabled:cursor-not-allowed/);
  });
});
