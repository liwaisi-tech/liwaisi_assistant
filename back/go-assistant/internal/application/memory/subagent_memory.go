package memory

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// SubAgentMemoryFactory creates isolated conversation contexts for subagents.
// Each child context is backed by the same ConversationMemory but uses a
// unique session ID, ensuring the subagent's conversation is fully isolated
// from the parent and sibling subagents.
type SubAgentMemoryFactory struct {
	memory *ConversationMemory
}

// NewSubAgentMemoryFactory creates a factory backed by the given memory store.
func NewSubAgentMemoryFactory(memory *ConversationMemory) *SubAgentMemoryFactory {
	return &SubAgentMemoryFactory{memory: memory}
}

// ChildSessionID generates a unique session ID for a subagent execution.
// Format: {parentSessionID}:subagent:{agentName}:{random8}
// The deterministic prefix aids debugging while the random suffix guarantees
// uniqueness across concurrent or repeated invocations.
func ChildSessionID(parentSessionID, agentName string) string {
	return fmt.Sprintf("%s:subagent:%s:%s", parentSessionID, agentName, randomHex8())
}

// CreateChildContext creates an isolated conversation for a subagent.
// The child conversation is seeded with the subagent's instruction as the
// system prompt. If spec.Context is non-empty, it is appended as the first
// user message to provide task-specific information.
// The returned string is the child session ID.
func (f *SubAgentMemoryFactory) CreateChildContext(
	parentSessionID string,
	spec *entity.SubAgentSpec,
) string {
	childID := ChildSessionID(parentSessionID, spec.Name)

	f.memory.GetOrCreate(childID, spec.Instruction)

	if spec.Context != "" {
		msg := entity.NewMessage(valueobject.RoleUser, spec.Context)
		f.memory.Append(childID, &msg)
	}

	return childID
}

// Append adds a message to a child session's conversation.
func (f *SubAgentMemoryFactory) Append(childSessionID string, msg *entity.Message) {
	f.memory.Append(childSessionID, msg)
}

// ChildHistory returns the message history for a child session.
func (f *SubAgentMemoryFactory) ChildHistory(childSessionID string) []entity.Message {
	return f.memory.History(childSessionID)
}

// CleanupChild removes a child session from memory entirely, freeing
// all associated resources. After cleanup, the session ID is invalid.
func (f *SubAgentMemoryFactory) CleanupChild(childSessionID string) {
	f.memory.Delete(childSessionID)
}

// randomHex8 returns an 8-character random hex string using crypto/rand.
func randomHex8() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return hex.EncodeToString(b)
}
