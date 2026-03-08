package planner

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// --- LLM mock for enhanced decomposer ---

type enhancedDecomposerFakeLLM struct {
	responses []string
	calls     int
	err       error
}

func (m *enhancedDecomposerFakeLLM) Complete(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	idx := m.calls
	m.calls++
	if idx < len(m.responses) {
		return &output.ChatResponse{Content: m.responses[idx]}, nil
	}
	return &output.ChatResponse{Content: "[]"}, nil
}

func (m *enhancedDecomposerFakeLLM) CompleteStream(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

var testRoles = []valueobject.RoleSpec{
	{Name: "software-architect", Perspective: "system design", Instruction: "eval arch"},
	{Name: "qa-engineer", Perspective: "testing", Instruction: "eval qa"},
}

func TestEnhancedDecomposer_Decompose(t *testing.T) {
	tests := []struct {
		name     string
		response string
		llmErr   error
		wantErr  bool
		wantIDs  []string
	}{
		{
			name:     "single task",
			response: `[{"id":"t1","description":"do t1","instruction":"inst"}]`,
			wantIDs:  []string{"t1"},
		},
		{
			name:     "multiple tasks with deps",
			response: `[{"id":"t1","description":"first","instruction":"do first"},{"id":"t2","description":"second","instruction":"do second","depends_on":["t1"]}]`,
			wantIDs:  []string{"t1", "t2"},
		},
		{
			name:     "empty task list",
			response: `[]`,
			wantErr:  true,
		},
		{
			name:     "invalid JSON",
			response: `not json`,
			wantErr:  true,
		},
		{
			name:    "LLM error",
			llmErr:  fmt.Errorf("timeout"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &enhancedDecomposerFakeLLM{responses: []string{tt.response}, err: tt.llmErr}
			decomposer := NewEnhancedDecomposer(mock, "test-model")

			graph, err := decomposer.Decompose(context.Background(), "complex task", testRoles)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(graph.Tasks) != len(tt.wantIDs) {
				t.Fatalf("got %d tasks, want %d", len(graph.Tasks), len(tt.wantIDs))
			}

			for i, wantID := range tt.wantIDs {
				if graph.Tasks[i].ID != wantID {
					t.Errorf("task[%d].ID = %q, want %q", i, graph.Tasks[i].ID, wantID)
				}
			}
		})
	}
}

func TestEnhancedDecomposer_DecomposeWithFeedback(t *testing.T) {
	mock := &enhancedDecomposerFakeLLM{
		responses: []string{`[{"id":"t1","description":"improved task","instruction":"better inst"}]`},
	}
	decomposer := NewEnhancedDecomposer(mock, "test-model")

	graph, err := decomposer.DecomposeWithFeedback(
		context.Background(),
		"complex task",
		testRoles,
		"need more granularity",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(graph.Tasks) != 1 {
		t.Errorf("got %d tasks, want 1", len(graph.Tasks))
	}
}

func TestFormatRolesForPrompt(t *testing.T) {
	result := formatRolesForPrompt(testRoles)
	if !strings.Contains(result, "software-architect") {
		t.Error("expected architect role in output")
	}
	if !strings.Contains(result, "qa-engineer") {
		t.Error("expected QA role in output")
	}
	if !strings.Contains(result, "1.") || !strings.Contains(result, "2.") {
		t.Error("expected numbered list")
	}
}

func TestFormatRolesForPrompt_Empty(t *testing.T) {
	result := formatRolesForPrompt(nil)
	if !strings.Contains(result, "No specific roles") {
		t.Errorf("expected fallback text, got %q", result)
	}
}
