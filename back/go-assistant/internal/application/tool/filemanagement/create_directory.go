package filemanagement

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

type createDirectoryArgs struct {
	Path string `json:"path"`
}

type createDirectoryResult struct {
	Path    string `json:"path"`
	Created bool   `json:"created"`
}

func registerCreateDirectory(registry *tool.Registry, sandbox *Sandbox) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "create_directory",
			Description: "Create a new directory (and any necessary parent directories) at the given path within the workspace. " +
				"The operation is idempotent: if the directory already exists, it succeeds without error. " +
				"The path must be relative to the workspace root (e.g., 'projects/my-app/src').",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Relative path for the directory within the workspace. Example: 'projects/my-app/src'"
					}
				},
				"required": ["path"]
			}`),
		},
	}

	registry.Register(def, createDirectoryHandler(sandbox))
}

func createDirectoryHandler(sandbox *Sandbox) tool.Handler {
	return func(_ context.Context, raw json.RawMessage) (string, error) {
		var args createDirectoryArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing create_directory arguments: %w", err)
		}

		abs, err := sandbox.Resolve(args.Path)
		if err != nil {
			return "", err
		}

		info, statErr := os.Stat(abs)
		if statErr == nil && !info.IsDir() {
			return "", fmt.Errorf("path exists but is a file, not a directory: %s", args.Path)
		}

		created := statErr != nil

		if err := os.MkdirAll(abs, dirPerm); err != nil {
			return "", fmt.Errorf("creating directory: %w", err)
		}

		result := createDirectoryResult{
			Path:    args.Path,
			Created: created,
		}

		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(out), nil
	}
}
