package subagent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// CLIReviewer presents subagent output in the terminal for human review.
// It reads decisions from the provided io.Reader and writes prompts to
// the provided io.Writer, making it testable without real stdin/stdout.
type CLIReviewer struct {
	reader *bufio.Reader
	writer io.Writer
}

// NewCLIReviewer creates a reviewer that reads from r and writes to w.
// For interactive use, pass os.Stdin and os.Stdout.
func NewCLIReviewer(r io.Reader, w io.Writer) *CLIReviewer {
	return &CLIReviewer{
		reader: bufio.NewReader(r),
		writer: w,
	}
}

// Review displays the subagent result and prompts the user for a decision.
// Input "y"/"yes" approves, "c"/"cancel" cancels, and any other input
// is treated as rejection feedback. Prefix "n " is stripped from feedback.
func (rv *CLIReviewer) Review(ctx context.Context, result *valueobject.SubAgentResult) (ApprovalDecision, error) {
	select {
	case <-ctx.Done():
		return ApprovalDecision{Canceled: true}, ctx.Err()
	default:
	}

	fmt.Fprintf(rv.writer, "\n--- APPROVAL REQUIRED ---\n")
	fmt.Fprintf(rv.writer, "Agent: %s\n", result.AgentName)
	fmt.Fprintf(rv.writer, "Output:\n%s\n", result.Output)
	fmt.Fprintf(rv.writer, "Tokens: %d | Elapsed: %s\n", result.Usage.TotalTokens, result.Elapsed)
	fmt.Fprintf(rv.writer, "\nApprove? (y)es / (n)o + feedback / (c)ancel: ")

	input, err := rv.reader.ReadString('\n')
	if err != nil {
		return ApprovalDecision{}, fmt.Errorf("read input: %w", err)
	}

	input = strings.TrimSpace(input)
	switch {
	case strings.EqualFold(input, "y") || strings.EqualFold(input, "yes"):
		return ApprovalDecision{Approved: true}, nil
	case strings.EqualFold(input, "c") || strings.EqualFold(input, "cancel"):
		return ApprovalDecision{Canceled: true}, nil
	default:
		feedback := input
		if len(input) > 2 && strings.EqualFold(input[:2], "n ") {
			feedback = input[2:]
		}
		return ApprovalDecision{Approved: false, Feedback: feedback}, nil
	}
}
