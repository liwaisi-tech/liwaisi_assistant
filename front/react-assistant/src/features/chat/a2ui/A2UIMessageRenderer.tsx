import { useDeferredValue, useCallback, type JSX } from 'react';
import { useTranslation } from 'react-i18next';
import type { A2UIPayload, A2UIComponent, A2UIAction, AlertSeverity, FormField } from './types.ts';

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

function TextComponent({ component }: ComponentProps) {
  const content = (component.props.content as string) ?? '';
  const variant = component.props.variant as string | undefined;
  const style: React.CSSProperties = {
    color: variant === 'muted' ? 'var(--text-muted)' : variant === 'secondary' ? 'var(--text-secondary)' : 'var(--text-primary)',
    margin: 0,
  };
  return (
    <p className="text-sm leading-relaxed" style={style}>
      {content}
    </p>
  );
}

// ── button ──────────────────────────────────────────────────────────────────

function ButtonComponent({ component, onAction }: ComponentProps) {
  const label = (component.props.label as string) ?? '';
  const componentId = (component.props.id as string) ?? '';
  const variant = (component.props.variant as string) ?? 'primary';
  const disabled = component.props.disabled === true;

  const handleClick = () => {
    onAction({
      type: 'click',
      componentId,
      payload: component.props.payload ?? null,
    });
  };

  const isPrimary = variant === 'primary';
  return (
    <button
      onClick={handleClick}
      disabled={disabled}
      className="px-4 py-1.5 rounded-lg text-xs font-medium transition-all"
      style={{
        backgroundColor: isPrimary ? 'rgba(14, 165, 233, 0.15)' : 'rgba(255, 255, 255, 0.05)',
        color: isPrimary ? 'var(--accent)' : 'var(--text-secondary)',
        border: `1px solid ${isPrimary ? 'rgba(14, 165, 233, 0.3)' : 'var(--border-dim)'}`,
        boxShadow: isPrimary ? '0 0 8px -2px var(--accent-glow)' : 'none',
        cursor: disabled ? 'not-allowed' : 'pointer',
        opacity: disabled ? 0.5 : 1,
      }}
    >
      {label}
    </button>
  );
}

// ── card ────────────────────────────────────────────────────────────────────

function CardComponent({ component, onAction }: ComponentProps) {
  const title = component.props.title as string | undefined;
  return (
    <div
      className="rounded-xl p-4 my-2"
      style={{
        backgroundColor: 'var(--bg-surface)',
        border: '1px solid var(--border-dim)',
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
  divider: DividerComponent,
  alert: AlertComponent,
  badge: BadgeComponent,
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
}

export function A2UIMessageRenderer({ payload, isStreaming, onAction }: A2UIMessageRendererProps) {
  const deferredPayload = useDeferredValue(payload);

  const handleAction = useCallback(
    (action: A2UIAction) => {
      onAction(action);
    },
    [onAction],
  );

  return (
    <div className="a2ui-content">
      {deferredPayload.components.map((component, i) => (
        <A2UIComponentRenderer key={i} component={component} onAction={handleAction} />
      ))}
      {isStreaming && <span className="streaming-cursor-inline" aria-hidden="true" />}
    </div>
  );
}

export { componentCatalog };
