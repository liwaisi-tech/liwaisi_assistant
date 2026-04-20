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

	// Check ownership before resolving HITL.
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

	err = h.App.ResolveHITL(r.Context(), sessionID, transitionID, resp)
	if err != nil {
		if errors.Is(err, app.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		// Orphan check must precede ErrSessionInactive because the orphan
		// error is constructed with %w wrapping and could otherwise be
		// shadowed by an intermediate matcher. The envelope shape is
		// specified verbatim by spec §4.3 and is load-bearing for the
		// frontend stale-card dismissal path (REQ-007 / AC-004).
		if errors.Is(err, app.ErrTransitionOrphaned) {
			writeHITLOrphaned(w, transitionID)
			return
		}
		if errors.Is(err, app.ErrSessionInactive) {
			writeError(w, http.StatusConflict, "session inactive")
			return
		}
		if errors.Is(err, app.ErrPersistenceUnavailable) {
			writeError(w, http.StatusServiceUnavailable, "persistence unavailable")
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

// hitlOrphanedError is the structured error envelope emitted when
// ResolveHITL fails with ErrTransitionOrphaned. The shape is load-bearing:
// the frontend branches on `error.code == "HITL_TRANSITION_ORPHANED"` to
// dismiss a stale approval card with a neutral toast (spec §4.3, §9.4).
type hitlOrphanedError struct {
	Error hitlOrphanedBody `json:"error"`
}

type hitlOrphanedBody struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	TransitionID string `json:"transitionId"`
}

// writeHITLOrphaned writes the §4.3 envelope with HTTP 409.
func writeHITLOrphaned(w http.ResponseWriter, transitionID string) {
	writeJSON(w, http.StatusConflict, hitlOrphanedError{
		Error: hitlOrphanedBody{
			Code:         "HITL_TRANSITION_ORPHANED",
			Message:      "Esta aprobación ya fue resuelta",
			TransitionID: transitionID,
		},
	})
}
