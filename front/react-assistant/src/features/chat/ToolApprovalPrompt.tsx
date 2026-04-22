import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { A2UIProviderWrapper } from './a2ui/A2UIProviderWrapper';
import { submitToolApproval } from '../../services/api';
import type { A2UIPayload, A2UIAction } from './a2ui/types';

// ── ToolApprovalPrompt ──────────────────────────────────────────────────────
//
// Renders the HITL gate the backend raises the first time a freshly
// synthesized tool is about to run. The preview is an A2UI envelope so we
// reuse the existing renderer — the component only owns the Approve / Deny
// affordance, the POST, and the transient error state.
//
// Wire contract (see task #9 / parent coordination):
//   SSE event:  `tool_approval_request` → { request_id, preview }
//   REST POST:  /api/v1/sessions/{sessionId}/tool-approvals
//               body: { request_id, decision: "approved" | "denied" }
//   Backend replies 204 on success; any non-2xx keeps the prompt open so
//   the user can retry.

interface ToolApprovalPromptProps {
  requestId: string;
  preview: A2UIPayload;
  sessionId: string;
  onResolved: () => void;
}

// No-op A2UI action handler — the preview is display-only; the component's
// own Approve/Deny buttons are the only interactive affordance. Any stray
// click on a button inside the preview is silently swallowed.
function noopA2UIAction(_action: A2UIAction): void {
  void _action;
}

export function ToolApprovalPrompt({
  requestId,
  preview,
  sessionId,
  onResolved,
}: ToolApprovalPromptProps) {
  const { t } = useTranslation('chat');
  const [submitting, setSubmitting] = useState<null | 'approved' | 'denied'>(null);
  const [error, setError] = useState<string | null>(null);

  const handle = useCallback(
    async (decision: 'approved' | 'denied') => {
      if (submitting) return;
      setSubmitting(decision);
      setError(null);
      try {
        await submitToolApproval(sessionId, requestId, decision);
        onResolved();
      } catch {
        setError(t('toolApproval.error'));
        setSubmitting(null);
      }
    },
    [submitting, sessionId, requestId, onResolved, t],
  );

  const disabled = submitting !== null;

  return (
    <section
      data-testid="tool-approval-prompt"
      data-request-id={requestId}
      aria-label={t('toolApproval.title')}
      className="relative rounded-2xl my-2 overflow-hidden"
      style={{
        backgroundColor: 'var(--bg-surface)',
        border: '1px solid var(--border-dim)',
        borderLeft: '4px solid var(--accent)',
        boxShadow:
          '0 0 0 1px rgba(14, 165, 233, 0.12), 0 8px 28px -12px rgba(0, 0, 0, 0.55)',
      }}
    >
      <header
        className="flex items-start gap-2 px-4 pt-3.5 pb-2"
        style={{ borderBottom: '1px solid var(--border-dim)' }}
      >
        <div className="flex flex-col gap-1 min-w-0">
          <span
            className="text-[10px] font-semibold uppercase tracking-widest"
            style={{
              color: 'var(--text-muted)',
              fontFamily: "'JetBrains Mono', monospace",
            }}
          >
            tool · hitl
          </span>
          <h3
            className="text-sm font-semibold leading-snug"
            style={{ color: 'var(--text-primary)' }}
          >
            {t('toolApproval.title')}
          </h3>
        </div>
      </header>

      <div className="px-4 py-3">
        <A2UIProviderWrapper
          payload={preview}
          isStreaming={false}
          onAction={noopA2UIAction}
        />
      </div>

      <footer
        className="flex flex-col gap-2 px-4 py-3"
        style={{ borderTop: '1px solid var(--border-dim)' }}
      >
        <div className="flex flex-wrap items-center gap-2">
          <button
            type="button"
            data-testid="tool-approval-approve"
            onClick={() => handle('approved')}
            disabled={disabled}
            aria-label={t('toolApproval.approve')}
            className="inline-flex items-center justify-center gap-1.5 rounded-lg px-3.5 py-2 text-xs font-medium transition-all duration-150"
            style={{
              backgroundColor: 'var(--accent)',
              color: 'var(--bg-deep)',
              border: '1px solid var(--accent)',
              boxShadow: '0 0 18px -4px var(--accent-glow)',
              opacity: disabled && submitting !== 'approved' ? 0.35 : 1,
              cursor: disabled ? 'not-allowed' : 'pointer',
              fontFamily: "'DM Sans', system-ui, sans-serif",
            }}
          >
            {submitting === 'approved'
              ? t('toolApproval.submitting')
              : t('toolApproval.approve')}
          </button>
          <button
            type="button"
            data-testid="tool-approval-deny"
            onClick={() => handle('denied')}
            disabled={disabled}
            aria-label={t('toolApproval.deny')}
            className="inline-flex items-center justify-center gap-1.5 rounded-lg px-3.5 py-2 text-xs font-medium transition-all duration-150"
            style={{
              backgroundColor: 'rgba(244, 63, 94, 0.12)',
              color: '#fb7185',
              border: '1px solid rgba(244, 63, 94, 0.4)',
              opacity: disabled && submitting !== 'denied' ? 0.35 : 1,
              cursor: disabled ? 'not-allowed' : 'pointer',
              fontFamily: "'DM Sans', system-ui, sans-serif",
            }}
          >
            {submitting === 'denied'
              ? t('toolApproval.submitting')
              : t('toolApproval.deny')}
          </button>
        </div>

        {error && (
          <div
            role="alert"
            data-testid="tool-approval-error"
            className="rounded-lg px-3 py-2 text-xs"
            style={{
              backgroundColor: 'rgba(239, 68, 68, 0.08)',
              color: '#fca5a5',
              border: '1px solid rgba(239, 68, 68, 0.3)',
            }}
          >
            {error}
          </div>
        )}
      </footer>
    </section>
  );
}
