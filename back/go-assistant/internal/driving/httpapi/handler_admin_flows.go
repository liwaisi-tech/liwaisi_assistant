package httpapi

// Admin agent-authored-flow REST API (GAP-4).
//
// Spec: spec/spec-architecture-cpn-synthesis-instantiate.md §3 REQ-060..062.
//
// All endpoints are wired behind AdminMiddleware in routes.go. They let
// an operator audit every CPN-authored topology and reject a specific
// flow_id so future NodeKindInstantiate transitions fail fast.

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

type adminAuthoredFlowResponse struct {
	FlowID                 string          `json:"flow_id"`
	Summary                string          `json:"summary,omitempty"`
	SizePlaces             int             `json:"size_places"`
	SizeTransitions        int             `json:"size_transitions"`
	Rejected               bool            `json:"rejected"`
	RejectedReason         string          `json:"rejected_reason,omitempty"`
	SafeLintPassed         bool            `json:"safe_lint_passed"`
	AuthoredByCPNID        string          `json:"authored_by_cpn_id,omitempty"`
	AuthoredFromPromptHash string          `json:"authored_from_prompt_digest,omitempty"`
	SessionID              string          `json:"session_id,omitempty"`
	Topology               json.RawMessage `json:"topology,omitempty"`
}

type adminAuthoredFlowsListResponse struct {
	Items []adminAuthoredFlowResponse `json:"items"`
	Total int                         `json:"total"`
}

type adminRejectFlowRequest struct {
	Reason string `json:"reason"`
}

func toAuthoredFlowResponse(rec *cpn.AuthoredFlowRecord, includeTopology bool) adminAuthoredFlowResponse {
	resp := adminAuthoredFlowResponse{
		FlowID:                 rec.FlowID,
		Summary:                rec.Summary,
		SizePlaces:             rec.SizePlaces,
		SizeTransitions:        rec.SizeTransitions,
		Rejected:               rec.Rejected,
		RejectedReason:         rec.RejectedReason,
		SafeLintPassed:         rec.SafeLintPassed,
		AuthoredByCPNID:        rec.Provenance.AuthoredByCPNID,
		AuthoredFromPromptHash: rec.Provenance.AuthoredFromPromptHash,
		SessionID:              rec.Provenance.SessionID,
	}
	if includeTopology {
		resp.Topology = rec.TopologyJSON
	}
	return resp
}

// HandleAdminListAuthoredFlows returns every agent-authored flow.
// GET /api/v1/admin/flows?origin=agent-authored
func (h *Handlers) HandleAdminListAuthoredFlows(w http.ResponseWriter, r *http.Request) {
	if h.AuthoredFlows == nil {
		writeError(w, http.StatusServiceUnavailable, "authored flow repository not configured")
		return
	}
	origin := r.URL.Query().Get("origin")
	if origin != "" && origin != "agent-authored" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "unsupported origin filter")
		return
	}
	limit := 200
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			limit = n
		}
	}
	flows, err := h.AuthoredFlows.ListAuthored(r.Context(), limit)
	if err != nil {
		h.Logger.Error("admin list authored flows", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	items := make([]adminAuthoredFlowResponse, 0, len(flows))
	for _, rec := range flows {
		items = append(items, toAuthoredFlowResponse(rec, false))
	}
	writeJSON(w, http.StatusOK, adminAuthoredFlowsListResponse{Items: items, Total: len(items)})
}

// HandleAdminGetAuthoredFlow returns the topology for a single flow_id.
// GET /api/v1/admin/flows/{id}
func (h *Handlers) HandleAdminGetAuthoredFlow(w http.ResponseWriter, r *http.Request) {
	if h.AuthoredFlows == nil {
		writeError(w, http.StatusServiceUnavailable, "authored flow repository not configured")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "flow id required")
		return
	}
	rec, err := h.AuthoredFlows.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "flow not found")
		return
	}
	writeJSON(w, http.StatusOK, toAuthoredFlowResponse(rec, true))
}

// HandleAdminRejectAuthoredFlow flips the rejected flag.
// POST /api/v1/admin/flows/{id}/reject  body {reason}
func (h *Handlers) HandleAdminRejectAuthoredFlow(w http.ResponseWriter, r *http.Request) {
	if h.AuthoredFlows == nil {
		writeError(w, http.StatusServiceUnavailable, "authored flow repository not configured")
		return
	}
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "flow id required")
		return
	}
	var req adminRejectFlowRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", err.Error())
		return
	}
	if strings.TrimSpace(req.Reason) == "" {
		writeErrorCode(w, http.StatusBadRequest, "invalid_input", "reason is required")
		return
	}
	if err := h.AuthoredFlows.Reject(r.Context(), id, req.Reason); err != nil {
		writeError(w, http.StatusNotFound, "flow not found")
		return
	}
	h.Logger.Info("authored flow rejected", "flow_id", id, "by", currentAdminEmail(r))
	rec, err := h.AuthoredFlows.GetByID(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"flow_id": id, "rejected": true})
		return
	}
	writeJSON(w, http.StatusOK, toAuthoredFlowResponse(rec, false))
}
