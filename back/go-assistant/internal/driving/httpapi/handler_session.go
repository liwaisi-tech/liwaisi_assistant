package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
)

// Handlers holds dependencies for HTTP handlers.
type Handlers struct {
	App            *app.SessionService
	Broker         *SSEBroker
	Logger         *slog.Logger
	BillingFetcher BillingFetcher
}

// HandleCreateSession creates a new session.
// POST /api/v1/sessions
func (h *Handlers) HandleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	info, err := h.App.CreateSession(r.Context(), req.UserID, cpn.ChannelType(req.Channel))
	if err != nil {
		if errors.Is(err, app.ErrInvalidInput) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, SessionResponse{
		ID:        info.ID,
		UserID:    info.UserID,
		Channel:   string(info.Channel),
		State:     string(info.State),
		CreatedAt: info.CreatedAt.Format(time.RFC3339),
	})
}

// HandleGetSession returns session details.
// GET /api/v1/sessions/{id}
func (h *Handlers) HandleGetSession(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	msgs := make([]MessageResponse, len(info.Messages))
	for i, m := range info.Messages {
		msgs[i] = MessageResponse{
			ID:        m.ID,
			Role:      string(m.Role),
			Content:   m.Content,
			CPNID:     m.CPNID,
			Timestamp: m.Timestamp.Format(time.RFC3339),
		}
	}

	writeJSON(w, http.StatusOK, SessionDetailResponse{
		SessionResponse: SessionResponse{
			ID:        info.ID,
			UserID:    info.UserID,
			Channel:   string(info.Channel),
			State:     string(info.State),
			CreatedAt: info.CreatedAt.Format(time.RFC3339),
		},
		Messages: msgs,
	})
}

// HandleSendMessage sends a user message to the session.
// POST /api/v1/sessions/{id}/messages
func (h *Handlers) HandleSendMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session ID is required")
		return
	}

	var req SendMessageRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := req.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	err := h.App.SendMessage(r.Context(), sessionID, req.Content)
	if err != nil {
		if errors.Is(err, app.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		if errors.Is(err, app.ErrSessionBusy) {
			writeError(w, http.StatusConflict, "session is busy")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusAccepted, StatusResponse{Status: "accepted"})
}
