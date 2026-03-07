// Package subagent provides the spawn_subagent and list_subagents meta-tools
// for delegating tasks to specialized subagents.
package subagent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// SessionIDProvider returns the current session ID for subagent spawning.
// This is needed because tool handlers don't receive session context.
type SessionIDProvider func() string

// RegisterSubAgentTools registers spawn_subagent, list_subagents,
// evaluate_team, and create_subagent tools on the given registrar.
// The sessionIDProvider supplies the parent session ID when
// spawn_subagent is invoked.
func RegisterSubAgentTools(
	registrar tool.Registrar,
	svc input.SubAgentService,
	sessionIDProvider SessionIDProvider,
) {
	registrar.Register(spawnSubagentDefinition(), spawnSubagentHandler(svc, sessionIDProvider))
	registrar.Register(listSubagentsDefinition(), listSubagentsHandler(svc))
	RegisterTeamTools(registrar, svc)
}

func spawnSubagentDefinition() valueobject.ToolDefinition {
	return valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "spawn_subagent",
			Description: "Delegate a task to a specialized subagent. " +
				"The subagent runs autonomously with its own tools and returns the result. " +
				"Use list_subagents first to see available agents and their capabilities.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"agent_name": {
						"type": "string",
						"description": "Name of the subagent to spawn (from list_subagents)"
					},
					"task": {
						"type": "string",
						"description": "The specific task to delegate to the subagent"
					},
					"context": {
						"type": "string",
						"description": "Additional context or data for the subagent (optional)"
					}
				},
				"required": ["agent_name", "task"]
			}`),
		},
	}
}

func listSubagentsDefinition() valueobject.ToolDefinition {
	return valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "list_subagents",
			Description: "List all available subagents with their names, descriptions, and capabilities. " +
				"Call this before spawn_subagent to discover which specialist agents are available.",
			Parameters: json.RawMessage(`{"type": "object", "properties": {}}`),
		},
	}
}

type spawnArgs struct {
	AgentName string `json:"agent_name"`
	Task      string `json:"task"`
	Context   string `json:"context"`
}

type spawnResult struct {
	AgentName string `json:"agent_name"`
	Output    string `json:"output"`
	Status    string `json:"status"`
	ElapsedMs int64  `json:"elapsed_ms"`
	Tokens    int    `json:"tokens"`
}

type listEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ModelTier   string `json:"model_tier,omitempty"`
	MaxTurns    int    `json:"max_turns"`
}

type listResult struct {
	Agents []listEntry `json:"agents"`
	Count  int         `json:"count"`
}

func spawnSubagentHandler(svc input.SubAgentService, sessionIDProvider SessionIDProvider) tool.Handler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args spawnArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing spawn_subagent arguments: %w", err)
		}

		if args.AgentName == "" {
			return "", fmt.Errorf("spawn_subagent: agent_name is required")
		}
		if args.Task == "" {
			return "", fmt.Errorf("spawn_subagent: task is required")
		}

		spec, ok := svc.GetAgent(args.AgentName)
		if !ok {
			available := svc.ListAgents()
			names := make([]string, len(available))
			for i := range available {
				names[i] = available[i].Name
			}
			return marshalJSON(map[string]interface{}{
				"error":            fmt.Sprintf("unknown subagent %q", args.AgentName),
				"available_agents": names,
			})
		}

		if args.Context != "" {
			spec.Context = args.Context
		}

		parentSessionID := ""
		if sessionIDProvider != nil {
			parentSessionID = sessionIDProvider()
		}

		result, err := svc.Spawn(ctx, parentSessionID, &spec, args.Task)
		if err != nil {
			return "", fmt.Errorf("spawn_subagent: %w", err)
		}

		if result.Err != nil {
			return marshalJSON(spawnResult{
				AgentName: result.AgentName,
				Output:    result.Err.Error(),
				Status:    string(result.Status),
				ElapsedMs: result.Elapsed.Milliseconds(),
				Tokens:    result.Usage.TotalTokens,
			})
		}

		return marshalJSON(spawnResult{
			AgentName: result.AgentName,
			Output:    result.Output,
			Status:    string(result.Status),
			ElapsedMs: result.Elapsed.Milliseconds(),
			Tokens:    result.Usage.TotalTokens,
		})
	}
}

func listSubagentsHandler(svc input.SubAgentService) tool.Handler {
	return func(_ context.Context, _ json.RawMessage) (string, error) {
		agents := svc.ListAgents()
		entries := make([]listEntry, len(agents))
		for i := range agents {
			entries[i] = listEntry{
				Name:        agents[i].Name,
				Description: agents[i].Description,
				ModelTier:   string(agents[i].ModelTier),
				MaxTurns:    agents[i].EffectiveMaxTurns(),
			}
		}
		return marshalJSON(listResult{Agents: entries, Count: len(entries)})
	}
}

func marshalJSON(v interface{}) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshaling result: %w", err)
	}
	return string(data), nil
}
