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
   * When true, the card renders in a display-only "obsolete" state with a
   * neutral "Aprobación obsoleta" notice in place of action buttons. Used
   * during rehydration of pre-fix chats (§9.3, CON-002): persisted
   * `t-review` rows and stale host-approval surfaces that have no live
   * transition to back them must render locked without crashing.
   */
  obsolete?: boolean;
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

// resolveBand maps the new REQ-003/§4.1 `risk` field (safe|caution|danger)
// and the legacy `risk_band` field (safe|caution|dangerous|forbidden) onto
// the internal band key so the rest of the render path is unchanged. The
// new `danger` value is treated as the existing `dangerous` band so the
// rose rail + always-HITL semantics still apply.
function resolveBand(payload: HostApprovalPayload): HostApprovalRiskBand {
  if (payload.risk === 'danger') return 'dangerous';
  if (payload.risk === 'caution') return 'caution';
  if (payload.risk === 'safe') return 'safe';
  const legacy = payload.risk_band;
  if (legacy && legacy in bandStyle) return legacy;
  return 'caution';
}

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

// ── Inline SVG icons ────────────────────────────────────────────────────────
// 14px stroke icons, no new dependency. Consumer buttons carry aria-label
// + native title; the icons are purely decorative.

const IconCheck = memo(function IconCheck() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth="2.25" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M20 6 9 17l-5-5" />
    </svg>
  );
});

const IconStar = memo(function IconStar() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="m12 17.3-6.2 3.7 1.6-7L2 9.2l7.1-.6L12 2l2.9 6.6 7.1.6-5.4 4.8 1.6 7z" />
    </svg>
  );
});

const IconX = memo(function IconX() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth="2.25" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <path d="M18 6 6 18M6 6l12 12" />
    </svg>
  );
});

const IconBan = memo(function IconBan() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="9" />
      <path d="m5.6 5.6 12.8 12.8" />
    </svg>
  );
});

interface PrimaryActionButtonProps {
  label: string;
  disabled?: boolean;
  chosen?: boolean;
  onClick: () => void;
  dataTestid?: string;
}

function PrimaryActionButton({
  label, disabled, chosen, onClick, dataTestid,
}: PrimaryActionButtonProps) {
  return (
    <button
      type="button"
      data-testid={dataTestid}
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      className="inline-flex items-center gap-1.5 rounded-lg px-3 py-1.5 text-xs font-semibold transition-all duration-150"
      style={{
        backgroundColor: 'var(--accent)',
        color: 'var(--bg-deep)',
        border: '1px solid var(--accent)',
        boxShadow: chosen ? '0 0 14px -4px var(--accent-glow)' : 'none',
        opacity: disabled && !chosen ? 0.4 : 1,
        cursor: disabled ? 'not-allowed' : 'pointer',
        fontFamily: "'DM Sans', system-ui, sans-serif",
      }}
    >
      <IconCheck />
      {label}
    </button>
  );
}

type IconTone = 'approve' | 'deny' | 'deny-strong';

interface IconActionButtonProps {
  icon: 'star' | 'x' | 'ban';
  tone: IconTone;
  ariaLabel: string;
  tooltip: string;
  disabled?: boolean;
  chosen?: boolean;
  onClick: () => void;
  dataTestid?: string;
}

function IconActionButton({
  icon, tone, ariaLabel, tooltip, disabled, chosen, onClick, dataTestid,
}: IconActionButtonProps) {
  const palette = useMemo(() => {
    if (tone === 'approve') {
      return {
        bg: chosen ? 'rgba(14, 165, 233, 0.18)' : 'transparent',
        color: 'var(--accent)',
        border: chosen ? 'rgba(14, 165, 233, 0.45)' : 'var(--border-dim)',
        hoverBg: 'rgba(14, 165, 233, 0.1)',
      };
    }
    const strong = tone === 'deny-strong';
    return {
      bg: chosen
        ? (strong ? 'rgba(244, 63, 94, 0.22)' : 'rgba(244, 63, 94, 0.16)')
        : 'transparent',
      color: '#fb7185',
      border: chosen
        ? (strong ? 'rgba(244, 63, 94, 0.55)' : 'rgba(244, 63, 94, 0.45)')
        : 'var(--border-dim)',
      hoverBg: strong ? 'rgba(244, 63, 94, 0.14)' : 'rgba(244, 63, 94, 0.1)',
    };
  }, [tone, chosen]);

  const Icon = icon === 'star' ? IconStar : icon === 'x' ? IconX : IconBan;
  const fullTooltip = `${ariaLabel} — ${tooltip}`;

  return (
    <button
      type="button"
      data-testid={dataTestid}
      onClick={onClick}
      disabled={disabled}
      title={fullTooltip}
      aria-label={fullTooltip}
      className="inline-flex h-7 w-7 items-center justify-center rounded-md transition-colors duration-150 focus:outline-none"
      style={{
        backgroundColor: palette.bg,
        color: palette.color,
        border: `1px solid ${palette.border}`,
        opacity: disabled && !chosen ? 0.3 : 1,
        cursor: disabled ? 'not-allowed' : 'pointer',
      }}
      onMouseEnter={(e) => {
        if (!disabled) e.currentTarget.style.backgroundColor = palette.hoverBg;
      }}
      onMouseLeave={(e) => {
        e.currentTarget.style.backgroundColor = palette.bg;
      }}
    >
      <Icon />
    </button>
  );
}

const lockedIconFor: Record<HostApprovalAction, 'check' | 'star' | 'x' | 'ban'> = {
  'approve-once': 'check',
  'approve-and-remember': 'star',
  deny: 'x',
  'deny-and-blacklist': 'ban',
};

function LockedStatusChip({
  action, label, time,
}: { action: HostApprovalAction; label: string; time: string | null }) {
  const isApprove = action === 'approve-once' || action === 'approve-and-remember';
  const iconKey = lockedIconFor[action];
  const Icon =
    iconKey === 'check' ? IconCheck
    : iconKey === 'star' ? IconStar
    : iconKey === 'x' ? IconX
    : IconBan;
  const color = isApprove ? 'var(--accent)' : '#fb7185';
  const bg = isApprove ? 'rgba(14, 165, 233, 0.1)' : 'rgba(244, 63, 94, 0.1)';
  const border = isApprove ? 'rgba(14, 165, 233, 0.35)' : 'rgba(244, 63, 94, 0.35)';
  return (
    <div
      className="inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[11px]"
      style={{
        backgroundColor: bg,
        border: `1px solid ${border}`,
        fontFamily: "'JetBrains Mono', monospace",
      }}
      data-testid="host-approval-resolved-state"
    >
      <span style={{ color, display: 'inline-flex' }}><Icon /></span>
      <span style={{ color: 'var(--text-primary)' }}>{label}</span>
      {time && (
        <span style={{ color: 'var(--text-muted)' }}>{`· ${time}`}</span>
      )}
    </div>
  );
}

export const HostApprovalCard = memo(function HostApprovalCard({
  payload,
  resolvedAction,
  resolvedAt,
  onRespond,
  error,
  onRetry,
  obsolete = false,
}: HostApprovalCardProps) {
  const { t } = useTranslation('chat');
  const [expanded, setExpanded] = useState(false);
  const [detailsOpen, setDetailsOpen] = useState(false);

  const band: HostApprovalRiskBand = resolveBand(payload);
  const style = bandStyle[band];

  const locked = resolvedAction != null || obsolete;
  const forbidden = band === 'forbidden';
  // AC-008: hide "Aprobar y recordar" when the backend tells us it is not
  // available (e.g. the user already used it for an equivalent command, or
  // the tool policy forbids sticky approvals).
  const rememberHidden = payload.rememberAvailable === false;

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
            {payload.operation && (
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
            )}
          </div>
          <h3
            className="text-sm font-semibold leading-snug"
            style={{ color: 'var(--text-primary)' }}
          >
            {payload.title ?? COPY.title}
          </h3>
          {payload.context && (
            <p
              className="text-[11px]"
              style={{
                color: 'var(--text-muted)',
                fontFamily: "'JetBrains Mono', monospace",
              }}
              data-testid="host-approval-context"
            >
              {payload.context}
            </p>
          )}
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

        {/* Technical details disclosure — REQ-005 / AC-002. Renders the raw
            tool-invocation JSON (tool name + args) inside a collapsed-by-
            default `<details>`. Kept styled like the alternatives panel so
            the card stays visually coherent. Only rendered when the backend
            supplied an `invocation` bag; legacy payloads fall through. */}
        {payload.invocation && (
          <details
            className="rounded-lg"
            open={detailsOpen}
            onToggle={(e) => setDetailsOpen((e.target as HTMLDetailsElement).open)}
            style={{
              backgroundColor: 'var(--bg-input)',
              border: '1px solid var(--border-dim)',
            }}
            data-testid="host-approval-technical-details"
          >
            <summary
              className="cursor-pointer list-none px-3 py-2 text-[11px] font-medium flex items-center gap-1.5"
              style={{
                color: 'var(--text-secondary)',
                fontFamily: "'JetBrains Mono', monospace",
              }}
            >
              <span aria-hidden="true">{detailsOpen ? '\u25BE' : '\u25B8'}</span>
              {t('a2ui.hostApproval.technicalDetails', {
                defaultValue: 'Detalles técnicos',
              })}
            </summary>
            <pre
              className="px-3 pb-3 pt-1 text-[11px] leading-snug overflow-x-auto"
              style={{
                color: 'var(--text-primary)',
                fontFamily: "'JetBrains Mono', monospace",
                whiteSpace: 'pre',
                margin: 0,
              }}
              data-testid="host-approval-invocation-json"
            >
              {JSON.stringify(payload.invocation, null, 2)}
            </pre>
          </details>
        )}

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
        {/* When locked, the disabled action row is replaced by a single
            compact status chip below (see "Locked status chip"). This keeps
            the rehydrated conversation visually quiet and preserves the
            decision as recognition-grade, not recall-grade, UI.
            HITL best-practice (Nielsen H6 · Google PAIR explainability):
            one primary CTA with label for Fitts-friendly tap, secondary
            actions as icon buttons with tooltips for progressive
            disclosure. */}
        {!locked && (
          <div className="flex flex-wrap items-center gap-2">
            <PrimaryActionButton
              dataTestid="host-approval-approve-once"
              label={COPY.actions.approveOnce}
              disabled={forbidden}
              onClick={() => handle('approve-once')}
            />
            {!rememberHidden && (
              <IconActionButton
                dataTestid="host-approval-approve-remember"
                icon="star"
                tone="approve"
                ariaLabel={COPY.actions.approveRemember}
                tooltip={COPY.tooltips.approveRemember}
                disabled={forbidden || band === 'dangerous'}
                onClick={() => handle('approve-and-remember')}
              />
            )}
            <span
              aria-hidden="true"
              className="mx-1"
              style={{
                width: 1,
                height: 18,
                backgroundColor: 'var(--border-dim)',
              }}
            />
            <IconActionButton
              dataTestid="host-approval-deny"
              icon="x"
              tone="deny"
              ariaLabel={COPY.actions.deny}
              tooltip="Cancela este comando sin bloquear futuros intentos."
              onClick={() => handle('deny')}
            />
            <IconActionButton
              dataTestid="host-approval-deny-blacklist"
              icon="ban"
              tone="deny-strong"
              ariaLabel={COPY.actions.denyBlacklist}
              tooltip={COPY.tooltips.denyBlacklist}
              onClick={() => handle('deny-and-blacklist')}
            />
          </div>
        )}

        {/* Obsolete notice — AC-005 / §9.3. Rendered when a rehydrated pre-
            fix chat carries a surface with no live transition to back it.
            The buttons above are already locked via `obsolete → locked`;
            this line gives the user a neutral reason why. */}
        {obsolete && !resolvedAction && (
          <p
            className="text-[11px]"
            data-testid="host-approval-obsolete-notice"
            style={{
              color: 'var(--text-muted)',
              fontFamily: "'JetBrains Mono', monospace",
            }}
          >
            {t('a2ui.hostApproval.obsolete', {
              defaultValue: 'Aprobación obsoleta',
            })}
          </p>
        )}

        {/* Locked status chip — compact single-line replacement for the
            previous "Respondido: …" paragraph plus the row of disabled
            buttons. Icon + label + time; recognition, not recall. */}
        {locked && resolvedAction && (
          <LockedStatusChip
            action={resolvedAction}
            label={COPY.locked[resolvedAction]}
            time={
              resolvedAt
                ? resolvedAt.toLocaleTimeString([], {
                    hour: '2-digit',
                    minute: '2-digit',
                  })
                : null
            }
          />
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
