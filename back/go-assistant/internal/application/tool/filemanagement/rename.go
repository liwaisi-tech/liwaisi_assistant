package filemanagement

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

type renameArgs struct {
	Path    string `json:"path"`
	NewName string `json:"new_name"`
}

type renameResult struct {
	OldPath string `json:"old_path"`
	NewPath string `json:"new_path"`
	Type    string `json:"type"`
}

func registerRename(registry *tool.Registry, sandbox *Sandbox) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "rename",
			Description: "Rename a file or directory in place within the workspace. The item stays in the same " +
				"parent directory — only its name changes. The new_name must be a simple name without path " +
				"separators (no '/' characters). Use this when you need to change a file or directory name " +
				"without moving it. Cannot overwrite existing names — move or remove the conflicting item first.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Relative path to the file or directory to rename. Example: 'src/old_name.go'"
					},
					"new_name": {
						"type": "string",
						"description": "The new name for the file or directory. Must not contain '/' characters. Example: 'new_name.go'"
					}
				},
				"required": ["path", "new_name"]
			}`),
		},
	}

	registry.Register(def, renameHandler(sandbox))
}

func renameHandler(sandbox *Sandbox) tool.Handler {
	return func(_ context.Context, raw json.RawMessage) (string, error) {
		var args renameArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing rename arguments: %w", err)
		}

		if args.Path == "" {
			return "", fmt.Errorf("path is required")
		}
		if args.NewName == "" {
			return "", fmt.Errorf("new_name is required")
		}
		if strings.Contains(args.NewName, "/") {
			return "", fmt.Errorf("new_name must not contain path separators (got %q)", args.NewName)
		}

		srcAbs, err := sandbox.Resolve(args.Path)
		if err != nil {
			return "", err
		}

		srcInfo, err := os.Stat(srcAbs)
		if err != nil {
			if os.IsNotExist(err) {
				return "", fmt.Errorf("source not found: %s", args.Path)
			}
			return "", fmt.Errorf("accessing source: %w", err)
		}

		newAbs := filepath.Join(filepath.Dir(srcAbs), args.NewName)

		newRel, err := filepath.Rel(sandbox.Root(), newAbs)
		if err != nil {
			return "", fmt.Errorf("computing new relative path: %w", err)
		}
		if _, err := sandbox.Resolve(newRel); err != nil {
			return "", err
		}

		if _, err := os.Stat(newAbs); err == nil {
			return "", fmt.Errorf("target name already exists: %s", args.NewName)
		}

		if err := os.Rename(srcAbs, newAbs); err != nil {
			return "", fmt.Errorf("renaming %s to %s: %w", args.Path, args.NewName, err)
		}

		entryType := "file"
		if srcInfo.IsDir() {
			entryType = "directory"
		}

		oldRel, _ := filepath.Rel(sandbox.Root(), srcAbs)
		newRelPath, _ := filepath.Rel(sandbox.Root(), newAbs)

		result := renameResult{
			OldPath: oldRel,
			NewPath: newRelPath,
			Type:    entryType,
		}

		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(out), nil
	}
}
