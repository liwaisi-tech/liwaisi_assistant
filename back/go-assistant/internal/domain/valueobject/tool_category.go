package valueobject

// ToolCategory identifies a group of related tools that are loaded together.
// Validity is determined by the Catalog at runtime — the catalog is the single
// source of truth for which categories exist.
type ToolCategory string

const (
	// ToolCategoryFileManagement groups file-system tools (read, write, search, navigate).
	ToolCategoryFileManagement ToolCategory = "filemanagement"
	// ToolCategoryShellExec groups command execution tools.
	ToolCategoryShellExec ToolCategory = "shellexec"
	// ToolCategoryWeb groups web-fetching tools.
	ToolCategoryWeb ToolCategory = "web"
	// ToolCategoryEnv groups environment variable tools.
	ToolCategoryEnv ToolCategory = "env"
)

// String returns the string representation of the category.
func (c ToolCategory) String() string {
	return string(c)
}
