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
	Name         string   `json:"name"`
	Namespace    string   `json:"namespace"`
	Description  string   `json:"description"`
	InputColor   string   `json:"input_color"`
	OutputColor  string   `json:"output_color"`
	RequiresHITL bool     `json:"requires_hitl"`
	Version      string   `json:"version"`
	Toolbox      string   `json:"toolbox"`
	Hashtags     []string `json:"hashtags"`
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

	entries := h.ToolRegistry.ListFiltered(r.Context(), tools.ToolFilter{Namespace: namespace})
	summaries := make([]ToolSummary, 0, len(entries))
	for _, e := range entries {
		if e == nil || e.Schema == nil {
			continue
		}
		summaries = append(summaries, toolEntryToSummary(e))
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
		ToolSummary: toolEntryToSummary(entry),
		Parameters:  entry.Schema.Parameters,
	})
}

// ── Internal helpers ────────────────────────────────────────────────────────

func toolEntryToSummary(e *tools.ToolEntry) ToolSummary {
	s := e.Schema
	toolbox := e.Toolbox
	if toolbox == "" {
		toolbox = s.Namespace
	}
	tags := append([]string{}, e.Hashtags...)
	return ToolSummary{
		Name:         s.QualifiedName(),
		Namespace:    s.Namespace,
		Description:  s.Description,
		InputColor:   string(s.InputColor),
		OutputColor:  string(s.OutputColor),
		RequiresHITL: s.RequiresHITL,
		Version:      s.Version,
		Toolbox:      toolbox,
		Hashtags:     tags,
	}
}
