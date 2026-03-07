package filemanagement

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
)

func TestListDirectory(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, root string)
		args    string
		wantErr string
		check   func(t *testing.T, result string)
	}{
		{
			name: "lists files and directories",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "file1.txt", "content")
				writeTestFile(t, root, "file2.go", "package main")
				if err := os.MkdirAll(filepath.Join(root, "subdir"), 0o750); err != nil {
					t.Fatal(err)
				}
			},
			args: `{}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r listDirectoryResult
				mustUnmarshal(t, result, &r)
				if r.Total != 3 {
					t.Errorf("total = %d, want 3", r.Total)
				}
				if r.Truncated {
					t.Error("truncated = true, want false")
				}
				types := map[string]string{}
				for _, e := range r.Entries {
					types[e.Name] = e.Type
				}
				if types["file1.txt"] != "file" {
					t.Errorf("file1.txt type = %q, want %q", types["file1.txt"], "file")
				}
				if types["subdir"] != "directory" {
					t.Errorf("subdir type = %q, want %q", types["subdir"], "directory")
				}
			},
		},
		{
			name: "lists subdirectory",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "sub/a.txt", "a")
				writeTestFile(t, root, "sub/b.txt", "b")
			},
			args: `{"path": "sub"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r listDirectoryResult
				mustUnmarshal(t, result, &r)
				if r.Total != 2 {
					t.Errorf("total = %d, want 2", r.Total)
				}
				if r.Path != "sub" {
					t.Errorf("path = %q, want %q", r.Path, "sub")
				}
			},
		},
		{
			name: "empty directory",
			args: `{}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r listDirectoryResult
				mustUnmarshal(t, result, &r)
				if r.Total != 0 {
					t.Errorf("total = %d, want 0", r.Total)
				}
				if len(r.Entries) != 0 {
					t.Errorf("entries length = %d, want 0", len(r.Entries))
				}
			},
		},
		{
			name: "defaults to root with empty path",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "rootfile.txt", "hello")
			},
			args: `{"path": ""}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r listDirectoryResult
				mustUnmarshal(t, result, &r)
				if r.Total != 1 {
					t.Errorf("total = %d, want 1", r.Total)
				}
			},
		},
		{
			name: "defaults to root with nil args",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "rootfile.txt", "hello")
			},
			args: ``,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r listDirectoryResult
				mustUnmarshal(t, result, &r)
				if r.Total != 1 {
					t.Errorf("total = %d, want 1", r.Total)
				}
			},
		},
		{
			name: "truncates at 500 entries",
			setup: func(t *testing.T, root string) {
				t.Helper()
				for i := 0; i < 510; i++ {
					writeTestFile(t, root, fmt.Sprintf("file_%04d.txt", i), "x")
				}
			},
			args: `{}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r listDirectoryResult
				mustUnmarshal(t, result, &r)
				if r.Total != 510 {
					t.Errorf("total = %d, want 510", r.Total)
				}
				if !r.Truncated {
					t.Error("truncated = false, want true")
				}
				if len(r.Entries) != 500 {
					t.Errorf("entries length = %d, want 500", len(r.Entries))
				}
			},
		},
		{
			name: "path is a file",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "afile.txt", "content")
			},
			args:    `{"path": "afile.txt"}`,
			wantErr: "path is a file, not a directory",
		},
		{
			name:    "directory not found",
			args:    `{"path": "nonexistent"}`,
			wantErr: "directory not found",
		},
		{
			name:    "sandbox violation",
			args:    `{"path": "../../etc"}`,
			wantErr: "escapes workspace boundary",
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
			registerListDirectory(reg, sb)

			var args json.RawMessage
			if tt.args != "" {
				args = json.RawMessage(tt.args)
			}

			result, err := reg.Execute(context.Background(), "list_directory", args)
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
