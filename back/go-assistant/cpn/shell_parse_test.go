package cpn

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestParseShellInvocation_SpecTable covers every row of
// spec-process-bugfix-tool-hitl-single-gate.md §4.2.
func TestParseShellInvocation_SpecTable(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		args    any
		wantCmd string
		wantOK  bool
	}{
		{
			name:    "bash_exec dash-c unwraps inner script",
			tool:    "bash_exec",
			args:    map[string]any{"command": "bash", "args": []string{"-c", "uname -a && whoami"}},
			wantCmd: "uname -a && whoami",
			wantOK:  true,
		},
		{
			name:    "bash_exec dash-lc unwraps inner script",
			tool:    "bash_exec",
			args:    map[string]any{"command": "bash", "args": []string{"-lc", "ls -F"}},
			wantCmd: "ls -F",
			wantOK:  true,
		},
		{
			name:    "bash_exec plain exec joins command and args",
			tool:    "bash_exec",
			args:    map[string]any{"command": "ls", "args": []string{"-la", "/tmp"}},
			wantCmd: "ls -la /tmp",
			wantOK:  true,
		},
		{
			name:    "file_write renders write arrow with path",
			tool:    "file_write",
			args:    map[string]any{"path": "/x.txt", "content": "..."},
			wantCmd: "write → /x.txt",
			wantOK:  true,
		},
		{
			name:    "file_read renders read arrow with path",
			tool:    "file_read",
			args:    map[string]any{"path": "/x.txt"},
			wantCmd: "read ← /x.txt",
			wantOK:  true,
		},
		{
			name:    "unknown tool falls back to JSON and ok=false",
			tool:    "mystery_tool",
			args:    map[string]any{"foo": "bar"},
			wantCmd: `{"foo":"bar"}`,
			wantOK:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotCmd, gotOK := ParseShellInvocation(tc.tool, tc.args)
			if gotCmd != tc.wantCmd {
				t.Errorf("command = %q, want %q", gotCmd, tc.wantCmd)
			}
			if gotOK != tc.wantOK {
				t.Errorf("ok = %v, want %v", gotOK, tc.wantOK)
			}
		})
	}
}

func TestParseShellInvocation_RawMessageInput(t *testing.T) {
	raw := json.RawMessage(`{"command":"bash","args":["-c","echo hi"]}`)
	cmd, ok := ParseShellInvocation("bash_exec", raw)
	if !ok {
		t.Fatal("ok = false, want true for well-formed bash_exec raw message")
	}
	if cmd != "echo hi" {
		t.Errorf("command = %q, want %q", cmd, "echo hi")
	}
}

func TestParseShellInvocation_StringInput(t *testing.T) {
	cmd, ok := ParseShellInvocation("bash_exec", `{"command":"ls","args":["-la","/tmp"]}`)
	if !ok {
		t.Fatal("ok = false, want true for well-formed string arg")
	}
	if cmd != "ls -la /tmp" {
		t.Errorf("command = %q, want %q", cmd, "ls -la /tmp")
	}
}

func TestParseShellInvocation_MalformedJSONFallsBack(t *testing.T) {
	cmd, ok := ParseShellInvocation("bash_exec", "not-valid-json{")
	if ok {
		t.Error("ok = true, want false for malformed json")
	}
	if !strings.Contains(cmd, "not-valid-json") {
		t.Errorf("fallback command = %q, want it to contain the raw input", cmd)
	}
}

func TestParseShellInvocation_EmptyArgsFallback(t *testing.T) {
	_, ok := ParseShellInvocation("bash_exec", nil)
	if ok {
		t.Error("ok = true for nil args, want false")
	}
}

func TestParseShellInvocation_BashCommandWithoutDashC(t *testing.T) {
	cmd, ok := ParseShellInvocation("bash_exec",
		map[string]any{"command": "bash", "args": []string{"foo.sh"}})
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if cmd != "bash foo.sh" {
		t.Errorf("command = %q, want %q", cmd, "bash foo.sh")
	}
}

// TestParseShellInvocation_BashBytesInput covers the []byte intake branch
// so rawArgsJSON's byte-slice case is exercised.
func TestParseShellInvocation_BashBytesInput(t *testing.T) {
	cmd, ok := ParseShellInvocation("bash_exec",
		[]byte(`{"command":"bash","args":["-c","whoami"]}`))
	if !ok || cmd != "whoami" {
		t.Fatalf("got (%q, %v), want (\"whoami\", true)", cmd, ok)
	}
}

// TestParseShellInvocation_BashExecEmptyCommand exercises the empty-command
// fallback branch of ParseShellInvocation.
func TestParseShellInvocation_BashExecEmptyCommand(t *testing.T) {
	cmd, ok := ParseShellInvocation("bash_exec", map[string]any{"command": ""})
	if ok {
		t.Error("ok = true for empty command, want false")
	}
	if cmd == "" {
		t.Error("command should fall through to raw JSON, not empty")
	}
}

// TestParseShellInvocation_FileReadMalformedArgs exercises the file_read
// Unmarshal-error branch.
func TestParseShellInvocation_FileReadMalformedArgs(t *testing.T) {
	_, ok := ParseShellInvocation("file_read", []byte("}{"))
	if ok {
		t.Error("ok = true for malformed file_read args, want false")
	}
}

// TestParseShellInvocation_EmptyRawMessageFallback covers the empty
// json.RawMessage branch.
func TestParseShellInvocation_EmptyRawMessageFallback(t *testing.T) {
	_, ok := ParseShellInvocation("bash_exec", json.RawMessage{})
	if ok {
		t.Error("ok = true for empty RawMessage, want false")
	}
}

func TestParseShellInvocation_FileToolsMissingPathFallback(t *testing.T) {
	cmd, ok := ParseShellInvocation("file_write", map[string]any{"content": "hi"})
	if ok {
		t.Error("ok = true for missing path, want false")
	}
	if cmd == "write → " {
		t.Error("command rendered empty-path arrow; want fallback")
	}
}
