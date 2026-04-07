package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
)

// BRAEExecutor handles A2A JSON-RPC requests by delegating to the SessionService.
// It bridges A2A protocol semantics to the CPN application layer.
type BRAEExecutor struct {
	service *app.SessionService
	mapper  *Mapper
	logger  *slog.Logger
}

// NewBRAEExecutor creates a new A2A executor wired to the given SessionService.
func NewBRAEExecutor(service *app.SessionService, mapper *Mapper, logger *slog.Logger) *BRAEExecutor {
	return &BRAEExecutor{
		service: service,
		mapper:  mapper,
		logger:  logger,
	}
}

// HandleSendMessage processes a synchronous message/send request.
// It creates a session (or reuses one from the task ID), sends the user message,
// waits for CPN completion, and returns a Task with artifacts.
func (e *BRAEExecutor) HandleSendMessage(ctx context.Context, reqID json.RawMessage, req *SendMessageRequest, userID string) *JSONRPCResponse {
	// Extract text content from the request message (validate before session creation).
	content := extractTextContent(req.Message)
	if content == "" {
		return errorResponse(reqID, CodeInvalidParams, "message must contain at least one text part")
	}

	sessionID, err := e.resolveSession(ctx, req, userID)
	if err != nil {
		return errorResponse(reqID, CodeInternalError, fmt.Sprintf("session error: %v", err))
	}

	// Send message to session (starts CPN execution in background).
	if err := e.service.SendMessage(ctx, sessionID, content); err != nil {
		return errorResponse(reqID, CodeInternalError, fmt.Sprintf("send message: %v", err))
	}

	// Wait for completion by polling the stream channel.
	streamCh, err := e.service.StreamChannel(sessionID)
	if err != nil {
		return errorResponse(reqID, CodeInternalError, fmt.Sprintf("stream channel: %v", err))
	}

	var contentBuf strings.Builder
	for chunk := range streamCh {
		if chunk.Done {
			break
		}
		contentBuf.WriteString(chunk.Content)
	}

	// Get final session state.
	info, err := e.service.GetSession(sessionID)
	if err != nil {
		return errorResponse(reqID, CodeInternalError, fmt.Sprintf("get session: %v", err))
	}

	task := e.mapper.SessionInfoToTask(info.ID, info.State, info.CreatedAt, info.Messages)

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      reqID,
		Result:  task,
	}
}

// HandleStreamMessage processes a message/stream request, yielding SSE events
// via the flush callback as the CPN executes.
func (e *BRAEExecutor) HandleStreamMessage(ctx context.Context, reqID json.RawMessage, req *SendMessageRequest, userID string, flush func([]byte)) {
	// Validate content before session creation.
	content := extractTextContent(req.Message)
	if content == "" {
		e.flushEvent(flush, "error", errorResponse(reqID, CodeInvalidParams, "message must contain at least one text part"))
		return
	}

	sessionID, err := e.resolveSession(ctx, req, userID)
	if err != nil {
		e.flushEvent(flush, "error", errorResponse(reqID, CodeInternalError, fmt.Sprintf("session error: %v", err)))
		return
	}

	taskID := sessionID + ":0"
	contextID := sessionID

	// Emit initial status: working.
	e.flushEvent(flush, "status", &TaskStatusUpdateEvent{
		ID:        taskID,
		ContextID: contextID,
		Status: TaskStatus{
			State:     TaskStateWorking,
			Timestamp: time.Now().Format(time.RFC3339),
		},
		Final: false,
	})

	// Send message to session.
	if err := e.service.SendMessage(ctx, sessionID, content); err != nil {
		e.flushEvent(flush, "error", errorResponse(reqID, CodeInternalError, fmt.Sprintf("send message: %v", err)))
		return
	}

	// Stream chunks from session.
	streamCh, err := e.service.StreamChannel(sessionID)
	if err != nil {
		e.flushEvent(flush, "error", errorResponse(reqID, CodeInternalError, fmt.Sprintf("stream channel: %v", err)))
		return
	}

	for chunk := range streamCh {
		evt := e.mapper.CPNEventToA2AEvent(taskID, contextID, &cpn.Event{
			Type:    cpn.EventStreamChunk,
			Payload: chunk,
		})
		if evt != nil {
			e.flushEvent(flush, "artifact", evt)
		}
		if chunk.Done {
			break
		}
	}

	// Emit final status: completed.
	e.flushEvent(flush, "status", &TaskStatusUpdateEvent{
		ID:        taskID,
		ContextID: contextID,
		Status: TaskStatus{
			State:     TaskStateCompleted,
			Timestamp: time.Now().Format(time.RFC3339),
		},
		Final: true,
	})
}

// HandleGetTask retrieves a task by its composite ID ("{sessionID}:0").
func (e *BRAEExecutor) HandleGetTask(ctx context.Context, reqID json.RawMessage, req GetTaskRequest, userID string) *JSONRPCResponse {
	sessionID := parseSessionID(req.TaskID)

	info, err := e.service.GetSession(sessionID)
	if err != nil {
		return errorResponse(reqID, CodeTaskNotFound, fmt.Sprintf("task not found: %v", err))
	}

	// Enforce session ownership per SEC-003.
	if info.UserID != userID {
		return errorResponse(reqID, CodeTaskNotFound, "task not found")
	}

	task := e.mapper.SessionInfoToTask(info.ID, info.State, info.CreatedAt, info.Messages)

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      reqID,
		Result:  task,
	}
}

// HandleListTasks returns a list of tasks for the authenticated user.
func (e *BRAEExecutor) HandleListTasks(ctx context.Context, reqID json.RawMessage, req ListTasksRequest, userID string) *JSONRPCResponse {
	// Use SessionService.ListSessions with pagination.
	// (Limit is currently advisory; ListSessions handles its own paging.)
	_ = req.Limit

	// ListSessions requires persistence. If not available, return empty list.
	page, err := e.service.ListSessions(ctx, userID, nil)
	if err != nil {
		// Gracefully handle no-persistence case.
		return &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      reqID,
			Result: map[string]any{
				"tasks":   []any{},
				"hasMore": false,
			},
		}
	}

	tasks := make([]*Task, 0, len(page.Items))
	for _, item := range page.Items {
		tasks = append(tasks, &Task{
			ID:        item.ID + ":0",
			ContextID: item.ID,
			Status: TaskStatus{
				State:     string(item.State),
				Timestamp: item.LastActivityAt.Format(time.RFC3339),
			},
			Metadata: map[string]any{
				"title":        item.Title,
				"messageCount": item.MessageCount,
			},
		})
	}

	result := map[string]any{
		"tasks":   tasks,
		"hasMore": page.HasMore,
	}
	if page.NextCursor != "" {
		result["cursor"] = page.NextCursor
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      reqID,
		Result:  result,
	}
}

// HandleCancelTask cancels a running task by deleting its session.
func (e *BRAEExecutor) HandleCancelTask(ctx context.Context, reqID json.RawMessage, req CancelTaskRequest, userID string) *JSONRPCResponse {
	sessionID := parseSessionID(req.TaskID)

	// Verify ownership before canceling per SEC-003.
	info, err := e.service.GetSession(sessionID)
	if err != nil {
		return errorResponse(reqID, CodeTaskNotFound, fmt.Sprintf("task not found: %v", err))
	}
	if info.UserID != userID {
		return errorResponse(reqID, CodeTaskNotFound, "task not found")
	}

	if err := e.service.DeleteSession(sessionID); err != nil {
		return errorResponse(reqID, CodeInternalError, fmt.Sprintf("cancel task: %v", err))
	}

	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      reqID,
		Result: &Task{
			ID:        req.TaskID,
			ContextID: sessionID,
			Status: TaskStatus{
				State:     TaskStateCanceled,
				Timestamp: time.Now().Format(time.RFC3339),
			},
		},
	}
}

// resolveSession creates a new session or reuses an existing one from the task ID.
func (e *BRAEExecutor) resolveSession(ctx context.Context, req *SendMessageRequest, userID string) (string, error) {
	// If a task ID is provided, extract the session ID.
	if req.TaskID != "" {
		sessionID := parseSessionID(req.TaskID)
		if _, err := e.service.GetSession(sessionID); err == nil {
			return sessionID, nil
		}
		// Fall through to create new session if the referenced one doesn't exist.
	}

	// Create a new session for this A2A interaction.
	info, err := e.service.CreateSession(ctx, userID, cpn.ChannelWeb)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	return info.ID, nil
}

// flushEvent serializes an event as SSE and writes it via the flush callback.
func (e *BRAEExecutor) flushEvent(flush func([]byte), eventType string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		e.logger.Error("marshal SSE event", slog.String("type", eventType), slog.Any("error", err))
		return
	}

	// Format as SSE: "event: <type>\ndata: <json>\n\n"
	var buf []byte
	buf = append(buf, "event: "...)
	buf = append(buf, eventType...)
	buf = append(buf, '\n')
	buf = append(buf, "data: "...)
	buf = append(buf, payload...)
	buf = append(buf, '\n', '\n')

	flush(buf)
}

// extractTextContent returns the concatenated text from all TextParts in a message.
func extractTextContent(msg Message) string {
	var parts []string
	for _, p := range msg.Parts {
		if p.Type == "text" && p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// parseSessionID extracts the session ID from a composite task ID ("{sessionID}:0").
func parseSessionID(taskID string) string {
	if idx := strings.LastIndex(taskID, ":"); idx > 0 {
		return taskID[:idx]
	}
	return taskID
}

// errorResponse builds a JSON-RPC error response.
func errorResponse(id json.RawMessage, code int, message string) *JSONRPCResponse {
	return &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
		},
	}
}
