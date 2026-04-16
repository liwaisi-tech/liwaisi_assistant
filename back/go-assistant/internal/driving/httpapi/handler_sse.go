package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
)

// HandleSSEStream establishes a Server-Sent Events connection for a session.
// GET /api/v1/sessions/{id}/events
//
// The handler:
//  1. Validates the session exists
//  2. Sets SSE headers
//  3. Resets write deadline for long-lived connection
//  4. Subscribes to the SSE broker for domain events
//  5. Gets the session's StreamChunk channel for LLM streaming
//  6. Runs a select loop over: broker events, stream chunks, heartbeat, client disconnect
//  7. Cleans up on exit
func (h *Handlers) HandleSSEStream(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session ID is required")
		return
	}

	// 1. Validate session exists, check ownership, and get stream channel.
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

	streamCh, err := h.App.StreamChannel(sessionID)
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

	// 2. Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// 3. Use http.ResponseController for flush and write deadline.
	rc := http.NewResponseController(w)
	// Reset write deadline — SSE connections are long-lived.
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		h.logSSE(sessionID, "failed to reset write deadline", err)
	}

	// 4. Subscribe to SSE broker for domain events.
	client, cleanup := h.Broker.Subscribe(sessionID)
	defer cleanup()

	// 5. Send initial retry directive and flush.
	if err := WriteSSERetry(w, 3000); err != nil {
		h.logSSE(sessionID, "write retry directive failed", err)
		return
	}
	rc.Flush()

	// 6. Start heartbeat ticker.
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	// 7. Select loop.
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return

		case <-client.done:
			return

		case eventData, ok := <-client.events:
			if !ok {
				return
			}
			if _, err := w.Write(eventData); err != nil {
				h.logSSE(sessionID, "write event failed", err)
				return
			}
			if err := rc.Flush(); err != nil {
				h.logSSE(sessionID, "flush failed", err)
				return
			}

		case chunk, ok := <-streamCh:
			if !ok {
				// Stream channel closed — send session_completed event.
				data, _ := json.Marshal(map[string]string{"status": "stream_closed"})
				_ = WriteSSEEvent(w, h.Broker.NextEventID(), "session_completed", data)
				rc.Flush()
				return
			}
			data, err := json.Marshal(chunk)
			if err != nil {
				h.logSSE(sessionID, "marshal chunk failed", err)
				continue
			}
			eventID := h.Broker.NextEventID()
			if err := WriteSSEEvent(w, eventID, "stream_chunk", data); err != nil {
				h.logSSE(sessionID, "write chunk failed", err)
				return
			}
			if err := rc.Flush(); err != nil {
				h.logSSE(sessionID, "flush failed", err)
				return
			}

		case <-heartbeat.C:
			if err := WriteSSEComment(w, "heartbeat"); err != nil {
				h.logSSE(sessionID, "heartbeat failed", err)
				return
			}
			if err := rc.Flush(); err != nil {
				h.logSSE(sessionID, "flush failed", err)
				return
			}
		}
	}
}

// logSSE is a helper for SSE handler logging.
func (h *Handlers) logSSE(sessionID, msg string, err error) {
	if h.Logger != nil {
		h.Logger.Debug(msg, slog.String("session_id", sessionID), slog.Any("error", err))
	}
}
