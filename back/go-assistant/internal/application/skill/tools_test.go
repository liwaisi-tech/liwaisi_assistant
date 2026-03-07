package skill

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
)

func newTestRegistry(t *testing.T) (*tool.Registry, *Registry) {
	t.Helper()
	fsys := fstest.MapFS{
		"test-skill/SKILL.md": &fstest.MapFile{
			Data: []byte(`---
name: test-skill
description: A test skill for tool tests.
metadata:
  author: tester
---
# Test Instructions

Step 1: Do something.
Step 2: Do something else.
`),
		},
		"test-skill/references/guide.md": &fstest.MapFile{
			Data: []byte("# Guide\n\nDetailed guidance.\n"),
		},
	}

	skillReg := NewRegistry()
	if err := skillReg.LoadEmbedded(fsys, "."); err != nil {
		t.Fatal(err)
	}

	toolReg := tool.NewRegistry()
	RegisterSkillTools(toolReg, skillReg)
	return toolReg, skillReg
}

func TestActivateSkill(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		args     string
		wantErr  string
		wantBody string
	}{
		{
			name:     "valid activation",
			args:     `{"name": "test-skill"}`,
			wantBody: "# Test Instructions",
		},
		{
			name:    "nonexistent skill",
			args:    `{"name": "no-such-skill"}`,
			wantErr: "not found",
		},
		{
			name:    "invalid json",
			args:    `{bad json}`,
			wantErr: "parsing activate_skill arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			toolReg, _ := newTestRegistry(t)
			ctx := context.Background()

			result, err := toolReg.Execute(ctx, "activate_skill", json.RawMessage(tt.args))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var parsed map[string]string
			if err := json.Unmarshal([]byte(result), &parsed); err != nil {
				t.Fatalf("result is not valid JSON: %v", err)
			}
			if parsed["status"] != "activated" {
				t.Errorf("status = %q, want %q", parsed["status"], "activated")
			}
			if !strings.Contains(parsed["instructions"], tt.wantBody) {
				t.Errorf("instructions missing %q", tt.wantBody)
			}
		})
	}
}

func TestReadSkillResource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        string
		wantErr     string
		wantContent string
	}{
		{
			name:        "valid resource",
			args:        `{"skill_name": "test-skill", "path": "references/guide.md"}`,
			wantContent: "Detailed guidance.",
		},
		{
			name:    "nonexistent skill",
			args:    `{"skill_name": "no-skill", "path": "foo.md"}`,
			wantErr: "not found",
		},
		{
			name:    "path traversal",
			args:    `{"skill_name": "test-skill", "path": "../../../etc/passwd"}`,
			wantErr: "escapes",
		},
		{
			name:    "nonexistent resource",
			args:    `{"skill_name": "test-skill", "path": "references/missing.md"}`,
			wantErr: "not found",
		},
		{
			name:    "invalid json",
			args:    `not json`,
			wantErr: "parsing read_skill_resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			toolReg, _ := newTestRegistry(t)
			ctx := context.Background()

			result, err := toolReg.Execute(ctx, "read_skill_resource", json.RawMessage(tt.args))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var parsed map[string]string
			if err := json.Unmarshal([]byte(result), &parsed); err != nil {
				t.Fatalf("result is not valid JSON: %v", err)
			}
			if !strings.Contains(parsed["content"], tt.wantContent) {
				t.Errorf("content missing %q", tt.wantContent)
			}
		})
	}
}

func TestListSkills(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		wantCount int
	}{
		{
			name:      "lists all skills",
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			toolReg, _ := newTestRegistry(t)
			ctx := context.Background()

			result, err := toolReg.Execute(ctx, "list_skills", json.RawMessage(`{}`))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var parsed listSkillsResult
			if err := json.Unmarshal([]byte(result), &parsed); err != nil {
				t.Fatalf("result is not valid JSON: %v", err)
			}
			if parsed.Count != tt.wantCount {
				t.Errorf("count = %d, want %d", parsed.Count, tt.wantCount)
			}
			if len(parsed.Skills) != tt.wantCount {
				t.Errorf("skills length = %d, want %d", len(parsed.Skills), tt.wantCount)
			}
		})
	}
}

func TestToolDefinitions_Registered(t *testing.T) {
	t.Parallel()

	toolReg, _ := newTestRegistry(t)
	defs := toolReg.Definitions()

	expectedTools := map[string]bool{
		"activate_skill":      false,
		"read_skill_resource": false,
		"list_skills":         false,
	}

	for _, d := range defs {
		if _, ok := expectedTools[d.Function.Name]; ok {
			expectedTools[d.Function.Name] = true
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func TestListSkills_EmptyRegistry(t *testing.T) {
	t.Parallel()

	skillReg := NewRegistry()
	toolReg := tool.NewRegistry()
	RegisterSkillTools(toolReg, skillReg)

	ctx := context.Background()
	result, err := toolReg.Execute(ctx, "list_skills", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed listSkillsResult
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result is not valid JSON: %v", err)
	}
	if parsed.Count != 0 {
		t.Errorf("count = %d, want 0", parsed.Count)
	}
}
