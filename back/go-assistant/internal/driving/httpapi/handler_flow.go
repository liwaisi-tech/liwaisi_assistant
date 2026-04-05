package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
)

// HandleListFlows returns all saved flows with stats.
// GET /api/v1/flows
func (h *Handlers) HandleListFlows(w http.ResponseWriter, r *http.Request) {
	if h.FlowRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "persistence not enabled")
		return
	}

	page, err := h.FlowRepo.List(r.Context(), &persist.FlowListOpts{Limit: 100})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list flows")
		return
	}

	items := make([]FlowSummaryResponse, 0, len(page.Items))
	for _, f := range page.Items {
		items = append(items, FlowSummaryResponse{
			Hash:           f.Hash,
			Role:           f.Role,
			ExecutionCount: f.Stats.ExecutionCount,
			SuccessRate:    f.Stats.SuccessRate,
			AvgCostUSD:     f.Stats.AvgCostUSD,
			AvgDurationMs:  f.Stats.AvgDurationMs,
			CreatedAt:      f.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			UpdatedAt:      f.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	writeJSON(w, http.StatusOK, FlowListResponse{
		Items:      items,
		HasMore:    page.HasMore,
		NextCursor: page.NextCursor,
	})
}

// HandleGetFlow returns a flow's full topology by hash.
// GET /api/v1/flows/{hash}
func (h *Handlers) HandleGetFlow(w http.ResponseWriter, r *http.Request) {
	if h.FlowRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "persistence not enabled")
		return
	}

	hash := r.PathValue("hash")
	if hash == "" {
		writeError(w, http.StatusBadRequest, "flow hash is required")
		return
	}

	flow, err := h.FlowRepo.GetByHash(r.Context(), hash)
	if err != nil {
		writeError(w, http.StatusNotFound, "flow not found")
		return
	}

	// Parse topology_json into a raw object for the response.
	var topology json.RawMessage
	if len(flow.TopologyJSON) > 0 {
		topology = flow.TopologyJSON
	}

	writeJSON(w, http.StatusOK, FlowDetailResponse{
		Hash:     flow.Hash,
		Role:     flow.Role,
		Topology: topology,
		Stats: FlowStatsResponse{
			ExecutionCount: flow.Stats.ExecutionCount,
			SuccessRate:    flow.Stats.SuccessRate,
			AvgCostUSD:     flow.Stats.AvgCostUSD,
			AvgDurationMs:  flow.Stats.AvgDurationMs,
		},
		CreatedAt: flow.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: flow.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

// HandleGetFlowExecutions returns execution records for a flow.
// GET /api/v1/flows/{hash}/executions
func (h *Handlers) HandleGetFlowExecutions(w http.ResponseWriter, r *http.Request) {
	if h.IntelRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "persistence not enabled")
		return
	}

	hash := r.PathValue("hash")
	if hash == "" {
		writeError(w, http.StatusBadRequest, "flow hash is required")
		return
	}

	// Get flow to find its role, then query executions by role.
	if h.FlowRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "persistence not enabled")
		return
	}
	flow, err := h.FlowRepo.GetByHash(r.Context(), hash)
	if err != nil {
		writeError(w, http.StatusNotFound, "flow not found")
		return
	}

	recs, err := h.IntelRepo.QueryByRole(r.Context(), flow.Role, flow.CreatedAt, flow.UpdatedAt.Add(365*24*3600e9))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query executions")
		return
	}

	items := make([]ExecutionRecordResponse, 0, len(recs))
	for _, rec := range recs {
		items = append(items, ExecutionRecordResponse{
			ID:               rec.ID,
			CPNID:            rec.CPNID,
			SessionID:        rec.SessionID,
			TransitionsFired: rec.TransitionsFired,
			LLMCalls:         rec.LLMCalls,
			ToolCalls:        rec.ToolCalls,
			TokensProduced:   rec.TokensProduced,
			TotalCostUSD:     rec.TotalCostUSD,
			DurationMs:       rec.DurationMs,
			Success:          rec.Success,
			StartedAt:        rec.StartedAt.Format("2006-01-02T15:04:05Z07:00"),
			CompletedAt:      rec.CompletedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	writeJSON(w, http.StatusOK, ExecutionListResponse{Items: items})
}

// HandleGetSessionExecution returns the execution trace for a session.
// GET /api/v1/sessions/{id}/execution
func (h *Handlers) HandleGetSessionExecution(w http.ResponseWriter, r *http.Request) {
	if h.EventRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "persistence not enabled")
		return
	}

	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session ID is required")
		return
	}

	page, err := h.EventRepo.QueryBySession(r.Context(), sessionID, &persist.EventQueryOpts{Limit: 500})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to query events")
		return
	}

	events := make([]EventResponse, 0, len(page.Items))
	for _, e := range page.Items {
		events = append(events, EventResponse{
			ID:             e.ID,
			Type:           e.Type,
			TransitionID:   e.TransitionID,
			TransitionKind: e.TransitionKind,
			CPNID:          e.CPNID,
			CPNDepth:       e.CPNDepth,
			CPNRole:        e.CPNRole,
			Payload:        e.Payload,
			TokenSnapshot:  e.TokenSnapshot,
			Timestamp:      e.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	writeJSON(w, http.StatusOK, SessionExecutionResponse{
		SessionID: sessionID,
		Events:    events,
	})
}
