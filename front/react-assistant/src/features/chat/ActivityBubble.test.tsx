import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, render, screen } from '@testing-library/react';
import {
  ActivityBubble,
  MIN_DISPLAY_TIME_MS,
  RECEIPT_AUTO_DISMISS_MS,
  SR_DEBOUNCE_MS,
  formatReceipt,
} from './ActivityBubble';
import type { CurrentActivity, RecentReceipt } from '../../hooks/useChat';

function activity(overrides: Partial<CurrentActivity> = {}): CurrentActivity {
  return {
    verb: 'Thinking',
    transitionId: 't-1',
    cpnId: 'cpn-root',
    startedAt: 1000,
    ...overrides,
  };
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  cleanup();
});

describe('ActivityBubble — formatReceipt', () => {
  it('formats a normal duration with cost', () => {
    expect(formatReceipt({ durationMs: 2310, costUsd: 0.0041, shownAt: 0 } as RecentReceipt))
      .toBe('Thought for 2.3s · $0.0041');
  });

  it('omits cost clause when cost is zero (AC-008)', () => {
    expect(formatReceipt({ durationMs: 2310, costUsd: 0, shownAt: 0 } as RecentReceipt))
      .toBe('Thought for 2.3s');
  });

  it('renders sub-second durations as "<1s"', () => {
    expect(formatReceipt({ durationMs: 800, costUsd: 0, shownAt: 0 } as RecentReceipt))
      .toBe('Thought for <1s');
  });
});

describe('ActivityBubble — null state', () => {
  it('renders nothing when both activity and receipt are null', () => {
    render(<ActivityBubble activity={null} receipt={null} prefersReducedMotion={true} />);
    expect(screen.queryByTestId('activity-bubble')).toBeNull();
  });
});

describe('ActivityBubble — first activity shows immediately', () => {
  it('renders the verb on initial mount with a non-null activity', () => {
    render(<ActivityBubble activity={activity()} receipt={null} prefersReducedMotion={true} />);
    expect(screen.getByTestId('activity-bubble')).toBeInTheDocument();
    expect(screen.getByText('Thinking')).toBeInTheDocument();
  });

  it('renders the detail when provided, separated from the verb', () => {
    render(
      <ActivityBubble
        activity={activity({ verb: 'Calling tool', detail: 'web_search' })}
        receipt={null}
        prefersReducedMotion={true}
      />
    );
    expect(screen.getByText('Calling tool')).toBeInTheDocument();
    expect(screen.getByText('web_search')).toBeInTheDocument();
  });
});

describe('ActivityBubble — coalescing controller (REQ-021..025)', () => {
  it('does not swap to a new verb before MDT elapses', () => {
    const { rerender } = render(
      <ActivityBubble activity={activity({ verb: 'Thinking', transitionId: 't-1' })} receipt={null} prefersReducedMotion={true} />
    );
    expect(screen.getByText('Thinking')).toBeInTheDocument();

    // Try to swap immediately (well under MDT).
    act(() => {
      vi.advanceTimersByTime(50);
    });
    rerender(
      <ActivityBubble activity={activity({ verb: 'Reading', transitionId: 't-2' })} receipt={null} prefersReducedMotion={true} />
    );
    // Still showing Thinking — coalescing window has not elapsed.
    expect(screen.queryByText('Reading')).toBeNull();
    expect(screen.getByText('Thinking')).toBeInTheDocument();
  });

  it('latest-wins: a third verb arriving inside MDT replaces the buffered second', () => {
    const { rerender } = render(
      <ActivityBubble activity={activity({ verb: 'Thinking', transitionId: 't-1' })} receipt={null} prefersReducedMotion={true} />
    );
    act(() => {
      vi.advanceTimersByTime(100);
    });
    rerender(
      <ActivityBubble activity={activity({ verb: 'Reading', transitionId: 't-2' })} receipt={null} prefersReducedMotion={true} />
    );
    act(() => {
      vi.advanceTimersByTime(100);
    });
    rerender(
      <ActivityBubble activity={activity({ verb: 'Calling tool', detail: 'web_search', transitionId: 't-3' })} receipt={null} prefersReducedMotion={true} />
    );
    // Advance past the original MDT — the buffered verb to swap to is the
    // most recent one (Calling tool), not the intermediate one (Reading).
    act(() => {
      vi.advanceTimersByTime(MIN_DISPLAY_TIME_MS);
    });
    expect(screen.queryByText('Reading')).toBeNull();
    expect(screen.getByText('Calling tool')).toBeInTheDocument();
  });

  it('discards buffered next-verb when activity ends mid-coalescing (REQ-023)', () => {
    const { rerender } = render(
      <ActivityBubble activity={activity({ verb: 'Thinking', transitionId: 't-1' })} receipt={null} prefersReducedMotion={true} />
    );
    act(() => {
      vi.advanceTimersByTime(100);
    });
    rerender(
      <ActivityBubble activity={activity({ verb: 'Reading', transitionId: 't-2' })} receipt={null} prefersReducedMotion={true} />
    );
    // End the activity before MDT elapses.
    rerender(<ActivityBubble activity={null} receipt={null} prefersReducedMotion={true} />);
    act(() => {
      vi.advanceTimersByTime(MIN_DISPLAY_TIME_MS * 2);
    });
    expect(screen.queryByText('Reading')).toBeNull();
    expect(screen.queryByText('Thinking')).toBeNull();
    expect(screen.queryByTestId('activity-bubble')).toBeNull();
  });

  it('caps swaps under burst: 12 transitions in 1s yield ≤ ⌈1000/MDT⌉ + 1 visible verbs (AC-003)', () => {
    const { rerender } = render(
      <ActivityBubble activity={activity({ verb: 'V0', transitionId: 't-0' })} receipt={null} prefersReducedMotion={true} />
    );
    const visibleVerbs = new Set<string>();
    visibleVerbs.add('V0');

    for (let i = 1; i < 12; i++) {
      act(() => {
        vi.advanceTimersByTime(83); // ~12 events / 1000ms
      });
      rerender(
        <ActivityBubble
          activity={activity({ verb: `V${i}`, transitionId: `t-${i}` })}
          receipt={null}
          prefersReducedMotion={true}
        />
      );
      // Capture whatever is currently visible after each rerender + tick.
      for (let v = 0; v <= i; v++) {
        if (screen.queryByText(`V${v}`)) {
          visibleVerbs.add(`V${v}`);
        }
      }
    }
    // Drain any pending swap.
    act(() => {
      vi.advanceTimersByTime(MIN_DISPLAY_TIME_MS);
    });

    // Ceiling = ⌈1000 / 400⌉ = 3. Allow one extra (the final pending swap
    // that the burst leaves visible after the loop). 4 is the practical
    // upper bound; flicker is contained well below the 12-event input.
    expect(visibleVerbs.size).toBeLessThanOrEqual(4);
  });
});

describe('ActivityBubble — receipt rendering & dismiss', () => {
  it('shows the receipt pill when activity is null and receipt is set', () => {
    render(
      <ActivityBubble
        activity={null}
        receipt={{ durationMs: 2310, costUsd: 0.0041, shownAt: 1000 }}
        prefersReducedMotion={true}
      />
    );
    expect(screen.getByText('Thought for 2.3s · $0.0041')).toBeInTheDocument();
  });

  it('calls onReceiptDismiss after RECEIPT_AUTO_DISMISS_MS (REQ-030)', () => {
    const onDismiss = vi.fn();
    render(
      <ActivityBubble
        activity={null}
        receipt={{ durationMs: 1000, costUsd: 0, shownAt: Date.now() }}
        onReceiptDismiss={onDismiss}
        prefersReducedMotion={true}
      />
    );
    expect(onDismiss).not.toHaveBeenCalled();
    act(() => {
      vi.advanceTimersByTime(RECEIPT_AUTO_DISMISS_MS);
    });
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it('hides the receipt while an activity is in flight', () => {
    render(
      <ActivityBubble
        activity={activity({ verb: 'Thinking' })}
        receipt={{ durationMs: 1000, costUsd: 0, shownAt: 0 }}
        prefersReducedMotion={true}
      />
    );
    expect(screen.queryByText('Thought for 1.0s')).toBeNull();
    expect(screen.getByText('Thinking')).toBeInTheDocument();
  });
});

describe('ActivityBubble — accessibility', () => {
  it('renders with role="status" and aria-live="polite" (SEC-001)', () => {
    render(<ActivityBubble activity={activity()} receipt={null} prefersReducedMotion={true} />);
    const status = screen.getByRole('status');
    expect(status.getAttribute('aria-live')).toBe('polite');
  });

  it('debounces the SR aria-label by SR_DEBOUNCE_MS (SEC-002)', () => {
    render(<ActivityBubble activity={activity({ verb: 'Thinking' })} receipt={null} prefersReducedMotion={true} />);
    const status = screen.getByRole('status');
    // Immediately after mount, aria-label is empty (debounce not elapsed).
    expect(status.getAttribute('aria-label')).toBe('');
    // After SR_DEBOUNCE_MS, the verb is announced.
    act(() => {
      vi.advanceTimersByTime(SR_DEBOUNCE_MS);
    });
    expect(status.getAttribute('aria-label')).toBe('Thinking');
  });

  // Awakening-phase labels ride the generic transition_started wire with a
  // backend-localised `display_label` (spec-architecture-brae-awakening-
  // self-discovery.md BEH-004). The bubble must surface whatever verb the
  // backend supplied without re-translation on the frontend.
  it('renders the Spanish "Despertando\u2026" verb for awakening transitions', () => {
    render(
      <ActivityBubble
        activity={activity({ verb: 'Despertando\u2026', detail: 'uname -a' })}
        receipt={null}
        prefersReducedMotion={true}
      />,
    );
    expect(screen.getByText('Despertando\u2026')).toBeInTheDocument();
    expect(screen.getByText('uname -a')).toBeInTheDocument();
  });

  it('renders the English "Waking up\u2026" verb when locale is en', () => {
    render(
      <ActivityBubble
        activity={activity({ verb: 'Waking up\u2026', detail: 'command -v git' })}
        receipt={null}
        prefersReducedMotion={true}
      />,
    );
    expect(screen.getByText('Waking up\u2026')).toBeInTheDocument();
  });
});
