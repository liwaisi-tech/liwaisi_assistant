package filemanagement

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

const (
	dirPerm      = 0o750
	filePerm     = 0o640
	maxWriteSize = 1 << 20 // 1MB
)

type writeFileArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type writeFileResult struct {
	Path         string `json:"path"`
	BytesWritten int    `json:"bytes_written"`
	Created      bool   `json:"created"`
}

func registerWriteFile(registry *tool.Registry, sandbox *Sandbox) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "write_file",
			Description: "Create a new file or overwrite an existing file at the given path within the workspace. " +
				"Parent directories are created automatically if they do not exist. Use this to create new files or " +
				"update existing file contents. The path must be relative to the workspace root (e.g., 'project/config.yaml'). " +
				"Maximum content size: 1MB.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Relative path for the file within the workspace. Example: 'src/main.go'"
					},
					"content": {
						"type": "string",
						"description": "The full content to write to the file."
					}
				},
				"required": ["path", "content"]
			}`),
		},
	}

	registry.Register(def, writeFileHandler(sandbox))
}

func writeFileHandler(sandbox *Sandbox) tool.Handler {
	return func(_ context.Context, raw json.RawMessage) (string, error) {
		var args writeFileArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing write_file arguments: %w", err)
		}

		if len(args.Content) > maxWriteSize {
			return "", fmt.Errorf("content too large (%d bytes, maximum is %d bytes)", len(args.Content), maxWriteSize)
		}

		abs, err := sandbox.Resolve(args.Path)
		if err != nil {
			return "", err
		}

		_, statErr := os.Stat(abs)
		created := statErr != nil

		if err := os.MkdirAll(filepath.Dir(abs), dirPerm); err != nil {
			return "", fmt.Errorf("creating parent directories: %w", err)
		}

		data := []byte(args.Content)
		if err := os.WriteFile(abs, data, filePerm); err != nil {
			return "", fmt.Errorf("writing file: %w", err)
		}

		result := writeFileResult{
			Path:         args.Path,
			BytesWritten: len(data),
			Created:      created,
		}

		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(out), nil
	}
}
