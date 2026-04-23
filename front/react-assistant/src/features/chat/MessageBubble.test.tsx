import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi } from 'vitest';
import { I18nTestWrapper } from '../../test/i18n-test-utils';
import { MessageBubble } from './MessageBubble';

// Mock MarkdownContent — used inside the A2UI text component
vi.mock('./MarkdownContent.tsx', () => ({
  MarkdownContent: ({ content }: { content: string; isStreaming: boolean }) => (
    <div data-testid="markdown-content">{content}</div>
  ),
}));

const wrapper = I18nTestWrapper;

// Helper: build A2UI-formatted HITL review card content (as backend would send)
function buildHITLContent(prompt: string, transitionId: string): string {
  return '$$a2ui:' + JSON.stringify({
    components: [
      { type: 'card', props: { title: 'Review Required' }, children: [
        { type: 'text', props: { content: prompt } },
        { type: 'divider', props: {} },
        { type: 'text', props: { content: 'Choose an action to continue:', variant: 'secondary' } },
      ]},
      { type: 'button', props: { label: '✓ Approve', variant: 'success', actionType: 'hitl:approve', id: transitionId } },
      { type: 'button', props: { label: '✎ Request Changes', variant: 'primary', actionType: 'hitl:revise', id: transitionId } },
      { type: 'button', props: { label: '✗ Discard', variant: 'danger', actionType: 'hitl:reject', id: transitionId } },
    ],
  });
}

describe('MessageBubble', () => {
  const baseProps = {
    id: 'msg-test-1',
    timestamp: new Date('2025-01-15T10:30:00Z'),
  };

  it('should render user message content as plain text', () => {
    render(
      <MessageBubble {...baseProps} role="user" content="Hello, world!" />,
      { wrapper },
    );
    expect(screen.getByText('Hello, world!')).toBeInTheDocument();
  });

  it('should render assistant message through A2UI text component (MarkdownContent)', async () => {
    render(
      <MessageBubble {...baseProps} role="assistant" content="I can help with that." />,
      { wrapper },
    );
    await waitFor(() => {
      expect(screen.getByTestId('markdown-content')).toHaveTextContent('I can help with that.');
    });
  });

  it('should apply user styling (right-aligned)', () => {
    const { container } = render(
      <MessageBubble {...baseProps} role="user" content="User message" />,
      { wrapper },
    );
    const outerDiv = container.firstElementChild as HTMLElement;
    expect(outerDiv.className).toContain('justify-end');
  });

  it('should apply assistant styling (left-aligned)', () => {
    const { container } = render(
      <MessageBubble {...baseProps} role="assistant" content="Assistant message" />,
      { wrapper },
    );
    const outerDiv = container.firstElementChild as HTMLElement;
    expect(outerDiv.className).toContain('justify-start');
  });

  it('should display timestamp', () => {
    render(
      <MessageBubble {...baseProps} role="user" content="Test" />,
      { wrapper },
    );
    const timeEl = screen.getByRole('time');
    expect(timeEl).toBeInTheDocument();
    expect(timeEl).toHaveAttribute('datetime', '2025-01-15T10:30:00.000Z');
  });

  it('should display CPN role badge for assistant messages', () => {
    render(
      <MessageBubble {...baseProps} role="assistant" content="Response" cpnRole="orchestrator" />,
      { wrapper },
    );
    expect(screen.getByText('orchestrator')).toBeInTheDocument();
  });

  it('should not display CPN role badge for user messages', () => {
    render(
      <MessageBubble {...baseProps} role="user" content="Question" cpnRole="orchestrator" />,
      { wrapper },
    );
    expect(screen.queryByText('orchestrator')).not.toBeInTheDocument();
  });

  it('should render A2UI HITL review card with approve/revise/discard buttons', async () => {
    const hitlContent = buildHITLContent('Please review the plan above.', 't-review');
    render(
      <MessageBubble
        {...baseProps}
        role="assistant"
        content={hitlContent}
        hitlTransitionId="t-review"
        onHITLAction={vi.fn()}
      />,
      { wrapper },
    );

    await waitFor(() => {
      expect(screen.getByText('Review Required')).toBeInTheDocument();
      expect(screen.getByText('✓ Approve')).toBeInTheDocument();
      expect(screen.getByText('✎ Request Changes')).toBeInTheDocument();
      expect(screen.getByText('✗ Discard')).toBeInTheDocument();
    });
  });

  it('should show resolved badge when hitlResolved is set', () => {
    render(
      <MessageBubble
        {...baseProps}
        role="assistant"
        content="Approved action"
        hitlTransitionId="t-1"
        hitlResolved="approve"
      />,
      { wrapper },
    );
    expect(screen.getByText('You approved this')).toBeInTheDocument();
  });

  // Regression: 'submit' (questionnaire answers) must NOT render the
  // HITL resolved badge. The locked questionnaire narrates itself via
  // "Responded at HH:MM"; the legacy badge would fall through to the
  // reject copy ("You cancelled this"), contradicting the locked view.
  it('should NOT show the resolved badge for submit (questionnaire answers)', () => {
    render(
      <MessageBubble
        {...baseProps}
        role="assistant"
        content="Questionnaire surface"
        hitlTransitionId="t-clarify"
        hitlResolved="submit"
      />,
      { wrapper },
    );
    expect(screen.queryByText('You cancelled this')).not.toBeInTheDocument();
    expect(screen.queryByText('You approved this')).not.toBeInTheDocument();
    expect(screen.queryByText('You requested changes')).not.toBeInTheDocument();
  });

  // ── parsePayload contract (REQ-011 / REQ-012 / AC-008..010 / CON-003) ─────

  it('should route to A2UI renderer when content has leading whitespace before the marker (AC-008 / REQ-011)', async () => {
    const content =
      '\n  \t$$a2ui:' +
      JSON.stringify({ components: [{ type: 'text', props: { content: 'hi' } }] });
    render(
      <MessageBubble {...baseProps} role="assistant" content={content} />,
      { wrapper },
    );
    await waitFor(() => {
      // A2UI text component renders its props.content verbatim (no whitespace prefix)
      expect(screen.getByTestId('markdown-content')).toHaveTextContent('hi');
      // Crucially: the raw JSON / marker is NOT visible
      expect(screen.queryByText(/\$\$a2ui:/)).not.toBeInTheDocument();
    });
  });

  it('should split prefix from inline $$a2ui: marker and never leak it to MarkdownContent (AC-009 / REQ-012)', async () => {
    // The literal "$$a2ui:" must NEVER reach MarkdownContent — remark-math
    // treats `$$…$$` as a KaTeX block and renders the JSON as a broken
    // empty math node. Prefix is rendered, marker and trailing JSON are
    // dropped (or parsed as A2UI when valid; here the suffix "inline" is
    // not valid JSON so it is silently discarded).
    const content = 'Here is the marker $$a2ui: inline';
    render(
      <MessageBubble {...baseProps} role="assistant" content={content} />,
      { wrapper },
    );
    await waitFor(() => {
      const md = screen.getByTestId('markdown-content');
      expect(md).toHaveTextContent('Here is the marker');
      expect(md.textContent ?? '').not.toContain('$$a2ui:');
    });
  });

  it('should render prefix as text AND inline A2UI surface when LLM mimics $$a2ui: mid-message', async () => {
    // Regression: streamed t-execute output sometimes mimics the previous
    // fireHITL surface, producing "...question?\n$$a2ui:{...buttons...}".
    // The prefix must render as markdown and the suffix JSON, when valid,
    // must materialize as A2UI components (not be silently dropped).
    const surface = JSON.stringify({
      components: [
        { type: 'button', props: { label: 'Inline Approve', actionType: 'noop', id: 'x' } },
      ],
    });
    const content = `Plan question?\n$$a2ui:${surface}`;
    render(
      <MessageBubble {...baseProps} role="assistant" content={content} />,
      { wrapper },
    );
    await waitFor(() => {
      expect(screen.getByTestId('markdown-content')).toHaveTextContent('Plan question?');
      expect(screen.getByText('Inline Approve')).toBeInTheDocument();
    });
  });

  it('should route to MarkdownContent when A2UI JSON fails to parse (REQ-012)', async () => {
    const content = '$$a2ui:{not valid json';
    render(
      <MessageBubble {...baseProps} role="assistant" content={content} />,
      { wrapper },
    );
    await waitFor(() => {
      // Fall-through preserves ORIGINAL content byte-identical
      expect(screen.getByTestId('markdown-content')).toHaveTextContent(content);
    });
  });

  it('should route LaTeX display-math content to MarkdownContent untouched (AC-010 / CON-003)', async () => {
    const content = '$$x^2 + y^2 = z^2$$';
    render(
      <MessageBubble {...baseProps} role="assistant" content={content} />,
      { wrapper },
    );
    await waitFor(() => {
      expect(screen.getByTestId('markdown-content')).toHaveTextContent(content);
    });
  });

  // ── Responding-model badge (REQ-GAP-IND-004/005 / AC-IND-001..003) ────────

  it('renders the responding-model badge when metadata.responding_model is set on an assistant message', () => {
    render(
      <MessageBubble
        {...baseProps}
        role="assistant"
        content="Here is your answer."
        metadata={{ responding_model: 'anthropic/claude-opus-4-6 · openrouter' }}
      />,
      { wrapper },
    );
    const badge = screen.getByTestId('responding-model-badge');
    // Vendor prefix stripped for scannability.
    expect(badge).toHaveTextContent('claude-opus-4-6 · openrouter');
    // Full registry id still available via the tooltip.
    expect(badge).toHaveAttribute('title', expect.stringContaining('anthropic/claude-opus-4-6 · openrouter'));
  });

  it('does NOT render the badge for user messages regardless of metadata', () => {
    render(
      <MessageBubble
        {...baseProps}
        role="user"
        content="what time is it?"
        metadata={{ responding_model: 'anthropic/claude-opus-4-6 · openrouter' }}
      />,
      { wrapper },
    );
    expect(screen.queryByTestId('responding-model-badge')).not.toBeInTheDocument();
  });

  it('does NOT render the badge when metadata is absent (backward-compat rehydration)', () => {
    render(
      <MessageBubble {...baseProps} role="assistant" content="Legacy row." />,
      { wrapper },
    );
    expect(screen.queryByTestId('responding-model-badge')).not.toBeInTheDocument();
  });

  it('does NOT render the badge on HITL surfaces (hitlTransitionId set)', () => {
    render(
      <MessageBubble
        {...baseProps}
        role="assistant"
        content="Pending review."
        hitlTransitionId="t-review"
        metadata={{ responding_model: 'google/gemma-4-31b-it · openrouter' }}
      />,
      { wrapper },
    );
    expect(screen.queryByTestId('responding-model-badge')).not.toBeInTheDocument();
  });

  it('does NOT render the badge while streaming (final value arrives on Done=true)', () => {
    render(
      <MessageBubble
        {...baseProps}
        role="assistant"
        content="partial"
        isStreaming
        metadata={{ responding_model: 'anthropic/claude-opus-4-6 · openrouter' }}
      />,
      { wrapper },
    );
    expect(screen.queryByTestId('responding-model-badge')).not.toBeInTheDocument();
  });

  // ── HOST·HITL extended-action encoding (AC-FE-003) ───────────────────────
  // Regression lock for MessageBubble.tsx:137-145. The four host-approval
  // buttons MUST each produce a single onHITLAction call with:
  //   • the correct narrowed HITLAction ('approve' | 'reject')
  //   • a JSON-stringified payload whose `action` field is the extended
  //     four-way keyword — this envelope is what the backend dispatches on
  //     and what the reducer rehydrates from. Spec §4.1.
  describe('HOST·HITL extended-action encoding (AC-FE-003)', () => {
    function buildHostApprovalContent(): string {
      return (
        '$$a2ui:' +
        JSON.stringify({
          schema: 'host.approval',
          hostApproval: {
            command: 'ls -la',
            risk: 'caution',
            rememberAvailable: true,
          },
          components: [],
        })
      );
    }

    async function clickAndAssert(
      accessibleName: RegExp,
      expected: [string, string, string],
    ) {
      const onHITLAction = vi.fn();
      const user = userEvent.setup();
      render(
        <MessageBubble
          {...baseProps}
          role="assistant"
          content={buildHostApprovalContent()}
          hitlTransitionId="t-host-1"
          onHITLAction={onHITLAction}
        />,
        { wrapper },
      );
      const btn = await screen.findByRole('button', { name: accessibleName });
      await user.click(btn);
      expect(onHITLAction).toHaveBeenCalledTimes(1);
      expect(onHITLAction).toHaveBeenCalledWith(...expected);
    }

    it('"Aprobar solo esta vez" → (id, approve, {"action":"approve-once"})', async () => {
      await clickAndAssert(/^Aprobar solo esta vez$/, [
        't-host-1',
        'approve',
        '{"action":"approve-once"}',
      ]);
    });

    it('"Aprobar y recordar" → (id, approve, {"action":"approve-and-remember"})', async () => {
      await clickAndAssert(/^Aprobar y recordar\b/, [
        't-host-1',
        'approve',
        '{"action":"approve-and-remember"}',
      ]);
    });

    it('"Rechazar" → (id, reject, {"action":"deny"})', async () => {
      await clickAndAssert(/^Rechazar\b(?! y )/, [
        't-host-1',
        'reject',
        '{"action":"deny"}',
      ]);
    });

    it('"Rechazar y bloquear para siempre" → (id, reject, {"action":"deny-and-blacklist"})', async () => {
      await clickAndAssert(/^Rechazar y bloquear para siempre\b/, [
        't-host-1',
        'reject',
        '{"action":"deny-and-blacklist"}',
      ]);
    });
  });

  it('falls back to the raw string when responding_model has no vendor prefix', () => {
    render(
      <MessageBubble
        {...baseProps}
        role="assistant"
        content="ok"
        metadata={{ responding_model: 'gemma-4-31b-it · openrouter' }}
      />,
      { wrapper },
    );
    expect(screen.getByTestId('responding-model-badge')).toHaveTextContent('gemma-4-31b-it · openrouter');
  });
});
