package httpapi

import (
	"encoding/json"
	"net/http"
)

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeErrorCode writes a JSON error response with a stable machine-readable
// error code alongside the human-readable message. Used for client-facing
// error classifications the frontend branches on (e.g.
// ONBOARDING_MODEL_REQUIRED from REQ-ONB-003).
func writeErrorCode(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]string{"error": msg, "code": code})
}

// SessionResponse is the JSON response for session operations.
type SessionResponse struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Channel   string `json:"channel"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
}

// SessionDetailResponse includes messages in addition to session info.
type SessionDetailResponse struct {
	SessionResponse
	Messages []MessageResponse `json:"messages"`
}

// MessageResponse is a single message in the session history.
// ParentMessageID, when present, links a HITL response row to the earlier
// A2UI surface it answers so the frontend can lock the questionnaire
// component on rehydration (REQ-006/REQ-101
// — spec-process-bugfix-a2ui-hitl-response-persistence.md).
type MessageResponse struct {
	ID              string `json:"id"`
	Role            string `json:"role"`
	Content         string `json:"content"`
	CPNID           string `json:"cpn_id,omitempty"`
	CPNRole         string `json:"cpn_role,omitempty"`
	Timestamp       string `json:"timestamp"`
	ParentMessageID string `json:"parent_message_id,omitempty"`
}

// StatusResponse is a simple status response.
type StatusResponse struct {
	Status string `json:"status"`
}

// HealthResponse is the health check response.
type HealthResponse struct {
	Status string `json:"status"`
}

// VersionResponse is the version info response.
type VersionResponse struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Built   string `json:"built"`
}

// ErrorResponse is the error response format.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ── Flow & Execution Responses ─────────────────────────────────────────────

// FlowSummaryResponse is a flow in a list view.
type FlowSummaryResponse struct {
	Hash           string  `json:"hash"`
	Role           string  `json:"role"`
	ExecutionCount int64   `json:"execution_count"`
	SuccessRate    float64 `json:"success_rate"`
	AvgCostUSD     float64 `json:"avg_cost_usd"`
	AvgDurationMs  int64   `json:"avg_duration_ms"`
	CreatedAt      string  `json:"created_at"`
	UpdatedAt      string  `json:"updated_at"`
}

// FlowListResponse is the paginated list of flows.
type FlowListResponse struct {
	Items      []FlowSummaryResponse `json:"items"`
	HasMore    bool                  `json:"has_more"`
	NextCursor string                `json:"next_cursor"`
}

// FlowStatsResponse holds execution statistics.
type FlowStatsResponse struct {
	ExecutionCount int64   `json:"execution_count"`
	SuccessRate    float64 `json:"success_rate"`
	AvgCostUSD     float64 `json:"avg_cost_usd"`
	AvgDurationMs  int64   `json:"avg_duration_ms"`
}

// FlowDetailResponse is the full flow with topology.
type FlowDetailResponse struct {
	Hash      string            `json:"hash"`
	Role      string            `json:"role"`
	Topology  json.RawMessage   `json:"topology"`
	Stats     FlowStatsResponse `json:"stats"`
	CreatedAt string            `json:"created_at"`
	UpdatedAt string            `json:"updated_at"`
}

// ExecutionRecordResponse is a single execution record.
type ExecutionRecordResponse struct {
	ID               string  `json:"id"`
	CPNID            string  `json:"cpn_id"`
	SessionID        string  `json:"session_id"`
	TransitionsFired int     `json:"transitions_fired"`
	LLMCalls         int     `json:"llm_calls"`
	ToolCalls        int     `json:"tool_calls"`
	TokensProduced   int     `json:"tokens_produced"`
	TotalCostUSD     float64 `json:"total_cost_usd"`
	DurationMs       int64   `json:"duration_ms"`
	Success          bool    `json:"success"`
	StartedAt        string  `json:"started_at"`
	CompletedAt      string  `json:"completed_at"`
}

// ExecutionListResponse is a list of execution records.
type ExecutionListResponse struct {
	Items []ExecutionRecordResponse `json:"items"`
}

// EventResponse is a single event in an execution trace.
type EventResponse struct {
	ID             string          `json:"id"`
	Type           string          `json:"type"`
	TransitionID   string          `json:"transition_id"`
	TransitionKind string          `json:"transition_kind"`
	CPNID          string          `json:"cpn_id"`
	CPNDepth       int             `json:"cpn_depth"`
	CPNRole        string          `json:"cpn_role"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	TokenSnapshot  json.RawMessage `json:"token_snapshot,omitempty"`
	Timestamp      string          `json:"timestamp"`
}

// SessionExecutionResponse is the execution trace for a session.
type SessionExecutionResponse struct {
	SessionID string          `json:"session_id"`
	Events    []EventResponse `json:"events"`
}

// CreateFlowRequest is the body for POST /api/v1/flows.
// It expresses the user's intent in structured form so the deterministic
// architect Planner can route to Reuse / Compose / ToolForge / Reject
// without an LLM call.
type CreateFlowRequest struct {
	Intent          string   `json:"intent"`
	Hashtags        []string `json:"hashtags"`
	RequiredCaps    []string `json:"required_caps"`
	InputColors     []string `json:"input_colors"`
	ParallelismHint string   `json:"parallelism_hint"`
}

// FlowCandidateResponse is a library near-match surfaced in a draft.
type FlowCandidateResponse struct {
	Hash     string   `json:"hash"`
	Role     string   `json:"role"`
	Hashtags []string `json:"hashtags"`
}

// ToolMatchResponse is one retrieval match in a compose draft.
type ToolMatchResponse struct {
	QualifiedName string   `json:"qualified_name"`
	Score         float64  `json:"score"`
	Hashtags      []string `json:"hashtags"`
}

// FlowDraftResponse is the POST /api/v1/flows result: a single
// Planner.Plan() iteration serialised for the frontend. The topology blob is
// included verbatim when Strategy == "compose" so the UI can preview the
// deterministic JIT output before the user chooses to instantiate it.
type FlowDraftResponse struct {
	Strategy     string                  `json:"strategy"`
	BaseFlowID   string                  `json:"base_flow_id,omitempty"`
	Topology     json.RawMessage         `json:"topology,omitempty"`
	Matches      []ToolMatchResponse     `json:"matches,omitempty"`
	MissingCaps  []string                `json:"missing_caps,omitempty"`
	Reason       string                  `json:"reason"`
	Confidence   float64                 `json:"confidence"`
	Candidates   []FlowCandidateResponse `json:"candidates,omitempty"`
}

// BalanceResponse is the billing balance response.
type BalanceResponse struct {
	LimitRemaining *float64 `json:"limit_remaining"`
	Usage          float64  `json:"usage"`
	UsageDaily     float64  `json:"usage_daily"`
	UsageWeekly    float64  `json:"usage_weekly"`
	UsageMonthly   float64  `json:"usage_monthly"`
	IsFreeTier     bool     `json:"is_free_tier"`
}
