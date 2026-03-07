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

func TestDelete(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, root string)
		args    string
		wantErr string
		ctxFn   func() context.Context
		check   func(t *testing.T, root string, result string)
	}{
		{
			name: "deletes single file",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "remove-me.txt", "bye")
			},
			args: `{"paths": ["remove-me.txt"]}`,
			check: func(t *testing.T, root string, result string) {
				t.Helper()
				var r deleteResult
				mustUnmarshal(t, result, &r)
				if r.Summary.Succeeded != 1 {
					t.Errorf("succeeded = %d, want 1", r.Summary.Succeeded)
				}
				if r.Summary.Failed != 0 {
					t.Errorf("failed = %d, want 0", r.Summary.Failed)
				}
				if r.Results[0].Type != "file" {
					t.Errorf("type = %q, want %q", r.Results[0].Type, "file")
				}
				if _, err := os.Stat(filepath.Join(root, "remove-me.txt")); !os.IsNotExist(err) {
					t.Error("file still exists after deletion")
				}
			},
		},
		{
			name: "deletes multiple files concurrently",
			setup: func(t *testing.T, root string) {
				t.Helper()
				for i := range 5 {
					writeTestFile(t, root, filepath.Join("batch", strings.Repeat("a", i+1)+".txt"), "data")
				}
			},
			args: `{"paths": ["batch/a.txt", "batch/aa.txt", "batch/aaa.txt", "batch/aaaa.txt", "batch/aaaaa.txt"]}`,
			check: func(t *testing.T, root string, result string) {
				t.Helper()
				var r deleteResult
				mustUnmarshal(t, result, &r)
				if r.Summary.Succeeded != 5 {
					t.Errorf("succeeded = %d, want 5", r.Summary.Succeeded)
				}
				if r.Summary.Failed != 0 {
					t.Errorf("failed = %d, want 0", r.Summary.Failed)
				}
			},
		},
		{
			name: "deletes empty directory without force",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "empty-dir"), 0o750); err != nil {
					t.Fatal(err)
				}
			},
			args: `{"paths": ["empty-dir"]}`,
			check: func(t *testing.T, root string, result string) {
				t.Helper()
				var r deleteResult
				mustUnmarshal(t, result, &r)
				if r.Summary.Succeeded != 1 {
					t.Errorf("succeeded = %d, want 1", r.Summary.Succeeded)
				}
				if r.Results[0].Type != "directory" {
					t.Errorf("type = %q, want %q", r.Results[0].Type, "directory")
				}
			},
		},
		{
			name: "fails on non-empty directory without force",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "nonempty/child.txt", "inside")
			},
			args: `{"paths": ["nonempty"]}`,
			check: func(t *testing.T, _ string, result string) {
				t.Helper()
				var r deleteResult
				mustUnmarshal(t, result, &r)
				if r.Summary.Succeeded != 0 {
					t.Errorf("succeeded = %d, want 0", r.Summary.Succeeded)
				}
				if r.Summary.Failed != 1 {
					t.Errorf("failed = %d, want 1", r.Summary.Failed)
				}
				if !strings.Contains(r.Results[0].Error, "force=true") {
					t.Errorf("error = %q, want containing 'force=true'", r.Results[0].Error)
				}
			},
		},
		{
			name: "deletes non-empty directory with force",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "tree/a/b/c.txt", "deep")
				writeTestFile(t, root, "tree/x.txt", "shallow")
			},
			args: `{"paths": ["tree"], "force": true}`,
			check: func(t *testing.T, root string, result string) {
				t.Helper()
				var r deleteResult
				mustUnmarshal(t, result, &r)
				if r.Summary.Succeeded != 1 {
					t.Errorf("succeeded = %d, want 1", r.Summary.Succeeded)
				}
				if _, err := os.Stat(filepath.Join(root, "tree")); !os.IsNotExist(err) {
					t.Error("directory still exists after force deletion")
				}
			},
		},
		{
			name: "best effort — partial failure continues",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "keep.txt", "data")
				writeTestFile(t, root, "also-keep.txt", "more")
			},
			args: `{"paths": ["keep.txt", "nonexistent.txt", "also-keep.txt"]}`,
			check: func(t *testing.T, root string, result string) {
				t.Helper()
				var r deleteResult
				mustUnmarshal(t, result, &r)
				if r.Summary.Succeeded != 2 {
					t.Errorf("succeeded = %d, want 2", r.Summary.Succeeded)
				}
				if r.Summary.Failed != 1 {
					t.Errorf("failed = %d, want 1", r.Summary.Failed)
				}
				if _, err := os.Stat(filepath.Join(root, "keep.txt")); !os.IsNotExist(err) {
					t.Error("keep.txt should have been deleted")
				}
				if _, err := os.Stat(filepath.Join(root, "also-keep.txt")); !os.IsNotExist(err) {
					t.Error("also-keep.txt should have been deleted")
				}
			},
		},
		{
			name: "sandbox violation per-path does not affect others",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "safe.txt", "ok")
			},
			args: `{"paths": ["safe.txt", "../../etc/passwd"]}`,
			check: func(t *testing.T, root string, result string) {
				t.Helper()
				var r deleteResult
				mustUnmarshal(t, result, &r)
				if r.Summary.Succeeded != 1 {
					t.Errorf("succeeded = %d, want 1", r.Summary.Succeeded)
				}
				if r.Summary.Failed != 1 {
					t.Errorf("failed = %d, want 1", r.Summary.Failed)
				}
				if !strings.Contains(r.Results[1].Error, "escapes workspace") {
					t.Errorf("error = %q, want containing 'escapes workspace'", r.Results[1].Error)
				}
			},
		},
		{
			name: "path not found per-path",
			args: `{"paths": ["ghost.txt"]}`,
			check: func(t *testing.T, _ string, result string) {
				t.Helper()
				var r deleteResult
				mustUnmarshal(t, result, &r)
				if r.Summary.Failed != 1 {
					t.Errorf("failed = %d, want 1", r.Summary.Failed)
				}
				if !strings.Contains(r.Results[0].Error, "not found") {
					t.Errorf("error = %q, want containing 'not found'", r.Results[0].Error)
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
			args:    `{"paths": ["` + strings.Join(makePaths(51), `","`) + `"]}`,
			wantErr: "too many paths",
		},
		{
			name: "single file basic execution",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "ctx-file.txt", "data")
			},
			args: `{"paths": ["ctx-file.txt"]}`,
			check: func(t *testing.T, _ string, result string) {
				t.Helper()
				var r deleteResult
				mustUnmarshal(t, result, &r)
				if r.Summary.Total != 1 {
					t.Errorf("total = %d, want 1", r.Summary.Total)
				}
			},
		},
		{
			name: "pre-canceled context returns context error",
			ctxFn: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			setup: func(t *testing.T, root string) {
				t.Helper()
				for i := range 15 {
					writeTestFile(t, root, fmt.Sprintf("f%d.txt", i), "data")
				}
			},
			args: `{"paths": ["f0.txt","f1.txt","f2.txt","f3.txt","f4.txt","f5.txt","f6.txt","f7.txt","f8.txt","f9.txt","f10.txt","f11.txt","f12.txt","f13.txt","f14.txt"]}`,
			check: func(t *testing.T, _ string, result string) {
				t.Helper()
				var r deleteResult
				mustUnmarshal(t, result, &r)
				if r.Summary.Total != 15 {
					t.Errorf("total = %d, want 15", r.Summary.Total)
				}
				var canceled int
				for _, res := range r.Results {
					if strings.Contains(res.Error, "context canceled") {
						canceled++
					}
				}
				if canceled == 0 {
					t.Error("expected at least one path to report context canceled")
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
			registerDelete(reg, sb)

			ctx := context.Background()
			if tt.ctxFn != nil {
				ctx = tt.ctxFn()
			}
			result, err := reg.Execute(ctx, "delete", json.RawMessage(tt.args))
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

func makePaths(n int) []string {
	paths := make([]string, n)
	for i := range n {
		paths[i] = filepath.Join("dir", strings.Repeat("f", i+1)+".txt")
	}
	return paths
}
