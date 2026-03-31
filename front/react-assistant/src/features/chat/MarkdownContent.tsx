import { Component, useDeferredValue } from 'react';
import type { ReactNode } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import remarkMath from 'remark-math';
import rehypeKatex from 'rehype-katex';
import rehypeSanitize, { defaultSchema } from 'rehype-sanitize';
import type { Components } from 'react-markdown';
import { CodeBlock } from './CodeBlock.tsx';

// --- Module-level constants (rerender-memo / rerender-no-inline-components) ---
// Stable references prevent react-markdown from re-initializing its
// unified pipeline on every streaming chunk.

// Sanitization schema: extend defaults to allow KaTeX HTML + MathML output
const sanitizeSchema = {
  ...defaultSchema,
  tagNames: [
    ...(defaultSchema.tagNames ?? []),
    // MathML elements (KaTeX semantic output)
    'math', 'semantics', 'annotation', 'mrow', 'mi', 'mn', 'mo',
    'msup', 'msub', 'mfrac', 'msqrt', 'mroot', 'mover', 'munder',
    'munderover', 'mtable', 'mtr', 'mtd', 'mtext', 'mspace', 'mpadded',
    'menclose', 'mglyph', 'mphantom', 'mstyle',
  ],
  attributes: {
    ...defaultSchema.attributes,
    // Allow KaTeX class names and inline styles on spans
    span: [
      ...(defaultSchema.attributes?.['span'] ?? []),
      'className', 'style', 'aria-hidden',
    ],
    // MathML attributes
    math: ['xmlns', 'display'],
    annotation: ['encoding'],
    mo: ['fence', 'stretchy', 'symmetric', 'lspace', 'rspace', 'minsize', 'maxsize'],
    mspace: ['width'],
    mtable: ['columnalign', 'columnspacing', 'rowspacing'],
    mtd: ['columnalign'],
    mpadded: ['width', 'height', 'depth', 'lspace', 'voffset'],
    mstyle: ['mathsize', 'scriptlevel', 'displaystyle'],
    // Code blocks
    code: [
      ...(defaultSchema.attributes?.['code'] ?? []),
      'className',
    ],
  },
};

const remarkPlugins = [remarkGfm, remarkMath];
// rehype-sanitize AFTER rehype-katex: sanitizes KaTeX's HTML output
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const rehypePlugins: any[] = [rehypeKatex, [rehypeSanitize, sanitizeSchema]];

function CustomLink({ href, children }: { href?: string; children?: ReactNode }) {
  return (
    <a href={href} target="_blank" rel="noopener noreferrer">
      {children}
    </a>
  );
}

function ScrollableTable({ children }: { children?: ReactNode }) {
  return (
    <div style={{ overflowX: 'auto' }}>
      <table>{children}</table>
    </div>
  );
}

function DisabledImage() {
  return null;
}

const markdownComponents: Components = {
  code: CodeBlock,
  a: CustomLink,
  table: ScrollableTable,
  img: DisabledImage,
};

// --- Error boundary ---

interface ErrorBoundaryProps {
  fallbackContent: string;
  children: ReactNode;
}

interface ErrorBoundaryState {
  hasError: boolean;
}

class MarkdownErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { hasError: false };

  static getDerivedStateFromError(): ErrorBoundaryState {
    return { hasError: true };
  }

  render() {
    if (this.state.hasError) {
      return (
        <p
          className="text-sm leading-relaxed whitespace-pre-wrap break-words"
          style={{ color: 'var(--text-primary)', margin: 0 }}
        >
          {this.props.fallbackContent}
        </p>
      );
    }
    return this.props.children;
  }
}

// --- Main component ---

interface MarkdownContentProps {
  content: string;
  isStreaming: boolean;
}

export function MarkdownContent({ content, isStreaming }: MarkdownContentProps) {
  // Defer expensive markdown re-parsing to keep UI responsive during streaming
  // (rerender-use-deferred-value)
  const deferredContent = useDeferredValue(content);

  return (
    <MarkdownErrorBoundary fallbackContent={content}>
      <div className="markdown-body">
        <ReactMarkdown remarkPlugins={remarkPlugins} rehypePlugins={rehypePlugins} components={markdownComponents}>
          {deferredContent}
        </ReactMarkdown>
        {isStreaming ? (
          <span className="streaming-cursor-inline" aria-hidden="true" />
        ) : null}
      </div>
    </MarkdownErrorBoundary>
  );
}
