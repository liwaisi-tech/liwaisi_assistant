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

func TestCreateDirectory(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, root string)
		args    string
		wantErr string
		check   func(t *testing.T, root, result string)
	}{
		{
			name: "creates new directory",
			args: `{"path": "newdir"}`,
			check: func(t *testing.T, root, result string) {
				t.Helper()
				var r createDirectoryResult
				mustUnmarshal(t, result, &r)
				if !r.Created {
					t.Error("created = false, want true")
				}
				info, err := os.Stat(filepath.Join(root, "newdir"))
				if err != nil {
					t.Fatal(err)
				}
				if !info.IsDir() {
					t.Error("expected directory")
				}
			},
		},
		{
			name: "creates nested directories",
			args: `{"path": "a/b/c"}`,
			check: func(t *testing.T, root, result string) {
				t.Helper()
				var r createDirectoryResult
				mustUnmarshal(t, result, &r)
				if !r.Created {
					t.Error("created = false, want true")
				}
				info, err := os.Stat(filepath.Join(root, "a", "b", "c"))
				if err != nil {
					t.Fatal(err)
				}
				if !info.IsDir() {
					t.Error("expected directory")
				}
			},
		},
		{
			name: "idempotent on existing directory",
			setup: func(t *testing.T, root string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(root, "existing"), 0o750); err != nil {
					t.Fatal(err)
				}
			},
			args: `{"path": "existing"}`,
			check: func(t *testing.T, _, result string) {
				t.Helper()
				var r createDirectoryResult
				mustUnmarshal(t, result, &r)
				if r.Created {
					t.Error("created = true, want false for existing directory")
				}
			},
		},
		{
			name: "errors when path is existing file",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "afile", "content")
			},
			args:    `{"path": "afile"}`,
			wantErr: "path exists but is a file",
		},
		{
			name:    "sandbox violation",
			args:    `{"path": "../../../tmp/evil"}`,
			wantErr: "escapes workspace boundary",
		},
		{
			name:    "absolute path rejected",
			args:    `{"path": "/tmp/evil"}`,
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
			registerCreateDirectory(reg, sb)

			result, err := reg.Execute(context.Background(), "create_directory", json.RawMessage(tt.args))
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
