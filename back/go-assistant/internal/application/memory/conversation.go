// Package memory provides in-memory conversation storage.
package memory

import (
	"sync"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// MessageCallback is called after a message is appended to a session.
type MessageCallback func(sessionID string, msg *entity.Message)

// ConversationMemory is a thread-safe in-memory conversation store.
type ConversationMemory struct {
	mu       sync.RWMutex
	sessions map[string]*entity.Conversation
	onAppend MessageCallback
}

// NewConversationMemory creates an empty ConversationMemory.
func NewConversationMemory() *ConversationMemory {
	return &ConversationMemory{
		sessions: make(map[string]*entity.Conversation),
	}
}

// GetOrCreate returns the conversation for the given session ID,
// creating a new one with the system prompt if it does not exist.
func (m *ConversationMemory) GetOrCreate(sessionID, systemPrompt string) *entity.Conversation {
	m.mu.Lock()
	defer m.mu.Unlock()

	if conv, ok := m.sessions[sessionID]; ok {
		return conv
	}

	conv := &entity.Conversation{
		SessionID:    sessionID,
		SystemPrompt: systemPrompt,
		Messages:     nil,
		CreatedAt:    time.Now(),
	}
	m.sessions[sessionID] = conv
	return conv
}

// SetOnAppend registers a callback that fires after each Append.
func (m *ConversationMemory) SetOnAppend(cb MessageCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onAppend = cb
}

// Append adds a message to the conversation identified by sessionID.
// If the session does not exist, the message is silently dropped and
// the onAppend callback is NOT invoked.
func (m *ConversationMemory) Append(sessionID string, msg *entity.Message) {
	m.mu.Lock()
	var appended bool
	if conv, ok := m.sessions[sessionID]; ok {
		conv.Messages = append(conv.Messages, *msg)
		appended = true
	}
	cb := m.onAppend
	m.mu.Unlock()

	if appended && cb != nil {
		cb(sessionID, msg)
	}
}

// History returns a copy of the messages for the given session.
// Returns nil if the session does not exist.
func (m *ConversationMemory) History(sessionID string) []entity.Message {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conv, ok := m.sessions[sessionID]
	if !ok {
		return nil
	}

	msgs := make([]entity.Message, len(conv.Messages))
	copy(msgs, conv.Messages)
	return msgs
}

// Clear removes all messages from the conversation identified by sessionID.
// If the session does not exist, this is a no-op.
func (m *ConversationMemory) Clear(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if conv, ok := m.sessions[sessionID]; ok {
		conv.Messages = nil
	}
}

// Delete removes the entire session entry from memory.
// Unlike Clear, which only nils the messages, Delete frees the session
// completely. This is appropriate for ephemeral sessions such as subagent
// child contexts. If the session does not exist, this is a no-op.
func (m *ConversationMemory) Delete(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, sessionID)
}

// MessageCount returns the number of messages for the given session.
// Returns 0 if the session does not exist.
func (m *ConversationMemory) MessageCount(sessionID string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if conv, ok := m.sessions[sessionID]; ok {
		return len(conv.Messages)
	}
	return 0
}

// Rewind truncates the conversation to keep only the first keepMessages messages.
// If keepMessages >= current length, this is a no-op.
// If the session does not exist, this is a no-op.
func (m *ConversationMemory) Rewind(sessionID string, keepMessages int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	conv, ok := m.sessions[sessionID]
	if !ok || keepMessages < 0 {
		return
	}
	if keepMessages >= len(conv.Messages) {
		return
	}
	conv.Messages = conv.Messages[:keepMessages]
}

// ReplaceMessages replaces the entire message history for a session.
// If the session does not exist, this is a no-op.
func (m *ConversationMemory) ReplaceMessages(sessionID string, msgs []entity.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if conv, ok := m.sessions[sessionID]; ok {
		conv.Messages = make([]entity.Message, len(msgs))
		copy(conv.Messages, msgs)
	}
}

// Trim keeps only the last keepLast logical message units in the
// conversation. A tool call group (assistant tool_calls message +
// all following tool result messages) counts as a single logical
// unit so that groups are never split. The system prompt is managed
// separately via Conversation.SystemPrompt, so Trim operates only
// on Messages.
func (m *ConversationMemory) Trim(sessionID string, keepLast int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	conv, ok := m.sessions[sessionID]
	if !ok || keepLast <= 0 {
		return
	}

	var systemMsgs []entity.Message
	var otherMsgs []entity.Message

	for _, msg := range conv.Messages {
		if msg.Role == valueobject.RoleSystem {
			systemMsgs = append(systemMsgs, msg)
		} else {
			otherMsgs = append(otherMsgs, msg)
		}
	}

	groups := groupMessages(otherMsgs)

	if len(groups) > keepLast {
		groups = groups[len(groups)-keepLast:]
	}

	var trimmed []entity.Message
	for _, g := range groups {
		trimmed = append(trimmed, g...)
	}

	result := make([]entity.Message, 0, len(systemMsgs)+len(trimmed))
	result = append(result, systemMsgs...)
	result = append(result, trimmed...)
	conv.Messages = result
}

// groupMessages partitions messages into logical units. An assistant
// message with ToolCalls and all consecutive RoleTool messages that
// follow it form a single group. Every other message is its own group.
func groupMessages(msgs []entity.Message) [][]entity.Message {
	var groups [][]entity.Message
	i := 0
	for i < len(msgs) {
		msg := msgs[i]
		if msg.Role == valueobject.RoleAssistant && len(msg.ToolCalls) > 0 {
			group := []entity.Message{msg}
			i++
			for i < len(msgs) && msgs[i].Role == valueobject.RoleTool {
				group = append(group, msgs[i])
				i++
			}
			groups = append(groups, group)
		} else {
			groups = append(groups, []entity.Message{msg})
			i++
		}
	}
	return groups
}
