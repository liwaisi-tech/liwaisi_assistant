package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/architect"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
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

// HandleCreateFlow runs the deterministic Architect Planner against a
// user-supplied intent and returns a FlowDraftResponse. This is the explicit
// "create flow" entry point — independent of a chat message — so the
// frontend can let users forge new CPN topologies from the Flujos page.
//
// POST /api/v1/flows
func (h *Handlers) HandleCreateFlow(w http.ResponseWriter, r *http.Request) {
	if h.FlowPlanner == nil {
		writeError(w, http.StatusServiceUnavailable, "flow planner not enabled")
		return
	}

	var req CreateFlowRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Intent = strings.TrimSpace(req.Intent)
	if req.Intent == "" && len(req.Hashtags) == 0 && len(req.RequiredCaps) == 0 {
		writeError(w, http.StatusBadRequest, "intent, hashtags, or required_caps must be set")
		return
	}

	archReq := architect.ArchitectRequest{
		NL:              req.Intent,
		Hashtags:        req.Hashtags,
		RequiredCaps:    req.RequiredCaps,
		InputColors:     req.InputColors,
		ParallelismHint: req.ParallelismHint,
	}

	draft, err := h.FlowPlanner.Plan(r.Context(), archReq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "planner failed: "+err.Error())
		return
	}

	resp := FlowDraftResponse{
		Strategy:    string(draft.Strategy),
		BaseFlowID:  draft.BaseFlowID,
		MissingCaps: draft.MissingCaps,
		Reason:      draft.Reason,
		Confidence:  draft.Confidence,
	}
	if len(draft.TopologyBlob) > 0 {
		resp.Topology = json.RawMessage(draft.TopologyBlob)
	}
	if len(draft.MatchSet.Matches) > 0 {
		resp.Matches = make([]ToolMatchResponse, 0, len(draft.MatchSet.Matches))
		for _, m := range draft.MatchSet.Matches {
			resp.Matches = append(resp.Matches, ToolMatchResponse{
				QualifiedName: m.QualifiedName,
				Score:         m.Score,
				Hashtags:      m.Hashtags,
			})
		}
	}
	if len(draft.Candidates) > 0 {
		resp.Candidates = make([]FlowCandidateResponse, 0, len(draft.Candidates))
		for _, c := range draft.Candidates {
			resp.Candidates = append(resp.Candidates, candidateToResponse(c))
		}
	}

	// Always 200: Reject / ToolForge are legitimate draft outcomes that the
	// frontend renders with a tailored UI. HTTP-error status codes are
	// reserved for transport / server faults.
	writeJSON(w, http.StatusOK, resp)
}

func candidateToResponse(c *cpn.FlowLibraryEntry) FlowCandidateResponse {
	if c == nil {
		return FlowCandidateResponse{}
	}
	role := ""
	if c.CPN != nil {
		role = c.CPN.Role
	}
	return FlowCandidateResponse{
		Hash:     c.Hash,
		Role:     role,
		Hashtags: c.Signature.Hashtags,
	}
}

// HandleRunFlow starts a new session bound to a library topology and
// dispatches the user's intent as the first message.
//
// POST /api/v1/flows/{hash}/run
//
// Body: {"intent": "..."} — forwarded as the first user message so the CPN's
// p-input place receives it like a regular chat turn.
//
// Response: {"session_id": "...", "role": "tool-atelier", "started_at": "..."}.
// The caller subscribes to SSE on the returned session_id for live events.
func (h *Handlers) HandleRunFlow(w http.ResponseWriter, r *http.Request) {
	if h.FlowRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "persistence not enabled")
		return
	}
	if len(h.FlowBuilders) == 0 {
		writeError(w, http.StatusServiceUnavailable, "flow builders not enabled")
		return
	}

	hash := r.PathValue("hash")
	if hash == "" {
		writeError(w, http.StatusBadRequest, "flow hash is required")
		return
	}

	var req RunFlowRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Intent = strings.TrimSpace(req.Intent)
	if req.Intent == "" {
		writeError(w, http.StatusBadRequest, "intent is required")
		return
	}

	flow, err := h.FlowRepo.GetByHash(r.Context(), hash)
	if err != nil {
		writeError(w, http.StatusNotFound, "flow not found")
		return
	}

	factory, ok := h.FlowBuilders[flow.Role]
	if !ok {
		writeError(w, http.StatusBadRequest, "no builder registered for role "+flow.Role)
		return
	}

	userID := ""
	if user := auth.UserFromContext(r.Context()); user != nil {
		userID = user.Sub
	}
	if userID == "" {
		userID = "dev-user"
	}

	info, err := h.App.CreateSessionWithFactory(r.Context(), userID, cpn.ChannelWeb, factory)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create session: "+err.Error())
		return
	}

	if err := h.App.SendMessage(r.Context(), info.ID, req.Intent); err != nil {
		writeError(w, http.StatusInternalServerError, "send intent: "+err.Error())
		return
	}

	writeJSON(w, http.StatusAccepted, RunFlowResponse{
		SessionID: info.ID,
		Role:      flow.Role,
		StartedAt: time.Now().UTC().Format(time.RFC3339),
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
