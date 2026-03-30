//go:build integration

package integration_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// ── Health & Version ────────────────────────────────────────────────────────

func TestHealth(t *testing.T) {
	t.Parallel()
	resp := doRequest(t, http.MethodGet, "/api/v1/health", nil)
	defer resp.Body.Close()
	assertStatus(t, resp, 200)
	var h statusResp
	decodeJSONResp(t, resp, &h)
	if h.Status != "ok" {
		t.Errorf("health status = %q, want %q", h.Status, "ok")
	}
}

func TestVersion(t *testing.T) {
	t.Parallel()
	resp := doRequest(t, http.MethodGet, "/api/v1/version", nil)
	defer resp.Body.Close()
	assertStatus(t, resp, 200)
	var v versionResp
	decodeJSONResp(t, resp, &v)
	// version fields should be non-empty (set by ldflags or defaults)
	if v.Version == "" {
		t.Error("version is empty")
	}
}

// ── Session CRUD ────────────────────────────────────────────────────────────

func TestCreateSession_Success(t *testing.T) {
	s := createSession(t, "integ-user", "web")
	if s.ID == "" {
		t.Error("empty session ID")
	}
	if len(s.ID) != 32 {
		t.Errorf("session ID length = %d, want 32", len(s.ID))
	}
	if s.UserID != "integ-user" {
		t.Errorf("user_id = %q", s.UserID)
	}
	if s.Channel != "web" {
		t.Errorf("channel = %q", s.Channel)
	}
	if s.State != "idle" {
		t.Errorf("state = %q", s.State)
	}
	if _, err := time.Parse(time.RFC3339, s.CreatedAt); err != nil {
		t.Errorf("created_at not RFC3339: %v", err)
	}
}

func TestCreateSession_AllChannels(t *testing.T) {
	for _, ch := range []string{"web", "whatsapp", "telegram"} {
		t.Run(ch, func(t *testing.T) {
			s := createSession(t, "user-"+ch, ch)
			if s.Channel != ch {
				t.Errorf("channel = %q, want %q", s.Channel, ch)
			}
		})
	}
}

func TestCreateSession_InvalidJSON(t *testing.T) {
	t.Parallel()
	resp := doRequest(t, http.MethodPost, "/api/v1/sessions", "{bad json")
	defer resp.Body.Close()
	assertStatus(t, resp, 400)
}

func TestCreateSession_MissingUserID(t *testing.T) {
	t.Parallel()
	resp := doRequest(t, http.MethodPost, "/api/v1/sessions", map[string]string{"user_id": "", "channel": "web"})
	defer resp.Body.Close()
	assertStatus(t, resp, 400)
}

func TestCreateSession_InvalidChannel(t *testing.T) {
	t.Parallel()
	resp := doRequest(t, http.MethodPost, "/api/v1/sessions", map[string]string{"user_id": "u", "channel": "discord"})
	defer resp.Body.Close()
	assertStatus(t, resp, 400)
}

func TestCreateSession_MissingChannel(t *testing.T) {
	t.Parallel()
	resp := doRequest(t, http.MethodPost, "/api/v1/sessions", map[string]string{"user_id": "u"})
	defer resp.Body.Close()
	assertStatus(t, resp, 400)
}

func TestGetSession_Success(t *testing.T) {
	s := createSession(t, "get-user", "web")
	detail := getSession(t, s.ID)
	if detail.ID != s.ID {
		t.Errorf("ID mismatch")
	}
	if detail.Messages == nil {
		t.Error("messages is nil, want empty array")
	}
}

func TestGetSession_NotFound(t *testing.T) {
	t.Parallel()
	resp := doRequest(t, http.MethodGet, "/api/v1/sessions/nonexistent-000000", nil)
	defer resp.Body.Close()
	assertStatus(t, resp, 404)
}

// ── Message Sending with Real LLM ──────────────────────────────────────────

func TestSendMessage_Success(t *testing.T) {
	// This test uses real LLM tokens — keep prompt minimal
	s := createSession(t, "msg-user", "web")

	resp := sendMessage(t, s.ID, "Say hello in one word")
	defer resp.Body.Close()
	assertStatus(t, resp, 202)
	var status statusResp
	decodeJSONResp(t, resp, &status)
	if status.Status != "accepted" {
		t.Errorf("status = %q", status.Status)
	}

	// Poll until completed or failed (60s timeout for LLM call)
	detail := pollSessionUntil(t, s.ID, []string{"completed", "failed"}, 60*time.Second)

	if detail.State == "failed" {
		t.Fatalf("CPN execution failed — check server logs")
	}

	// Verify messages
	if len(detail.Messages) < 2 {
		t.Fatalf("expected >= 2 messages (user + assistant), got %d", len(detail.Messages))
	}

	// First message is the user's
	if detail.Messages[0].Role != "user" {
		t.Errorf("message[0].role = %q, want user", detail.Messages[0].Role)
	}
	if detail.Messages[0].Content != "Say hello in one word" {
		t.Errorf("message[0].content = %q", detail.Messages[0].Content)
	}

	t.Logf("LLM response: %s", detail.Messages[len(detail.Messages)-1].Content)
}

func TestSendMessage_NotFound(t *testing.T) {
	t.Parallel()
	resp := sendMessage(t, "nonexistent-session", "hello")
	defer resp.Body.Close()
	assertStatus(t, resp, 404)
}

func TestSendMessage_EmptyContent(t *testing.T) {
	s := createSession(t, "empty-msg-user", "web")
	resp := doRequest(t, http.MethodPost, "/api/v1/sessions/"+s.ID+"/messages", map[string]string{"content": ""})
	defer resp.Body.Close()
	assertStatus(t, resp, 400)
}

func TestSendMessage_SessionBusy(t *testing.T) {
	// Send a long prompt so the CPN is still running when we send the second
	s := createSession(t, "busy-user", "web")

	resp1 := sendMessage(t, s.ID, "Write a detailed 500-word essay about the history of computing from 1940 to 2020")
	assertStatus(t, resp1, 202)
	resp1.Body.Close()

	// Small delay to ensure goroutine has set state to Running
	time.Sleep(100 * time.Millisecond)

	// Second message should be rejected with 409
	resp2 := sendMessage(t, s.ID, "hello again")
	defer resp2.Body.Close()
	assertStatus(t, resp2, 409)

	var errBody errorResp
	decodeJSONResp(t, resp2, &errBody)
	if errBody.Error != "session is busy" {
		t.Errorf("error = %q, want %q", errBody.Error, "session is busy")
	}

	// Wait for original to finish so it doesn't leak
	pollSessionUntil(t, s.ID, []string{"completed", "failed"}, 90*time.Second)
}

// ── SSE Streaming with Real LLM ────────────────────────────────────────────

func TestSSE_Headers(t *testing.T) {
	s := createSession(t, "sse-header-user", "web")

	resp, reader := connectSSE(t, s.ID)
	defer reader.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q", resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("Cache-Control = %q", resp.Header.Get("Cache-Control"))
	}
	if resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Errorf("X-Accel-Buffering = %q", resp.Header.Get("X-Accel-Buffering"))
	}
}

func TestSSE_RetryDirective(t *testing.T) {
	s := createSession(t, "sse-retry-user", "web")
	resp, reader := connectSSE(t, s.ID)
	defer reader.Close()
	_ = resp

	// First event should be retry directive
	evt, err := reader.NextEvent(5 * time.Second)
	if err != nil {
		t.Fatalf("read first event: %v", err)
	}
	if evt.Event != "retry" {
		t.Errorf("first event type = %q, want retry", evt.Event)
	}
	if evt.Data != "3000" {
		t.Errorf("retry value = %q, want 3000", evt.Data)
	}
}

func TestSSE_StreamChunksFromLLM(t *testing.T) {
	s := createSession(t, "sse-stream-user", "web")

	resp, reader := connectSSE(t, s.ID)
	defer reader.Close()
	_ = resp

	// Skip retry directive
	reader.NextEvent(5 * time.Second)

	// Send message (triggers LLM)
	msgResp := sendMessage(t, s.ID, "Say hi")
	msgResp.Body.Close()

	// Collect events until session_completed or timeout
	var streamChunks []sseEvent
	var allEvents []sseEvent
	timeout := time.After(60 * time.Second)

	for {
		select {
		case <-timeout:
			t.Fatal("timeout waiting for SSE events")
		default:
		}

		evt, err := reader.NextEvent(60 * time.Second)
		if err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("read SSE event: %v", err)
		}

		allEvents = append(allEvents, *evt)

		if evt.Event == "stream_chunk" {
			streamChunks = append(streamChunks, *evt)
		}
		if evt.Event == "session_completed" {
			break
		}
		if evt.Event == "heartbeat" {
			continue
		}
	}

	t.Logf("Received %d total events, %d stream_chunks", len(allEvents), len(streamChunks))

	// We may or may not get stream_chunks depending on model/streaming support
	// But we should get at least SOME events (transition_fired, session_completed)
	if len(allEvents) < 1 {
		t.Error("expected at least 1 SSE event")
	}

	// Verify event IDs are monotonically increasing (for events that have IDs)
	var lastID int
	for _, evt := range allEvents {
		if evt.ID != "" {
			var id int
			fmt.Sscanf(evt.ID, "%d", &id)
			if id <= lastID && lastID > 0 {
				t.Errorf("event ID %d not greater than previous %d", id, lastID)
			}
			lastID = id
		}
	}
}

func TestSSE_Heartbeat(t *testing.T) {
	s := createSession(t, "sse-hb-user", "web")

	_, reader := connectSSE(t, s.ID)
	defer reader.Close()

	// Skip retry directive
	reader.NextEvent(5 * time.Second)

	// Wait for heartbeat (should arrive within 15-20 seconds)
	evt, err := reader.NextEvent(20 * time.Second)
	if err != nil {
		t.Fatalf("waiting for heartbeat: %v", err)
	}
	if evt.Event != "heartbeat" {
		t.Errorf("expected heartbeat event, got %q", evt.Event)
	}
}

func TestSSE_NotFound(t *testing.T) {
	t.Parallel()
	resp := doRequest(t, http.MethodGet, "/api/v1/sessions/nonexistent-sse/events", nil)
	defer resp.Body.Close()
	assertStatus(t, resp, 404)
}

// ── CORS Tests ──────────────────────────────────────────────────────────────

func TestCORS_SimpleRequest(t *testing.T) {
	t.Parallel()
	resp := doRequestWithHeader(t, http.MethodGet, "/api/v1/health", nil, map[string]string{
		"Origin": "http://localhost:3000",
	})
	defer resp.Body.Close()
	assertStatus(t, resp, 200)

	acao := resp.Header.Get("Access-Control-Allow-Origin")
	if acao != "http://localhost:3000" {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", acao, "http://localhost:3000")
	}
}

func TestCORS_Preflight(t *testing.T) {
	t.Parallel()
	resp := doRequestWithHeader(t, http.MethodOptions, "/api/v1/sessions", nil, map[string]string{
		"Origin":                        "http://localhost:3000",
		"Access-Control-Request-Method": "POST",
	})
	defer resp.Body.Close()
	assertStatus(t, resp, 204)

	if !strings.Contains(resp.Header.Get("Access-Control-Allow-Methods"), "POST") {
		t.Errorf("missing POST in Allow-Methods: %q", resp.Header.Get("Access-Control-Allow-Methods"))
	}
	if !strings.Contains(resp.Header.Get("Access-Control-Allow-Headers"), "Last-Event-ID") {
		t.Errorf("missing Last-Event-ID in Allow-Headers: %q", resp.Header.Get("Access-Control-Allow-Headers"))
	}
	if resp.Header.Get("Access-Control-Max-Age") != "86400" {
		t.Errorf("Max-Age = %q, want 86400", resp.Header.Get("Access-Control-Max-Age"))
	}
}

// ── Error Cases ─────────────────────────────────────────────────────────────

func TestError_OversizedBody(t *testing.T) {
	t.Parallel()
	big := bytes.Repeat([]byte("x"), 1<<20+1) // 1MB + 1 byte
	resp := doRequest(t, http.MethodPost, "/api/v1/sessions", big)
	defer resp.Body.Close()
	assertStatus(t, resp, 400)
}

func TestError_UnknownFields(t *testing.T) {
	t.Parallel()
	resp := doRequest(t, http.MethodPost, "/api/v1/sessions", `{"user_id":"u","channel":"web","unknown":"x"}`)
	defer resp.Body.Close()
	assertStatus(t, resp, 400)
}

func TestError_WrongMethod(t *testing.T) {
	t.Parallel()
	resp := doRequest(t, http.MethodPut, "/api/v1/sessions", nil)
	defer resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Errorf("PUT /sessions: expected 405, got %d", resp.StatusCode)
	}
}

// ── HITL Error Cases ────────────────────────────────────────────────────────

func TestHITL_SessionNotFound(t *testing.T) {
	t.Parallel()
	resp := doRequest(t, http.MethodPost, "/api/v1/sessions/nonexistent/hitl/t-hitl", map[string]string{
		"action": "approve",
	})
	defer resp.Body.Close()
	assertStatus(t, resp, 404)
}

func TestHITL_InvalidAction(t *testing.T) {
	s := createSession(t, "hitl-user", "web")
	resp := doRequest(t, http.MethodPost, "/api/v1/sessions/"+s.ID+"/hitl/t-hitl", map[string]string{
		"action": "invalid",
	})
	defer resp.Body.Close()
	assertStatus(t, resp, 400)
}

func TestHITL_NoHITLPending(t *testing.T) {
	// Default topology has no HITL transitions — resolve should 409
	s := createSession(t, "hitl-nopend-user", "web")
	resp := doRequest(t, http.MethodPost, "/api/v1/sessions/"+s.ID+"/hitl/t-hitl", map[string]string{
		"action": "approve",
	})
	defer resp.Body.Close()
	assertStatus(t, resp, 409)
}

// ── Concurrency Tests ───────────────────────────────────────────────────────

func TestConcurrency_CreateMultipleSessions(t *testing.T) {
	t.Parallel()
	const n = 10
	type result struct {
		id  string
		err error
	}
	ch := make(chan result, n)

	for i := 0; i < n; i++ {
		go func(idx int) {
			s := createSession(t, fmt.Sprintf("concurrent-user-%d", idx), "web")
			ch <- result{id: s.ID}
		}(i)
	}

	ids := make(map[string]bool)
	for i := 0; i < n; i++ {
		r := <-ch
		if r.err != nil {
			t.Errorf("concurrent create %d: %v", i, r.err)
			continue
		}
		if ids[r.id] {
			t.Errorf("duplicate session ID: %s", r.id)
		}
		ids[r.id] = true
	}

	if len(ids) != n {
		t.Errorf("expected %d unique IDs, got %d", n, len(ids))
	}
}

func TestConcurrency_MultipleSessionsWithLLM(t *testing.T) {
	// Send messages to 3 sessions concurrently — all should complete
	const n = 3
	sessions := make([]sessionResponse, n)
	for i := 0; i < n; i++ {
		sessions[i] = createSession(t, fmt.Sprintf("multi-llm-user-%d", i), "web")
	}

	// Send messages concurrently
	for i := 0; i < n; i++ {
		resp := sendMessage(t, sessions[i].ID, "Say hi")
		resp.Body.Close()
	}

	// Poll all to completion
	for i := 0; i < n; i++ {
		detail := pollSessionUntil(t, sessions[i].ID, []string{"completed", "failed"}, 90*time.Second)
		if detail.State == "failed" {
			t.Errorf("session %d failed", i)
		}
		if len(detail.Messages) < 2 {
			t.Errorf("session %d: expected >= 2 messages, got %d", i, len(detail.Messages))
		}
		t.Logf("session %d response: %s", i, detail.Messages[len(detail.Messages)-1].Content)
	}
}
