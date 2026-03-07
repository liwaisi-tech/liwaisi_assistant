package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func TestEvaluateTeamTool_Success(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     string
		teamEval valueobject.TeamEvaluation
		wantTeam bool
		wantErr  bool
	}{
		{
			name: "team needed",
			args: `{"task": "Review security and performance of auth module"}`,
			teamEval: valueobject.TeamEvaluation{
				NeedsTeam:  true,
				Confidence: 0.9,
				Roles: []valueobject.RoleSpec{
					{Name: "security-reviewer", Perspective: "security"},
				},
				Reasoning: "multi-perspective review",
			},
			wantTeam: true,
		},
		{
			name: "no team needed",
			args: `{"task": "Fix the typo in readme"}`,
			teamEval: valueobject.TeamEvaluation{
				NeedsTeam:  false,
				Confidence: 1.0,
				Reasoning:  "simple task",
			},
			wantTeam: false,
		},
		{
			name:    "missing task",
			args:    `{}`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			args:    `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := &stubSubAgentService{
				teamEval: tt.teamEval,
			}
			registry := tool.NewRegistry()
			RegisterTeamTools(registry, svc)

			result, err := registry.Execute(context.Background(), "evaluate_team", json.RawMessage(tt.args))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var eval valueobject.TeamEvaluation
			if err := json.Unmarshal([]byte(result), &eval); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if eval.NeedsTeam != tt.wantTeam {
				t.Errorf("NeedsTeam = %v, want %v", eval.NeedsTeam, tt.wantTeam)
			}
		})
	}
}

func TestEvaluateTeamTool_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &stubSubAgentService{
		teamErr: fmt.Errorf("detector failed"),
	}
	registry := tool.NewRegistry()
	RegisterTeamTools(registry, svc)

	_, err := registry.Execute(context.Background(), "evaluate_team", json.RawMessage(`{"task": "test"}`))
	if err == nil {
		t.Fatal("expected error from service")
	}
}

func TestCreateSubagentTool_Success(t *testing.T) {
	t.Parallel()

	svc := &stubSubAgentService{}
	registry := tool.NewRegistry()
	RegisterTeamTools(registry, svc)

	args := `{
		"name": "test-agent",
		"description": "A test agent",
		"instruction": "You are a test agent.",
		"model_tier": "fast",
		"allowed_tools": ["read_file", "grep"],
		"max_turns": 5,
		"timeout": "3m"
	}`

	result, err := registry.Execute(context.Background(), "create_subagent", json.RawMessage(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "created") {
		t.Errorf("result should confirm creation: %s", result)
	}
	if !strings.Contains(result, "test-agent") {
		t.Errorf("result should contain agent name: %s", result)
	}

	if len(svc.created) != 1 {
		t.Fatalf("expected 1 created spec, got %d", len(svc.created))
	}
	spec := svc.created[0]
	if spec.Name != "test-agent" {
		t.Errorf("Name = %q, want %q", spec.Name, "test-agent")
	}
	if spec.ModelTier != valueobject.ModelTierFast {
		t.Errorf("ModelTier = %q, want fast", spec.ModelTier)
	}
	if len(spec.AllowedTools) != 2 {
		t.Errorf("AllowedTools = %v, want 2 items", spec.AllowedTools)
	}
}

func TestCreateSubagentTool_MinimalArgs(t *testing.T) {
	t.Parallel()

	svc := &stubSubAgentService{}
	registry := tool.NewRegistry()
	RegisterTeamTools(registry, svc)

	args := `{"name": "minimal-agent", "description": "Minimal", "instruction": "Do stuff."}`
	_, err := registry.Execute(context.Background(), "create_subagent", json.RawMessage(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(svc.created) != 1 {
		t.Fatalf("expected 1 created spec, got %d", len(svc.created))
	}
	if svc.created[0].ModelTier != "" {
		t.Errorf("ModelTier should be empty for minimal, got %q", svc.created[0].ModelTier)
	}
}

func TestCreateSubagentTool_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args string
	}{
		{name: "missing name", args: `{"description": "x", "instruction": "y"}`},
		{name: "missing description", args: `{"name": "test", "instruction": "y"}`},
		{name: "missing instruction", args: `{"name": "test", "description": "x"}`},
		{name: "invalid json", args: `{not valid`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc := &stubSubAgentService{}
			registry := tool.NewRegistry()
			RegisterTeamTools(registry, svc)

			_, err := registry.Execute(context.Background(), "create_subagent", json.RawMessage(tt.args))
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestCreateSubagentTool_InvalidTimeout(t *testing.T) {
	t.Parallel()

	svc := &stubSubAgentService{}
	registry := tool.NewRegistry()
	RegisterTeamTools(registry, svc)

	args := `{"name": "bad-timeout", "description": "x", "instruction": "y", "timeout": "not-a-duration"}`
	_, err := registry.Execute(context.Background(), "create_subagent", json.RawMessage(args))
	if err == nil {
		t.Fatal("expected error for invalid timeout")
	}
}

func TestCreateSubagentTool_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &stubSubAgentService{
		createErr: fmt.Errorf("persistence failed"),
	}
	registry := tool.NewRegistry()
	RegisterTeamTools(registry, svc)

	args := `{"name": "fail-agent", "description": "Will fail", "instruction": "Test."}`
	_, err := registry.Execute(context.Background(), "create_subagent", json.RawMessage(args))
	if err == nil {
		t.Fatal("expected error from service")
	}
}

// Verify stubSubAgentService implements the full interface.
var _ input.SubAgentService = (*stubSubAgentService)(nil)

// Ensure suppressed import usage.
var _ = entity.SubAgentSpec{}
