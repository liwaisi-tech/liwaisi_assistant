package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// Compile-time interface verification.
var _ output.LLMClient = (*Client)(nil)

// ClientConfig holds the configuration for the OpenRouter client.
type ClientConfig struct {
	APIKey  string
	BaseURL string
	Model   string
	Referer string
	Title   string
	Timeout time.Duration
	Retry   RetryConfig
}

// Client is an OpenRouter HTTP client implementing output.LLMClient.
type Client struct {
	apiKey           string
	baseURL          string
	model            string
	httpClient       *http.Client
	streamHTTPClient *http.Client
	referer          string
	title            string
	timeout          time.Duration
	retry            RetryConfig
	tracer           trace.Tracer
	requestCounter   metric.Int64Counter
	durationHist     metric.Float64Histogram
	tokenCounter     metric.Int64Counter
	errorCounter     metric.Int64Counter
}

// NewClient creates a new OpenRouter client.
func NewClient(cfg *ClientConfig) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("openrouter: API key is required")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://openrouter.ai/api/v1"
	}
	title := cfg.Title
	if title == "" {
		title = "liwaisi"
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	retry := cfg.Retry
	if retry.MaxRetries == 0 && len(retry.RetryableStatus) == 0 {
		retry = DefaultRetryConfig()
	}

	tracer := otel.Tracer("go-assistant/openrouter")
	meter := otel.Meter("go-assistant/openrouter")

	requestCounter, _ := meter.Int64Counter("llm.requests.total",
		metric.WithDescription("Total number of LLM requests"))
	durationHist, _ := meter.Float64Histogram("llm.request.duration.ms",
		metric.WithDescription("LLM request duration in milliseconds"))
	tokenCounter, _ := meter.Int64Counter("llm.tokens.total",
		metric.WithDescription("Total tokens used in LLM requests"))
	errorCounter, _ := meter.Int64Counter("llm.errors.total",
		metric.WithDescription("Total number of LLM request errors"))

	return &Client{
		apiKey:  cfg.APIKey,
		baseURL: baseURL,
		model:   cfg.Model,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		streamHTTPClient: &http.Client{},
		referer:          cfg.Referer,
		title:            title,
		timeout:          timeout,
		retry:            retry,
		tracer:           tracer,
		requestCounter:   requestCounter,
		durationHist:     durationHist,
		tokenCounter:     tokenCounter,
		errorCounter:     errorCounter,
	}, nil
}

// Complete sends a non-streaming chat completion request.
func (c *Client) Complete(ctx context.Context, req *output.ChatRequest) (*output.ChatResponse, error) {
	return WithRetry(ctx, c.retry, func(ctx context.Context) (*output.ChatResponse, error) {
		return c.doComplete(ctx, req)
	})
}

func (c *Client) doComplete(ctx context.Context, req *output.ChatRequest) (*output.ChatResponse, error) {
	model := req.Model
	if model == "" {
		model = c.model
	}

	ctx, span := c.tracer.Start(ctx, "openrouter.Complete",
		trace.WithAttributes(
			attribute.String("llm.model", model),
			attribute.String("llm.request_type", "complete"),
		))
	defer span.End()

	start := time.Now()
	c.requestCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("llm.model", model),
		attribute.String("llm.request_type", "complete"),
	))

	payload := c.buildPayload(req, model, false)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		c.recordError(ctx, model, "complete")
		span.RecordError(err)
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		c.recordError(ctx, model, "complete")
		return nil, c.handleErrorResponse(resp)
	}

	var result chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("empty choices in response")
	}

	elapsed := time.Since(start)
	c.durationHist.Record(ctx, float64(elapsed.Milliseconds()), metric.WithAttributes(
		attribute.String("llm.model", model),
		attribute.String("llm.request_type", "complete"),
	))
	c.tokenCounter.Add(ctx, int64(result.Usage.TotalTokens), metric.WithAttributes(
		attribute.String("llm.model", model),
	))

	span.SetAttributes(
		attribute.Int("llm.prompt_tokens", result.Usage.PromptTokens),
		attribute.Int("llm.completion_tokens", result.Usage.CompletionTokens),
		attribute.Int("llm.total_tokens", result.Usage.TotalTokens),
	)

	slog.InfoContext(ctx, "LLM request completed",
		"model", model,
		"prompt_tokens", result.Usage.PromptTokens,
		"completion_tokens", result.Usage.CompletionTokens,
		"duration_ms", elapsed.Milliseconds(),
	)

	chatResp := &output.ChatResponse{
		Content:          result.Choices[0].Message.Content,
		Model:            result.Model,
		PromptTokens:     result.Usage.PromptTokens,
		CompletionTokens: result.Usage.CompletionTokens,
		TotalTokens:      result.Usage.TotalTokens,
		FinishReason:     result.Choices[0].FinishReason,
	}

	if len(result.Choices[0].Message.ToolCalls) > 0 {
		chatResp.ToolCalls = make([]valueobject.ToolCall, len(result.Choices[0].Message.ToolCalls))
		for i, tc := range result.Choices[0].Message.ToolCalls {
			chatResp.ToolCalls[i] = valueobject.ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: valueobject.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			}
		}
	}

	return chatResp, nil
}

// CompleteStream sends a streaming chat completion request with retry on transient errors.
func (c *Client) CompleteStream(ctx context.Context, req *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
	type streamResult struct {
		ch <-chan valueobject.StreamChunk
	}

	result, err := WithRetry(ctx, c.retry, func(ctx context.Context) (streamResult, error) {
		ch, err := c.doCompleteStream(ctx, req)
		if err != nil {
			return streamResult{}, err
		}
		return streamResult{ch: ch}, nil
	})
	if err != nil {
		return nil, err
	}
	return result.ch, nil
}

func (c *Client) doCompleteStream(ctx context.Context, req *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
	model := req.Model
	if model == "" {
		model = c.model
	}

	ctx, span := c.tracer.Start(ctx, "openrouter.CompleteStream",
		trace.WithAttributes(
			attribute.String("llm.model", model),
			attribute.String("llm.request_type", "stream"),
		))

	c.requestCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("llm.model", model),
		attribute.String("llm.request_type", "stream"),
	))

	slog.InfoContext(ctx, "LLM stream request started", "model", model)

	payload := c.buildPayload(req, model, true)
	body, err := json.Marshal(payload)
	if err != nil {
		span.End()
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		span.End()
		return nil, fmt.Errorf("creating request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.streamHTTPClient.Do(httpReq)
	if err != nil {
		c.recordError(ctx, model, "stream")
		span.RecordError(err)
		span.End()
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		c.recordError(ctx, model, "stream")
		span.End()
		return nil, c.handleErrorResponse(resp)
	}

	ch := parseSSEStream(ctx, resp.Body)

	go func() {
		<-ctx.Done()
		span.End()
	}()

	return ch, nil
}

func (c *Client) buildPayload(req *output.ChatRequest, model string, stream bool) chatCompletionRequest {
	msgs := make([]messagePayload, len(req.Messages))
	for i, m := range req.Messages {
		mp := messagePayload{
			Role:    m.Role.String(),
			Content: m.Content,
		}
		if len(m.ToolCalls) > 0 {
			mp.ToolCalls = make([]toolCallPayload, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				mp.ToolCalls[j] = toolCallPayload{
					ID:   tc.ID,
					Type: tc.Type,
					Function: functionCallPayload{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}
		}
		if m.ToolCallID != "" {
			mp.ToolCallID = m.ToolCallID
		}
		msgs[i] = mp
	}

	payload := chatCompletionRequest{
		Model:       model,
		Messages:    msgs,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Stream:      stream,
	}

	if len(req.Tools) > 0 {
		payload.Tools = make([]toolPayload, len(req.Tools))
		for i, t := range req.Tools {
			payload.Tools[i] = toolPayload{
				Type: t.Type,
				Function: functionPayload{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					Parameters:  t.Function.Parameters,
				},
			}
		}
	}

	if req.ToolChoice != "" {
		payload.ToolChoice = string(req.ToolChoice)
	}

	return payload
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if c.referer != "" {
		req.Header.Set("HTTP-Referer", c.referer)
	}
	if c.title != "" {
		req.Header.Set("X-OpenRouter-Title", c.title)
	}
}

func (c *Client) handleErrorResponse(resp *http.Response) error {
	respBody, _ := io.ReadAll(resp.Body)

	var apiErr apiError
	if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Error.Message != "" {
		return &retryableError{
			statusCode: resp.StatusCode,
			err:        fmt.Errorf("openrouter API error (status %d): %s", resp.StatusCode, apiErr.Error.Message),
		}
	}

	return &retryableError{
		statusCode: resp.StatusCode,
		err:        fmt.Errorf("openrouter API error (status %d): %s", resp.StatusCode, string(respBody)),
	}
}

func (c *Client) recordError(ctx context.Context, model, requestType string) {
	c.errorCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("llm.model", model),
		attribute.String("llm.request_type", requestType),
	))
}
