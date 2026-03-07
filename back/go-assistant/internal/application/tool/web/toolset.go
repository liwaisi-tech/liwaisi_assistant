package web

import (
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// ToolSet groups web content tools for batch registration.
type ToolSet struct {
	pipeline *Pipeline
}

// NewToolSet creates a web ToolSet with default configuration.
// Use Option values to override defaults.
func NewToolSet(opts ...Option) *ToolSet {
	cfg := DefaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	return &ToolSet{pipeline: NewPipeline(cfg)}
}

// CatalogEntry returns the catalog metadata and factory for the web tool
// category, enabling JIT registration into an ActiveRegistry.
func (ts *ToolSet) CatalogEntry() *tool.CatalogEntry {
	return &tool.CatalogEntry{
		Info: valueobject.ToolCategoryInfo{
			Category:    valueobject.ToolCategoryWeb,
			Description: "Web content fetching: fetch a URL and return clean Markdown. Great for reading documentation, READMEs, articles, and API references.",
			Tools: []valueobject.ToolSummary{
				{Name: "web_fetch", Description: "Fetch a URL and return clean Markdown content"},
			},
			EstTokenCost: 250,
		},
		Factory: func(r *tool.Registry) { ts.Register(r) },
	}
}

// Register adds all web tools to the registry.
func (ts *ToolSet) Register(registry *tool.Registry) {
	registerWebFetch(registry, ts.pipeline)
}
