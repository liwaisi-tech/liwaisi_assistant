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

func TestWriteFile(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, root string)
		args    string
		wantErr string
		check   func(t *testing.T, root, result string)
	}{
		{
			name: "creates new file",
			args: `{"path": "new.txt", "content": "hello world"}`,
			check: func(t *testing.T, root, result string) {
				t.Helper()
				var r writeFileResult
				mustUnmarshal(t, result, &r)
				if !r.Created {
					t.Error("created = false, want true")
				}
				if r.BytesWritten != 11 {
					t.Errorf("bytes_written = %d, want 11", r.BytesWritten)
				}
				got, err := os.ReadFile(filepath.Join(root, "new.txt"))
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != "hello world" {
					t.Errorf("file content = %q, want %q", string(got), "hello world")
				}
			},
		},
		{
			name: "overwrites existing file",
			setup: func(t *testing.T, root string) {
				t.Helper()
				writeTestFile(t, root, "existing.txt", "old content")
			},
			args: `{"path": "existing.txt", "content": "new content"}`,
			check: func(t *testing.T, root, result string) {
				t.Helper()
				var r writeFileResult
				mustUnmarshal(t, result, &r)
				if r.Created {
					t.Error("created = true, want false for overwrite")
				}
				got, err := os.ReadFile(filepath.Join(root, "existing.txt"))
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != "new content" {
					t.Errorf("file content = %q, want %q", string(got), "new content")
				}
			},
		},
		{
			name: "auto-creates parent directories",
			args: `{"path": "a/b/c/deep.txt", "content": "nested"}`,
			check: func(t *testing.T, root, result string) {
				t.Helper()
				var r writeFileResult
				mustUnmarshal(t, result, &r)
				if !r.Created {
					t.Error("created = false, want true")
				}
				got, err := os.ReadFile(filepath.Join(root, "a", "b", "c", "deep.txt"))
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != "nested" {
					t.Errorf("file content = %q, want %q", string(got), "nested")
				}
			},
		},
		{
			name: "writes empty content",
			args: `{"path": "empty.txt", "content": ""}`,
			check: func(t *testing.T, root, result string) {
				t.Helper()
				var r writeFileResult
				mustUnmarshal(t, result, &r)
				if r.BytesWritten != 0 {
					t.Errorf("bytes_written = %d, want 0", r.BytesWritten)
				}
			},
		},
		{
			name:    "sandbox violation",
			args:    `{"path": "../../etc/shadow", "content": "bad"}`,
			wantErr: "escapes workspace boundary",
		},
		{
			name:    "absolute path rejected",
			args:    `{"path": "/tmp/evil.txt", "content": "bad"}`,
			wantErr: "absolute paths are not allowed",
		},
		{
			name: "content too large",
			args: func() string {
				big := strings.Repeat("x", maxWriteSize+1)
				b, _ := json.Marshal(writeFileArgs{Path: "big.txt", Content: big})
				return string(b)
			}(),
			wantErr: "content too large",
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
			registerWriteFile(reg, sb)

			result, err := reg.Execute(context.Background(), "write_file", json.RawMessage(tt.args))
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
