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
//
// Field layout (REQ-OBS-002, spec-architecture-model-selection-centralization.md):
//   - Default          — new, spec-mandated, top-level "default" key that the
//                        frontend reads to seed the onboarding wizard. Always
//                        equals openrouter.PRODUCT_DEFAULT_MODEL.
//   - DefaultModel     — legacy top-level alias of Default, retained so existing
//                        frontends that already parse `default_model` keep
//                        working through the transition.
//   - AvailableModels  — the curated catalog exposed in the Settings picker.
//   - Roles            — per-role default, surfaced for the per-role override
//                        table in Settings.
type ModelsResponse struct {
	Default         string              `json:"default"`
	DefaultModel    string              `json:"default_model"`
	AvailableModels []string            `json:"available_models"`
	Roles           []ModelRoleResponse `json:"roles"`
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
		Default:         openrouter.PRODUCT_DEFAULT_MODEL,
		DefaultModel:    openrouter.PRODUCT_DEFAULT_MODEL,
		AvailableModels: openrouter.AvailableModels,
		Roles:           roles,
	})
}
