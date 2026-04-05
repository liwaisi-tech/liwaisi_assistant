package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

// ── Tools Response Types ────────────────────────────────────────────────────

// ToolListResponse is the JSON response for GET /api/v1/tools.
type ToolListResponse struct {
	Tools []ToolSummary `json:"tools"`
}

// ToolSummary is a tool in the list view.
type ToolSummary struct {
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Description  string `json:"description"`
	InputColor   string `json:"input_color"`
	OutputColor  string `json:"output_color"`
	RequiresHITL bool   `json:"requires_hitl"`
	Version      string `json:"version"`
}

// ToolDetailResponse is the full tool detail.
type ToolDetailResponse struct {
	ToolSummary
	Parameters json.RawMessage `json:"parameters,omitempty"`
}

// ── Handler ─────────────────────────────────────────────────────────────────

// HandleListTools returns all tools, optionally filtered by namespace.
// GET /api/v1/tools
func (h *Handlers) HandleListTools(w http.ResponseWriter, r *http.Request) {
	if h.ToolRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "tool registry not configured")
		return
	}

	namespace := r.URL.Query().Get("namespace")

	var summaries []ToolSummary
	if namespace != "" {
		schemas := h.ToolRegistry.List(namespace)
		summaries = make([]ToolSummary, 0, len(schemas))
		for _, s := range schemas {
			summaries = append(summaries, toolSchemaToSummary(s))
		}
	} else {
		schemas := h.ToolRegistry.ListAll()
		summaries = make([]ToolSummary, 0, len(schemas))
		for _, s := range schemas {
			summaries = append(summaries, toolSchemaToSummary(s))
		}
	}

	writeJSON(w, http.StatusOK, ToolListResponse{Tools: summaries})
}

// HandleGetTool returns a single tool by qualified name.
// GET /api/v1/tools/{name...}
func (h *Handlers) HandleGetTool(w http.ResponseWriter, r *http.Request) {
	if h.ToolRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "tool registry not configured")
		return
	}

	name := r.PathValue("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "tool name is required")
		return
	}

	entry, ok := h.ToolRegistry.Resolve(name)
	if !ok {
		writeError(w, http.StatusNotFound, "tool not found")
		return
	}

	writeJSON(w, http.StatusOK, ToolDetailResponse{
		ToolSummary: toolSchemaToSummary(entry.Schema),
		Parameters:  entry.Schema.Parameters,
	})
}

// ── Internal helpers ────────────────────────────────────────────────────────

func toolSchemaToSummary(s *tools.ToolSchema) ToolSummary {
	return ToolSummary{
		Name:         s.QualifiedName(),
		Namespace:    s.Namespace,
		Description:  s.Description,
		InputColor:   string(s.InputColor),
		OutputColor:  string(s.OutputColor),
		RequiresHITL: s.RequiresHITL,
		Version:      s.Version,
	}
}
