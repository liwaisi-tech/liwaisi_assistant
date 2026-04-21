// Package llmmock provides a tiny in-process OpenAI-compatible chat
// completions server. Used by the SC-17 E2E harness so the awakening flow
// can be exercised end-to-end without requiring real LLM credentials.
//
// The server returns canned responses in a fixed sequence: the first POST
// to /v1/chat/completions receives PlanJSON, the second receives ReportJSON,
// and any call beyond that receives a third response or the last canned
// response in the queue. Callers pass the sequence they want via Responses.
package llmmock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
)

// Server wraps an httptest.Server that replays Responses round-robin.
type Server struct {
	ts        *httptest.Server
	responses []string
	idx       atomic.Int64
	mu        sync.Mutex
	calls     int
}

// New starts a new mock server with the given canned response bodies.
// Each response is the `content` field of the first choice in an OpenAI
// chat.completions reply. Close must be called to release the socket.
func New(responses []string) *Server {
	s := &Server{responses: responses}
	s.ts = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// URL returns the base URL for the mock server (e.g. http://127.0.0.1:XXXXX).
func (s *Server) URL() string { return s.ts.URL }

// Calls returns the number of requests served so far.
func (s *Server) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// Close shuts down the underlying httptest server.
func (s *Server) Close() { s.ts.Close() }

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Choices []chatChoice `json:"choices"`
}

type chatChoice struct {
	Index        int         `json:"index"`
	Message      chatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req chatRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	s.mu.Lock()
	s.calls++
	s.mu.Unlock()

	i := int(s.idx.Add(1)) - 1
	if i >= len(s.responses) {
		i = len(s.responses) - 1
	}
	if i < 0 {
		i = 0
	}
	content := ""
	if len(s.responses) > 0 {
		content = s.responses[i]
	}

	resp := chatResponse{
		ID:     "llmmock-1",
		Object: "chat.completion",
		Choices: []chatChoice{{
			Index:        0,
			Message:      chatMessage{Role: "assistant", Content: content},
			FinishReason: "stop",
		}},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
