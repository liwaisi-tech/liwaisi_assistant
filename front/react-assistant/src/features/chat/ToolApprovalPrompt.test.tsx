import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { I18nTestWrapper } from '../../test/i18n-test-utils';
import { ToolApprovalPrompt } from './ToolApprovalPrompt';
import type { A2UIPayload } from './a2ui/types';

// Spy the api module so the component's POST is observable + controllable
// without a real fetch. Vitest hoists vi.mock above imports automatically.
vi.mock('../../services/api', () => ({
  submitToolApproval: vi.fn(),
}));

import { submitToolApproval } from '../../services/api';

const wrapper = I18nTestWrapper;

const samplePreview: A2UIPayload = {
  components: [
    {
      type: 'text',
      props: { content: 'New tool: file_summarize', isStreaming: false },
    },
  ],
};

describe('ToolApprovalPrompt', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('calls submitToolApproval with "approved" and onResolved on success', async () => {
    (submitToolApproval as ReturnType<typeof vi.fn>).mockResolvedValueOnce(undefined);
    const onResolved = vi.fn();
    const user = userEvent.setup();

    render(
      <ToolApprovalPrompt
        requestId="req-123"
        preview={samplePreview}
        sessionId="sess-1"
        onResolved={onResolved}
      />,
      { wrapper },
    );

    await user.click(screen.getByTestId('tool-approval-approve'));

    await waitFor(() => {
      expect(submitToolApproval).toHaveBeenCalledWith('sess-1', 'req-123', 'approved');
      expect(onResolved).toHaveBeenCalledTimes(1);
    });
  });

  it('shows an error and stays mounted when submit fails', async () => {
    (submitToolApproval as ReturnType<typeof vi.fn>).mockRejectedValueOnce(
      new Error('boom'),
    );
    const onResolved = vi.fn();
    const user = userEvent.setup();

    render(
      <ToolApprovalPrompt
        requestId="req-err"
        preview={samplePreview}
        sessionId="sess-1"
        onResolved={onResolved}
      />,
      { wrapper },
    );

    await user.click(screen.getByTestId('tool-approval-deny'));

    await waitFor(() => {
      expect(screen.getByTestId('tool-approval-error')).toBeInTheDocument();
    });
    expect(onResolved).not.toHaveBeenCalled();
    // Buttons re-enabled for retry
    expect(screen.getByTestId('tool-approval-approve')).not.toBeDisabled();
    expect(screen.getByTestId('tool-approval-deny')).not.toBeDisabled();
  });
});
