/**
 * A2UI type definitions — independent of any SDK.
 * These types model the declarative UI descriptors that the backend agent
 * can embed within streamed messages using the $$a2ui: marker prefix.
 */

/** Root payload containing one or more UI component descriptors. */
export interface A2UIPayload {
  components: A2UIComponent[];
  data?: Record<string, unknown>;
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
