package osnative

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecurityInterceptor(t *testing.T) {
	workspace := "/tmp/mock-workspace"
	interceptor := NewSecurityInterceptor(workspace)

	tests := []struct {
		desc    string
		command string
		args    []string
		wantErr bool
	}{
		{
			desc:    "safe command",
			command: "echo",
			args:    []string{"hello"},
			wantErr: false,
		},
		{
			desc:    "sudo command",
			command: "sudo",
			args:    []string{"ls"},
			wantErr: true,
		},
		{
			desc:    "su command",
			command: "su",
			args:    []string{"-"},
			wantErr: true,
		},
		{
			desc:    "sudo in args",
			command: "echo",
			args:    []string{"sudo"},
			wantErr: true,
		},
		{
			desc:    "destructive command inside workspace",
			command: "rm",
			args:    []string{"-rf", "/tmp/mock-workspace/foo"},
			wantErr: false,
		},
		{
			desc:    "destructive command outside workspace",
			command: "rm",
			args:    []string{"-rf", "/etc/passwd"},
			wantErr: true,
		},
		{
			desc:    "destructive command with relative path escaping workspace",
			command: "rm",
			args:    []string{"../outside"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			err := interceptor.Check(tt.command, tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("Check() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestToolSet_Execute(t *testing.T) {
	// Create a temporary workspace
	tempDir, err := os.MkdirTemp("", "osnative-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	ts, err := NewToolSet(tempDir)
	if err != nil {
		t.Fatalf("failed to create ToolSet: %v", err)
	}

	// Create a dummy file to interact with
	dummyFile := filepath.Join(tempDir, "test.txt")
	err = os.WriteFile(dummyFile, []byte("hello world\n"), 0644)
	if err != nil {
		t.Fatalf("failed to create dummy file: %v", err)
	}

	tests := []struct {
		desc      string
		argsJSON  string
		wantMatch string
		wantErr   bool
	}{
		{
			desc:      "valid grep command",
			argsJSON:  `{"command": "grep", "args": ["world", "test.txt"]}`,
			wantMatch: "hello world",
			wantErr:   false,
		},
		{
			desc:      "blocked sudo command",
			argsJSON:  `{"command": "sudo", "args": ["cat", "/etc/shadow"]}`,
			wantMatch: "",
			wantErr:   true,
		},
		{
			desc:      "invalid json",
			argsJSON:  `{"command": "grep"`, // syntax error
			wantMatch: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			rawArgs := json.RawMessage(tt.argsJSON)
			output, err := ts.handleOSNativeCommand(context.Background(), rawArgs)

			if (err != nil) != tt.wantErr {
				t.Errorf("handleOSNativeCommand() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.wantMatch != "" {
				if !strings.Contains(output, tt.wantMatch) {
					t.Errorf("output %q does not contain expected match %q", output, tt.wantMatch)
				}
			}
		})
	}
}
