package subagent

import (
	"context"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func TestAutoApproveGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result *valueobject.SubAgentResult
	}{
		{
			name: "approves completed result",
			result: &valueobject.SubAgentResult{
				AgentName: "test-agent",
				Output:    "some output",
				Status:    valueobject.SubAgentStatusCompleted,
			},
		},
		{
			name: "approves empty output",
			result: &valueobject.SubAgentResult{
				AgentName: "empty-agent",
				Output:    "",
				Status:    valueobject.SubAgentStatusCompleted,
			},
		},
		{
			name: "approves result with usage",
			result: &valueobject.SubAgentResult{
				AgentName: "usage-agent",
				Output:    "detailed analysis",
				Status:    valueobject.SubAgentStatusCompleted,
				Usage:     valueobject.SubAgentUsage{TotalTokens: 500},
				Elapsed:   2 * time.Second,
			},
		},
	}

	gate := AutoApproveGate{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			decision, err := gate.Review(context.Background(), tt.result)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !decision.Approved {
				t.Error("AutoApproveGate should always approve")
			}
			if decision.Canceled {
				t.Error("AutoApproveGate should never cancel")
			}
			if decision.Feedback != "" {
				t.Errorf("Feedback = %q, want empty", decision.Feedback)
			}
		})
	}
}

func TestDefaultApprovalConfig(t *testing.T) {
	t.Parallel()
	cfg := DefaultApprovalConfig()

	if cfg.Gate == nil {
		t.Fatal("Gate should not be nil")
	}
	if _, ok := cfg.Gate.(AutoApproveGate); !ok {
		t.Errorf("Gate type = %T, want AutoApproveGate", cfg.Gate)
	}
	if cfg.MaxRevisions != 3 {
		t.Errorf("MaxRevisions = %d, want 3", cfg.MaxRevisions)
	}
}
