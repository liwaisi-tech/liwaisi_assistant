import { render, screen, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { I18nTestWrapper } from '../../../../test/i18n-test-utils';
import { A2UIMessageRenderer, parseResolvedAnswers } from '../A2UIMessageRenderer';
import type { A2UIPayload, A2UIAction } from '../types';

const wrapper = I18nTestWrapper;

function renderWithPayload(payload: A2UIPayload, onAction = vi.fn()) {
  return {
    onAction,
    ...render(
      <A2UIMessageRenderer payload={payload} isStreaming={false} onAction={onAction} />,
      { wrapper },
    ),
  };
}

describe('A2UIMessageRenderer', () => {
  it('renders a text component', () => {
    renderWithPayload({
      components: [{ type: 'text', props: { content: 'Hello from A2UI' } }],
    });
    expect(screen.getByText('Hello from A2UI')).toBeInTheDocument();
  });

  it('renders a button and dispatches action on click', () => {
    const onAction = vi.fn();
    renderWithPayload(
      {
        components: [
          { type: 'button', props: { label: 'Click me', id: 'btn-1', payload: { key: 'val' } } },
        ],
      },
      onAction,
    );

    const button = screen.getByText('Click me');
    expect(button).toBeInTheDocument();

    fireEvent.click(button);
    expect(onAction).toHaveBeenCalledWith({
      type: 'click',
      componentId: 'btn-1',
      payload: { key: 'val' },
    } satisfies A2UIAction);
  });

  it('renders a card with title and children', () => {
    renderWithPayload({
      components: [
        {
          type: 'card',
          props: { title: 'My Card' },
          children: [{ type: 'text', props: { content: 'Card body text' } }],
        },
      ],
    });
    expect(screen.getByText('My Card')).toBeInTheDocument();
    expect(screen.getByText('Card body text')).toBeInTheDocument();
  });

  it('renders a code block', () => {
    renderWithPayload({
      components: [{ type: 'code', props: { code: 'console.log("hi")', language: 'js' } }],
    });
    expect(screen.getByText('console.log("hi")')).toBeInTheDocument();
    expect(screen.getByText('js')).toBeInTheDocument();
  });

  it('renders a progress bar', () => {
    renderWithPayload({
      components: [{ type: 'progress', props: { value: 75, max: 100, label: 'Loading...' } }],
    });
    expect(screen.getByText('Loading...')).toBeInTheDocument();
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', '75');
  });

  it('renders an alert with severity', () => {
    renderWithPayload({
      components: [{ type: 'alert', props: { severity: 'warn', message: 'Be careful!' } }],
    });
    expect(screen.getByRole('alert')).toBeInTheDocument();
    expect(screen.getByText('Be careful!')).toBeInTheDocument();
  });

  it('renders a badge', () => {
    renderWithPayload({
      components: [{ type: 'badge', props: { label: 'Planner' } }],
    });
    expect(screen.getByText('Planner')).toBeInTheDocument();
  });

  it('renders a list', () => {
    renderWithPayload({
      components: [{ type: 'list', props: { items: ['Item A', 'Item B'] } }],
    });
    expect(screen.getByText('Item A')).toBeInTheDocument();
    expect(screen.getByText('Item B')).toBeInTheDocument();
  });

  it('renders a divider', () => {
    const { container } = renderWithPayload({
      components: [{ type: 'divider', props: {} }],
    });
    expect(container.querySelector('hr')).toBeInTheDocument();
  });

  it('shows placeholder for unknown component type', () => {
    renderWithPayload({
      components: [{ type: 'widget_xyz', props: {} }],
    });
    expect(screen.getByText(/Unsupported component: widget_xyz/)).toBeInTheDocument();
  });

  it('renders multiple components', () => {
    renderWithPayload({
      components: [
        { type: 'text', props: { content: 'First' } },
        { type: 'text', props: { content: 'Second' } },
      ],
    });
    expect(screen.getByText('First')).toBeInTheDocument();
    expect(screen.getByText('Second')).toBeInTheDocument();
  });

  it('shows streaming cursor when isStreaming is true', () => {
    const { container } = render(
      <A2UIMessageRenderer
        payload={{ components: [{ type: 'text', props: { content: 'streaming' } }] }}
        isStreaming={true}
        onAction={vi.fn()}
      />,
      { wrapper },
    );
    expect(container.querySelector('.streaming-cursor-inline')).toBeInTheDocument();
  });

  describe('parseResolvedAnswers', () => {
    let warnSpy: ReturnType<typeof vi.spyOn>;

    beforeEach(() => {
      warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    });

    afterEach(() => {
      warnSpy.mockRestore();
    });

    it('parses the wrapped shape', () => {
      const result = parseResolvedAnswers('{"answers":{"q1":"opt-a","q2":"opt-b"}}');
      expect(result).toEqual({ q1: 'opt-a', q2: 'opt-b' });
      expect(warnSpy).not.toHaveBeenCalled();
    });

    it('parses the flat shape from the current emitter (AC-FE-005 fixture)', () => {
      const result = parseResolvedAnswers(
        '{"q1":"es software yo soy el AI engineer","q2":"opt-c"}',
      );
      expect(result).toEqual({
        q1: 'es software yo soy el AI engineer',
        q2: 'opt-c',
      });
      expect(warnSpy).not.toHaveBeenCalled();
    });

    it('returns {} for action envelope approve and does not warn', () => {
      const result = parseResolvedAnswers('{"action":"approve"}');
      expect(result).toEqual({});
      expect(warnSpy).not.toHaveBeenCalled();
    });

    it('returns {} for action envelope revise and does not warn', () => {
      const result = parseResolvedAnswers('{"action":"revise","content":"tighten"}');
      expect(result).toEqual({});
      expect(warnSpy).not.toHaveBeenCalled();
    });

    it('warns with JSON parse error on malformed JSON', () => {
      const result = parseResolvedAnswers('{not-valid');
      expect(result).toEqual({});
      expect(warnSpy).toHaveBeenCalledTimes(1);
      expect(warnSpy).toHaveBeenCalledWith(
        'parseResolvedAnswers: JSON parse error',
        '{not-valid',
      );
    });

    it('warns with non-object JSON on array root', () => {
      const result = parseResolvedAnswers('[1,2,3]');
      expect(result).toEqual({});
      expect(warnSpy).toHaveBeenCalledTimes(1);
      expect(warnSpy.mock.calls[0][0]).toBe('parseResolvedAnswers: non-object JSON');
    });

    it('warns with non-object JSON on null root', () => {
      const result = parseResolvedAnswers('null');
      expect(result).toEqual({});
      expect(warnSpy).toHaveBeenCalledTimes(1);
      expect(warnSpy.mock.calls[0][0]).toBe('parseResolvedAnswers: non-object JSON');
    });

    it('coerces non-string values to string', () => {
      const result = parseResolvedAnswers('{"q1":42,"q2":true}');
      expect(result).toEqual({ q1: '42', q2: 'true' });
      expect(warnSpy).not.toHaveBeenCalled();
    });

    it('returns {} for empty object with no warnings', () => {
      const result = parseResolvedAnswers('{}');
      expect(result).toEqual({});
      expect(warnSpy).not.toHaveBeenCalled();
    });

    it('truncates preview to 120 characters on parse error', () => {
      const long = '{' + 'x'.repeat(500);
      parseResolvedAnswers(long);
      expect(warnSpy).toHaveBeenCalledTimes(1);
      const preview = warnSpy.mock.calls[0][1] as string;
      expect(preview.length).toBeLessThanOrEqual(120);
    });
  });

  it('renders form and dispatches submit action', () => {
    const onAction = vi.fn();
    renderWithPayload(
      {
        components: [
          {
            type: 'form',
            props: {
              id: 'form-1',
              fields: [{ name: 'email', label: 'Email', type: 'email', placeholder: 'you@example.com' }],
              submitLabel: 'Send',
            },
          },
        ],
      },
      onAction,
    );

    const input = screen.getByPlaceholderText('you@example.com');
    fireEvent.change(input, { target: { value: 'test@test.com' } });
    fireEvent.click(screen.getByText('Send'));

    expect(onAction).toHaveBeenCalledTimes(1);
    expect(onAction).toHaveBeenCalledWith(
      expect.objectContaining({
        type: 'submit',
        componentId: 'form-1',
      }),
    );
  });

  // Regression: when resolvedPayload flips undefined → defined live (after a
  // HITL submit), the questionnaire must swap to its locked view without
  // throwing a Rules-of-Hooks "Rendered fewer hooks" error. The early
  // locked-render must sit AFTER every useState call.
  it('swaps to locked view live when resolvedPayload arrives without crashing', () => {
    const payload: A2UIPayload = {
      components: [
        {
          type: 'questionnaire',
          props: { id: 't-clarify', submitLabel: 'Send answers' },
          children: [
            {
              type: 'choice',
              props: {
                id: 'q1',
                label: 'Pick one',
                options: [
                  { id: 'opt-a', label: 'Option A' },
                  { id: 'opt-b', label: 'Option B' },
                ],
              },
            },
          ],
        },
      ],
    };

    const { rerender } = render(
      <A2UIMessageRenderer payload={payload} isStreaming={false} onAction={vi.fn()} />,
      { wrapper },
    );
    expect(screen.getByText('Send answers')).toBeInTheDocument();

    rerender(
      <A2UIMessageRenderer
        payload={payload}
        isStreaming={false}
        onAction={vi.fn()}
        resolvedPayload={'{"q1":"opt-a"}'}
        resolvedAt={new Date('2026-04-16T12:00:00Z')}
      />,
    );

    expect(screen.queryByText('Send answers')).not.toBeInTheDocument();
    expect(screen.getByText('Option A')).toBeInTheDocument();
  });
});
