package httpapi

import (
	"errors"
	"net/http"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
)

// HandleResolveHITL resolves a pending HITL request.
// POST /api/v1/sessions/{id}/hitl/{transitionID}
func (h *Handlers) HandleResolveHITL(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	transitionID := r.PathValue("transitionID")
	if sessionID == "" || transitionID == "" {
		writeError(w, http.StatusBadRequest, "session ID and transition ID are required")
		return
	}

	var req ResolveHITLRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	resp := cpn.HITLResponse{
		Action:  cpn.HITLAction(req.Action),
		Content: req.Content,
	}

	err := h.App.ResolveHITL(r.Context(), sessionID, transitionID, resp)
	if err != nil {
		if errors.Is(err, app.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		if errors.Is(err, cpn.ErrNoHITLWaiting) {
			writeError(w, http.StatusConflict, "no HITL request pending for this transition")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, StatusResponse{Status: "resolved"})
}
