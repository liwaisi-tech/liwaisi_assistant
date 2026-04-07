import { useDeferredValue, useCallback, useMemo, useState, type JSX } from 'react';
import { useTranslation } from 'react-i18next';
import { MarkdownContent } from '../MarkdownContent.tsx';
import type {
  A2UIPayload,
  A2UIComponent,
  A2UIAction,
  AlertSeverity,
  FormField,
  ChoiceOption,
} from './types.ts';

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

function ButtonComponent({ component, onAction }: ComponentProps) {
  const label = (component.props.label as string) ?? '';
  const componentId = (component.props.id as string) ?? '';
  const actionType = (component.props.actionType as string) ?? 'click';
  const variant = (component.props.variant as string) ?? 'primary';
  const disabled = component.props.disabled === true;

  const handleClick = () => {
    onAction({
      type: actionType,
      componentId,
      payload: component.props.payload ?? null,
    });
  };

  const variantStyles: Record<string, { bg: string; color: string; border: string; glow: string }> = {
    primary: { bg: 'rgba(14, 165, 233, 0.15)', color: 'var(--accent)', border: 'rgba(14, 165, 233, 0.3)', glow: '0 0 8px -2px var(--accent-glow)' },
    secondary: { bg: 'rgba(255, 255, 255, 0.05)', color: 'var(--text-secondary)', border: 'var(--border-dim)', glow: 'none' },
    danger: { bg: 'rgba(239, 68, 68, 0.1)', color: '#f87171', border: 'rgba(239, 68, 68, 0.2)', glow: 'none' },
    success: { bg: 'rgba(16, 185, 129, 0.15)', color: '#34d399', border: 'rgba(16, 185, 129, 0.3)', glow: 'none' },
  };
  const s = variantStyles[variant] ?? variantStyles.primary;

  return (
    <button
      onClick={handleClick}
      disabled={disabled}
      className="px-4 py-1.5 rounded-lg text-xs font-medium transition-all cursor-pointer"
      style={{
        backgroundColor: s.bg,
        color: s.color,
        border: `1px solid ${s.border}`,
        boxShadow: s.glow,
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

function QuestionnaireComponent({ component, onAction }: ComponentProps) {
  const { t } = useTranslation('chat');
  const componentId = (component.props.id as string) ?? '';
  const submitLabel = (component.props.submitLabel as string) ?? t('a2ui.submit', 'Submit');
  const restatedGoal = (component.props.restatedGoal as string | undefined) ?? '';
  const assumptions = (component.props.assumptions as string[] | undefined) ?? [];
  const questions = useMemo(() => extractQuestions(component.children), [component.children]);
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
