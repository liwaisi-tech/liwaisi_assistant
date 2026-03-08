package input

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// AgentService defines the input port for agent interactions.
type AgentService interface {
	// Chat sends a user message within a session and returns a channel
	// that streams the assistant's response token by token.
	Chat(ctx context.Context, sessionID string, userMessage string) (<-chan valueobject.StreamChunk, error)

	// Ask sends a single query without session context and returns
	// the complete response.
	Ask(ctx context.Context, query string) (string, error)

	// ChatStream handles a persistent bidirectional channel with the client.
	ChatStream(ctx context.Context, sessionID string, inCh <-chan valueobject.ClientMessage, outCh chan<- valueobject.ServerMessage)
}
