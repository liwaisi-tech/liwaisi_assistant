// Package session provides JSONL-based conversation session persistence.
package session

import (
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// RecordType discriminates JSONL record kinds.
type RecordType string

const (
	RecordMetadata RecordType = "metadata"
	RecordMessage  RecordType = "message"
	RecordRewind   RecordType = "rewind"
	RecordClear    RecordType = "clear"
)

// Record is the serializable JSONL envelope for all record types.
type Record struct {
	Type RecordType `json:"type"`

	// Metadata fields (type == "metadata").
	SessionID   string `json:"session_id,omitempty"`
	ProjectHash string `json:"project_hash,omitempty"`
	ProjectPath string `json:"project_path,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
	Model       string `json:"model,omitempty"`

	// Message fields (type == "message").
	Role       string                 `json:"role,omitempty"`
	Content    string                 `json:"content,omitempty"`
	Timestamp  string                 `json:"timestamp,omitempty"`
	ToolCalls  []valueobject.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string                 `json:"tool_call_id,omitempty"`

	// Rewind fields (type == "rewind").
	ToIndex int `json:"to_index"`
}

// Summary holds metadata for the session browser.
type Summary struct {
	SessionID    string
	CreatedAt    time.Time
	LastActivity time.Time
	MessageCount int
	FirstUserMsg string
	Model        string
}

// MessageToRecord converts a domain Message to a JSONL Record.
func MessageToRecord(msg *entity.Message) Record {
	r := Record{
		Type:      RecordMessage,
		Role:      string(msg.Role),
		Content:   msg.Content,
		Timestamp: msg.Timestamp.UTC().Format(time.RFC3339),
	}
	if len(msg.ToolCalls) > 0 {
		r.ToolCalls = msg.ToolCalls
	}
	if msg.ToolCallID != "" {
		r.ToolCallID = msg.ToolCallID
	}
	return r
}

// RecordToMessage converts a JSONL Record back to a domain Message.
func RecordToMessage(r *Record) entity.Message {
	ts, _ := time.Parse(time.RFC3339, r.Timestamp)
	return entity.Message{
		Role:       valueobject.Role(r.Role),
		Content:    r.Content,
		Timestamp:  ts,
		ToolCalls:  r.ToolCalls,
		ToolCallID: r.ToolCallID,
	}
}
