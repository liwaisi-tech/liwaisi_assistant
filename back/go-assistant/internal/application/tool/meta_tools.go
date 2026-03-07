package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// RegisterFindToolsOnActive registers the find_tools meta-tool directly on an
// ActiveRegistry. This is the typical entry point for JIT mode bootstrap.
func RegisterFindToolsOnActive(active *ActiveRegistry, catalog *Catalog) {
	def := findToolsDefinition()
	active.Register(def, findToolsHandler(catalog, active))
}

func findToolsDefinition() valueobject.ToolDefinition {
	return valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "find_tools",
			Description: "Discover and load tool categories on demand. " +
				"Call with action='list' to see all available tool categories with descriptions. " +
				"Call with action='load' and category='<name>' to load a specific category's tools into your active toolset. " +
				"Tools are organized in categories by domain: filemanagement (file read/write/search), " +
				"shellexec (command execution), web (URL fetching), env (environment variables). " +
				"Once loaded, tools from that category become available for the rest of this conversation. " +
				"Loading is idempotent — loading an already-loaded category is free.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"action": {
						"type": "string",
						"enum": ["list", "load"],
						"description": "Action to perform: 'list' shows available categories, 'load' activates a category"
					},
					"category": {
						"type": "string",
						"description": "Category name to load (required when action='load'). One of: filemanagement, shellexec, web, env"
					}
				},
				"required": ["action"]
			}`),
		},
	}
}

type findToolsArgs struct {
	Action   string `json:"action"`
	Category string `json:"category"`
}

type findToolsListEntry struct {
	Category     string                    `json:"category"`
	Description  string                    `json:"description"`
	Tools        []valueobject.ToolSummary `json:"tools"`
	EstTokenCost int                       `json:"estimated_token_cost"`
	Loaded       bool                      `json:"loaded"`
}

type findToolsListResult struct {
	Categories []findToolsListEntry `json:"categories"`
	Count      int                  `json:"count"`
}

type findToolsLoadResult struct {
	Category    string                    `json:"category"`
	Status      string                    `json:"status"`
	ToolsLoaded []valueobject.ToolSummary `json:"tools_loaded"`
	Message     string                    `json:"message"`
}

func findToolsHandler(catalog *Catalog, active *ActiveRegistry) Handler {
	return func(_ context.Context, raw json.RawMessage) (string, error) {
		var args findToolsArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing find_tools arguments: %w", err)
		}

		switch args.Action {
		case "list":
			return handleFindToolsList(catalog, active)
		case "load":
			return handleFindToolsLoad(catalog, active, args.Category)
		default:
			return "", fmt.Errorf("find_tools: unknown action %q, expected 'list' or 'load'", args.Action)
		}
	}
}

func handleFindToolsList(catalog *Catalog, active *ActiveRegistry) (string, error) {
	infos := catalog.List()
	entries := make([]findToolsListEntry, len(infos))
	for i, info := range infos {
		entries[i] = findToolsListEntry{
			Category:     info.Category.String(),
			Description:  info.Description,
			Tools:        info.Tools,
			EstTokenCost: info.EstTokenCost,
			Loaded:       active.IsLoaded(info.Category),
		}
	}

	result, err := json.Marshal(findToolsListResult{Categories: entries, Count: len(entries)})
	if err != nil {
		return "", fmt.Errorf("marshaling result: %w", err)
	}
	return string(result), nil
}

func handleFindToolsLoad(_ *Catalog, active *ActiveRegistry, category string) (string, error) {
	if category == "" {
		return "", fmt.Errorf("find_tools: 'category' parameter is required when action='load'")
	}

	cat := valueobject.ToolCategory(category)
	info, err := active.LoadCategory(cat)
	if err != nil {
		return "", fmt.Errorf("find_tools: %w", err)
	}

	status := "loaded"
	msg := fmt.Sprintf("Category '%s' loaded. %d tools are now available.", category, len(info.Tools))

	result, err := json.Marshal(findToolsLoadResult{
		Category:    category,
		Status:      status,
		ToolsLoaded: info.Tools,
		Message:     msg,
	})
	if err != nil {
		return "", fmt.Errorf("marshaling result: %w", err)
	}
	return string(result), nil
}
