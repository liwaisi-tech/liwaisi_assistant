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

func TestRename(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, root string)
		args    string
		wantErr string
		check   func(t *testing.T, root, result string)
	}{
		{
			name: "rename a file",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "src/old.go", "package old")
			},
			args: `{"path": "src/old.go", "new_name": "new.go"}`,
			check: func(t *testing.T, root, result string) {
				t.Helper()
				var r renameResult
				mustUnmarshal(t, result, &r)
				if r.Type != "file" {
					t.Errorf("type = %q, want %q", r.Type, "file")
				}
				if r.OldPath != "src/old.go" {
					t.Errorf("old_path = %q, want %q", r.OldPath, "src/old.go")
				}
				if r.NewPath != "src/new.go" {
					t.Errorf("new_path = %q, want %q", r.NewPath, "src/new.go")
				}

				if _, err := os.Stat(filepath.Join(root, "src", "old.go")); !os.IsNotExist(err) {
					t.Error("old file should no longer exist")
				}
				data, err := os.ReadFile(filepath.Join(root, "src", "new.go"))
				if err != nil {
					t.Fatalf("reading renamed file: %v", err)
				}
				if string(data) != "package old" {
					t.Errorf("content = %q, want %q", string(data), "package old")
				}
			},
		},
		{
			name: "rename a directory",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "old-dir/file.txt", "inside")
			},
			args: `{"path": "old-dir", "new_name": "new-dir"}`,
			check: func(t *testing.T, root, result string) {
				t.Helper()
				var r renameResult
				mustUnmarshal(t, result, &r)
				if r.Type != "directory" {
					t.Errorf("type = %q, want %q", r.Type, "directory")
				}

				if _, err := os.Stat(filepath.Join(root, "old-dir")); !os.IsNotExist(err) {
					t.Error("old directory should no longer exist")
				}
				data, err := os.ReadFile(filepath.Join(root, "new-dir", "file.txt"))
				if err != nil {
					t.Fatalf("reading file in renamed dir: %v", err)
				}
				if string(data) != "inside" {
					t.Errorf("content = %q, want %q", string(data), "inside")
				}
			},
		},
		{
			name: "slash in new_name rejected",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "file.txt", "x")
			},
			args:    `{"path": "file.txt", "new_name": "sub/name.txt"}`,
			wantErr: "new_name must not contain path separators",
		},
		{
			name:    "source not found",
			args:    `{"path": "nonexistent.txt", "new_name": "new.txt"}`,
			wantErr: "source not found",
		},
		{
			name: "target name already exists",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "a.txt", "a")
				writeTestFile(t, root, "b.txt", "b")
			},
			args:    `{"path": "a.txt", "new_name": "b.txt"}`,
			wantErr: "target name already exists",
		},
		{
			name:    "sandbox violation via path",
			args:    `{"path": "../../etc/passwd", "new_name": "new.txt"}`,
			wantErr: "escapes workspace boundary",
		},
		{
			name: "dotdot in new_name rejected by sandbox",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "file.txt", "x")
			},
			args:    `{"path": "file.txt", "new_name": ".."}`,
			wantErr: "escapes workspace boundary",
		},
		{
			name:    "empty path",
			args:    `{"path": "", "new_name": "new.txt"}`,
			wantErr: "path is required",
		},
		{
			name: "empty new_name",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "file.txt", "x")
			},
			args:    `{"path": "file.txt", "new_name": ""}`,
			wantErr: "new_name is required",
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
			registerRename(reg, sb)

			result, err := reg.Execute(context.Background(), "rename", json.RawMessage(tt.args))
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
