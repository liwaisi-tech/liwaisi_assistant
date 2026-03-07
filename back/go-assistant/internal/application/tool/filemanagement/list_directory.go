package filemanagement

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

const maxListEntries = 500

type listDirectoryArgs struct {
	Path string `json:"path"`
}

type listDirectoryEntry struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

type listDirectoryResult struct {
	Path      string               `json:"path"`
	Entries   []listDirectoryEntry `json:"entries"`
	Total     int                  `json:"total"`
	Truncated bool                 `json:"truncated"`
}

func registerListDirectory(registry *tool.Registry, sandbox *Sandbox) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "list_directory",
			Description: "List the contents of a directory within the workspace. Returns an array of entries with " +
				"name, type (file/directory), size, and last modified time. If no path is provided, lists the " +
				"workspace root. The path must be relative to the workspace root. Maximum 500 entries.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Relative path to the directory within the workspace. Defaults to workspace root if empty. Example: 'src'"
					}
				}
			}`),
		},
	}

	registry.Register(def, listDirectoryHandler(sandbox))
}

func listDirectoryHandler(sandbox *Sandbox) tool.Handler {
	return func(_ context.Context, raw json.RawMessage) (string, error) {
		var args listDirectoryArgs
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("parsing list_directory arguments: %w", err)
			}
		}

		path := args.Path
		if path == "" {
			path = "."
		}

		abs, err := sandbox.Resolve(path)
		if err != nil {
			return "", err
		}

		info, err := os.Stat(abs)
		if err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("directory not found: %s", args.Path)
			}
			return "", fmt.Errorf("accessing directory: %w", err)
		}

		if !info.IsDir() {
			return "", fmt.Errorf("path is a file, not a directory: %s", args.Path)
		}

		dirEntries, err := os.ReadDir(abs)
		if err != nil {
			return "", fmt.Errorf("reading directory: %w", err)
		}

		total := len(dirEntries)
		truncated := total > maxListEntries
		if truncated {
			dirEntries = dirEntries[:maxListEntries]
		}

		entries := make([]listDirectoryEntry, 0, len(dirEntries))
		for _, de := range dirEntries {
			entryType := "file"
			if de.IsDir() {
				entryType = "directory"
			}

			var size int64
			var modified string
			if fi, err := de.Info(); err == nil {
				size = fi.Size()
				modified = fi.ModTime().UTC().Format(time.RFC3339)
			}

			entries = append(entries, listDirectoryEntry{
				Name:     de.Name(),
				Type:     entryType,
				Size:     size,
				Modified: modified,
			})
		}

		result := listDirectoryResult{
			Path:      args.Path,
			Entries:   entries,
			Total:     total,
			Truncated: truncated,
		}

		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(out), nil
	}
}
