package planner

import (
	"context"
	"fmt"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// --- LLM mock for team assembler ---

type assemblerFakeLLM struct {
	response string
	err      error
}

func (m *assemblerFakeLLM) Complete(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &output.ChatResponse{Content: m.response}, nil
}

func (m *assemblerFakeLLM) CompleteStream(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestTeamAssembler_Assemble(t *testing.T) {
	tests := []struct {
		name         string
		response     string
		llmErr       error
		wantErr      bool
		minRoles     int
		hasArchitect bool
		hasQA        bool
	}{
		{
			name: "valid response with architect and QA",
			response: `{
				"needs_team": true,
				"confidence": 0.9,
				"roles": [
					{"name": "software-architect", "perspective": "design", "instruction": "do arch"},
					{"name": "qa-engineer", "perspective": "testing", "instruction": "do qa"}
				],
				"reasoning": "needs both"
			}`,
			minRoles:     2,
			hasArchitect: true,
			hasQA:        true,
		},
		{
			name: "response wrapped in code fence",
			response: "```json\n" + `{
				"needs_team": true,
				"confidence": 0.8,
				"roles": [
					{"name": "architect", "perspective": "design", "instruction": "arch"},
					{"name": "qa-engineer", "perspective": "test", "instruction": "qa"}
				],
				"reasoning": "reason"
			}` + "\n```",
			minRoles:     2,
			hasArchitect: true,
			hasQA:        true,
		},
		{
			name: "missing architect - gets added automatically",
			response: `{
				"needs_team": true,
				"confidence": 0.85,
				"roles": [
					{"name": "devops-engineer", "perspective": "infra", "instruction": "deploy"}
				],
				"reasoning": "devops only"
			}`,
			minRoles:     3, // devops + auto-added architect + auto-added QA
			hasArchitect: true,
			hasQA:        true,
		},
		{
			name: "missing QA - gets added automatically",
			response: `{
				"needs_team": true,
				"confidence": 0.85,
				"roles": [
					{"name": "software-architect", "perspective": "design", "instruction": "arch"}
				],
				"reasoning": "arch only"
			}`,
			minRoles:     2, // architect + auto-added QA
			hasArchitect: true,
			hasQA:        true,
		},
		{
			name:     "invalid JSON",
			response: "not valid json at all",
			wantErr:  true,
		},
		{
			name:    "LLM error",
			llmErr:  fmt.Errorf("service unavailable"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &assemblerFakeLLM{response: tt.response, err: tt.llmErr}
			assembler := NewTeamAssembler(mock, "test-model")

			eval, err := assembler.Assemble(context.Background(), "build a complex feature")

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !eval.NeedsTeam {
				t.Error("NeedsTeam should always be true")
			}

			if len(eval.Roles) < tt.minRoles {
				t.Errorf("got %d roles, want at least %d", len(eval.Roles), tt.minRoles)
			}

			if tt.hasArchitect {
				found := false
				for _, r := range eval.Roles {
					if containsCI(r.Name, "architect") {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected architect role")
				}
			}

			if tt.hasQA {
				found := false
				for _, r := range eval.Roles {
					if containsCI(r.Name, "qa") || containsCI(r.Name, "quality") || containsCI(r.Name, "testing") {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected QA role")
				}
			}
		})
	}
}

func TestEnsureMinimumTeam_AlreadyComplete(t *testing.T) {
	eval := valueobject.TeamEvaluation{
		Roles: []valueobject.RoleSpec{
			{Name: "software-architect"},
			{Name: "qa-engineer"},
			{Name: "devops"},
		},
	}
	result := ensureMinimumTeam(eval)
	if len(result.Roles) != 3 {
		t.Errorf("should not add extra roles, got %d want 3", len(result.Roles))
	}
}

func TestStripJSONCodeFence(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no fence", `{"a":1}`, `{"a":1}`},
		{"json fence", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"plain fence", "```\n{\"a\":1}\n```", `{"a":1}`},
		{"whitespace", "  ```json\n{\"a\":1}\n```  ", `{"a":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripJSONCodeFence(tt.in); got != tt.want {
				t.Errorf("stripJSONCodeFence() = %q, want %q", got, tt.want)
			}
		})
	}
}

func containsCI(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && contains(lower(s), lower(substr)))
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
