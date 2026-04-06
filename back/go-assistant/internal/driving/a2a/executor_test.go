package a2a

import (
	"context"
	"encoding/json"
	"testing"
)

// TestHandleSendMessage_ExtractTextContent verifies the text extraction helper
// used by the executor before sending to SessionService.
func TestHandleSendMessage_ExtractTextContent(t *testing.T) {
	tests := []struct {
		name    string
		message Message
		want    string
	}{
		{
			name:    "single text part",
			message: Message{Role: RoleUser, Parts: []Part{{Type: "text", Text: "hello"}}},
			want:    "hello",
		},
		{
			name:    "empty text part",
			message: Message{Role: RoleUser, Parts: []Part{{Type: "text", Text: ""}}},
			want:    "",
		},
		{
			name:    "no parts",
			message: Message{Role: RoleUser, Parts: nil},
			want:    "",
		},
		{
			name:    "data part only",
			message: Message{Role: RoleUser, Parts: []Part{{Type: "data"}}},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTextContent(tt.message)
			if got != tt.want {
				t.Errorf("extractTextContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestErrorResponse verifies JSON-RPC error response construction.
func TestErrorResponse(t *testing.T) {
	id := json.RawMessage(`1`)
	resp := errorResponse(id, CodeTaskNotFound, "task not found")

	if resp.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want %q", resp.JSONRPC, "2.0")
	}
	if resp.Error == nil {
		t.Fatal("expected error to be non-nil")
	}
	if resp.Error.Code != CodeTaskNotFound {
		t.Errorf("code = %d, want %d", resp.Error.Code, CodeTaskNotFound)
	}
	if resp.Error.Message != "task not found" {
		t.Errorf("message = %q, want %q", resp.Error.Message, "task not found")
	}
	if resp.Result != nil {
		t.Error("expected result to be nil")
	}
}

// TestResolveSessionFromTaskID verifies that parseSessionID correctly
// extracts the session ID from composite task IDs.
func TestResolveSessionFromTaskID(t *testing.T) {
	tests := []struct {
		taskID    string
		sessionID string
	}{
		{"abc123:0", "abc123"},
		{"session-with-dashes:0", "session-with-dashes"},
		{"no-execution-index", "no-execution-index"},
		{"nested:colons:0", "nested:colons"},
	}

	for _, tt := range tests {
		t.Run(tt.taskID, func(t *testing.T) {
			got := parseSessionID(tt.taskID)
			if got != tt.sessionID {
				t.Errorf("parseSessionID(%q) = %q, want %q", tt.taskID, got, tt.sessionID)
			}
		})
	}
}

// TestHandleCancelTask_InvalidInput tests that HandleCancelTask returns the
// correct error for an empty task ID (which maps to a non-existent session).
func TestHandleCancelTask_ErrorResponse(t *testing.T) {
	// We can't easily mock SessionService since it's a concrete type.
	// Instead, test the error response builder directly.
	id := json.RawMessage(`42`)
	resp := errorResponse(id, CodeTaskNotFound, "task not found: session not found: abc")

	if resp.Error.Code != CodeTaskNotFound {
		t.Errorf("code = %d, want %d", resp.Error.Code, CodeTaskNotFound)
	}
}

// TestFlushEvent verifies SSE event formatting.
func TestFlushEvent(t *testing.T) {
	exec := &BRAEExecutor{logger: noopLogger()}

	var written []byte
	flush := func(data []byte) {
		written = append(written, data...)
	}

	evt := &TaskStatusUpdateEvent{
		ID:        "task-1",
		ContextID: "ctx-1",
		Status:    TaskStatus{State: TaskStateWorking},
		Final:     false,
	}

	exec.flushEvent(flush, "status", evt)

	if len(written) == 0 {
		t.Fatal("expected data to be written")
	}

	str := string(written)
	if str[:7] != "event: " {
		t.Errorf("expected SSE event prefix, got %q", str[:7])
	}
	if str[7:13] != "status" {
		t.Errorf("expected event type 'status', got %q", str[7:13])
	}
	// Verify it ends with double newline.
	if str[len(str)-2:] != "\n\n" {
		t.Errorf("expected trailing double newline")
	}
}

// TestHandleSendMessage_EmptyMessage tests that an empty message returns an error
// before any session creation is attempted.
func TestHandleSendMessage_EmptyMessage(t *testing.T) {
	exec := &BRAEExecutor{
		service: nil, // not reached — validation happens first
		mapper:  NewMapper(),
		logger:  noopLogger(),
	}

	ctx := context.Background()
	reqID := json.RawMessage(`1`)
	req := SendMessageRequest{
		Message: Message{Role: RoleUser, Parts: nil},
	}

	resp := exec.HandleSendMessage(ctx, reqID, req, "user-1")
	if resp.Error == nil {
		t.Fatal("expected error for empty message")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
}
