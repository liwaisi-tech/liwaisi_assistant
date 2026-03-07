package skill

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

const validSkillMD = `---
name: test-skill
description: A test skill for unit tests.
metadata:
  author: test
---
# Test Skill Instructions

Follow these steps.
`

const validSkillMD2 = `---
name: another-skill
description: Another test skill.
---
# Another Skill

Do something else.
`

func writeSkillDir(t *testing.T, base, name, content string) string {
	t.Helper()
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRegistry_LoadFromDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(t *testing.T, base string)
		wantCount int
		wantNames []string
	}{
		{
			name: "single valid skill",
			setup: func(t *testing.T, base string) {
				writeSkillDir(t, base, "test-skill", validSkillMD)
			},
			wantCount: 1,
			wantNames: []string{"test-skill"},
		},
		{
			name: "multiple valid skills",
			setup: func(t *testing.T, base string) {
				writeSkillDir(t, base, "test-skill", validSkillMD)
				writeSkillDir(t, base, "another-skill", validSkillMD2)
			},
			wantCount: 2,
			wantNames: []string{"another-skill", "test-skill"},
		},
		{
			name:      "empty directory",
			setup:     func(_ *testing.T, _ string) {},
			wantCount: 0,
		},
		{
			name: "mixed valid and invalid",
			setup: func(t *testing.T, base string) {
				writeSkillDir(t, base, "good-skill", validSkillMD)
				writeSkillDir(t, base, "bad-skill", "not a valid skill file")
			},
			wantCount: 1,
			wantNames: []string{"test-skill"},
		},
		{
			name: "non-directory entries are ignored",
			setup: func(t *testing.T, base string) {
				writeSkillDir(t, base, "test-skill", validSkillMD)
				if err := os.WriteFile(filepath.Join(base, "README.md"), []byte("hi"), 0o640); err != nil {
					t.Fatal(err)
				}
			},
			wantCount: 1,
			wantNames: []string{"test-skill"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			base := t.TempDir()
			tt.setup(t, base)

			reg := NewRegistry()
			if err := reg.LoadFromDir(base); err != nil {
				t.Fatalf("LoadFromDir error: %v", err)
			}

			if got := reg.Count(); got != tt.wantCount {
				t.Errorf("Count() = %d, want %d", got, tt.wantCount)
			}

			if tt.wantNames != nil {
				metas := reg.List()
				var names []string
				for _, m := range metas {
					names = append(names, m.Name)
				}
				if len(names) != len(tt.wantNames) {
					t.Fatalf("List() names = %v, want %v", names, tt.wantNames)
				}
				for i, n := range names {
					if n != tt.wantNames[i] {
						t.Errorf("List()[%d].Name = %q, want %q", i, n, tt.wantNames[i])
					}
				}
			}
		})
	}
}

func TestRegistry_LoadFromDir_NonExistent(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	err := reg.LoadFromDir("/nonexistent/path/skills")
	if err != nil {
		t.Fatalf("expected nil for nonexistent dir, got: %v", err)
	}
	if reg.Count() != 0 {
		t.Errorf("Count() = %d, want 0", reg.Count())
	}
}

func TestRegistry_LoadEmbedded(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"skill-creator/SKILL.md": &fstest.MapFile{
			Data: []byte(`---
name: skill-creator
description: Create new skills for the agent.
metadata:
  builtin: "true"
---
# Skill Creator

Step 1: Capture intent.
`),
		},
		"skill-creator/references/format.md": &fstest.MapFile{
			Data: []byte("# SKILL.md Format\n\nUse YAML frontmatter.\n"),
		},
	}

	reg := NewRegistry()
	if err := reg.LoadEmbedded(fsys, "."); err != nil {
		t.Fatalf("LoadEmbedded error: %v", err)
	}

	if !reg.Has("skill-creator") {
		t.Fatal("expected skill-creator to be registered")
	}
	if reg.Count() != 1 {
		t.Errorf("Count() = %d, want 1", reg.Count())
	}
}

func TestRegistry_Activate(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"test-skill/SKILL.md": &fstest.MapFile{
			Data: []byte(validSkillMD),
		},
	}

	reg := NewRegistry()
	if err := reg.LoadEmbedded(fsys, "."); err != nil {
		t.Fatal(err)
	}

	body, err := reg.Activate("test-skill")
	if err != nil {
		t.Fatalf("Activate error: %v", err)
	}
	if !strings.Contains(body, "# Test Skill Instructions") {
		t.Errorf("body does not contain expected content: %q", body)
	}
}

func TestRegistry_Activate_NotFound(t *testing.T) {
	t.Parallel()
	reg := NewRegistry()
	_, err := reg.Activate("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent skill")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want 'not found'", err)
	}
}

func TestRegistry_ReadResource_Filesystem(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	skillDir := writeSkillDir(t, base, "test-skill", validSkillMD)

	refDir := filepath.Join(skillDir, "references")
	if err := os.MkdirAll(refDir, 0o750); err != nil {
		t.Fatal(err)
	}
	refContent := "# Checklist\n\n- Item 1\n- Item 2\n"
	if err := os.WriteFile(filepath.Join(refDir, "checklist.md"), []byte(refContent), 0o640); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry()
	if err := reg.LoadFromDir(base); err != nil {
		t.Fatal(err)
	}

	content, err := reg.ReadResource("test-skill", "references/checklist.md")
	if err != nil {
		t.Fatalf("ReadResource error: %v", err)
	}
	if content != refContent {
		t.Errorf("content = %q, want %q", content, refContent)
	}
}

func TestRegistry_ReadResource_PathTraversal(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	writeSkillDir(t, base, "test-skill", validSkillMD)

	reg := NewRegistry()
	if err := reg.LoadFromDir(base); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
	}{
		{"parent directory", "../etc/passwd"},
		{"double parent", "../../etc/shadow"},
		{"hidden traversal", "references/../../etc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := reg.ReadResource("test-skill", tt.path)
			if err == nil {
				t.Fatal("expected error for path traversal")
			}
			if !strings.Contains(err.Error(), "escapes") {
				t.Errorf("error = %q, want 'escapes'", err)
			}
		})
	}
}

func TestRegistry_ReadResource_SymlinkTraversal(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	skillDir := writeSkillDir(t, base, "test-skill", validSkillMD)

	secretDir := t.TempDir()
	secretPath := filepath.Join(secretDir, "secret.txt")
	if err := os.WriteFile(secretPath, []byte("top secret"), 0o640); err != nil {
		t.Fatal(err)
	}

	refsDir := filepath.Join(skillDir, "references")
	if err := os.MkdirAll(refsDir, 0o750); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(refsDir, "sneaky.txt")
	if err := os.Symlink(secretPath, symlink); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	reg := NewRegistry()
	if err := reg.LoadFromDir(base); err != nil {
		t.Fatal(err)
	}

	_, err := reg.ReadResource("test-skill", "references/sneaky.txt")
	if err == nil {
		t.Fatal("expected error for symlink escaping skill directory")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error = %q, want 'escapes'", err)
	}
}

func TestRegistry_ReadResource_Embedded(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte(`---
name: my-skill
description: Skill with resources.
---
# Instructions
`),
		},
		"my-skill/references/guide.md": &fstest.MapFile{
			Data: []byte("# Guide\n\nStep-by-step.\n"),
		},
	}

	reg := NewRegistry()
	if err := reg.LoadEmbedded(fsys, "."); err != nil {
		t.Fatal(err)
	}

	content, err := reg.ReadResource("my-skill", "references/guide.md")
	if err != nil {
		t.Fatalf("ReadResource error: %v", err)
	}
	if !strings.Contains(content, "Step-by-step") {
		t.Errorf("content = %q, want 'Step-by-step'", content)
	}
}

func TestRegistry_ReadResource_Embedded_PathTraversal(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"my-skill/SKILL.md": &fstest.MapFile{
			Data: []byte(`---
name: my-skill
description: Skill with resources.
---
`),
		},
	}

	reg := NewRegistry()
	if err := reg.LoadEmbedded(fsys, "."); err != nil {
		t.Fatal(err)
	}

	_, err := reg.ReadResource("my-skill", "../other/secret.txt")
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error = %q, want 'escapes'", err)
	}
}

func TestRegistry_Refresh(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	writeSkillDir(t, base, "skill-one", validSkillMD)

	reg := NewRegistry()
	if err := reg.LoadFromDir(base); err != nil {
		t.Fatal(err)
	}
	if reg.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", reg.Count())
	}

	// Add a new skill on disk.
	writeSkillDir(t, base, "another-skill", validSkillMD2)

	if err := reg.Refresh(); err != nil {
		t.Fatalf("Refresh error: %v", err)
	}
	if reg.Count() != 2 {
		t.Errorf("after add: Count() = %d, want 2", reg.Count())
	}

	// Remove the first skill from disk.
	if err := os.RemoveAll(filepath.Join(base, "skill-one")); err != nil {
		t.Fatal(err)
	}

	if err := reg.Refresh(); err != nil {
		t.Fatalf("Refresh error: %v", err)
	}
	if reg.Count() != 1 {
		t.Errorf("after remove: Count() = %d, want 1", reg.Count())
	}
	if reg.Has("test-skill") {
		t.Error("deleted skill should not be present after refresh")
	}
	if !reg.Has("another-skill") {
		t.Error("remaining skill should still be present")
	}
}

func TestRegistry_SystemPromptFragment(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()

	// Empty registry returns empty string.
	if got := reg.SystemPromptFragment(); got != "" {
		t.Errorf("empty registry: SystemPromptFragment() = %q, want empty", got)
	}

	fsys := fstest.MapFS{
		"test-skill/SKILL.md": &fstest.MapFile{Data: []byte(validSkillMD)},
	}
	if err := reg.LoadEmbedded(fsys, "."); err != nil {
		t.Fatal(err)
	}

	fragment := reg.SystemPromptFragment()
	if !strings.Contains(fragment, "<available_skills>") {
		t.Error("fragment should contain <available_skills> tag")
	}
	if !strings.Contains(fragment, "</available_skills>") {
		t.Error("fragment should contain </available_skills> tag")
	}
	if !strings.Contains(fragment, `name="test-skill"`) {
		t.Error("fragment should contain skill name attribute")
	}
	if !strings.Contains(fragment, "A test skill for unit tests.") {
		t.Error("fragment should contain skill description")
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	fsys := fstest.MapFS{
		"skill-a/SKILL.md": &fstest.MapFile{
			Data: []byte(`---
name: skill-a
description: Concurrent test skill A.
---
Body A.
`),
		},
		"skill-b/SKILL.md": &fstest.MapFile{
			Data: []byte(`---
name: skill-b
description: Concurrent test skill B.
---
Body B.
`),
		},
	}

	reg := NewRegistry()
	if err := reg.LoadEmbedded(fsys, "."); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(4)
		go func() {
			defer wg.Done()
			reg.List()
		}()
		go func() {
			defer wg.Done()
			reg.Has("skill-a")
		}()
		go func() {
			defer wg.Done()
			_, _ = reg.Activate("skill-a")
		}()
		go func() {
			defer wg.Done()
			reg.SystemPromptFragment()
		}()
	}
	wg.Wait()
}
