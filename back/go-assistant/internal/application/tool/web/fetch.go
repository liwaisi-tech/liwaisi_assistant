package web

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

type webFetchArgs struct {
	URL string `json:"url"`
	Raw bool   `json:"raw"`
}

// registerWebFetch adds the web_fetch tool to the registry.
func registerWebFetch(registry *tool.Registry, pipeline *Pipeline) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "web_fetch",
			Description: "Fetch a web page by URL and return its content converted to clean Markdown. " +
				"Returns the HTTP status code and the page content extracted using Mozilla's Readability " +
				"algorithm (same as Firefox Reader Mode) and converted to Markdown. Use this to read " +
				"documentation, articles, READMEs, API references, blog posts, or any web page. " +
				"The content is automatically cleaned of navigation, ads, scripts, and other non-content " +
				"elements to maximize readability. Only HTTP and HTTPS URLs are supported. " +
				"JavaScript-rendered single-page applications may return incomplete content — for those, " +
				"the page must serve server-rendered HTML. Maximum response body size is 5MB. " +
				"Request timeout is 30 seconds.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"url": {
						"type": "string",
						"description": "The fully-qualified URL to fetch. Must start with http:// or https://. Example: 'https://go.dev/doc/effective_go'"
					},
					"raw": {
						"type": "boolean",
						"description": "If true, skip Readability content extraction and convert the full page body to Markdown. Useful for index pages, listing pages, dashboards, or non-article layouts. Defaults to false."
					}
				},
				"required": ["url"]
			}`),
		},
	}

	registry.Register(def, webFetchHandler(pipeline))
}

// webFetchHandler returns a tool.Handler that fetches a URL and returns
// the content as structured JSON with Markdown.
func webFetchHandler(pipeline *Pipeline) tool.Handler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args webFetchArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing web_fetch arguments: %w", err)
		}

		result, err := pipeline.Fetch(ctx, args.URL, args.Raw)
		if err != nil {
			return "", err
		}

		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshaling result: %w", err)
		}
		return string(out), nil
	}
}
