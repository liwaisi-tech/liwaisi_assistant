import { lazy, Suspense, useState, useCallback } from 'react';
import type { ComponentPropsWithoutRef } from 'react';

const SyntaxHighlighter = lazy(() =>
  import('./syntax-highlighter').then(mod => ({
    default: mod.default,
  }))
);

const themePromise = import('./syntax-highlighter').then(mod => mod.deepSpaceTheme);

// Regex hoisted outside component (js-hoist-regexp)
const LANGUAGE_RE = /language-(\w+)/;

// Transparent background so parent <pre> controls it
const highlighterStyle: React.CSSProperties = {
  background: 'transparent',
  margin: 0,
  padding: 0,
};

function CodeBlockFallback({ children }: { children: React.ReactNode }) {
  return (
    <code style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: '0.8125rem' }}>
      {children}
    </code>
  );
}

export function CodeBlock({ className, children, ...rest }: ComponentPropsWithoutRef<'code'>) {
  const match = LANGUAGE_RE.exec(className || '');
  const codeString = String(children).replace(/\n$/, '');

  // Inline code — no language class (js-early-exit)
  if (!match) {
    return (
      <code className={className} {...rest}>
        {children}
      </code>
    );
  }

  const language = match[1];

  return (
    <div className="code-block-wrapper">
      <span className="code-lang-badge" aria-label={`${language} code`}>
        {language}
      </span>
      <CopyButton text={codeString} />
      <Suspense fallback={<CodeBlockFallback>{children}</CodeBlockFallback>}>
        <HighlightedCode language={language} code={codeString} />
      </Suspense>
    </div>
  );
}

// Separated to avoid inline component inside CodeBlock (rerender-no-inline-components)
function HighlightedCode({ language, code }: { language: string; code: string }) {
  const [theme, setTheme] = useState<Record<string, React.CSSProperties> | null>(null);

  // Load theme once (rerender-lazy-state-init pattern via promise)
  if (!theme) {
    themePromise.then(setTheme);
    return (
      <code style={{ fontFamily: "'JetBrains Mono', monospace", fontSize: '0.8125rem' }}>
        {code}
      </code>
    );
  }

  return (
    <SyntaxHighlighter
      language={language}
      style={theme}
      customStyle={highlighterStyle}
      codeTagProps={{ style: {} }}
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
