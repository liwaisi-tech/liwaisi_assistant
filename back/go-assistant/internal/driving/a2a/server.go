package a2a

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/auth"
)

// RegisterHandlers registers the A2A protocol routes on the given ServeMux.
// The agent card endpoint is public (no auth) per SEC-002.
// The JSON-RPC endpoint requires authentication per SEC-001.
//
// Routes:
//   - GET  /.well-known/agent-card.json — public, serves AgentCard with 5min cache
//   - POST /a2a                         — authenticated, JSON-RPC 2.0 dispatch
func RegisterHandlers(
	mux *http.ServeMux,
	executor *BRAEExecutor,
	card *AgentCard,
	authMiddleware func(http.Handler) http.Handler,
	logger *slog.Logger,
) {
	// Agent Card endpoint — public, no auth required.
	cardJSON, err := json.Marshal(card)
	if err != nil {
		logger.Error("failed to marshal agent card", slog.Any("error", err))
		return
	}

	mux.HandleFunc("GET /.well-known/agent-card.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(cardJSON)
	})

	// JSON-RPC endpoint — authenticated.
	a2aHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleJSONRPC(w, r, executor, logger)
	})

	if authMiddleware != nil {
		mux.Handle("POST /a2a", authMiddleware(a2aHandler))
	} else {
		mux.Handle("POST /a2a", a2aHandler)
	}
}

// handleJSONRPC parses a JSON-RPC 2.0 request and dispatches to the executor.
func handleJSONRPC(w http.ResponseWriter, r *http.Request, executor *BRAEExecutor, logger *slog.Logger) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		writeJSONRPCError(w, nil, CodeParseError, "failed to read request body")
		return
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSONRPCError(w, nil, CodeParseError, "invalid JSON")
		return
	}

	if req.JSONRPC != "2.0" {
		writeJSONRPCError(w, req.ID, CodeInvalidRequest, "jsonrpc must be \"2.0\"")
		return
	}

	// Extract user ID from auth context.
	user := auth.UserFromContext(r.Context())
	userID := "anonymous"
	if user != nil {
		userID = user.Sub
	}

	logger.Info("a2a request",
		slog.String("method", req.Method),
		slog.String("user_id", userID),
	)

	switch req.Method {
	case MethodSendMessage:
		var params SendMessageRequest
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeJSONRPCError(w, req.ID, CodeInvalidParams, "invalid params: "+err.Error())
			return
		}
		resp := executor.HandleSendMessage(r.Context(), req.ID, params, userID)
		writeJSONResponse(w, resp)

	case MethodStreamMessage:
		var params SendMessageRequest
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeJSONRPCError(w, req.ID, CodeInvalidParams, "invalid params: "+err.Error())
			return
		}
		handleStreamSSE(w, r, req.ID, params, userID, executor)

	case MethodGetTask:
		var params GetTaskRequest
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeJSONRPCError(w, req.ID, CodeInvalidParams, "invalid params: "+err.Error())
			return
		}
		resp := executor.HandleGetTask(r.Context(), req.ID, params, userID)
		writeJSONResponse(w, resp)

	case MethodListTasks:
		var params ListTasksRequest
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeJSONRPCError(w, req.ID, CodeInvalidParams, "invalid params: "+err.Error())
			return
		}
		resp := executor.HandleListTasks(r.Context(), req.ID, params, userID)
		writeJSONResponse(w, resp)

	case MethodCancelTask:
		var params CancelTaskRequest
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeJSONRPCError(w, req.ID, CodeInvalidParams, "invalid params: "+err.Error())
			return
		}
		resp := executor.HandleCancelTask(r.Context(), req.ID, params, userID)
		writeJSONResponse(w, resp)

	default:
		writeJSONRPCError(w, req.ID, CodeMethodNotFound, "method not found: "+req.Method)
	}
}

// handleStreamSSE sets up SSE headers and streams A2A events.
func handleStreamSSE(w http.ResponseWriter, r *http.Request, reqID json.RawMessage, params SendMessageRequest, userID string, executor *BRAEExecutor) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONRPCError(w, reqID, CodeInternalError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	flush := func(data []byte) {
		_, _ = w.Write(data)
		flusher.Flush()
	}

	executor.HandleStreamMessage(r.Context(), reqID, params, userID, flush)
}

// writeJSONResponse writes a JSON-RPC response with appropriate headers.
func writeJSONResponse(w http.ResponseWriter, resp *JSONRPCResponse) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(resp)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// writeJSONRPCError writes a JSON-RPC error response.
func writeJSONRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	resp := &JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &JSONRPCError{
			Code:    code,
			Message: message,
		},
	}
	writeJSONResponse(w, resp)
}
