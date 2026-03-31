package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// Compile-time interface check.
var _ cpn.LLMClient = (*Client)(nil)

// ── ModelRegistry ────────────────────────────────────────────────────────────

// ModelRegistry maps task roles to OpenRouter model strings.
// Using the cheapest model that can do the job operationalizes Axiom A11.
var ModelRegistry = map[string]string{
	"classifier":   "google/gemini-2.0-flash-001",
	"structured":   "anthropic/claude-haiku-4-5-20251001",
	"reasoning":    "anthropic/claude-sonnet-4-6",
	"long-context": "google/gemini-2.0-pro-001",
	"summarize":    "meta-llama/llama-3.3-8b-instruct",
	"thinking":     "anthropic/claude-opus-4-6",
}

// modelCostTable holds per-model pricing in USD per 1M tokens.
// [input_rate, output_rate] per 1M tokens.
var modelCostTable = map[string][2]float64{
	"google/gemini-2.0-flash-001":         {0.10, 0.40},
	"anthropic/claude-haiku-4-5-20251001": {1.00, 5.00},
	"anthropic/claude-sonnet-4-6":         {3.00, 15.00},
	"google/gemini-2.0-pro-001":           {1.25, 5.00},
	"meta-llama/llama-3.3-8b-instruct":    {0.05, 0.08},
	"anthropic/claude-opus-4-6":           {15.00, 75.00},
}

// ── Client ─────────────────────────────────────────────────────────

// Client is the production LLMClient backed by OpenRouter.
type Client struct {
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

// NewClient returns a configured client with sensible defaults.
func NewClient(apiKey, defaultModel string) *Client {
	return &Client{
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
func (c *Client) String() string {
	return fmt.Sprintf("Client{model: %s, base: %s}", c.DefaultModel, c.BaseURL)
}

// Complete sends a request to the LLM and returns the response.
// Routes to /chat/completions (EndpointChat, default) or /messages (EndpointMessages).
func (c *Client) Complete(ctx context.Context, req *cpn.LLMRequest) (cpn.LLMResponse, error) {
	resolvedModel := c.resolveModel(req.Model)

	// Build a shallow copy with the resolved model to avoid mutating the caller's request.
	resolved := *req
	resolved.Model = resolvedModel

	var body []byte
	var endpoint string
	var buildErr error

	switch resolved.Endpoint {
	case cpn.EndpointMessages:
		body, buildErr = buildMessagesBody(&resolved)
		endpoint = c.BaseURL + "/messages"
	default: // EndpointChat or empty
		body, buildErr = buildCompletionsBody(&resolved)
		endpoint = c.BaseURL + "/chat/completions"
	}
	if buildErr != nil {
		return cpn.LLMResponse{}, fmt.Errorf("build request body: %w", buildErr)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return cpn.LLMResponse{}, fmt.Errorf("create HTTP request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	if req.SessionID != "" {
		httpReq.Header.Set("X-Session-Id", req.SessionID)
	}

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return cpn.LLMResponse{}, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return cpn.LLMResponse{}, mapHTTPStatusToError(resp.StatusCode)
	}

	var llmResp cpn.LLMResponse
	switch resolved.Endpoint {
	case cpn.EndpointMessages:
		llmResp, err = parseMessagesResponse(resp.Body)
	default:
		var apiResp openRouterResponse
		if err2 := json.NewDecoder(resp.Body).Decode(&apiResp); err2 != nil {
			return cpn.LLMResponse{}, fmt.Errorf("decode response: %w", err2)
		}
		llmResp = parseLLMResponse(apiResp)
	}
	if err != nil {
		return cpn.LLMResponse{}, err
	}

	if req.SessionID != "" {
		if llmResp.CacheReadTokens > 0 || llmResp.CacheCreationTokens > 0 {
			c.TokenLedger.RecordWithCache(req.SessionID, llmResp.InputTokens, llmResp.OutputTokens,
				llmResp.CacheReadTokens, llmResp.CacheCreationTokens, llmResp.CostUSD)
		} else {
			c.TokenLedger.Record(req.SessionID, llmResp.InputTokens, llmResp.OutputTokens, llmResp.CostUSD)
		}
	}

	return llmResp, nil
}

// EstimateCost returns a USD cost estimate for the request
// based on model pricing and estimated token counts.
func (c *Client) EstimateCost(req *cpn.LLMRequest) (float64, error) {
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
func (c *Client) resolveModel(model string) string {
	if model == "" {
		return c.DefaultModel
	}
	if resolved, ok := ModelRegistry[model]; ok {
		return resolved
	}
	return model
}

// ── Format Functions ────────────────────────────────────────────────────────

// formatChatMessages converts LLMMessages to OpenAI chat format.
// System messages remain in-place. Tool results use role "tool" with tool_call_id.
func formatChatMessages(msgs []*cpn.LLMMessage) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		msg := map[string]any{
			"role":    m.Role,
			"content": m.Content,
		}
		if m.ToolCall != nil {
			msg["tool_calls"] = []map[string]any{{
				"id":   m.ToolCall.ID,
				"type": "function",
				"function": map[string]any{
					"name":      m.ToolCall.ToolName,
					"arguments": string(m.ToolCall.Arguments),
				},
			}}
		}
		if m.ToolResult != nil {
			msg["role"] = "tool"
			msg["tool_call_id"] = m.ToolResult.ToolCallID
			msg["content"] = m.ToolResult.Content
		}
		out = append(out, msg)
	}
	return out
}

// formatAnthropicMessages converts LLMMessages to Anthropic /messages format.
// System messages are excluded (extracted to top-level by buildMessagesBody).
// ThinkingBlocks are serialized with signature preserved byte-for-byte.
// Cache control is applied to user messages when cache config is set.
func formatAnthropicMessages(msgs []*cpn.LLMMessage, cache *cpn.CacheControlConfig) []map[string]any {
	out := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "system" {
			continue
		}

		msg := map[string]any{
			"role": m.Role,
		}

		// Build content blocks based on message type.
		switch {
		case m.ThinkingContent != nil:
			blocks := []map[string]any{
				{
					"type":      "thinking",
					"thinking":  m.ThinkingContent.Thinking,
					"signature": m.ThinkingContent.Signature,
				},
			}
			if m.Content != "" {
				blocks = append(blocks, map[string]any{
					"type": "text",
					"text": m.Content,
				})
			}
			msg["content"] = blocks
		case m.ToolResult != nil:
			msg["content"] = []map[string]any{{
				"type":        "tool_result",
				"tool_use_id": m.ToolResult.ToolCallID,
				"content":     m.ToolResult.Content,
			}}
		case m.ToolCall != nil:
			msg["content"] = []map[string]any{{
				"type":  "tool_use",
				"id":    m.ToolCall.ID,
				"name":  m.ToolCall.ToolName,
				"input": m.ToolCall.Arguments,
			}}
		default:
			msg["content"] = m.Content
		}

		// Apply cache control to user messages.
		if cache != nil && m.Role == "user" {
			msg["cache_control"] = formatCacheControl(cache)
		}

		out = append(out, msg)
	}
	return out
}

// formatChatTools converts LLMTools to OpenAI chat format (uses "parameters").
func formatChatTools(tools []*cpn.LLMTool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  t.Parameters,
			},
		})
	}
	return out
}

// formatAnthropicTools converts LLMTools to Anthropic format (uses "input_schema").
func formatAnthropicTools(tools []*cpn.LLMTool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		out = append(out, map[string]any{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": t.Parameters,
		})
	}
	return out
}

// formatProviderConfig serializes ProviderConfig to the OpenRouter body format.
// ZDR enforcement: when ZDR==true, data_collection is forced to "deny".
func formatProviderConfig(p *cpn.ProviderConfig) map[string]any {
	out := map[string]any{}

	if len(p.Order) > 0 {
		out["order"] = p.Order
	}
	if len(p.Only) > 0 {
		out["only"] = p.Only
	}
	if len(p.Ignore) > 0 {
		out["ignore"] = p.Ignore
	}
	if p.AllowFallbacks != nil {
		out["allow_fallbacks"] = *p.AllowFallbacks
	}
	if p.Sort != "" {
		out["sort"] = p.Sort
	}
	if p.RequireParameters {
		out["require_parameters"] = true
	}
	if p.MaxPrice != nil {
		mp := map[string]string{}
		if p.MaxPrice.Prompt != "" {
			mp["prompt"] = p.MaxPrice.Prompt
		}
		if p.MaxPrice.Completion != "" {
			mp["completion"] = p.MaxPrice.Completion
		}
		if p.MaxPrice.Image != "" {
			mp["image"] = p.MaxPrice.Image
		}
		if p.MaxPrice.Audio != "" {
			mp["audio"] = p.MaxPrice.Audio
		}
		if p.MaxPrice.Request != "" {
			mp["request"] = p.MaxPrice.Request
		}
		if len(mp) > 0 {
			out["max_price"] = mp
		}
	}

	// SEC-001: ZDR enforcement at serialization layer.
	if p.ZDR {
		out["data_collection"] = "deny"
	} else if p.DataCollection != "" {
		out["data_collection"] = p.DataCollection
	}

	return out
}

// formatPlugins returns the plugins slice directly.
// OpenRouter plugins are string identifiers per the API spec.
func formatPlugins(plugins []cpn.PluginConfig) []string {
	return plugins
}

// formatTrace serializes TraceConfig to the OpenRouter body format.
func formatTrace(t *cpn.TraceConfig) map[string]any {
	out := map[string]any{}
	if t.TraceID != "" {
		out["trace_id"] = t.TraceID
	}
	if t.TraceName != "" {
		out["trace_name"] = t.TraceName
	}
	if t.SpanName != "" {
		out["span_name"] = t.SpanName
	}
	if t.GenerationName != "" {
		out["generation_name"] = t.GenerationName
	}
	if t.ParentSpanID != "" {
		out["parent_span_id"] = t.ParentSpanID
	}
	return out
}

// ── Body Builders ───────────────────────────────────────────────────────────

// buildCompletionsBody produces OpenAI-compatible JSON for /chat/completions.
func buildCompletionsBody(req *cpn.LLMRequest) ([]byte, error) {
	body := map[string]any{
		"model":      req.Model,
		"messages":   formatChatMessages(req.Messages),
		"max_tokens": req.MaxTokens,
	}

	if req.Temperature != 0 {
		body["temperature"] = req.Temperature
	}
	if req.SessionID != "" {
		body["session_id"] = req.SessionID
	}
	if len(req.Tools) > 0 {
		body["tools"] = formatChatTools(req.Tools)
	}
	if req.ToolChoice != "" {
		body["tool_choice"] = req.ToolChoice
	}

	// Response format.
	if req.JSONSchema != nil {
		schema := map[string]any{
			"name":   req.JSONSchema.Name,
			"schema": req.JSONSchema.Schema,
		}
		if req.JSONSchema.Description != "" {
			schema["description"] = req.JSONSchema.Description
		}
		if req.JSONSchema.Strict != nil {
			schema["strict"] = *req.JSONSchema.Strict
		}
		body["response_format"] = map[string]any{
			"type":        "json_schema",
			"json_schema": schema,
		}
	} else if req.ResponseFmt != "" {
		body["response_format"] = map[string]any{"type": req.ResponseFmt}
	}

	// FallbackModels → "models" array.
	if len(req.FallbackModels) > 0 {
		models := make([]string, 0, 1+len(req.FallbackModels))
		models = append(models, req.Model)
		models = append(models, req.FallbackModels...)
		body["models"] = models
	}

	// Reasoning for chat endpoint — all documented fields.
	if req.Reasoning != nil {
		r := map[string]any{}
		if req.Reasoning.Effort != "" {
			r["effort"] = req.Reasoning.Effort
		}
		if req.Reasoning.Summary != "" {
			r["summary"] = req.Reasoning.Summary
		}
		if req.Reasoning.MaxTokens > 0 {
			r["max_tokens"] = req.Reasoning.MaxTokens
		}
		if req.Reasoning.Enabled != nil {
			r["enabled"] = *req.Reasoning.Enabled
		}
		if req.Reasoning.Exclude != nil {
			r["exclude"] = *req.Reasoning.Exclude
		}
		if len(r) > 0 {
			body["reasoning"] = r
		}
	}

	// Shared fields.
	if req.Provider != nil {
		body["provider"] = formatProviderConfig(req.Provider)
	}
	if req.Trace != nil {
		body["trace"] = formatTrace(req.Trace)
	}
	if len(req.Plugins) > 0 {
		body["plugins"] = formatPlugins(req.Plugins)
	}
	if req.Cache != nil {
		body["cache_control"] = formatCacheControl(req.Cache)
	}

	return json.Marshal(body)
}

// formatCacheControl serializes CacheControlConfig per the OpenRouter API spec.
// Always includes type:"ephemeral". Includes ttl when set (e.g., "1h").
func formatCacheControl(c *cpn.CacheControlConfig) map[string]string {
	cc := map[string]string{"type": "ephemeral"}
	if c.TTL != "" {
		cc["ttl"] = c.TTL
	}
	return cc
}

// buildMessagesBody produces Anthropic-native JSON for /messages.
func buildMessagesBody(req *cpn.LLMRequest) ([]byte, error) {
	body := map[string]any{
		"model":      req.Model,
		"max_tokens": req.MaxTokens,
	}

	// Extract system prompt to top-level field.
	var systemContent string
	for _, m := range req.Messages {
		if m.Role == "system" {
			systemContent = m.Content
			break
		}
	}
	if systemContent != "" {
		if req.Cache != nil {
			// Prompt caching: system as array with cache_control block.
			body["system"] = []map[string]any{
				{
					"type":          "text",
					"text":          systemContent,
					"cache_control": formatCacheControl(req.Cache),
				},
			}
		} else {
			body["system"] = systemContent
		}
	}

	body["messages"] = formatAnthropicMessages(req.Messages, req.Cache)

	if req.Temperature != 0 {
		body["temperature"] = req.Temperature
	}
	if req.SessionID != "" {
		body["session_id"] = req.SessionID
	}
	if len(req.Tools) > 0 {
		body["tools"] = formatAnthropicTools(req.Tools)
	}
	if req.ToolChoice != "" {
		body["tool_choice"] = req.ToolChoice
	}

	// Extended thinking for messages endpoint.
	if req.Reasoning != nil && req.Reasoning.BudgetTokens > 0 {
		body["thinking"] = map[string]any{
			"type":          "enabled",
			"budget_tokens": req.Reasoning.BudgetTokens,
		}
	}

	// FallbackModels → "models" array.
	if len(req.FallbackModels) > 0 {
		models := make([]string, 0, 1+len(req.FallbackModels))
		models = append(models, req.Model)
		models = append(models, req.FallbackModels...)
		body["models"] = models
	}

	// Shared fields.
	if req.Provider != nil {
		body["provider"] = formatProviderConfig(req.Provider)
	}
	if req.Trace != nil {
		body["trace"] = formatTrace(req.Trace)
	}
	if len(req.Plugins) > 0 {
		body["plugins"] = formatPlugins(req.Plugins)
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
	PromptTokens        int                  `json:"prompt_tokens"`
	CompletionTokens    int                  `json:"completion_tokens"`
	TotalCost           float64              `json:"total_cost"`
	Cost                float64              `json:"cost"`
	PromptTokensDetails *promptTokensDetails `json:"prompt_tokens_details"`
}

// promptTokensDetails holds cache metrics from the chat/completions endpoint.
type promptTokensDetails struct {
	CachedTokens     int `json:"cached_tokens"`
	CacheWriteTokens int `json:"cache_write_tokens"`
}

// parseLLMResponse converts the chat/completions API response into an LLMResponse.
func parseLLMResponse(apiResp openRouterResponse) cpn.LLMResponse {
	var resp cpn.LLMResponse
	resp.Model = apiResp.Model

	if len(apiResp.Choices) > 0 {
		msg := apiResp.Choices[0].Message
		resp.Content = msg.Content

		for _, tc := range msg.ToolCalls {
			resp.ToolCalls = append(resp.ToolCalls, &cpn.LLMToolCall{
				ID:        tc.ID,
				ToolName:  tc.Function.Name,
				Arguments: json.RawMessage(tc.Function.Arguments),
			})
		}
	}

	if apiResp.Usage != nil {
		resp.InputTokens = apiResp.Usage.PromptTokens
		resp.OutputTokens = apiResp.Usage.CompletionTokens
		// OpenRouter uses "cost" at top-level; fall back to "total_cost" for compat.
		resp.CostUSD = apiResp.Usage.Cost
		if resp.CostUSD == 0 {
			resp.CostUSD = apiResp.Usage.TotalCost
		}
		// Cache tokens from prompt_tokens_details (chat/completions format).
		if apiResp.Usage.PromptTokensDetails != nil {
			resp.CacheReadTokens = apiResp.Usage.PromptTokensDetails.CachedTokens
			resp.CacheCreationTokens = apiResp.Usage.PromptTokensDetails.CacheWriteTokens
		}
	}

	return resp
}

// ── Anthropic Messages Response Parsing ─────────────────────────────────────

// anthropicMessagesResponse represents the Anthropic /messages API response.
type anthropicMessagesResponse struct {
	Content    []anthropicContentBlock `json:"content"`
	Model      string                  `json:"model"`
	StopReason string                  `json:"stop_reason"`
	Usage      *anthropicUsage         `json:"usage"`
}

type anthropicContentBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
	// Tool use fields.
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type anthropicUsage struct {
	InputTokens              int     `json:"input_tokens"`
	OutputTokens             int     `json:"output_tokens"`
	CacheReadInputTokens     int     `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int     `json:"cache_creation_input_tokens"`
	TotalCost                float64 `json:"total_cost"`
}

// parseMessagesResponse parses an Anthropic /messages response body.
func parseMessagesResponse(body io.Reader) (cpn.LLMResponse, error) {
	var apiResp anthropicMessagesResponse
	if err := json.NewDecoder(body).Decode(&apiResp); err != nil {
		return cpn.LLMResponse{}, fmt.Errorf("decode messages response: %w", err)
	}

	var resp cpn.LLMResponse
	resp.Model = apiResp.Model
	resp.StopReason = apiResp.StopReason

	for _, block := range apiResp.Content {
		switch block.Type {
		case "text":
			if resp.Content != "" {
				resp.Content += block.Text
			} else {
				resp.Content = block.Text
			}
		case "thinking":
			resp.ThinkingBlocks = append(resp.ThinkingBlocks, cpn.ThinkingBlock{
				Thinking:  block.Thinking,
				Signature: block.Signature,
			})
		case "tool_use":
			resp.ToolCalls = append(resp.ToolCalls, &cpn.LLMToolCall{
				ID:        block.ID,
				ToolName:  block.Name,
				Arguments: block.Input,
			})
		}
	}

	if apiResp.Usage != nil {
		resp.InputTokens = apiResp.Usage.InputTokens
		resp.OutputTokens = apiResp.Usage.OutputTokens
		resp.CostUSD = apiResp.Usage.TotalCost
		resp.CacheReadTokens = apiResp.Usage.CacheReadInputTokens
		resp.CacheCreationTokens = apiResp.Usage.CacheCreationInputTokens
	}

	return resp, nil
}

// ── HTTP Error Mapping ───────────────────────────────────────────────────────

// mapHTTPStatusToError maps OpenRouter HTTP status codes to sentinel errors.
func mapHTTPStatusToError(status int) error {
	switch status {
	case 400:
		return cpn.ErrBadRequest
	case 401:
		return cpn.ErrUnauthorized
	case 402:
		return cpn.ErrInsufficientCredits
	case 403:
		return cpn.ErrForbidden
	case 404:
		return cpn.ErrNotFound
	case 408:
		return cpn.ErrRequestTimeout
	case 413:
		return cpn.ErrPayloadTooLarge
	case 422:
		return cpn.ErrUnprocessableEntity
	case 429:
		return cpn.ErrRateLimited
	case 524:
		return cpn.ErrEdgeTimeout
	case 529:
		return cpn.ErrProviderOverloaded
	default:
		if status >= 500 && status < 600 {
			return cpn.ErrProviderUnavailable
		}
		return fmt.Errorf("unexpected HTTP status %d", status)
	}
}

// ── OpenRouter Retry Policy ─────────────────────────────────────────────────

// openRouterPermanentErrors lists errors that should never be retried.
var openRouterPermanentErrors = []error{
	cpn.ErrBadRequest,
	cpn.ErrUnauthorized,
	cpn.ErrInsufficientCredits,
	cpn.ErrForbidden,
	cpn.ErrNotFound,
	cpn.ErrPayloadTooLarge,
	cpn.ErrUnprocessableEntity,
}

// RetryOn classifies errors as permanent (no retry) or transient (retry).
// Returns false for context errors and client-side 4xx errors.
// Returns true for transient errors (408, 429, 5xx, 524, 529).
func RetryOn(err error, _ int) bool {
	if err == nil {
		return false
	}

	// Context errors — never retry.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Permanent client errors — never retry.
	for _, pe := range openRouterPermanentErrors {
		if errors.Is(err, pe) {
			return false
		}
	}

	// All other errors are considered transient.
	return true
}

// RetryPolicy returns the domain-specific retry policy for OpenRouter.
// MaxAttempts=3, InitialWait=500ms, MaxWait=30s, Multiplier=2.0,
// CircuitBreaker(FailureThreshold=5, OpenDuration=60s).
func RetryPolicy() *cpn.RetryPolicy {
	return &cpn.RetryPolicy{
		MaxAttempts: 3,
		InitialWait: 500 * time.Millisecond,
		MaxWait:     30 * time.Second,
		Multiplier:  2.0,
		RetryOn:     RetryOn,
		CircuitBreaker: &cpn.CircuitBreakerConfig{
			FailureThreshold: 5,
			OpenDuration:     60 * time.Second,
		},
	}
}
