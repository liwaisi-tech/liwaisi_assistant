import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { I18nTestWrapper } from '../../test/i18n-test-utils';
import { MessageInput } from './MessageInput';

function renderInput(
  props: Partial<React.ComponentProps<typeof MessageInput>> = {},
) {
  return render(
    <I18nTestWrapper>
      <MessageInput
        onSend={vi.fn()}
        disabled={false}
        error={null}
        {...props}
      />
    </I18nTestWrapper>,
  );
}

// AC-010 — The Send button affordance relies on the global CSS rule in
// index.css (Tailwind v4 Preflight restore). These tests guard the
// semantic contract: the control must be a native <button> with no
// inline `cursor` override so the global rule resolves to `pointer`
// when enabled and to `not-allowed` (via Tailwind utility) when disabled.
describe('MessageInput send button affordance (AC-010)', () => {
  it('renders the send control as a native <button> with type="button"', () => {
    renderInput();
    const btn = screen.getByRole('button');
    expect(btn.tagName).toBe('BUTTON');
    expect(btn).toHaveAttribute('type', 'button');
  });

  it('does not set an inline cursor override on the send button', () => {
    renderInput();
    const btn = screen.getByRole('button') as HTMLButtonElement;
    // No inline `style.cursor` means the global CSS rule decides — which
    // resolves to `pointer` for enabled buttons per spec REQ-001.
    expect(btn.style.cursor).toBe('');
  });

  it('keeps disabled:cursor-not-allowed utility on the send button', () => {
    renderInput({ disabled: true });
    const btn = screen.getByRole('button');
    expect(btn).toBeDisabled();
    // Tailwind utility ensures REQ-004 (disabled → not-allowed) even with
    // the global pointer rule active.
    expect(btn.className).toMatch(/disabled:cursor-not-allowed/);
  });

  it('applies an enabled hover state on the send button (REQ-009)', () => {
    renderInput();
    const btn = screen.getByRole('button');
    // Hover styling is now CSS-only — an `enabled:hover:` utility must be
    // present so pointer users get visual feedback without JS re-renders.
    expect(btn.className).toMatch(/enabled:hover:/);
  });
});

// Awakening gate — spec-architecture-brae-awakening-self-discovery.md
// §4.4, REQ-008 / AC-001. While the first-turn awakening card has not
// yet arrived, the composer is disabled and narrates its own reason with
// a distinctive placeholder — callers must NOT fall through to the
// generic "Waiting for response" copy.
describe('MessageInput awakening gate (REQ-008)', () => {
  it('shows the "waking up" placeholder when awakeningPhase is pending', () => {
    renderInput({ disabled: true, awakeningPhase: 'pending', sessionState: 'running' });
    const textarea = screen.getByRole('textbox') as HTMLTextAreaElement;
    expect(textarea.disabled).toBe(true);
    expect(textarea.placeholder).toBe('brae is waking up\u2026');
    expect(textarea.dataset.awakening).toBe('true');
  });

  it('unlocks and restores the default placeholder when awakeningPhase flips to complete', () => {
    const { rerender } = renderInput({
      disabled: true,
      awakeningPhase: 'pending',
      sessionState: 'running',
    });
    const textareaLocked = screen.getByRole('textbox') as HTMLTextAreaElement;
    expect(textareaLocked.disabled).toBe(true);

    rerender(
      <I18nTestWrapper>
        <MessageInput
          onSend={vi.fn()}
          disabled={false}
          error={null}
          awakeningPhase="complete"
          sessionState="idle"
        />
      </I18nTestWrapper>,
    );
    const textareaUnlocked = screen.getByRole('textbox') as HTMLTextAreaElement;
    expect(textareaUnlocked.disabled).toBe(false);
    expect(textareaUnlocked.placeholder).toBe('Type a message...');
    expect(textareaUnlocked.dataset.awakening).toBeUndefined();
  });

  it('awakening placeholder wins over the generic waiting copy', () => {
    // Even if the session state reports "waiting", a pending awakening
    // phase must surface the awakening copy — it is more specific and
    // directly explains the one-shot discovery turn to the user.
    renderInput({ disabled: true, awakeningPhase: 'pending', sessionState: 'waiting' });
    const textarea = screen.getByRole('textbox') as HTMLTextAreaElement;
    expect(textarea.placeholder).toBe('brae is waking up\u2026');
  });
});

// Also import MessageInput + I18nTestWrapper locally for the rerender in
// the second test — the original helper's renderInput returns render()'s
// result so we pull the rerender out of its generic fixture.
// (kept at bottom so the diff stays small.)
import { MessageInput as _MessageInput } from './MessageInput';
void _MessageInput;
