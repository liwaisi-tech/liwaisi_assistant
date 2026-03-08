package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

type mockLLMClient struct {
	completeFunc       func(ctx context.Context, req *output.ChatRequest) (*output.ChatResponse, error)
	completeStreamFunc func(ctx context.Context, req *output.ChatRequest) (<-chan valueobject.StreamChunk, error)
}

func (m *mockLLMClient) Complete(ctx context.Context, req *output.ChatRequest) (*output.ChatResponse, error) {
	if m.completeFunc != nil {
		return m.completeFunc(ctx, req)
	}
	return &output.ChatResponse{Content: "default"}, nil
}

func (m *mockLLMClient) CompleteStream(ctx context.Context, req *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
	if m.completeStreamFunc != nil {
		return m.completeStreamFunc(ctx, req)
	}
	ch := make(chan valueobject.StreamChunk)
	close(ch)
	return ch, nil
}

func defaultAgentConfig() AgentConfig {
	return AgentConfig{
		Model:        "test-model",
		SystemPrompt: "You are a test bot.",
		Temperature:  0.7,
		MaxTokens:    100,
		HistoryLimit: 50,
	}
}

func emptyRegistry() *tool.Registry {
	return tool.NewRegistry()
}

// drainChunks reads all chunks from ch, returning content tokens,
// tool events, and any error encountered.
func drainChunks(ch <-chan valueobject.StreamChunk) (content string, events []valueobject.ToolEvent, err error) {
	for chunk := range ch {
		if chunk.Err != nil {
			return content, events, chunk.Err
		}
		if chunk.ToolEvent != nil {
			events = append(events, *chunk.ToolEvent)
			continue
		}
		if chunk.Done {
			break
		}
		content += chunk.Content
	}
	return content, events, nil
}

func TestAgentService_Ask(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		mockResp *output.ChatResponse
		mockErr  error
		want     string
		wantErr  bool
	}{
		{
			name:     "happy path",
			query:    "what is Go?",
			mockResp: &output.ChatResponse{Content: "Go is a programming language."},
			want:     "Go is a programming language.",
		},
		{
			name:    "LLM error propagates",
			query:   "fail please",
			mockErr: errors.New("API down"),
			wantErr: true,
		},
		{
			name:     "empty query still works",
			query:    "",
			mockResp: &output.ChatResponse{Content: "I need a question."},
			want:     "I need a question.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockLLMClient{
				completeFunc: func(_ context.Context, req *output.ChatRequest) (*output.ChatResponse, error) {
					if len(req.Messages) != 2 {
						t.Errorf("Ask should send 2 messages (system+user), got %d", len(req.Messages))
					}
					return tt.mockResp, tt.mockErr
				},
			}

			svc := NewAgentService(mock, defaultAgentConfig(), emptyRegistry(), nil)
			got, err := svc.Ask(context.Background(), tt.query)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Ask() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("Ask() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAgentService_Chat_HappyPath(t *testing.T) {
	mock := &mockLLMClient{
		completeStreamFunc: func(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
			ch := make(chan valueobject.StreamChunk, 3)
			ch <- valueobject.StreamChunk{Content: "Hello "}
			ch <- valueobject.StreamChunk{Content: "World"}
			ch <- valueobject.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), emptyRegistry(), nil)
	ch, err := svc.Chat(context.Background(), "sess1", "Hi")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	content, _, chunkErr := drainChunks(ch)
	if chunkErr != nil {
		t.Fatalf("unexpected error chunk: %v", chunkErr)
	}
	if content != "Hello World" {
		t.Errorf("content = %q, want %q", content, "Hello World")
	}
}

func TestAgentService_Chat_LLMStreamError(t *testing.T) {
	mock := &mockLLMClient{
		completeStreamFunc: func(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
			return nil, errors.New("connection refused")
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), emptyRegistry(), nil)
	ch, err := svc.Chat(context.Background(), "sess1", "Hi")
	if err != nil {
		t.Fatalf("Chat() should not return error directly, got: %v", err)
	}

	_, _, chunkErr := drainChunks(ch)
	if chunkErr == nil {
		t.Fatal("expected error chunk from stream failure")
	}
}

func TestAgentService_Chat_MidStreamError(t *testing.T) {
	mock := &mockLLMClient{
		completeStreamFunc: func(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
			ch := make(chan valueobject.StreamChunk, 2)
			ch <- valueobject.StreamChunk{Content: "partial"}
			ch <- valueobject.StreamChunk{Err: errors.New("mid-stream failure")}
			close(ch)
			return ch, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), emptyRegistry(), nil)
	ch, err := svc.Chat(context.Background(), "sess1", "Hi")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	_, _, chunkErr := drainChunks(ch)
	if chunkErr == nil {
		t.Error("expected error chunk from mid-stream failure")
	}
}

func TestAgentService_Chat_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	blockCh := make(chan valueobject.StreamChunk)
	mock := &mockLLMClient{
		completeStreamFunc: func(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
			return blockCh, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), emptyRegistry(), nil)
	ch, err := svc.Chat(ctx, "sess1", "Hi")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	cancel()

	chunk := <-ch
	if chunk.Err == nil {
		t.Error("expected context cancellation error")
	}
}

func TestAgentService_Chat_MaintainsHistory(t *testing.T) {
	var callCount int
	mock := &mockLLMClient{
		completeStreamFunc: func(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
			callCount++
			ch := make(chan valueobject.StreamChunk, 2)
			ch <- valueobject.StreamChunk{Content: "response"}
			ch <- valueobject.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), emptyRegistry(), nil)

	for i := 0; i < 3; i++ {
		ch, err := svc.Chat(context.Background(), "sess1", "msg")
		if err != nil {
			t.Fatalf("Chat() round %d error = %v", i, err)
		}
		for range ch {
		}
	}

	if callCount != 3 {
		t.Errorf("LLM was called %d times, want 3", callCount)
	}
}

func TestAgentService_Chat_ChannelClosedByLLM(t *testing.T) {
	mock := &mockLLMClient{
		completeStreamFunc: func(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
			ch := make(chan valueobject.StreamChunk, 1)
			ch <- valueobject.StreamChunk{Content: "only chunk"}
			close(ch)
			return ch, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), emptyRegistry(), nil)
	ch, err := svc.Chat(context.Background(), "sess1", "Hi")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	chunks := make([]valueobject.StreamChunk, 0, 2)
	for c := range ch {
		chunks = append(chunks, c)
	}

	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2 (content + done)", len(chunks))
	}
	if chunks[0].Content != "only chunk" {
		t.Errorf("chunks[0].Content = %q, want %q", chunks[0].Content, "only chunk")
	}
	if !chunks[1].Done {
		t.Error("last chunk should have Done=true")
	}
}

// --- Tool calling tests ---

func testToolCall(id, name, args string) valueobject.ToolCall {
	return valueobject.ToolCall{
		ID:   id,
		Type: "function",
		Function: valueobject.FunctionCall{
			Name:      name,
			Arguments: args,
		},
	}
}

func registryWithEcho() *tool.Registry {
	r := tool.NewRegistry()
	r.Register(valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name:        "echo",
			Description: "Echoes args back",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}, func(_ context.Context, args json.RawMessage) (string, error) {
		return string(args), nil
	})
	return r
}

func TestAgentService_Ask_ToolCallLoop_OneIteration(t *testing.T) {
	var callIndex int
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
			callIndex++
			if callIndex == 1 {
				return &output.ChatResponse{
					FinishReason: "tool_calls",
					ToolCalls:    []valueobject.ToolCall{testToolCall("call_1", "echo", `{"msg":"hi"}`)},
				}, nil
			}
			return &output.ChatResponse{
				Content:      "echoed: hi",
				FinishReason: "stop",
			}, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), registryWithEcho(), nil)
	got, err := svc.Ask(context.Background(), "echo this")
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if got != "echoed: hi" {
		t.Errorf("Ask() = %q, want %q", got, "echoed: hi")
	}
	if callIndex != 2 {
		t.Errorf("LLM called %d times, want 2", callIndex)
	}
}

func TestAgentService_Ask_ToolCallLoop_MultipleIterations(t *testing.T) {
	var callIndex int
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
			callIndex++
			if callIndex <= 3 {
				return &output.ChatResponse{
					FinishReason: "tool_calls",
					ToolCalls:    []valueobject.ToolCall{testToolCall(fmt.Sprintf("call_%d", callIndex), "echo", `{}`)},
				}, nil
			}
			return &output.ChatResponse{
				Content:      "done after 3 tool calls",
				FinishReason: "stop",
			}, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), registryWithEcho(), nil)
	got, err := svc.Ask(context.Background(), "keep echoing")
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if got != "done after 3 tool calls" {
		t.Errorf("Ask() = %q, want %q", got, "done after 3 tool calls")
	}
	if callIndex != 4 {
		t.Errorf("LLM called %d times, want 4", callIndex)
	}
}

func TestAgentService_Ask_ToolCallLoop_ExceedsMaxIterations(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
			return &output.ChatResponse{
				FinishReason: "tool_calls",
				ToolCalls:    []valueobject.ToolCall{testToolCall("call_inf", "echo", `{}`)},
			}, nil
		},
	}

	cfg := defaultAgentConfig()
	cfg.MaxToolIterations = 3
	svc := NewAgentService(mock, cfg, registryWithEcho(), nil)
	_, err := svc.Ask(context.Background(), "loop forever")
	if err == nil {
		t.Fatal("Ask() should have returned error for max iterations exceeded")
	}
}

func TestAgentService_Ask_NoTools_SkipsLoop(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, req *output.ChatRequest) (*output.ChatResponse, error) {
			if len(req.Tools) != 0 {
				t.Error("expected no tools in request for empty registry")
			}
			return &output.ChatResponse{Content: "no tools", FinishReason: "stop"}, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), emptyRegistry(), nil)
	got, err := svc.Ask(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if got != "no tools" {
		t.Errorf("Ask() = %q, want %q", got, "no tools")
	}
}

func TestAgentService_Ask_ToolExecutionError_ReturnsJSON(t *testing.T) {
	r := tool.NewRegistry()
	r.Register(valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name:       "fail_tool",
			Parameters: json.RawMessage(`{"type":"object"}`),
		},
	}, func(_ context.Context, _ json.RawMessage) (string, error) {
		return "", errors.New("something broke")
	})

	var secondCallMessages []string
	var callIndex int
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, req *output.ChatRequest) (*output.ChatResponse, error) {
			callIndex++
			if callIndex == 1 {
				return &output.ChatResponse{
					FinishReason: "tool_calls",
					ToolCalls:    []valueobject.ToolCall{testToolCall("call_fail", "fail_tool", `{}`)},
				}, nil
			}
			for _, m := range req.Messages {
				if m.Role == valueobject.RoleTool {
					secondCallMessages = append(secondCallMessages, m.Content)
				}
			}
			return &output.ChatResponse{Content: "handled error", FinishReason: "stop"}, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), r, nil)
	got, err := svc.Ask(context.Background(), "break it")
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if got != "handled error" {
		t.Errorf("Ask() = %q, want %q", got, "handled error")
	}
	if len(secondCallMessages) != 1 {
		t.Fatalf("expected 1 tool result message, got %d", len(secondCallMessages))
	}
	if secondCallMessages[0] != `{"error":"something broke"}` {
		t.Errorf("tool result = %q, want error JSON", secondCallMessages[0])
	}
}

func TestAgentService_Chat_WithToolCalls(t *testing.T) {
	var completeCallCount int
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
			completeCallCount++
			if completeCallCount == 1 {
				return &output.ChatResponse{
					FinishReason: "tool_calls",
					ToolCalls:    []valueobject.ToolCall{testToolCall("call_chat", "echo", `{"x":1}`)},
				}, nil
			}
			return &output.ChatResponse{FinishReason: "stop"}, nil
		},
		completeStreamFunc: func(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
			ch := make(chan valueobject.StreamChunk, 2)
			ch <- valueobject.StreamChunk{Content: "final"}
			ch <- valueobject.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), registryWithEcho(), nil)
	ch, err := svc.Chat(context.Background(), "sess_tc", "trigger tool")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	content, events, chunkErr := drainChunks(ch)
	if chunkErr != nil {
		t.Fatalf("unexpected error: %v", chunkErr)
	}
	if content != "final" {
		t.Errorf("streamed content = %q, want %q", content, "final")
	}
	if completeCallCount != 2 {
		t.Errorf("Complete called %d times, want 2 (tool loop)", completeCallCount)
	}
	if len(events) == 0 {
		t.Error("expected tool events during tool loop, got none")
	}
}

func TestAgentService_Chat_ToolLoopError(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
			return nil, errors.New("LLM down during tool loop")
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), registryWithEcho(), nil)
	ch, err := svc.Chat(context.Background(), "sess_err", "trigger")
	if err != nil {
		t.Fatalf("Chat() should not return error directly, got: %v", err)
	}

	_, _, chunkErr := drainChunks(ch)
	if chunkErr == nil {
		t.Fatal("expected error chunk from tool loop failure")
	}
}

func TestAgentService_Ask_NilRegistry(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
			return &output.ChatResponse{Content: "ok", FinishReason: "stop"}, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), nil, nil)
	got, err := svc.Ask(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if got != "ok" {
		t.Errorf("Ask() = %q, want %q", got, "ok")
	}
}

func TestAsk_JITMode_HasTools(t *testing.T) {
	toolResult := `{"answer":"42"}`

	catalog := tool.NewCatalog()
	catalog.RegisterCategory(&tool.CatalogEntry{
		Info: valueobject.ToolCategoryInfo{
			Category:     valueobject.ToolCategoryFileManagement,
			Description:  "files",
			Tools:        []valueobject.ToolSummary{{Name: "read_file", Description: "Read"}},
			EstTokenCost: 100,
		},
		Factory: func(r *tool.Registry) {
			r.Register(valueobject.ToolDefinition{
				Type: "function",
				Function: valueobject.FunctionDefinition{
					Name:        "read_file",
					Description: "Read",
					Parameters:  json.RawMessage(`{"type":"object"}`),
				},
			}, func(_ context.Context, _ json.RawMessage) (string, error) {
				return toolResult, nil
			})
		},
	})

	sessionStore := tool.NewSessionRegistryStore(catalog, func(ar *tool.ActiveRegistry, _ *tool.Catalog, _ input.SubAgentService) {
		if err := ar.LoadAll(); err != nil {
			t.Fatalf("LoadAll: %v", err)
		}
	})

	var callCount int
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, req *output.ChatRequest) (*output.ChatResponse, error) {
			callCount++
			if callCount == 1 {
				if len(req.Tools) == 0 {
					t.Error("expected tools in JIT Ask request, got none")
				}
				return &output.ChatResponse{
					FinishReason: "tool_calls",
					ToolCalls:    []valueobject.ToolCall{testToolCall("call_rf", "read_file", `{"path":"x"}`)},
				}, nil
			}
			return &output.ChatResponse{Content: "the answer is 42", FinishReason: "stop"}, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), nil, nil,
		WithSessionStore(sessionStore),
	)
	got, err := svc.Ask(context.Background(), "read the file")
	if err != nil {
		t.Fatalf("Ask() error = %v", err)
	}
	if got != "the answer is 42" {
		t.Errorf("Ask() = %q, want %q", got, "the answer is 42")
	}
	if sessionStore.Count() != 0 {
		t.Errorf("transient session not cleaned up, Count() = %d", sessionStore.Count())
	}
}

func TestAgentService_Chat_NilRegistry(t *testing.T) {
	mock := &mockLLMClient{
		completeStreamFunc: func(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
			ch := make(chan valueobject.StreamChunk, 2)
			ch <- valueobject.StreamChunk{Content: "hello"}
			ch <- valueobject.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), nil, nil)
	ch, err := svc.Chat(context.Background(), "sess_nil", "hi")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	content, _, chunkErr := drainChunks(ch)
	if chunkErr != nil {
		t.Fatalf("unexpected error: %v", chunkErr)
	}
	if content != "hello" {
		t.Errorf("streamed content = %q, want %q", content, "hello")
	}
}

func TestAgentService_Chat_ToolEvents(t *testing.T) {
	var completeCallCount int
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
			completeCallCount++
			if completeCallCount == 1 {
				return &output.ChatResponse{
					FinishReason: "tool_calls",
					ToolCalls:    []valueobject.ToolCall{testToolCall("call_1", "echo", `{}`)},
				}, nil
			}
			return &output.ChatResponse{FinishReason: "stop"}, nil
		},
		completeStreamFunc: func(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
			ch := make(chan valueobject.StreamChunk, 2)
			ch <- valueobject.StreamChunk{Content: "done"}
			ch <- valueobject.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), registryWithEcho(), nil)
	ch, err := svc.Chat(context.Background(), "sess_ev", "go")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	_, events, chunkErr := drainChunks(ch)
	if chunkErr != nil {
		t.Fatalf("unexpected error: %v", chunkErr)
	}

	// One tool call iteration produces: thinking(1) + calling("echo")
	if len(events) < 2 {
		t.Fatalf("expected at least 2 tool events, got %d", len(events))
	}

	if events[0].Kind != valueobject.ToolEventThinking {
		t.Errorf("events[0].Kind = %q, want %q", events[0].Kind, valueobject.ToolEventThinking)
	}
	if events[0].Iteration != 1 {
		t.Errorf("events[0].Iteration = %d, want 1", events[0].Iteration)
	}

	if events[1].Kind != valueobject.ToolEventCalling {
		t.Errorf("events[1].Kind = %q, want %q", events[1].Kind, valueobject.ToolEventCalling)
	}
	if events[1].ToolName != "echo" {
		t.Errorf("events[1].ToolName = %q, want %q", events[1].ToolName, "echo")
	}

	// Second iteration thinking event (LLM returns stop)
	if len(events) >= 3 && events[2].Kind != valueobject.ToolEventThinking {
		t.Errorf("events[2].Kind = %q, want %q", events[2].Kind, valueobject.ToolEventThinking)
	}
}

func TestAgentService_Chat_DefaultMaxIterations(t *testing.T) {
	cfg := defaultAgentConfig()
	svc := NewAgentService(&mockLLMClient{}, cfg, emptyRegistry(), nil).(*agentService)
	if svc.maxToolIterations != 50 {
		t.Errorf("default maxToolIterations = %d, want 50", svc.maxToolIterations)
	}
}

func TestEmitMetaToolEvents_FindToolsLoaded(t *testing.T) {
	ch := make(chan valueobject.StreamChunk, 5)
	svc := &agentService{}

	result := `{"category":"filemanagement","status":"loaded","tools_loaded":[{"name":"read_file","description":"Read"},{"name":"write_file","description":"Write"}],"message":"ok"}`
	svc.emitMetaToolEvents(ch, "find_tools", result, 0)

	select {
	case chunk := <-ch:
		if chunk.ToolEvent == nil {
			t.Fatal("expected ToolEvent, got nil")
		}
		if chunk.ToolEvent.Kind != valueobject.ToolEventToolLoaded {
			t.Errorf("Kind = %q, want %q", chunk.ToolEvent.Kind, valueobject.ToolEventToolLoaded)
		}
		if chunk.ToolEvent.ToolName != "filemanagement" {
			t.Errorf("ToolName = %q, want %q", chunk.ToolEvent.ToolName, "filemanagement")
		}
		if chunk.ToolEvent.Detail != "2 tools available" {
			t.Errorf("Detail = %q, want %q", chunk.ToolEvent.Detail, "2 tools available")
		}
	default:
		t.Fatal("expected event on channel, got nothing")
	}
}

func TestEmitMetaToolEvents_FindToolsList_NoEvent(t *testing.T) {
	ch := make(chan valueobject.StreamChunk, 5)
	svc := &agentService{}

	result := `{"categories":[{"category":"web","loaded":false}],"count":1}`
	svc.emitMetaToolEvents(ch, "find_tools", result, 0)

	select {
	case chunk := <-ch:
		t.Fatalf("expected no event for list action, got %+v", chunk)
	default:
	}
}

func TestEmitMetaToolEvents_FindSkillsActivated(t *testing.T) {
	ch := make(chan valueobject.StreamChunk, 5)
	svc := &agentService{}

	result := `{"skill":"golang-pro","status":"activated","instructions":"You are a senior Go engineer..."}`
	svc.emitMetaToolEvents(ch, "find_skills", result, 0)

	select {
	case chunk := <-ch:
		if chunk.ToolEvent == nil {
			t.Fatal("expected ToolEvent, got nil")
		}
		if chunk.ToolEvent.Kind != valueobject.ToolEventSkillActivated {
			t.Errorf("Kind = %q, want %q", chunk.ToolEvent.Kind, valueobject.ToolEventSkillActivated)
		}
		if chunk.ToolEvent.ToolName != "golang-pro" {
			t.Errorf("ToolName = %q, want %q", chunk.ToolEvent.ToolName, "golang-pro")
		}
		if chunk.ToolEvent.Detail != "skill activated" {
			t.Errorf("Detail = %q, want %q", chunk.ToolEvent.Detail, "skill activated")
		}
	default:
		t.Fatal("expected event on channel, got nothing")
	}
}

func TestEmitMetaToolEvents_FindSkillsList_NoEvent(t *testing.T) {
	ch := make(chan valueobject.StreamChunk, 5)
	svc := &agentService{}

	result := `{"skills":[{"name":"golang-pro","description":"Go expert"}],"count":1}`
	svc.emitMetaToolEvents(ch, "find_skills", result, 0)

	select {
	case chunk := <-ch:
		t.Fatalf("expected no event for list action, got %+v", chunk)
	default:
	}
}

func TestEmitMetaToolEvents_UnrelatedTool_NoEvent(t *testing.T) {
	ch := make(chan valueobject.StreamChunk, 5)
	svc := &agentService{}

	svc.emitMetaToolEvents(ch, "echo", `{"msg":"hello"}`, 0)

	select {
	case chunk := <-ch:
		t.Fatalf("expected no event for unrelated tool, got %+v", chunk)
	default:
	}
}

func TestEmitMetaToolEvents_NilChannel_NoPanic(t *testing.T) {
	svc := &agentService{}
	svc.emitMetaToolEvents(nil, "find_tools", `{"status":"loaded","category":"web","tools_loaded":[]}`, 0)
}

func TestEmitMetaToolEvents_InvalidJSON_NoEvent(t *testing.T) {
	ch := make(chan valueobject.StreamChunk, 5)
	svc := &agentService{}

	svc.emitMetaToolEvents(ch, "find_tools", `{invalid json`, 0)

	select {
	case chunk := <-ch:
		t.Fatalf("expected no event for invalid JSON, got %+v", chunk)
	default:
	}
}

func TestChat_FindToolsEmitsToolLoadedEvent(t *testing.T) {
	loadResult := `{"category":"shellexec","status":"loaded","tools_loaded":[{"name":"run_command","description":"Run"}],"message":"ok"}`

	r := tool.NewRegistry()
	r.Register(valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name:        "find_tools",
			Description: "Discover tools",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}, func(_ context.Context, _ json.RawMessage) (string, error) {
		return loadResult, nil
	})

	var callCount int
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
			callCount++
			if callCount == 1 {
				return &output.ChatResponse{
					FinishReason: "tool_calls",
					ToolCalls:    []valueobject.ToolCall{testToolCall("call_ft", "find_tools", `{"action":"load","category":"shellexec"}`)},
				}, nil
			}
			return &output.ChatResponse{FinishReason: "stop"}, nil
		},
		completeStreamFunc: func(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
			ch := make(chan valueobject.StreamChunk, 2)
			ch <- valueobject.StreamChunk{Content: "done"}
			ch <- valueobject.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), r, nil)
	ch, err := svc.Chat(context.Background(), "sess_ft", "load tools")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	_, events, chunkErr := drainChunks(ch)
	if chunkErr != nil {
		t.Fatalf("unexpected error: %v", chunkErr)
	}

	var foundLoaded bool
	for _, ev := range events {
		if ev.Kind == valueobject.ToolEventToolLoaded {
			foundLoaded = true
			if ev.ToolName != "shellexec" {
				t.Errorf("ToolEventToolLoaded.ToolName = %q, want %q", ev.ToolName, "shellexec")
			}
			if ev.Detail != "1 tool available" {
				t.Errorf("ToolEventToolLoaded.Detail = %q, want %q", ev.Detail, "1 tool available")
			}
		}
	}
	if !foundLoaded {
		t.Error("expected ToolEventToolLoaded event in stream, not found")
	}
}

func TestChat_FindSkillsEmitsSkillActivatedEvent(t *testing.T) {
	activateResult := `{"skill":"review-pr","status":"activated","instructions":"Review code..."}`

	r := tool.NewRegistry()
	r.Register(valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name:        "find_skills",
			Description: "Discover skills",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}, func(_ context.Context, _ json.RawMessage) (string, error) {
		return activateResult, nil
	})

	var callCount int
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
			callCount++
			if callCount == 1 {
				return &output.ChatResponse{
					FinishReason: "tool_calls",
					ToolCalls:    []valueobject.ToolCall{testToolCall("call_fs", "find_skills", `{"action":"activate","name":"review-pr"}`)},
				}, nil
			}
			return &output.ChatResponse{FinishReason: "stop"}, nil
		},
		completeStreamFunc: func(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
			ch := make(chan valueobject.StreamChunk, 2)
			ch <- valueobject.StreamChunk{Content: "done"}
			ch <- valueobject.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), r, nil)
	ch, err := svc.Chat(context.Background(), "sess_fs", "activate skill")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	_, events, chunkErr := drainChunks(ch)
	if chunkErr != nil {
		t.Fatalf("unexpected error: %v", chunkErr)
	}

	var foundActivated bool
	for _, ev := range events {
		if ev.Kind == valueobject.ToolEventSkillActivated {
			foundActivated = true
			if ev.ToolName != "review-pr" {
				t.Errorf("ToolEventSkillActivated.ToolName = %q, want %q", ev.ToolName, "review-pr")
			}
		}
	}
	if !foundActivated {
		t.Error("expected ToolEventSkillActivated event in stream, not found")
	}
}

func TestChat_XMLToolCall_FindTools_EmitsEvent(t *testing.T) {
	loadResult := `{"category":"shellexec","status":"loaded","tools_loaded":[{"name":"run_command","description":"Run"}],"message":"ok"}`

	r := tool.NewRegistry()
	r.Register(valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name:        "find_tools",
			Description: "Discover tools",
			Parameters:  json.RawMessage(`{"type":"object"}`),
		},
	}, func(_ context.Context, _ json.RawMessage) (string, error) {
		return loadResult, nil
	})

	xmlBlock := `<minimax:tool_call><invoke name="find_tools"><parameter name="action">"load"</parameter><parameter name="category">"shellexec"</parameter></invoke></minimax:tool_call>`

	streamCallCount := 0
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
			return &output.ChatResponse{FinishReason: "stop"}, nil
		},
		completeStreamFunc: func(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
			streamCallCount++
			ch := make(chan valueobject.StreamChunk, 3)
			if streamCallCount == 1 {
				ch <- valueobject.StreamChunk{Content: xmlBlock}
				ch <- valueobject.StreamChunk{Done: true}
			} else {
				ch <- valueobject.StreamChunk{Content: "done"}
				ch <- valueobject.StreamChunk{Done: true}
			}
			close(ch)
			return ch, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), r, nil)
	ch, err := svc.Chat(context.Background(), "sess_xml_ft", "load tools")
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	_, events, chunkErr := drainChunks(ch)
	if chunkErr != nil {
		t.Fatalf("unexpected error: %v", chunkErr)
	}

	var foundLoaded bool
	for _, ev := range events {
		if ev.Kind == valueobject.ToolEventToolLoaded {
			foundLoaded = true
			if ev.ToolName != "shellexec" {
				t.Errorf("ToolEventToolLoaded.ToolName = %q, want %q", ev.ToolName, "shellexec")
			}
		}
	}
	if !foundLoaded {
		t.Error("expected ToolEventToolLoaded event from XML tool call path, not found")
	}
}

func TestAgentService_Chat_PanicInToolHandler(t *testing.T) {
	r := tool.NewRegistry()
	r.Register(valueobject.ToolDefinition{
		Type: "function",
		Function: valueobject.FunctionDefinition{
			Name:       "panic_tool",
			Parameters: json.RawMessage(`{"type":"object"}`),
		},
	}, func(_ context.Context, _ json.RawMessage) (string, error) {
		panic("tool handler exploded")
	})

	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
			return &output.ChatResponse{
				FinishReason: "tool_calls",
				ToolCalls:    []valueobject.ToolCall{testToolCall("call_panic", "panic_tool", `{}`)},
			}, nil
		},
	}

	svc := NewAgentService(mock, defaultAgentConfig(), r, nil)
	ch, err := svc.Chat(context.Background(), "sess_panic", "trigger panic")
	if err != nil {
		t.Fatalf("Chat() should not return error directly, got: %v", err)
	}

	_, _, chunkErr := drainChunks(ch)
	if chunkErr == nil {
		t.Fatal("expected error chunk from panic recovery")
	}
	if !strings.Contains(chunkErr.Error(), "panic") {
		t.Errorf("error = %q, want substring 'panic'", chunkErr)
	}
}
