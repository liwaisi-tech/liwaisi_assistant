package openrouter

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

// StreamHandler parses SSE streams from both OpenRouter endpoint formats.
type StreamHandler struct{}

// NewStreamHandler creates a StreamHandler.
func NewStreamHandler() *StreamHandler { return &StreamHandler{} }

// toolCallAccumulator assembles a tool call from incremental SSE deltas.
type toolCallAccumulator struct {
	id   string
	name string
	args strings.Builder
}

// blockAccumulator tracks an Anthropic content block during SSE parsing.
type blockAccumulator struct {
	blockType string // "text" or "thinking"
	content   strings.Builder
	signature string
}

// ParseChatSSE parses an OpenAI-compatible /chat/completions SSE stream.
//
// Format:
//
//	data: {"choices":[{"delta":{"content":"..."}}]}
//	data: [DONE]
//
// Assembles content from multiple delta chunks, accumulates tool calls
// by index, and extracts usage from the final chunk.
// Takes ownership of body (calls body.Close via defer).
func (h *StreamHandler) ParseChatSSE(body io.ReadCloser) (cpn.LLMResponse, error) {
	return h.parseChatSSEInternal(body, nil)
}

// ParseChatSSEWithCallback parses an OpenAI-compatible /chat/completions SSE stream,
// invoking onDelta for each non-empty content delta as it arrives.
// Takes ownership of body (calls body.Close via defer).
func (h *StreamHandler) ParseChatSSEWithCallback(body io.ReadCloser, onDelta func(string)) (cpn.LLMResponse, error) {
	return h.parseChatSSEInternal(body, onDelta)
}

// parseChatSSEInternal is the shared implementation for ParseChatSSE and ParseChatSSEWithCallback.
func (h *StreamHandler) parseChatSSEInternal(body io.ReadCloser, onDelta func(string)) (cpn.LLMResponse, error) {
	defer body.Close()

	var resp cpn.LLMResponse
	var content strings.Builder
	toolCalls := make(map[int]*toolCallAccumulator)

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()

		data, found := strings.CutPrefix(line, "data: ")
		if !found {
			continue
		}

		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int     `json:"prompt_tokens"`
				CompletionTokens int     `json:"completion_tokens"`
				Cost             float64 `json:"cost"`
			} `json:"usage"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue // REQ-008: malformed JSON skipped
		}

		if chunk.Model != "" {
			resp.Model = chunk.Model
		}

		if len(chunk.Choices) > 0 {
			choice := chunk.Choices[0]
			content.WriteString(choice.Delta.Content)
			if onDelta != nil && choice.Delta.Content != "" {
				onDelta(choice.Delta.Content)
			}

			for _, tc := range choice.Delta.ToolCalls {
				acc, ok := toolCalls[tc.Index]
				if !ok {
					acc = &toolCallAccumulator{}
					toolCalls[tc.Index] = acc
				}
				if tc.ID != "" {
					acc.id = tc.ID
				}
				if tc.Function.Name != "" {
					acc.name = tc.Function.Name
				}
				acc.args.WriteString(tc.Function.Arguments)
			}

			if choice.FinishReason != "" {
				resp.StopReason = choice.FinishReason
			}
		}

		if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 || chunk.Usage.Cost > 0 {
			resp.InputTokens = chunk.Usage.PromptTokens
			resp.OutputTokens = chunk.Usage.CompletionTokens
			resp.CostUSD = chunk.Usage.Cost
		}
	}

	if err := scanner.Err(); err != nil {
		return cpn.LLMResponse{}, err
	}

	resp.Content = content.String()

	// Convert accumulated tool calls to LLMToolCall slice, sorted by index.
	if len(toolCalls) > 0 {
		indices := make([]int, 0, len(toolCalls))
		for idx := range toolCalls {
			indices = append(indices, idx)
		}
		sort.Ints(indices)
		resp.ToolCalls = make([]*cpn.LLMToolCall, 0, len(indices))
		for _, idx := range indices {
			acc := toolCalls[idx]
			resp.ToolCalls = append(resp.ToolCalls, &cpn.LLMToolCall{
				ID:        acc.id,
				ToolName:  acc.name,
				Arguments: json.RawMessage(acc.args.String()),
			})
		}
	}

	return resp, nil
}

// ParseAnthropicSSE parses an Anthropic /messages SSE stream.
//
// Event types:
//
//	message_start       — model, input_tokens, cache tokens
//	content_block_start — block type (text | thinking)
//	content_block_delta — text_delta, thinking_delta, signature_delta
//	message_delta       — stop_reason, output_tokens
//	message_stop        — terminates loop
//	error               — returns ErrProviderUnavailable
//
// Thinking blocks accumulate via thinking_delta and are sealed by
// signature_delta. The Signature field is preserved byte-for-byte.
// Takes ownership of body (calls body.Close via defer).
func (h *StreamHandler) ParseAnthropicSSE(body io.ReadCloser) (cpn.LLMResponse, error) {
	return h.parseAnthropicSSEInternal(body, nil)
}

// ParseAnthropicSSEWithCallback parses an Anthropic /messages SSE stream,
// invoking onDelta for each non-empty text delta as it arrives.
// Takes ownership of body (calls body.Close via defer).
func (h *StreamHandler) ParseAnthropicSSEWithCallback(body io.ReadCloser, onDelta func(string)) (cpn.LLMResponse, error) {
	return h.parseAnthropicSSEInternal(body, onDelta)
}

// parseAnthropicSSEInternal is the shared implementation for ParseAnthropicSSE and ParseAnthropicSSEWithCallback.
func (h *StreamHandler) parseAnthropicSSEInternal(body io.ReadCloser, onDelta func(string)) (cpn.LLMResponse, error) {
	defer body.Close()

	var resp cpn.LLMResponse
	var content strings.Builder
	blocks := make(map[int]*blockAccumulator)
	var currentEventType string

	scanner := bufio.NewScanner(body)
scanLoop:
	for scanner.Scan() {
		line := scanner.Text()

		// Event type lines set state for the next data line.
		if evt, found := strings.CutPrefix(line, "event: "); found {
			currentEventType = evt
			continue
		}

		data, found := strings.CutPrefix(line, "data: ")
		if !found {
			continue
		}

		switch currentEventType {
		case "message_start":
			var msg struct {
				Message struct {
					Model string `json:"model"`
					Usage struct {
						InputTokens              int `json:"input_tokens"`
						CacheReadInputTokens     int `json:"cache_read_input_tokens"`
						CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
					} `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(data), &msg); err != nil {
				currentEventType = ""
				continue
			}
			resp.Model = msg.Message.Model
			resp.InputTokens = msg.Message.Usage.InputTokens
			resp.CacheReadTokens = msg.Message.Usage.CacheReadInputTokens
			resp.CacheCreationTokens = msg.Message.Usage.CacheCreationInputTokens

		case "content_block_start":
			var cbs struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type string `json:"type"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(data), &cbs); err != nil {
				currentEventType = ""
				continue
			}
			blocks[cbs.Index] = &blockAccumulator{blockType: cbs.ContentBlock.Type}

		// Note: Anthropic tool_use blocks (input_json_delta) are not handled here.
		// They will be added when executor integration requires them.
		case "content_block_delta":
			var cbd struct {
				Index int `json:"index"`
				Delta struct {
					Type      string `json:"type"`
					Text      string `json:"text"`
					Thinking  string `json:"thinking"`
					Signature string `json:"signature"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &cbd); err != nil {
				currentEventType = ""
				continue
			}
			acc, ok := blocks[cbd.Index]
			if !ok {
				currentEventType = ""
				continue
			}
			switch cbd.Delta.Type {
			case "text_delta":
				content.WriteString(cbd.Delta.Text)
				if onDelta != nil && cbd.Delta.Text != "" {
					onDelta(cbd.Delta.Text)
				}
			case "thinking_delta":
				acc.content.WriteString(cbd.Delta.Thinking)
			case "signature_delta":
				acc.signature = cbd.Delta.Signature
				// Seal the thinking block.
				resp.ThinkingBlocks = append(resp.ThinkingBlocks, cpn.ThinkingBlock{
					Thinking:  acc.content.String(),
					Signature: acc.signature,
				})
			}

		case "message_delta":
			var md struct {
				Delta struct {
					StopReason string `json:"stop_reason"`
				} `json:"delta"`
				Usage struct {
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal([]byte(data), &md); err != nil {
				currentEventType = ""
				continue
			}
			resp.StopReason = md.Delta.StopReason
			resp.OutputTokens = md.Usage.OutputTokens

		case "message_stop":
			// Terminate parsing loop.
			break scanLoop

		case "error":
			var errEvt struct {
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(data), &errEvt); err != nil {
				return cpn.LLMResponse{}, fmt.Errorf("%w: %s", cpn.ErrProviderUnavailable, data)
			}
			return cpn.LLMResponse{}, fmt.Errorf("%w: %s", cpn.ErrProviderUnavailable, errEvt.Error.Message)
		}

		currentEventType = ""
	}

	if err := scanner.Err(); err != nil {
		return cpn.LLMResponse{}, err
	}

	resp.Content = content.String()
	return resp, nil
}
