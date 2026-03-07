package shellexec

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
)

func mustNewToolSet(t *testing.T, root string, opts ...ToolSetOption) *ToolSet {
	t.Helper()
	ts, err := NewToolSet(root, opts...)
	if err != nil {
		t.Fatalf("NewToolSet(%q): %v", root, err)
	}
	return ts
}

func TestShellExecToolSet_Register(t *testing.T) {
	root := t.TempDir()
	reg := tool.NewRegistry()
	ts := mustNewToolSet(t, root)
	ts.Register(reg)

	defs := reg.Definitions()
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool definition, got %d", len(defs))
	}

	def := defs[0]
	if def.Function.Name != "execute_command" {
		t.Errorf("tool name = %q, want %q", def.Function.Name, "execute_command")
	}
	if def.Type != "function" {
		t.Errorf("tool type = %q, want %q", def.Type, "function")
	}
	if def.Function.Description == "" {
		t.Error("tool description is empty")
	}
	if len(def.Function.Parameters) == 0 {
		t.Error("tool parameters is empty")
	}
}

func TestShellExecToolSet_ToolsExecutable(t *testing.T) {
	root := t.TempDir()
	reg := tool.NewRegistry()
	mustNewToolSet(t, root).Register(reg)

	ctx := context.Background()

	t.Run("execute echo command", func(t *testing.T) {
		result, err := reg.Execute(ctx, "execute_command", json.RawMessage(`{"command": "echo hello"}`))
		if err != nil {
			t.Fatalf("Execute execute_command: %v", err)
		}
		var r executeCommandResult
		if err := json.Unmarshal([]byte(result), &r); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if r.ExitCode != 0 {
			t.Errorf("exit_code = %d, want 0", r.ExitCode)
		}
		if got := strings.TrimSpace(r.Stdout); got != "hello" {
			t.Errorf("stdout = %q, want %q", got, "hello")
		}
	})
}

func TestShellExecToolSet_WithPolicy(t *testing.T) {
	root := t.TempDir()
	customPolicy := NewDefaultPolicy(WithBlockedCommands("npm"))
	reg := tool.NewRegistry()
	mustNewToolSet(t, root, WithPolicy(customPolicy)).Register(reg)

	_, err := reg.Execute(context.Background(), "execute_command", json.RawMessage(`{"command": "npm install"}`))
	if err == nil {
		t.Fatal("expected error for blocked npm command, got nil")
	}
	if !strings.Contains(err.Error(), "npm") {
		t.Errorf("error = %q, want containing %q", err.Error(), "npm")
	}
}

func TestShellExecToolSet_WithExecutorConfig(t *testing.T) {
	root := t.TempDir()
	reg := tool.NewRegistry()
	mustNewToolSet(t, root, WithExecutorConfig(ExecutorConfig{
		DefaultTimeout: 5 * time.Second,
		MaxTimeout:     10 * time.Second,
	})).Register(reg)

	result, err := reg.Execute(context.Background(), "execute_command", json.RawMessage(`{"command": "echo configured"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var r executeCommandResult
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if r.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0", r.ExitCode)
	}
}

func TestShellExecToolSet_InvalidRoot(t *testing.T) {
	_, err := NewToolSet("relative/path")
	if err == nil {
		t.Fatal("expected error for relative path, got nil")
	}
	if !strings.Contains(err.Error(), "absolute path") {
		t.Errorf("error = %q, want containing %q", err.Error(), "absolute path")
	}
}

func TestShellExecToolSet_NilPolicy(t *testing.T) {
	root := t.TempDir()
	reg := tool.NewRegistry()
	ts := mustNewToolSet(t, root, WithPolicy(nil))
	ts.Register(reg)

	result, err := reg.Execute(context.Background(), "execute_command", json.RawMessage(`{"command": "echo works"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var r executeCommandResult
	if err := json.Unmarshal([]byte(result), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := strings.TrimSpace(r.Stdout); got != "works" {
		t.Errorf("stdout = %q, want %q", got, "works")
	}
}

// Compile-time check that ToolSet implements tool.Set.
var _ tool.Set = (*ToolSet)(nil)
