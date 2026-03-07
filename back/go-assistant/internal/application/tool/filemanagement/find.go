package filemanagement

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

const (
	maxFindResults = 1000
	maxFindDepth   = 20
	findTypeFile   = "file"
	findTypeDir    = "directory"
	findTypeAny    = "any"
)

type findArgs struct {
	Pattern  string `json:"pattern"`
	Path     string `json:"path"`
	Type     string `json:"type"`
	MaxDepth int    `json:"max_depth"`
}

type findEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size,omitempty"`
}

type findSummary struct {
	TotalMatches    int  `json:"total_matches"`
	SearchedDirs    int  `json:"searched_dirs"`
	MaxDepthReached bool `json:"max_depth_reached"`
	Truncated       bool `json:"truncated"`
}

type findResult struct {
	Pattern string      `json:"pattern"`
	Path    string      `json:"path"`
	Results []findEntry `json:"results"`
	Summary findSummary `json:"summary"`
}

func registerFind(registry *tool.Registry, sandbox *Sandbox) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "find",
			Description: "Search for files and directories within the workspace by name pattern and type. Uses " +
				"optimized recursive traversal for efficient directory scanning. Supports glob patterns " +
				"(e.g., '*.go', 'test_*', 'Makefile'). Use this when you need to discover files matching a " +
				"pattern across the directory tree. For flat directory listing, use list_directory instead. " +
				"Maximum 1,000 results.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"pattern": {
						"type": "string",
						"description": "Glob pattern to match file/directory names. Uses standard glob syntax: '*' matches any sequence of characters, '?' matches any single character, '[abc]' matches character class. Example: '*.go', 'test_*', 'Makefile'"
					},
					"path": {
						"type": "string",
						"description": "Relative path to the root directory for the search. Defaults to workspace root if empty. Example: 'src/'"
					},
					"type": {
						"type": "string",
						"enum": ["file", "directory", "any"],
						"description": "Filter results by type. 'file' returns only files, 'directory' returns only directories, 'any' returns both. Default: 'any'"
					},
					"max_depth": {
						"type": "integer",
						"description": "Maximum directory depth to traverse (1-20). Depth 1 searches only the root directory. Omit or set to 0 for unlimited depth.",
						"minimum": 0,
						"maximum": 20
					}
				},
				"required": ["pattern"]
			}`),
		},
	}

	registry.Register(def, findHandler(sandbox))
}

func findHandler(sandbox *Sandbox) tool.Handler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args findArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing find arguments: %w", err)
		}

		if args.Pattern == "" {
			return "", fmt.Errorf("pattern is required")
		}

		if _, err := filepath.Match(args.Pattern, ""); err != nil {
			return "", fmt.Errorf("invalid glob pattern %q: %w", args.Pattern, err)
		}

		searchPath := args.Path
		if searchPath == "" {
			searchPath = "."
		}

		filterType := args.Type
		if filterType == "" {
			filterType = findTypeAny
		}
		if filterType != findTypeFile && filterType != findTypeDir && filterType != findTypeAny {
			return "", fmt.Errorf("invalid type %q, must be one of: file, directory, any", filterType)
		}

		maxDepth := args.MaxDepth
		if maxDepth < 0 {
			maxDepth = 0
		}
		if maxDepth > maxFindDepth {
			maxDepth = maxFindDepth
		}

		abs, err := sandbox.Resolve(searchPath)
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

		var entries []findEntry
		var searchedDirs int
		var maxDepthReached bool
		truncated := false

		walkErr := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return fs.SkipAll
			}
			if err != nil {
				return nil
			}

			rel, relErr := filepath.Rel(abs, path)
			if relErr != nil {
				return nil
			}

			if rel == "." {
				searchedDirs++
				return nil
			}

			depth := pathDepth(rel)

			if d.IsDir() {
				searchedDirs++
				if maxDepth > 0 && depth >= maxDepth {
					maxDepthReached = true
					matched, _ := filepath.Match(args.Pattern, d.Name())
					if matched && (filterType == findTypeDir || filterType == findTypeAny) {
						if len(entries) >= maxFindResults {
							truncated = true
							return fs.SkipAll
						}
						entries = append(entries, findEntry{
							Path: buildFindRelPath(searchPath, rel),
							Type: findTypeDir,
						})
					}
					return fs.SkipDir
				}
			}

			matched, _ := filepath.Match(args.Pattern, d.Name())
			if !matched {
				return nil
			}

			entryType := findTypeFile
			if d.IsDir() {
				entryType = findTypeDir
			}

			if filterType != findTypeAny && filterType != entryType {
				return nil
			}

			if len(entries) >= maxFindResults {
				truncated = true
				return fs.SkipAll
			}

			entry := findEntry{
				Path: buildFindRelPath(searchPath, rel),
				Type: entryType,
			}

			if !d.IsDir() {
				if fi, fiErr := d.Info(); fiErr == nil {
					entry.Size = fi.Size()
				}
			}

			entries = append(entries, entry)
			return nil
		})

		if walkErr != nil {
			return "", fmt.Errorf("walking directory: %w", walkErr)
		}

		sort.Slice(entries, func(i, j int) bool {
			di := entries[i].Type == findTypeDir
			dj := entries[j].Type == findTypeDir
			if di != dj {
				return di
			}
			return entries[i].Path < entries[j].Path
		})

		if entries == nil {
			entries = []findEntry{}
		}

		result := findResult{
			Pattern: args.Pattern,
			Path:    args.Path,
			Results: entries,
			Summary: findSummary{
				TotalMatches:    len(entries),
				SearchedDirs:    searchedDirs,
				MaxDepthReached: maxDepthReached,
				Truncated:       truncated,
			},
		}

		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(out), nil
	}
}

func pathDepth(rel string) int {
	if rel == "." {
		return 0
	}
	depth := 1
	for _, c := range rel {
		if c == filepath.Separator {
			depth++
		}
	}
	return depth
}

func buildFindRelPath(searchRoot, rel string) string {
	if searchRoot == "" || searchRoot == "." {
		return rel
	}
	return filepath.Join(searchRoot, rel)
}
