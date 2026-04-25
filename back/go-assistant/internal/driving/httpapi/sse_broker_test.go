package httpapi

import (
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestSSEBroker_Subscribe(t *testing.T) {
	t.Parallel()
	broker := NewSSEBroker(newTestLogger())

	client, cleanup := broker.Subscribe("session-1")
	defer cleanup()

	if client == nil {
		t.Fatal("Subscribe returned nil client")
	}
	if client.events == nil {
		t.Fatal("client.events channel is nil")
	}
	if client.done == nil {
		t.Fatal("client.done channel is nil")
	}
	if client.sessionID != "session-1" {
		t.Errorf("client.sessionID = %q, want %q", client.sessionID, "session-1")
	}
	if client.id == "" {
		t.Error("client.id is empty")
	}
}

func TestSSEBroker_SubscribeMultiple(t *testing.T) {
	t.Parallel()
	broker := NewSSEBroker(newTestLogger())

	c1, cleanup1 := broker.Subscribe("session-1")
	defer cleanup1()
	c2, cleanup2 := broker.Subscribe("session-1")
	defer cleanup2()

	if c1.id == c2.id {
		t.Error("two clients for same session have identical IDs")
	}
	if broker.ClientCount("session-1") != 2 {
		t.Errorf("ClientCount = %d, want 2", broker.ClientCount("session-1"))
	}
}

func TestSSEBroker_Publish(t *testing.T) {
	t.Parallel()
	broker := NewSSEBroker(newTestLogger())

	client, cleanup := broker.Subscribe("session-1")
	defer cleanup()

	broker.Publish("session-1", "test_event", []byte(`{"key":"value"}`))

	select {
	case msg := <-client.events:
		got := string(msg)
		if !strings.Contains(got, "event: test_event") {
			t.Errorf("event missing event type, got:\n%s", got)
		}
		if !strings.Contains(got, `data: {"key":"value"}`) {
			t.Errorf("event missing data, got:\n%s", got)
		}
		if !strings.Contains(got, "id: ") {
			t.Errorf("event missing id, got:\n%s", got)
		}
	default:
		t.Fatal("no event received on client channel")
	}
}

func TestSSEBroker_PublishMultipleClients(t *testing.T) {
	t.Parallel()
	broker := NewSSEBroker(newTestLogger())

	c1, cleanup1 := broker.Subscribe("session-1")
	defer cleanup1()
	c2, cleanup2 := broker.Subscribe("session-1")
	defer cleanup2()

	broker.Publish("session-1", "broadcast", []byte("hello"))

	for _, c := range []*sseClient{c1, c2} {
		select {
		case msg := <-c.events:
			if !strings.Contains(string(msg), "data: hello") {
				t.Errorf("client %s: unexpected event content: %s", c.id, msg)
			}
		default:
			t.Errorf("client %s: no event received", c.id)
		}
	}
}

func TestSSEBroker_PublishNoClients(t *testing.T) {
	t.Parallel()
	broker := NewSSEBroker(newTestLogger())

	// Should not panic
	broker.Publish("nonexistent-session", "test", []byte("data"))
}

func TestSSEBroker_PublishDifferentSession(t *testing.T) {
	t.Parallel()
	broker := NewSSEBroker(newTestLogger())

	clientA, cleanupA := broker.Subscribe("session-A")
	defer cleanupA()
	clientB, cleanupB := broker.Subscribe("session-B")
	defer cleanupB()

	broker.Publish("session-A", "event_a", []byte("for A"))

	select {
	case <-clientA.events:
		// expected
	default:
		t.Error("client A should have received the event")
	}

	select {
	case msg := <-clientB.events:
		t.Errorf("client B should NOT have received event, got: %s", msg)
	default:
		// expected
	}
}

func TestSSEBroker_Cleanup(t *testing.T) {
	t.Parallel()
	broker := NewSSEBroker(newTestLogger())

	client, cleanup := broker.Subscribe("session-1")
	cleanup()

	// After cleanup, done channel should be closed
	select {
	case <-client.done:
		// expected
	default:
		t.Error("client.done should be closed after cleanup")
	}

	// Client count should be zero
	if broker.ClientCount("session-1") != 0 {
		t.Errorf("ClientCount = %d after cleanup, want 0", broker.ClientCount("session-1"))
	}

	// Publishing after cleanup should not panic
	broker.Publish("session-1", "test", []byte("data"))
}

func TestSSEBroker_SlowClient(t *testing.T) {
	t.Parallel()
	broker := NewSSEBroker(newTestLogger())

	client, cleanup := broker.Subscribe("session-1")
	defer cleanup()

	// Fill the buffer
	for i := 0; i < DefaultClientBuffer; i++ {
		broker.Publish("session-1", "fill", []byte("data"))
	}

	// This should be dropped, not block
	broker.Publish("session-1", "overflow", []byte("dropped"))

	// Drain and count
	count := 0
	for {
		select {
		case <-client.events:
			count++
		default:
			goto done
		}
	}
done:
	if count != DefaultClientBuffer {
		t.Errorf("received %d events, want %d (buffer size)", count, DefaultClientBuffer)
	}
}

func TestSSEBroker_ClientCount(t *testing.T) {
	t.Parallel()
	broker := NewSSEBroker(newTestLogger())

	if broker.ClientCount("empty") != 0 {
		t.Errorf("ClientCount for empty session = %d, want 0", broker.ClientCount("empty"))
	}

	_, cleanup1 := broker.Subscribe("session-1")
	_, cleanup2 := broker.Subscribe("session-1")
	_, cleanup3 := broker.Subscribe("session-2")

	if broker.ClientCount("session-1") != 2 {
		t.Errorf("ClientCount(session-1) = %d, want 2", broker.ClientCount("session-1"))
	}
	if broker.ClientCount("session-2") != 1 {
		t.Errorf("ClientCount(session-2) = %d, want 1", broker.ClientCount("session-2"))
	}

	cleanup1()
	if broker.ClientCount("session-1") != 1 {
		t.Errorf("ClientCount(session-1) after cleanup = %d, want 1", broker.ClientCount("session-1"))
	}

	cleanup2()
	cleanup3()
}

func TestSSEBroker_PublishEvent(t *testing.T) {
	t.Parallel()
	broker := NewSSEBroker(newTestLogger())

	client, cleanup := broker.Subscribe("session-1")
	defer cleanup()

	evt := cpn.Event{
		ID:        "evt-1",
		Type:      cpn.EventTransitionFired,
		SessionID: "session-1",
		CPNID:     "cpn-root",
	}
	broker.PublishEvent("session-1", &evt)

	select {
	case msg := <-client.events:
		got := string(msg)
		if !strings.Contains(got, "event: transition_fired") {
			t.Errorf("missing event type in SSE output:\n%s", got)
		}
		// Verify data is valid JSON containing the event fields
		dataLine := extractDataLine(got)
		var parsed cpn.Event
		if err := json.Unmarshal([]byte(dataLine), &parsed); err != nil {
			t.Fatalf("data is not valid JSON: %v\ndata: %s", err, dataLine)
		}
		if parsed.ID != "evt-1" {
			t.Errorf("parsed event ID = %q, want %q", parsed.ID, "evt-1")
		}
	default:
		t.Fatal("no event received")
	}
}

func TestSSEBroker_PublishStreamChunk(t *testing.T) {
	t.Parallel()
	broker := NewSSEBroker(newTestLogger())

	client, cleanup := broker.Subscribe("session-1")
	defer cleanup()

	chunk := cpn.StreamChunk{
		SessionID: "session-1",
		CPNID:     "cpn-root",
		CPNRole:   "assistant",
		Content:   "Hello",
		Done:      false,
	}
	broker.PublishStreamChunk("session-1", chunk)

	select {
	case msg := <-client.events:
		got := string(msg)
		if !strings.Contains(got, "event: stream_chunk") {
			t.Errorf("missing event type in SSE output:\n%s", got)
		}
		dataLine := extractDataLine(got)
		var parsed cpn.StreamChunk
		if err := json.Unmarshal([]byte(dataLine), &parsed); err != nil {
			t.Fatalf("data is not valid JSON: %v\ndata: %s", err, dataLine)
		}
		if parsed.Content != "Hello" {
			t.Errorf("parsed chunk Content = %q, want %q", parsed.Content, "Hello")
		}
	default:
		t.Fatal("no event received")
	}
}

func TestSSEBroker_ConcurrentPublishSubscribe(t *testing.T) {
	t.Parallel()
	broker := NewSSEBroker(newTestLogger())

	var wg sync.WaitGroup
	const numGoroutines = 50

	// Concurrent subscribes
	cleanups := make([]func(), numGoroutines)
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, cleanup := broker.Subscribe("session-race")
			mu.Lock()
			cleanups[idx] = cleanup
			mu.Unlock()
		}(i)
	}

	// Concurrent publishes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			broker.Publish("session-race", "race_event", []byte("data"))
		}()
	}

	wg.Wait()

	// Cleanup all
	for _, cleanup := range cleanups {
		if cleanup != nil {
			cleanup()
		}
	}

	if broker.ClientCount("session-race") != 0 {
		t.Errorf("ClientCount after all cleanups = %d, want 0", broker.ClientCount("session-race"))
	}
}

func TestSSEBroker_EventIDs(t *testing.T) {
	t.Parallel()
	broker := NewSSEBroker(newTestLogger())

	client, cleanup := broker.Subscribe("session-1")
	defer cleanup()

	broker.Publish("session-1", "evt", []byte("a"))
	broker.Publish("session-1", "evt", []byte("b"))
	broker.Publish("session-1", "evt", []byte("c"))

	var ids []string
	for i := 0; i < 3; i++ {
		select {
		case msg := <-client.events:
			id := extractIDField(string(msg))
			ids = append(ids, id)
		default:
			t.Fatalf("expected event %d, got none", i)
		}
	}

	// Verify monotonically increasing
	for i := 1; i < len(ids); i++ {
		if ids[i] <= ids[i-1] {
			t.Errorf("event IDs not monotonically increasing: %v", ids)
		}
	}
}

func TestSSEBroker_CleanupDoubleCall(t *testing.T) {
	t.Parallel()
	broker := NewSSEBroker(newTestLogger())

	client, cleanup := broker.Subscribe("session-dc")

	// First cleanup should work normally.
	cleanup()

	select {
	case <-client.done:
		// expected — done channel closed
	default:
		t.Error("client.done should be closed after first cleanup")
	}

	if broker.ClientCount("session-dc") != 0 {
		t.Errorf("ClientCount = %d after cleanup, want 0", broker.ClientCount("session-dc"))
	}

	// Second cleanup must not panic.
	cleanup()
}

func TestSSEBroker_BacklogReplayOnLateSubscribe(t *testing.T) {
	t.Parallel()
	broker := NewSSEBroker(newTestLogger())

	// Publish N events BEFORE any client subscribes.
	const N = 5
	for i := 0; i < N; i++ {
		broker.Publish("session-late", "evt", []byte{byte('a' + i)})
	}

	if got := broker.BacklogLen("session-late"); got != N {
		t.Fatalf("BacklogLen = %d, want %d", got, N)
	}

	client, cleanup := broker.Subscribe("session-late")
	defer cleanup()

	// Client should receive all N replayed events in publish order.
	for i := 0; i < N; i++ {
		select {
		case msg := <-client.events:
			want := string([]byte{byte('a' + i)})
			if !strings.Contains(string(msg), "data: "+want) {
				t.Errorf("event %d: expected data %q, got:\n%s", i, want, msg)
			}
		default:
			t.Fatalf("expected replayed event %d, got none", i)
		}
	}

	// No more events queued.
	select {
	case msg := <-client.events:
		t.Errorf("unexpected extra event after backlog replay: %s", msg)
	default:
	}
}

func TestSSEBroker_BacklogRingEviction(t *testing.T) {
	t.Parallel()
	const ringSize = 4
	broker := NewSSEBrokerWithBacklog(newTestLogger(), ringSize)

	// Publish 2x ring capacity before any subscriber.
	total := ringSize * 2
	for i := 0; i < total; i++ {
		broker.Publish("s", "evt", []byte{byte('0' + i)})
	}

	if got := broker.BacklogLen("s"); got != ringSize {
		t.Fatalf("BacklogLen = %d, want %d", got, ringSize)
	}

	client, cleanup := broker.Subscribe("s")
	defer cleanup()

	// Should receive only the most-recent ringSize events, in order.
	for i := 0; i < ringSize; i++ {
		expectByte := byte('0' + (total - ringSize + i))
		select {
		case msg := <-client.events:
			if !strings.Contains(string(msg), "data: "+string([]byte{expectByte})) {
				t.Errorf("replayed event %d: expected byte %c, got:\n%s", i, expectByte, msg)
			}
		default:
			t.Fatalf("expected replayed event %d, got none", i)
		}
	}

	select {
	case msg := <-client.events:
		t.Errorf("unexpected extra event: %s", msg)
	default:
	}
}

func TestSSEBroker_BacklogDisabled(t *testing.T) {
	t.Parallel()
	broker := NewSSEBrokerWithBacklog(newTestLogger(), 0)

	broker.Publish("s", "evt", []byte("a"))
	if got := broker.BacklogLen("s"); got != 0 {
		t.Errorf("BacklogLen with disabled backlog = %d, want 0", got)
	}

	client, cleanup := broker.Subscribe("s")
	defer cleanup()

	select {
	case msg := <-client.events:
		t.Errorf("unexpected replay with backlog disabled: %s", msg)
	default:
	}
}

func TestSSEBroker_BacklogConcurrentPublishLateSubscribe(t *testing.T) {
	t.Parallel()
	broker := NewSSEBroker(newTestLogger())

	var wg sync.WaitGroup
	const publishers = 10
	const perPublisher = 20

	for i := 0; i < publishers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < perPublisher; j++ {
				broker.Publish("race", "evt", []byte{byte(idx), byte(j)})
			}
		}(i)
	}

	// Late subscriber, joining while publishes are in flight.
	wg.Add(1)
	go func() {
		defer wg.Done()
		client, cleanup := broker.Subscribe("race")
		defer cleanup()
		// Drain whatever is replayed; we only care that the code is race-free
		// and that the client receives at least one event.
		got := 0
		for {
			select {
			case <-client.events:
				got++
			default:
				if got > 0 {
					return
				}
				// keep polling briefly until we see something
			}
		}
	}()

	wg.Wait()

	// Total events published must equal publishers * perPublisher; the backlog
	// length is capped by DefaultBacklogSize.
	if bl := broker.BacklogLen("race"); bl > DefaultBacklogSize {
		t.Errorf("BacklogLen = %d exceeds cap %d", bl, DefaultBacklogSize)
	}
}

// extractDataLine extracts the value after "data: " from an SSE message.
func extractDataLine(sse string) string {
	for _, line := range strings.Split(sse, "\n") {
		if strings.HasPrefix(line, "data: ") {
			return strings.TrimPrefix(line, "data: ")
		}
	}
	return ""
}

// extractIDField extracts the value after "id: " from an SSE message.
func extractIDField(sse string) string {
	for _, line := range strings.Split(sse, "\n") {
		if strings.HasPrefix(line, "id: ") {
			return strings.TrimPrefix(line, "id: ")
		}
	}
	return ""
}
