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

func TestGrep(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, root string)
		args    string
		wantErr string
		check   func(t *testing.T, result string)
	}{
		{
			name: "matches simple string in single file",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "hello.txt", "hello world\ngoodbye world\nhello again")
			},
			args: `{"pattern": "hello", "path": "hello.txt"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r grepResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalMatches != 2 {
					t.Errorf("total_matches = %d, want 2", r.Summary.TotalMatches)
				}
				if r.Summary.FilesMatched != 1 {
					t.Errorf("files_matched = %d, want 1", r.Summary.FilesMatched)
				}
			},
		},
		{
			name: "matches regex pattern",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "code.go", "func main() {\n\tfmt.Println()\n}\n\nfunc helper() {\n}")
			},
			args: `{"pattern": "func\\s+\\w+", "path": "code.go"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r grepResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalMatches != 2 {
					t.Errorf("total_matches = %d, want 2", r.Summary.TotalMatches)
				}
			},
		},
		{
			name: "case insensitive matching",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "mixed.txt", "Hello World\nHELLO WORLD\nhello world")
			},
			args: `{"pattern": "hello", "path": "mixed.txt", "case_insensitive": true}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r grepResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalMatches != 3 {
					t.Errorf("total_matches = %d, want 3", r.Summary.TotalMatches)
				}
			},
		},
		{
			name: "recursive directory search",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "a.txt", "target line")
				writeTestFile(t, root, "sub/b.txt", "another target here")
				writeTestFile(t, root, "sub/deep/c.txt", "no match here")
			},
			args: `{"pattern": "target"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r grepResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalMatches != 2 {
					t.Errorf("total_matches = %d, want 2", r.Summary.TotalMatches)
				}
				if r.Summary.FilesMatched != 2 {
					t.Errorf("files_matched = %d, want 2", r.Summary.FilesMatched)
				}
			},
		},
		{
			name: "include filter only searches matching files",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "main.go", "func main() {}")
				writeTestFile(t, root, "readme.md", "func main in docs")
				writeTestFile(t, root, "utils.go", "func helper() {}")
			},
			args: `{"pattern": "func", "include": "*.go"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r grepResult
				mustUnmarshal(t, result, &r)
				if r.Summary.FilesSearched != 2 {
					t.Errorf("files_searched = %d, want 2", r.Summary.FilesSearched)
				}
				if r.Summary.TotalMatches != 2 {
					t.Errorf("total_matches = %d, want 2", r.Summary.TotalMatches)
				}
			},
		},
		{
			name: "context lines before and after match",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "ctx.txt", "line1\nline2\nMATCH\nline4\nline5")
			},
			args: `{"pattern": "MATCH", "path": "ctx.txt", "context_lines": 2}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r grepResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalMatches != 1 {
					t.Errorf("total_matches = %d, want 1", r.Summary.TotalMatches)
				}
				m := r.Results[0]
				if len(m.ContextBefore) != 2 {
					t.Errorf("context_before len = %d, want 2", len(m.ContextBefore))
				}
				if len(m.ContextAfter) != 2 {
					t.Errorf("context_after len = %d, want 2", len(m.ContextAfter))
				}
				if m.ContextBefore[0] != "line1" {
					t.Errorf("context_before[0] = %q, want %q", m.ContextBefore[0], "line1")
				}
				if m.ContextAfter[1] != "line5" {
					t.Errorf("context_after[1] = %q, want %q", m.ContextAfter[1], "line5")
				}
			},
		},
		{
			name: "max results cap with truncated true",
			setup: func(t *testing.T, root string) {
				t.Helper()
				lines := make([]string, 200)
				for i := range lines {
					lines[i] = "match here"
				}
				writeTestFile(t, root, "many.txt", strings.Join(lines, "\n"))
			},
			args: `{"pattern": "match", "path": "many.txt", "max_results": 5}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r grepResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalMatches > 5 {
					t.Errorf("total_matches = %d, want <= 5", r.Summary.TotalMatches)
				}
				if !r.Summary.Truncated {
					t.Error("truncated should be true")
				}
			},
		},
		{
			name: "binary file skipped",
			setup: func(t *testing.T, root string) {
				t.Helper()
				data := []byte("match\x00binary\x00data")
				if err := os.WriteFile(filepath.Join(root, "bin.dat"), data, 0o640); err != nil {
					t.Fatal(err)
				}
			},
			args: `{"pattern": "match", "path": "bin.dat"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r grepResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalMatches != 0 {
					t.Errorf("total_matches = %d, want 0 (binary skipped)", r.Summary.TotalMatches)
				}
				if r.Summary.FilesSkipped != 1 {
					t.Errorf("files_skipped = %d, want 1", r.Summary.FilesSkipped)
				}
			},
		},
		{
			name: "empty results for non-matching pattern",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "data.txt", "no matches here")
			},
			args: `{"pattern": "zzzzz", "path": "data.txt"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r grepResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalMatches != 0 {
					t.Errorf("total_matches = %d, want 0", r.Summary.TotalMatches)
				}
			},
		},
		{
			name:    "invalid regex returns error",
			args:    `{"pattern": "["}`,
			wantErr: "invalid regex pattern",
		},
		{
			name:    "empty pattern rejected",
			args:    `{"pattern": ""}`,
			wantErr: "pattern is required",
		},
		{
			name:    "sandbox violation on path",
			args:    `{"pattern": "test", "path": "../../etc/passwd"}`,
			wantErr: "escapes workspace boundary",
		},
		{
			name:    "path not found",
			args:    `{"pattern": "test", "path": "nonexistent.txt"}`,
			wantErr: "path not found",
		},
		{
			name: "results sorted by file then line",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "b.txt", "match\nmatch")
				writeTestFile(t, root, "a.txt", "match")
			},
			args: `{"pattern": "match"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r grepResult
				mustUnmarshal(t, result, &r)
				if len(r.Results) < 2 {
					t.Fatalf("expected at least 2 results, got %d", len(r.Results))
				}
				if r.Results[0].File > r.Results[1].File {
					t.Errorf("results not sorted: %q comes after %q", r.Results[0].File, r.Results[1].File)
				}
			},
		},
		{
			name: "line numbers are correct",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "lines.txt", "no\nno\nyes\nno\nyes")
			},
			args: `{"pattern": "yes", "path": "lines.txt"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r grepResult
				mustUnmarshal(t, result, &r)
				if r.Results[0].Line != 3 {
					t.Errorf("first match line = %d, want 3", r.Results[0].Line)
				}
				if r.Results[1].Line != 5 {
					t.Errorf("second match line = %d, want 5", r.Results[1].Line)
				}
			},
		},
		{
			name: "consecutive matches within context range",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "close.txt", "before\nMATCH1\nbetween\nMATCH2\nafter")
			},
			args: `{"pattern": "MATCH", "path": "close.txt", "context_lines": 2}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r grepResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalMatches < 1 {
					t.Fatal("expected at least 1 match")
				}
				for i := 1; i < len(r.Results); i++ {
					if r.Results[i].Line <= r.Results[i-1].Line {
						t.Errorf("results not in ascending line order: line %d <= %d",
							r.Results[i].Line, r.Results[i-1].Line)
					}
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
			registerGrep(reg, sb)

			result, err := reg.Execute(context.Background(), "grep", json.RawMessage(tt.args))
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
