package cpn

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// sseReader creates a mock io.ReadCloser from SSE text.
func sseReader(sse string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(sse))
}

// ── ParseChatSSE Tests ──────────────────────────────────────────────────────

func TestParseChatSSE_MultipleDeltas(t *testing.T) {
	sse := `data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":" "}}]}

data: {"choices":[{"delta":{"content":"world"}}]}

data: [DONE]
`
	h := NewStreamHandler()
	resp, err := h.ParseChatSSE(sseReader(sse))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hello world" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello world")
	}
}

func TestParseChatSSE_ToolCalls(t *testing.T) {
	sse := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"get_weather","arguments":"{\"loc"}}]}}]}

data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ation\":\"NYC\"}"}}]}}]}

data: {"choices":[{"delta":{},"finish_reason":"tool_calls"}]}

data: [DONE]
`
	h := NewStreamHandler()
	resp, err := h.ParseChatSSE(sseReader(sse))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_1" {
		t.Errorf("ToolCall ID = %q, want %q", tc.ID, "call_1")
	}
	if tc.ToolName != "get_weather" {
		t.Errorf("ToolCall Name = %q, want %q", tc.ToolName, "get_weather")
	}
	wantArgs := `{"location":"NYC"}`
	if string(tc.Arguments) != wantArgs {
		t.Errorf("ToolCall Args = %q, want %q", string(tc.Arguments), wantArgs)
	}
	if resp.StopReason != "tool_calls" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "tool_calls")
	}
}

func TestParseChatSSE_Usage(t *testing.T) {
	sse := `data: {"choices":[{"delta":{"content":"Hi"}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"cost":0.001}}

data: [DONE]
`
	h := NewStreamHandler()
	resp, err := h.ParseChatSSE(sseReader(sse))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", resp.InputTokens)
	}
	if resp.OutputTokens != 2 {
		t.Errorf("OutputTokens = %d, want 2", resp.OutputTokens)
	}
	if resp.CostUSD != 0.001 {
		t.Errorf("CostUSD = %f, want 0.001", resp.CostUSD)
	}
}

func TestParseChatSSE_DoneTerminates(t *testing.T) {
	sse := `data: {"choices":[{"delta":{"content":"before"}}]}

data: [DONE]

data: {"choices":[{"delta":{"content":"after"}}]}
`
	h := NewStreamHandler()
	resp, err := h.ParseChatSSE(sseReader(sse))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "before" {
		t.Errorf("Content = %q, want %q (data after [DONE] should be ignored)", resp.Content, "before")
	}
}

func TestParseChatSSE_MalformedJSON(t *testing.T) {
	sse := `data: {"choices":[{"delta":{"content":"ok"}}]}

data: {INVALID JSON}

data: {"choices":[{"delta":{"content":" fine"}}]}

data: [DONE]
`
	h := NewStreamHandler()
	resp, err := h.ParseChatSSE(sseReader(sse))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok fine" {
		t.Errorf("Content = %q, want %q", resp.Content, "ok fine")
	}
}

func TestParseChatSSE_EmptyStream(t *testing.T) {
	sse := `data: [DONE]
`
	h := NewStreamHandler()
	resp, err := h.ParseChatSSE(sseReader(sse))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "" {
		t.Errorf("Content = %q, want empty", resp.Content)
	}
	if resp.Model != "" {
		t.Errorf("Model = %q, want empty", resp.Model)
	}
}

func TestParseChatSSE_ModelAndStopReason(t *testing.T) {
	sse := `data: {"model":"anthropic/claude-sonnet-4-6","choices":[{"delta":{"content":"test"}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]
`
	h := NewStreamHandler()
	resp, err := h.ParseChatSSE(sseReader(sse))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Model != "anthropic/claude-sonnet-4-6" {
		t.Errorf("Model = %q, want %q", resp.Model, "anthropic/claude-sonnet-4-6")
	}
	if resp.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "stop")
	}
}

func TestParseChatSSE_NonDataLinesSkipped(t *testing.T) {
	sse := `: this is a comment
event: message

data: {"choices":[{"delta":{"content":"works"}}]}

data: [DONE]
`
	h := NewStreamHandler()
	resp, err := h.ParseChatSSE(sseReader(sse))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "works" {
		t.Errorf("Content = %q, want %q", resp.Content, "works")
	}
}

func TestParseChatSSE_SingleChunk(t *testing.T) {
	sse := `data: {"model":"gpt-4","choices":[{"delta":{"content":"single"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"cost":0.0005}}

data: [DONE]
`
	h := NewStreamHandler()
	resp, err := h.ParseChatSSE(sseReader(sse))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "single" {
		t.Errorf("Content = %q, want %q", resp.Content, "single")
	}
	if resp.Model != "gpt-4" {
		t.Errorf("Model = %q, want %q", resp.Model, "gpt-4")
	}
	if resp.StopReason != "stop" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "stop")
	}
	if resp.InputTokens != 5 {
		t.Errorf("InputTokens = %d, want 5", resp.InputTokens)
	}
	if resp.OutputTokens != 1 {
		t.Errorf("OutputTokens = %d, want 1", resp.OutputTokens)
	}
}

// ── ParseAnthropicSSE Tests ─────────────────────────────────────────────────

func TestParseAnthropicSSE_TextContent(t *testing.T) {
	sse := `event: message_start
data: {"type":"message_start","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":25}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"The answer "}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"is 42."}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":8}}

event: message_stop
data: {"type":"message_stop"}
`
	h := NewStreamHandler()
	resp, err := h.ParseAnthropicSSE(sseReader(sse))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "The answer is 42." {
		t.Errorf("Content = %q, want %q", resp.Content, "The answer is 42.")
	}
	if resp.Model != "claude-sonnet-4-6" {
		t.Errorf("Model = %q, want %q", resp.Model, "claude-sonnet-4-6")
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "end_turn")
	}
	if resp.InputTokens != 25 {
		t.Errorf("InputTokens = %d, want 25", resp.InputTokens)
	}
	if resp.OutputTokens != 8 {
		t.Errorf("OutputTokens = %d, want 8", resp.OutputTokens)
	}
}

func TestParseAnthropicSSE_ThinkingAccumulation(t *testing.T) {
	sse := `event: message_start
data: {"type":"message_start","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":10}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me "}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"analyze..."}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"ErUBx123"}}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"The answer."}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":15}}

event: message_stop
data: {"type":"message_stop"}
`
	h := NewStreamHandler()
	resp, err := h.ParseAnthropicSSE(sseReader(sse))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "The answer." {
		t.Errorf("Content = %q, want %q", resp.Content, "The answer.")
	}
	if len(resp.ThinkingBlocks) != 1 {
		t.Fatalf("ThinkingBlocks len = %d, want 1", len(resp.ThinkingBlocks))
	}
	tb := resp.ThinkingBlocks[0]
	if tb.Thinking != "Let me analyze..." {
		t.Errorf("Thinking = %q, want %q", tb.Thinking, "Let me analyze...")
	}
	if tb.Signature != "ErUBx123" {
		t.Errorf("Signature = %q, want %q", tb.Signature, "ErUBx123")
	}
}

func TestParseAnthropicSSE_MultipleThinkingBlocks(t *testing.T) {
	sse := `event: message_start
data: {"type":"message_start","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":5}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"First thought"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_AAA"}}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"thinking"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"thinking_delta","thinking":"Second thought"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"signature_delta","signature":"sig_BBB"}}

event: content_block_start
data: {"type":"content_block_start","index":2,"content_block":{"type":"text"}}

event: content_block_delta
data: {"type":"content_block_delta","index":2,"delta":{"type":"text_delta","text":"Final answer."}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":20}}

event: message_stop
data: {"type":"message_stop"}
`
	h := NewStreamHandler()
	resp, err := h.ParseAnthropicSSE(sseReader(sse))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ThinkingBlocks) != 2 {
		t.Fatalf("ThinkingBlocks len = %d, want 2", len(resp.ThinkingBlocks))
	}
	if resp.ThinkingBlocks[0].Thinking != "First thought" {
		t.Errorf("ThinkingBlocks[0].Thinking = %q, want %q", resp.ThinkingBlocks[0].Thinking, "First thought")
	}
	if resp.ThinkingBlocks[0].Signature != "sig_AAA" {
		t.Errorf("ThinkingBlocks[0].Signature = %q, want %q", resp.ThinkingBlocks[0].Signature, "sig_AAA")
	}
	if resp.ThinkingBlocks[1].Thinking != "Second thought" {
		t.Errorf("ThinkingBlocks[1].Thinking = %q, want %q", resp.ThinkingBlocks[1].Thinking, "Second thought")
	}
	if resp.ThinkingBlocks[1].Signature != "sig_BBB" {
		t.Errorf("ThinkingBlocks[1].Signature = %q, want %q", resp.ThinkingBlocks[1].Signature, "sig_BBB")
	}
	if resp.Content != "Final answer." {
		t.Errorf("Content = %q, want %q", resp.Content, "Final answer.")
	}
}

func TestParseAnthropicSSE_SignaturePreserved(t *testing.T) {
	// Signature with special characters that must be preserved byte-for-byte.
	const knownSig = "ErUBCkYIAxJCCiBkaXNjb3Vyc2UtMjAyNTAyMjItbW9kZWwtcHJlZhIeC/v8XPc+42=="

	sse := `event: message_start
data: {"type":"message_start","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"deep thought"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"ErUBCkYIAxJCCiBkaXNjb3Vyc2UtMjAyNTAyMjItbW9kZWwtcHJlZhIeC/v8XPc+42=="}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}
`
	h := NewStreamHandler()
	resp, err := h.ParseAnthropicSSE(sseReader(sse))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ThinkingBlocks) != 1 {
		t.Fatalf("ThinkingBlocks len = %d, want 1", len(resp.ThinkingBlocks))
	}
	if resp.ThinkingBlocks[0].Signature != knownSig {
		t.Errorf("Signature not preserved byte-for-byte:\ngot:  %q\nwant: %q", resp.ThinkingBlocks[0].Signature, knownSig)
	}
}

func TestParseAnthropicSSE_CacheTokens(t *testing.T) {
	sse := `event: message_start
data: {"type":"message_start","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":50,"cache_read_input_tokens":100,"cache_creation_input_tokens":200}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"cached"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}

event: message_stop
data: {"type":"message_stop"}
`
	h := NewStreamHandler()
	resp, err := h.ParseAnthropicSSE(sseReader(sse))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.CacheReadTokens != 100 {
		t.Errorf("CacheReadTokens = %d, want 100", resp.CacheReadTokens)
	}
	if resp.CacheCreationTokens != 200 {
		t.Errorf("CacheCreationTokens = %d, want 200", resp.CacheCreationTokens)
	}
	if resp.InputTokens != 50 {
		t.Errorf("InputTokens = %d, want 50", resp.InputTokens)
	}
}

func TestParseAnthropicSSE_ErrorEvent(t *testing.T) {
	sse := `event: message_start
data: {"type":"message_start","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":5}}}

event: error
data: {"type":"error","error":{"message":"overloaded"}}
`
	h := NewStreamHandler()
	_, err := h.ParseAnthropicSSE(sseReader(sse))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Errorf("error = %v, want ErrProviderUnavailable", err)
	}
	if !strings.Contains(err.Error(), "overloaded") {
		t.Errorf("error message should contain 'overloaded': %v", err)
	}
}

func TestParseAnthropicSSE_MessageStop(t *testing.T) {
	sse := `event: message_start
data: {"type":"message_start","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"before stop"}}

event: message_stop
data: {"type":"message_stop"}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" after stop"}}
`
	h := NewStreamHandler()
	resp, err := h.ParseAnthropicSSE(sseReader(sse))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "before stop" {
		t.Errorf("Content = %q, want %q (data after message_stop should be ignored)", resp.Content, "before stop")
	}
}

func TestParseAnthropicSSE_MalformedJSON(t *testing.T) {
	sse := `event: message_start
data: {"type":"message_start","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":1}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}

event: content_block_delta
data: {BROKEN JSON}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"recovered"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}

event: message_stop
data: {"type":"message_stop"}
`
	h := NewStreamHandler()
	resp, err := h.ParseAnthropicSSE(sseReader(sse))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "recovered" {
		t.Errorf("Content = %q, want %q", resp.Content, "recovered")
	}
}

func TestParseAnthropicSSE_OutputTokens(t *testing.T) {
	sse := `event: message_start
data: {"type":"message_start","message":{"model":"claude-sonnet-4-6","usage":{"input_tokens":10}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":42}}

event: message_stop
data: {"type":"message_stop"}
`
	h := NewStreamHandler()
	resp, err := h.ParseAnthropicSSE(sseReader(sse))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OutputTokens != 42 {
		t.Errorf("OutputTokens = %d, want 42", resp.OutputTokens)
	}
	if resp.Model != "claude-sonnet-4-6" {
		t.Errorf("Model = %q, want %q", resp.Model, "claude-sonnet-4-6")
	}
}
