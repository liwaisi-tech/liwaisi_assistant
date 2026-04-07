package a2a

import (
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func TestStateToA2A(t *testing.T) {
	m := NewMapper()

	tests := []struct {
		name     string
		state    cpn.State
		expected string
	}{
		{"idle maps to submitted", cpn.StateIdle, TaskStateSubmitted},
		{"running maps to working", cpn.StateRunning, TaskStateWorking},
		{"waiting maps to input-required", cpn.StateWaiting, TaskStateInputRequired},
		{"completed maps to completed", cpn.StateCompleted, TaskStateCompleted},
		{"failed maps to failed", cpn.StateFailed, TaskStateFailed},
		{"unknown maps to submitted", cpn.State("unknown"), TaskStateSubmitted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.StateToA2A(tt.state)
			if got != tt.expected {
				t.Errorf("StateToA2A(%q) = %q, want %q", tt.state, got, tt.expected)
			}
		})
	}
}

func TestCPNMessageToA2A(t *testing.T) {
	m := NewMapper()

	tests := []struct {
		name         string
		msg          cpn.Message
		expectedRole string
		expectedText string
	}{
		{
			name:         "user message",
			msg:          cpn.Message{ID: "m1", Role: cpn.RoleUser, Content: "hello"},
			expectedRole: RoleUser,
			expectedText: "hello",
		},
		{
			name:         "assistant message",
			msg:          cpn.Message{ID: "m2", Role: cpn.RoleAssistant, Content: "hi there"},
			expectedRole: RoleAgent,
			expectedText: "hi there",
		},
		{
			name:         "observer message maps to agent",
			msg:          cpn.Message{ID: "m3", Role: cpn.RoleObserver, Content: "summary"},
			expectedRole: RoleAgent,
			expectedText: "summary",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := tt.msg
			got := m.CPNMessageToA2A(&msg)
			if got.Role != tt.expectedRole {
				t.Errorf("role = %q, want %q", got.Role, tt.expectedRole)
			}
			if len(got.Parts) != 1 {
				t.Fatalf("parts len = %d, want 1", len(got.Parts))
			}
			if got.Parts[0].Text != tt.expectedText {
				t.Errorf("text = %q, want %q", got.Parts[0].Text, tt.expectedText)
			}
			if got.Parts[0].Type != "text" {
				t.Errorf("type = %q, want %q", got.Parts[0].Type, "text")
			}
		})
	}
}

func TestTokenPayloadToPart(t *testing.T) {
	m := NewMapper()

	tests := []struct {
		name         string
		payload      any
		expectedType string
		checkText    string
		checkData    bool
	}{
		{
			name:         "string payload",
			payload:      "hello world",
			expectedType: "text",
			checkText:    "hello world",
		},
		{
			name:         "byte slice payload",
			payload:      []byte("raw data"),
			expectedType: "raw",
			checkText:    "",
		},
		{
			name:         "map payload",
			payload:      map[string]any{"key": "value"},
			expectedType: "data",
			checkData:    true,
		},
		{
			name:         "struct payload marshals to data",
			payload:      struct{ Name string }{"test"},
			expectedType: "data",
			checkData:    true,
		},
		{
			name:         "integer payload to text",
			payload:      42,
			expectedType: "text",
			checkText:    "42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.TokenPayloadToPart(tt.payload)
			if got.Type != tt.expectedType {
				t.Errorf("type = %q, want %q", got.Type, tt.expectedType)
			}
			if tt.checkText != "" && got.Text != tt.checkText {
				t.Errorf("text = %q, want %q", got.Text, tt.checkText)
			}
			if tt.checkData && got.Data == nil {
				t.Error("expected data to be non-nil")
			}
		})
	}
}

func TestCPNEventToA2AEvent_StreamChunk(t *testing.T) {
	m := NewMapper()

	tests := []struct {
		name       string
		chunk      cpn.StreamChunk
		wantText   string
		wantLast   bool
		wantAppend bool
	}{
		{
			name:       "regular chunk",
			chunk:      cpn.StreamChunk{Content: "hello", Done: false},
			wantText:   "hello",
			wantLast:   false,
			wantAppend: true,
		},
		{
			name:       "final chunk",
			chunk:      cpn.StreamChunk{Content: "", Done: true},
			wantText:   "",
			wantLast:   true,
			wantAppend: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt := cpn.Event{Type: cpn.EventStreamChunk, Payload: tt.chunk}
			result := m.CPNEventToA2AEvent("task-1", "ctx-1", &evt)
			if result == nil {
				t.Fatal("expected non-nil result")
			}

			artEvt, ok := result.(*TaskArtifactUpdateEvent)
			if !ok {
				t.Fatalf("expected *TaskArtifactUpdateEvent, got %T", result)
			}
			if artEvt.ID != "task-1" {
				t.Errorf("id = %q, want %q", artEvt.ID, "task-1")
			}
			if artEvt.ContextID != "ctx-1" {
				t.Errorf("contextId = %q, want %q", artEvt.ContextID, "ctx-1")
			}
			if artEvt.Artifact.Append != tt.wantAppend {
				t.Errorf("append = %v, want %v", artEvt.Artifact.Append, tt.wantAppend)
			}
			if artEvt.Artifact.LastChunk != tt.wantLast {
				t.Errorf("lastChunk = %v, want %v", artEvt.Artifact.LastChunk, tt.wantLast)
			}
			if len(artEvt.Artifact.Parts) != 1 || artEvt.Artifact.Parts[0].Text != tt.wantText {
				t.Errorf("text = %q, want %q", artEvt.Artifact.Parts[0].Text, tt.wantText)
			}
		})
	}
}

func TestCPNEventToA2AEvent_HITLRequested(t *testing.T) {
	m := NewMapper()

	evt := cpn.Event{
		Type:      cpn.EventHITLRequested,
		Token:     &cpn.Token{Payload: "Please review the plan"},
		Timestamp: time.Now(),
	}

	result := m.CPNEventToA2AEvent("task-1", "ctx-1", &evt)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	statusEvt, ok := result.(*TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("expected *TaskStatusUpdateEvent, got %T", result)
	}
	if statusEvt.Status.State != TaskStateInputRequired {
		t.Errorf("state = %q, want %q", statusEvt.Status.State, TaskStateInputRequired)
	}
	if statusEvt.Status.Message == nil {
		t.Fatal("expected status message")
	}
	if statusEvt.Status.Message.Parts[0].Text != "Please review the plan" {
		t.Errorf("text = %q, want %q", statusEvt.Status.Message.Parts[0].Text, "Please review the plan")
	}
}

func TestCPNEventToA2AEvent_HITLResolved(t *testing.T) {
	m := NewMapper()

	evt := cpn.Event{
		Type:      cpn.EventHITLResolved,
		Timestamp: time.Now(),
	}

	result := m.CPNEventToA2AEvent("task-1", "ctx-1", &evt)
	statusEvt, ok := result.(*TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("expected *TaskStatusUpdateEvent, got %T", result)
	}
	if statusEvt.Status.State != TaskStateWorking {
		t.Errorf("state = %q, want %q", statusEvt.Status.State, TaskStateWorking)
	}
}

func TestCPNEventToA2AEvent_Unhandled(t *testing.T) {
	m := NewMapper()

	evt := cpn.Event{Type: cpn.EventTokenDeposited}
	result := m.CPNEventToA2AEvent("task-1", "ctx-1", &evt)
	if result != nil {
		t.Errorf("expected nil for unhandled event type, got %T", result)
	}
}

func TestSessionInfoToTask(t *testing.T) {
	m := NewMapper()

	now := time.Now()
	messages := []cpn.Message{
		{ID: "m1", Role: cpn.RoleUser, Content: "hello"},
		{ID: "m2", Role: cpn.RoleAssistant, Content: "hi there"},
	}

	task := m.SessionInfoToTask("sess-123", cpn.StateIdle, now, messages)

	if task.ID != "sess-123:0" {
		t.Errorf("id = %q, want %q", task.ID, "sess-123:0")
	}
	if task.ContextID != "sess-123" {
		t.Errorf("contextId = %q, want %q", task.ContextID, "sess-123")
	}
	if task.Status.State != TaskStateSubmitted {
		t.Errorf("state = %q, want %q", task.Status.State, TaskStateSubmitted)
	}
	if len(task.History) != 2 {
		t.Fatalf("history len = %d, want 2", len(task.History))
	}
	if task.History[0].Role != RoleUser {
		t.Errorf("history[0] role = %q, want %q", task.History[0].Role, RoleUser)
	}
	if task.History[1].Role != RoleAgent {
		t.Errorf("history[1] role = %q, want %q", task.History[1].Role, RoleAgent)
	}
	if len(task.Artifacts) != 1 {
		t.Fatalf("artifacts len = %d, want 1", len(task.Artifacts))
	}
	if task.Artifacts[0].Parts[0].Text != "hi there" {
		t.Errorf("artifact text = %q, want %q", task.Artifacts[0].Parts[0].Text, "hi there")
	}
}

func TestSessionInfoToTask_NoMessages(t *testing.T) {
	m := NewMapper()

	task := m.SessionInfoToTask("sess-empty", cpn.StateRunning, time.Now(), nil)

	if task.Status.State != TaskStateWorking {
		t.Errorf("state = %q, want %q", task.Status.State, TaskStateWorking)
	}
	if len(task.History) != 0 {
		t.Errorf("history len = %d, want 0", len(task.History))
	}
	if len(task.Artifacts) != 0 {
		t.Errorf("artifacts len = %d, want 0", len(task.Artifacts))
	}
}

func TestParseSessionID(t *testing.T) {
	tests := []struct {
		taskID   string
		expected string
	}{
		{"sess-123:0", "sess-123"},
		{"abc:1", "abc"},
		{"no-colon", "no-colon"},
		{"multi:colon:2", "multi:colon"},
	}

	for _, tt := range tests {
		t.Run(tt.taskID, func(t *testing.T) {
			got := parseSessionID(tt.taskID)
			if got != tt.expected {
				t.Errorf("parseSessionID(%q) = %q, want %q", tt.taskID, got, tt.expected)
			}
		})
	}
}

func TestExtractTextContent(t *testing.T) {
	tests := []struct {
		name     string
		msg      Message
		expected string
	}{
		{
			name:     "single text part",
			msg:      Message{Parts: []Part{{Type: "text", Text: "hello"}}},
			expected: "hello",
		},
		{
			name: "multiple text parts",
			msg: Message{Parts: []Part{
				{Type: "text", Text: "hello"},
				{Type: "text", Text: "world"},
			}},
			expected: "hello\nworld",
		},
		{
			name:     "no text parts",
			msg:      Message{Parts: []Part{{Type: "data", Data: map[string]any{"key": "val"}}}},
			expected: "",
		},
		{
			name:     "empty parts",
			msg:      Message{Parts: nil},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTextContent(tt.msg)
			if got != tt.expected {
				t.Errorf("extractTextContent() = %q, want %q", got, tt.expected)
			}
		})
	}
}
