package filemanagement

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

const (
	maxReadFilesPaths    = 20
	readFilesConcurrency = 10
)

type readFilesArgs struct {
	Paths []string `json:"paths"`
}

type readFilesPathResult struct {
	Path    string               `json:"path"`
	Type    string               `json:"type"`
	Content string               `json:"content,omitempty"`
	Size    int64                `json:"size,omitempty"`
	Entries []listDirectoryEntry `json:"entries,omitempty"`
	Total   int                  `json:"total,omitempty"`
	Error   string               `json:"error,omitempty"`
}

type readFilesSummary struct {
	Total      int `json:"total"`
	FilesRead  int `json:"files_read"`
	DirsListed int `json:"dirs_listed"`
	Errors     int `json:"errors"`
}

type readFilesResult struct {
	Results []readFilesPathResult `json:"results"`
	Summary readFilesSummary      `json:"summary"`
}

func registerReadFiles(registry *tool.Registry, sandbox *Sandbox) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "read_files",
			Description: "Read multiple files concurrently within the workspace. Accepts a list of relative paths " +
				"and returns the content of each. If a path points to a directory, returns a listing of its " +
				"contents instead of an error — this allows sending mixed paths without knowing in advance " +
				"whether each is a file or directory. Maximum 20 paths per call. Maximum 1MB per file.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"paths": {
						"type": "array",
						"items": { "type": "string" },
						"description": "List of relative paths to read within the workspace. Paths can be files or directories. Example: ['go.mod', 'cmd/cli/main.go', 'internal/']",
						"minItems": 1,
						"maxItems": 20
					}
				},
				"required": ["paths"]
			}`),
		},
	}

	registry.Register(def, readFilesHandler(sandbox))
}

func readFilesHandler(sandbox *Sandbox) tool.Handler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args readFilesArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing read_files arguments: %w", err)
		}

		if len(args.Paths) == 0 {
			return "", fmt.Errorf("at least one path is required")
		}
		if len(args.Paths) > maxReadFilesPaths {
			return "", fmt.Errorf("too many paths (%d), maximum is %d", len(args.Paths), maxReadFilesPaths)
		}

		results := make([]readFilesPathResult, len(args.Paths))
		sem := make(chan struct{}, readFilesConcurrency)
		var wg sync.WaitGroup

		for i, p := range args.Paths {
			wg.Add(1)
			go func(idx int, path string) {
				defer wg.Done()

				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					results[idx] = readFilesPathResult{
						Path:  path,
						Error: "context canceled",
					}
					return
				}

				results[idx] = readSinglePath(sandbox, path)
			}(i, p)
		}

		wg.Wait()

		var filesRead, dirsListed, errors int
		for _, r := range results {
			switch {
			case r.Error != "":
				errors++
			case r.Type == "file":
				filesRead++
			case r.Type == "directory":
				dirsListed++
			}
		}

		res := readFilesResult{
			Results: results,
			Summary: readFilesSummary{
				Total:      len(args.Paths),
				FilesRead:  filesRead,
				DirsListed: dirsListed,
				Errors:     errors,
			},
		}

		out, err := json.Marshal(res)
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(out), nil
	}
}

func readSinglePath(sandbox *Sandbox, path string) readFilesPathResult {
	abs, err := sandbox.Resolve(path)
	if err != nil {
		return readFilesPathResult{Path: path, Error: err.Error()}
	}

	if sandbox.IsSensitiveFile(abs) {
		return readFilesPathResult{
			Path:  path,
			Error: fmt.Sprintf("access denied: %q matches a sensitive file pattern. Use 'liwaisi env list' to manage credentials", path),
		}
	}

	info, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return readFilesPathResult{Path: path, Error: fmt.Sprintf("not found: %s", path)}
		}
		return readFilesPathResult{Path: path, Error: fmt.Sprintf("accessing path: %v", err)}
	}

	if info.IsDir() {
		return readDirEntries(path, abs)
	}

	return readFileContent(path, abs, info)
}

func readFileContent(path, abs string, info os.FileInfo) readFilesPathResult {
	if info.Size() > maxReadSize {
		return readFilesPathResult{
			Path:  path,
			Type:  "file",
			Error: fmt.Sprintf("file too large (%d bytes, maximum is %d bytes)", info.Size(), maxReadSize),
		}
	}

	data, err := os.ReadFile(abs)
	if err != nil {
		return readFilesPathResult{Path: path, Type: "file", Error: fmt.Sprintf("reading file: %v", err)}
	}

	return readFilesPathResult{
		Path:    path,
		Type:    "file",
		Content: string(data),
		Size:    info.Size(),
	}
}

func readDirEntries(path, abs string) readFilesPathResult {
	dirEntries, err := os.ReadDir(abs)
	if err != nil {
		return readFilesPathResult{Path: path, Type: "directory", Error: fmt.Sprintf("reading directory: %v", err)}
	}

	total := len(dirEntries)
	if total > maxListEntries {
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
		if fi, fiErr := de.Info(); fiErr == nil {
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

	return readFilesPathResult{
		Path:    path,
		Type:    "directory",
		Entries: entries,
		Total:   total,
	}
}
