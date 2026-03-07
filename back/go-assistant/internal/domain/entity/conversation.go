package entity

import "time"

// Conversation holds a session's chat history.
type Conversation struct {
	SessionID    string
	SystemPrompt string
	Messages     []Message
	CreatedAt    time.Time
}
