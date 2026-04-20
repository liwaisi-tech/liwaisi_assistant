package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// ── Test Helper ──────────────────────────────────────────────────────────────

func mockOpenRouterServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := NewClient("test-api-key-secret", "test-default-model")
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
			"cost":              0.001,
		},
		"model": "test-default-model",
	})
}

// ── Constructor ──────────────────────────────────────────────────────────────

func TestNewClient(t *testing.T) {
	client := NewClient("my-key", "my-model")

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
	var _ cpn.LLMClient = (*Client)(nil)
}

// ── Complete: Success ────────────────────────────────────────────────────────

func TestOpenRouterClient_Complete_Success(t *testing.T) {
	_, client := mockOpenRouterServer(t, successHandler)

	resp, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "test-default-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
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

	resp, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "test-default-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Weather?"}},
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
		{400, cpn.ErrBadRequest},
		{401, cpn.ErrUnauthorized},
		{402, cpn.ErrInsufficientCredits},
		{403, cpn.ErrForbidden},
		{404, cpn.ErrNotFound},
		{408, cpn.ErrRequestTimeout},
		{413, cpn.ErrPayloadTooLarge},
		{422, cpn.ErrUnprocessableEntity},
		{429, cpn.ErrRateLimited},
		{500, cpn.ErrProviderUnavailable},
		{502, cpn.ErrProviderUnavailable},
		{503, cpn.ErrProviderUnavailable},
		{524, cpn.ErrEdgeTimeout},
		{529, cpn.ErrProviderOverloaded},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.status), func(t *testing.T) {
			_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			})

			_, err := client.Complete(context.Background(), &cpn.LLMRequest{
				Model:     "test-model",
				Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
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

	_, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		SessionID: "sess-rl",
	})
	if err != cpn.ErrRateLimited {
		t.Errorf("got %v, want cpn.ErrRateLimited", err)
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

	_, err := client.Complete(ctx, &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
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

	_, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
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

	_, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
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

	_, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
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

	_, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if hasHeader {
		t.Error("X-Session-Id header should be absent when SessionID is empty")
	}
}

// ── Complete: App Identification Headers ─────────────────────────────────────

func TestOpenRouterClient_Complete_AppHeaders(t *testing.T) {
	var gotReferer, gotTitle string
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotReferer = r.Header.Get("HTTP-Referer")
		gotTitle = r.Header.Get("X-Title")
		successHandler(w, r)
	})
	client.AppURL = "https://liwaisi.example.com"
	client.AppTitle = "brae"

	_, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if gotReferer != "https://liwaisi.example.com" {
		t.Errorf("HTTP-Referer = %q, want %q", gotReferer, "https://liwaisi.example.com")
	}
	if gotTitle != "brae" {
		t.Errorf("X-Title = %q, want %q", gotTitle, "brae")
	}
}

func TestOpenRouterClient_Complete_NoAppHeadersWhenEmpty(t *testing.T) {
	var hasReferer, hasTitle bool
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, r *http.Request) {
		hasReferer = r.Header.Get("HTTP-Referer") != ""
		hasTitle = r.Header.Get("X-Title") != ""
		successHandler(w, r)
	})

	_, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if hasReferer {
		t.Error("HTTP-Referer header should be absent when AppURL is empty")
	}
	if hasTitle {
		t.Error("X-Title header should be absent when AppTitle is empty")
	}
}

// ── Complete: Records to Ledger ──────────────────────────────────────────────

func TestOpenRouterClient_Complete_RecordsToLedger(t *testing.T) {
	_, client := mockOpenRouterServer(t, successHandler)

	_, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
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

	_, _ = client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
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

	_, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "classifier",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		SessionID: "sess-resolve",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	want := PRODUCT_DEFAULT_MODEL
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

	_, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "", // empty — should use DefaultModel
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
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

	_, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:       "test-model",
		Messages:    []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
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

	_, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		Tools: []*cpn.LLMTool{{
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
	client := NewClient("super-secret-key-12345", "my-model")
	s := client.String()

	if strings.Contains(s, "super-secret-key-12345") {
		t.Errorf("String() contains API key: %s", s)
	}
	if !strings.Contains(s, "my-model") {
		t.Errorf("String() should contain model: %s", s)
	}
	if !strings.Contains(s, "Client") {
		t.Errorf("String() should contain type name: %s", s)
	}
}

// ── EstimateCost: Known Model ────────────────────────────────────────────────

func TestOpenRouterClient_EstimateCost_KnownModel(t *testing.T) {
	client := NewClient("key", "google/gemini-2.0-flash-001")

	cost, err := client.EstimateCost(&cpn.LLMRequest{
		Model:     "google/gemini-2.0-flash-001",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: strings.Repeat("a", 400)}},
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
	client := NewClient("key", "unknown/model")

	cost, err := client.EstimateCost(&cpn.LLMRequest{
		Model:     "unknown/model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
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

	resp, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
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

	resp, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
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

	req := &cpn.LLMRequest{
		Model:     "classifier",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
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

	_, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
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

// ═══════════════════════════════════════════════════════════════════════════
// Block 19: Dual Endpoints, v1.2 cpn.LLMConfig, Format Functions, Retry Policy
// ═══════════════════════════════════════════════════════════════════════════

// ── Body Builders: Messages Endpoint ────────────────────────────────────────

func TestBuildMessagesBody_SystemExtracted(t *testing.T) {
	req := &cpn.LLMRequest{
		Model: "anthropic/claude-sonnet-4-6",
		Messages: []*cpn.LLMMessage{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hello"},
		},
		MaxTokens: 100,
		Endpoint:  cpn.EndpointMessages,
	}

	body, err := buildMessagesBody(req)
	if err != nil {
		t.Fatalf("buildMessagesBody: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// System prompt must be at top-level.
	sys, ok := parsed["system"]
	if !ok {
		t.Fatal("system field missing from messages body")
	}
	if sysStr, ok := sys.(string); !ok || sysStr != "You are a helpful assistant." {
		t.Errorf("system = %v, want %q", sys, "You are a helpful assistant.")
	}

	// Messages must NOT contain system role.
	msgs, _ := parsed["messages"].([]any)
	for _, m := range msgs {
		msg, _ := m.(map[string]any)
		if msg["role"] == "system" {
			t.Error("messages array must not contain system messages")
		}
	}
}

func TestBuildMessagesBody_ThinkingBlockSignature(t *testing.T) {
	// Signature with special chars that must be preserved byte-for-byte.
	signature := "aBc123+/=XyZ_sig-test!@#$%^&*()"

	req := &cpn.LLMRequest{
		Model: "anthropic/claude-opus-4-6",
		Messages: []*cpn.LLMMessage{
			{Role: "user", Content: "Think about this"},
			{
				Role:    "assistant",
				Content: "Here is my answer",
				ThinkingContent: &cpn.ThinkingBlock{
					Thinking:  "Let me think...",
					Signature: signature,
				},
			},
		},
		MaxTokens: 200,
		Endpoint:  cpn.EndpointMessages,
	}

	body, err := buildMessagesBody(req)
	if err != nil {
		t.Fatalf("buildMessagesBody: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	msgs, _ := parsed["messages"].([]any)
	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}

	// Second message (assistant) should have thinking block content.
	assistantMsg, _ := msgs[1].(map[string]any)
	content, _ := assistantMsg["content"].([]any)
	if len(content) == 0 {
		t.Fatal("assistant message content blocks empty")
	}

	thinkingBlock, _ := content[0].(map[string]any)
	if thinkingBlock["type"] != "thinking" {
		t.Errorf("first block type = %v, want thinking", thinkingBlock["type"])
	}
	if thinkingBlock["thinking"] != "Let me think..." {
		t.Errorf("thinking = %v, want %q", thinkingBlock["thinking"], "Let me think...")
	}
	gotSig, _ := thinkingBlock["signature"].(string)
	if gotSig != signature {
		t.Errorf("signature not preserved: got %q, want %q", gotSig, signature)
	}
}

func TestBuildMessagesBody_ExtendedThinking(t *testing.T) {
	req := &cpn.LLMRequest{
		Model:     "anthropic/claude-opus-4-6",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Think deeply"}},
		MaxTokens: 500,
		Endpoint:  cpn.EndpointMessages,
		Reasoning: &cpn.ReasoningConfig{BudgetTokens: 10000},
	}

	body, err := buildMessagesBody(req)
	if err != nil {
		t.Fatalf("buildMessagesBody: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	thinking, ok := parsed["thinking"].(map[string]any)
	if !ok {
		t.Fatal("thinking field missing")
	}
	if thinking["type"] != "enabled" {
		t.Errorf("thinking.type = %v, want enabled", thinking["type"])
	}
	budget, _ := thinking["budget_tokens"].(float64)
	if int(budget) != 10000 {
		t.Errorf("thinking.budget_tokens = %v, want 10000", budget)
	}
}

func TestBuildMessagesBody_CacheControl(t *testing.T) {
	req := &cpn.LLMRequest{
		Model: "anthropic/claude-sonnet-4-6",
		Messages: []*cpn.LLMMessage{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hi"},
		},
		MaxTokens: 100,
		Endpoint:  cpn.EndpointMessages,
		Cache:     &cpn.CacheControlConfig{TTL: "5m"},
	}

	body, err := buildMessagesBody(req)
	if err != nil {
		t.Fatalf("buildMessagesBody: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// System prompt should be array with cache_control.
	sys, ok := parsed["system"].([]any)
	if !ok {
		t.Fatal("system should be an array when caching is enabled")
	}
	sysBlock, _ := sys[0].(map[string]any)
	cc, _ := sysBlock["cache_control"].(map[string]any)
	if cc["type"] != "ephemeral" {
		t.Errorf("cache_control.type = %v, want ephemeral", cc["type"])
	}
}

// ── Body Builders: Completions Endpoint ─────────────────────────────────────

func TestBuildCompletionsBody_FallbackModels(t *testing.T) {
	req := &cpn.LLMRequest{
		Model:          "anthropic/claude-sonnet-4-6",
		Messages:       []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens:      100,
		FallbackModels: []string{"anthropic/claude-haiku-4-5-20251001", "google/gemini-2.0-flash-001"},
	}

	body, err := buildCompletionsBody(req)
	if err != nil {
		t.Fatalf("buildCompletionsBody: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	models, ok := parsed["models"].([]any)
	if !ok {
		t.Fatal("models field missing")
	}
	if len(models) != 3 {
		t.Fatalf("models len = %d, want 3", len(models))
	}
	if models[0] != "anthropic/claude-sonnet-4-6" {
		t.Errorf("models[0] = %v, want primary model", models[0])
	}
	if models[1] != "anthropic/claude-haiku-4-5-20251001" {
		t.Errorf("models[1] = %v, want first fallback", models[1])
	}
}

func TestBuildCompletionsBody_Reasoning(t *testing.T) {
	req := &cpn.LLMRequest{
		Model:     "anthropic/claude-sonnet-4-6",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Think"}},
		MaxTokens: 100,
		Reasoning: &cpn.ReasoningConfig{Effort: "high", Summary: "auto"},
	}

	body, err := buildCompletionsBody(req)
	if err != nil {
		t.Fatalf("buildCompletionsBody: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	reasoning, ok := parsed["reasoning"].(map[string]any)
	if !ok {
		t.Fatal("reasoning field missing")
	}
	if reasoning["effort"] != "high" {
		t.Errorf("reasoning.effort = %v, want high", reasoning["effort"])
	}
	if reasoning["summary"] != "auto" {
		t.Errorf("reasoning.summary = %v, want auto", reasoning["summary"])
	}
}

func TestBuildCompletionsBody_JSONSchema(t *testing.T) {
	strict := true
	req := &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
		JSONSchema: &cpn.JSONSchemaConfig{
			Name:        "my_schema",
			Description: "A test schema",
			Schema:      json.RawMessage(`{"type":"object"}`),
			Strict:      &strict,
		},
	}

	body, err := buildCompletionsBody(req)
	if err != nil {
		t.Fatalf("buildCompletionsBody: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	rf, ok := parsed["response_format"].(map[string]any)
	if !ok {
		t.Fatal("response_format missing")
	}
	if rf["type"] != "json_schema" {
		t.Errorf("response_format.type = %v, want json_schema", rf["type"])
	}
	js, _ := rf["json_schema"].(map[string]any)
	if js["name"] != "my_schema" {
		t.Errorf("json_schema.name = %v, want my_schema", js["name"])
	}
}

func TestBuildCompletionsBody_ReasoningAllFields(t *testing.T) {
	enabled := true
	exclude := false
	req := &cpn.LLMRequest{
		Model:     "anthropic/claude-sonnet-4-6",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Think"}},
		MaxTokens: 100,
		Reasoning: &cpn.ReasoningConfig{
			Effort:    "high",
			Summary:   "auto",
			MaxTokens: 2000,
			Enabled:   &enabled,
			Exclude:   &exclude,
		},
	}

	body, err := buildCompletionsBody(req)
	if err != nil {
		t.Fatalf("buildCompletionsBody: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	reasoning, ok := parsed["reasoning"].(map[string]any)
	if !ok {
		t.Fatal("reasoning field missing")
	}
	if reasoning["effort"] != "high" {
		t.Errorf("effort = %v, want high", reasoning["effort"])
	}
	if reasoning["summary"] != "auto" {
		t.Errorf("summary = %v, want auto", reasoning["summary"])
	}
	maxTok, _ := reasoning["max_tokens"].(float64)
	if int(maxTok) != 2000 {
		t.Errorf("max_tokens = %v, want 2000", maxTok)
	}
	if reasoning["enabled"] != true {
		t.Errorf("enabled = %v, want true", reasoning["enabled"])
	}
	if reasoning["exclude"] != false {
		t.Errorf("exclude = %v, want false", reasoning["exclude"])
	}
}

func TestComplete_ChatEndpoint_CacheTokensParsing(t *testing.T) {
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message": map[string]any{
					"role":    "assistant",
					"content": "Cached chat response.",
				},
			}},
			"usage": map[string]any{
				"prompt_tokens":     100,
				"completion_tokens": 20,
				"cost":              0.003,
				"prompt_tokens_details": map[string]any{
					"cached_tokens":      400,
					"cache_write_tokens": 100,
				},
			},
			"model": "test-model",
		})
	})

	resp, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
		SessionID: "sess-chat-cache",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.CacheReadTokens != 400 {
		t.Errorf("CacheReadTokens = %d, want 400", resp.CacheReadTokens)
	}
	if resp.CacheCreationTokens != 100 {
		t.Errorf("CacheCreationTokens = %d, want 100", resp.CacheCreationTokens)
	}
	if resp.CostUSD != 0.003 {
		t.Errorf("CostUSD = %f, want 0.003", resp.CostUSD)
	}

	// Verify ledger also got cache tokens.
	rec := client.TokenLedger.Get("sess-chat-cache")
	if rec == nil {
		t.Fatal("expected ledger record")
	}
	if rec.CacheReadTokens != 400 {
		t.Errorf("ledger CacheReadTokens = %d, want 400", rec.CacheReadTokens)
	}
}

func TestBuildCompletionsBody_CacheControlWithTTL(t *testing.T) {
	req := &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
		Cache:     &cpn.CacheControlConfig{TTL: "1h"},
	}

	body, err := buildCompletionsBody(req)
	if err != nil {
		t.Fatalf("buildCompletionsBody: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	cc, ok := parsed["cache_control"].(map[string]any)
	if !ok {
		t.Fatal("cache_control field missing")
	}
	if cc["type"] != "ephemeral" {
		t.Errorf("cache_control.type = %v, want ephemeral", cc["type"])
	}
	if cc["ttl"] != "1h" {
		t.Errorf("cache_control.ttl = %v, want 1h", cc["ttl"])
	}
}

func TestBuildMessagesBody_CacheControlWithTTL(t *testing.T) {
	req := &cpn.LLMRequest{
		Model: "anthropic/claude-sonnet-4-6",
		Messages: []*cpn.LLMMessage{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hi"},
		},
		MaxTokens: 100,
		Endpoint:  cpn.EndpointMessages,
		Cache:     &cpn.CacheControlConfig{TTL: "1h"},
	}

	body, err := buildMessagesBody(req)
	if err != nil {
		t.Fatalf("buildMessagesBody: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// System prompt cache_control should include TTL.
	sys, _ := parsed["system"].([]any)
	if len(sys) == 0 {
		t.Fatal("system should be an array when caching is enabled")
	}
	sysBlock, _ := sys[0].(map[string]any)
	cc, _ := sysBlock["cache_control"].(map[string]any)
	if cc["ttl"] != "1h" {
		t.Errorf("system cache_control.ttl = %v, want 1h", cc["ttl"])
	}

	// User message cache_control should also include TTL.
	msgs, _ := parsed["messages"].([]any)
	for _, m := range msgs {
		msg, _ := m.(map[string]any)
		if msg["role"] == "user" {
			ucc, ok := msg["cache_control"].(map[string]any)
			if !ok {
				t.Error("user message missing cache_control")
				continue
			}
			if ucc["ttl"] != "1h" {
				t.Errorf("user cache_control.ttl = %v, want 1h", ucc["ttl"])
			}
		}
	}
}

// ── Format Functions ────────────────────────────────────────────────────────

func TestFormatChatMessages_ToolResult(t *testing.T) {
	msgs := []*cpn.LLMMessage{
		{Role: "user", Content: "What's the weather?"},
		{
			Role:    "assistant",
			Content: "",
			ToolCall: &cpn.LLMToolCall{
				ID:        "call-1",
				ToolName:  "get_weather",
				Arguments: json.RawMessage(`{"city":"Lima"}`),
			},
		},
		{
			Role: "tool",
			ToolResult: &cpn.LLMToolResult{
				ToolCallID: "call-1",
				Content:    "Sunny, 25°C",
			},
		},
	}

	formatted := formatChatMessages(msgs)
	if len(formatted) != 3 {
		t.Fatalf("len = %d, want 3", len(formatted))
	}

	// Assistant message should have tool_calls.
	assistantMsg := formatted[1]
	tcs, ok := assistantMsg["tool_calls"].([]map[string]any)
	if !ok || len(tcs) == 0 {
		t.Fatal("assistant message missing tool_calls")
	}
	if tcs[0]["id"] != "call-1" {
		t.Errorf("tool_call id = %v, want call-1", tcs[0]["id"])
	}

	// Tool result message should have role "tool" and tool_call_id.
	toolMsg := formatted[2]
	if toolMsg["role"] != "tool" {
		t.Errorf("tool msg role = %v, want tool", toolMsg["role"])
	}
	if toolMsg["tool_call_id"] != "call-1" {
		t.Errorf("tool_call_id = %v, want call-1", toolMsg["tool_call_id"])
	}
	if toolMsg["content"] != "Sunny, 25°C" {
		t.Errorf("content = %v, want 'Sunny, 25°C'", toolMsg["content"])
	}
}

func TestBuildMessagesBody_WithTools(t *testing.T) {
	req := &cpn.LLMRequest{
		Model: "anthropic/claude-sonnet-4-6",
		Messages: []*cpn.LLMMessage{
			{Role: "user", Content: "Use the tool"},
		},
		MaxTokens: 100,
		Endpoint:  cpn.EndpointMessages,
		Tools: []*cpn.LLMTool{{
			Name:        "get_weather",
			Description: "Get weather",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
		}},
		ToolChoice: "auto",
	}

	body, err := buildMessagesBody(req)
	if err != nil {
		t.Fatalf("buildMessagesBody: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Tools should use input_schema (Anthropic format).
	tools, ok := parsed["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatal("tools missing from messages body")
	}
	tool, _ := tools[0].(map[string]any)
	if _, ok := tool["input_schema"]; !ok {
		t.Error("tool missing input_schema in messages body")
	}
	if _, ok := tool["parameters"]; ok {
		t.Error("tool should not have parameters in messages body (Anthropic uses input_schema)")
	}

	// ToolChoice should be serialized.
	if parsed["tool_choice"] != "auto" {
		t.Errorf("tool_choice = %v, want auto", parsed["tool_choice"])
	}
}

func TestFormatAnthropicTools_InputSchema(t *testing.T) {
	tools := []*cpn.LLMTool{{
		Name:        "get_weather",
		Description: "Get weather",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
	}}

	formatted := formatAnthropicTools(tools)
	if len(formatted) != 1 {
		t.Fatalf("len = %d, want 1", len(formatted))
	}

	tool := formatted[0]
	if _, ok := tool["input_schema"]; !ok {
		t.Error("input_schema missing from Anthropic tool format")
	}
	if _, ok := tool["parameters"]; ok {
		t.Error("parameters should NOT be present in Anthropic tool format")
	}
}

func TestFormatChatTools_Parameters(t *testing.T) {
	tools := []*cpn.LLMTool{{
		Name:        "get_weather",
		Description: "Get weather",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}}

	formatted := formatChatTools(tools)
	if len(formatted) != 1 {
		t.Fatalf("len = %d, want 1", len(formatted))
	}

	fn, _ := formatted[0]["function"].(map[string]any)
	if _, ok := fn["parameters"]; !ok {
		t.Error("parameters missing from chat tool format")
	}
	if _, ok := fn["input_schema"]; ok {
		t.Error("input_schema should NOT be present in chat tool format")
	}
}

func TestFormatProviderConfig_ZDR(t *testing.T) {
	p := &cpn.ProviderConfig{
		ZDR:            true,
		DataCollection: "allow", // Should be overridden by ZDR.
	}

	formatted := formatProviderConfig(p)
	dc, ok := formatted["data_collection"]
	if !ok {
		t.Fatal("data_collection missing")
	}
	if dc != "deny" {
		t.Errorf("data_collection = %v, want deny (ZDR enforcement)", dc)
	}
}

func TestFormatProviderConfig_AllFields(t *testing.T) {
	af := false
	p := &cpn.ProviderConfig{
		Order:             []string{"openai", "anthropic"},
		Only:              []string{"openai"},
		Ignore:            []string{"google"},
		AllowFallbacks:    &af,
		Sort:              "price",
		MaxPrice:          &cpn.ProviderMaxPrice{Prompt: "0.01", Completion: "0.02"},
		DataCollection:    "allow",
		RequireParameters: true,
	}

	formatted := formatProviderConfig(p)
	if formatted["sort"] != "price" {
		t.Errorf("sort = %v, want price", formatted["sort"])
	}
	if formatted["data_collection"] != "allow" {
		t.Errorf("data_collection = %v, want allow", formatted["data_collection"])
	}
	if formatted["require_parameters"] != true {
		t.Error("require_parameters missing or false")
	}
	order, _ := formatted["order"].([]string)
	if len(order) != 2 {
		t.Errorf("order len = %d, want 2", len(order))
	}
	mp, _ := formatted["max_price"].(map[string]string)
	if mp["prompt"] != "0.01" {
		t.Errorf("max_price.prompt = %v, want 0.01", mp["prompt"])
	}
}

func TestFormatPlugins(t *testing.T) {
	plugins := []cpn.PluginConfig{"auto-router", "moderation", "web"}

	formatted := formatPlugins(plugins)
	if len(formatted) != 3 {
		t.Fatalf("len = %d, want 3", len(formatted))
	}
	if formatted[0] != "auto-router" {
		t.Errorf("plugins[0] = %q, want auto-router", formatted[0])
	}
	if formatted[1] != "moderation" {
		t.Errorf("plugins[1] = %q, want moderation", formatted[1])
	}
	if formatted[2] != "web" {
		t.Errorf("plugins[2] = %q, want web", formatted[2])
	}
}

func TestFormatPlugins_InBody(t *testing.T) {
	req := &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
		Plugins:   []cpn.PluginConfig{"auto-router", "web"},
	}

	body, err := buildCompletionsBody(req)
	if err != nil {
		t.Fatalf("buildCompletionsBody: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	plugins, ok := parsed["plugins"].([]any)
	if !ok {
		t.Fatal("plugins field missing or wrong type")
	}
	if len(plugins) != 2 {
		t.Fatalf("plugins len = %d, want 2", len(plugins))
	}
	// Must be strings, not objects.
	if plugins[0] != "auto-router" {
		t.Errorf("plugins[0] = %v, want auto-router", plugins[0])
	}
	if plugins[1] != "web" {
		t.Errorf("plugins[1] = %v, want web", plugins[1])
	}
}

func TestFormatTrace(t *testing.T) {
	tr := &cpn.TraceConfig{
		TraceID:        "trace-1",
		TraceName:      "my-trace",
		SpanName:       "my-span",
		GenerationName: "gen-1",
		ParentSpanID:   "parent-1",
	}

	formatted := formatTrace(tr)
	if formatted["trace_id"] != "trace-1" {
		t.Errorf("trace_id = %v, want trace-1", formatted["trace_id"])
	}
	if formatted["trace_name"] != "my-trace" {
		t.Errorf("trace_name = %v, want my-trace", formatted["trace_name"])
	}
	if formatted["span_name"] != "my-span" {
		t.Errorf("span_name = %v, want my-span", formatted["span_name"])
	}
	if formatted["generation_name"] != "gen-1" {
		t.Errorf("generation_name = %v, want gen-1", formatted["generation_name"])
	}
	if formatted["parent_span_id"] != "parent-1" {
		t.Errorf("parent_span_id = %v, want parent-1", formatted["parent_span_id"])
	}
}

// ── Dual Routing ────────────────────────────────────────────────────────────

func TestComplete_RoutesToChatEndpoint(t *testing.T) {
	var gotPath string
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		successHandler(w, r)
	})

	_, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		SessionID: "sess-chat",
		Endpoint:  cpn.EndpointChat,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
	}
}

func TestComplete_RoutesToMessagesEndpoint(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "Hello!"},
			},
			"model":       "anthropic/claude-sonnet-4-6",
			"stop_reason": "end_turn",
			"usage": map[string]any{
				"input_tokens":  10,
				"output_tokens": 5,
			},
		})
	}))
	t.Cleanup(srv.Close)

	client := NewClient("test-key", "test-model")
	client.BaseURL = srv.URL

	_, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "anthropic/claude-sonnet-4-6",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 10,
		SessionID: "sess-msg",
		Endpoint:  cpn.EndpointMessages,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if gotPath != "/messages" {
		t.Errorf("path = %q, want /messages", gotPath)
	}
}

// ── Messages Response Parsing ───────────────────────────────────────────────

func TestComplete_MessagesResponse_ThinkingBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "thinking", "thinking": "Let me think...", "signature": "sig123abc"},
				{"type": "text", "text": "The answer is 42."},
			},
			"model":       "anthropic/claude-opus-4-6",
			"stop_reason": "end_turn",
			"usage":       map[string]any{"input_tokens": 20, "output_tokens": 15},
		})
	}))
	t.Cleanup(srv.Close)

	client := NewClient("test-key", "test-model")
	client.BaseURL = srv.URL

	resp, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "anthropic/claude-opus-4-6",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Think"}},
		MaxTokens: 100,
		SessionID: "sess-think",
		Endpoint:  cpn.EndpointMessages,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Content != "The answer is 42." {
		t.Errorf("Content = %q, want %q", resp.Content, "The answer is 42.")
	}
	if len(resp.ThinkingBlocks) != 1 {
		t.Fatalf("ThinkingBlocks len = %d, want 1", len(resp.ThinkingBlocks))
	}
	tb := resp.ThinkingBlocks[0]
	if tb.Thinking != "Let me think..." {
		t.Errorf("Thinking = %q, want %q", tb.Thinking, "Let me think...")
	}
	if tb.Signature != "sig123abc" {
		t.Errorf("Signature = %q, want %q", tb.Signature, "sig123abc")
	}
}

func TestComplete_MessagesResponse_CacheTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "Cached response."},
			},
			"model":       "anthropic/claude-sonnet-4-6",
			"stop_reason": "end_turn",
			"usage": map[string]any{
				"input_tokens":                10,
				"output_tokens":               5,
				"cache_read_input_tokens":     500,
				"cache_creation_input_tokens": 200,
			},
		})
	}))
	t.Cleanup(srv.Close)

	client := NewClient("test-key", "test-model")
	client.BaseURL = srv.URL

	resp, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "anthropic/claude-sonnet-4-6",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
		SessionID: "sess-cache",
		Endpoint:  cpn.EndpointMessages,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.CacheReadTokens != 500 {
		t.Errorf("CacheReadTokens = %d, want 500", resp.CacheReadTokens)
	}
	if resp.CacheCreationTokens != 200 {
		t.Errorf("CacheCreationTokens = %d, want 200", resp.CacheCreationTokens)
	}
}

// ── Retry Policy ────────────────────────────────────────────────────────────

func TestRetryOn_PermanentErrors(t *testing.T) {
	permanentErrors := []error{
		cpn.ErrBadRequest,
		cpn.ErrUnauthorized,
		cpn.ErrInsufficientCredits,
		cpn.ErrForbidden,
		cpn.ErrNotFound,
		cpn.ErrPayloadTooLarge,
		cpn.ErrUnprocessableEntity,
	}

	for _, err := range permanentErrors {
		if RetryOn(err, 1) {
			t.Errorf("RetryOn(%v) = true, want false (permanent)", err)
		}
	}
}

func TestRetryOn_TransientErrors(t *testing.T) {
	transientErrors := []error{
		cpn.ErrRequestTimeout,
		cpn.ErrRateLimited,
		cpn.ErrProviderUnavailable,
		cpn.ErrEdgeTimeout,
		cpn.ErrProviderOverloaded,
	}

	for _, err := range transientErrors {
		if !RetryOn(err, 1) {
			t.Errorf("RetryOn(%v) = false, want true (transient)", err)
		}
	}
}

func TestRetryOn_ContextErrors(t *testing.T) {
	if RetryOn(context.Canceled, 1) {
		t.Error("RetryOn(context.Canceled) = true, want false")
	}
	if RetryOn(context.DeadlineExceeded, 1) {
		t.Error("RetryOn(context.DeadlineExceeded) = true, want false")
	}
}

func TestRetryPolicy(t *testing.T) {
	policy := RetryPolicy()

	if policy.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", policy.MaxAttempts)
	}
	if policy.InitialWait != 500*time.Millisecond {
		t.Errorf("InitialWait = %v, want 500ms", policy.InitialWait)
	}
	if policy.MaxWait != 30*time.Second {
		t.Errorf("MaxWait = %v, want 30s", policy.MaxWait)
	}
	if policy.Multiplier != 2.0 {
		t.Errorf("Multiplier = %f, want 2.0", policy.Multiplier)
	}
	if policy.RetryOn == nil {
		t.Fatal("RetryOn is nil")
	}
	if policy.CircuitBreaker == nil {
		t.Fatal("CircuitBreaker is nil")
	}
	if policy.CircuitBreaker.FailureThreshold != 5 {
		t.Errorf("CB.FailureThreshold = %d, want 5", policy.CircuitBreaker.FailureThreshold)
	}
	if policy.CircuitBreaker.OpenDuration != 60*time.Second {
		t.Errorf("CB.OpenDuration = %v, want 60s", policy.CircuitBreaker.OpenDuration)
	}
}

// ── TokenLedger: RecordWithCache ────────────────────────────────────────────

func TestTokenLedger_RecordWithCache(t *testing.T) {
	ledger := NewTokenLedger()

	ledger.RecordWithCache("sess-cache", 100, 50, 500, 200, 0.005)
	ledger.RecordWithCache("sess-cache", 80, 40, 300, 100, 0.003)

	rec := ledger.Get("sess-cache")
	if rec == nil {
		t.Fatal("expected record")
	}
	if rec.InputTokens != 180 {
		t.Errorf("InputTokens = %d, want 180", rec.InputTokens)
	}
	if rec.OutputTokens != 90 {
		t.Errorf("OutputTokens = %d, want 90", rec.OutputTokens)
	}
	if rec.CacheReadTokens != 800 {
		t.Errorf("CacheReadTokens = %d, want 800", rec.CacheReadTokens)
	}
	if rec.CacheCreationTokens != 300 {
		t.Errorf("CacheCreationTokens = %d, want 300", rec.CacheCreationTokens)
	}
	if rec.Calls != 2 {
		t.Errorf("Calls = %d, want 2", rec.Calls)
	}
}

// ── Complete: RecordWithCache Integration ────────────────────────────────────

func TestComplete_MessagesEndpoint_RecordsCacheTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "Cached!"},
			},
			"model":       "anthropic/claude-sonnet-4-6",
			"stop_reason": "end_turn",
			"usage": map[string]any{
				"input_tokens":                10,
				"output_tokens":               5,
				"cache_read_input_tokens":     300,
				"cache_creation_input_tokens": 150,
				"total_cost":                  0.002,
			},
		})
	}))
	t.Cleanup(srv.Close)

	client := NewClient("test-key", "test-model")
	client.BaseURL = srv.URL

	_, err := client.Complete(context.Background(), &cpn.LLMRequest{
		Model:     "anthropic/claude-sonnet-4-6",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
		SessionID: "sess-cache-ledger",
		Endpoint:  cpn.EndpointMessages,
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	rec := client.TokenLedger.Get("sess-cache-ledger")
	if rec == nil {
		t.Fatal("expected ledger record")
	}
	if rec.CacheReadTokens != 300 {
		t.Errorf("CacheReadTokens = %d, want 300", rec.CacheReadTokens)
	}
	if rec.CacheCreationTokens != 150 {
		t.Errorf("CacheCreationTokens = %d, want 150", rec.CacheCreationTokens)
	}
}

// ── Backward Compatibility ──────────────────────────────────────────────────

func TestLLMConfig_BackwardCompatibility(t *testing.T) {
	// v1.1 usage: only 6 original fields. Must still compile and work.
	cfg := &cpn.LLMConfig{
		Model:        "reasoning",
		MaxTokens:    1000,
		Temperature:  0.7,
		StreamOutput: false,
		RequireJSON:  true,
		Budget:       0.05,
	}

	if cfg.Model != "reasoning" {
		t.Errorf("Model = %q, want reasoning", cfg.Model)
	}
	if cfg.MaxTokens != 1000 {
		t.Errorf("MaxTokens = %d, want 1000", cfg.MaxTokens)
	}
	if cfg.Temperature != 0.7 {
		t.Errorf("Temperature = %f, want 0.7", cfg.Temperature)
	}
	if cfg.RequireJSON != true {
		t.Error("RequireJSON should be true")
	}
	if cfg.Budget != 0.05 {
		t.Errorf("Budget = %f, want 0.05", cfg.Budget)
	}

	// New v1.2 fields should have zero values.
	if cfg.FallbackModels != nil {
		t.Error("FallbackModels should be nil for v1.1 usage")
	}
	if cfg.Endpoint != "" {
		t.Error("Endpoint should be empty for v1.1 usage")
	}
	if cfg.Provider != nil {
		t.Error("Provider should be nil for v1.1 usage")
	}
}

// ── Anthropic Messages: User Cache Control ──────────────────────────────────

func TestFormatAnthropicMessages_UserCacheControl(t *testing.T) {
	msgs := []*cpn.LLMMessage{
		{Role: "system", Content: "System prompt"},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
		{Role: "user", Content: "Follow up"},
	}
	cache := &cpn.CacheControlConfig{TTL: "5m"}

	formatted := formatAnthropicMessages(msgs, cache)

	// System messages should be excluded.
	for _, m := range formatted {
		if m["role"] == "system" {
			t.Error("system message should be excluded")
		}
	}

	// User messages should have cache_control.
	userCount := 0
	for _, m := range formatted {
		if m["role"] == "user" {
			userCount++
			cc, ok := m["cache_control"].(map[string]string)
			if !ok {
				t.Error("user message missing cache_control")
				continue
			}
			if cc["type"] != "ephemeral" {
				t.Errorf("cache_control.type = %v, want ephemeral", cc["type"])
			}
		}
		if m["role"] == "assistant" {
			if _, ok := m["cache_control"]; ok {
				t.Error("assistant message should NOT have cache_control")
			}
		}
	}
	if userCount != 2 {
		t.Errorf("expected 2 user messages, got %d", userCount)
	}
}

// ── ModelRegistry ───────────────────────────────────────────────────────────

func TestModelRegistry_ThinkingEntry(t *testing.T) {
	client := NewClient("key", "model")
	model, ok := client.ModelRegistry["thinking"]
	if !ok {
		t.Fatal("ModelRegistry missing 'thinking' entry")
	}
	if model != PRODUCT_DEFAULT_MODEL {
		t.Errorf("thinking model = %q, want %q", model, PRODUCT_DEFAULT_MODEL)
	}
}

func TestBuildModelRegistry_Defaults(t *testing.T) {
	noEnv := func(string) string { return "" }
	registry := buildModelRegistry(noEnv)

	for key, want := range DefaultModelRegistry {
		if got := registry[key]; got != want {
			t.Errorf("registry[%q] = %q, want %q", key, got, want)
		}
	}
}

// TestBuildModelRegistry_IgnoresEnv asserts REQ-CFG-002: removing the
// legacy per-role env variables means buildModelRegistry MUST NOT consult
// the env function at all. The test uses an env function that returns a
// sentinel for EVERY key; if buildModelRegistry ever looked up a key, the
// sentinel would leak into the registry and the assertion would fail.
func TestBuildModelRegistry_IgnoresEnv(t *testing.T) {
	callCount := 0
	registry := buildModelRegistry(func(string) string {
		callCount++
		return "custom/sentinel"
	})

	if callCount != 0 {
		t.Errorf("buildModelRegistry must not read env; called %d times", callCount)
	}
	for role, got := range registry {
		if got != PRODUCT_DEFAULT_MODEL {
			t.Errorf("registry[%q] = %q, want %q (env overrides must be ignored)", role, got, PRODUCT_DEFAULT_MODEL)
		}
	}
}

// TestProductDefaultModel asserts REQ-CFG-001: PRODUCT_DEFAULT_MODEL is a
// compile-time constant pinned to Gemini 3 Flash Preview.
func TestProductDefaultModel(t *testing.T) {
	if PRODUCT_DEFAULT_MODEL != "google/gemini-3-flash-preview" {
		t.Fatalf("PRODUCT_DEFAULT_MODEL = %q, want google/gemini-3-flash-preview", PRODUCT_DEFAULT_MODEL)
	}
	for role, model := range DefaultModelRegistry {
		if model != PRODUCT_DEFAULT_MODEL {
			t.Errorf("DefaultModelRegistry[%q] = %q, want %q", role, model, PRODUCT_DEFAULT_MODEL)
		}
	}
}

// ── Stream Flag in Request Body ────────────────────────────────────────────

func TestBuildCompletionsBody_StreamFlag(t *testing.T) {
	body, err := buildCompletionsBody(&cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
		Stream:    true,
	})
	if err != nil {
		t.Fatalf("buildCompletionsBody error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	stream, ok := parsed["stream"]
	if !ok {
		t.Fatal("stream field missing from completions body")
	}
	if stream != true {
		t.Errorf("stream = %v, want true", stream)
	}
}

func TestBuildCompletionsBody_NoStreamFlagByDefault(t *testing.T) {
	body, err := buildCompletionsBody(&cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("buildCompletionsBody error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if _, ok := parsed["stream"]; ok {
		t.Error("stream field should not be present when Stream is false")
	}
}

func TestBuildMessagesBody_StreamFlag(t *testing.T) {
	body, err := buildMessagesBody(&cpn.LLMRequest{
		Model:     "test-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
		Stream:    true,
	})
	if err != nil {
		t.Fatalf("buildMessagesBody error: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	stream, ok := parsed["stream"]
	if !ok {
		t.Fatal("stream field missing from messages body")
	}
	if stream != true {
		t.Errorf("stream = %v, want true", stream)
	}
}

// ── CompleteStream ─────────────────────────────────────────────────────────

func TestCompleteStream_ChatEndpoint(t *testing.T) {
	var gotStream bool
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		if s, ok := reqBody["stream"]; ok && s == true {
			gotStream = true
		}

		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2,\"cost\":0.001}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})

	var chunks []string
	resp, err := client.CompleteStream(context.Background(), &cpn.LLMRequest{
		Model:     "test-default-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
		SessionID: "sess-stream",
	}, func(chunk string) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("CompleteStream() error = %v", err)
	}
	if !gotStream {
		t.Error("request body did not contain stream: true")
	}
	if resp.Content != "Hello world" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello world")
	}
	if len(chunks) != 2 {
		t.Fatalf("chunks len = %d, want 2; got %v", len(chunks), chunks)
	}
	if chunks[0] != "Hello" || chunks[1] != " world" {
		t.Errorf("chunks = %v, want [Hello, world]", chunks)
	}
	if resp.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "stop")
	}
	if resp.InputTokens != 5 {
		t.Errorf("InputTokens = %d, want 5", resp.InputTokens)
	}
}

func TestCompleteStream_MessagesEndpoint(t *testing.T) {
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"claude-sonnet-4-6\",\"usage\":{\"input_tokens\":10}}}\n\n")
		fmt.Fprint(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Streamed!\"}}\n\n")
		fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n")
		fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	})

	var chunks []string
	resp, err := client.CompleteStream(context.Background(), &cpn.LLMRequest{
		Model:     "claude-sonnet-4-6",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
		Endpoint:  cpn.EndpointMessages,
		SessionID: "sess-stream-anth",
	}, func(chunk string) {
		chunks = append(chunks, chunk)
	})
	if err != nil {
		t.Fatalf("CompleteStream() error = %v", err)
	}
	if resp.Content != "Streamed!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Streamed!")
	}
	if len(chunks) != 1 || chunks[0] != "Streamed!" {
		t.Errorf("chunks = %v, want [Streamed!]", chunks)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "end_turn")
	}
}

func TestCompleteStream_NilCallback(t *testing.T) {
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})

	resp, err := client.CompleteStream(context.Background(), &cpn.LLMRequest{
		Model:     "test-default-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
	}, nil)
	if err != nil {
		t.Fatalf("CompleteStream() error = %v", err)
	}
	if resp.Content != "ok" {
		t.Errorf("Content = %q, want %q", resp.Content, "ok")
	}
}

func TestCompleteStream_ErrorStatus(t *testing.T) {
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, err := client.CompleteStream(context.Background(), &cpn.LLMRequest{
		Model:     "test-default-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
	}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "rate") {
		// ErrRateLimited
		if err.Error() == "" {
			t.Error("expected rate limit error")
		}
	}
}

func TestCompleteStream_TokenLedgerRecorded(t *testing.T) {
	_, client := mockOpenRouterServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5,\"cost\":0.002}}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	})

	_, err := client.CompleteStream(context.Background(), &cpn.LLMRequest{
		Model:     "test-default-model",
		Messages:  []*cpn.LLMMessage{{Role: "user", Content: "Hi"}},
		MaxTokens: 100,
		SessionID: "sess-ledger",
	}, nil)
	if err != nil {
		t.Fatalf("CompleteStream() error = %v", err)
	}

	rec := client.TokenLedger.Get("sess-ledger")
	if rec == nil {
		t.Fatal("TokenLedger record not found for sess-ledger")
	}
	if rec.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", rec.InputTokens)
	}
	if rec.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", rec.OutputTokens)
	}
}

// TestBuildModelRegistry_AllRolesMapToDefault asserts REQ-CFG-001: every
// role in DefaultModelRegistry resolves to PRODUCT_DEFAULT_MODEL. Replaces
// the pre-spec TestBuildModelRegistry_AllOverrides, which exercised the
// removed MODEL_* env override path.
func TestBuildModelRegistry_AllRolesMapToDefault(t *testing.T) {
	registry := buildModelRegistry(func(string) string { return "" })

	for _, role := range []string{"classifier", "structured", "reasoning", "long-context", "summarize", "thinking"} {
		got, ok := registry[role]
		if !ok {
			t.Errorf("registry missing role %q", role)
			continue
		}
		if got != PRODUCT_DEFAULT_MODEL {
			t.Errorf("registry[%q] = %q, want %q", role, got, PRODUCT_DEFAULT_MODEL)
		}
	}
}
