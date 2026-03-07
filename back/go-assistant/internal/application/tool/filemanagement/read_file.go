package filemanagement

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

const maxReadSize = 1 << 20 // 1MB

type readFileArgs struct {
	Path string `json:"path"`
}

type readFileResult struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Size    int64  `json:"size"`
}

func registerReadFile(registry *tool.Registry, sandbox *Sandbox) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "read_file",
			Description: "Read the contents of a file at the given path within the workspace. " +
				"Returns the file content as a string. Use this when you need to inspect, analyze, " +
				"or reference the content of an existing file. The path must be relative to the " +
				"workspace root (e.g., 'project/main.go'). Maximum file size: 1MB.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Relative path to the file within the workspace. Example: 'notes/todo.txt'"
					}
				},
				"required": ["path"]
			}`),
		},
	}

	registry.Register(def, readFileHandler(sandbox))
}

func readFileHandler(sandbox *Sandbox) tool.Handler {
	return func(_ context.Context, raw json.RawMessage) (string, error) {
		var args readFileArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing read_file arguments: %w", err)
		}

		abs, err := sandbox.Resolve(args.Path)
		if err != nil {
			return "", err
		}

		if sandbox.IsSensitiveFile(abs) {
			return "", fmt.Errorf(
				"access denied: %q matches a sensitive file pattern. Use 'liwaisi env list' to manage credentials",
				args.Path,
			)
		}

		info, err := os.Stat(abs)
		if err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("file not found: %s", args.Path)
			}
			return "", fmt.Errorf("accessing file: %w", err)
		}

		if info.IsDir() {
			return "", fmt.Errorf("path is a directory, not a file: %s", args.Path)
		}

		if info.Size() > maxReadSize {
			return "", fmt.Errorf("file too large (%d bytes, maximum is %d bytes): %s", info.Size(), maxReadSize, args.Path)
		}

		data, err := os.ReadFile(abs)
		if err != nil {
			return "", fmt.Errorf("reading file: %w", err)
		}

		result := readFileResult{
			Path:    args.Path,
			Content: string(data),
			Size:    info.Size(),
		}

		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(out), nil
	}
}
