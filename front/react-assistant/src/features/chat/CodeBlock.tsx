import { lazy, Suspense, useState, useCallback, isValidElement } from 'react';
import type { ComponentPropsWithoutRef, ReactElement, ReactNode } from 'react';

const SyntaxHighlighter = lazy(() =>
  import('./syntax-highlighter').then(mod => ({
    default: mod.default,
  }))
);

const themePromise = import('./syntax-highlighter').then(mod => mod.deepSpaceTheme);

// Regex hoisted outside component (js-hoist-regexp)
const LANGUAGE_RE = /language-(\w+)/;

// Transparent background so the .code-block-wrapper controls it.
// PreTag="div" ensures no nested <pre> element inherits markdown styles.
const highlighterStyle: React.CSSProperties = {
  background: 'transparent',
  margin: 0,
  padding: 0,
};

const codeTagStyle: React.CSSProperties = {
  background: 'transparent',
};

/**
 * Inline `<code>` handler for react-markdown.
 * Fenced code blocks are handled by `PreBlock` (the `pre` element handler),
 * so this component only needs to render inline spans.
 */
export function CodeBlock({ className, children, ...rest }: ComponentPropsWithoutRef<'code'>) {
  return (
    <code className={className} {...rest}>
      {children}
    </code>
  );
}

interface PreBlockProps {
  children?: ReactNode;
}

interface CodeChildProps {
  className?: string;
  children?: ReactNode;
}

/**
 * `<pre>` handler for react-markdown. Replaces the default <pre> wrapper
 * with our styled `.code-block-wrapper` so there is exactly one box around
 * fenced code blocks (fixes the double-box rendering bug).
 */
export function PreBlock({ children }: PreBlockProps) {
  // react-markdown passes a single <code> React element as children for
  // fenced code blocks.
  if (!isValidElement(children)) {
    return <pre>{children}</pre>;
  }

  const codeEl = children as ReactElement<CodeChildProps>;
  const className = codeEl.props.className ?? '';
  const match = LANGUAGE_RE.exec(className);
  const codeString = String(codeEl.props.children ?? '').replace(/\n$/, '');

  if (!match) {
    // Fenced block without a language — render a plain styled wrapper so it
    // still gets the single-box treatment.
    return (
      <div className="code-block-wrapper code-block-wrapper--plain">
        <CopyButton text={codeString} />
        <pre className="code-block-pre">
          <code>{codeString}</code>
        </pre>
      </div>
    );
  }

  const language = match[1];
  return (
    <div className="code-block-wrapper">
      <span className="code-lang-badge" aria-label={`${language} code`}>
        {language}
      </span>
      <CopyButton text={codeString} />
      <Suspense
        fallback={
          <pre className="code-block-pre">
            <code>{codeString}</code>
          </pre>
        }
      >
        <HighlightedCode language={language} code={codeString} />
      </Suspense>
    </div>
  );
}

// Separated to avoid inline component inside PreBlock (rerender-no-inline-components)
function HighlightedCode({ language, code }: { language: string; code: string }) {
  const [theme, setTheme] = useState<Record<string, React.CSSProperties> | null>(null);

  // Load theme once (rerender-lazy-state-init pattern via promise)
  if (!theme) {
    themePromise.then(setTheme);
    return (
      <pre className="code-block-pre">
        <code>{code}</code>
      </pre>
    );
  }

  return (
    <SyntaxHighlighter
      language={language}
      style={theme}
      customStyle={highlighterStyle}
      codeTagProps={{ style: codeTagStyle }}
      PreTag="div"
      className="code-block-pre"
    >
      {code}
    </SyntaxHighlighter>
  );
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = useCallback(() => {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  }, [text]);

  return (
    <button
      type="button"
      className="code-copy-btn"
      onClick={handleCopy}
      aria-label="Copy code to clipboard"
    >
      {copied ? 'COPIED' : 'COPY'}
    </button>
  );
}
