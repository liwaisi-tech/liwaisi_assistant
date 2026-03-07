package valueobject

// ToolSummary provides a one-line description of a tool within a category.
type ToolSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ToolCategoryInfo holds the catalog metadata for a tool category.
// It provides enough information for the LLM to decide whether to load the
// category, without exposing full JSON schemas.
type ToolCategoryInfo struct {
	Category     ToolCategory  `json:"category"`
	Description  string        `json:"description"`
	Tools        []ToolSummary `json:"tools"`
	EstTokenCost int           `json:"estimated_token_cost"`
}
