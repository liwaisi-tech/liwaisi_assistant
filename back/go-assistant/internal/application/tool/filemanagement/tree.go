package filemanagement

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

const (
	maxTreeEntries  = 10_000
	maxTreeDepth    = 10
	minTreeDepth    = 1
	treeConcurrency = 10
)

type treeArgs struct {
	Path  string `json:"path"`
	Depth int    `json:"depth"`
}

type treeNode struct {
	Name            string      `json:"name"`
	Type            string      `json:"type"`
	Size            int64       `json:"size,omitempty"`
	Children        []*treeNode `json:"children"`
	ChildrenOmitted bool        `json:"children_omitted,omitempty"`
}

type treeSummary struct {
	TotalFiles   int  `json:"total_files"`
	TotalDirs    int  `json:"total_dirs"`
	DepthReached int  `json:"depth_reached"`
	Truncated    bool `json:"truncated"`
}

type treeResult struct {
	Path    string      `json:"path"`
	Tree    *treeNode   `json:"tree"`
	Summary treeSummary `json:"summary"`
}

func registerTree(registry *tool.Registry, sandbox *Sandbox) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "tree",
			Description: "Generate a recursive directory tree for a path within the workspace. Returns a hierarchical " +
				"JSON structure showing files and directories up to the specified depth. Use this to understand project " +
				"structure before navigating or modifying files. The depth parameter controls how many levels deep to " +
				"traverse — use lower values (1-2) for large directories to avoid expensive traversal of folders like " +
				"node_modules/, .git/, or vendor/. Subdirectories are read concurrently for fast results. Maximum " +
				"10,000 total entries.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {
						"type": "string",
						"description": "Relative path to the root directory for the tree. Defaults to workspace root if empty. Example: 'my-project/src'"
					},
					"depth": {
						"type": "integer",
						"description": "Maximum depth to traverse (1-10). Depth 1 shows only immediate children. Depth 2 shows children and grandchildren. Use lower values for large or unknown directories. Example: 3",
						"minimum": 1,
						"maximum": 10
					}
				},
				"required": ["depth"]
			}`),
		},
	}

	registry.Register(def, treeHandler(sandbox))
}

func clampDepth(d int) int {
	if d < minTreeDepth {
		return minTreeDepth
	}
	if d > maxTreeDepth {
		return maxTreeDepth
	}
	return d
}

func treeHandler(sandbox *Sandbox) tool.Handler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args treeArgs
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &args); err != nil {
				return "", fmt.Errorf("parsing tree arguments: %w", err)
			}
		}

		path := args.Path
		if path == "" {
			path = "."
		}
		depth := clampDepth(args.Depth)

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

		w := &treeWalker{
			sandbox: sandbox,
			sem:     make(chan struct{}, treeConcurrency),
		}

		root := w.walk(ctx, abs, filepath.Base(abs), depth, 1)

		totalFiles := int(w.totalFiles.Load())
		totalDirs := int(w.totalDirs.Load())
		truncated := w.truncated.Load()
		depthReached := int(w.maxDepthSeen.Load())

		result := treeResult{
			Path: args.Path,
			Tree: root,
			Summary: treeSummary{
				TotalFiles:   totalFiles,
				TotalDirs:    totalDirs,
				DepthReached: depthReached,
				Truncated:    truncated,
			},
		}

		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(out), nil
	}
}

type treeWalker struct {
	sandbox      *Sandbox
	sem          chan struct{}
	entryCount   atomic.Int32
	totalFiles   atomic.Int32
	totalDirs    atomic.Int32
	truncated    atomic.Bool
	maxDepthSeen atomic.Int32
}

func (w *treeWalker) updateMaxDepth(d int32) {
	for {
		cur := w.maxDepthSeen.Load()
		if d <= cur {
			return
		}
		if w.maxDepthSeen.CompareAndSwap(cur, d) {
			return
		}
	}
}

func (w *treeWalker) walk(ctx context.Context, absPath, name string, maxDepth, currentDepth int) *treeNode {
	if ctx.Err() != nil {
		return &treeNode{Name: name, Type: "directory", ChildrenOmitted: true}
	}

	w.updateMaxDepth(int32(currentDepth)) //nolint:gosec // currentDepth is bounded by maxTreeDepth (10)

	entries, err := os.ReadDir(absPath)
	if err != nil {
		return &treeNode{Name: name, Type: "directory", ChildrenOmitted: true}
	}

	sort.Slice(entries, func(i, j int) bool {
		di, dj := entries[i].IsDir(), entries[j].IsDir()
		if di != dj {
			return di
		}
		return entries[i].Name() < entries[j].Name()
	})

	children := make([]*treeNode, 0, len(entries))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, entry := range entries {
		if w.truncated.Load() || ctx.Err() != nil {
			break
		}

		if w.entryCount.Add(1) > int32(maxTreeEntries) {
			w.truncated.Store(true)
			break
		}

		if entry.IsDir() {
			w.totalDirs.Add(1)
			childPath := filepath.Join(absPath, entry.Name())

			if currentDepth >= maxDepth {
				mu.Lock()
				children = append(children, &treeNode{
					Name:            entry.Name(),
					Type:            "directory",
					ChildrenOmitted: true,
				})
				mu.Unlock()
				continue
			}

			wg.Add(1)
			go func(e os.DirEntry, cp string) {
				defer wg.Done()

				select {
				case w.sem <- struct{}{}:
					defer func() { <-w.sem }()
				case <-ctx.Done():
					mu.Lock()
					children = append(children, &treeNode{
						Name:            e.Name(),
						Type:            "directory",
						ChildrenOmitted: true,
					})
					mu.Unlock()
					return
				}

				child := w.walk(ctx, cp, e.Name(), maxDepth, currentDepth+1)
				mu.Lock()
				children = append(children, child)
				mu.Unlock()
			}(entry, childPath)
		} else {
			w.totalFiles.Add(1)
			var size int64
			if fi, fiErr := entry.Info(); fiErr == nil {
				size = fi.Size()
			}
			mu.Lock()
			children = append(children, &treeNode{
				Name: entry.Name(),
				Type: "file",
				Size: size,
			})
			mu.Unlock()
		}
	}

	wg.Wait()

	sort.Slice(children, func(i, j int) bool {
		di := children[i].Type == "directory"
		dj := children[j].Type == "directory"
		if di != dj {
			return di
		}
		return children[i].Name < children[j].Name
	})

	if children == nil {
		children = []*treeNode{}
	}

	return &treeNode{
		Name:     name,
		Type:     "directory",
		Children: children,
	}
}
