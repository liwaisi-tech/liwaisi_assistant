package cpn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// ── Test Helper ──────────────────────────────────────────────────────────────

func mockOpenRouterServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *OpenRouterClient) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := NewOpenRouterClient("test-api-key-secret", "test-default-model")
	client.BaseURL = srv.URL
	return srv, client
}

func successHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{{
			"message": map[string]any{
				"role":    "assistant",
				"content": "Hello!",
			},
		}},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_cost":        0.001,
		},
		"model": "test-default-model",
	})
}

// ── Constructor ──────────────────────────────────────────────────────────────

func TestNewOpenRouterClient(t *testing.T) {
	client := NewOpenRouterClient("my-key", "my-model")

	if client.BaseURL != "https://openrouter.ai/api/v1" {
		t.Errorf("BaseURL = %q, want %q", client.BaseURL, "https://openrouter.ai/api/v1")
	}
	if client.DefaultModel != "my-model" {
		t.Errorf("DefaultModel = %q, want %q", client.DefaultModel, "my-model")
	}
	if client.HTTPClient == nil {
		t.Fatal("HTTPClient is nil")
	}
	if client.HTTPClient.Timeout.Seconds() != 120 {
		t.Errorf("Timeout = %v, want 120s", client.HTTPClient.Timeout)
	}
	if client.TokenLedger == nil {
		t.Fatal("TokenLedger is nil")
	}
}

// ── Interface Compliance ─────────────────────────────────────────────────────

func TestOpenRouterClient_ImplementsLLMClient(t *testing.T) {
	var _ LLMClient = (*OpenRouterClient)(nil)
}

// ── Complete: Success ────────────────────────────────────────────────────────

func TestOpenRouterClient_Complete_Success(t *testing.T) {
	_, client := mockOpenRouterServer(t, successHandler)

	resp, err := client.Complete(context.Background(), &LLMRequest{
		Model:     "test-default-model",
		Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
		SessionID: "sess-1",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello!")
	}
	if resp.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", resp.InputTokens)
	}
	if resp.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", resp.OutputTokens)
	}
	if resp.Model != "test-default-model" {
		t.Errorf("Model = %q, want %q", resp.Model, "test-default-model")
	}
	if resp.CostUSD != 0.001 {
		t.Errorf("CostUSD = %f, want 0.001", resp.CostUSD)
	}
}

// ── Complete: Tool Calls ─────────────────────────────────────────────────────

func TestOpenRouterClient_Complete_ToolCalls(t *testing.T) {
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": "",
					"tool_calls": []map[string]any{{
						"id":   "call-1",
						"type": "function",
						"function": map[string]any{
							"name":      "get_weather",
							"arguments": `{"city":"Lima"}`,
						},
					}},
				},
			}},
			"usage": map[string]any{
				"prompt_tokens":     15,
				"completion_tokens": 8,
			},
			"model": "test-default-model",
		})
	})

	resp, err := client.Complete(context.Background(), &LLMRequest{
		Model:     "test-default-model",
		Messages:  []LLMMessage{{Role: "user", Content: "Weather?"}},
		MaxTokens: 100,
		SessionID: "sess-tc",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call-1" {
		t.Errorf("ToolCall.ID = %q, want %q", tc.ID, "call-1")
	}
	if tc.ToolName != "get_weather" {
		t.Errorf("ToolCall.ToolName = %q, want %q", tc.ToolName, "get_weather")
	}
	if string(tc.Arguments) != `{"city":"Lima"}` {
		t.Errorf("ToolCall.Arguments = %s, want %s", tc.Arguments, `{"city":"Lima"}`)
	}
}

// ── Complete: Error Status Codes ─────────────────────────────────────────────

func TestOpenRouterClient_Complete_ServerErrors(t *testing.T) {
	tests := []struct {
		status int
		want   error
	}{
		{400, ErrBadRequest},
		{401, ErrUnauthorized},
		{402, ErrInsufficientCredits},
		{403, ErrForbidden},
		{404, ErrNotFound},
		{408, ErrRequestTimeout},
		{413, ErrPayloadTooLarge},
		{422, ErrUnprocessableEntity},
		{429, ErrRateLimited},
		{500, ErrProviderUnavailable},
		{502, ErrProviderUnavailable},
		{503, ErrProviderUnavailable},
		{524, ErrEdgeTimeout},
		{529, ErrProviderOverloaded},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.status), func(t *testing.T) {
			_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			})

			_, err := client.Complete(context.Background(), &LLMRequest{
				Model:     "test-model",
				Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
				MaxTokens: 10,
				SessionID: "sess-err",
			})
			if err == nil {
				t.Fatalf("expected error for status %d", tt.status)
			}
			if err != tt.want {
				t.Errorf("status %d: got %v, want %v", tt.status, err, tt.want)
			}
		})
	}
}

// ── Complete: Rate Limit ─────────────────────────────────────────────────────

func TestOpenRouterClient_Complete_RateLimit(t *testing.T) {
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
	})

	_, err := client.Complete(context.Background(), &LLMRequest{
		Model:     "test-model",
		Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		SessionID: "sess-rl",
	})
	if err != ErrRateLimited {
		t.Errorf("got %v, want ErrRateLimited", err)
	}
}

// ── Complete: Context Canceled ───────────────────────────────────────────────

func TestOpenRouterClient_Complete_ContextCanceled(t *testing.T) {
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		// Block forever — context cancel should interrupt.
		select {}
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := client.Complete(ctx, &LLMRequest{
		Model:     "test-model",
		Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
	})
	if err == nil {
		t.Fatal("expected error for canceled context")
	}
}

// ── Complete: Malformed JSON ─────────────────────────────────────────────────

func TestOpenRouterClient_Complete_MalformedJSON(t *testing.T) {
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{invalid json`))
	})

	_, err := client.Complete(context.Background(), &LLMRequest{
		Model:     "test-model",
		Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		SessionID: "sess-bad",
	})
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

// ── Complete: API Key in Header ──────────────────────────────────────────────

func TestOpenRouterClient_Complete_APIKeyInHeader(t *testing.T) {
	var gotAuth string
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		successHandler(w, r)
	})

	_, err := client.Complete(context.Background(), &LLMRequest{
		Model:     "test-model",
		Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		SessionID: "sess-auth",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if gotAuth != "Bearer test-api-key-secret" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-api-key-secret")
	}
}

// ── Complete: Session ID Header ──────────────────────────────────────────────

func TestOpenRouterClient_Complete_SessionIdHeader(t *testing.T) {
	var gotSessionID string
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotSessionID = r.Header.Get("X-Session-Id")
		successHandler(w, r)
	})

	_, err := client.Complete(context.Background(), &LLMRequest{
		Model:     "test-model",
		Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		SessionID: "sess-42",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if gotSessionID != "sess-42" {
		t.Errorf("X-Session-Id = %q, want %q", gotSessionID, "sess-42")
	}
}

// ── Complete: No Session ID Header When Empty ────────────────────────────────

func TestOpenRouterClient_Complete_NoSessionIdWhenEmpty(t *testing.T) {
	var hasHeader bool
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, hasHeader = r.Header["X-Session-Id"]
		successHandler(w, r)
	})

	_, err := client.Complete(context.Background(), &LLMRequest{
		Model:     "test-model",
		Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if hasHeader {
		t.Error("X-Session-Id header should be absent when SessionID is empty")
	}
}

// ── Complete: Records to Ledger ──────────────────────────────────────────────

func TestOpenRouterClient_Complete_RecordsToLedger(t *testing.T) {
	_, client := mockOpenRouterServer(t, successHandler)

	_, err := client.Complete(context.Background(), &LLMRequest{
		Model:     "test-model",
		Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		SessionID: "sess-ledger",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	rec := client.TokenLedger.Get("sess-ledger")
	if rec == nil {
		t.Fatal("expected ledger record")
	}
	if rec.Calls != 1 {
		t.Errorf("Calls = %d, want 1", rec.Calls)
	}
	if rec.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", rec.InputTokens)
	}
	if rec.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", rec.OutputTokens)
	}
}

// ── Complete: No Record on Error ─────────────────────────────────────────────

func TestOpenRouterClient_Complete_NoRecordOnError(t *testing.T) {
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	})

	_, _ = client.Complete(context.Background(), &LLMRequest{
		Model:     "test-model",
		Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		SessionID: "sess-no-record",
	})

	rec := client.TokenLedger.Get("sess-no-record")
	if rec != nil {
		t.Errorf("expected nil ledger record on error, got %+v", rec)
	}
}

// ── Complete: Model Resolution ───────────────────────────────────────────────

func TestOpenRouterClient_Complete_ModelResolution(t *testing.T) {
	var gotModel string
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel, _ = body["model"].(string)
		successHandler(w, r)
	})

	_, err := client.Complete(context.Background(), &LLMRequest{
		Model:     "classifier",
		Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		SessionID: "sess-resolve",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	want := "google/gemini-2.0-flash-001"
	if gotModel != want {
		t.Errorf("resolved model = %q, want %q", gotModel, want)
	}
}

// ── Complete: Default Model ──────────────────────────────────────────────────

func TestOpenRouterClient_Complete_DefaultModel(t *testing.T) {
	var gotModel string
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel, _ = body["model"].(string)
		successHandler(w, r)
	})

	_, err := client.Complete(context.Background(), &LLMRequest{
		Model:     "", // empty — should use DefaultModel
		Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		SessionID: "sess-default",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if gotModel != "test-default-model" {
		t.Errorf("model = %q, want %q", gotModel, "test-default-model")
	}
}

// ── Complete: Response Format ────────────────────────────────────────────────

func TestOpenRouterClient_Complete_ResponseFormat(t *testing.T) {
	var gotFormat map[string]any
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotFormat, _ = body["response_format"].(map[string]any)
		successHandler(w, r)
	})

	_, err := client.Complete(context.Background(), &LLMRequest{
		Model:       "test-model",
		Messages:    []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens:   10,
		ResponseFmt: "json_object",
		SessionID:   "sess-fmt",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if gotFormat == nil {
		t.Fatal("response_format not present in request body")
	}
	if gotFormat["type"] != "json_object" {
		t.Errorf("response_format.type = %v, want %q", gotFormat["type"], "json_object")
	}
}

// ── Complete: Tools in Body ──────────────────────────────────────────────────

func TestOpenRouterClient_Complete_ToolsInBody(t *testing.T) {
	var gotTools []any
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTools, _ = body["tools"].([]any)
		successHandler(w, r)
	})

	_, err := client.Complete(context.Background(), &LLMRequest{
		Model:     "test-model",
		Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		Tools: []LLMTool{{
			Name:        "get_weather",
			Description: "Get weather for a city",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
		}},
		SessionID: "sess-tools",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if len(gotTools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(gotTools))
	}
}

// ── String: Redacts API Key ──────────────────────────────────────────────────

func TestOpenRouterClient_String_RedactsKey(t *testing.T) {
	client := NewOpenRouterClient("super-secret-key-12345", "my-model")
	s := client.String()

	if strings.Contains(s, "super-secret-key-12345") {
		t.Errorf("String() contains API key: %s", s)
	}
	if !strings.Contains(s, "my-model") {
		t.Errorf("String() should contain model: %s", s)
	}
	if !strings.Contains(s, "OpenRouterClient") {
		t.Errorf("String() should contain type name: %s", s)
	}
}

// ── EstimateCost: Known Model ────────────────────────────────────────────────

func TestOpenRouterClient_EstimateCost_KnownModel(t *testing.T) {
	client := NewOpenRouterClient("key", "google/gemini-2.0-flash-001")

	cost, err := client.EstimateCost(&LLMRequest{
		Model:     "google/gemini-2.0-flash-001",
		Messages:  []LLMMessage{{Role: "user", Content: strings.Repeat("a", 400)}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("EstimateCost() error = %v", err)
	}
	if cost <= 0 {
		t.Errorf("cost = %f, want positive value", cost)
	}
}

// ── EstimateCost: Unknown Model ──────────────────────────────────────────────

func TestOpenRouterClient_EstimateCost_UnknownModel(t *testing.T) {
	client := NewOpenRouterClient("key", "unknown/model")

	cost, err := client.EstimateCost(&LLMRequest{
		Model:     "unknown/model",
		Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("EstimateCost() error = %v", err)
	}
	if cost != 0 {
		t.Errorf("cost = %f, want 0 for unknown model", cost)
	}
}

// ── TokenLedger: Record ──────────────────────────────────────────────────────

func TestTokenLedger_Record(t *testing.T) {
	ledger := NewTokenLedger()

	ledger.Record("sess-1", 10, 5, 0.001)
	ledger.Record("sess-1", 20, 10, 0.002)

	rec := ledger.Get("sess-1")
	if rec == nil {
		t.Fatal("expected record")
	}
	if rec.InputTokens != 30 {
		t.Errorf("InputTokens = %d, want 30", rec.InputTokens)
	}
	if rec.OutputTokens != 15 {
		t.Errorf("OutputTokens = %d, want 15", rec.OutputTokens)
	}
	if rec.TotalCostUSD != 0.003 {
		t.Errorf("TotalCostUSD = %f, want 0.003", rec.TotalCostUSD)
	}
	if rec.Calls != 2 {
		t.Errorf("Calls = %d, want 2", rec.Calls)
	}
}

// ── TokenLedger: Get Not Found ───────────────────────────────────────────────

func TestTokenLedger_Get_NotFound(t *testing.T) {
	ledger := NewTokenLedger()

	rec := ledger.Get("nonexistent")
	if rec != nil {
		t.Errorf("expected nil for unknown session, got %+v", rec)
	}
}

// ── TokenLedger: Concurrent Access ───────────────────────────────────────────

func TestTokenLedger_ConcurrentAccess(t *testing.T) {
	ledger := NewTokenLedger()

	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			ledger.Record("sess-concurrent", 10, 5, 0.001)
			ledger.Get("sess-concurrent")
		})
	}
	wg.Wait()

	rec := ledger.Get("sess-concurrent")
	if rec == nil {
		t.Fatal("expected record")
	}
	if rec.Calls != 100 {
		t.Errorf("Calls = %d, want 100", rec.Calls)
	}
	if rec.InputTokens != 1000 {
		t.Errorf("InputTokens = %d, want 1000", rec.InputTokens)
	}
}

// ── Complete: No Usage Field (Graceful) ──────────────────────────────────────

func TestOpenRouterClient_Complete_NoUsageField(t *testing.T) {
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": "Hello!",
				},
			}},
			"model": "test-model",
		})
	})

	resp, err := client.Complete(context.Background(), &LLMRequest{
		Model:     "test-model",
		Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		SessionID: "sess-no-usage",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.InputTokens != 0 {
		t.Errorf("InputTokens = %d, want 0", resp.InputTokens)
	}
	if resp.OutputTokens != 0 {
		t.Errorf("OutputTokens = %d, want 0", resp.OutputTokens)
	}
}

// ── Complete: No Choices (Graceful) ──────────────────────────────────────────

func TestOpenRouterClient_Complete_NoChoices(t *testing.T) {
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{},
			"model":   "test-model",
		})
	})

	resp, err := client.Complete(context.Background(), &LLMRequest{
		Model:     "test-model",
		Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		SessionID: "sess-no-choices",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Content != "" {
		t.Errorf("Content = %q, want empty", resp.Content)
	}
}

// ── Complete: Does Not Mutate Request ────────────────────────────────────────

func TestOpenRouterClient_Complete_DoesNotMutateRequest(t *testing.T) {
	_, client := mockOpenRouterServer(t, successHandler)

	req := &LLMRequest{
		Model:     "classifier",
		Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		SessionID: "sess-mutate",
	}

	_, err := client.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	// Model must remain "classifier", not resolved to "google/gemini-2.0-flash-001".
	if req.Model != "classifier" {
		t.Errorf("req.Model was mutated to %q, want %q", req.Model, "classifier")
	}
}

// ── Complete: HTTP Method is POST ────────────────────────────────────────────

func TestOpenRouterClient_Complete_HTTPMethodPOST(t *testing.T) {
	var gotMethod string
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		successHandler(w, r)
	})

	_, err := client.Complete(context.Background(), &LLMRequest{
		Model:     "test-model",
		Messages:  []LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		SessionID: "sess-method",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("HTTP method = %q, want %q", gotMethod, http.MethodPost)
	}
}
