/**
 * A2UI type definitions — independent of any SDK.
 * These types model the declarative UI descriptors that the backend agent
 * can embed within streamed messages using the $$a2ui: marker prefix.
 */

/** Round metadata for a clarify-loop A2UI surface. Present only when the
 * backend has issued a re-fired `t-clarify` round via `t-reassess`; absent
 * on the very first clarify surface of a turn (round 0) and on every non-
 * clarify surface. See spec-architecture-cpn-iterative-clarification-loop
 * §4.3. `n` is 1-indexed (1 = second round, i.e. first follow-up). */
export interface A2UIRound {
  n: number;
  max: number;
}

/** Root payload containing one or more UI component descriptors. */
export interface A2UIPayload {
  components: A2UIComponent[];
  data?: Record<string, unknown>;
  /** Optional clarify-loop round metadata. When present and `n >= 1` and
   * `max >= 2`, the renderer draws a RoundBadge above the component tree.
   * See REQ-100 / REQ-127 in the iterative clarification loop spec. */
  round?: A2UIRound;
  /** Optional schema discriminator. When present, the renderer may pick a
   * dedicated surface component instead of walking `components`. Used by
   * GAP-6 HostGate approvals (schema = 'host.approval'), where the payload
   * carries a structured `hostApproval` bag and the UI treatment is
   * risk-band-specific rather than generic a2ui primitives. Unknown schemas
   * fall through to the default component walk so older clients stay
   * compatible. See spec-architecture-host-gate-security-policy.md §3
   * REQ-040..REQ-042. */
  schema?: string;
  /** Payload for schema='host.approval'. Present iff the surface is a
   * HostGate approval prompt. See REQ-040 in the HostGate policy spec. */
  hostApproval?: HostApprovalPayload;
}

/** HostGate approval prompt payload — surfaced when a shell, PTY, write, or
 * kill operation hits a pattern that requires human confirmation before
 * the host adapter may proceed. REQ-040. */
export interface HostApprovalPayload {
  operation: HostApprovalOperation;
  command: string;
  risk_band: HostApprovalRiskBand;
  rationale: string;
  alternatives?: string[];
}

/** The four host operations that can require an approval prompt. */
export type HostApprovalOperation = 'exec' | 'spawn_pty' | 'write_file' | 'kill';

/** Risk bands the UI actually renders. `forbidden` is server-denied before
 * it ever reaches the UI, so the renderer treats it as a defensive case
 * and never exposes an Approve button for it. */
export type HostApprovalRiskBand = 'safe' | 'caution' | 'dangerous' | 'forbidden';

/** User decisions on a host.approval prompt. Matches REQ-042 verbatim. */
export type HostApprovalAction =
  | 'approve-once'
  | 'approve-and-remember'
  | 'deny'
  | 'deny-and-blacklist';

/** A single declarative UI component descriptor. */
export interface A2UIComponent {
  type: string;
  props: Record<string, unknown>;
  children?: A2UIComponent[];
}

/** An action dispatched from an interactive A2UI component. */
export interface A2UIAction {
  type: string;
  componentId: string;
  payload: unknown;
}

/** Alert severity levels supported by the alert component. */
export type AlertSeverity = 'info' | 'warn' | 'error';

/** Button size. `sm` is the default (px-4 py-1.5 text-xs). `lg` is used for
 * escape-hatch choices where tap-target size matters (px-5 py-2.5 text-sm).
 * See spec-architecture-cpn-iterative-clarification-loop §4.4. */
export type ButtonSize = 'sm' | 'lg';

/** Card variant.
 *  - `default`: baseline card (existing behavior, no change).
 *  - `escape-frustration`: 3px accent-coloured left rail; used on the
 *    frustration escape-hatch (REQ-121..123).
 *  - `escape-contradiction`: 3px muted-coloured left rail; used on the
 *    contradiction escape-hatch (REQ-124..125).
 * See spec-architecture-cpn-iterative-clarification-loop §4.4. */
export type CardVariant = 'default' | 'escape-frustration' | 'escape-contradiction';

/** Progress bar state. */
export interface ProgressProps {
  value: number;
  max?: number;
  label?: string;
}

/** Form field descriptor. */
export interface FormField {
  name: string;
  label?: string;
  type?: string;
  placeholder?: string;
  required?: boolean;
  defaultValue?: string;
}

/** Choice option descriptor for the choice component. */
export interface ChoiceOption {
  id: string;
  label: string;
}

/** Props for the choice component (a single radio question). */
export interface ChoiceProps {
  id: string;
  label: string;
  recommended?: string;
  quoteFromUser?: string;
  whyItMatters?: string;
  options: ChoiceOption[];
}

/** Props for the questionnaire component (a group of choice children + submit). */
export interface QuestionnaireProps {
  id: string;
  submitLabel?: string;
  restatedGoal?: string;
  assumptions?: string[];
}

/** Payload dispatched by the questionnaire submit action. */
export interface QuestionnaireSubmitPayload {
  answers: Record<string, string>;
}
