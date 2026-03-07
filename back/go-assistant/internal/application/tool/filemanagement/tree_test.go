package filemanagement

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
)

func TestTree(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, root string)
		args    string
		wantErr string
		check   func(t *testing.T, result string)
	}{
		{
			name: "depth 1 shows only immediate children",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o750); err != nil {
					t.Fatal(err)
				}
				writeTestFile(t, root, "file.txt", "x")
			},
			args: `{"depth": 1}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r treeResult
				mustUnmarshal(t, result, &r)

				if r.Summary.TotalDirs != 1 {
					t.Errorf("total_dirs = %d, want 1", r.Summary.TotalDirs)
				}
				if r.Summary.TotalFiles != 1 {
					t.Errorf("total_files = %d, want 1", r.Summary.TotalFiles)
				}

				found := findChild(r.Tree, "a")
				if found == nil {
					t.Fatal("expected child 'a'")
				}
				if !found.ChildrenOmitted {
					t.Error("expected children_omitted=true for 'a' at depth 1")
				}

				bNode := findChild(r.Tree, "b")
				if bNode != nil {
					t.Error("'b' should not appear at depth 1")
				}
			},
		},
		{
			name: "depth 3 recursive traversal",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "a/b/c/deep.txt", "deep")
				writeTestFile(t, root, "top.txt", "top")
			},
			args: `{"depth": 3}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r treeResult
				mustUnmarshal(t, result, &r)

				aNode := findChild(r.Tree, "a")
				if aNode == nil {
					t.Fatal("missing 'a'")
				}
				bNode := findChild(aNode, "b")
				if bNode == nil {
					t.Fatal("missing 'a/b'")
				}
				cNode := findChild(bNode, "c")
				if cNode == nil {
					t.Fatal("missing 'a/b/c'")
				}
				if !cNode.ChildrenOmitted {
					t.Error("expected children_omitted=true for 'c' at depth 3")
				}
			},
		},
		{
			name:  "depth clamped below minimum",
			setup: func(t *testing.T, root string) { t.Helper(); writeTestFile(t, root, "f.txt", "x") },
			args:  `{"depth": 0}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r treeResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalFiles != 1 {
					t.Errorf("total_files = %d, want 1", r.Summary.TotalFiles)
				}
			},
		},
		{
			name:  "depth clamped above maximum",
			setup: func(t *testing.T, root string) { t.Helper(); writeTestFile(t, root, "f.txt", "x") },
			args:  `{"depth": 99}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r treeResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalFiles != 1 {
					t.Errorf("total_files = %d, want 1", r.Summary.TotalFiles)
				}
			},
		},
		{
			name: "empty directory",
			args: `{"depth": 1}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r treeResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalFiles != 0 {
					t.Errorf("total_files = %d, want 0", r.Summary.TotalFiles)
				}
				if r.Summary.TotalDirs != 0 {
					t.Errorf("total_dirs = %d, want 0", r.Summary.TotalDirs)
				}
				if r.Tree.Children == nil {
					t.Error("children should be empty slice, not nil")
				}
				if len(r.Tree.Children) != 0 {
					t.Errorf("children length = %d, want 0", len(r.Tree.Children))
				}
			},
		},
		{
			name: "sorted output directories first then alphabetical",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "z_file.txt", "z")
				writeTestFile(t, root, "a_file.txt", "a")
				if err := os.MkdirAll(filepath.Join(root, "m_dir"), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(root, "b_dir"), 0o750); err != nil {
					t.Fatal(err)
				}
			},
			args: `{"depth": 1}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r treeResult
				mustUnmarshal(t, result, &r)

				if len(r.Tree.Children) != 4 {
					t.Fatalf("children count = %d, want 4", len(r.Tree.Children))
				}
				names := make([]string, len(r.Tree.Children))
				for i, c := range r.Tree.Children {
					names[i] = c.Name
				}
				want := []string{"b_dir", "m_dir", "a_file.txt", "z_file.txt"}
				for i, n := range want {
					if names[i] != n {
						t.Errorf("position %d = %q, want %q (got %v)", i, names[i], n, names)
						break
					}
				}
			},
		},
		{
			name: "subdirectory path",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "sub/inner.txt", "inner")
			},
			args: `{"path": "sub", "depth": 1}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r treeResult
				mustUnmarshal(t, result, &r)
				if r.Path != "sub" {
					t.Errorf("path = %q, want %q", r.Path, "sub")
				}
				if r.Summary.TotalFiles != 1 {
					t.Errorf("total_files = %d, want 1", r.Summary.TotalFiles)
				}
			},
		},
		{
			name:    "directory not found",
			args:    `{"path": "nonexistent", "depth": 1}`,
			wantErr: "directory not found",
		},
		{
			name: "path is a file",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "afile.txt", "content")
			},
			args:    `{"path": "afile.txt", "depth": 1}`,
			wantErr: "path is a file, not a directory",
		},
		{
			name:    "sandbox violation",
			args:    `{"path": "../../etc", "depth": 1}`,
			wantErr: "escapes workspace boundary",
		},
		{
			name: "concurrent traversal completes without race",
			setup: func(t *testing.T, root string) {
				t.Helper()
				for i := 0; i < 50; i++ {
					dir := filepath.Join(root, fmt.Sprintf("dir-%03d", i))
					if err := os.MkdirAll(dir, 0o750); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o640); err != nil {
						t.Fatal(err)
					}
				}
			},
			args: `{"depth": 2}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r treeResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalDirs != 50 {
					t.Errorf("total_dirs = %d, want 50", r.Summary.TotalDirs)
				}
				if r.Summary.TotalFiles != 50 {
					t.Errorf("total_files = %d, want 50", r.Summary.TotalFiles)
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
			registerTree(reg, sb)

			var args json.RawMessage
			if tt.args != "" {
				args = json.RawMessage(tt.args)
			}

			result, err := reg.Execute(context.Background(), "tree", args)
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

func TestTreeContextCancellation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 20; i++ {
		dir := filepath.Join(root, fmt.Sprintf("dir-%03d", i))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 5; j++ {
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d.txt", j)), []byte("x"), 0o640); err != nil {
				t.Fatal(err)
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	reg := tool.NewRegistry()
	sb := mustNewSandbox(t, root)
	registerTree(reg, sb)

	_, err := reg.Execute(ctx, "tree", json.RawMessage(`{"depth": 5}`))
	if err != nil {
		t.Fatalf("tree should not error on canceled context, got: %v", err)
	}
}

func TestTreeGoroutineLeak(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 30; i++ {
		dir := filepath.Join(root, fmt.Sprintf("dir-%03d", i))
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}

	before := runtime.NumGoroutine()

	reg := tool.NewRegistry()
	sb := mustNewSandbox(t, root)
	registerTree(reg, sb)

	_, err := reg.Execute(context.Background(), "tree", json.RawMessage(`{"depth": 3}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	after := runtime.NumGoroutine()

	// Allow a small delta for runtime goroutines.
	if delta := after - before; delta > 5 {
		t.Errorf("goroutine leak: before=%d, after=%d, delta=%d", before, after, delta)
	}
}

func findChild(node *treeNode, name string) *treeNode {
	if node == nil {
		return nil
	}
	for _, c := range node.Children {
		if c.Name == name {
			return c
		}
	}
	return nil
}
