package cpn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// Compile-time interface check.
var _ LLMClient = (*OpenRouterClient)(nil)

// ── LLMClient Interface ─────────────────────────────────────────────────────

// LLMClient is the interface all LLM transitions call.
// OpenRouterClient is the production implementation.
// MockLLMClient is used in tests (defined in test files).
type LLMClient interface {
	// Complete sends a request to the LLM and returns the response.
	Complete(ctx context.Context, req *LLMRequest) (LLMResponse, error)

	// EstimateCost returns a USD cost estimate for the request
	// based on model pricing and estimated token counts.
	EstimateCost(req *LLMRequest) (float64, error)
}

// ── Request / Response Types ─────────────────────────────────────────────────

// LLMRequest is the input to LLMClient.Complete().
type LLMRequest struct {
	// Model is the OpenRouter model string or a ModelRegistry key.
	Model string

	// Messages is the conversation history including system prompt.
	Messages []*LLMMessage

	// MaxTokens is the hard cap for the completion.
	MaxTokens int

	// Temperature controls randomness (0.0 = deterministic, 2.0 = max).
	Temperature float64

	// Tools lists the tools available to the LLM for this request.
	Tools []*LLMTool

	// ResponseFmt is "json_object" when structured JSON output is required.
	ResponseFmt string

	// SessionID is passed to OpenRouter for per-user token tracking.
	SessionID string
}

// LLMResponse is the output from LLMClient.Complete().
type LLMResponse struct {
	// Content is the text content of the assistant's response.
	Content string

	// ToolCalls contains any tool invocations requested by the LLM.
	ToolCalls []*LLMToolCall

	// InputTokens is the number of prompt tokens consumed.
	InputTokens int

	// OutputTokens is the number of completion tokens generated.
	OutputTokens int

	// Model is the actual model used (may differ if fallback occurred).
	Model string

	// CostUSD is the actual cost reported by OpenRouter.
	CostUSD float64
}

// LLMConfig is per-transition model selection and budget constraints.
// Used by fireLLM (Block 9) to configure LLMRequest parameters.
type LLMConfig struct {
	// Model is an OpenRouter model string or ModelRegistry key.
	// If empty, falls back to OpenRouterClient.DefaultModel.
	Model string

	// MaxTokens is the hard cap for the completion. Required.
	MaxTokens int

	// Temperature controls randomness. Default 0.0 for deterministic.
	Temperature float64

	// StreamOutput enables real-time streaming (not implemented until Block 20).
	StreamOutput bool

	// RequireJSON sets response_format to json_object.
	RequireJSON bool

	// Budget is the per-call cost ceiling in USD. 0 disables budget check.
	Budget float64
}

// ── ModelRegistry ────────────────────────────────────────────────────────────

// ModelRegistry maps task roles to OpenRouter model strings.
// Using the cheapest model that can do the job operationalizes Axiom A11.
var ModelRegistry = map[string]string{
	"classifier":   "google/gemini-2.0-flash-001",
	"structured":   "anthropic/claude-haiku-4-5-20251001",
	"reasoning":    "anthropic/claude-sonnet-4-6",
	"long-context": "google/gemini-2.0-pro-001",
	"summarize":    "meta-llama/llama-3.3-8b-instruct",
}

// modelCostTable holds per-model pricing in USD per 1M tokens.
// [input_rate, output_rate] per 1M tokens.
var modelCostTable = map[string][2]float64{
	"google/gemini-2.0-flash-001":         {0.10, 0.40},
	"anthropic/claude-haiku-4-5-20251001": {1.00, 5.00},
	"anthropic/claude-sonnet-4-6":         {3.00, 15.00},
	"google/gemini-2.0-pro-001":           {1.25, 5.00},
	"meta-llama/llama-3.3-8b-instruct":    {0.05, 0.08},
}

// ── OpenRouterClient ─────────────────────────────────────────────────────────

// OpenRouterClient is the production LLMClient backed by OpenRouter.
type OpenRouterClient struct {
	// apiKey is the OpenRouter API key. Never logged.
	apiKey string

	// DefaultModel is used when LLMRequest.Model is empty.
	DefaultModel string

	// BaseURL defaults to "https://openrouter.ai/api/v1".
	BaseURL string

	// HTTPClient is the HTTP client for API calls. Injectable for testing.
	HTTPClient *http.Client

	// TokenLedger tracks per-session token usage and cost.
	TokenLedger *TokenLedger
}

// NewOpenRouterClient returns a configured client with sensible defaults.
func NewOpenRouterClient(apiKey, defaultModel string) *OpenRouterClient {
	return &OpenRouterClient{
		apiKey:       apiKey,
		DefaultModel: defaultModel,
		BaseURL:      "https://openrouter.ai/api/v1",
		HTTPClient: &http.Client{
			Timeout: 120 * time.Second,
		},
		TokenLedger: NewTokenLedger(),
	}
}

// String implements fmt.Stringer. Redacts the API key.
func (c *OpenRouterClient) String() string {
	return fmt.Sprintf("OpenRouterClient{model: %s, base: %s}", c.DefaultModel, c.BaseURL)
}

// Complete sends a request to the LLM and returns the response.
func (c *OpenRouterClient) Complete(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
	resolvedModel := c.resolveModel(req.Model)

	// Build a shallow copy with the resolved model to avoid mutating the caller's request.
	resolved := *req
	resolved.Model = resolvedModel

	body, err := buildRequestBody(&resolved)
	if err != nil {
		return LLMResponse{}, fmt.Errorf("build request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return LLMResponse{}, fmt.Errorf("create HTTP request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	if req.SessionID != "" {
		httpReq.Header.Set("X-Session-Id", req.SessionID)
	}

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return LLMResponse{}, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Drain body to allow connection reuse.
		_, _ = io.Copy(io.Discard, resp.Body)
		return LLMResponse{}, mapHTTPStatusToError(resp.StatusCode)
	}

	var apiResp openRouterResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return LLMResponse{}, fmt.Errorf("decode response: %w", err)
	}

	llmResp := parseLLMResponse(apiResp)

	if req.SessionID != "" {
		c.TokenLedger.Record(req.SessionID, llmResp.InputTokens, llmResp.OutputTokens, llmResp.CostUSD)
	}

	return llmResp, nil
}

// EstimateCost returns a USD cost estimate for the request
// based on model pricing and estimated token counts.
func (c *OpenRouterClient) EstimateCost(req *LLMRequest) (float64, error) {
	model := c.resolveModel(req.Model)

	rates, ok := modelCostTable[model]
	if !ok {
		return 0, nil
	}

	// Estimate input tokens: sum content lengths / 4.
	var inputChars int
	for _, m := range req.Messages {
		inputChars += len(m.Content)
	}
	estimatedInput := float64(inputChars) / 4.0
	estimatedOutput := float64(req.MaxTokens)

	cost := (estimatedInput*rates[0] + estimatedOutput*rates[1]) / 1_000_000
	return cost, nil
}

// resolveModel resolves a model string: check ModelRegistry first,
// fall back to using as-is, use DefaultModel if empty.
func (c *OpenRouterClient) resolveModel(model string) string {
	if model == "" {
		return c.DefaultModel
	}
	if resolved, ok := ModelRegistry[model]; ok {
		return resolved
	}
	return model
}

// ── Request Body Serialization ───────────────────────────────────────────────

// chatCompletionRequest is the OpenAI-compatible request body.
type chatCompletionRequest struct {
	Model          string              `json:"model"`
	Messages       []chatMessage       `json:"messages"`
	MaxTokens      int                 `json:"max_tokens"`
	Temperature    *float64            `json:"temperature,omitempty"`
	Tools          []chatTool          `json:"tools,omitempty"`
	ResponseFormat *chatResponseFormat `json:"response_format,omitempty"`
	SessionID      string              `json:"session_id,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatFunction `json:"function"`
}

type chatFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type chatResponseFormat struct {
	Type string `json:"type"`
}

// buildRequestBody produces OpenAI-compatible JSON for /chat/completions.
func buildRequestBody(req *LLMRequest) ([]byte, error) {
	body := chatCompletionRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
	}

	if req.SessionID != "" {
		body.SessionID = req.SessionID
	}

	if req.Temperature != 0 {
		t := req.Temperature
		body.Temperature = &t
	}

	body.Messages = make([]chatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, chatMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	for _, tool := range req.Tools {
		body.Tools = append(body.Tools, chatTool{
			Type:     "function",
			Function: chatFunction(*tool),
		})
	}

	if req.ResponseFmt != "" {
		body.ResponseFormat = &chatResponseFormat{Type: req.ResponseFmt}
	}

	return json.Marshal(body)
}

// ── Response Parsing ─────────────────────────────────────────────────────────

// openRouterResponse is the OpenAI-compatible response from OpenRouter.
type openRouterResponse struct {
	Choices []openRouterChoice `json:"choices"`
	Usage   *openRouterUsage   `json:"usage"`
	Model   string             `json:"model"`
}

type openRouterChoice struct {
	Message openRouterMessage `json:"message"`
}

type openRouterMessage struct {
	Role      string               `json:"role"`
	Content   string               `json:"content"`
	ToolCalls []openRouterToolCall `json:"tool_calls"`
}

type openRouterToolCall struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Function openRouterToolFunction `json:"function"`
}

type openRouterToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openRouterUsage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalCost        float64 `json:"total_cost"`
}

// parseLLMResponse converts the API response into an LLMResponse.
func parseLLMResponse(apiResp openRouterResponse) LLMResponse {
	var resp LLMResponse
	resp.Model = apiResp.Model

	if len(apiResp.Choices) > 0 {
		msg := apiResp.Choices[0].Message
		resp.Content = msg.Content

		for _, tc := range msg.ToolCalls {
			resp.ToolCalls = append(resp.ToolCalls, &LLMToolCall{
				ID:        tc.ID,
				ToolName:  tc.Function.Name,
				Arguments: json.RawMessage(tc.Function.Arguments),
			})
		}
	}

	if apiResp.Usage != nil {
		resp.InputTokens = apiResp.Usage.PromptTokens
		resp.OutputTokens = apiResp.Usage.CompletionTokens
		resp.CostUSD = apiResp.Usage.TotalCost
	}

	return resp
}

// ── HTTP Error Mapping ───────────────────────────────────────────────────────

// mapHTTPStatusToError maps OpenRouter HTTP status codes to sentinel errors.
func mapHTTPStatusToError(status int) error {
	switch status {
	case 400:
		return ErrBadRequest
	case 401:
		return ErrUnauthorized
	case 402:
		return ErrInsufficientCredits
	case 403:
		return ErrForbidden
	case 404:
		return ErrNotFound
	case 408:
		return ErrRequestTimeout
	case 413:
		return ErrPayloadTooLarge
	case 422:
		return ErrUnprocessableEntity
	case 429:
		return ErrRateLimited
	case 524:
		return ErrEdgeTimeout
	case 529:
		return ErrProviderOverloaded
	default:
		if status >= 500 && status < 600 {
			return ErrProviderUnavailable
		}
		return fmt.Errorf("unexpected HTTP status %d", status)
	}
}

// ── TokenLedger ──────────────────────────────────────────────────────────────

// TokenLedger tracks per-session token usage and cost. Thread-safe.
type TokenLedger struct {
	records map[string]*SessionTokenRecord
	mu      sync.RWMutex
}

// SessionTokenRecord accumulates usage for one session.
type SessionTokenRecord struct {
	SessionID    string
	InputTokens  int
	OutputTokens int
	TotalCostUSD float64
	Calls        int
	LastUpdated  time.Time
}

// NewTokenLedger creates an initialized TokenLedger.
func NewTokenLedger() *TokenLedger {
	return &TokenLedger{
		records: make(map[string]*SessionTokenRecord),
	}
}

// Record accumulates token usage and cost for a session. Thread-safe.
func (l *TokenLedger) Record(sessionID string, in, out int, costUSD float64) {
	l.mu.Lock()
	defer l.mu.Unlock()

	rec, ok := l.records[sessionID]
	if !ok {
		rec = &SessionTokenRecord{SessionID: sessionID}
		l.records[sessionID] = rec
	}

	rec.InputTokens += in
	rec.OutputTokens += out
	rec.TotalCostUSD += costUSD
	rec.Calls++
	rec.LastUpdated = time.Now()
}

// Get returns a snapshot of the session record, or nil for unknown sessions.
// The returned value is a copy — safe to read without holding any lock.
func (l *TokenLedger) Get(sessionID string) *SessionTokenRecord {
	l.mu.RLock()
	defer l.mu.RUnlock()
	rec, ok := l.records[sessionID]
	if !ok {
		return nil
	}
	cp := *rec
	return &cp
}
