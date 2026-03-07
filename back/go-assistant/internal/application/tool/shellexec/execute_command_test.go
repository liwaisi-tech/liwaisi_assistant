package shellexec

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool/filemanagement"
)

func mustNewSandbox(t *testing.T, root string) *filemanagement.Sandbox {
	t.Helper()
	sb, err := filemanagement.NewSandbox(root)
	if err != nil {
		t.Fatalf("NewSandbox(%q): %v", root, err)
	}
	return sb
}

func TestExecuteCommand(t *testing.T) {
	tests := []struct {
		name    string
		args    string
		wantErr string
		check   func(t *testing.T, result string)
	}{
		{
			name: "valid echo command",
			args: `{"command": "echo hello"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
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
				if r.Command != "echo hello" {
					t.Errorf("command = %q, want %q", r.Command, "echo hello")
				}
				if r.DurationMs < 0 {
					t.Errorf("duration_ms = %d, want >= 0", r.DurationMs)
				}
			},
		},
		{
			name: "valid command with custom working directory",
			args: `{"command": "pwd", "working_directory": "."}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r executeCommandResult
				if err := json.Unmarshal([]byte(result), &r); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if r.ExitCode != 0 {
					t.Errorf("exit_code = %d, want 0", r.ExitCode)
				}
				if r.WorkingDirectory != "." {
					t.Errorf("working_directory = %q, want %q", r.WorkingDirectory, ".")
				}
			},
		},
		{
			name: "non-zero exit code reported",
			args: `{"command": "exit 7"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r executeCommandResult
				if err := json.Unmarshal([]byte(result), &r); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if r.ExitCode != 7 {
					t.Errorf("exit_code = %d, want 7", r.ExitCode)
				}
			},
		},
		{
			name: "timeout clamped to max",
			args: `{"command": "echo fast", "timeout_seconds": 999}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r executeCommandResult
				if err := json.Unmarshal([]byte(result), &r); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if r.ExitCode != 0 {
					t.Errorf("exit_code = %d, want 0", r.ExitCode)
				}
			},
		},
		{
			name: "default timeout when zero",
			args: `{"command": "echo default", "timeout_seconds": 0}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r executeCommandResult
				if err := json.Unmarshal([]byte(result), &r); err != nil {
					t.Fatalf("unmarshal: %v", err)
				}
				if r.ExitCode != 0 {
					t.Errorf("exit_code = %d, want 0", r.ExitCode)
				}
			},
		},

		// Error cases
		{
			name:    "blocked command rejected",
			args:    `{"command": "sudo apt install curl"}`,
			wantErr: "sudo",
		},
		{
			name:    "empty command rejected",
			args:    `{"command": ""}`,
			wantErr: "empty",
		},
		{
			name:    "sandbox violation on working directory",
			args:    `{"command": "echo test", "working_directory": "../../etc"}`,
			wantErr: "escapes workspace boundary",
		},
		{
			name:    "absolute working directory rejected",
			args:    `{"command": "echo test", "working_directory": "/etc"}`,
			wantErr: "absolute paths are not allowed",
		},
		{
			name:    "invalid JSON args",
			args:    `{invalid}`,
			wantErr: "parsing execute_command arguments",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			reg := tool.NewRegistry()
			sb := mustNewSandbox(t, root)
			policy := NewDefaultPolicy()
			executor := NewCommandExecutor(ExecutorConfig{})

			registerExecuteCommand(reg, sb, policy, executor, nil)

			result, err := reg.Execute(context.Background(), "execute_command", json.RawMessage(tt.args))
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want containing %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, result)
			}
		})
	}
}

type testRedactor struct {
	values map[string]string
}

func (r *testRedactor) Redact(output string) string {
	for secret, placeholder := range r.values {
		output = strings.ReplaceAll(output, secret, placeholder)
	}
	return output
}

func TestExecuteCommand_OutputRedaction(t *testing.T) {
	root := t.TempDir()
	reg := tool.NewRegistry()
	sb := mustNewSandbox(t, root)
	policy := NewDefaultPolicy()
	executor := NewCommandExecutor(ExecutorConfig{})
	redactor := &testRedactor{
		values: map[string]string{
			"super-secret-key-123": "[REDACTED:API_KEY]",
		},
	}

	registerExecuteCommand(reg, sb, policy, executor, redactor)

	args := `{"command": "echo super-secret-key-123"}`
	result, err := reg.Execute(context.Background(), "execute_command", json.RawMessage(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var parsed executeCommandResult
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if strings.Contains(parsed.Stdout, "super-secret-key-123") {
		t.Fatal("SECURITY VIOLATION: redacted secret value found in stdout")
	}
	if !strings.Contains(parsed.Stdout, "[REDACTED:API_KEY]") {
		t.Errorf("expected redaction placeholder in stdout, got %q", parsed.Stdout)
	}
}

func TestExecuteCommand_NilRedactor(t *testing.T) {
	root := t.TempDir()
	reg := tool.NewRegistry()
	sb := mustNewSandbox(t, root)
	policy := NewDefaultPolicy()
	executor := NewCommandExecutor(ExecutorConfig{})

	registerExecuteCommand(reg, sb, policy, executor, nil)

	args := `{"command": "echo hello"}`
	result, err := reg.Execute(context.Background(), "execute_command", json.RawMessage(args))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "hello") {
		t.Errorf("expected 'hello' in output, got %q", result)
	}
}
