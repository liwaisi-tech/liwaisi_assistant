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

type moveArgs struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type moveResult struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Type        string `json:"type"`
}

func registerMove(registry *tool.Registry, sandbox *Sandbox) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "move",
			Description: "Move a file or directory from one location to another within the workspace. Both source " +
				"and destination must be relative paths within the workspace. If the destination's parent directory " +
				"does not exist, it will be created automatically. Use this to reorganize files and directories. " +
				"Cannot overwrite existing files — rename or remove the target first.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"source": {
						"type": "string",
						"description": "Relative path to the file or directory to move. Example: 'old/location/file.go'"
					},
					"destination": {
						"type": "string",
						"description": "Relative path for the new location. Example: 'new/location/file.go'"
					}
				},
				"required": ["source", "destination"]
			}`),
		},
	}

	registry.Register(def, moveHandler(sandbox))
}

func moveHandler(sandbox *Sandbox) tool.Handler {
	return func(_ context.Context, raw json.RawMessage) (string, error) {
		var args moveArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing move arguments: %w", err)
		}

		if args.Source == "" {
			return "", fmt.Errorf("source path is required")
		}
		if args.Destination == "" {
			return "", fmt.Errorf("destination path is required")
		}

		srcAbs, err := sandbox.Resolve(args.Source)
		if err != nil {
			return "", err
		}

		dstAbs, err := sandbox.Resolve(args.Destination)
		if err != nil {
			return "", err
		}

		srcInfo, err := os.Stat(srcAbs)
		if err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("source not found: %s", args.Source)
			}
			return "", fmt.Errorf("accessing source: %w", err)
		}

		if _, err := os.Stat(dstAbs); err == nil {
			return "", fmt.Errorf("destination already exists: %s", args.Destination)
		}

		dstDir := filepath.Dir(dstAbs)
		if err := os.MkdirAll(dstDir, 0o750); err != nil {
			return "", fmt.Errorf("creating destination directory: %w", err)
		}

		if err := os.Rename(srcAbs, dstAbs); err != nil {
			return "", fmt.Errorf("moving %s to %s: %w", args.Source, args.Destination, err)
		}

		entryType := "file"
		if srcInfo.IsDir() {
			entryType = "directory"
		}

		result := moveResult{
			Source:      args.Source,
			Destination: args.Destination,
			Type:        entryType,
		}

		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(out), nil
	}
}
