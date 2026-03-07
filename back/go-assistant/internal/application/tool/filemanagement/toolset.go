package filemanagement

import (
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// ToolSet groups file management tools for the agent workspace.
type ToolSet struct {
	sandbox *Sandbox
}

// NewToolSet creates a file management ToolSet sandboxed to workspaceRoot.
// Returns an error if workspaceRoot is not an absolute path.
func NewToolSet(workspaceRoot string) (*ToolSet, error) {
	sb, err := NewSandbox(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("creating file management sandbox: %w", err)
	}
	return &ToolSet{sandbox: sb}, nil
}

// CatalogEntry returns the catalog metadata and factory for the file management
// tool category, enabling JIT registration into an ActiveRegistry.
func (ts *ToolSet) CatalogEntry() *tool.CatalogEntry {
	return &tool.CatalogEntry{
		Info: valueobject.ToolCategoryInfo{
			Category:    valueobject.ToolCategoryFileManagement,
			Description: "File system operations: read, write, search, navigate, and manage files and directories within the workspace.",
			Tools: []valueobject.ToolSummary{
				{Name: "read_file", Description: "Read the contents of a single file"},
				{Name: "read_files", Description: "Read multiple files or directories at once"},
				{Name: "write_file", Description: "Create or overwrite a file with content"},
				{Name: "create_directory", Description: "Create a new directory"},
				{Name: "list_directory", Description: "List files and subdirectories in a directory"},
				{Name: "tree", Description: "Display directory structure as a tree"},
				{Name: "move", Description: "Move files or directories to a new location"},
				{Name: "rename", Description: "Rename a file or directory in place"},
				{Name: "delete", Description: "Delete files or directories (supports batch)"},
				{Name: "find", Description: "Search for files by name pattern"},
				{Name: "grep", Description: "Search for content inside files"},
			},
			EstTokenCost: 3000,
		},
		Factory: func(r *tool.Registry) { ts.Register(r) },
	}
}

// Register adds all file management tools to the registry.
func (ts *ToolSet) Register(registry *tool.Registry) {
	registerReadFile(registry, ts.sandbox)
	registerWriteFile(registry, ts.sandbox)
	registerCreateDirectory(registry, ts.sandbox)
	registerListDirectory(registry, ts.sandbox)
	registerTree(registry, ts.sandbox)
	registerMove(registry, ts.sandbox)
	registerRename(registry, ts.sandbox)
	registerDelete(registry, ts.sandbox)
	registerReadFiles(registry, ts.sandbox)
	registerFind(registry, ts.sandbox)
	registerGrep(registry, ts.sandbox)
}
