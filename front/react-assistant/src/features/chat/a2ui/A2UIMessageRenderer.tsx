import { useDeferredValue, useCallback, useMemo, useState, useContext, createContext, useTransition, type JSX } from 'react';
import { useTranslation } from 'react-i18next';
import { MarkdownContent } from '../MarkdownContent.tsx';
import { HostApprovalCard } from '../hitl/HostApprovalCard.tsx';
import type {
  A2UIPayload,
  A2UIComponent,
  A2UIAction,
  A2UIRound,
  AlertSeverity,
  ButtonSize,
  CardVariant,
  FormField,
  ChoiceOption,
  HostApprovalAction,
  HostApprovalPayload,
} from './types.ts';

// ── Resolution context ─────────────────────────────────────────────────────
// ResolutionContext carries the HITL response payload paired with the
// current A2UI surface when the backend persisted a response row after
// submission. When present, interactive components (QuestionnaireComponent)
// render in a read-only locked state showing the submitted answers
// (REQ-102..105 — spec-process-bugfix-a2ui-hitl-response-persistence.md).
// Kept narrow on purpose: only consumers that must change render shape
// in response to resolution should read this; forward it explicitly
// when you really need it.

interface ResolutionContextValue {
  resolvedPayload?: string;
  resolvedAt?: Date;
}

const ResolutionContext = createContext<ResolutionContextValue>({});

// ── Form state context ─────────────────────────────────────────────────────
// Threaded by the top-level renderer through any A2UI surface that includes
// standalone `textfield` / `checkbox` / `choice` primitives bound to a
// shared data model. Fields register + update their value by path (e.g.
// `/form/registry_id`) so a sibling `button` with actionType=`submit_*`
// can harvest the full bag in one shot and dispatch it via onAction.
//
// Design notes:
// • Values are stored as string | boolean | number to match what A2UI v0.8
//   primitives emit through their `value` / `text` bindings.
// • initialValues seed the bag on first mount so pre-filled data-models
//   (RegisterModelForm's `form.primary_route_adapter: "openrouter"`,
//   `enable_enrichment: true`) are persisted even if the user never
//   touches that field.
// • getValue returns `undefined` when the path has never been set — the
//   caller decides whether to coerce to empty string, 0, or false.

type FormFieldValue = string | number | boolean | undefined;

interface FormStateContextValue {
  getValue: (path: string) => FormFieldValue;
  setValue: (path: string, value: FormFieldValue) => void;
  snapshot: () => Record<string, FormFieldValue>;
}

const FormStateContext = createContext<FormStateContextValue | null>(null);

function useFormState(): FormStateContextValue | null {
  return useContext(FormStateContext);
}

// ── Component Catalog ──────────────────────────────────────────────────────

interface ComponentProps {
  component: A2UIComponent;
  onAction: (action: A2UIAction) => void;
}

function renderChildren(children: A2UIComponent[] | undefined, onAction: (action: A2UIAction) => void): JSX.Element[] | null {
  if (!children || children.length === 0) return null;
  return children.map((child, i) => (
    <A2UIComponentRenderer key={i} component={child} onAction={onAction} />
  ));
}

// ── text ────────────────────────────────────────────────────────────────────
// Uses MarkdownContent for full markdown rendering (code blocks, tables, math, etc.)

function TextComponent({ component }: ComponentProps) {
  const content = (component.props.content as string) ?? '';
  const isStreaming = component.props.isStreaming === true;
  return <MarkdownContent content={content} isStreaming={isStreaming} />;
}

// ── button ──────────────────────────────────────────────────────────────────

// FORM_ACTION_TYPES is the set of unprefixed action names that, when fired
// from a Button, harvest the surrounding form-state bag and hand it to the
// parent as the action payload. Keeps the dispatch ergonomic: surfaces
// describe fields + a submit button, the renderer does the book-keeping.
// Extending this list is cheap — add the action name here, the collector
// below picks it up automatically.
const FORM_ACTION_TYPES = new Set<string>([
  'submit_register',
  'submit_default',
  'accept_license',
]);

// REGISTER_CONTEXT_LENGTH_KEYS / REGISTER_BOOL_KEYS drive the type coercion
// at submit time (REQ-GAP-REG-004). A2UI v0.8 TextField always reports
// `text`/`textFieldType=number` as a string bucket through the data model,
// so the renderer casts before dispatch so the backend never receives
// `"262000"` where it expects `262000`.
const REGISTER_NUMBER_KEYS = new Set<string>(['context_length']);
const REGISTER_BOOL_KEYS = new Set<string>(['enable_enrichment']);

function coerceFormValue(key: string, raw: FormFieldValue): FormFieldValue {
  if (REGISTER_NUMBER_KEYS.has(key)) {
    if (typeof raw === 'number') return raw;
    const n = Number(raw ?? 0);
    return Number.isFinite(n) ? n : 0;
  }
  if (REGISTER_BOOL_KEYS.has(key)) {
    return raw === true || raw === 'true';
  }
  return raw ?? '';
}

/**
 * collectFormContext turns the FormState bag into the v0.8 `context` array
 * shape the backend expects for a userAction — one `{key, value}` pair per
 * field. Path segments are stripped to the leaf name (e.g. `/form/vendor`
 * → `vendor`) so the backend's handler does not need path-awareness.
 */
function collectFormContext(
  snapshot: Record<string, FormFieldValue>,
): Array<{ key: string; value: FormFieldValue }> {
  const out: Array<{ key: string; value: FormFieldValue }> = [];
  for (const [path, raw] of Object.entries(snapshot)) {
    const leaf = path.includes('/') ? path.slice(path.lastIndexOf('/') + 1) : path;
    out.push({ key: leaf, value: coerceFormValue(leaf, raw) });
  }
  return out;
}

function ButtonComponent({ component, onAction }: ComponentProps) {
  const label = (component.props.label as string) ?? '';
  const componentId = (component.props.id as string) ?? '';
  const actionType = (component.props.actionType as string) ?? 'click';
  const variant = (component.props.variant as string) ?? 'primary';
  // `size` governs tap-target scale. `sm` (default) is the existing compact
  // button used inline inside cards / questionnaires. `lg` is used by the
  // escape-hatch binary choice where the user is being asked to make a
  // decisive handoff — see spec §4.4 REQ-102 and ButtonSize in types.ts.
  const size: ButtonSize = component.props.size === 'lg' ? 'lg' : 'sm';
  const propDisabled = component.props.disabled === true;

  // When this surface has been resolved (live submit OR rehydration), every
  // hitl:* button locks. The chosen action stays at full opacity; the
  // others fade. Keeps the visual record of what the user picked without
  // leaving the buttons clickable. Card-level lock for t-review.
  const { resolvedPayload } = useContext(ResolutionContext);
  const resolvedAction = useMemo(() => extractResolvedHITLAction(resolvedPayload), [resolvedPayload]);
  const isHitl = actionType.startsWith('hitl:');
  const lockedByResolution = isHitl && resolvedPayload !== undefined;
  const isChosen = lockedByResolution && resolvedAction !== null && actionType === `hitl:${resolvedAction}`;

  // React 19 `useTransition` surfaces a pending state for the submit click
  // without blocking the main thread. GUD-REG-001 / vercel-react-best-
  // practices: wrap dispatch in startTransition so the rest of the form
  // stays interactive while the parent onAction handler propagates the
  // userAction upstream.
  const [isPending, startTransition] = useTransition();
  const form = useFormState();

  const disabled = propDisabled || lockedByResolution || isPending;
  const dim = lockedByResolution && !isChosen;

  const handleClick = useCallback(() => {
    if (disabled) return;
    // Form-submit actions harvest the FormState bag and expose it as the
    // v0.8 `context` array. Non-form actions pass through the propPayload
    // unchanged for backward-compat with existing button call-sites.
    if (FORM_ACTION_TYPES.has(actionType) && form) {
      const snapshot = form.snapshot();
      const context = collectFormContext(snapshot);
      startTransition(() => {
        onAction({
          type: actionType,
          componentId,
          payload: { context, fields: snapshot },
        });
      });
      return;
    }
    onAction({
      type: actionType,
      componentId,
      payload: component.props.payload ?? null,
    });
  }, [disabled, actionType, form, componentId, onAction, component.props.payload]);

  const variantStyles: Record<string, { bg: string; color: string; border: string; glow: string }> = {
    primary: { bg: 'rgba(14, 165, 233, 0.15)', color: 'var(--accent)', border: 'rgba(14, 165, 233, 0.3)', glow: '0 0 8px -2px var(--accent-glow)' },
    secondary: { bg: 'rgba(255, 255, 255, 0.05)', color: 'var(--text-secondary)', border: 'var(--border-dim)', glow: 'none' },
    danger: { bg: 'rgba(239, 68, 68, 0.1)', color: '#f87171', border: 'rgba(239, 68, 68, 0.2)', glow: 'none' },
    success: { bg: 'rgba(16, 185, 129, 0.15)', color: '#34d399', border: 'rgba(16, 185, 129, 0.3)', glow: 'none' },
  };
  const s = variantStyles[variant] ?? variantStyles.primary;
  const opacity = propDisabled ? 0.5 : dim ? 0.35 : 1;
  // Size classes kept parallel so a single token swap gives the larger
  // tap-target without changing the surrounding DOM.
  const sizeClasses =
    size === 'lg'
      ? 'px-5 py-2.5 rounded-lg text-sm font-medium'
      : 'px-4 py-1.5 rounded-lg text-xs font-medium';

  return (
    <button
      onClick={handleClick}
      disabled={disabled}
      aria-disabled={disabled}
      className={`${sizeClasses} transition-all`}
      style={{
        backgroundColor: s.bg,
        color: s.color,
        border: `1px solid ${s.border}`,
        boxShadow: isChosen ? s.glow : 'none',
        cursor: disabled ? 'not-allowed' : 'pointer',
        opacity,
      }}
    >
      {isChosen ? `✓ ${label}` : label}
    </button>
  );
}

// extractResolvedHITLAction reads the persisted HITL response content and
// returns the action keyword (approve | revise | reject | submit) when the
// payload is an action envelope. Returns null for questionnaire answer maps
// (which are handled by QuestionnaireLocked) and for malformed input.
function extractResolvedHITLAction(payload: string | undefined): string | null {
  if (!payload) return null;
  try {
    const parsed = JSON.parse(payload) as unknown;
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const obj = parsed as Record<string, unknown>;
      if (typeof obj.action === 'string') return obj.action;
    }
  } catch {
    // fall through
  }
  return null;
}

// ── card ────────────────────────────────────────────────────────────────────

function CardComponent({ component, onAction }: ComponentProps) {
  const title = component.props.title as string | undefined;
  // `variant` drives the escape-hatch card's left-rail accent. The accent
  // tint distinguishes the two escape flavors at a glance without a new
  // surface type: frustration uses --accent (offer of agency), contradiction
  // uses --text-muted (neutral path-framing per REQ-124). Default is unchanged.
  const variant: CardVariant =
    component.props.variant === 'escape-frustration' ||
    component.props.variant === 'escape-contradiction'
      ? (component.props.variant as CardVariant)
      : 'default';

  const variantStyle =
    variant === 'escape-frustration'
      ? { borderLeft: '3px solid var(--accent)' }
      : variant === 'escape-contradiction'
        ? { borderLeft: '3px solid var(--text-muted)' }
        : undefined;

  return (
    <div
      className="rounded-xl p-4 my-2"
      style={{
        backgroundColor: 'var(--bg-surface)',
        border: '1px solid var(--border-dim)',
        ...variantStyle,
      }}
    >
      {title && (
        <h4
          className="text-sm font-semibold mb-2"
          style={{ color: 'var(--text-primary)', fontFamily: "'JetBrains Mono', monospace" }}
        >
          {title}
        </h4>
      )}
      {renderChildren(component.children, onAction)}
    </div>
  );
}

// ── code ────────────────────────────────────────────────────────────────────

function CodeComponent({ component }: ComponentProps) {
  const code = (component.props.code as string) ?? (component.props.content as string) ?? '';
  const language = (component.props.language as string) ?? '';
  return (
    <div className="code-block-wrapper my-2">
      {language && <span className="code-lang-badge">{language}</span>}
      <pre
        style={{
          background: 'var(--bg-deep)',
          border: '1px solid var(--border-dim)',
          borderRadius: '8px',
          padding: '1rem',
          paddingTop: language ? '2rem' : '1rem',
          overflowX: 'auto',
          boxShadow: 'inset 0 1px 4px rgba(0,0,0,0.4), 0 0 8px -2px var(--accent-glow)',
        }}
      >
        <code
          style={{
            fontFamily: "'JetBrains Mono', monospace",
            fontSize: '0.8125rem',
            lineHeight: 1.6,
            color: 'var(--text-primary)',
          }}
        >
          {code}
        </code>
      </pre>
    </div>
  );
}

// ── progress ────────────────────────────────────────────────────────────────

function ProgressComponent({ component }: ComponentProps) {
  const value = (component.props.value as number) ?? 0;
  const max = (component.props.max as number) ?? 100;
  const label = component.props.label as string | undefined;
  const pct = Math.min(100, Math.max(0, (value / max) * 100));

  return (
    <div className="my-2">
      {label && (
        <span className="text-xs mb-1 block" style={{ color: 'var(--text-secondary)' }}>
          {label}
        </span>
      )}
      <div
        className="w-full h-2 rounded-full overflow-hidden"
        style={{ backgroundColor: 'var(--bg-input)' }}
        role="progressbar"
        aria-valuenow={value}
        aria-valuemin={0}
        aria-valuemax={max}
      >
        <div
          className="h-full rounded-full transition-all duration-500 ease-out"
          style={{
            width: `${pct}%`,
            backgroundColor: 'var(--accent)',
            boxShadow: '0 0 8px var(--accent-glow)',
          }}
        />
      </div>
    </div>
  );
}

// ── form ────────────────────────────────────────────────────────────────────

function FormComponent({ component, onAction }: ComponentProps) {
  const fields = (component.props.fields as FormField[]) ?? [];
  const submitLabel = (component.props.submitLabel as string) ?? 'Submit';
  const componentId = (component.props.id as string) ?? '';

  const handleSubmit = (e: React.FormEvent<HTMLFormElement>) => {
    e.preventDefault();
    const formData = new FormData(e.currentTarget);
    const data: Record<string, string> = {};
    formData.forEach((val, key) => {
      data[key] = val as string;
    });
    onAction({ type: 'submit', componentId, payload: data });
  };

  return (
    <form onSubmit={handleSubmit} className="flex flex-col gap-3 my-2">
      {fields.map((field) => (
        <div key={field.name} className="flex flex-col gap-1">
          {field.label && (
            <label className="text-xs font-medium" style={{ color: 'var(--text-secondary)' }}>
              {field.label}
            </label>
          )}
          <input
            name={field.name}
            type={field.type ?? 'text'}
            placeholder={field.placeholder}
            required={field.required}
            defaultValue={field.defaultValue}
            className="px-3 py-2 rounded-lg text-sm outline-none transition-colors"
            style={{
              backgroundColor: 'var(--bg-input)',
              color: 'var(--text-primary)',
              border: '1px solid var(--border-dim)',
              fontFamily: "'DM Sans', system-ui, sans-serif",
            }}
          />
        </div>
      ))}
      <button
        type="submit"
        className="self-start px-4 py-1.5 rounded-lg text-xs font-medium transition-all"
        style={{
          backgroundColor: 'rgba(14, 165, 233, 0.15)',
          color: 'var(--accent)',
          border: '1px solid rgba(14, 165, 233, 0.3)',
          boxShadow: '0 0 8px -2px var(--accent-glow)',
          cursor: 'pointer',
        }}
      >
        {submitLabel}
      </button>
    </form>
  );
}

// ── row ─────────────────────────────────────────────────────────────────────
// Horizontal flex-wrap container. Fills a gap in the v0.8 primitive set for
// cases like the model-admin surface where buttons/chips need to flow
// horizontally and wrap to the next line on narrow screens. Children render
// sequentially; `gap` maps to Tailwind spacing (`sm|md|lg`).

function RowComponent({ component, onAction }: ComponentProps) {
  const gap = (component.props.gap as string) ?? 'sm';
  const wrap = component.props.wrap !== false;
  const align = (component.props.align as string) ?? 'center';
  const gapCls = gap === 'lg' ? 'gap-3' : gap === 'md' ? 'gap-2' : 'gap-1.5';
  const wrapCls = wrap ? 'flex-wrap' : '';
  const alignCls = align === 'start' ? 'items-start' : align === 'end' ? 'items-end' : 'items-center';
  return (
    <div className={`flex ${wrapCls} ${alignCls} ${gapCls} my-1`}>
      {renderChildren(component.children, onAction)}
    </div>
  );
}

// ── column ──────────────────────────────────────────────────────────────────
// Vertical flex container. Sibling to `row`. a2ui v0.8 core primitive (see
// https://a2ui.org/specification/v0.8-a2ui/ — Layout), used by every
// management surface as the outer wrapper. Children render sequentially;
// `gap` maps to the same spacing tokens as `row`.

function ColumnComponent({ component, onAction }: ComponentProps) {
  const gap = (component.props.gap as string) ?? 'md';
  const align = (component.props.align as string) ?? 'stretch';
  const gapCls = gap === 'lg' ? 'gap-3' : gap === 'sm' ? 'gap-1.5' : 'gap-2';
  const alignCls =
    align === 'start' ? 'items-start' : align === 'end' ? 'items-end' : align === 'center' ? 'items-center' : 'items-stretch';
  return (
    <div className={`flex flex-col ${alignCls} ${gapCls} my-1`}>
      {renderChildren(component.children, onAction)}
    </div>
  );
}

// ── textfield ───────────────────────────────────────────────────────────────
// Standalone controlled text input — maps to a2ui v0.8 `TextField`. Writes
// to FormStateContext by `path` so a sibling submit-button can collect the
// full form bag in one shot. `validationRegexp` (if present) is applied as
// a native `pattern` attribute for cheap browser-side validation; the
// authoritative check lives on the server (REQ-A2UI-005).

function TextFieldComponent({ component }: ComponentProps) {
  const id = (component.props.id as string) ?? '';
  const label = component.props.label as string | undefined;
  const path = (component.props.path as string) ?? '';
  const textFieldType = (component.props.textFieldType as string) ?? 'shortText';
  const placeholder = component.props.placeholder as string | undefined;
  const validationRegexp = component.props.validationRegexp as string | undefined;
  const required = component.props.required === true;

  const form = useFormState();
  // Controlled read — fall back to the seeded initial value, then to '' so
  // React does not warn about uncontrolled → controlled swaps.
  const rawValue = form?.getValue(path);
  const value = rawValue === undefined || rawValue === null ? '' : String(rawValue);

  const handleChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      if (!form || !path) return;
      if (textFieldType === 'number') {
        const parsed = Number(e.target.value);
        form.setValue(path, Number.isFinite(parsed) ? parsed : 0);
        return;
      }
      form.setValue(path, e.target.value);
    },
    [form, path, textFieldType],
  );

  return (
    <div className="flex flex-col gap-1 my-1">
      {label && (
        <label htmlFor={id || path} className="text-xs font-medium" style={{ color: 'var(--text-secondary)' }}>
          {label}
        </label>
      )}
      <input
        id={id || path}
        name={path}
        type={textFieldType === 'number' ? 'number' : 'text'}
        value={value}
        onChange={handleChange}
        placeholder={placeholder}
        required={required}
        pattern={validationRegexp}
        className="px-3 py-2 rounded-lg text-sm outline-none transition-colors"
        style={{
          backgroundColor: 'var(--bg-input)',
          color: 'var(--text-primary)',
          border: '1px solid var(--border-dim)',
          fontFamily: "'DM Sans', system-ui, sans-serif",
        }}
      />
    </div>
  );
}

// ── checkbox ────────────────────────────────────────────────────────────────
// Standalone controlled boolean input — maps to a2ui v0.8 `CheckBox`. Like
// `textfield`, writes to FormStateContext by `path` so a sibling submit
// button can collect the final bag.

function CheckBoxComponent({ component }: ComponentProps) {
  const id = (component.props.id as string) ?? '';
  const label = component.props.label as string | undefined;
  const path = (component.props.path as string) ?? '';

  const form = useFormState();
  const rawValue = form?.getValue(path);
  const checked = rawValue === true;

  const handleChange = useCallback(
    (e: React.ChangeEvent<HTMLInputElement>) => {
      if (!form || !path) return;
      form.setValue(path, e.target.checked);
    },
    [form, path],
  );

  return (
    <label htmlFor={id || path} className="flex items-start gap-2 my-1 cursor-pointer text-sm"
           style={{ color: 'var(--text-primary)' }}>
      <input
        id={id || path}
        name={path}
        type="checkbox"
        checked={checked}
        onChange={handleChange}
        className="mt-0.5"
        style={{ accentColor: 'var(--accent)' }}
      />
      {label && <span className="leading-snug">{label}</span>}
    </label>
  );
}

// ── multiplechoice ──────────────────────────────────────────────────────────
// Select-style single-choice for `maxAllowedSelections: 1`. a2ui v0.8 core
// primitive. Reads/writes a single string value (the selected option's
// `value`) through FormStateContext. For multi-select (>1) this would need
// a set-based bag; out of scope for the RegisterModelForm surface.

interface MultipleChoiceOption {
  label: string;
  value: string;
}

function MultipleChoiceComponent({ component }: ComponentProps) {
  const id = (component.props.id as string) ?? '';
  const label = component.props.label as string | undefined;
  const path = (component.props.path as string) ?? '';
  const options = (component.props.options as MultipleChoiceOption[]) ?? [];
  const maxAllowedSelections =
    typeof component.props.maxAllowedSelections === 'number'
      ? (component.props.maxAllowedSelections as number)
      : 1;

  const form = useFormState();
  const rawValue = form?.getValue(path);
  const selected = typeof rawValue === 'string' ? rawValue : '';

  const handleChange = useCallback(
    (e: React.ChangeEvent<HTMLSelectElement>) => {
      if (!form || !path) return;
      form.setValue(path, e.target.value);
    },
    [form, path],
  );

  // Single-select fallback renders as a native <select>. Multi-select (not
  // currently exercised by REQ-GAP-REG-001) renders nothing interactive so
  // we never silently accept user input the server cannot reconcile.
  if (maxAllowedSelections !== 1) {
    return <UnknownComponent component={component} />;
  }

  return (
    <div className="flex flex-col gap-1 my-1">
      {label && (
        <label htmlFor={id || path} className="text-xs font-medium" style={{ color: 'var(--text-secondary)' }}>
          {label}
        </label>
      )}
      <select
        id={id || path}
        name={path}
        value={selected}
        onChange={handleChange}
        className="px-3 py-2 rounded-lg text-sm outline-none transition-colors"
        style={{
          backgroundColor: 'var(--bg-input)',
          color: 'var(--text-primary)',
          border: '1px solid var(--border-dim)',
          fontFamily: "'DM Sans', system-ui, sans-serif",
        }}
      >
        {options.map((opt) => (
          <option key={opt.value} value={opt.value}>{opt.label}</option>
        ))}
      </select>
    </div>
  );
}

// ── list ────────────────────────────────────────────────────────────────────

function ListComponent({ component, onAction }: ComponentProps) {
  const items = (component.props.items as string[]) ?? [];
  const ordered = component.props.ordered === true;
  const Tag = ordered ? 'ol' : 'ul';

  return (
    <Tag className="my-1 pl-5 text-sm" style={{ color: 'var(--text-primary)', listStyle: ordered ? 'decimal' : 'disc' }}>
      {items.map((item, i) => (
        <li key={i} className="my-0.5">{item}</li>
      ))}
      {renderChildren(component.children, onAction)}
    </Tag>
  );
}

// ── divider ─────────────────────────────────────────────────────────────────

function DividerComponent() {
  return <hr className="my-3" style={{ border: 'none', borderTop: '1px solid var(--border-dim)' }} />;
}

// ── alert ───────────────────────────────────────────────────────────────────

const alertColors: Record<AlertSeverity, { bg: string; border: string; text: string }> = {
  info: { bg: 'rgba(14, 165, 233, 0.1)', border: 'rgba(14, 165, 233, 0.3)', text: '#7dd3fc' },
  warn: { bg: 'rgba(245, 158, 11, 0.1)', border: 'rgba(245, 158, 11, 0.3)', text: '#fbbf24' },
  error: { bg: 'rgba(239, 68, 68, 0.1)', border: 'rgba(239, 68, 68, 0.3)', text: '#f87171' },
};

function AlertComponent({ component }: ComponentProps) {
  const severity = (component.props.severity as AlertSeverity) ?? 'info';
  const message = (component.props.message as string) ?? '';
  const title = component.props.title as string | undefined;
  const colors = alertColors[severity] ?? alertColors.info;

  return (
    <div
      className="rounded-lg p-3 my-2 text-sm"
      role="alert"
      style={{
        backgroundColor: colors.bg,
        border: `1px solid ${colors.border}`,
        color: colors.text,
      }}
    >
      {title && <strong className="block mb-1">{title}</strong>}
      {message}
    </div>
  );
}

// ── round badge ─────────────────────────────────────────────────────────────
// RoundBadge is *chrome* — a read-only status strip that prefixes the A2UI
// surface whenever the backend re-fires `t-clarify` via `t-reassess` and
// stamps the outgoing A2UI envelope with a top-level `round: {n, max}` field.
// Deliberately not registered in componentCatalog: the backend never emits a
// `round_badge` component; this pill is derived from envelope metadata and
// rendered by the renderer itself (REQ-100).
//
// Visual contract (see spec §4.3 + REQ-120/127):
//   • Reuses BadgeComponent's pill language: 1px accent border, 8% tint,
//     JetBrains Mono 10px uppercase tracked-widest.
//   • Leading ◉ glyph is purely decorative (aria-hidden).
//   • Caption after a `·` separator: "seguimiento" (active mid-loop),
//     "última ronda" (n === max), or "respondida {{time}}" (locked).
//   • NO tooltip — REQ-127 — silence is intentional mid-loop.
//   • Hidden when round is absent, `n < 1`, or `max < 2` (single-round
//     turns must look unchanged).
//
// Wrapped in role="status" aria-live="polite" so screen readers announce
// "Follow-up 2 of 3, follow-up" the moment the bubble mounts, without
// stealing focus.
interface RoundBadgeProps {
  round: A2UIRound;
  resolvedAt?: Date;
}

function RoundBadge({ round, resolvedAt }: RoundBadgeProps) {
  const { t } = useTranslation('chat');
  // Gate on the exact conditions spelled out in REQ-100 + types.ts doc:
  // suppress for the very first round of a turn and for single-round turns.
  if (!round || round.n < 1 || round.max < 2) return null;

  const isLocked = resolvedAt !== undefined;
  const isFinal = round.n === round.max;

  // Caption selection: locked state wins over final/active; otherwise the
  // active surface of the last budgeted round reads "última ronda" to soften
  // the stakes without hiding the cap.
  const caption = isLocked
    ? t('a2ui.clarify.badge.responded', {
        time: resolvedAt.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      })
    : isFinal
      ? t('a2ui.clarify.badge.final')
      : t('a2ui.clarify.badge.followup');

  const label = t('a2ui.clarify.badge.label', { n: round.n, max: round.max });
  const aria = t('a2ui.clarify.badge.aria', { n: round.n, max: round.max, caption });

  return (
    <span
      role="status"
      aria-live="polite"
      aria-label={aria}
      className="inline-flex items-center gap-1.5 text-[10px] font-medium uppercase tracking-widest px-2 py-0.5 rounded-full mb-2 self-start"
      style={{
        color: 'var(--accent)',
        border: '1px solid var(--accent)',
        background: 'rgba(14, 165, 233, 0.08)',
        fontFamily: "'JetBrains Mono', monospace",
      }}
    >
      <span aria-hidden="true">{'\u25C9'}</span>
      <span>{label}</span>
      <span aria-hidden="true" style={{ opacity: 0.5 }}>·</span>
      <span style={{ color: 'var(--text-muted)' }}>{caption}</span>
    </span>
  );
}

// ── badge ───────────────────────────────────────────────────────────────────

function BadgeComponent({ component }: ComponentProps) {
  const label = (component.props.label as string) ?? '';
  return (
    <span
      className="inline-block text-[10px] font-medium uppercase tracking-widest px-2 py-0.5 rounded-full"
      style={{
        color: 'var(--accent)',
        border: '1px solid var(--accent)',
        background: 'rgba(14, 165, 233, 0.08)',
        fontFamily: "'JetBrains Mono', monospace",
      }}
    >
      {label}
    </span>
  );
}

// ── choice ──────────────────────────────────────────────────────────────────
// Standalone radio group. Inside a questionnaire it is rendered by
// QuestionnaireComponent (which owns answer state); when used outside one it
// still renders as a read-only group so it never silently disappears.

interface ChoiceFieldProps {
  questionId: string;
  label: string;
  options: ChoiceOption[];
  recommended?: string;
  quoteFromUser?: string;
  whyItMatters?: string;
  selected?: string;
  onSelect?: (optionId: string) => void;
  groupName: string;
  allowFreeText?: boolean;
  freeTextPlaceholder?: string;
  isFreeText?: boolean;
  freeTextValue?: string;
  onFreeTextChange?: (value: string) => void;
  onFreeTextSelect?: () => void;
}

const OTHER_OPTION_ID = '__other__';

function ChoiceField({
  questionId,
  label,
  options,
  recommended,
  quoteFromUser,
  whyItMatters,
  selected,
  onSelect,
  groupName,
  allowFreeText = true,
  freeTextPlaceholder,
  isFreeText = false,
  freeTextValue = '',
  onFreeTextChange,
  onFreeTextSelect,
}: ChoiceFieldProps) {
  const { t } = useTranslation('chat');
  const recommendedLabel = t('a2ui.recommended', 'Recommended');
  const otherLabel = t('a2ui.other', 'Other');
  const defaultFreeTextPlaceholder = t('a2ui.freeTextPlaceholder', 'Type your answer');
  const otherInputId = `${groupName}-${OTHER_OPTION_ID}`;
  const otherTextInputId = `${groupName}-${OTHER_OPTION_ID}-text`;
  return (
    <fieldset className="my-2">
      {quoteFromUser && (
        <div
          className="mb-2 inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[10px] font-medium"
          style={{
            color: 'var(--accent)',
            border: '1px solid rgba(14, 165, 233, 0.35)',
            background: 'rgba(14, 165, 233, 0.08)',
            fontFamily: "'JetBrains Mono', monospace",
          }}
        >
          <span aria-hidden="true">{'\u201C'}</span>
          <span className="italic">{quoteFromUser}</span>
          <span aria-hidden="true">{'\u201D'}</span>
        </div>
      )}
      <legend
        className="block text-sm font-medium mb-1 leading-snug"
        style={{ color: 'var(--text-primary)' }}
      >
        {label}
      </legend>
      {whyItMatters && (
        <p
          className="text-[11px] mb-2.5 leading-snug"
          style={{ color: 'var(--text-muted)' }}
        >
          {whyItMatters}
        </p>
      )}
      <div role="radiogroup" aria-label={label} className="flex flex-col gap-2">
        {options.map((opt) => {
          const isRecommended = recommended === opt.id;
          const isSelected = selected === opt.id && !isFreeText;
          const inputId = `${groupName}-${opt.id}`;
          return (
            <label
              key={opt.id}
              htmlFor={inputId}
              className="group relative flex items-start gap-3 cursor-pointer rounded-xl px-3.5 py-3 text-sm transition-all duration-150 hover:-translate-y-px"
              style={{
                color: 'var(--text-primary)',
                border: `1.5px solid ${isSelected ? 'var(--accent)' : 'var(--border-dim)'}`,
                backgroundColor: isSelected ? 'rgba(14, 165, 233, 0.10)' : 'var(--bg-input)',
                boxShadow: isSelected ? '0 0 0 3px rgba(14, 165, 233, 0.12)' : 'none',
              }}
            >
              <input
                id={inputId}
                type="radio"
                name={groupName}
                value={opt.id}
                checked={isSelected}
                onChange={() => onSelect?.(opt.id)}
                aria-describedby={isRecommended ? `${inputId}-rec` : undefined}
                className="sr-only"
              />
              {/* Custom indicator dot */}
              <span
                aria-hidden="true"
                className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full transition-colors"
                style={{
                  border: `1.5px solid ${isSelected ? 'var(--accent)' : 'var(--border-dim)'}`,
                  backgroundColor: isSelected ? 'var(--accent)' : 'transparent',
                }}
              >
                {isSelected && (
                  <span
                    className="h-1.5 w-1.5 rounded-full"
                    style={{ backgroundColor: 'var(--bg-deep)' }}
                  />
                )}
              </span>
              <span className="flex-1 leading-snug">{opt.label}</span>
              {isRecommended && (
                <span
                  id={`${inputId}-rec`}
                  className="inline-block text-[9px] font-semibold uppercase tracking-wider px-1.5 py-0.5 rounded-full whitespace-nowrap self-start"
                  style={{
                    color: 'var(--accent)',
                    border: '1px solid var(--accent)',
                    background: 'rgba(14, 165, 233, 0.10)',
                    fontFamily: "'JetBrains Mono', monospace",
                  }}
                >
                  {recommendedLabel}
                </span>
              )}
            </label>
          );
        })}
        {allowFreeText && (
          <label
            htmlFor={otherInputId}
            className="group relative flex items-start gap-3 cursor-pointer rounded-xl px-3.5 py-3 text-sm transition-all duration-150 hover:-translate-y-px"
            style={{
              color: 'var(--text-primary)',
              border: `1.5px dashed ${isFreeText ? 'var(--accent)' : 'var(--border-dim)'}`,
              backgroundColor: isFreeText ? 'rgba(14, 165, 233, 0.10)' : 'transparent',
              boxShadow: isFreeText ? '0 0 0 3px rgba(14, 165, 233, 0.12)' : 'none',
            }}
          >
            <input
              id={otherInputId}
              type="radio"
              name={groupName}
              value={OTHER_OPTION_ID}
              checked={isFreeText}
              onChange={() => onFreeTextSelect?.()}
              className="sr-only"
            />
            <span
              aria-hidden="true"
              className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full"
              style={{
                border: `1.5px solid ${isFreeText ? 'var(--accent)' : 'var(--border-dim)'}`,
                backgroundColor: isFreeText ? 'var(--accent)' : 'transparent',
              }}
            >
              {isFreeText && (
                <span
                  className="h-1.5 w-1.5 rounded-full"
                  style={{ backgroundColor: 'var(--bg-deep)' }}
                />
              )}
            </span>
            <span className="flex-1 leading-snug italic" style={{ color: 'var(--text-secondary)' }}>
              {otherLabel}…
            </span>
          </label>
        )}
        {allowFreeText && isFreeText && (
          <input
            id={otherTextInputId}
            type="text"
            autoFocus
            aria-label={`${label} — ${otherLabel}`}
            placeholder={freeTextPlaceholder ?? defaultFreeTextPlaceholder}
            value={freeTextValue}
            onChange={(e) => onFreeTextChange?.(e.target.value)}
            className="mt-1 px-3.5 py-2.5 rounded-xl text-sm outline-none transition-colors focus:ring-2"
            style={{
              backgroundColor: 'var(--bg-deep)',
              color: 'var(--text-primary)',
              border: '1.5px solid var(--accent)',
              fontFamily: "'DM Sans', system-ui, sans-serif",
            }}
          />
        )}
      </div>
      <input type="hidden" name={questionId} value={isFreeText ? freeTextValue : (selected ?? '')} />
    </fieldset>
  );
}

function ChoiceComponent({ component }: ComponentProps) {
  const id = (component.props.id as string) ?? '';
  const label = (component.props.label as string) ?? '';
  const options = (component.props.options as ChoiceOption[]) ?? [];
  const recommended = component.props.recommended as string | undefined;
  const allowFreeText = component.props.allowFreeText !== false;
  const freeTextPlaceholder = component.props.freeTextPlaceholder as string | undefined;
  return (
    <ChoiceField
      questionId={id}
      label={label}
      options={options}
      recommended={recommended}
      groupName={id}
      allowFreeText={allowFreeText}
      freeTextPlaceholder={freeTextPlaceholder}
    />
  );
}

// ── questionnaire ───────────────────────────────────────────────────────────

interface QuestionDescriptor {
  id: string;
  label: string;
  options: ChoiceOption[];
  recommended?: string;
  quoteFromUser?: string;
  whyItMatters?: string;
  allowFreeText?: boolean;
  freeTextPlaceholder?: string;
}

function extractQuestions(children: A2UIComponent[] | undefined): QuestionDescriptor[] {
  if (!children) return [];
  const out: QuestionDescriptor[] = [];
  for (const child of children) {
    if (child.type !== 'choice') continue;
    const id = (child.props.id as string) ?? '';
    if (!id) continue;
    out.push({
      id,
      label: (child.props.label as string) ?? '',
      options: (child.props.options as ChoiceOption[]) ?? [],
      recommended: child.props.recommended as string | undefined,
      quoteFromUser: child.props.quoteFromUser as string | undefined,
      whyItMatters: child.props.whyItMatters as string | undefined,
      allowFreeText: child.props.allowFreeText !== false,
      freeTextPlaceholder: child.props.freeTextPlaceholder as string | undefined,
    });
  }
  return out;
}

// QuestionnaireLocked renders a read-only view of a resolved questionnaire.
// Displayed labels resolve each recorded answer id back to its option label
// (falling back to the id itself if the answer was a free-text value that
// does not match any option). Fields with no recorded answer still render
// with a placeholder so the frame stays stable with the active state.
interface QuestionnaireLockedProps {
  questions: QuestionDescriptor[];
  restatedGoal: string;
  assumptions: string[];
  resolvedPayload: string;
  resolvedAt?: Date;
}

export function parseResolvedAnswers(resolvedPayload: string): Record<string, string> {
  let parsed: unknown;
  try {
    parsed = JSON.parse(resolvedPayload);
  } catch {
    console.warn(
      'parseResolvedAnswers: JSON parse error',
      resolvedPayload.slice(0, 120),
    );
    return {};
  }
  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    console.warn(
      'parseResolvedAnswers: non-object JSON',
      typeof parsed,
      String(resolvedPayload).slice(0, 120),
    );
    return {};
  }

  const obj = parsed as Record<string, unknown>;

  // Wrapped shape — REQ-FE-001 (legacy / forward-compat).
  if (obj.answers && typeof obj.answers === 'object' && !Array.isArray(obj.answers)) {
    return coerceAll(obj.answers as Record<string, unknown>);
  }

  // Action envelope — REQ-FE-002. Approve/revise/reject MUST NOT be
  // misread as a questionnaire answer map.
  if ('action' in obj || 'content' in obj) {
    return {};
  }

  // Flat shape — REQ-FE-001 (current emitter). Treat the top-level
  // object as the answer map, coercing each value to string.
  const out = coerceAll(obj);
  if (Object.keys(out).length === 0 && Object.keys(obj).length > 0) {
    console.warn(
      'parseResolvedAnswers: unrecognized shape',
      { keys: Object.keys(obj), preview: resolvedPayload.slice(0, 120) },
    );
  }
  return out;
}

function coerceAll(o: Record<string, unknown>): Record<string, string> {
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(o)) {
    out[k] = typeof v === 'string' ? v : String(v ?? '');
  }
  return out;
}

function QuestionnaireLocked({
  questions,
  restatedGoal,
  assumptions,
  resolvedPayload,
  resolvedAt,
}: QuestionnaireLockedProps) {
  const { t } = useTranslation('chat');
  const answers = useMemo(() => parseResolvedAnswers(resolvedPayload), [resolvedPayload]);
  const hasFraming = Boolean(restatedGoal) || assumptions.length > 0;

  const respondedAtLabel = resolvedAt
    ? t('a2ui.questionnaire.respondedAt', {
        defaultValue: 'Responded at {{time}}',
        time: resolvedAt.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      })
    : t('a2ui.questionnaire.responded', 'Responded');

  const resolveLabel = (q: QuestionDescriptor, answer: string): string => {
    const match = q.options?.find((o) => o.id === answer);
    return match?.label ?? answer;
  };

  return (
    <section
      aria-label={t('a2ui.questionnaireAriaLabel', 'Clarification questionnaire')}
      aria-live="polite"
      className="flex flex-col gap-3 my-2"
    >
      {hasFraming && (
        <div
          className="rounded-xl px-3.5 py-3 flex flex-col gap-2"
          style={{
            backgroundColor: 'var(--bg-input)',
            border: '1px solid var(--border-dim)',
          }}
        >
          <div
            className="text-[10px] uppercase tracking-widest font-semibold"
            style={{ color: 'var(--accent)', fontFamily: "'JetBrains Mono', monospace" }}
          >
            {t('a2ui.understoodAs', 'I understood')}
          </div>
          {restatedGoal && (
            <div className="text-sm leading-snug" style={{ color: 'var(--text-primary)' }}>
              {restatedGoal}
            </div>
          )}
          {assumptions.length > 0 && (
            <ul className="flex flex-col gap-1 mt-1 list-disc pl-5">
              {assumptions.map((a, i) => (
                <li key={i} className="text-[12px] leading-snug" style={{ color: 'var(--text-secondary)' }}>
                  {a}
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      <div
        className="text-[10px] uppercase tracking-widest font-semibold"
        style={{ color: 'var(--text-muted)', fontFamily: "'JetBrains Mono', monospace" }}
      >
        {respondedAtLabel}
      </div>

      <ul className="flex flex-col gap-2">
        {questions.map((q) => {
          const answer = answers[q.id];
          return (
            <li
              key={q.id}
              className="rounded-lg px-3 py-2 flex flex-col gap-1"
              style={{ backgroundColor: 'var(--bg-input)', border: '1px solid var(--border-dim)' }}
            >
              <div className="text-[11px]" style={{ color: 'var(--text-muted)' }}>
                {q.label}
              </div>
              <div className="text-sm" style={{ color: 'var(--text-primary)' }}>
                {answer ? resolveLabel(q, answer) : '—'}
              </div>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

function QuestionnaireComponent({ component, onAction }: ComponentProps) {
  const { t } = useTranslation('chat');
  const componentId = (component.props.id as string) ?? '';
  const submitLabel = (component.props.submitLabel as string) ?? t('a2ui.submit', 'Submit');
  const restatedGoal = (component.props.restatedGoal as string | undefined) ?? '';
  const assumptions = (component.props.assumptions as string[] | undefined) ?? [];
  const questions = useMemo(() => extractQuestions(component.children), [component.children]);
  const { resolvedPayload, resolvedAt } = useContext(ResolutionContext);
  // All hooks MUST run on every render — when resolvedPayload flips from
  // undefined to defined live (after HITL submit), the early return for
  // QuestionnaireLocked would otherwise change the hook count and crash
  // the tree under React's rules of hooks.
  const [answers, setAnswers] = useState<Record<string, string>>({});
  const [freeTextMode, setFreeTextMode] = useState<Record<string, boolean>>({});
  const [freeTextValues, setFreeTextValues] = useState<Record<string, string>>({});
  const [stepIndex, setStepIndex] = useState(0);

  const totalSteps = questions.length;
  const safeStepIndex = totalSteps === 0 ? 0 : Math.min(stepIndex, totalSteps - 1);
  const isSingleQuestion = totalSteps === 1;
  const isFinalStep = safeStepIndex === totalSteps - 1;
  const currentQuestion = totalSteps > 0 ? questions[safeStepIndex] : undefined;

  const isQuestionAnswered = useCallback(
    (q: QuestionDescriptor) => {
      if (freeTextMode[q.id]) {
        return Boolean(freeTextValues[q.id]?.trim());
      }
      return Boolean(answers[q.id]);
    },
    [answers, freeTextMode, freeTextValues],
  );

  const allAnswered =
    totalSteps > 0 && questions.every((q) => isQuestionAnswered(q));
  const currentAnswered = currentQuestion ? isQuestionAnswered(currentQuestion) : false;

  const handleSelect = useCallback((questionId: string, optionId: string) => {
    setFreeTextMode((prev) => ({ ...prev, [questionId]: false }));
    setAnswers((prev) => ({ ...prev, [questionId]: optionId }));
  }, []);

  const handleFreeTextSelect = useCallback((questionId: string) => {
    setFreeTextMode((prev) => ({ ...prev, [questionId]: true }));
    setAnswers((prev) => {
      const next = { ...prev };
      delete next[questionId];
      return next;
    });
  }, []);

  const handleFreeTextChange = useCallback((questionId: string, value: string) => {
    setFreeTextValues((prev) => ({ ...prev, [questionId]: value }));
  }, []);

  const handleSubmit = useCallback(
    (e: React.FormEvent<HTMLFormElement>) => {
      e.preventDefault();
      if (!allAnswered) return;
      const consolidated: Record<string, string> = {};
      for (const q of questions) {
        consolidated[q.id] = freeTextMode[q.id]
          ? (freeTextValues[q.id] ?? '').trim()
          : (answers[q.id] ?? '');
      }
      onAction({
        type: 'hitl:submit',
        componentId,
        payload: { answers: consolidated },
      });
    },
    [allAnswered, answers, componentId, freeTextMode, freeTextValues, onAction, questions],
  );

  const goPrevious = useCallback(() => {
    setStepIndex((i) => Math.max(0, i - 1));
  }, []);

  const goNext = useCallback(() => {
    setStepIndex((i) => Math.min(totalSteps - 1, i + 1));
  }, [totalSteps]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLFormElement>) => {
      if (e.key !== 'Enter') return;
      const target = e.target as HTMLElement | null;
      // Allow native submit when focus is already on the submit button.
      if (target && target.tagName === 'BUTTON') return;
      // Allow newlines in textareas (none today, but defensive).
      if (target && target.tagName === 'TEXTAREA') return;
      if (!currentAnswered) return;
      if (isFinalStep) {
        // Let the form submit naturally.
        return;
      }
      e.preventDefault();
      goNext();
    },
    [currentAnswered, goNext, isFinalStep],
  );

  // REQ-102..105: when a HITL response is paired with this surface (live
  // submission OR rehydration), render the read-only summary. Placed AFTER
  // every hook so the hook count is identical across renders.
  if (resolvedPayload !== undefined) {
    return (
      <QuestionnaireLocked
        questions={questions}
        restatedGoal={restatedGoal}
        assumptions={assumptions}
        resolvedPayload={resolvedPayload}
        resolvedAt={resolvedAt}
      />
    );
  }

  const hasFraming = Boolean(restatedGoal) || assumptions.length > 0;

  const framingHeader = hasFraming ? (
    <div
      className="rounded-xl px-3.5 py-3 flex flex-col gap-2"
      style={{
        backgroundColor: 'var(--bg-input)',
        border: '1px solid var(--border-dim)',
      }}
    >
      <div
        className="text-[10px] uppercase tracking-widest font-semibold"
        style={{ color: 'var(--accent)', fontFamily: "'JetBrains Mono', monospace" }}
      >
        {t('a2ui.understoodAs', 'I understood')}
      </div>
      {restatedGoal && (
        <div className="text-sm leading-snug" style={{ color: 'var(--text-primary)' }}>
          {restatedGoal}
        </div>
      )}
      {assumptions.length > 0 && (
        <ul className="flex flex-col gap-1 mt-1 list-disc pl-5">
          {assumptions.map((a, i) => (
            <li key={i} className="text-[12px] leading-snug" style={{ color: 'var(--text-secondary)' }}>
              {a}
            </li>
          ))}
        </ul>
      )}
    </div>
  ) : null;

  if (totalSteps === 0 || !currentQuestion) {
    if (!hasFraming) return null;
    const confirmLabel = t('a2ui.confirmAndContinue', 'Confirm and continue');
    return (
      <form
        onSubmit={(e) => {
          e.preventDefault();
          onAction({ type: 'hitl:submit', componentId, payload: { answers: {} } });
        }}
        aria-label={t('a2ui.questionnaireAriaLabel', 'Clarification questionnaire')}
        className="flex flex-col gap-3 my-2"
      >
        {framingHeader}
        <div>
          <button
            type="submit"
            className="px-4 py-1.5 rounded-lg text-xs font-medium transition-all"
            style={{
              backgroundColor: 'rgba(14, 165, 233, 0.15)',
              color: 'var(--accent)',
              border: '1px solid rgba(14, 165, 233, 0.3)',
              boxShadow: '0 0 8px -2px var(--accent-glow)',
              cursor: 'pointer',
            }}
          >
            {confirmLabel}
          </button>
        </div>
      </form>
    );
  }

  const previousLabel = t('a2ui.previous', 'Previous');
  const nextLabel = t('a2ui.next', 'Next');
  const stepOfLabel = t('a2ui.stepOf', {
    defaultValue: 'Question {{current}} of {{total}}',
    current: safeStepIndex + 1,
    total: totalSteps,
  });

  const primaryDisabled = isFinalStep ? !allAnswered : !currentAnswered;
  const primaryLabel = isFinalStep ? submitLabel : nextLabel;
  const primaryAriaLabel = isFinalStep
    ? submitLabel
    : `${nextLabel} (${safeStepIndex + 1}/${totalSteps})`;
  const previousAriaLabel = `${previousLabel} (${safeStepIndex + 1}/${totalSteps})`;

  return (
    <form
      onSubmit={handleSubmit}
      onKeyDown={handleKeyDown}
      aria-label={t('a2ui.questionnaireAriaLabel', 'Clarification questionnaire')}
      className="flex flex-col gap-3 my-2"
    >
      {framingHeader}
      {!isSingleQuestion && (
        <div className="flex flex-col gap-1.5">
          <div
            aria-live="polite"
            className="text-[11px] uppercase tracking-widest"
            style={{
              color: 'var(--text-muted)',
              fontFamily: "'JetBrains Mono', monospace",
            }}
          >
            {stepOfLabel}
          </div>
          <div
            role="progressbar"
            aria-valuemin={1}
            aria-valuemax={totalSteps}
            aria-valuenow={safeStepIndex + 1}
            className="h-1 w-full rounded-full overflow-hidden"
            style={{ backgroundColor: 'var(--border-dim)' }}
          >
            <div
              className="h-full transition-all duration-200 ease-out"
              style={{
                width: `${((safeStepIndex + 1) / totalSteps) * 100}%`,
                backgroundColor: 'var(--accent)',
                boxShadow: '0 0 8px -2px var(--accent-glow)',
              }}
            />
          </div>
        </div>
      )}

      <ChoiceField
        key={currentQuestion.id}
        questionId={currentQuestion.id}
        label={currentQuestion.label}
        options={currentQuestion.options}
        recommended={currentQuestion.recommended}
        quoteFromUser={currentQuestion.quoteFromUser}
        whyItMatters={currentQuestion.whyItMatters}
        selected={answers[currentQuestion.id]}
        onSelect={(optionId) => handleSelect(currentQuestion.id, optionId)}
        groupName={`${componentId}-${currentQuestion.id}`}
        allowFreeText={currentQuestion.allowFreeText}
        freeTextPlaceholder={currentQuestion.freeTextPlaceholder}
        isFreeText={Boolean(freeTextMode[currentQuestion.id])}
        freeTextValue={freeTextValues[currentQuestion.id] ?? ''}
        onFreeTextChange={(value) => handleFreeTextChange(currentQuestion.id, value)}
        onFreeTextSelect={() => handleFreeTextSelect(currentQuestion.id)}
      />

      <div className="flex items-center gap-2">
        {!isSingleQuestion && (
          <button
            type="button"
            onClick={goPrevious}
            disabled={safeStepIndex === 0}
            aria-label={previousAriaLabel}
            className="px-3 py-1.5 rounded-lg text-xs font-medium transition-all"
            style={{
              backgroundColor: 'transparent',
              color: 'var(--text-primary)',
              border: '1px solid var(--border-dim)',
              opacity: safeStepIndex === 0 ? 0.4 : 1,
              cursor: safeStepIndex === 0 ? 'not-allowed' : 'pointer',
            }}
          >
            {previousLabel}
          </button>
        )}
        <button
          type={isFinalStep ? 'submit' : 'button'}
          onClick={isFinalStep ? undefined : goNext}
          disabled={primaryDisabled}
          aria-label={primaryAriaLabel}
          className="px-4 py-1.5 rounded-lg text-xs font-medium transition-all"
          style={{
            backgroundColor: 'rgba(14, 165, 233, 0.15)',
            color: 'var(--accent)',
            border: '1px solid rgba(14, 165, 233, 0.3)',
            boxShadow: !primaryDisabled ? '0 0 8px -2px var(--accent-glow)' : 'none',
            opacity: primaryDisabled ? 0.5 : 1,
            cursor: primaryDisabled ? 'not-allowed' : 'pointer',
          }}
        >
          {primaryLabel}
        </button>
      </div>
    </form>
  );
}

// ── Unknown / placeholder ──────────────────────────────────────────────────

function UnknownComponent({ component }: { component: A2UIComponent }) {
  const { t } = useTranslation('chat');
  return (
    <div
      className="rounded-lg p-2 my-1 text-xs"
      style={{
        backgroundColor: 'var(--bg-input)',
        border: '1px dashed var(--border-dim)',
        color: 'var(--text-muted)',
        fontFamily: "'JetBrains Mono', monospace",
      }}
    >
      {t('a2ui.unknownComponent', { type: component.type })}
    </div>
  );
}

// ── Component Registry ─────────────────────────────────────────────────────

const componentCatalog: Record<string, React.FC<ComponentProps>> = {
  text: TextComponent,
  button: ButtonComponent,
  card: CardComponent,
  code: CodeComponent,
  progress: ProgressComponent,
  form: FormComponent,
  list: ListComponent,
  row: RowComponent,
  // a2ui v0.8 core primitives added to render the RegisterModelForm
  // surface (REQ-GAP-REG-001). These are not new extensions — they are
  // part of the v0.8 primitive set documented at
  // https://a2ui.org/specification/v0.8-a2ui/ (Column, TextField,
  // CheckBox, MultipleChoice). REQ-A2UI-002 forbids NEW catalog entries
  // (extensions beyond the existing set); filling in missing core
  // primitives is explicitly outside that prohibition.
  column: ColumnComponent,
  textfield: TextFieldComponent,
  checkbox: CheckBoxComponent,
  multiplechoice: MultipleChoiceComponent,
  divider: DividerComponent,
  alert: AlertComponent,
  badge: BadgeComponent,
  choice: ChoiceComponent,
  questionnaire: QuestionnaireComponent,
};

function A2UIComponentRenderer({ component, onAction }: ComponentProps) {
  const Comp = componentCatalog[component.type];
  if (!Comp) {
    return <UnknownComponent component={component} />;
  }
  return <Comp component={component} onAction={onAction} />;
}

// ── Main Renderer ──────────────────────────────────────────────────────────

interface A2UIMessageRendererProps {
  payload: A2UIPayload;
  isStreaming: boolean;
  onAction: (action: A2UIAction) => void;
  resolvedPayload?: string;
  resolvedAt?: Date;
}

/**
 * flattenInitialValues walks the payload's optional `data` bag into the
 * path-keyed form-state map. e.g. `{form: {registry_id: "", vendor: ""}}`
 * becomes `{"/form/registry_id": "", "/form/vendor": ""}`. This way the
 * authors of the surface can emit a nested data-model (the shape used in
 * the parent spec §4.6.2) and the FormState bag presents it by path
 * without the fields having to walk into nested objects themselves.
 * Only leaf primitives (string | number | boolean) are retained.
 */
function flattenInitialValues(
  data: Record<string, unknown> | undefined,
  prefix = '',
): Record<string, FormFieldValue> {
  if (!data) return {};
  const out: Record<string, FormFieldValue> = {};
  for (const [k, v] of Object.entries(data)) {
    const path = `${prefix}/${k}`;
    if (v === null || v === undefined) {
      out[path] = '';
    } else if (typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean') {
      out[path] = v;
    } else if (typeof v === 'object' && !Array.isArray(v)) {
      Object.assign(out, flattenInitialValues(v as Record<string, unknown>, path));
    }
  }
  return out;
}

// HOST_APPROVAL_ACTIONS is the allow-list used to recover a
// HostApprovalAction from a persisted response row after rehydration.
// Anything outside this set is treated as "unresolved" so the card renders
// interactive rather than silently locking on a surprise string.
const HOST_APPROVAL_ACTIONS = new Set<HostApprovalAction>([
  'approve-once',
  'approve-and-remember',
  'deny',
  'deny-and-blacklist',
]);

/**
 * extractHostApprovalAction parses the resolved-payload envelope that
 * HostApprovalCard emits through onRespond. Mirrors extractResolvedHITLAction
 * above but narrows to the HostApproval action set so the card's "locked"
 * visual is exact on rehydration (REQ-041/042 +
 * spec-process-bugfix-a2ui-hitl-rehydration.md — surface must survive a
 * reload byte-identically).
 */
/**
 * isObsoleteEnvelope returns true when the resolved-payload envelope marks
 * the surface as stale — either the rehydration-time `expired` sentinel or
 * the live-race `orphaned` sentinel. Callers render an "Aprobación
 * obsoleta" notice instead of interactive buttons (§9.3 / AC-005).
 */
function isObsoleteEnvelope(payload: string | undefined): boolean {
  if (!payload) return false;
  try {
    const parsed = JSON.parse(payload) as unknown;
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return false;
    const a = (parsed as Record<string, unknown>).action;
    return a === 'expired' || a === 'orphaned';
  } catch {
    return false;
  }
}

function extractHostApprovalAction(payload: string | undefined): HostApprovalAction | null {
  if (!payload) return null;
  try {
    const parsed = JSON.parse(payload) as unknown;
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return null;
    const obj = parsed as Record<string, unknown>;
    // Direct envelope — `{"action":"approve-once"}` — the shape
    // HostApprovalCard sends through onRespond.
    const direct = obj.action ?? obj.extended;
    if (typeof direct === 'string' && HOST_APPROVAL_ACTIONS.has(direct as HostApprovalAction)) {
      return direct as HostApprovalAction;
    }
    // Wrapped envelope — useChat.handleResolveHITL wraps the extended
    // action under `content` when the HITL action narrows to `approve`:
    // `{"action":"approve","content":"{\"action\":\"approve-once\"}"}`.
    // Unwrap one level and try again so rehydration matches live state.
    if (typeof obj.content === 'string') {
      try {
        const inner = JSON.parse(obj.content) as unknown;
        if (inner && typeof inner === 'object' && !Array.isArray(inner)) {
          const innerObj = inner as Record<string, unknown>;
          const nested = innerObj.action ?? innerObj.extended;
          if (typeof nested === 'string' && HOST_APPROVAL_ACTIONS.has(nested as HostApprovalAction)) {
            return nested as HostApprovalAction;
          }
        }
      } catch {
        // nested malformed — treat as unresolved.
      }
    }
  } catch {
    // fall through — live or malformed envelopes stay unresolved.
  }
  return null;
}

export function A2UIMessageRenderer({
  payload,
  isStreaming,
  onAction,
  resolvedPayload,
  resolvedAt,
}: A2UIMessageRendererProps) {
  const deferredPayload = useDeferredValue(payload);

  const handleAction = useCallback(
    (action: A2UIAction) => {
      onAction(action);
    },
    [onAction],
  );

  const resolutionValue = useMemo(
    () => ({ resolvedPayload, resolvedAt }),
    [resolvedPayload, resolvedAt],
  );

  // Seed FormState once per payload identity so the user's edits survive
  // re-renders triggered by streaming text updates. A fresh surface (e.g.
  // after the backend emits a surfaceUpdate with the SAME surfaceId) is
  // considered equal to the previous payload if its `data` is unchanged,
  // and its fields therefore keep their values across validation rounds
  // (REQ-GAP-REG-003 / AC-REG-003).
  const [formBag, setFormBag] = useState<Record<string, FormFieldValue>>(() =>
    flattenInitialValues(deferredPayload.data),
  );

  const getValue = useCallback(
    (path: string): FormFieldValue => formBag[path],
    [formBag],
  );
  const setValue = useCallback((path: string, value: FormFieldValue) => {
    setFormBag((prev) => ({ ...prev, [path]: value }));
  }, []);
  const snapshot = useCallback(() => ({ ...formBag }), [formBag]);
  const formContextValue = useMemo<FormStateContextValue>(
    () => ({ getValue, setValue, snapshot }),
    [getValue, setValue, snapshot],
  );

  // Motion — §Motion in the iterative-clarification-loop spec.
  // Only A2UI bubbles that carry a `round` field animate on mount; other
  // bubbles keep their current mount behavior to avoid introducing motion
  // inconsistency across surfaces that pre-date this feature. The `.a2ui-
  // round-in` class is defined in index.css (180ms fade + 4px lift,
  // reduced-motion → fade only).
  const hasRound = Boolean(deferredPayload.round);
  const contentClass = hasRound ? 'a2ui-content a2ui-round-in' : 'a2ui-content';

  // ── host.approval short-circuit ────────────────────────────────────────
  // GAP-6 (spec-architecture-host-gate-security-policy.md §3 REQ-040..042)
  // ships the HostGate approval HITL via a schema-discriminated envelope
  // rather than generic a2ui primitives. Catching it here keeps the
  // existing component catalog untouched while giving the HostGate its own
  // risk-aware visual treatment.
  //
  // Rehydration parity: ResolutionContext already carries resolvedPayload
  // + resolvedAt, so we decode the HostApprovalAction once and pass the
  // locked state into the card — the rehydrated DOM is byte-identical to
  // the live post-submit DOM (per spec-process-bugfix-a2ui-hitl-
  // rehydration.md).
  if (deferredPayload.schema === 'host.approval' && deferredPayload.hostApproval) {
    const resolvedAction = extractHostApprovalAction(resolvedPayload);
    const hostApproval: HostApprovalPayload = deferredPayload.hostApproval;
    // AC-005 / §9.3: the reducer marks pre-fix surfaces with
    // `{"action":"expired"}` and the orphaned-resolve path marks stale
    // cards with `{"action":"orphaned"}`. Either envelope means the card
    // has no live transition to back it — render it locked with the
    // neutral "Aprobación obsoleta" notice instead of crashing.
    const isObsolete = !resolvedAction && isObsoleteEnvelope(resolvedPayload);
    return (
      <ResolutionContext.Provider value={resolutionValue}>
        <div className={contentClass}>
          {deferredPayload.round ? (
            <RoundBadge round={deferredPayload.round} resolvedAt={resolvedAt} />
          ) : null}
          <HostApprovalCard
            payload={hostApproval}
            resolvedAction={resolvedAction}
            resolvedAt={resolvedAt}
            obsolete={isObsolete}
            onRespond={(action) =>
              handleAction({
                type: 'hitl:host-approval',
                // componentId is derived from the payload's operation+command
                // so the reducer can key on a stable value when the backend
                // returns the full transitionId separately via onHITLAction.
                componentId: `host-approval:${hostApproval.operation}`,
                payload: { action },
              })
            }
          />
        </div>
      </ResolutionContext.Provider>
    );
  }

  return (
    <ResolutionContext.Provider value={resolutionValue}>
      <FormStateContext.Provider value={formContextValue}>
        <div className={contentClass}>
          {deferredPayload.round ? (
            <RoundBadge round={deferredPayload.round} resolvedAt={resolvedAt} />
          ) : null}
          {deferredPayload.components.map((component, i) => (
            <A2UIComponentRenderer key={i} component={component} onAction={handleAction} />
          ))}
          {isStreaming && <span className="streaming-cursor-inline" aria-hidden="true" />}
        </div>
      </FormStateContext.Provider>
    </ResolutionContext.Provider>
  );
}

export { componentCatalog };
