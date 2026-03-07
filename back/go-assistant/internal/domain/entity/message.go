package entity

import (
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// Message represents a single chat message in a conversation.
type Message struct {
	Role       valueobject.Role
	Content    string
	Timestamp  time.Time
	ToolCalls  []valueobject.ToolCall // populated when Role == RoleAssistant
	ToolCallID string                 // populated when Role == RoleTool
}

// NewMessage creates a Message with the current timestamp.
func NewMessage(role valueobject.Role, content string) Message {
	return Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	}
}

// NewToolCallMessage creates an assistant message containing tool calls.
func NewToolCallMessage(toolCalls []valueobject.ToolCall) Message {
	return Message{
		Role:      valueobject.RoleAssistant,
		Content:   "",
		ToolCalls: toolCalls,
		Timestamp: time.Now(),
	}
}

// NewToolResultMessage creates a tool result message.
func NewToolResultMessage(toolCallID, content string) Message {
	return Message{
		Role:       valueobject.RoleTool,
		Content:    content,
		ToolCallID: toolCallID,
		Timestamp:  time.Now(),
	}
}
