package httpapi

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// HTTPChannelAdapter implements cpn.ChannelAdapter for the HTTP/SSE channel.
// It bridges HTTP POST /messages (incoming) and SSE stream (outgoing).
type HTTPChannelAdapter struct {
	sessionID string
	broker    *SSEBroker
	incoming  chan cpn.Message
	channel   cpn.ChannelType
}

// NewHTTPChannelAdapter creates a new adapter for the given session.
func NewHTTPChannelAdapter(sessionID string, broker *SSEBroker) *HTTPChannelAdapter {
	return &HTTPChannelAdapter{
		sessionID: sessionID,
		broker:    broker,
		incoming:  make(chan cpn.Message, 16),
		channel:   cpn.ChannelWeb,
	}
}

// Send delivers a stream chunk to connected SSE clients.
func (a *HTTPChannelAdapter) Send(ctx context.Context, chunk cpn.StreamChunk) error {
	a.broker.PublishStreamChunk(a.sessionID, chunk)
	return nil
}

// Receive blocks until a user message arrives from the HTTP API.
func (a *HTTPChannelAdapter) Receive(ctx context.Context) (cpn.Message, error) {
	select {
	case msg := <-a.incoming:
		return msg, nil
	case <-ctx.Done():
		return cpn.Message{}, ctx.Err()
	}
}

// Channel returns the channel type (web).
func (a *HTTPChannelAdapter) Channel() cpn.ChannelType {
	return a.channel
}

// Inject pushes a message from the HTTP handler into the adapter.
// Called by POST /messages handler.
func (a *HTTPChannelAdapter) Inject(msg *cpn.Message) {
	select {
	case a.incoming <- *msg:
	default:
		// Drop if buffer full — this shouldn't happen in practice
	}
}
