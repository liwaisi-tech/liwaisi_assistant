package filemanagement

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

const (
	maxDeletePaths    = 50
	deleteConcurrency = 10
)

type deleteArgs struct {
	Paths []string `json:"paths"`
	Force bool     `json:"force"`
}

type deletePathResult struct {
	Path    string `json:"path"`
	Deleted bool   `json:"deleted"`
	Type    string `json:"type"`
	Error   string `json:"error,omitempty"`
}

type deleteSummary struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

type deleteResult struct {
	Results []deletePathResult `json:"results"`
	Summary deleteSummary      `json:"summary"`
}

func registerDelete(registry *tool.Registry, sandbox *Sandbox) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "delete",
			Description: "Delete one or more files or directories within the workspace. Accepts a list of relative " +
				"paths and processes each deletion concurrently. Uses best-effort semantics: if some paths fail " +
				"to delete (e.g., not found, permission error), the tool continues deleting the remaining paths " +
				"and reports per-path results. For directories, set force to true to delete non-empty directories " +
				"recursively. Without force, only empty directories can be deleted. Maximum 50 paths per call.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"paths": {
						"type": "array",
						"items": { "type": "string" },
						"description": "List of relative paths to delete within the workspace. Example: ['build/output.bin', 'tmp/', 'old_config.yaml']",
						"minItems": 1,
						"maxItems": 50
					},
					"force": {
						"type": "boolean",
						"description": "If true, delete non-empty directories recursively (like rm -rf). If false (default), only empty directories can be deleted. Files are always deleted regardless of this flag.",
						"default": false
					}
				},
				"required": ["paths"]
			}`),
		},
	}

	registry.Register(def, deleteHandler(sandbox))
}

func deleteHandler(sandbox *Sandbox) tool.Handler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args deleteArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing delete arguments: %w", err)
		}

		if len(args.Paths) == 0 {
			return "", fmt.Errorf("at least one path is required")
		}
		if len(args.Paths) > maxDeletePaths {
			return "", fmt.Errorf("too many paths (%d), maximum is %d", len(args.Paths), maxDeletePaths)
		}

		results := make([]deletePathResult, len(args.Paths))
		sem := make(chan struct{}, deleteConcurrency)
		var wg sync.WaitGroup

		for i, p := range args.Paths {
			wg.Add(1)
			go func(idx int, path string) {
				defer wg.Done()

				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					results[idx] = deletePathResult{
						Path:  path,
						Error: "context canceled",
					}
					return
				}

				results[idx] = deletePath(sandbox, path, args.Force)
			}(i, p)
		}

		wg.Wait()

		var succeeded, failed int
		for _, r := range results {
			if r.Deleted {
				succeeded++
			} else {
				failed++
			}
		}

		res := deleteResult{
			Results: results,
			Summary: deleteSummary{
				Total:     len(args.Paths),
				Succeeded: succeeded,
				Failed:    failed,
			},
		}

		out, err := json.Marshal(res)
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(out), nil
	}
}

func deletePath(sandbox *Sandbox, path string, force bool) deletePathResult {
	abs, err := sandbox.Resolve(path)
	if err != nil {
		return deletePathResult{Path: path, Error: err.Error()}
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return deletePathResult{Path: path, Error: fmt.Sprintf("not found: %s", path)}
		}
		return deletePathResult{Path: path, Error: fmt.Sprintf("accessing path: %v", err)}
	}

	if info.IsDir() {
		return deleteDirectory(path, abs, force)
	}

	if err := os.Remove(abs); err != nil {
		return deletePathResult{Path: path, Type: "file", Error: fmt.Sprintf("deleting file: %v", err)}
	}
	return deletePathResult{Path: path, Deleted: true, Type: "file"}
}

func deleteDirectory(path, abs string, force bool) deletePathResult {
	if force {
		if err := os.RemoveAll(abs); err != nil {
			return deletePathResult{Path: path, Type: "directory", Error: fmt.Sprintf("deleting directory: %v", err)}
		}
		return deletePathResult{Path: path, Deleted: true, Type: "directory"}
	}

	if err := os.Remove(abs); err != nil {
		return deletePathResult{
			Path:  path,
			Type:  "directory",
			Error: "directory not empty, use force=true to delete recursively",
		}
	}
	return deletePathResult{Path: path, Deleted: true, Type: "directory"}
}
