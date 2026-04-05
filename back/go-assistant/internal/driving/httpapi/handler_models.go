package httpapi

import (
	"net/http"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/infra/openrouter"
)

// ModelRoleResponse describes a model role in the registry.
type ModelRoleResponse struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	DefaultModel string `json:"default_model"`
}

// ModelsResponse is the JSON response for GET /api/v1/models.
type ModelsResponse struct {
	DefaultModel string              `json:"default_model"`
	Roles        []ModelRoleResponse `json:"roles"`
}

// modelRoleLabels provides human-readable labels and descriptions for model roles.
var modelRoleLabels = map[string][2]string{
	"classifier":   {"Intent Classifier", "Fast model for routing user messages"},
	"structured":   {"Structured Output", "Model for JSON/structured responses"},
	"reasoning":    {"Reasoning", "Primary model for complex tasks"},
	"long-context": {"Long Context", "Model for large document processing"},
	"summarize":    {"Summarization", "Lightweight model for summaries"},
	"thinking":     {"Deep Thinking", "Most capable model for complex reasoning"},
}

// HandleGetModels returns the available model registry.
// GET /api/v1/models
func (h *Handlers) HandleGetModels(w http.ResponseWriter, r *http.Request) {
	roles := make([]ModelRoleResponse, 0, len(openrouter.DefaultModelRegistry))

	// Use a stable order matching modelRoleLabels.
	orderedKeys := []string{"classifier", "structured", "reasoning", "long-context", "summarize", "thinking"}
	for _, key := range orderedKeys {
		defaultModel, ok := openrouter.DefaultModelRegistry[key]
		if !ok {
			continue
		}
		labels := modelRoleLabels[key]
		roles = append(roles, ModelRoleResponse{
			Key:          key,
			Label:        labels[0],
			Description:  labels[1],
			DefaultModel: defaultModel,
		})
	}

	writeJSON(w, http.StatusOK, ModelsResponse{
		DefaultModel: "anthropic/claude-sonnet-4-6",
		Roles:        roles,
	})
}
