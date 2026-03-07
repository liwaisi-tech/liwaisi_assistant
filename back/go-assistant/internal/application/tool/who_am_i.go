package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// AgentInfo holds the agent's self-description.
type AgentInfo struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Description  string   `json:"description"`
	Model        string   `json:"model"`
	Capabilities []string `json:"capabilities"`
	CreatedBy    string   `json:"created_by"`
}

// RegisterWhoAmI registers the who_am_i tool in the given registrar.
func RegisterWhoAmI(registry Registrar, info *AgentInfo) {
	def := valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name:        "who_am_i",
			Description: "Returns a structured description of the agent, including its name, version, capabilities, and model. Use this tool when the user asks about the agent's identity, capabilities, or wants to know who they are talking to.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{},"required":[]}`),
		},
	}

	handler := func(_ context.Context, _ json.RawMessage) (string, error) {
		result, err := json.Marshal(info)
		if err != nil {
			return "", fmt.Errorf("marshaling agent info: %w", err)
		}
		return string(result), nil
	}

	registry.Register(def, handler)
}
