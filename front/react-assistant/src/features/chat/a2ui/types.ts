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
}

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
