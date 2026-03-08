package osnative

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// ToolSet provides native OS capabilities (Unix/Linux commands).
type ToolSet struct {
	workspaceRoot string
	interceptor   *SecurityInterceptor
}

// NewToolSet creates a new osnative ToolSet bounded by the workspace path.
func NewToolSet(workspaceRoot string) (*ToolSet, error) {
	if workspaceRoot == "" {
		return nil, fmt.Errorf("workspaceRoot cannot be empty")
	}

	return &ToolSet{
		workspaceRoot: workspaceRoot,
		interceptor:   NewSecurityInterceptor(workspaceRoot),
	}, nil
}

// CatalogEntry returns the catalog metadata for JIT registration.
func (ts *ToolSet) CatalogEntry() *tool.CatalogEntry {
	return &tool.CatalogEntry{
		Info: valueobject.ToolCategoryInfo{
			Category:    valueobject.ToolCategory("osnative"),
			Description: "OS-Native commands for a proficient Linux/Unix sysadmin (find, grep, awk, ps, ping, etc.). Bounded to workspace.",
			Tools: []valueobject.ToolSummary{
				{Name: "os_native_command", Description: "Run a native OS command (no sudo allowed)"},
			},
			EstTokenCost: 150,
		},
		Factory: func(r *tool.Registry) { ts.Register(r) },
	}
}

// Register adds the osnative commands to the registry.
func (ts *ToolSet) Register(registry *tool.Registry) {
	registry.Register(
		valueobject.ToolDefinition{
			Type: "function",
			Function: valueobject.FunctionDefinition{
				Name:        "os_native_command",
				Description: "Run native Linux/Unix commands safely within the workspace. Common usages: 'grep', 'find', 'awk', 'sed', 'ps', 'ping', 'curl'. Strictly bounded to ethical sysadmin practices; sudo or privilege escalation is blocked.",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"command": {
							"type": "string",
							"description": "The base command to run (e.g., 'grep', 'find', 'ps')"
						},
						"args": {
							"type": "array",
							"items": { "type": "string" },
							"description": "List of arguments to pass to the command"
						}
					},
					"required": ["command", "args"]
				}`),
			},
		},
		ts.handleOSNativeCommand,
	)
}

func (ts *ToolSet) handleOSNativeCommand(ctx context.Context, args json.RawMessage) (string, error) {
	var input struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return "", fmt.Errorf("invalid arguments parsing error: %w", err)
	}

	command := strings.TrimSpace(input.Command)
	if command == "" {
		return "", fmt.Errorf("command cannot be empty")
	}

	output, err := ts.interceptor.Execute(command, input.Args, ts.workspaceRoot)
	if err != nil {
		if len(output) > 0 {
			return "", fmt.Errorf("command failed: %v\nOutput: %s", err, string(output))
		}
		return "", fmt.Errorf("command failed: %v", err)
	}

	return string(output), nil
}
