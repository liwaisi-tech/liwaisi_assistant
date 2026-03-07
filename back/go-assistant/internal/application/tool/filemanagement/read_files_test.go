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

func TestReadFiles(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, root string)
		args    string
		wantErr string
		check   func(t *testing.T, result string)
	}{
		{
			name: "reads single file",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "hello.txt", "world")
			},
			args: `{"paths": ["hello.txt"]}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r readFilesResult
				mustUnmarshal(t, result, &r)
				if r.Summary.FilesRead != 1 {
					t.Errorf("files_read = %d, want 1", r.Summary.FilesRead)
				}
				if r.Results[0].Content != "world" {
					t.Errorf("content = %q, want %q", r.Results[0].Content, "world")
				}
				if r.Results[0].Type != "file" {
					t.Errorf("type = %q, want %q", r.Results[0].Type, "file")
				}
			},
		},
		{
			name: "reads multiple files concurrently",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "a.txt", "alpha")
				writeTestFile(t, root, "b.txt", "beta")
				writeTestFile(t, root, "c.txt", "gamma")
			},
			args: `{"paths": ["a.txt", "b.txt", "c.txt"]}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r readFilesResult
				mustUnmarshal(t, result, &r)
				if r.Summary.FilesRead != 3 {
					t.Errorf("files_read = %d, want 3", r.Summary.FilesRead)
				}
				if r.Summary.Errors != 0 {
					t.Errorf("errors = %d, want 0", r.Summary.Errors)
				}
			},
		},
		{
			name: "directory detection returns listing",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "mydir/child.txt", "inside")
				if err := os.MkdirAll(filepath.Join(root, "mydir", "subdir"), 0o750); err != nil {
					t.Fatal(err)
				}
			},
			args: `{"paths": ["mydir"]}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r readFilesResult
				mustUnmarshal(t, result, &r)
				if r.Summary.DirsListed != 1 {
					t.Errorf("dirs_listed = %d, want 1", r.Summary.DirsListed)
				}
				if r.Results[0].Type != "directory" {
					t.Errorf("type = %q, want %q", r.Results[0].Type, "directory")
				}
				if r.Results[0].Total != 2 {
					t.Errorf("total = %d, want 2", r.Results[0].Total)
				}
			},
		},
		{
			name: "mixed file and directory paths",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "config.yaml", "key: value")
				writeTestFile(t, root, "src/main.go", "package main")
			},
			args: `{"paths": ["config.yaml", "src"]}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r readFilesResult
				mustUnmarshal(t, result, &r)
				if r.Summary.FilesRead != 1 {
					t.Errorf("files_read = %d, want 1", r.Summary.FilesRead)
				}
				if r.Summary.DirsListed != 1 {
					t.Errorf("dirs_listed = %d, want 1", r.Summary.DirsListed)
				}
			},
		},
		{
			name: "not found per-path does not affect others",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "exists.txt", "here")
			},
			args: `{"paths": ["exists.txt", "ghost.txt"]}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r readFilesResult
				mustUnmarshal(t, result, &r)
				if r.Summary.FilesRead != 1 {
					t.Errorf("files_read = %d, want 1", r.Summary.FilesRead)
				}
				if r.Summary.Errors != 1 {
					t.Errorf("errors = %d, want 1", r.Summary.Errors)
				}
				if !strings.Contains(r.Results[1].Error, "not found") {
					t.Errorf("error = %q, want containing 'not found'", r.Results[1].Error)
				}
			},
		},
		{
			name: "file too large per-path",
			setup: func(t *testing.T, root string) {
				t.Helper()
				data := make([]byte, maxReadSize+1)
				if err := os.WriteFile(filepath.Join(root, "big.bin"), data, 0o640); err != nil {
					t.Fatal(err)
				}
				writeTestFile(t, root, "small.txt", "ok")
			},
			args: `{"paths": ["big.bin", "small.txt"]}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r readFilesResult
				mustUnmarshal(t, result, &r)
				if r.Summary.Errors != 1 {
					t.Errorf("errors = %d, want 1", r.Summary.Errors)
				}
				if r.Summary.FilesRead != 1 {
					t.Errorf("files_read = %d, want 1", r.Summary.FilesRead)
				}
				if !strings.Contains(r.Results[0].Error, "too large") {
					t.Errorf("error = %q, want containing 'too large'", r.Results[0].Error)
				}
			},
		},
		{
			name:    "empty paths list rejected",
			args:    `{"paths": []}`,
			wantErr: "at least one path",
		},
		{
			name:    "too many paths rejected",
			args:    `{"paths": ["` + strings.Join(makePaths(21), `","`) + `"]}`,
			wantErr: "too many paths",
		},
		{
			name: "sandbox violation per-path",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "safe.txt", "ok")
			},
			args: `{"paths": ["safe.txt", "../../etc/passwd"]}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r readFilesResult
				mustUnmarshal(t, result, &r)
				if r.Summary.FilesRead != 1 {
					t.Errorf("files_read = %d, want 1", r.Summary.FilesRead)
				}
				if r.Summary.Errors != 1 {
					t.Errorf("errors = %d, want 1", r.Summary.Errors)
				}
			},
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
			registerReadFiles(reg, sb)

			result, err := reg.Execute(context.Background(), "read_files", json.RawMessage(tt.args))
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
