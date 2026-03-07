package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func newTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	c, err := NewClient(&ClientConfig{
		APIKey:  "test-key",
		BaseURL: serverURL,
		Model:   "test-model",
		Retry: RetryConfig{
			MaxRetries:      0,
			BaseDelay:       1 * time.Millisecond,
			MaxDelay:        10 * time.Millisecond,
			RetryableStatus: []int{429, 502, 503, 529},
		},
	})
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}
	return c
}

func testRequest() *output.ChatRequest {
	return &output.ChatRequest{
		Model: "test-model",
		Messages: []entity.Message{
			{Role: valueobject.RoleSystem, Content: "You are helpful."},
			{Role: valueobject.RoleUser, Content: "Hello"},
		},
		Temperature: 0.7,
		MaxTokens:   100,
	}
}

func TestNewClient_RequiresAPIKey(t *testing.T) {
	_, err := NewClient(&ClientConfig{})
	if err == nil {
		t.Error("expected error for empty API key")
	}
}

func TestClient_Complete_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing or wrong Authorization header")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("missing Content-Type header")
		}

		var req chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		if req.Stream {
			t.Error("Complete should send stream=false")
		}

		resp := chatCompletionResponse{
			ID:    "resp-1",
			Model: "test-model",
			Choices: []choice{
				{Message: messagePayload{Role: "assistant", Content: "Hello there!"}},
			},
			Usage: usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encoding response: %v", err)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	resp, err := client.Complete(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}

	if resp.Content != "Hello there!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello there!")
	}
	if resp.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", resp.TotalTokens)
	}
}

func TestClient_Complete_ErrorStatus(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"unauthorized", http.StatusUnauthorized},
		{"bad request", http.StatusBadRequest},
		{"internal error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprintf(w, `{"error":{"message":"test error","code":%d}}`, tt.status)
			}))
			defer server.Close()

			client := newTestClient(t, server.URL)
			_, err := client.Complete(context.Background(), testRequest())
			if err == nil {
				t.Errorf("expected error for status %d", tt.status)
			}
		})
	}
}

func TestClient_Complete_EmptyChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := chatCompletionResponse{
			Choices: []choice{},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encoding response: %v", err)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.Complete(context.Background(), testRequest())
	if err == nil {
		t.Error("expected error for empty choices")
	}
}

func TestClient_CompleteStream_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}
		if !req.Stream {
			t.Error("CompleteStream should send stream=true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)

		chunks := []string{
			`data: {"id":"1","model":"m","choices":[{"index":0,"delta":{"content":"Hello "},"finish_reason":""}]}`,
			`data: {"id":"1","model":"m","choices":[{"index":0,"delta":{"content":"World"},"finish_reason":""}]}`,
			`data: [DONE]`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "%s\n\n", c)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	ch, err := client.CompleteStream(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("CompleteStream error: %v", err)
	}

	tokens := make([]string, 0, 2)
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		if chunk.Done {
			break
		}
		tokens = append(tokens, chunk.Content)
	}

	if len(tokens) != 2 {
		t.Fatalf("got %d tokens, want 2", len(tokens))
	}
	if tokens[0] != "Hello " || tokens[1] != "World" {
		t.Errorf("tokens = %v", tokens)
	}
}

func TestClient_CompleteStream_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":{"message":"invalid key","code":401}}`)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	_, err := client.CompleteStream(context.Background(), testRequest())
	if err == nil {
		t.Error("expected error for 401")
	}
}

func TestClient_Complete_WithRetry(t *testing.T) {
	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"message":"rate limited","code":429}}`)
			return
		}
		resp := chatCompletionResponse{
			Choices: []choice{
				{Message: messagePayload{Role: "assistant", Content: "finally"}},
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	c, err := NewClient(&ClientConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "test-model",
		Retry: RetryConfig{
			MaxRetries:      5,
			BaseDelay:       1 * time.Millisecond,
			MaxDelay:        10 * time.Millisecond,
			RetryableStatus: []int{429},
		},
	})
	if err != nil {
		t.Fatalf("NewClient error: %v", err)
	}

	resp, err := c.Complete(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if resp.Content != "finally" {
		t.Errorf("Content = %q, want %q", resp.Content, "finally")
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestClient_Complete_ContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Complete(ctx, testRequest())
	if err == nil {
		t.Error("expected error from context timeout")
	}
}

func TestClient_SetsCustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-OpenRouter-Title") != "liwaisi" {
			t.Errorf("X-OpenRouter-Title = %q, want %q", r.Header.Get("X-OpenRouter-Title"), "liwaisi")
		}
		if r.Header.Get("HTTP-Referer") != "https://example.com" {
			t.Errorf("HTTP-Referer = %q, want %q", r.Header.Get("HTTP-Referer"), "https://example.com")
		}

		resp := chatCompletionResponse{
			Choices: []choice{
				{Message: messagePayload{Role: "assistant", Content: "ok"}},
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encoding response: %v", err)
		}
	}))
	defer server.Close()

	c, _ := NewClient(&ClientConfig{
		APIKey:  "key",
		BaseURL: server.URL,
		Model:   "m",
		Referer: "https://example.com",
		Title:   "liwaisi",
		Retry:   RetryConfig{MaxRetries: 0, RetryableStatus: []int{429}},
	})

	_, err := c.Complete(context.Background(), testRequest())
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
}

func TestClient_Complete_WithToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}

		if len(req.Tools) != 1 {
			t.Errorf("expected 1 tool, got %d", len(req.Tools))
		}
		if req.Tools[0].Function.Name != "who_am_i" {
			t.Errorf("tool name = %q, want %q", req.Tools[0].Function.Name, "who_am_i")
		}
		if req.ToolChoice != "auto" {
			t.Errorf("tool_choice = %v, want %q", req.ToolChoice, "auto")
		}

		resp := chatCompletionResponse{
			ID:    "resp-tc",
			Model: "test-model",
			Choices: []choice{
				{
					Message: messagePayload{
						Role: "assistant",
						ToolCalls: []toolCallPayload{
							{
								ID:   "call_abc123",
								Type: "function",
								Function: functionCallPayload{
									Name:      "who_am_i",
									Arguments: "{}",
								},
							},
						},
					},
					FinishReason: "tool_calls",
				},
			},
			Usage: usage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encoding response: %v", err)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	req := &output.ChatRequest{
		Model: "test-model",
		Messages: []entity.Message{
			{Role: valueobject.RoleUser, Content: "Who are you?"},
		},
		Temperature: 0.7,
		MaxTokens:   100,
		Tools: []valueobject.ToolDefinition{
			{
				Type: "function",
				Function: valueobject.FunctionDefinition{
					Name:        "who_am_i",
					Description: "Returns agent info.",
					Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
				},
			},
		},
		ToolChoice: valueobject.ToolChoiceAuto,
	}

	resp, err := client.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}

	if resp.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, "tool_calls")
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_abc123" {
		t.Errorf("ToolCall.ID = %q, want %q", tc.ID, "call_abc123")
	}
	if tc.Function.Name != "who_am_i" {
		t.Errorf("ToolCall.Function.Name = %q, want %q", tc.Function.Name, "who_am_i")
	}
	if tc.Function.Arguments != "{}" {
		t.Errorf("ToolCall.Function.Arguments = %q, want %q", tc.Function.Arguments, "{}")
	}
}

func TestClient_Complete_ToolResultMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decoding request: %v", err)
		}

		// Verify the tool result message is serialized correctly.
		if len(req.Messages) < 3 {
			t.Fatalf("expected at least 3 messages, got %d", len(req.Messages))
		}

		assistantMsg := req.Messages[1]
		if assistantMsg.Role != "assistant" {
			t.Errorf("msg[1].Role = %q, want %q", assistantMsg.Role, "assistant")
		}
		if len(assistantMsg.ToolCalls) != 1 {
			t.Fatalf("msg[1] should have 1 tool call, got %d", len(assistantMsg.ToolCalls))
		}

		toolMsg := req.Messages[2]
		if toolMsg.Role != "tool" {
			t.Errorf("msg[2].Role = %q, want %q", toolMsg.Role, "tool")
		}
		if toolMsg.ToolCallID != "call_abc123" {
			t.Errorf("msg[2].ToolCallID = %q, want %q", toolMsg.ToolCallID, "call_abc123")
		}

		resp := chatCompletionResponse{
			Model: "test-model",
			Choices: []choice{
				{
					Message:      messagePayload{Role: "assistant", Content: "I am liwaisi."},
					FinishReason: "stop",
				},
			},
			Usage: usage{TotalTokens: 50},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encoding response: %v", err)
		}
	}))
	defer server.Close()

	client := newTestClient(t, server.URL)
	req := &output.ChatRequest{
		Model: "test-model",
		Messages: []entity.Message{
			{Role: valueobject.RoleUser, Content: "Who are you?"},
			{
				Role: valueobject.RoleAssistant,
				ToolCalls: []valueobject.ToolCall{
					{
						ID:   "call_abc123",
						Type: "function",
						Function: valueobject.FunctionCall{
							Name:      "who_am_i",
							Arguments: "{}",
						},
					},
				},
			},
			{
				Role:       valueobject.RoleTool,
				Content:    `{"name":"liwaisi"}`,
				ToolCallID: "call_abc123",
			},
		},
		Temperature: 0.7,
		MaxTokens:   100,
	}

	resp, err := client.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if resp.Content != "I am liwaisi." {
		t.Errorf("Content = %q, want %q", resp.Content, "I am liwaisi.")
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", resp.FinishReason, "stop")
	}
}
