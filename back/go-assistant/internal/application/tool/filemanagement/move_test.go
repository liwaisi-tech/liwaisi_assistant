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

func TestMove(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, root string)
		args    string
		wantErr string
		check   func(t *testing.T, root, result string)
	}{
		{
			name: "move a file",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "src/file.txt", "hello")
			},
			args: `{"source": "src/file.txt", "destination": "dst/file.txt"}`,
			check: func(t *testing.T, root, result string) {
				t.Helper()
				var r moveResult
				mustUnmarshal(t, result, &r)
				if r.Type != "file" {
					t.Errorf("type = %q, want %q", r.Type, "file")
				}
				if r.Source != "src/file.txt" {
					t.Errorf("source = %q, want %q", r.Source, "src/file.txt")
				}
				if r.Destination != "dst/file.txt" {
					t.Errorf("destination = %q, want %q", r.Destination, "dst/file.txt")
				}

				if _, err := os.Stat(filepath.Join(root, "src", "file.txt")); !os.IsNotExist(err) {
					t.Error("source file should no longer exist")
				}
				data, err := os.ReadFile(filepath.Join(root, "dst", "file.txt"))
				if err != nil {
					t.Fatalf("reading destination: %v", err)
				}
				if string(data) != "hello" {
					t.Errorf("content = %q, want %q", string(data), "hello")
				}
			},
		},
		{
			name: "move a directory",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "old/a.txt", "a")
				writeTestFile(t, root, "old/b.txt", "b")
			},
			args: `{"source": "old", "destination": "new"}`,
			check: func(t *testing.T, root, result string) {
				t.Helper()
				var r moveResult
				mustUnmarshal(t, result, &r)
				if r.Type != "directory" {
					t.Errorf("type = %q, want %q", r.Type, "directory")
				}
				if _, err := os.Stat(filepath.Join(root, "old")); !os.IsNotExist(err) {
					t.Error("source directory should no longer exist")
				}
				if _, err := os.Stat(filepath.Join(root, "new", "a.txt")); err != nil {
					t.Errorf("new/a.txt should exist: %v", err)
				}
				if _, err := os.Stat(filepath.Join(root, "new", "b.txt")); err != nil {
					t.Errorf("new/b.txt should exist: %v", err)
				}
			},
		},
		{
			name: "auto-creates destination parent directories",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "file.txt", "content")
			},
			args: `{"source": "file.txt", "destination": "deep/nested/dir/file.txt"}`,
			check: func(t *testing.T, root, result string) {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(root, "deep", "nested", "dir", "file.txt"))
				if err != nil {
					t.Fatalf("reading destination: %v", err)
				}
				if string(data) != "content" {
					t.Errorf("content = %q, want %q", string(data), "content")
				}
			},
		},
		{
			name:    "source not found",
			args:    `{"source": "nonexistent.txt", "destination": "dest.txt"}`,
			wantErr: "source not found",
		},
		{
			name: "destination already exists",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "src.txt", "src")
				writeTestFile(t, root, "dst.txt", "dst")
			},
			args:    `{"source": "src.txt", "destination": "dst.txt"}`,
			wantErr: "destination already exists",
		},
		{
			name: "sandbox violation on source",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "file.txt", "x")
			},
			args:    `{"source": "../../etc/passwd", "destination": "file.txt"}`,
			wantErr: "escapes workspace boundary",
		},
		{
			name: "sandbox violation on destination",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "file.txt", "x")
			},
			args:    `{"source": "file.txt", "destination": "../../tmp/evil.txt"}`,
			wantErr: "escapes workspace boundary",
		},
		{
			name:    "empty source path",
			args:    `{"source": "", "destination": "dst.txt"}`,
			wantErr: "source path is required",
		},
		{
			name: "empty destination path",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "file.txt", "x")
			},
			args:    `{"source": "file.txt", "destination": ""}`,
			wantErr: "destination path is required",
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
			registerMove(reg, sb)

			result, err := reg.Execute(context.Background(), "move", json.RawMessage(tt.args))
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
				tt.check(t, root, result)
			}
		})
	}
}
