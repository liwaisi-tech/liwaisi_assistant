package filemanagement

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
)

func TestReadFile(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, root string)
		args    string
		wantErr string
		check   func(t *testing.T, result string)
	}{
		{
			name: "reads existing file",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "hello.txt", "world")
			},
			args: `{"path": "hello.txt"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r readFileResult
				mustUnmarshal(t, result, &r)
				if r.Content != "world" {
					t.Errorf("content = %q, want %q", r.Content, "world")
				}
				if r.Size != 5 {
					t.Errorf("size = %d, want 5", r.Size)
				}
				if r.Path != "hello.txt" {
					t.Errorf("path = %q, want %q", r.Path, "hello.txt")
				}
			},
		},
		{
			name: "reads nested file",
			setup: func(t *testing.T, root string) {
				t.Helper()
				dir := filepath.Join(root, "sub", "dir")
				if err := os.MkdirAll(dir, 0o750); err != nil {
					t.Fatal(err)
				}
				writeTestFile(t, root, "sub/dir/nested.txt", "deep content")
			},
			args: `{"path": "sub/dir/nested.txt"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r readFileResult
				mustUnmarshal(t, result, &r)
				if r.Content != "deep content" {
					t.Errorf("content = %q, want %q", r.Content, "deep content")
				}
			},
		},
		{
			name:    "file not found",
			args:    `{"path": "nonexistent.txt"}`,
			wantErr: "file not found",
		},
		{
			name: "path is directory",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "mydir"), 0o750); err != nil {
					t.Fatal(err)
				}
			},
			args:    `{"path": "mydir"}`,
			wantErr: "path is a directory",
		},
		{
			name: "file too large",
			setup: func(t *testing.T, root string) {
				t.Helper()
				data := make([]byte, maxReadSize+1)
				if err := os.WriteFile(filepath.Join(root, "big.bin"), data, 0o640); err != nil {
					t.Fatal(err)
				}
			},
			args:    `{"path": "big.bin"}`,
			wantErr: "file too large",
		},
		{
			name:    "sandbox violation",
			args:    `{"path": "../../etc/passwd"}`,
			wantErr: "escapes workspace boundary",
		},
		{
			name:    "absolute path rejected",
			args:    `{"path": "/etc/passwd"}`,
			wantErr: "absolute paths are not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.setup != nil {
				tt.setup(t, root)
			}

			reg := tool.NewRegistry()
			sb := mustNewSandbox(t, root)
			registerReadFile(reg, sb)

			result, err := reg.Execute(context.Background(), "read_file", json.RawMessage(tt.args))
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

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
}

func mustUnmarshal(t *testing.T, data string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(data), v); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
}
