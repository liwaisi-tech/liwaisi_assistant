package httpapi

import (
	"net/http"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
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
//   - Default          — top-level "default" key that the frontend reads to
//                        seed the onboarding wizard. When a ModelRegistry is
//                        wired this equals the product-default row's registry_id;
//                        otherwise it falls back to openrouter.PRODUCT_DEFAULT_MODEL.
//   - DefaultModel     — legacy top-level alias of Default, retained so existing
//                        frontends that already parse `default_model` keep working.
//   - AvailableModels  — backwards-compatible flat slice of registry_ids (invokable
//                        only), for the existing Settings/onboarding picker.
//   - Roles            — per-role default, surfaced for the per-role override table.
//   - Registry         — when the registry is wired, the full entry list so the
//                        frontend can render capability badges + pricing without
//                        round-trips to /admin/models. Never nil; empty slice
//                        when the feature is disabled.
type ModelsResponse struct {
	Default         string                       `json:"default"`
	DefaultModel    string                       `json:"default_model"`
	AvailableModels []string                     `json:"available_models"`
	Roles           []ModelRoleResponse          `json:"roles"`
	Registry        []ModelRegistryEntryResponse `json:"registry"`
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

// HandleGetModels returns the available model registry. When h.ModelRegistry
// is wired, it reads from Postgres; otherwise it falls back to the compile-time
// constants in infra/openrouter (legacy path, preserved for dev-mode boot).
// GET /api/v1/models
func (h *Handlers) HandleGetModels(w http.ResponseWriter, r *http.Request) {
	if h.ModelRegistry == nil {
		h.writeLegacyModelsResponse(w)
		return
	}

	ctx := r.Context()

	entries, err := h.ModelRegistry.ListInvokable(ctx)
	if err != nil {
		h.Logger.Error("models: list invokable failed, falling back", "error", err)
		h.writeLegacyModelsResponse(w)
		return
	}

	def, err := h.ModelRegistry.GetProductDefault(ctx)
	if err != nil {
		h.Logger.Error("models: get product default failed, falling back", "error", err)
		h.writeLegacyModelsResponse(w)
		return
	}

	available := make([]string, 0, len(entries))
	registry := make([]ModelRegistryEntryResponse, 0, len(entries))
	for _, e := range entries {
		available = append(available, e.RegistryID)
		registry = append(registry, modelRegistryEntryResponse(e))
	}

	roles := make([]ModelRoleResponse, 0, len(cpn.CanonicalRoles))
	for _, role := range cpn.CanonicalRoles {
		labels, ok := modelRoleLabels[role]
		if !ok {
			continue
		}
		roleDefault := def.RegistryID
		if id, err := h.ModelRegistry.GetRoleDefault(ctx, role); err == nil && id != "" {
			roleDefault = id
		}
		roles = append(roles, ModelRoleResponse{
			Key:          role,
			Label:        labels[0],
			Description:  labels[1],
			DefaultModel: roleDefault,
		})
	}

	writeJSON(w, http.StatusOK, ModelsResponse{
		Default:         def.RegistryID,
		DefaultModel:    def.RegistryID,
		AvailableModels: available,
		Roles:           roles,
		Registry:        registry,
	})
}

// writeLegacyModelsResponse emits the pre-registry response shape so existing
// frontends boot cleanly when the registry feature is disabled (e.g. dev-mode
// without migrations applied).
func (h *Handlers) writeLegacyModelsResponse(w http.ResponseWriter) {
	roles := make([]ModelRoleResponse, 0, len(openrouter.DefaultModelRegistry))
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
		Registry:        []ModelRegistryEntryResponse{},
	})
}
