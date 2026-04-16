import { render, screen, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { I18nTestWrapper } from '../../test/i18n-test-utils';
import { SessionResumedNotice } from './SessionResumedNotice';

describe('SessionResumedNotice', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('is hidden when resumedAt is null', () => {
    render(
      <I18nTestWrapper>
        <SessionResumedNotice resumedAt={null} />
      </I18nTestWrapper>
    );
    expect(screen.queryByTestId('session-resumed-notice')).toBeNull();
  });

  it('announces politely and does not steal focus (AC-008, AC-009)', () => {
    const composer = document.createElement('textarea');
    document.body.appendChild(composer);
    composer.focus();
    expect(document.activeElement).toBe(composer);

    render(
      <I18nTestWrapper>
        <SessionResumedNotice resumedAt={1} />
      </I18nTestWrapper>
    );

    const notice = screen.getByTestId('session-resumed-notice');
    expect(notice).toHaveAttribute('role', 'status');
    expect(notice).toHaveAttribute('aria-live', 'polite');
    // Focus must remain on the composer (no autofocus, no focus trap).
    expect(document.activeElement).toBe(composer);

    document.body.removeChild(composer);
  });

  it('auto-dismisses within 4s by default (REQ-111, AC-008)', () => {
    render(
      <I18nTestWrapper>
        <SessionResumedNotice resumedAt={1} />
      </I18nTestWrapper>
    );
    expect(screen.getByTestId('session-resumed-notice')).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(4000);
    });

    expect(screen.queryByTestId('session-resumed-notice')).toBeNull();
  });

  it('re-triggers when resumedAt changes to a new value', () => {
    const { rerender } = render(
      <I18nTestWrapper>
        <SessionResumedNotice resumedAt={1} />
      </I18nTestWrapper>
    );
    expect(screen.getByTestId('session-resumed-notice')).toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(4000);
    });
    expect(screen.queryByTestId('session-resumed-notice')).toBeNull();

    rerender(
      <I18nTestWrapper>
        <SessionResumedNotice resumedAt={2} />
      </I18nTestWrapper>
    );
    expect(screen.getByTestId('session-resumed-notice')).toBeInTheDocument();
  });
});
