package skill_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/skill"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
)

func setupTestSkillRegistry(t *testing.T) *skill.Registry {
	t.Helper()
	dir := t.TempDir()

	// Create two test skills.
	for _, s := range []struct{ name, desc string }{
		{"test-skill", "A test skill for unit tests"},
		{"another-skill", "Another test skill"},
	} {
		skillDir := filepath.Join(dir, s.name)
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatal(err)
		}
		content := "---\nname: " + s.name + "\ndescription: " + s.desc + "\n---\n# " + s.name + "\nBody content.\n"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Add a resource to test-skill.
	refDir := filepath.Join(dir, "test-skill", "references")
	if err := os.MkdirAll(refDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refDir, "guide.md"), []byte("reference content"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := skill.NewRegistry()
	if err := reg.LoadFromDir(dir); err != nil {
		t.Fatal(err)
	}
	return reg
}

func TestFindSkills_List(t *testing.T) {
	t.Parallel()
	skills := setupTestSkillRegistry(t)
	reg := tool.NewRegistry()
	skill.RegisterFindSkills(reg, skills)

	args, _ := json.Marshal(map[string]string{"action": "list"})
	result, err := reg.Execute(context.Background(), "find_skills", args)
	if err != nil {
		t.Fatalf("Execute(find_skills, list) error = %v", err)
	}

	var listResult struct {
		Skills []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"skills"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal([]byte(result), &listResult); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if listResult.Count != 2 {
		t.Errorf("count = %d, want 2", listResult.Count)
	}
}

func TestFindSkills_Activate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		skill   string
		wantErr bool
	}{
		{name: "valid_skill", skill: "test-skill"},
		{name: "unknown_skill", skill: "nonexistent", wantErr: true},
		{name: "missing_name", skill: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			skills := setupTestSkillRegistry(t)
			reg := tool.NewRegistry()
			skill.RegisterFindSkills(reg, skills)

			args, _ := json.Marshal(map[string]string{
				"action": "activate",
				"name":   tt.skill,
			})
			result, err := reg.Execute(context.Background(), "find_skills", args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			var activateResult struct {
				Skill  string `json:"skill"`
				Status string `json:"status"`
			}
			if err := json.Unmarshal([]byte(result), &activateResult); err != nil {
				t.Fatal(err)
			}
			if activateResult.Status != "activated" {
				t.Errorf("status = %q, want %q", activateResult.Status, "activated")
			}
		})
	}
}

func TestFindSkills_ReadResource(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		skill   string
		path    string
		wantErr bool
	}{
		{name: "valid_resource", skill: "test-skill", path: "references/guide.md"},
		{name: "missing_resource", skill: "test-skill", path: "references/nope.md", wantErr: true},
		{name: "missing_name", skill: "", path: "references/guide.md", wantErr: true},
		{name: "missing_path", skill: "test-skill", path: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			skills := setupTestSkillRegistry(t)
			reg := tool.NewRegistry()
			skill.RegisterFindSkills(reg, skills)

			args, _ := json.Marshal(map[string]string{
				"action": "read_resource",
				"name":   tt.skill,
				"path":   tt.path,
			})
			result, err := reg.Execute(context.Background(), "find_skills", args)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			var readResult struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal([]byte(result), &readResult); err != nil {
				t.Fatal(err)
			}
			if readResult.Content != "reference content" {
				t.Errorf("content = %q, want %q", readResult.Content, "reference content")
			}
		})
	}
}

func TestFindSkills_UnknownAction(t *testing.T) {
	t.Parallel()
	skills := setupTestSkillRegistry(t)
	reg := tool.NewRegistry()
	skill.RegisterFindSkills(reg, skills)

	args, _ := json.Marshal(map[string]string{"action": "delete"})
	_, err := reg.Execute(context.Background(), "find_skills", args)
	if err == nil {
		t.Error("expected error for unknown action")
	}
}
