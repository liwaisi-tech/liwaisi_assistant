package httpapi

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

const (
	// DefaultClientBuffer is the buffer capacity for SSE client event channels.
	DefaultClientBuffer = 128
)

// sseClient represents a single SSE client connection.
type sseClient struct {
	id        string
	sessionID string
	events    chan []byte   // Pre-formatted SSE event bytes
	done      chan struct{} // Closed when client disconnects
}

// SSEBroker manages per-session SSE client connections.
// Thread-safe for concurrent Subscribe/Publish/Unsubscribe operations.
type SSEBroker struct {
	clients map[string]map[string]*sseClient // sessionID -> clientID -> client
	mu      sync.RWMutex
	logger  *slog.Logger
	eventID atomic.Int64 // Monotonically increasing event ID
}

// NewSSEBroker creates an initialized SSEBroker.
func NewSSEBroker(logger *slog.Logger) *SSEBroker {
	return &SSEBroker{
		clients: make(map[string]map[string]*sseClient),
		logger:  logger,
	}
}

// Subscribe registers a new SSE client for a session.
// Returns the client and a cleanup function that MUST be called on disconnect.
func (b *SSEBroker) Subscribe(sessionID string) (client *sseClient, cleanup func()) {
	clientID := generateClientID()

	client = &sseClient{
		id:        clientID,
		sessionID: sessionID,
		events:    make(chan []byte, DefaultClientBuffer),
		done:      make(chan struct{}),
	}

	b.mu.Lock()
	if b.clients[sessionID] == nil {
		b.clients[sessionID] = make(map[string]*sseClient)
	}
	b.clients[sessionID][clientID] = client
	b.mu.Unlock()

	b.logger.Info("SSE client subscribed",
		slog.String("session_id", sessionID),
		slog.String("client_id", clientID),
	)

	var closeOnce sync.Once
	cleanup = func() {
		closeOnce.Do(func() {
			b.mu.Lock()
			if clients, ok := b.clients[sessionID]; ok {
				delete(clients, clientID)
				if len(clients) == 0 {
					delete(b.clients, sessionID)
				}
			}
			b.mu.Unlock()
			close(client.done)

			b.logger.Info("SSE client unsubscribed",
				slog.String("session_id", sessionID),
				slog.String("client_id", clientID),
			)
		})
	}

	return client, cleanup
}

// Publish sends a pre-formatted SSE event to all clients subscribed to a session.
// Non-blocking: if a client's buffer is full, the event is dropped and a warning logged.
func (b *SSEBroker) Publish(sessionID, eventType string, data []byte) {
	var buf bytes.Buffer
	eventID := b.NextEventID()
	if err := WriteSSEEvent(&buf, eventID, eventType, data); err != nil {
		b.logger.Error("failed to format SSE event",
			slog.String("session_id", sessionID),
			slog.String("error", err.Error()),
		)
		return
	}
	formatted := buf.Bytes()

	b.mu.RLock()
	srcClients := b.clients[sessionID]
	clients := make([]*sseClient, 0, len(srcClients))
	for _, c := range srcClients {
		clients = append(clients, c)
	}
	b.mu.RUnlock()

	for _, client := range clients {
		select {
		case client.events <- formatted:
		default:
			b.logger.Warn("SSE event dropped for slow client",
				slog.String("session_id", sessionID),
				slog.String("client_id", client.id),
				slog.String("event_type", eventType),
			)
		}
	}
}

// PublishEvent converts a CPN Event to JSON and publishes it as an SSE event.
func (b *SSEBroker) PublishEvent(sessionID string, evt *cpn.Event) {
	data, err := json.Marshal(evt)
	if err != nil {
		b.logger.Error("failed to marshal CPN event",
			slog.String("session_id", sessionID),
			slog.String("error", err.Error()),
		)
		return
	}
	b.Publish(sessionID, string(evt.Type), data)
}

// PublishStreamChunk converts a StreamChunk to JSON and publishes as an SSE stream_chunk event.
func (b *SSEBroker) PublishStreamChunk(sessionID string, chunk cpn.StreamChunk) {
	data, err := json.Marshal(chunk)
	if err != nil {
		b.logger.Error("failed to marshal stream chunk",
			slog.String("session_id", sessionID),
			slog.String("error", err.Error()),
		)
		return
	}
	b.Publish(sessionID, "stream_chunk", data)
}

// ClientCount returns the number of active SSE clients for a session.
func (b *SSEBroker) ClientCount(sessionID string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients[sessionID])
}

// NextEventID returns the next monotonically increasing event ID as a string.
func (b *SSEBroker) NextEventID() string {
	return fmt.Sprintf("%d", b.eventID.Add(1))
}

// generateClientID creates a random 8-byte hex-encoded client identifier.
func generateClientID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
