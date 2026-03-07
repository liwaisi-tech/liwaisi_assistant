package filemanagement

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
)

func mustNewToolSet(t *testing.T, root string) *ToolSet {
	t.Helper()
	ts, err := NewToolSet(root)
	if err != nil {
		t.Fatalf("NewToolSet(%q): %v", root, err)
	}
	return ts
}

func TestFileManagementToolSet_Register(t *testing.T) {
	root := t.TempDir()
	reg := tool.NewRegistry()
	ts := mustNewToolSet(t, root)
	ts.Register(reg)

	defs := reg.Definitions()
	if len(defs) != 11 {
		t.Fatalf("expected 11 tool definitions, got %d", len(defs))
	}

	want := map[string]bool{
		"read_file":        false,
		"write_file":       false,
		"create_directory": false,
		"list_directory":   false,
		"tree":             false,
		"move":             false,
		"rename":           false,
		"delete":           false,
		"read_files":       false,
		"find":             false,
		"grep":             false,
	}

	for _, d := range defs {
		name := d.Function.Name
		if _, ok := want[name]; !ok {
			t.Errorf("unexpected tool registered: %s", name)
			continue
		}
		want[name] = true
	}

	for name, found := range want {
		if !found {
			t.Errorf("missing expected tool: %s", name)
		}
	}
}

func TestFileManagementToolSet_ToolsExecutable(t *testing.T) {
	root := t.TempDir()
	reg := tool.NewRegistry()
	mustNewToolSet(t, root).Register(reg)

	ctx := context.Background()

	t.Run("list_directory on empty workspace", func(t *testing.T) {
		result, err := reg.Execute(ctx, "list_directory", json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("Execute list_directory: %v", err)
		}
		var r listDirectoryResult
		mustUnmarshal(t, result, &r)
		if r.Total != 0 {
			t.Errorf("total = %d, want 0", r.Total)
		}
	})

	t.Run("write then read file", func(t *testing.T) {
		_, err := reg.Execute(ctx, "write_file", json.RawMessage(`{"path":"test.txt","content":"integration"}`))
		if err != nil {
			t.Fatalf("Execute write_file: %v", err)
		}

		result, err := reg.Execute(ctx, "read_file", json.RawMessage(`{"path":"test.txt"}`))
		if err != nil {
			t.Fatalf("Execute read_file: %v", err)
		}

		var r readFileResult
		mustUnmarshal(t, result, &r)
		if r.Content != "integration" {
			t.Errorf("content = %q, want %q", r.Content, "integration")
		}
	})
}

// Compile-time check that ToolSet implements tool.Set.
var _ tool.Set = (*ToolSet)(nil)
