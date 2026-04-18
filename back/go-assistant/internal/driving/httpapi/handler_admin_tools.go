package httpapi

// Admin tool-registry REST API (GAP-3).
//
// Spec: spec-architecture-dynamic-tool-registry.md §3 REQ-040.
//
// All endpoints are wired behind AdminMiddleware in routes.go. Error codes
// mirror the cpn/tools sentinel codes so the admin UI can branch
// deterministically:
//   CodeDuplicate     → 409 duplicate
//   CodeNotFound      → 404 tool_not_found
//   CodeSchemaInvalid → 400 schema_invalid
//   CodeForbidden     → 409 forbidden
//   CodeInvalidInput  → 400 invalid_input

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
)

// ── Response types ───────────────────────────────────────────────────────

type adminToolResponse struct {
	ID                string             `json:"id"`
	Namespace         string             `json:"namespace"`
	Name              string             `json:"name"`
	Version           string             `json:"version"`
	QualifiedName     string             `json:"qualified_name"`
	Schema            json.RawMessage    `json:"schema,omitempty"`
	HelpText          string             `json:"help_text,omitempty"`
	ManPage           string             `json:"man_page,omitempty"`
	BinaryPath        string             `json:"binary_path,omitempty"`
	BinarySHA256      string             `json:"binary_sha256,omitempty"`
	Origin            string             `json:"origin"`
	Provenance        persist.Provenance `json:"provenance"`
	RegisteredAt      string             `json:"registered_at"`
	RegisteredBy      string             `json:"registered_by"`
	Deprecated        bool               `json:"deprecated"`
	DeprecatedAt      string             `json:"deprecated_at,omitempty"`
	DeprecationReason string             `json:"deprecation_reason,omitempty"`
}

type adminToolListResponse struct {
	Items []adminToolResponse `json:"items"`
	Total int                 `json:"total"`
}

type adminDeprecateRequest struct {
	QualifiedName string `json:"qualified_name"`
	Reason        string `json:"reason"`
}

func toolEntryResponse(e *tools.ToolEntry) adminToolResponse {
	if e == nil {
		return adminToolResponse{}
	}
	out := adminToolResponse{
		ID:            e.ID,
		Namespace:     e.Namespace,
		Name:          e.Name,
		Version:       e.Version,
		QualifiedName: e.QualifiedName(),
		Schema:        e.JSONSchema,
		HelpText:      e.HelpText,
		ManPage:       e.ManPage,
		BinaryPath:    e.BinaryPath,
		BinarySHA256:  e.BinarySHA256,
		Origin:        e.Origin,
		Provenance:    e.Provenance,
		RegisteredAt:  e.RegisteredAt.UTC().Format(time.RFC3339Nano),
		RegisteredBy:  e.RegisteredBy,
		Deprecated:    e.Deprecated,
	}
	if !e.DeprecatedAt.IsZero() {
		out.DeprecatedAt = e.DeprecatedAt.UTC().Format(time.RFC3339Nano)
	}
	out.DeprecationReason = e.DeprecationReason
	return out
}

// ── Handlers ─────────────────────────────────────────────────────────────

// HandleAdminListTools returns every registered tool, optionally filtered.
// GET /api/v1/admin/tools?origin=&namespace=&deprecated=
func (h *Handlers) HandleAdminListTools(w http.ResponseWriter, r *http.Request) {
	if h.ToolRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "tool registry not configured")
		return
	}

	q := r.URL.Query()
	filter := tools.ToolFilter{
		Origin:    q.Get("origin"),
		Namespace: q.Get("namespace"),
	}
	if dep := q.Get("deprecated"); dep != "" {
		b, err := strconv.ParseBool(dep)
		if err != nil {
			writeErrorCode(w, http.StatusBadRequest, "invalid_input", "deprecated must be true|false")
			return
		}
		filter.Deprecated = &b
	}

	entries := h.ToolRegistry.ListFiltered(r.Context(), filter)
	items := make([]adminToolResponse, 0, len(entries))
	for _, e := range entries {
		items = append(items, toolEntryResponse(e))
	}
	writeJSON(w, http.StatusOK, adminToolListResponse{
		Items: items,
		Total: len(items),
	})
}

// HandleAdminGetTool returns a single tool by qualified name (ns/name or
// ns/name@ver). The path uses a single wildcard {qn...}.
// GET /api/v1/admin/tools/{qn...}
func (h *Handlers) HandleAdminGetTool(w http.ResponseWriter, r *http.Request) {
	if h.ToolRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "tool registry not configured")
		return
	}
	qn := strings.TrimPrefix(r.PathValue("qn"), "/")
	if qn == "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "qualified name is required")
		return
	}
	entry, err := h.ToolRegistry.Get(r.Context(), qn)
	if err != nil {
		writeToolError(h, w, "admin get tool", qn, err)
		return
	}
	writeJSON(w, http.StatusOK, toolEntryResponse(entry))
}

// HandleAdminDeprecateTool marks a specific (ns/name@ver) as deprecated.
// POST /api/v1/admin/tools/deprecate  — body {qualified_name, reason}
func (h *Handlers) HandleAdminDeprecateTool(w http.ResponseWriter, r *http.Request) {
	if h.ToolRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "tool registry not configured")
		return
	}
	var req adminDeprecateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	qn := req.QualifiedName
	if qn == "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "qualified_name is required")
		return
	}
	if err := h.ToolRegistry.Deprecate(r.Context(), qn, req.Reason); err != nil {
		writeToolError(h, w, "admin deprecate tool", qn, err)
		return
	}
	entry, err := h.ToolRegistry.Get(r.Context(), qn)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"qualified_name": qn, "deprecated": true})
		return
	}
	h.Logger.Info("tool deprecated", "qualified_name", qn, "by", currentAdminEmail(r))
	writeJSON(w, http.StatusOK, toolEntryResponse(entry))
}

// HandleAdminDeleteTool hard-deletes an agent-authored tool.
// DELETE /api/v1/admin/tools/{qn...}
func (h *Handlers) HandleAdminDeleteTool(w http.ResponseWriter, r *http.Request) {
	if h.ToolRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "tool registry not configured")
		return
	}
	qn := strings.TrimPrefix(r.PathValue("qn"), "/")
	if qn == "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "qualified name is required")
		return
	}
	if err := h.ToolRegistry.Unregister(r.Context(), qn); err != nil {
		writeToolError(h, w, "admin delete tool", qn, err)
		return
	}
	h.Logger.Info("tool deleted", "qualified_name", qn, "by", currentAdminEmail(r))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "qualified_name": qn})
}

// writeToolError translates tools.RegistryError codes into HTTP responses.
func writeToolError(h *Handlers, w http.ResponseWriter, op, qn string, err error) {
	var regErr *tools.RegistryError
	if errors.As(err, &regErr) {
		switch regErr.Code {
		case tools.CodeNotFound:
			writeErrorCode(w, http.StatusNotFound, "tool_not_found", regErr.Error())
			return
		case tools.CodeDuplicate:
			writeErrorCode(w, http.StatusConflict, "duplicate", regErr.Error())
			return
		case tools.CodeSchemaInvalid:
			writeErrorCode(w, http.StatusBadRequest, "schema_invalid", regErr.Error())
			return
		case tools.CodeForbidden:
			writeErrorCode(w, http.StatusConflict, "forbidden", regErr.Error())
			return
		case tools.CodeInvalidInput:
			writeErrorCode(w, http.StatusBadRequest, "invalid_input", regErr.Error())
			return
		}
	}
	h.Logger.Error(op, "qualified_name", qn, "error", err)
	writeError(w, http.StatusInternalServerError, "internal error")
}
