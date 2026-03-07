package subagent

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func writeSubAgentFile(t *testing.T, dir, name, content string) {
	t.Helper()
	agentDir := filepath.Join(dir, name)
	if err := os.MkdirAll(agentDir, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", agentDir, err)
	}
	path := filepath.Join(agentDir, subagentFileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestSubAgentRegistry_LoadFromDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(dir string)
		wantCount int
		wantNames []string
	}{
		{
			name: "loads multiple subagents",
			setup: func(dir string) {
				writeSubAgentFile(t, dir, "agent-alpha", `---
name: agent-alpha
description: Alpha agent.
---
You are agent alpha.
`)
				writeSubAgentFile(t, dir, "agent-beta", `---
name: agent-beta
description: Beta agent.
model_tier: fast
---
You are agent beta.
`)
			},
			wantCount: 2,
			wantNames: []string{"agent-alpha", "agent-beta"},
		},
		{
			name: "skips invalid subagents",
			setup: func(dir string) {
				writeSubAgentFile(t, dir, "valid-agent", `---
name: valid-agent
description: Valid agent.
---
You are valid.
`)
				writeSubAgentFile(t, dir, "bad-agent", `invalid content`)
			},
			wantCount: 1,
			wantNames: []string{"valid-agent"},
		},
		{
			name:      "empty directory",
			setup:     func(_ string) {},
			wantCount: 0,
		},
		{
			name: "skips non-directory entries",
			setup: func(dir string) {
				writeSubAgentFile(t, dir, "real-agent", `---
name: real-agent
description: Real agent.
---
You are real.
`)
				if err := os.WriteFile(filepath.Join(dir, "not-a-dir.txt"), []byte("nope"), 0o644); err != nil {
					t.Fatalf("write file: %v", err)
				}
			},
			wantCount: 1,
			wantNames: []string{"real-agent"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			tt.setup(dir)

			reg := NewRegistry()
			if err := reg.LoadFromDir(dir); err != nil {
				t.Fatalf("LoadFromDir: %v", err)
			}

			if got := reg.Count(); got != tt.wantCount {
				t.Errorf("Count() = %d, want %d", got, tt.wantCount)
			}

			for _, name := range tt.wantNames {
				if !reg.Has(name) {
					t.Errorf("Has(%q) = false, want true", name)
				}
			}
		})
	}
}

func TestSubAgentRegistry_LoadFromDir_NonExistent(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	err := reg.LoadFromDir("/nonexistent/path/subagents")
	if err != nil {
		t.Fatalf("expected nil error for nonexistent dir, got: %v", err)
	}
	if reg.Count() != 0 {
		t.Errorf("Count() = %d, want 0", reg.Count())
	}
}

func TestSubAgentRegistry_Lookup(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSubAgentFile(t, dir, "lookup-agent", `---
name: lookup-agent
description: Agent for lookup test.
model_tier: balanced
---
You are the lookup agent.
`)

	reg := NewRegistry()
	if err := reg.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	tests := []struct {
		name     string
		lookup   string
		wantOK   bool
		wantTier valueobject.ModelTier
	}{
		{
			name:     "found",
			lookup:   "lookup-agent",
			wantOK:   true,
			wantTier: valueobject.ModelTierBalanced,
		},
		{
			name:   "not found",
			lookup: "nonexistent",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			spec, ok := reg.Lookup(tt.lookup)
			if ok != tt.wantOK {
				t.Fatalf("Lookup(%q) ok = %v, want %v", tt.lookup, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if spec.ModelTier != tt.wantTier {
				t.Errorf("ModelTier = %q, want %q", spec.ModelTier, tt.wantTier)
			}
		})
	}
}

func TestSubAgentRegistry_List(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSubAgentFile(t, dir, "charlie", `---
name: charlie
description: Agent C.
---
Instruction C.
`)
	writeSubAgentFile(t, dir, "alpha", `---
name: alpha
description: Agent A.
---
Instruction A.
`)
	writeSubAgentFile(t, dir, "bravo", `---
name: bravo
description: Agent B.
---
Instruction B.
`)

	reg := NewRegistry()
	if err := reg.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	specs := reg.List()
	if len(specs) != 3 {
		t.Fatalf("List() returned %d specs, want 3", len(specs))
	}

	want := []string{"alpha", "bravo", "charlie"}
	for i, s := range specs {
		if s.Name != want[i] {
			t.Errorf("List()[%d].Name = %q, want %q", i, s.Name, want[i])
		}
	}
}

func TestSubAgentRegistry_Save(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	reg := NewRegistry()

	spec := &entity.SubAgentSpec{
		Name:         "saved-agent",
		Description:  "A saved agent.",
		Instruction:  "You are a saved agent.",
		ModelTier:    valueobject.ModelTierCapable,
		AllowedTools: []string{"read_file"},
		MaxTurns:     7,
	}

	if err := reg.Save(spec, dir); err != nil {
		t.Fatalf("Save: %v", err)
	}

	filePath := filepath.Join(dir, "saved-agent", subagentFileName)
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("saved file not found: %v", err)
	}

	if !reg.Has("saved-agent") {
		t.Error("Has(saved-agent) = false after Save")
	}

	lookedUp, ok := reg.Lookup("saved-agent")
	if !ok {
		t.Fatal("Lookup(saved-agent) = false after Save")
	}
	if lookedUp.ModelTier != valueobject.ModelTierCapable {
		t.Errorf("ModelTier = %q, want %q", lookedUp.ModelTier, valueobject.ModelTierCapable)
	}
}

func TestSubAgentRegistry_Save_NilSpec(t *testing.T) {
	t.Parallel()

	reg := NewRegistry()
	err := reg.Save(nil, t.TempDir())
	if err == nil {
		t.Fatal("expected error for nil spec")
	}
}

func TestSubAgentRegistry_Refresh(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSubAgentFile(t, dir, "initial-agent", `---
name: initial-agent
description: Initial agent.
---
Initial instruction.
`)

	reg := NewRegistry()
	if err := reg.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if reg.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", reg.Count())
	}

	writeSubAgentFile(t, dir, "new-agent", `---
name: new-agent
description: New agent added after initial load.
---
New instruction.
`)

	if err := reg.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if reg.Count() != 2 {
		t.Errorf("Count() = %d after refresh, want 2", reg.Count())
	}
	if !reg.Has("new-agent") {
		t.Error("Has(new-agent) = false after refresh")
	}
}

func TestSubAgentRegistry_Refresh_RemovesDeleted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeSubAgentFile(t, dir, "removable", `---
name: removable
description: Will be removed.
---
Temporary instruction.
`)

	reg := NewRegistry()
	if err := reg.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}
	if !reg.Has("removable") {
		t.Fatal("expected removable to exist")
	}

	if err := os.RemoveAll(filepath.Join(dir, "removable")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if err := reg.Refresh(); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if reg.Has("removable") {
		t.Error("Has(removable) = true after delete + refresh")
	}
}

func TestSubAgentRegistry_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		name := "agent-" + string(rune('a'+i))
		writeSubAgentFile(t, dir, name, `---
name: `+name+`
description: Concurrent test agent.
---
You are `+name+`.
`)
	}

	reg := NewRegistry()
	if err := reg.LoadFromDir(dir); err != nil {
		t.Fatalf("LoadFromDir: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			switch idx % 4 {
			case 0:
				reg.List()
			case 1:
				reg.Lookup("agent-a")
			case 2:
				reg.Has("agent-b")
			case 3:
				reg.Count()
			}
		}(i)
	}
	wg.Wait()
}

func TestSubAgentRegistry_SystemPromptFragment(t *testing.T) {
	t.Parallel()

	t.Run("empty registry", func(t *testing.T) {
		t.Parallel()
		reg := NewRegistry()
		if got := reg.SystemPromptFragment(); got != "" {
			t.Errorf("SystemPromptFragment() = %q, want empty", got)
		}
	})

	t.Run("with subagents", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeSubAgentFile(t, dir, "test-agent", `---
name: test-agent
description: A test subagent.
---
Instruction.
`)

		reg := NewRegistry()
		if err := reg.LoadFromDir(dir); err != nil {
			t.Fatalf("LoadFromDir: %v", err)
		}

		fragment := reg.SystemPromptFragment()
		if fragment == "" {
			t.Fatal("expected non-empty fragment")
		}
		if !strings.Contains(fragment, "test-agent") {
			t.Error("fragment should contain agent name")
		}
		if !strings.Contains(fragment, "<available_subagents>") {
			t.Error("fragment should contain XML wrapper")
		}
	})
}
