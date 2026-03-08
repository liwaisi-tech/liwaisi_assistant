package planner

import (
	"context"
	"fmt"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// --- LLM mock for plan evaluator ---

type evaluatorFakeLLM struct {
	response string
	err      error
}

func (m *evaluatorFakeLLM) Complete(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &output.ChatResponse{Content: m.response}, nil
}

func (m *evaluatorFakeLLM) CompleteStream(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

var testGraph = &entity.PlanGraph{
	Tasks: []*entity.MicroTask{
		{ID: "t1", Description: "first task"},
		{ID: "t2", Description: "second task", DependsOn: []string{"t1"}},
	},
}

func TestPlanEvaluator_Evaluate(t *testing.T) {
	tests := []struct {
		name     string
		response string
		llmErr   error
		wantErr  bool
		wantPass bool
		minScore float64
	}{
		{
			name: "high score passes",
			response: `{
				"score": 0.85,
				"completeness": 0.9,
				"feasibility": 0.8,
				"granularity": 0.85,
				"risk": 0.85,
				"feedback": "solid plan",
				"passed": true
			}`,
			wantPass: true,
			minScore: 0.7,
		},
		{
			name: "low score fails",
			response: `{
				"score": 0.5,
				"completeness": 0.4,
				"feasibility": 0.6,
				"granularity": 0.5,
				"risk": 0.5,
				"feedback": "needs more granularity and risk mitigation",
				"passed": false
			}`,
			wantPass: false,
			minScore: 0.0,
		},
		{
			name: "borderline score (exactly 0.7) passes",
			response: `{
				"score": 0.7,
				"feedback": "just barely sufficient",
				"passed": true
			}`,
			wantPass: true,
			minScore: 0.7,
		},
		{
			name: "LLM says passed but score below threshold - overridden",
			response: `{
				"score": 0.6,
				"feedback": "LLM thinks ok, but below threshold",
				"passed": true
			}`,
			wantPass: false,
			minScore: 0.0,
		},
		{
			name: "score clamped above 1.0",
			response: `{
				"score": 1.5,
				"feedback": "over-enthusiastic",
				"passed": true
			}`,
			wantPass: true,
			minScore: 0.7,
		},
		{
			name: "score clamped below 0.0",
			response: `{
				"score": -0.5,
				"feedback": "negative score",
				"passed": false
			}`,
			wantPass: false,
			minScore: 0.0,
		},
		{
			name:     "invalid JSON",
			response: "not json",
			wantErr:  true,
		},
		{
			name:    "LLM error",
			llmErr:  fmt.Errorf("network error"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &evaluatorFakeLLM{response: tt.response, err: tt.llmErr}
			evaluator := NewPlanEvaluator(mock, "test-model")

			eval, err := evaluator.Evaluate(context.Background(), "build feature", testRoles, testGraph)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if eval.Passed != tt.wantPass {
				t.Errorf("Passed = %v, want %v (score=%.2f)", eval.Passed, tt.wantPass, eval.Score)
			}

			if eval.Score < tt.minScore {
				t.Errorf("Score = %.2f, want >= %.2f", eval.Score, tt.minScore)
			}

			// Score should be clamped to [0, 1].
			if eval.Score < 0 || eval.Score > 1 {
				t.Errorf("Score = %.2f, should be clamped to [0, 1]", eval.Score)
			}
		})
	}
}

func TestFormatGraphForPrompt(t *testing.T) {
	result := formatGraphForPrompt(testGraph)
	if result == "(Empty plan)" {
		t.Error("expected non-empty graph formatting")
	}

	empty := formatGraphForPrompt(nil)
	if empty != "(Empty plan)" {
		t.Errorf("got %q for nil graph, want '(Empty plan)'", empty)
	}
}
