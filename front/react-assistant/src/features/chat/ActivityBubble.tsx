import { useEffect, useRef, useState } from 'react';
import type { CurrentActivity, RecentReceipt } from '../../hooks/useChat';

// Spec constants (spec-design-agent-activity-indicator.md §3.4, §3.5).
export const MIN_DISPLAY_TIME_MS = 400;
export const SR_DEBOUNCE_MS = 1200;
export const CROSSFADE_MS = 150;
export const RECEIPT_AUTO_DISMISS_MS = 4000;

interface DisplayedVerb {
  verb: string;
  detail?: string;
  shownAt: number;
}

interface ActivityBubbleProps {
  activity: CurrentActivity | null;
  receipt: RecentReceipt | null;
  onReceiptDismiss?: () => void;
  prefersReducedMotion?: boolean;
}

// ActivityBubble renders a verb-driven status pill while the agent is
// working, and a brief receipt pill after the run completes. Implements
// the two-state stable/coalescing controller (REQ-021..025) so rapid
// transition bursts coalesce into ≤ 1 visible swap per MDT window.
export function ActivityBubble({
  activity,
  receipt,
  onReceiptDismiss,
  prefersReducedMotion: prefersReducedMotionProp,
}: ActivityBubbleProps) {
  const prefersReducedMotion =
    prefersReducedMotionProp ?? usePrefersReducedMotion();

  // Visually displayed verb. Updated either immediately (when nothing was
  // showing or MDT has elapsed) or via a timer (when the previous verb
  // hasn't met its minimum time yet).
  const [displayed, setDisplayed] = useState<DisplayedVerb | null>(null);

  // Verb that screen readers should announce. Updated only after a
  // verb has been visually stable for SR_DEBOUNCE_MS so SR users do not
  // get pelted with rapid announcements (SEC-002).
  const [announced, setAnnounced] = useState<string>('');

  const coalescingTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const srTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Coalescing controller (PAT-002): one effect keyed on the underlying
  // activity identity, centralising all timer lifecycle.
  useEffect(() => {
    // No active activity: clear any pending visual swap and drop displayed.
    if (!activity) {
      if (coalescingTimer.current) {
        clearTimeout(coalescingTimer.current);
        coalescingTimer.current = null;
      }
      setDisplayed(null);
      return;
    }

    // Same verb + detail as currently displayed → silent identity update,
    // no crossfade (REQ-024). The reducer already keeps startedAt stable
    // for repeated transitionIds; this branch covers different transitions
    // resolving to the same display label.
    if (
      displayed &&
      displayed.verb === activity.verb &&
      displayed.detail === activity.detail
    ) {
      return;
    }

    // Nothing displayed yet → show immediately.
    if (!displayed) {
      setDisplayed({
        verb: activity.verb,
        detail: activity.detail,
        shownAt: Date.now(),
      });
      return;
    }

    // A different verb is requested. Compute when the current verb has
    // satisfied its MDT and schedule the swap then. Latest-wins semantics:
    // if a newer activity replaces this one before the timer fires, the
    // next render of this effect supersedes the pending swap (we clear
    // the previous timer at the top of the next run via the cleanup).
    const elapsed = Date.now() - displayed.shownAt;
    const remaining = Math.max(0, MIN_DISPLAY_TIME_MS - elapsed);

    if (coalescingTimer.current) {
      clearTimeout(coalescingTimer.current);
    }
    coalescingTimer.current = setTimeout(() => {
      coalescingTimer.current = null;
      setDisplayed({
        verb: activity.verb,
        detail: activity.detail,
        shownAt: Date.now(),
      });
    }, remaining);

    return () => {
      if (coalescingTimer.current) {
        clearTimeout(coalescingTimer.current);
        coalescingTimer.current = null;
      }
    };
    // displayed is intentionally omitted — including it would cause the
    // timer to be reset on every visual swap, defeating the coalescing
    // ceiling. We use refs to coordinate instead.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [activity?.verb, activity?.detail, activity?.transitionId, activity == null]);

  // Screen-reader debounce (SEC-002): only announce a verb that has been
  // visually stable for SR_DEBOUNCE_MS. Verbs that flicker through faster
  // than this are silent to SR users.
  useEffect(() => {
    if (!displayed) {
      setAnnounced('');
      if (srTimer.current) {
        clearTimeout(srTimer.current);
        srTimer.current = null;
      }
      return;
    }
    const verbToAnnounce = displayed.detail
      ? `${displayed.verb}, ${displayed.detail}`
      : displayed.verb;
    if (srTimer.current) {
      clearTimeout(srTimer.current);
    }
    srTimer.current = setTimeout(() => {
      srTimer.current = null;
      setAnnounced(verbToAnnounce);
    }, SR_DEBOUNCE_MS);
    return () => {
      if (srTimer.current) {
        clearTimeout(srTimer.current);
        srTimer.current = null;
      }
    };
  }, [displayed?.verb, displayed?.detail]);

  // Receipt auto-dismiss (REQ-030).
  useEffect(() => {
    if (!receipt) return;
    const id = setTimeout(() => {
      onReceiptDismiss?.();
    }, RECEIPT_AUTO_DISMISS_MS);
    return () => clearTimeout(id);
  }, [receipt?.shownAt, onReceiptDismiss]);

  const showActivity = displayed !== null;
  const showReceipt = !showActivity && receipt !== null;
  if (!showActivity && !showReceipt) return null;

  return (
    <div className="flex justify-start" data-testid="activity-bubble">
      <span
        role="status"
        aria-live="polite"
        aria-label={announced}
        className="inline-flex items-center gap-1.5 text-[10px] font-medium uppercase tracking-widest px-3 py-1 rounded-full self-start"
        style={{
          color: 'var(--accent)',
          border: '1px solid var(--accent)',
          background: showActivity
            ? 'rgba(14, 165, 233, 0.08)'
            : 'rgba(14, 165, 233, 0.04)',
          fontFamily: "'JetBrains Mono', monospace",
          opacity: showReceipt ? 0.65 : 1,
          transition: prefersReducedMotion ? 'none' : `opacity ${CROSSFADE_MS}ms ease-out`,
        }}
      >
        {showActivity ? (
          <ActivityContent
            verb={displayed!.verb}
            detail={displayed!.detail}
            prefersReducedMotion={prefersReducedMotion}
          />
        ) : (
          <ReceiptContent receipt={receipt!} />
        )}
      </span>
    </div>
  );
}

interface ActivityContentProps {
  verb: string;
  detail?: string;
  prefersReducedMotion: boolean;
}

function ActivityContent({ verb, detail, prefersReducedMotion }: ActivityContentProps) {
  return (
    <>
      <span
        aria-hidden="true"
        className={prefersReducedMotion ? '' : 'activity-pulse'}
        style={{ display: 'inline-block', width: 6, height: 6, borderRadius: 9999, background: 'var(--accent)' }}
      />
      <span>{verb}</span>
      {detail ? (
        <>
          <span aria-hidden="true" style={{ opacity: 0.5 }}>·</span>
          <span
            style={{
              color: 'var(--text-muted)',
              textTransform: 'none',
              letterSpacing: 'normal',
              maxWidth: '40ch',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {detail}
          </span>
        </>
      ) : null}
    </>
  );
}

function ReceiptContent({ receipt }: { receipt: RecentReceipt }) {
  return <span>{formatReceipt(receipt)}</span>;
}

// formatReceipt produces the receipt-pill text from raw totals.
//   "Thought for 2.3s · $0.0041"
//   "Thought for <1s"          (sub-second runs)
//   "Thought for 4.2s"         (cost == 0; cost clause omitted, AC-008)
export function formatReceipt(receipt: RecentReceipt): string {
  const seconds = receipt.durationMs < 1000 ? '<1s' : `${(receipt.durationMs / 1000).toFixed(1)}s`;
  if (receipt.costUsd > 0) {
    return `Thought for ${seconds} · $${receipt.costUsd.toFixed(4)}`;
  }
  return `Thought for ${seconds}`;
}

// usePrefersReducedMotion subscribes to the `prefers-reduced-motion: reduce`
// media query so the pulse glyph and crossfade fall back to instant swaps
// for users who request reduced motion (SEC-003).
function usePrefersReducedMotion(): boolean {
  const [reduced, setReduced] = useState(() => {
    if (typeof window === 'undefined' || !window.matchMedia) return false;
    return window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  });
  useEffect(() => {
    if (typeof window === 'undefined' || !window.matchMedia) return;
    const mq = window.matchMedia('(prefers-reduced-motion: reduce)');
    const handler = (e: MediaQueryListEvent) => setReduced(e.matches);
    mq.addEventListener('change', handler);
    return () => mq.removeEventListener('change', handler);
  }, []);
  return reduced;
}
