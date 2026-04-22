package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/awakens/toolapproval"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
)

// toolApprovalRequest is the body shape POSTed by the frontend after the
// operator clicks Approve or Deny. Matches ToolApprovalPrompt.tsx.
type toolApprovalRequest struct {
	RequestID string `json:"request_id"`
	Decision  string `json:"decision"`
}

// HandleToolApprovalDecision resolves an outstanding synthesized-tool HITL
// prompt. POST /api/v1/sessions/{id}/tool-approvals
//
// On success returns 204; on unknown request_id returns 404. Ownership is
// enforced against the session so a user cannot resolve another session's
// prompts.
func (h *Handlers) HandleToolApprovalDecision(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session ID is required")
		return
	}

	info, err := h.App.GetSession(sessionID)
	if err != nil {
		if errors.Is(err, app.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		if errors.Is(err, app.ErrPersistenceUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "persistence unavailable")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !h.checkSessionOwnership(w, r, info.UserID) {
		return
	}

	if h.ToolApproval == nil {
		writeError(w, http.StatusServiceUnavailable, "tool approval not configured")
		return
	}

	var req toolApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.RequestID == "" {
		writeError(w, http.StatusBadRequest, "request_id is required")
		return
	}

	var decision toolapproval.Decision
	switch req.Decision {
	case "approved":
		decision = toolapproval.DecisionApproved
	case "denied":
		decision = toolapproval.DecisionDenied
	default:
		writeError(w, http.StatusBadRequest, "decision must be 'approved' or 'denied'")
		return
	}

	if err := h.ToolApproval.Resolve(sessionID, req.RequestID, decision); err != nil {
		if errors.Is(err, ErrUnknownToolApprovalRequest) {
			writeError(w, http.StatusNotFound, "unknown or already-resolved tool-approval request")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
