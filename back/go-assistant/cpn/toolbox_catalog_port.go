package cpn

import "context"

// ToolboxCatalogPort is a read-only adapter the synthesize transition uses
// to render a prompt-ready catalogue of the host's toolboxes and their
// tools. The concrete implementation lives in cpn/tools so the cpn package
// stays free of the full Registry surface (Axiom A13).
//
// Filter.ToolNames, when non-empty, scopes the rendered catalogue to tools
// whose qualified-or-bare names match. Empty filter = render every toolbox.
//
// The rendered block is appended to the synthesize system prompt by
// buildSynthesizePrompt so the LLM can reference tool IDs when authoring a
// sub-CPN topology.
type ToolboxCatalogPort interface {
	RenderCatalogue(ctx context.Context, filter ToolboxCatalogFilter) string
}

// ToolboxCatalogFilter narrows the rendered catalogue. Zero value asks
// the adapter to render everything it has.
type ToolboxCatalogFilter struct {
	// ToolNames, when non-empty, restricts rendered tools to those with
	// a matching qualified name (namespace/name) or bare name. Toolboxes
	// with no surviving member tool MUST be omitted.
	ToolNames []string
}
