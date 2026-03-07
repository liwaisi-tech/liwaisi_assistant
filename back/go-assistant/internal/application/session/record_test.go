package session

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func TestMessageToRecord_RoundTrip(t *testing.T) {
	tests := []struct {
		name string
		msg  entity.Message
	}{
		{
			name: "user message",
			msg: entity.Message{
				Role:      valueobject.RoleUser,
				Content:   "hello world",
				Timestamp: time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "assistant message with tool calls",
			msg: entity.Message{
				Role:      valueobject.RoleAssistant,
				Timestamp: time.Date(2026, 3, 4, 10, 0, 1, 0, time.UTC),
				ToolCalls: []valueobject.ToolCall{
					{ID: "call_1", Type: "function", Function: valueobject.FunctionCall{Name: "tree", Arguments: `{"path":"."}`}},
				},
			},
		},
		{
			name: "tool result message",
			msg: entity.Message{
				Role:       valueobject.RoleTool,
				Content:    `{"result": "ok"}`,
				Timestamp:  time.Date(2026, 3, 4, 10, 0, 2, 0, time.UTC),
				ToolCallID: "call_1",
			},
		},
		{
			name: "assistant text response",
			msg: entity.Message{
				Role:      valueobject.RoleAssistant,
				Content:   "Here are the files.",
				Timestamp: time.Date(2026, 3, 4, 10, 0, 3, 0, time.UTC),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := MessageToRecord(&tt.msg)

			data, err := json.Marshal(rec)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var decoded Record
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}

			got := RecordToMessage(&decoded)

			if got.Role != tt.msg.Role {
				t.Errorf("Role = %q, want %q", got.Role, tt.msg.Role)
			}
			if got.Content != tt.msg.Content {
				t.Errorf("Content = %q, want %q", got.Content, tt.msg.Content)
			}
			if got.ToolCallID != tt.msg.ToolCallID {
				t.Errorf("ToolCallID = %q, want %q", got.ToolCallID, tt.msg.ToolCallID)
			}
			if len(got.ToolCalls) != len(tt.msg.ToolCalls) {
				t.Errorf("ToolCalls len = %d, want %d", len(got.ToolCalls), len(tt.msg.ToolCalls))
			}
			if !got.Timestamp.Equal(tt.msg.Timestamp) {
				t.Errorf("Timestamp = %v, want %v", got.Timestamp, tt.msg.Timestamp)
			}
		})
	}
}

func TestRecord_JSON_Metadata(t *testing.T) {
	rec := Record{
		Type:        RecordMetadata,
		SessionID:   "chat-123",
		ProjectHash: "a1b2c3d4",
		ProjectPath: "/home/user/project",
		CreatedAt:   "2026-03-04T10:00:00Z",
		Model:       "test-model",
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Record
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Type != RecordMetadata {
		t.Errorf("Type = %q, want %q", decoded.Type, RecordMetadata)
	}
	if decoded.SessionID != rec.SessionID {
		t.Errorf("SessionID = %q, want %q", decoded.SessionID, rec.SessionID)
	}
	if decoded.Model != rec.Model {
		t.Errorf("Model = %q, want %q", decoded.Model, rec.Model)
	}
}

func TestRecord_JSON_Rewind(t *testing.T) {
	rec := Record{
		Type:      RecordRewind,
		ToIndex:   3,
		Timestamp: "2026-03-04T10:05:00Z",
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Record
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Type != RecordRewind {
		t.Errorf("Type = %q, want %q", decoded.Type, RecordRewind)
	}
	if decoded.ToIndex != 3 {
		t.Errorf("ToIndex = %d, want 3", decoded.ToIndex)
	}
}

func TestRecord_JSON_Clear(t *testing.T) {
	rec := Record{
		Type:      RecordClear,
		Timestamp: "2026-03-04T10:10:00Z",
	}

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded Record
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.Type != RecordClear {
		t.Errorf("Type = %q, want %q", decoded.Type, RecordClear)
	}
}
