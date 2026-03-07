package subagent

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func testResult() *valueobject.SubAgentResult {
	return &valueobject.SubAgentResult{
		AgentName: "test-agent",
		Output:    "generated output",
		Status:    valueobject.SubAgentStatusCompleted,
		Usage:     valueobject.SubAgentUsage{TotalTokens: 100},
		Elapsed:   time.Second,
	}
}

func TestCLIReviewer_Review(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		input        string
		wantApproved bool
		wantCanceled bool
		wantFeedback string
	}{
		{
			name:         "approve with y",
			input:        "y\n",
			wantApproved: true,
		},
		{
			name:         "approve with yes",
			input:        "yes\n",
			wantApproved: true,
		},
		{
			name:         "approve with YES",
			input:        "YES\n",
			wantApproved: true,
		},
		{
			name:         "cancel with c",
			input:        "c\n",
			wantCanceled: true,
		},
		{
			name:         "cancel with cancel",
			input:        "cancel\n",
			wantCanceled: true,
		},
		{
			name:         "cancel with CANCEL",
			input:        "CANCEL\n",
			wantCanceled: true,
		},
		{
			name:         "reject with n prefix strips prefix",
			input:        "n needs more detail\n",
			wantFeedback: "needs more detail",
		},
		{
			name:         "reject with plain feedback",
			input:        "the output is wrong\n",
			wantFeedback: "the output is wrong",
		},
		{
			name:         "reject with just n",
			input:        "n\n",
			wantFeedback: "n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reader := strings.NewReader(tt.input)
			var out bytes.Buffer
			rv := NewCLIReviewer(reader, &out)

			decision, err := rv.Review(context.Background(), testResult())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if decision.Approved != tt.wantApproved {
				t.Errorf("Approved = %v, want %v", decision.Approved, tt.wantApproved)
			}
			if decision.Canceled != tt.wantCanceled {
				t.Errorf("Canceled = %v, want %v", decision.Canceled, tt.wantCanceled)
			}
			if decision.Feedback != tt.wantFeedback {
				t.Errorf("Feedback = %q, want %q", decision.Feedback, tt.wantFeedback)
			}

			output := out.String()
			if !strings.Contains(output, "APPROVAL REQUIRED") {
				t.Error("output should contain APPROVAL REQUIRED header")
			}
			if !strings.Contains(output, "test-agent") {
				t.Error("output should contain agent name")
			}
		})
	}
}

func TestCLIReviewer_ContextCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reader := strings.NewReader("y\n")
	var out bytes.Buffer
	rv := NewCLIReviewer(reader, &out)

	decision, err := rv.Review(ctx, testResult())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !decision.Canceled {
		t.Error("decision should be canceled on context cancellation")
	}
}

func TestCLIReviewer_ReadError(t *testing.T) {
	t.Parallel()

	reader := strings.NewReader("")
	var out bytes.Buffer
	rv := NewCLIReviewer(reader, &out)

	_, err := rv.Review(context.Background(), testResult())
	if err == nil {
		t.Fatal("expected error on empty input, got nil")
	}
	if !strings.Contains(err.Error(), "read input") {
		t.Errorf("error = %q, should contain 'read input'", err.Error())
	}
}
