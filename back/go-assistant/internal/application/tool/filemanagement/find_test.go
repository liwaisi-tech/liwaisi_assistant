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

func TestFind(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, root string)
		args    string
		wantErr string
		check   func(t *testing.T, result string)
	}{
		{
			name: "finds files by glob pattern",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "main.go", "package main")
				writeTestFile(t, root, "utils.go", "package utils")
				writeTestFile(t, root, "readme.md", "# Hello")
			},
			args: `{"pattern": "*.go"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r findResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalMatches != 2 {
					t.Errorf("total_matches = %d, want 2", r.Summary.TotalMatches)
				}
				for _, e := range r.Results {
					if e.Type != "file" {
						t.Errorf("type = %q, want %q", e.Type, "file")
					}
				}
			},
		},
		{
			name: "finds directories by glob pattern",
			setup: func(t *testing.T, root string) {
				t.Helper()
				for _, d := range []string{"cmd", "cmd-tools", "internal", "pkg"} {
					if err := os.MkdirAll(filepath.Join(root, d), 0o750); err != nil {
						t.Fatal(err)
					}
				}
			},
			args: `{"pattern": "cmd*", "type": "directory"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r findResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalMatches != 2 {
					t.Errorf("total_matches = %d, want 2", r.Summary.TotalMatches)
				}
				for _, e := range r.Results {
					if e.Type != "directory" {
						t.Errorf("type = %q, want %q", e.Type, "directory")
					}
				}
			},
		},
		{
			name: "type filter file returns only files",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "test_data", "data")
				if err := os.MkdirAll(filepath.Join(root, "test_dir"), 0o750); err != nil {
					t.Fatal(err)
				}
			},
			args: `{"pattern": "test_*", "type": "file"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r findResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalMatches != 1 {
					t.Errorf("total_matches = %d, want 1", r.Summary.TotalMatches)
				}
				if r.Results[0].Type != "file" {
					t.Errorf("type = %q, want file", r.Results[0].Type)
				}
			},
		},
		{
			name: "type any returns both files and directories",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "Makefile", "all:")
				if err := os.MkdirAll(filepath.Join(root, "Makefile.d"), 0o750); err != nil {
					t.Fatal(err)
				}
			},
			args: `{"pattern": "Makefile*", "type": "any"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r findResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalMatches != 2 {
					t.Errorf("total_matches = %d, want 2", r.Summary.TotalMatches)
				}
			},
		},
		{
			name: "max depth 1 searches only root",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "top.go", "package top")
				writeTestFile(t, root, "sub/deep.go", "package deep")
			},
			args: `{"pattern": "*.go", "max_depth": 1}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r findResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalMatches != 1 {
					t.Errorf("total_matches = %d, want 1", r.Summary.TotalMatches)
				}
				if r.Results[0].Path != "top.go" {
					t.Errorf("path = %q, want %q", r.Results[0].Path, "top.go")
				}
				if !r.Summary.MaxDepthReached {
					t.Error("max_depth_reached should be true")
				}
			},
		},
		{
			name: "max depth 0 means unlimited",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "a/b/c/d/e.go", "deep")
			},
			args: `{"pattern": "*.go", "max_depth": 0}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r findResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalMatches != 1 {
					t.Errorf("total_matches = %d, want 1", r.Summary.TotalMatches)
				}
			},
		},
		{
			name: "empty results for non-matching pattern",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "hello.txt", "data")
			},
			args: `{"pattern": "*.rs"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r findResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalMatches != 0 {
					t.Errorf("total_matches = %d, want 0", r.Summary.TotalMatches)
				}
			},
		},
		{
			name:    "invalid glob pattern",
			args:    `{"pattern": "["}`,
			wantErr: "invalid glob pattern",
		},
		{
			name:    "empty pattern rejected",
			args:    `{"pattern": ""}`,
			wantErr: "pattern is required",
		},
		{
			name: "search within subdirectory",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "src/main.go", "package main")
				writeTestFile(t, root, "src/lib/utils.go", "package lib")
				writeTestFile(t, root, "other/other.go", "package other")
			},
			args: `{"pattern": "*.go", "path": "src"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r findResult
				mustUnmarshal(t, result, &r)
				if r.Summary.TotalMatches != 2 {
					t.Errorf("total_matches = %d, want 2", r.Summary.TotalMatches)
				}
				for _, e := range r.Results {
					if !strings.HasPrefix(e.Path, "src/") {
						t.Errorf("path %q should start with 'src/'", e.Path)
					}
				}
			},
		},
		{
			name:    "sandbox violation on root path",
			args:    `{"pattern": "*.go", "path": "../../etc"}`,
			wantErr: "escapes workspace boundary",
		},
		{
			name: "sorted output — directories first then alphabetical",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "z_file", "data")
				writeTestFile(t, root, "a_file", "data")
				if err := os.MkdirAll(filepath.Join(root, "m_dir"), 0o750); err != nil {
					t.Fatal(err)
				}
			},
			args: `{"pattern": "*"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r findResult
				mustUnmarshal(t, result, &r)
				if len(r.Results) < 3 {
					t.Fatalf("expected at least 3 results, got %d", len(r.Results))
				}
				if r.Results[0].Type != "directory" {
					t.Errorf("first result type = %q, want directory", r.Results[0].Type)
				}
			},
		},
		{
			name: "file size included in results",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "sized.go", "12345")
			},
			args: `{"pattern": "*.go"}`,
			check: func(t *testing.T, result string) {
				t.Helper()
				var r findResult
				mustUnmarshal(t, result, &r)
				if r.Results[0].Size != 5 {
					t.Errorf("size = %d, want 5", r.Results[0].Size)
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
			registerFind(reg, sb)

			result, err := reg.Execute(context.Background(), "find", json.RawMessage(tt.args))
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
