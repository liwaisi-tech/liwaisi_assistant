package subagent

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

const defaultMaxRevisions = 3

// ApprovalDecision represents a reviewer's judgment on a subagent output.
type ApprovalDecision struct {
	Approved bool
	Feedback string
	Canceled bool
}

// ApprovalGate reviews a subagent's output before it is accepted.
// Implementations must be safe for concurrent use.
type ApprovalGate interface {
	// Review presents the output for review and returns the decision.
	// Implementations should respect context cancellation.
	Review(ctx context.Context, result *valueobject.SubAgentResult) (ApprovalDecision, error)
}

// AutoApproveGate approves all outputs without human review.
type AutoApproveGate struct{}

// Review always returns an approved decision.
func (AutoApproveGate) Review(_ context.Context, _ *valueobject.SubAgentResult) (ApprovalDecision, error) {
	return ApprovalDecision{Approved: true}, nil
}

// ApprovalConfig configures the approval gate for a subagent execution.
type ApprovalConfig struct {
	Gate         ApprovalGate
	MaxRevisions int
}

// DefaultApprovalConfig returns config with auto-approve (no human review)
// and a maximum of 3 revision cycles.
func DefaultApprovalConfig() ApprovalConfig {
	return ApprovalConfig{
		Gate:         AutoApproveGate{},
		MaxRevisions: defaultMaxRevisions,
	}
}
