import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { I18nTestWrapper } from '../../test/i18n-test-utils';
import { MessageBubble } from './MessageBubble';

// Mock MarkdownContent since it's an internal dependency with its own rendering concerns
vi.mock('./MarkdownContent.tsx', () => ({
  MarkdownContent: ({ content }: { content: string; isStreaming: boolean }) => (
    <div data-testid="markdown-content">{content}</div>
  ),
}));

const wrapper = I18nTestWrapper;

describe('MessageBubble', () => {
  const baseProps = {
    timestamp: new Date('2025-01-15T10:30:00Z'),
  };

  it('should render user message content as plain text', () => {
    render(
      <MessageBubble
        {...baseProps}
        role="user"
        content="Hello, world!"
      />,
      { wrapper },
    );

    expect(screen.getByText('Hello, world!')).toBeInTheDocument();
  });

  it('should render assistant message content through MarkdownContent', () => {
    render(
      <MessageBubble
        {...baseProps}
        role="assistant"
        content="I can help with that."
      />,
      { wrapper },
    );

    expect(screen.getByTestId('markdown-content')).toHaveTextContent('I can help with that.');
  });

  it('should apply user styling (right-aligned)', () => {
    const { container } = render(
      <MessageBubble
        {...baseProps}
        role="user"
        content="User message"
      />,
      { wrapper },
    );

    const outerDiv = container.firstElementChild as HTMLElement;
    expect(outerDiv.className).toContain('justify-end');
  });

  it('should apply assistant styling (left-aligned)', () => {
    const { container } = render(
      <MessageBubble
        {...baseProps}
        role="assistant"
        content="Assistant message"
      />,
      { wrapper },
    );

    const outerDiv = container.firstElementChild as HTMLElement;
    expect(outerDiv.className).toContain('justify-start');
  });

  it('should display timestamp', () => {
    render(
      <MessageBubble
        {...baseProps}
        role="user"
        content="Test"
      />,
      { wrapper },
    );

    const timeEl = screen.getByRole('time');
    expect(timeEl).toBeInTheDocument();
    expect(timeEl).toHaveAttribute('datetime', '2025-01-15T10:30:00.000Z');
  });

  it('should display CPN role badge for assistant messages', () => {
    render(
      <MessageBubble
        {...baseProps}
        role="assistant"
        content="Response"
        cpnRole="orchestrator"
      />,
      { wrapper },
    );

    expect(screen.getByText('orchestrator')).toBeInTheDocument();
  });

  it('should not display CPN role badge for user messages', () => {
    render(
      <MessageBubble
        {...baseProps}
        role="user"
        content="Question"
        cpnRole="orchestrator"
      />,
      { wrapper },
    );

    expect(screen.queryByText('orchestrator')).not.toBeInTheDocument();
  });

  it('should show HITL action buttons when hitlActions are present and not resolved', () => {
    render(
      <MessageBubble
        {...baseProps}
        role="assistant"
        content="Please approve"
        hitlTransitionId="t-1"
        hitlActions={['approve', 'reject']}
        onHITLAction={vi.fn()}
      />,
      { wrapper },
    );

    expect(screen.getByText('Looks good')).toBeInTheDocument();
    expect(screen.getByText('Cancel')).toBeInTheDocument();
  });

  it('should show resolved state when hitlResolved is set', () => {
    render(
      <MessageBubble
        {...baseProps}
        role="assistant"
        content="Approved action"
        hitlTransitionId="t-1"
        hitlActions={['approve', 'reject']}
        hitlResolved="approve"
      />,
      { wrapper },
    );

    expect(screen.getByText('You approved this')).toBeInTheDocument();
    expect(screen.queryByText('Looks good')).not.toBeInTheDocument();
  });
});
