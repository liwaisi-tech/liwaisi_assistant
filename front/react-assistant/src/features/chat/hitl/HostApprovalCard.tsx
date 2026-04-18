import { memo, useCallback, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type {
  HostApprovalAction,
  HostApprovalOperation,
  HostApprovalPayload,
  HostApprovalRiskBand,
} from '../a2ui/types';

// ── HostApprovalCard ────────────────────────────────────────────────────────
//
// Renders a GAP-6 `host.approval` HITL prompt: the affordance the user sees
// before brae is allowed to run a shell command, spawn a PTY, write a file,
// or kill a process. The visual language is deliberately divergent from the
// regular clarify/questionnaire surfaces so the user cannot mistake "brae is
// about to touch the host" for "brae is asking a follow-up question".
//
// Design constraints (spec-architecture-host-gate-security-policy.md §3):
//   • REQ-041 — distinct visual treatment per risk band
//       safe       → slate rail      (informational; still ask)
//       caution    → amber rail      (first-run / network / package-mgr)
//       dangerous  → rose rail       (always-HITL, no remember sticks)
//       forbidden  → never shown     (server denies before UI is reached)
//   • REQ-042 — four user actions: approve-once, approve-and-remember,
//     deny, deny-and-blacklist.
//   • Microcopy stays Spanish-LATAM (per brief); action labels localised.
//   • Uses only tokens already defined in index.css / DESIGN.md — no new
//     component library.
//
// Rehydration contract:
//   • The card is stateless wrt. "already responded" — the parent bubble
//     passes `hitlResolved` (mapped from the backend HITL_RESOLVED row) so
//     the card knows which button the user picked. This mirrors the
//     questionnaire/review-card pattern fixed in
//     spec-process-bugfix-a2ui-hitl-rehydration.md: the UI shape MUST be
//     byte-identical after a page reload from persisted state.

interface HostApprovalCardProps {
  payload: HostApprovalPayload;
  /**
   * Previously-submitted action for this prompt, if any. When set, the
   * buttons render locked in the chosen state — the same pattern the
   * existing A2UI HITL buttons use under ResolutionContext
   * (A2UIMessageRenderer.tsx:154-170). Kept optional so the live (first
   * render) case doesn't need to thread a value.
   */
  resolvedAction?: HostApprovalAction | null;
  resolvedAt?: Date;
  /**
   * Callback invoked synchronously on click. The parent (MessageBubble
   * → ChatContainer) is responsible for POSTing to the HITL-response
   * endpoint and re-rendering the card with `resolvedAction` once the
   * backend ACKs. Errors surface via `error` below.
   */
  onRespond: (action: HostApprovalAction) => void;
  /**
   * Transient "request failed" copy. When present the card stays
   * interactive (user can retry) and shows the message inline. Absent →
   * the card is either live (no resolvedAction) or locked.
   */
  error?: string | null;
  /**
   * Retry handler for the last action that failed. When provided alongside
   * `error`, the card shows a retry button. Optional because the first
   * failure is always retryable via the original action buttons.
   */
  onRetry?: () => void;
}

// Band-scoped colour tokens. Kept local to this component so a future
// brand retheme (DESIGN.md colour tokens) only requires editing one map.
// All values come from the existing CSS custom-property palette; no new
// swatches are introduced.
const bandStyle: Record<
  HostApprovalRiskBand,
  {
    rail: string;
    halo: string;
    badgeBg: string;
    badgeColor: string;
    badgeBorder: string;
  }
> = {
  safe: {
    rail: 'var(--text-muted)',
    halo: 'rgba(100, 116, 139, 0.12)',
    badgeBg: 'rgba(100, 116, 139, 0.12)',
    badgeColor: 'var(--text-secondary)',
    badgeBorder: 'rgba(100, 116, 139, 0.35)',
  },
  caution: {
    rail: '#f59e0b',
    halo: 'rgba(245, 158, 11, 0.14)',
    badgeBg: 'rgba(245, 158, 11, 0.12)',
    badgeColor: '#fbbf24',
    badgeBorder: 'rgba(245, 158, 11, 0.4)',
  },
  dangerous: {
    rail: '#f43f5e',
    halo: 'rgba(244, 63, 94, 0.14)',
    badgeBg: 'rgba(244, 63, 94, 0.12)',
    badgeColor: '#fb7185',
    badgeBorder: 'rgba(244, 63, 94, 0.45)',
  },
  // `forbidden` should never reach the UI — the server denies before
  // emission. We keep a style row so a defensive render does not crash
  // (colour matches `dangerous`, copy handled by the body).
  forbidden: {
    rail: '#7f1d1d',
    halo: 'rgba(127, 29, 29, 0.2)',
    badgeBg: 'rgba(127, 29, 29, 0.18)',
    badgeColor: '#fca5a5',
    badgeBorder: 'rgba(127, 29, 29, 0.5)',
  },
};

// Spanish-LATAM copy — per brief. Kept inline rather than in i18n JSON
// because the brief pinned the exact wording; a future i18n pass should
// move these to the `chat` namespace under `a2ui.hostApproval.*`.
const COPY = {
  title: 'Permiso para operar en tu máquina',
  riskLabel: 'Riesgo',
  riskBand: {
    safe: 'Seguro',
    caution: 'Precaución',
    dangerous: 'Peligroso',
    forbidden: 'Prohibido',
  } as Record<HostApprovalRiskBand, string>,
  operationLabel: 'Operación',
  operation: {
    exec: 'ejecutar comando',
    spawn_pty: 'iniciar PTY',
    write_file: 'escribir archivo',
    kill: 'terminar proceso',
  } as Record<HostApprovalOperation, string>,
  commandLabel: 'Comando',
  rationaleLabel: '¿Por qué?',
  alternativesLabel: 'Alternativas que consideró',
  actions: {
    approveOnce: 'Aprobar solo esta vez',
    approveRemember: 'Aprobar y recordar',
    deny: 'Rechazar',
    denyBlacklist: 'Rechazar y bloquear para siempre',
  },
  tooltips: {
    approveRemember:
      'Brae guardará este binario + patrón de argumentos en su lista de confianza. Los comandos peligrosos vuelven a pedir permiso aunque los recuerdes.',
    denyBlacklist:
      'El patrón queda en la lista de rechazo: brae nunca volverá a pedirte permiso para comandos parecidos.',
  },
  locked: {
    'approve-once': 'Aprobaste esta vez',
    'approve-and-remember': 'Aprobado y recordado',
    deny: 'Rechazaste este comando',
    'deny-and-blacklist': 'Rechazado y bloqueado',
  } as Record<HostApprovalAction, string>,
  respondedAt: 'Respondido a las {{time}}',
  forbiddenWarning:
    'Este comando está en la lista de prohibidos. brae no debería pedirte permiso — avisa al equipo.',
  retry: 'Reintentar',
};

// ActionButton — small, opinionated wrapper that carries a short label,
// an optional tooltip (rendered as native `title` so it works on touch
// via long-press), and a pair of variant × tone colours derived from
// the band. Keeps the markup inside the card body readable.
interface ActionButtonProps {
  label: string;
  tone: 'approve' | 'deny' | 'primary';
  tooltip?: string;
  disabled?: boolean;
  chosen?: boolean;
  onClick: () => void;
  emphasis?: 'solid' | 'soft';
  dataTestid?: string;
}

function ActionButton({
  label,
  tone,
  tooltip,
  disabled,
  chosen,
  onClick,
  emphasis = 'soft',
  dataTestid,
}: ActionButtonProps) {
  const palette = useMemo(() => {
    // Approve tone is always the brand accent so the primary CTA reads as
    // "green-go". Deny tone borrows the rose from the dangerous band so
    // users get a consistent "this cancels things" association across the
    // two buttons. `primary` is reserved for the neutral "remember" pair
    // which uses secondary/slate treatment to keep the tap-hierarchy
    // legible.
    if (tone === 'approve') {
      return emphasis === 'solid'
        ? {
            bg: 'var(--accent)',
            color: 'var(--bg-deep)',
            border: 'var(--accent)',
            glow: '0 0 18px -4px var(--accent-glow)',
          }
        : {
            bg: 'rgba(14, 165, 233, 0.14)',
            color: 'var(--accent)',
            border: 'rgba(14, 165, 233, 0.35)',
            glow: '0 0 10px -4px var(--accent-glow)',
          };
    }
    if (tone === 'deny') {
      return emphasis === 'solid'
        ? {
            bg: '#f43f5e',
            color: '#fff1f2',
            border: '#f43f5e',
            glow: '0 0 18px -4px rgba(244, 63, 94, 0.5)',
          }
        : {
            bg: 'rgba(244, 63, 94, 0.12)',
            color: '#fb7185',
            border: 'rgba(244, 63, 94, 0.4)',
            glow: '0 0 10px -4px rgba(244, 63, 94, 0.35)',
          };
    }
    return {
      bg: 'rgba(255, 255, 255, 0.04)',
      color: 'var(--text-secondary)',
      border: 'var(--border-dim)',
      glow: 'none',
    };
  }, [tone, emphasis]);

  return (
    <button
      type="button"
      data-testid={dataTestid}
      onClick={onClick}
      disabled={disabled}
      title={tooltip}
      aria-label={tooltip ? `${label} — ${tooltip}` : label}
      className="inline-flex items-center justify-center gap-1.5 rounded-lg px-3.5 py-2 text-xs font-medium transition-all duration-150"
      style={{
        backgroundColor: palette.bg,
        color: palette.color,
        border: `1px solid ${palette.border}`,
        boxShadow: chosen ? palette.glow : 'none',
        opacity: disabled && !chosen ? 0.35 : 1,
        cursor: disabled ? 'not-allowed' : 'pointer',
        fontFamily: "'DM Sans', system-ui, sans-serif",
      }}
    >
      {chosen ? `\u2713 ${label}` : label}
    </button>
  );
}

export const HostApprovalCard = memo(function HostApprovalCard({
  payload,
  resolvedAction,
  resolvedAt,
  onRespond,
  error,
  onRetry,
}: HostApprovalCardProps) {
  const { t } = useTranslation('chat');
  const [expanded, setExpanded] = useState(false);

  const band: HostApprovalRiskBand =
    payload.risk_band in bandStyle ? payload.risk_band : 'caution';
  const style = bandStyle[band];

  const locked = resolvedAction != null;
  const forbidden = band === 'forbidden';

  const handle = useCallback(
    (action: HostApprovalAction) => {
      if (locked) return;
      onRespond(action);
    },
    [locked, onRespond],
  );

  const commandIsLong = payload.command.length > 68;
  const truncatedCommand = commandIsLong
    ? `${payload.command.slice(0, 68).trimEnd()}\u2026`
    : payload.command;

  // Clock-time for "responded at". Falls back to locale default; short
  // enough to sit inside the badge row without wrapping.
  const respondedAtLabel = resolvedAt
    ? COPY.respondedAt.replace(
        '{{time}}',
        resolvedAt.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      )
    : null;

  return (
    <section
      data-testid="host-approval-card"
      data-risk-band={band}
      aria-label={t('a2ui.hostApproval.aria', {
        defaultValue: 'Host approval prompt',
      })}
      className="relative rounded-2xl my-2 overflow-hidden"
      style={{
        backgroundColor: 'var(--bg-surface)',
        border: '1px solid var(--border-dim)',
        borderLeft: `4px solid ${style.rail}`,
        boxShadow: `0 0 0 1px ${style.halo}, 0 8px 28px -12px rgba(0, 0, 0, 0.55)`,
      }}
    >
      {/* ── Header row ── */}
      <header
        className="flex items-start justify-between gap-3 px-4 pt-3.5 pb-2"
        style={{ borderBottom: `1px solid var(--border-dim)` }}
      >
        <div className="flex flex-col gap-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <span
              className="text-[10px] font-semibold uppercase tracking-widest"
              style={{
                color: 'var(--text-muted)',
                fontFamily: "'JetBrains Mono', monospace",
              }}
            >
              host · hitl
            </span>
            <span
              className="inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-semibold uppercase tracking-widest"
              style={{
                backgroundColor: style.badgeBg,
                color: style.badgeColor,
                border: `1px solid ${style.badgeBorder}`,
                fontFamily: "'JetBrains Mono', monospace",
              }}
              data-testid="host-approval-risk-badge"
            >
              <span aria-hidden="true">{band === 'dangerous' ? '\u25B2' : '\u25CF'}</span>
              {COPY.riskLabel}: {COPY.riskBand[band]}
            </span>
            <span
              className="inline-block rounded-full px-2 py-0.5 text-[10px] font-medium tracking-wide"
              style={{
                backgroundColor: 'var(--bg-input)',
                color: 'var(--text-secondary)',
                border: '1px solid var(--border-dim)',
                fontFamily: "'JetBrains Mono', monospace",
              }}
              data-testid="host-approval-operation-pill"
            >
              {COPY.operation[payload.operation] ?? payload.operation}
            </span>
          </div>
          <h3
            className="text-sm font-semibold leading-snug"
            style={{ color: 'var(--text-primary)' }}
          >
            {COPY.title}
          </h3>
        </div>
      </header>

      {/* ── Body ── */}
      <div className="flex flex-col gap-3 px-4 py-3">
        {/* Command block */}
        <div className="flex flex-col gap-1">
          <span
            className="text-[10px] font-semibold uppercase tracking-widest"
            style={{
              color: 'var(--text-muted)',
              fontFamily: "'JetBrains Mono', monospace",
            }}
          >
            {COPY.commandLabel}
          </span>
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className="text-left rounded-lg px-3 py-2 text-xs leading-relaxed transition-colors"
            style={{
              backgroundColor: 'var(--bg-deep)',
              color: 'var(--text-primary)',
              border: '1px solid var(--border-dim)',
              fontFamily: "'JetBrains Mono', monospace",
              wordBreak: 'break-all',
              whiteSpace: expanded ? 'pre-wrap' : 'nowrap',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              cursor: commandIsLong ? 'zoom-in' : 'text',
            }}
            title={commandIsLong ? payload.command : undefined}
            aria-expanded={expanded}
            data-testid="host-approval-command"
          >
            <span aria-hidden="true" style={{ color: 'var(--accent)' }}>$&nbsp;</span>
            {expanded || !commandIsLong ? payload.command : truncatedCommand}
          </button>
        </div>

        {/* Rationale */}
        {payload.rationale && (
          <div className="flex flex-col gap-1">
            <span
              className="text-[10px] font-semibold uppercase tracking-widest"
              style={{
                color: 'var(--text-muted)',
                fontFamily: "'JetBrains Mono', monospace",
              }}
            >
              {COPY.rationaleLabel}
            </span>
            <p
              className="text-sm leading-snug"
              style={{ color: 'var(--text-secondary)' }}
            >
              {payload.rationale}
            </p>
          </div>
        )}

        {/* Alternatives */}
        {payload.alternatives && payload.alternatives.length > 0 && (
          <details
            className="rounded-lg"
            style={{
              backgroundColor: 'var(--bg-input)',
              border: '1px solid var(--border-dim)',
            }}
          >
            <summary
              className="cursor-pointer list-none px-3 py-2 text-[11px] font-medium flex items-center gap-1.5"
              style={{
                color: 'var(--text-secondary)',
                fontFamily: "'JetBrains Mono', monospace",
              }}
            >
              <span aria-hidden="true">{'\u25B8'}</span>
              {COPY.alternativesLabel} ({payload.alternatives.length})
            </summary>
            <ul className="flex flex-col gap-1 px-4 pb-3 pt-1 list-disc">
              {payload.alternatives.map((alt, i) => (
                <li
                  key={`${i}-${alt.slice(0, 12)}`}
                  className="text-xs leading-snug"
                  style={{
                    color: 'var(--text-primary)',
                    fontFamily: "'JetBrains Mono', monospace",
                  }}
                >
                  {alt}
                </li>
              ))}
            </ul>
          </details>
        )}

        {/* Forbidden defensive banner — server should have blocked this. */}
        {forbidden && (
          <div
            role="alert"
            className="rounded-lg px-3 py-2 text-[11px]"
            style={{
              backgroundColor: 'rgba(239, 68, 68, 0.1)',
              color: '#fca5a5',
              border: '1px solid rgba(239, 68, 68, 0.35)',
              fontFamily: "'DM Sans', system-ui, sans-serif",
            }}
          >
            {COPY.forbiddenWarning}
          </div>
        )}
      </div>

      {/* ── Action row ── */}
      <footer
        className="flex flex-col gap-2 px-4 py-3"
        style={{ borderTop: '1px solid var(--border-dim)' }}
      >
        <div className="flex flex-wrap items-center gap-2">
          <ActionButton
            dataTestid="host-approval-approve-once"
            label={COPY.actions.approveOnce}
            tone="approve"
            emphasis="solid"
            disabled={locked || forbidden}
            chosen={resolvedAction === 'approve-once'}
            onClick={() => handle('approve-once')}
          />
          <ActionButton
            dataTestid="host-approval-approve-remember"
            label={COPY.actions.approveRemember}
            tone="approve"
            emphasis="soft"
            tooltip={COPY.tooltips.approveRemember}
            disabled={locked || forbidden || band === 'dangerous'}
            chosen={resolvedAction === 'approve-and-remember'}
            onClick={() => handle('approve-and-remember')}
          />
          <span
            aria-hidden="true"
            className="mx-1 hidden sm:inline-block"
            style={{
              width: 1,
              height: 18,
              backgroundColor: 'var(--border-dim)',
            }}
          />
          <ActionButton
            dataTestid="host-approval-deny"
            label={COPY.actions.deny}
            tone="deny"
            emphasis="soft"
            disabled={locked}
            chosen={resolvedAction === 'deny'}
            onClick={() => handle('deny')}
          />
          <ActionButton
            dataTestid="host-approval-deny-blacklist"
            label={COPY.actions.denyBlacklist}
            tone="deny"
            emphasis="soft"
            tooltip={COPY.tooltips.denyBlacklist}
            disabled={locked}
            chosen={resolvedAction === 'deny-and-blacklist'}
            onClick={() => handle('deny-and-blacklist')}
          />
        </div>

        {/* Locked status line — kept subtle; the ✓ on the chosen button is the primary cue. */}
        {locked && (
          <p
            className="text-[11px]"
            data-testid="host-approval-resolved-state"
            style={{
              color: 'var(--text-muted)',
              fontFamily: "'JetBrains Mono', monospace",
            }}
          >
            Respondido: {COPY.locked[resolvedAction!]}
            {respondedAtLabel ? ` · ${respondedAtLabel}` : ''}
          </p>
        )}

        {/* Error row */}
        {error && !locked && (
          <div
            role="alert"
            data-testid="host-approval-error"
            className="flex items-center justify-between gap-3 rounded-lg px-3 py-2 text-xs"
            style={{
              backgroundColor: 'rgba(239, 68, 68, 0.08)',
              color: '#fca5a5',
              border: '1px solid rgba(239, 68, 68, 0.3)',
            }}
          >
            <span>{error}</span>
            {onRetry && (
              <button
                type="button"
                onClick={onRetry}
                className="text-[11px] font-medium rounded-md px-2 py-0.5 transition-colors"
                style={{
                  color: '#fca5a5',
                  border: '1px solid rgba(239, 68, 68, 0.5)',
                  backgroundColor: 'transparent',
                  cursor: 'pointer',
                  fontFamily: "'JetBrains Mono', monospace",
                }}
              >
                {COPY.retry}
              </button>
            )}
          </div>
        )}
      </footer>
    </section>
  );
});

/**
 * mapHostApprovalToHITL converts a HostApprovalAction into the
 * `HITLAction | string` tuple the existing HITL-response endpoint expects.
 * The backend's GAP-6 handler will unpack the extended action keyword from
 * the payload; the HITLAction field stays 'approve'/'reject' so legacy
 * rehydration paths keep working.
 *
 * Exported for use by MessageBubble → ChatContainer wiring.
 */
export function mapHostApprovalToHITL(action: HostApprovalAction): {
  hitlAction: 'approve' | 'reject';
  extended: HostApprovalAction;
} {
  switch (action) {
    case 'approve-once':
    case 'approve-and-remember':
      return { hitlAction: 'approve', extended: action };
    case 'deny':
    case 'deny-and-blacklist':
    default:
      return { hitlAction: 'reject', extended: action };
  }
}
