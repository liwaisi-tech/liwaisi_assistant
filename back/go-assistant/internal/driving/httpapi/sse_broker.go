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

	// DefaultBacklogSize is the per-session ring-buffer capacity for recent
	// events. When a new client subscribes, the most recent events in the
	// backlog are replayed to the client in publish order.
	DefaultBacklogSize = 256
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
//
// A per-session bounded ring buffer retains the most recent events, so a
// client that subscribes after events have been published still receives
// recent activity on connect. This prevents event loss when the HTTP client
// opens the SSE stream after POST-ing a request that triggers CPN firings.
type SSEBroker struct {
	clients     map[string]map[string]*sseClient // sessionID -> clientID -> client
	backlogs    map[string]*ringBuffer           // sessionID -> bounded ring
	backlogSize int
	mu          sync.RWMutex
	logger      *slog.Logger
	eventID     atomic.Int64 // Monotonically increasing event ID
}

// ringBuffer is a simple bounded FIFO of pre-formatted SSE byte payloads.
// Not safe for concurrent use; callers must hold SSEBroker.mu.
type ringBuffer struct {
	data [][]byte
	cap  int
}

func newRingBuffer(capacity int) *ringBuffer {
	return &ringBuffer{
		data: make([][]byte, 0, capacity),
		cap:  capacity,
	}
}

func (r *ringBuffer) push(b []byte) {
	if r.cap <= 0 {
		return
	}
	if len(r.data) < r.cap {
		r.data = append(r.data, b)
		return
	}
	// Drop oldest: shift left and overwrite tail.
	copy(r.data, r.data[1:])
	r.data[r.cap-1] = b
}

// snapshot returns a copy of the current backlog contents in publish order.
func (r *ringBuffer) snapshot() [][]byte {
	out := make([][]byte, len(r.data))
	copy(out, r.data)
	return out
}

// NewSSEBroker creates an initialized SSEBroker using DefaultBacklogSize.
func NewSSEBroker(logger *slog.Logger) *SSEBroker {
	return NewSSEBrokerWithBacklog(logger, DefaultBacklogSize)
}

// NewSSEBrokerWithBacklog creates an SSEBroker with a configurable per-session
// backlog ring size. A size <= 0 disables the backlog.
func NewSSEBrokerWithBacklog(logger *slog.Logger, backlogSize int) *SSEBroker {
	if backlogSize < 0 {
		backlogSize = 0
	}
	return &SSEBroker{
		clients:     make(map[string]map[string]*sseClient),
		backlogs:    make(map[string]*ringBuffer),
		backlogSize: backlogSize,
		logger:      logger,
	}
}

// Subscribe registers a new SSE client for a session.
// Returns the client and a cleanup function that MUST be called on disconnect.
//
// Any events currently in the session's backlog are replayed into the client's
// event channel before Subscribe returns, so that an SSE stream opened after
// the first events have been published still sees the recent history.
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

	// Replay backlog while holding the lock so we preserve ordering with any
	// concurrent Publish: no new events can be appended to the ring between
	// the snapshot and the client becoming a live subscriber.
	var replay [][]byte
	if ring, ok := b.backlogs[sessionID]; ok {
		replay = ring.snapshot()
	}
	b.mu.Unlock()

	replayed := 0
	dropped := 0
	for _, evt := range replay {
		select {
		case client.events <- evt:
			replayed++
		default:
			dropped++
		}
	}

	b.logger.Info("SSE client subscribed",
		slog.String("session_id", sessionID),
		slog.String("client_id", clientID),
		slog.Int("backlog_replayed", replayed),
		slog.Int("backlog_dropped", dropped),
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
// The event is also appended to the session's bounded backlog for replay on
// future Subscribe calls.
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

	b.mu.Lock()
	// Append to backlog first so a late subscriber that races with this
	// publish still observes the event via snapshot.
	if b.backlogSize > 0 {
		ring, ok := b.backlogs[sessionID]
		if !ok {
			ring = newRingBuffer(b.backlogSize)
			b.backlogs[sessionID] = ring
		}
		ring.push(formatted)
	}
	srcClients := b.clients[sessionID]
	clients := make([]*sseClient, 0, len(srcClients))
	for _, c := range srcClients {
		clients = append(clients, c)
	}
	b.mu.Unlock()

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

// BacklogLen returns the number of events currently retained in the
// session's backlog ring. Primarily useful for tests and diagnostics.
func (b *SSEBroker) BacklogLen(sessionID string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if ring, ok := b.backlogs[sessionID]; ok {
		return len(ring.data)
	}
	return 0
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
