package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// RegisterTeamTools registers evaluate_team and create_subagent tools
// on the given registrar.
func RegisterTeamTools(registrar tool.Registrar, svc input.SubAgentService) {
	registrar.Register(evaluateTeamDefinition(), evaluateTeamHandler(svc))
	registrar.Register(createSubagentDefinition(), createSubagentHandler(svc))
}

func evaluateTeamDefinition() valueobject.ToolDefinition {
	return valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "evaluate_team",
			Description: "Evaluate whether a task requires a multi-disciplinary team of subagents. " +
				"Returns an assessment with confidence score and suggested roles. " +
				"Use this before spawning multiple agents to avoid unnecessary complexity.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"task": {
						"type": "string",
						"description": "The task description to evaluate for team formation"
					}
				},
				"required": ["task"]
			}`),
		},
	}
}

func createSubagentDefinition() valueobject.ToolDefinition {
	return valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name: "create_subagent",
			Description: "Create and persist a new subagent definition. " +
				"The subagent is saved as a SUBAGENT.md file and becomes available via spawn_subagent. " +
				"Use this to create specialized agents for recurring tasks.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"name": {
						"type": "string",
						"description": "Unique name for the subagent (lowercase, hyphens, 3-50 chars)"
					},
					"description": {
						"type": "string",
						"description": "Brief description of what the subagent specializes in"
					},
					"instruction": {
						"type": "string",
						"description": "System instruction defining the subagent's role and behavior"
					},
					"model_tier": {
						"type": "string",
						"enum": ["fast", "balanced", "capable"],
						"description": "Model tier for the subagent (default: fast)"
					},
					"allowed_tools": {
						"type": "array",
						"items": {"type": "string"},
						"description": "Tools the subagent can use (omit for all tools)"
					},
					"denied_tools": {
						"type": "array",
						"items": {"type": "string"},
						"description": "Tools the subagent cannot use"
					},
					"max_turns": {
						"type": "integer",
						"description": "Maximum LLM turns (default: 10)"
					},
					"timeout": {
						"type": "string",
						"description": "Max execution time, e.g. '2m', '5m' (default: 2m)"
					}
				},
				"required": ["name", "description", "instruction"]
			}`),
		},
	}
}

type evaluateTeamArgs struct {
	Task string `json:"task"`
}

type createSubagentArgs struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Instruction  string   `json:"instruction"`
	ModelTier    string   `json:"model_tier,omitempty"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	DeniedTools  []string `json:"denied_tools,omitempty"`
	MaxTurns     int      `json:"max_turns,omitempty"`
	Timeout      string   `json:"timeout,omitempty"`
}

func evaluateTeamHandler(svc input.SubAgentService) tool.Handler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args evaluateTeamArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing evaluate_team arguments: %w", err)
		}
		if args.Task == "" {
			return "", fmt.Errorf("evaluate_team: task is required")
		}

		eval, err := svc.EvaluateTeam(ctx, args.Task)
		if err != nil {
			return "", fmt.Errorf("evaluate_team: %w", err)
		}

		return marshalJSON(eval)
	}
}

func createSubagentHandler(svc input.SubAgentService) tool.Handler {
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		var args createSubagentArgs
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", fmt.Errorf("parsing create_subagent arguments: %w", err)
		}

		if args.Name == "" {
			return "", fmt.Errorf("create_subagent: name is required")
		}
		if args.Description == "" {
			return "", fmt.Errorf("create_subagent: description is required")
		}
		if args.Instruction == "" {
			return "", fmt.Errorf("create_subagent: instruction is required")
		}

		spec := &entity.SubAgentSpec{
			Name:         args.Name,
			Description:  args.Description,
			Instruction:  args.Instruction,
			AllowedTools: args.AllowedTools,
			DeniedTools:  args.DeniedTools,
			MaxTurns:     args.MaxTurns,
		}

		if args.ModelTier != "" {
			spec.ModelTier = valueobject.ModelTier(args.ModelTier)
		}
		if args.Timeout != "" {
			d, err := time.ParseDuration(args.Timeout)
			if err != nil {
				return "", fmt.Errorf("create_subagent: invalid timeout %q: %w", args.Timeout, err)
			}
			spec.Timeout = d
		}

		if err := svc.CreateAgent(ctx, spec); err != nil {
			return "", fmt.Errorf("create_subagent: %w", err)
		}

		return marshalJSON(map[string]interface{}{
			"status":  "created",
			"name":    spec.Name,
			"message": fmt.Sprintf("Subagent %q created successfully. Use spawn_subagent to invoke it.", spec.Name),
		})
	}
}
