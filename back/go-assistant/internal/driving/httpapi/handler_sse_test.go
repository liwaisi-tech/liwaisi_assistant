package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/app"
)

// newTestHandlers creates a Handlers with a real SessionService backed by a minimal topology.
func newTestHandlers(t *testing.T) *Handlers {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	factory := func(sessionID string) *cpn.CPN {
		places := map[string]*cpn.Place{
			"p-input":  cpn.NewPlace("p-input", cpn.ColorString, cpn.SpaceSurface),
			"p-output": cpn.NewPlace("p-output", cpn.ColorString, cpn.SpaceSurface),
		}
		transitions := map[string]*cpn.Transition{
			"t-echo": cpn.NewTransition("t-echo", cpn.NodeKindTool,
				[]string{"p-input"}, []string{"p-output"}),
		}
		return cpn.NewCPN("cpn-"+sessionID, "test", 0, cpn.ModeMAS, sessionID, places, transitions)
	}
	svc := app.NewSessionService(nil, nil, logger, factory)
	broker := NewSSEBroker(logger)
	return &Handlers{
		App:    svc,
		Broker: broker,
		Logger: logger,
	}
}

func TestHandleSSEStream_NotFound(t *testing.T) {
	t.Parallel()

	h := newTestHandlers(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/sessions/{id}/events", h.HandleSSEStream)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/nonexistent/events", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != "session not found" {
		t.Errorf("expected 'session not found', got %q", resp.Error)
	}
}

func TestHandleSSEStream_MissingID(t *testing.T) {
	t.Parallel()

	h := newTestHandlers(t)

	// Directly invoke handler without path value — simulates missing ID.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions//events", nil)
	rec := httptest.NewRecorder()
	h.HandleSSEStream(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleSSEStream_Headers(t *testing.T) {
	t.Parallel()

	h := newTestHandlers(t)

	// Create a real session.
	info, err := h.App.CreateSession(context.Background(), "user-1", cpn.ChannelWeb)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/sessions/{id}/events", h.HandleSSEStream)

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/sessions/"+info.ID+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected Content-Type 'text/event-stream', got %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("expected Cache-Control 'no-cache', got %q", cc)
	}
	if xab := resp.Header.Get("X-Accel-Buffering"); xab != "no" {
		t.Errorf("expected X-Accel-Buffering 'no', got %q", xab)
	}

	// Read first line — should be the retry directive.
	scanner := bufio.NewScanner(resp.Body)
	if scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "retry:") {
			t.Errorf("expected first line to start with 'retry:', got %q", line)
		}
	}
}

func TestHandleSSEStream_StreamChunk(t *testing.T) {
	t.Parallel()

	h := newTestHandlers(t)

	info, err := h.App.CreateSession(context.Background(), "user-1", cpn.ChannelWeb)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/sessions/{id}/events", h.HandleSSEStream)

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/sessions/"+info.ID+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	// Wait for handler to subscribe.
	time.Sleep(100 * time.Millisecond)

	chunk := cpn.StreamChunk{
		SessionID: info.ID,
		CPNID:     "cpn-1",
		CPNRole:   "assistant",
		Content:   "Hello",
		Done:      false,
	}
	if err := h.App.SendStreamChunk(info.ID, chunk); err != nil {
		t.Fatalf("send chunk: %v", err)
	}

	// Read SSE lines until we find the stream_chunk event.
	scanner := bufio.NewScanner(resp.Body)
	foundChunk := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "event: stream_chunk" {
			foundChunk = true
		}
		if foundChunk && strings.HasPrefix(line, "data: ") {
			dataStr := strings.TrimPrefix(line, "data: ")
			var got cpn.StreamChunk
			if err := json.Unmarshal([]byte(dataStr), &got); err != nil {
				t.Fatalf("unmarshal chunk: %v", err)
			}
			if got.Content != "Hello" {
				t.Errorf("expected Content 'Hello', got %q", got.Content)
			}
			if got.CPNID != "cpn-1" {
				t.Errorf("expected CPNID 'cpn-1', got %q", got.CPNID)
			}
			break
		}
	}
	if !foundChunk {
		t.Error("did not receive stream_chunk event")
	}
}

func TestHandleSSEStream_BrokerEvent(t *testing.T) {
	t.Parallel()

	h := newTestHandlers(t)

	info, err := h.App.CreateSession(context.Background(), "user-1", cpn.ChannelWeb)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/sessions/{id}/events", h.HandleSSEStream)

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/sessions/"+info.ID+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	// Wait briefly for the SSE handler to subscribe to the broker.
	time.Sleep(100 * time.Millisecond)

	// Publish a domain event via the broker.
	eventPayload, _ := json.Marshal(map[string]string{"msg": "test"})
	h.Broker.Publish(info.ID, "test_event", eventPayload)

	// Read lines until we find the test_event.
	scanner := bufio.NewScanner(resp.Body)
	foundEvent := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "event: test_event" {
			foundEvent = true
		}
		if foundEvent && strings.HasPrefix(line, "data: ") {
			dataStr := strings.TrimPrefix(line, "data: ")
			var got map[string]string
			if err := json.Unmarshal([]byte(dataStr), &got); err != nil {
				t.Fatalf("unmarshal event: %v", err)
			}
			if got["msg"] != "test" {
				t.Errorf("expected msg 'test', got %q", got["msg"])
			}
			break
		}
	}
	if !foundEvent {
		t.Error("did not receive test_event from broker")
	}
}

func TestHandleSSEStream_StreamClose(t *testing.T) {
	t.Parallel()

	h := newTestHandlers(t)

	info, err := h.App.CreateSession(context.Background(), "user-1", cpn.ChannelWeb)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/sessions/{id}/events", h.HandleSSEStream)

	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/sessions/"+info.ID+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()

	// Wait for handler to be ready, then close the stream channel.
	time.Sleep(100 * time.Millisecond)
	if err := h.App.CloseStream(info.ID); err != nil {
		t.Fatalf("close stream: %v", err)
	}

	// Read lines until we find the session_completed event.
	scanner := bufio.NewScanner(resp.Body)
	foundCompleted := false
	for scanner.Scan() {
		line := scanner.Text()
		if line == "event: session_completed" {
			foundCompleted = true
		}
		if foundCompleted && strings.HasPrefix(line, "data: ") {
			dataStr := strings.TrimPrefix(line, "data: ")
			var got map[string]string
			if err := json.Unmarshal([]byte(dataStr), &got); err != nil {
				t.Fatalf("unmarshal event: %v", err)
			}
			if got["status"] != "stream_closed" {
				t.Errorf("expected status 'stream_closed', got %q", got["status"])
			}
			break
		}
	}
	if !foundCompleted {
		t.Error("did not receive session_completed event")
	}
}
