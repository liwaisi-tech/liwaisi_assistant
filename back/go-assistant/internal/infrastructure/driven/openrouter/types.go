// Package openrouter implements the OpenRouter LLM client adapter.
package openrouter

import "encoding/json"

// chatCompletionRequest is the OpenRouter API request body.
type chatCompletionRequest struct {
	Model       string           `json:"model"`
	Messages    []messagePayload `json:"messages"`
	Temperature float64          `json:"temperature"`
	MaxTokens   int              `json:"max_tokens"`
	Stream      bool             `json:"stream"`
	Tools       []toolPayload    `json:"tools,omitempty"`
	ToolChoice  interface{}      `json:"tool_choice,omitempty"`
}

type messagePayload struct {
	Role       string            `json:"role"`
	Content    string            `json:"content,omitempty"`
	ToolCalls  []toolCallPayload `json:"tool_calls,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
}

type toolPayload struct {
	Type     string          `json:"type"`
	Function functionPayload `json:"function"`
}

type functionPayload struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type toolCallPayload struct {
	ID       string              `json:"id"`
	Type     string              `json:"type"`
	Function functionCallPayload `json:"function"`
}

type functionCallPayload struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// chatCompletionResponse is the non-streaming response.
type chatCompletionResponse struct {
	ID      string   `json:"id"`
	Model   string   `json:"model"`
	Choices []choice `json:"choices"`
	Usage   usage    `json:"usage"`
}

type choice struct {
	Index        int            `json:"index"`
	Message      messagePayload `json:"message"`
	FinishReason string         `json:"finish_reason"`
}

type usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// chatStreamChunk is a single SSE chunk from a streaming response.
type chatStreamChunk struct {
	ID      string        `json:"id"`
	Model   string        `json:"model"`
	Choices []streamDelta `json:"choices"`
}

type streamDelta struct {
	Index        int    `json:"index"`
	Delta        delta  `json:"delta"`
	FinishReason string `json:"finish_reason"`
}

type delta struct {
	Role      string                   `json:"role,omitempty"`
	Content   string                   `json:"content,omitempty"`
	ToolCalls []streamingToolCallDelta `json:"tool_calls,omitempty"`
}

type streamingToolCallDelta struct {
	Index    int                       `json:"index"`
	ID       string                    `json:"id,omitempty"`
	Type     string                    `json:"type,omitempty"`
	Function *functionCallDeltaPayload `json:"function,omitempty"`
}

type functionCallDeltaPayload struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// apiError represents an OpenRouter API error response.
type apiError struct {
	Error struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}
