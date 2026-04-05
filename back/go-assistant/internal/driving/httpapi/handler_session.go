package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/tools"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

// ── Multi-chat management types ───────────────────────────────────────────────

// SessionListItemResponse is a lightweight session in a list view.
type SessionListItemResponse struct {
	ID                  string  `json:"id"`
	Title               string  `json:"title"`
	State               string  `json:"state"`
	LastMessagePreview  string  `json:"last_message_preview"`
	LastActivityAt      string  `json:"last_activity_at"`
	CreatedAt           string  `json:"created_at"`
	TotalCostUSD        float64 `json:"total_cost_usd"`
	MessageCount        int     `json:"message_count"`
	ForkedFromSessionID string  `json:"forked_from_session_id,omitempty"`
}

// SessionListResponse is the paginated list of sessions.
type SessionListResponse struct {
	Items      []SessionListItemResponse `json:"items"`
	NextCursor string                    `json:"next_cursor"`
	HasMore    bool                      `json:"has_more"`
}

// UpdateSessionRequest is the request body for PATCH /api/v1/sessions/{id}.
type UpdateSessionRequest struct {
	Title   *string `json:"title"`
	Deleted *bool   `json:"deleted"`
}

// ForkSessionRequest is the request body for POST /api/v1/sessions/{id}/fork.
type ForkSessionRequest struct {
	MessageIndex int `json:"message_index"`
}

// ForkSessionResponse is the response for a forked session.
type ForkSessionResponse struct {
	SessionResponse
	Title               string  `json:"title"`
	ForkedFromSessionID string  `json:"forked_from_session_id"`
	ForkMessageCount    int     `json:"fork_message_count"`
	TotalCostUSD        float64 `json:"total_cost_usd"`
}

// Handlers holds dependencies for HTTP handlers.
type Handlers struct {
	App             *app.SessionService
	Broker          *SSEBroker
	Logger          *slog.Logger
	BillingFetcher  BillingFetcher
	FlowRepo        persist.FlowRepository
	IntelRepo       persist.IntelligenceRepository
	EventRepo       persist.EventRepository
	Verifier        auth.TokenVerifier
	UserRepo        persist.UserRepository
	PersonalityRepo persist.PersonalityRepository
	ToolRegistry    *tools.Registry
	RateLimitCfg    *RateLimitConfig
}

// HandleCreateSession creates a new session.
// POST /api/v1/sessions
// User identity is derived from the authenticated token context (REQ-009).
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

	// Derive user_id from authenticated token context.
	// Falls back to request body user_id for backward compatibility in dev-mode.
	userID := req.UserID
	if user := auth.UserFromContext(r.Context()); user != nil && user.Sub != "dev-user" {
		userID = user.Sub

		// Upsert user record if repository is available.
		if h.UserRepo != nil {
			now := time.Now()
			_ = h.UserRepo.Upsert(r.Context(), &persist.UserRecord{
				ID:        user.Sub,
				Email:     user.Email,
				Name:      user.Name,
				Picture:   user.Picture,
				CreatedAt: now,
				UpdatedAt: now,
			})
		}
	}

	info, err := h.App.CreateSession(r.Context(), userID, cpn.ChannelType(req.Channel))
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

// checkSessionOwnership verifies the authenticated user owns the session.
// Returns false and writes a 403 response if the user does not own the session.
// In dev-mode (user.Sub == "dev-user"), ownership is always granted.
func (h *Handlers) checkSessionOwnership(w http.ResponseWriter, r *http.Request, sessionUserID string) bool {
	user := auth.UserFromContext(r.Context())
	if user == nil || user.Sub == "dev-user" {
		return true // dev-mode: no ownership check
	}
	if user.Sub != sessionUserID {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error":   "forbidden",
			"message": "you do not have access to this session",
		})
		return false
	}
	return true
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

	if !h.checkSessionOwnership(w, r, info.UserID) {
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

// HandleDeleteSession deletes a session and releases its resources.
// DELETE /api/v1/sessions/{id}
func (h *Handlers) HandleDeleteSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session ID is required")
		return
	}

	// Check ownership before deleting.
	info, err := h.App.GetSession(sessionID)
	if err != nil {
		if errors.Is(err, app.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !h.checkSessionOwnership(w, r, info.UserID) {
		return
	}

	if err := h.App.DeleteSession(sessionID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, StatusResponse{Status: "deleted"})
}

// HandleSendMessage sends a user message to the session.
// POST /api/v1/sessions/{id}/messages
func (h *Handlers) HandleSendMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session ID is required")
		return
	}

	// Check ownership before sending.
	info, err := h.App.GetSession(sessionID)
	if err != nil {
		if errors.Is(err, app.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !h.checkSessionOwnership(w, r, info.UserID) {
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

	err = h.App.SendMessage(r.Context(), sessionID, req.Content)
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

// HandleListSessions returns a paginated list of sessions for the authenticated user.
// GET /api/v1/sessions
func (h *Handlers) HandleListSessions(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	opts := &persist.SessionListOpts{
		Cursor: r.URL.Query().Get("cursor"),
	}
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit < 1 {
			writeError(w, http.StatusBadRequest, "invalid limit parameter")
			return
		}
		opts.Limit = limit
	}

	page, err := h.App.ListSessions(r.Context(), user.Sub, opts)
	if err != nil {
		if errors.Is(err, app.ErrNoPersistence) {
			writeError(w, http.StatusServiceUnavailable, "persistence not configured")
			return
		}
		h.Logger.Error("list sessions", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	items := make([]SessionListItemResponse, len(page.Items))
	for i, item := range page.Items {
		items[i] = SessionListItemResponse{
			ID:                  item.ID,
			Title:               item.Title,
			State:               string(item.State),
			LastMessagePreview:  item.LastMessagePreview,
			LastActivityAt:      item.LastActivityAt.Format(time.RFC3339),
			CreatedAt:           item.CreatedAt.Format(time.RFC3339),
			TotalCostUSD:        item.TotalCostUSD,
			MessageCount:        item.MessageCount,
			ForkedFromSessionID: item.ForkedFromSessionID,
		}
	}

	writeJSON(w, http.StatusOK, SessionListResponse{
		Items:      items,
		NextCursor: page.NextCursor,
		HasMore:    page.HasMore,
	})
}

// HandleUpdateSession updates a session's title or soft-deletes it.
// PATCH /api/v1/sessions/{id}
func (h *Handlers) HandleUpdateSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session ID is required")
		return
	}

	// Check ownership via persisted session record.
	user := auth.UserFromContext(r.Context())
	if h.App != nil {
		info, err := h.App.GetSession(sessionID)
		if err != nil {
			// Session may not be in memory; check persistence directly.
			if errors.Is(err, app.ErrSessionNotFound) {
				// For persisted-only sessions, we still allow updates.
				// Ownership will be checked at persistence layer if needed.
			} else {
				writeError(w, http.StatusInternalServerError, "internal error")
				return
			}
		} else if !h.checkSessionOwnership(w, r, info.UserID) {
			return
		}
	}
	_ = user // user checked via checkSessionOwnership

	var req UpdateSessionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	softDelete := req.Deleted != nil && *req.Deleted

	if err := h.App.UpdateSession(r.Context(), sessionID, req.Title, softDelete); err != nil {
		if errors.Is(err, app.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		if errors.Is(err, app.ErrNoPersistence) {
			writeError(w, http.StatusServiceUnavailable, "persistence not configured")
			return
		}
		h.Logger.Error("update session", "session_id", sessionID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusOK, StatusResponse{Status: "updated"})
}

// HandleForkSession forks a session at the given message index.
// POST /api/v1/sessions/{id}/fork
func (h *Handlers) HandleForkSession(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session ID is required")
		return
	}

	user := auth.UserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	// Check ownership: try in-memory first, then fall back to persistence.
	info, err := h.App.GetSession(sessionID)
	if err != nil && !errors.Is(err, app.ErrSessionNotFound) {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if info != nil {
		if !h.checkSessionOwnership(w, r, info.UserID) {
			return
		}
	}
	// If session is not in memory but exists in persistence, ForkSession will handle it.

	var req ForkSessionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	newInfo, err := h.App.ForkSession(r.Context(), sessionID, user.Sub, "web", req.MessageIndex)
	if err != nil {
		if errors.Is(err, app.ErrNoPersistence) {
			writeError(w, http.StatusServiceUnavailable, "persistence not configured")
			return
		}
		if errors.Is(err, app.ErrSessionNotFound) {
			writeError(w, http.StatusNotFound, "source session not found")
			return
		}
		h.Logger.Error("fork session", "session_id", sessionID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	writeJSON(w, http.StatusCreated, ForkSessionResponse{
		SessionResponse: SessionResponse{
			ID:        newInfo.ID,
			UserID:    newInfo.UserID,
			Channel:   string(newInfo.Channel),
			State:     string(newInfo.State),
			CreatedAt: newInfo.CreatedAt.Format(time.RFC3339),
		},
		Title:               newInfo.Title,
		ForkedFromSessionID: newInfo.ForkedFromSessionID,
		ForkMessageCount:    newInfo.ForkMessageCount,
		TotalCostUSD:        0,
	})
}
