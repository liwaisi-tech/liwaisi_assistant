package cpn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// ── Mock LLMClient ───────────────────────────────────────────────────────────

type mockLLMClient struct {
	completeFunc       func(ctx context.Context, req *LLMRequest) (LLMResponse, error)
	completeStreamFunc func(ctx context.Context, req *LLMRequest, onChunk func(string)) (LLMResponse, error)
	estimateCostFunc   func(req *LLMRequest) (float64, error)
	mu                 sync.Mutex
	calls              []*LLMRequest
}

func (m *mockLLMClient) Complete(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
	m.mu.Lock()
	m.calls = append(m.calls, req)
	m.mu.Unlock()
	if m.completeFunc != nil {
		return m.completeFunc(ctx, req)
	}
	return LLMResponse{Content: "default response"}, nil
}

func (m *mockLLMClient) CompleteStream(ctx context.Context, req *LLMRequest, onChunk func(string)) (LLMResponse, error) {
	if m.completeStreamFunc != nil {
		m.mu.Lock()
		m.calls = append(m.calls, req)
		m.mu.Unlock()
		return m.completeStreamFunc(ctx, req, onChunk)
	}
	// Default: delegate to Complete, no streaming.
	return m.Complete(ctx, req)
}

func (m *mockLLMClient) EstimateCost(req *LLMRequest) (float64, error) {
	if m.estimateCostFunc != nil {
		return m.estimateCostFunc(req)
	}
	return 0.001, nil
}

func (m *mockLLMClient) getCalls() []*LLMRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]*LLMRequest, len(m.calls))
	copy(cp, m.calls)
	return cp
}

// ── Test Helpers ─────────────────────────────────────────────────────────────

func newTestCPNForLLM(mock *mockLLMClient, transitions map[string]*Transition) *CPN {
	places := map[string]*Place{
		"P:INPUT": {
			ID:    "P:INPUT",
			Space: SpaceComputation,
			Color: ColorString,
		},
		"P:OUTPUT": {
			ID:    "P:OUTPUT",
			Space: SpaceComputation,
			Color: ColorArtifact,
		},
	}

	return &CPN{
		ID:                "test-cpn",
		Role:              "worker",
		Depth:             2,
		SessionID:         "session-1",
		LLMClient:         mock,
		History:           nil,
		ContextWindowSize: DefaultContextWindowSize,
		Places:            places,
		Transitions:       transitions,
	}
}

func newBasicLLMTransition() *Transition {
	return &Transition{
		ID:           "llm-classify",
		Kind:         NodeKindLLM,
		InputPlaces:  []string{"P:INPUT"},
		OutputPlaces: []string{"P:OUTPUT"},
		SystemPrompt: "Classify the input.",
		LLMConfig: &LLMConfig{
			Model:     "classifier",
			MaxTokens: 50,
		},
	}
}

// ── Tests ────────────────────────────────────────────────────────────────────

func TestFireLLM_ContextWindowAssembly(t *testing.T) {
	mock := &mockLLMClient{}
	trans := newBasicLLMTransition()
	cpn := newTestCPNForLLM(mock, map[string]*Transition{trans.ID: trans})

	cpn.History = []*Message{
		{Role: RoleUser, Content: "hello"},
		{Role: RoleAssistant, Content: "hi there"},
		{Role: RoleObserver, Content: "summary from research", CPNRole: "researcher"},
	}

	consumed := []Token{{Color: ColorString, Payload: "test input"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := mock.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	req := calls[0]
	// Should have: system + observer + user + assistant + consumed payload
	if len(req.Messages) < 4 {
		t.Fatalf("expected at least 4 messages, got %d", len(req.Messages))
	}

	// First message should be system prompt.
	if req.Messages[0].Role != "system" {
		t.Errorf("expected first message role=system, got %q", req.Messages[0].Role)
	}
	// fireLLM prepends the per-session regional-variant preamble at runtime
	// (CON-003: it never mutates t.SystemPrompt). The static body must still
	// appear at the end of the rendered system prompt.
	if !strings.HasSuffix(req.Messages[0].Content, "Classify the input.") {
		t.Errorf("unexpected system prompt: %q", req.Messages[0].Content)
	}

	// Last message should be the consumed token payload.
	last := req.Messages[len(req.Messages)-1]
	if last.Role != "user" {
		t.Errorf("expected last message role=user, got %q", last.Role)
	}
	if last.Content != "test input" {
		t.Errorf("expected consumed payload 'test input', got %q", last.Content)
	}
}

func TestFireLLM_BudgetExceeded(t *testing.T) {
	mock := &mockLLMClient{
		estimateCostFunc: func(req *LLMRequest) (float64, error) {
			return 0.05, nil
		},
	}
	trans := newBasicLLMTransition()
	trans.LLMConfig.Budget = 0.01
	cpn := newTestCPNForLLM(mock, map[string]*Transition{trans.ID: trans})

	consumed := []Token{{Color: ColorString, Payload: "input"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if !errors.Is(err, ErrBudgetExceeded) {
		t.Fatalf("expected ErrBudgetExceeded, got: %v", err)
	}

	// Complete should NOT have been called.
	calls := mock.getCalls()
	if len(calls) != 0 {
		t.Errorf("Complete should not be called when budget exceeded, got %d calls", len(calls))
	}
}

func TestFireLLM_BudgetZero_NoBudgetCheck(t *testing.T) {
	estimateCalled := false
	mock := &mockLLMClient{
		estimateCostFunc: func(req *LLMRequest) (float64, error) {
			estimateCalled = true
			return 0.05, nil
		},
	}
	trans := newBasicLLMTransition()
	trans.LLMConfig.Budget = 0 // Disabled
	cpn := newTestCPNForLLM(mock, map[string]*Transition{trans.ID: trans})

	consumed := []Token{{Color: ColorString, Payload: "input"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if estimateCalled {
		t.Error("EstimateCost should not be called when Budget=0")
	}
}

func TestFireLLM_ToolCallDispatched(t *testing.T) {
	var callCount atomic.Int32
	var secondCallMessages []*LLMMessage
	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			count := callCount.Add(1)
			if count == 1 {
				// First call: request a tool call.
				return LLMResponse{
					ToolCalls: []*LLMToolCall{
						{ID: "tc-1", ToolName: "web_search", Arguments: json.RawMessage(`{"query":"test"}`)},
					},
				}, nil
			}
			// Second call: capture messages and return final content.
			secondCallMessages = req.Messages
			return LLMResponse{Content: "final answer"}, nil
		},
	}

	toolExecuted := false
	webSearchTransition := &Transition{
		ID:       "web_search",
		Kind:     NodeKindTool,
		ToolName: "web_search",
		Executor: func(ctx context.Context, in Token) (Token, error) {
			toolExecuted = true
			return Token{Payload: "search results"}, nil
		},
	}

	trans := newBasicLLMTransition()
	trans.LLMTools = []string{"web_search"}

	transitions := map[string]*Transition{
		trans.ID:               trans,
		webSearchTransition.ID: webSearchTransition,
	}
	cpn := newTestCPNForLLM(mock, transitions)

	consumed := []Token{{Color: ColorString, Payload: "input"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !toolExecuted {
		t.Error("tool executor should have been called")
	}

	// Verify output was deposited.
	output := cpn.Places["P:OUTPUT"]
	if output.Len() != 1 {
		t.Fatalf("expected 1 output token, got %d", output.Len())
	}

	tokens, _ := output.Peek()
	if tokens[0].Payload != "final answer" {
		t.Errorf("expected 'final answer', got %v", tokens[0].Payload)
	}

	// Verify tool result was sent back to LLM in the second call.
	foundToolResult := false
	for _, msg := range secondCallMessages {
		if msg.Role == "tool" && msg.ToolResult != nil {
			if strings.Contains(msg.ToolResult.Content, "search results") {
				foundToolResult = true
				break
			}
		}
	}
	if !foundToolResult {
		t.Error("tool result should have been sent back to LLM in second call")
	}
}

func TestFireLLM_ToolCallLoop_MaxIterations(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			// Always return tool calls — never produce content.
			return LLMResponse{
				ToolCalls: []*LLMToolCall{
					{ID: "tc-loop", ToolName: "looper", Arguments: json.RawMessage(`{}`)},
				},
			}, nil
		},
	}

	looperTransition := &Transition{
		ID:       "looper",
		Kind:     NodeKindTool,
		ToolName: "looper",
		Executor: func(ctx context.Context, in Token) (Token, error) {
			return Token{Payload: "loop result"}, nil
		},
	}

	trans := newBasicLLMTransition()
	trans.LLMTools = []string{"looper"}

	transitions := map[string]*Transition{
		trans.ID:            trans,
		looperTransition.ID: looperTransition,
	}
	cpn := newTestCPNForLLM(mock, transitions)

	consumed := []Token{{Color: ColorString, Payload: "input"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if !errors.Is(err, ErrToolCallLoopExceeded) {
		t.Fatalf("expected ErrToolCallLoopExceeded, got: %v", err)
	}

	// Should have been called MaxToolCallIterations + 1 times (initial + re-calls).
	calls := mock.getCalls()
	if len(calls) != MaxToolCallIterations+1 {
		t.Errorf("expected %d LLM calls, got %d", MaxToolCallIterations+1, len(calls))
	}
}

func TestFireLLM_DisallowedTool(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			return LLMResponse{
				ToolCalls: []*LLMToolCall{
					{ID: "tc-1", ToolName: "forbidden_tool", Arguments: json.RawMessage(`{}`)},
				},
			}, nil
		},
	}

	trans := newBasicLLMTransition()
	trans.LLMTools = []string{"allowed_tool"} // forbidden_tool is NOT in list

	transitions := map[string]*Transition{trans.ID: trans}
	cpn := newTestCPNForLLM(mock, transitions)

	consumed := []Token{{Color: ColorString, Payload: "input"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if !errors.Is(err, ErrDisallowedTool) {
		t.Fatalf("expected ErrDisallowedTool, got: %v", err)
	}
}

func TestFireLLM_ToolExecutorError_PassedToLLM(t *testing.T) {
	var callCount atomic.Int32
	var secondCallMessages []*LLMMessage
	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			count := callCount.Add(1)
			if count == 1 {
				return LLMResponse{
					ToolCalls: []*LLMToolCall{
						{ID: "tc-1", ToolName: "failing_tool", Arguments: json.RawMessage(`{}`)},
					},
				}, nil
			}
			secondCallMessages = req.Messages
			return LLMResponse{Content: "recovered"}, nil
		},
	}

	failingTool := &Transition{
		ID:       "failing_tool",
		Kind:     NodeKindTool,
		ToolName: "failing_tool",
		Executor: func(ctx context.Context, in Token) (Token, error) {
			return Token{}, fmt.Errorf("tool broke")
		},
	}

	trans := newBasicLLMTransition()
	trans.LLMTools = []string{"failing_tool"}

	transitions := map[string]*Transition{
		trans.ID:       trans,
		failingTool.ID: failingTool,
	}
	cpn := newTestCPNForLLM(mock, transitions)

	consumed := []Token{{Color: ColorString, Payload: "input"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the error was passed back to the LLM as a tool result.
	found := false
	for _, msg := range secondCallMessages {
		if msg.Role == "tool" && msg.ToolResult != nil {
			if strings.Contains(msg.ToolResult.Content, "error: tool broke") {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("tool error should have been passed to LLM as tool result")
	}
}

func TestFireLLM_ContextCancellation(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			// First call returns a tool call.
			return LLMResponse{
				ToolCalls: []*LLMToolCall{
					{ID: "tc-1", ToolName: "slow_tool", Arguments: json.RawMessage(`{}`)},
				},
			}, nil
		},
	}

	slowTool := &Transition{
		ID:       "slow_tool",
		Kind:     NodeKindTool,
		ToolName: "slow_tool",
		Executor: func(ctx context.Context, in Token) (Token, error) {
			return Token{Payload: "result"}, nil
		},
	}

	trans := newBasicLLMTransition()
	trans.LLMTools = []string{"slow_tool"}

	transitions := map[string]*Transition{
		trans.ID:    trans,
		slowTool.ID: slowTool,
	}
	cpn := newTestCPNForLLM(mock, transitions)

	consumed := []Token{{Color: ColorString, Payload: "input"}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, _, err := fireLLM(ctx, trans, cpn, consumed)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
}

func TestFireLLM_RequireJSON_OutputColorJSON(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			if req.ResponseFmt != "json_object" {
				t.Errorf("expected ResponseFmt=json_object, got %q", req.ResponseFmt)
			}
			return LLMResponse{Content: `{"class":"simple"}`}, nil
		},
	}

	trans := newBasicLLMTransition()
	trans.LLMConfig.RequireJSON = true

	cpn := newTestCPNForLLM(mock, map[string]*Transition{trans.ID: trans})
	// Override output place color to match ColorJSON output.
	cpn.Places["P:OUTPUT"] = &Place{
		ID:    "P:OUTPUT",
		Space: SpaceComputation,
		Color: ColorJSON,
	}
	consumed := []Token{{Color: ColorString, Payload: "input"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := cpn.Places["P:OUTPUT"]
	tokens, _ := output.Peek()
	if tokens[0].Color != ColorJSON {
		t.Errorf("expected ColorJSON, got %s", tokens[0].Color)
	}
}

func TestFireLLM_NoRequireJSON_OutputColorArtifact(t *testing.T) {
	mock := &mockLLMClient{}
	trans := newBasicLLMTransition()
	trans.LLMConfig.RequireJSON = false

	cpn := newTestCPNForLLM(mock, map[string]*Transition{trans.ID: trans})
	consumed := []Token{{Color: ColorString, Payload: "input"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := cpn.Places["P:OUTPUT"]
	tokens, _ := output.Peek()
	if tokens[0].Color != ColorArtifact {
		t.Errorf("expected ColorArtifact, got %s", tokens[0].Color)
	}
}

func TestFireLLM_MultipleInputPlaces(t *testing.T) {
	mock := &mockLLMClient{}
	trans := newBasicLLMTransition()
	cpn := newTestCPNForLLM(mock, map[string]*Transition{trans.ID: trans})

	// Two ColorString tokens → both forwarded as labeled sections.
	consumed := []Token{
		{Color: ColorString, Payload: "first input"},
		{Color: ColorString, Payload: "second input"},
	}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := mock.getCalls()
	last := calls[0].Messages[len(calls[0].Messages)-1]
	if !strings.Contains(last.Content, "Token 1") || !strings.Contains(last.Content, "Token 2") {
		t.Errorf("expected labeled sections for multiple tokens, got: %q", last.Content)
	}
}

func TestFireLLM_SkipsNonUserTokenColors(t *testing.T) {
	mock := &mockLLMClient{}
	trans := newBasicLLMTransition()
	cpn := newTestCPNForLLM(mock, map[string]*Transition{trans.ID: trans})

	// ColorJSON tokens (e.g. from classifier) should be filtered out.
	consumed := []Token{
		{Color: ColorJSON, Payload: `{"intent":"conversation"}`},
	}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := mock.getCalls()
	// Only system prompt should be present — no user message from JSON token.
	for _, msg := range calls[0].Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "intent") {
			t.Errorf("JSON routing token should not appear as user message, got: %q", msg.Content)
		}
	}
}

func TestFireLLM_MultipleOutputPlaces(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			return LLMResponse{Content: "output content"}, nil
		},
	}

	trans := newBasicLLMTransition()
	trans.OutputPlaces = []string{"P:OUTPUT", "P:OUTPUT2"}

	transitions := map[string]*Transition{trans.ID: trans}
	places := map[string]*Place{
		"P:INPUT": {
			ID:    "P:INPUT",
			Space: SpaceComputation,
			Color: ColorString,
		},
		"P:OUTPUT": {
			ID:    "P:OUTPUT",
			Space: SpaceComputation,
			Color: ColorArtifact,
		},
		"P:OUTPUT2": {
			ID:    "P:OUTPUT2",
			Space: SpaceComputation,
			Color: ColorArtifact,
		},
	}

	cpn := &CPN{
		ID:                "test-cpn",
		Depth:             2,
		SessionID:         "session-1",
		LLMClient:         mock,
		ContextWindowSize: DefaultContextWindowSize,
		Places:            places,
		Transitions:       transitions,
	}

	consumed := []Token{{Color: ColorString, Payload: "input"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cpn.Places["P:OUTPUT"].Len() != 1 {
		t.Errorf("expected 1 token in P:OUTPUT, got %d", cpn.Places["P:OUTPUT"].Len())
	}
	if cpn.Places["P:OUTPUT2"].Len() != 1 {
		t.Errorf("expected 1 token in P:OUTPUT2, got %d", cpn.Places["P:OUTPUT2"].Len())
	}
}

func TestFireLLM_LLMClientError_Propagates(t *testing.T) {
	expectedErr := fmt.Errorf("connection refused")
	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			return LLMResponse{}, expectedErr
		},
	}

	trans := newBasicLLMTransition()
	cpn := newTestCPNForLLM(mock, map[string]*Transition{trans.ID: trans})
	consumed := []Token{{Color: ColorString, Payload: "input"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected error to contain 'connection refused', got: %v", err)
	}
}

func TestFireLLM_NilLLMConfig(t *testing.T) {
	mock := &mockLLMClient{}
	trans := newBasicLLMTransition()
	trans.LLMConfig = nil
	cpn := newTestCPNForLLM(mock, map[string]*Transition{trans.ID: trans})

	consumed := []Token{{Color: ColorString, Payload: "input"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if err == nil {
		t.Fatal("expected error for nil LLMConfig")
	}
	if !strings.Contains(err.Error(), "nil LLMConfig") {
		t.Errorf("expected 'nil LLMConfig' error, got: %v", err)
	}
}

func TestFireLLM_OutputMetadata(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			return LLMResponse{Content: "result"}, nil
		},
	}

	trans := newBasicLLMTransition()
	cpn := newTestCPNForLLM(mock, map[string]*Transition{trans.ID: trans})

	consumed := []Token{{Color: ColorString, Payload: "input"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := cpn.Places["P:OUTPUT"]
	tokens, _ := output.Peek()
	tok := tokens[0]

	if tok.OriginID != "test-cpn" {
		t.Errorf("expected OriginID=test-cpn, got %q", tok.OriginID)
	}
	if tok.OriginDepth != 2 {
		t.Errorf("expected OriginDepth=2, got %d", tok.OriginDepth)
	}
	if tok.OriginKind != NodeKindLLM {
		t.Errorf("expected OriginKind=NodeKindLLM, got %q", tok.OriginKind)
	}
	if tok.SessionID != "session-1" {
		t.Errorf("expected SessionID=session-1, got %q", tok.SessionID)
	}
	if tok.Timestamp.IsZero() {
		t.Error("expected non-zero Timestamp")
	}
}

func TestFormatTokenPayload_Single(t *testing.T) {
	tokens := []Token{{Color: ColorString, Payload: "hello world"}}
	result := formatTokenPayload(tokens)
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestFormatTokenPayload_Multiple(t *testing.T) {
	tokens := []Token{
		{Color: ColorString, Payload: "first"},
		{Color: ColorJSON, Payload: `{"key":"val"}`},
	}
	result := formatTokenPayload(tokens)
	if !strings.Contains(result, "[Token 1 (STRING)]") {
		t.Errorf("expected Token 1 label, got: %q", result)
	}
	if !strings.Contains(result, "[Token 2 (JSON)]") {
		t.Errorf("expected Token 2 label, got: %q", result)
	}
	if !strings.Contains(result, "---") {
		t.Errorf("expected separator, got: %q", result)
	}
}

func TestBuildToolSchema(t *testing.T) {
	trans := &Transition{
		ID:       "web_search",
		Kind:     NodeKindTool,
		ToolName: "web_search",
	}
	schema := buildToolSchema(trans)
	if schema.Name != "web_search" {
		t.Errorf("expected Name=web_search, got %q", schema.Name)
	}
	if schema.Description == "" {
		t.Error("expected non-empty Description")
	}
}

func TestInferOutputColor(t *testing.T) {
	if inferOutputColor(true) != ColorJSON {
		t.Error("expected ColorJSON for requireJSON=true")
	}
	if inferOutputColor(false) != ColorArtifact {
		t.Error("expected ColorArtifact for requireJSON=false")
	}
}

func TestHandleToolCalls_NoToolCalls(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			return LLMResponse{Content: "direct answer"}, nil
		},
	}

	trans := newBasicLLMTransition()
	cpn := newTestCPNForLLM(mock, map[string]*Transition{trans.ID: trans})

	consumed := []Token{{Color: ColorString, Payload: "input"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := cpn.Places["P:OUTPUT"]
	tokens, _ := output.Peek()
	if tokens[0].Payload != "direct answer" {
		t.Errorf("expected 'direct answer', got %v", tokens[0].Payload)
	}

	// Only 1 LLM call — no tool loop.
	calls := mock.getCalls()
	if len(calls) != 1 {
		t.Errorf("expected 1 call, got %d", len(calls))
	}
}

func TestFireLLM_EstimateCostError_GracefulDegradation(t *testing.T) {
	mock := &mockLLMClient{
		estimateCostFunc: func(req *LLMRequest) (float64, error) {
			return 0, fmt.Errorf("cost estimation failed")
		},
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			return LLMResponse{Content: "still works"}, nil
		},
	}

	trans := newBasicLLMTransition()
	trans.LLMConfig.Budget = 0.01 // Budget set but EstimateCost fails

	cpn := newTestCPNForLLM(mock, map[string]*Transition{trans.ID: trans})
	consumed := []Token{{Color: ColorString, Payload: "input"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("expected graceful degradation, got error: %v", err)
	}

	output := cpn.Places["P:OUTPUT"]
	if output.Len() != 1 {
		t.Fatalf("expected 1 output token, got %d", output.Len())
	}
}

// ── buildTrace ──────────────────────────────────────────────────────────────

func TestBuildTrace_AutoPopulates(t *testing.T) {
	trans := NewTransition("t-classify", NodeKindLLM, []string{"P:IN"}, []string{"P:OUT"})
	trans.LLMConfig = &LLMConfig{MaxTokens: 100}

	c := &CPN{ID: "cpn-sess-1", SessionID: "sess-1"}

	trace := buildTrace(trans, c)

	if trace.TraceID != "sess-1" {
		t.Errorf("TraceID = %q, want %q", trace.TraceID, "sess-1")
	}
	if trace.GenerationName != "t-classify" {
		t.Errorf("GenerationName = %q, want %q", trace.GenerationName, "t-classify")
	}
	if trace.SpanName != "cpn-sess-1/t-classify" {
		t.Errorf("SpanName = %q, want %q", trace.SpanName, "cpn-sess-1/t-classify")
	}
}

func TestBuildTrace_ExplicitConfigTakesPrecedence(t *testing.T) {
	explicit := &TraceConfig{
		TraceID:        "custom-trace",
		GenerationName: "custom-gen",
		SpanName:       "custom-span",
	}
	trans := NewTransition("t-llm", NodeKindLLM, []string{"P:IN"}, []string{"P:OUT"})
	trans.LLMConfig = &LLMConfig{MaxTokens: 100, Trace: explicit}

	c := &CPN{ID: "cpn-1", SessionID: "sess-1"}

	trace := buildTrace(trans, c)

	if trace != explicit {
		t.Error("expected explicit TraceConfig to be returned, got auto-generated")
	}
}

func TestFireLLM_SetsTrace(t *testing.T) {
	mock := &mockLLMClient{}
	trans := NewTransition("t-reason", NodeKindLLM, []string{"P:INPUT"}, []string{"P:OUTPUT"})
	trans.LLMConfig = &LLMConfig{MaxTokens: 100}

	cpn := newTestCPNForLLM(mock, map[string]*Transition{trans.ID: trans})
	consumed := []Token{{Color: ColorString, Payload: "test"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("fireLLM() error = %v", err)
	}

	if len(mock.calls) == 0 {
		t.Fatal("expected at least 1 LLM call")
	}
	trace := mock.calls[0].Trace
	if trace == nil {
		t.Fatal("expected Trace to be set on LLMRequest")
	}
	if trace.GenerationName != "t-reason" {
		t.Errorf("GenerationName = %q, want %q", trace.GenerationName, "t-reason")
	}
}

// ── Streaming Tests ────────────────────────────────────────────────────────

func TestFireLLM_StreamOutput_EmitsChunks(t *testing.T) {
	deltas := []string{"Hello", ", ", "world", "!"}
	mock := &mockLLMClient{
		completeStreamFunc: func(ctx context.Context, req *LLMRequest, onChunk func(string)) (LLMResponse, error) {
			if !req.Stream {
				t.Error("expected req.Stream=true")
			}
			for _, d := range deltas {
				onChunk(d)
			}
			return LLMResponse{Content: "Hello, world!"}, nil
		},
	}

	trans := newBasicLLMTransition()
	trans.LLMConfig.StreamOutput = true

	ec := &eventCollector{}
	cpn := newTestCPNForLLM(mock, map[string]*Transition{trans.ID: trans})
	cpn.EventSink = ec.sink

	consumed := []Token{{Color: ColorString, Payload: "input"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := ec.getEvents()

	// Count stream chunk events (excluding done sentinel).
	var chunkEvents []Event
	var doneEvents []Event
	for _, e := range events {
		if e.Type != EventStreamChunk {
			continue
		}
		chunk, ok := e.Payload.(StreamChunk)
		if !ok {
			t.Fatalf("expected StreamChunk payload, got %T", e.Payload)
		}
		if chunk.Done {
			doneEvents = append(doneEvents, e)
		} else {
			chunkEvents = append(chunkEvents, e)
		}
	}

	if len(chunkEvents) != len(deltas) {
		t.Fatalf("expected %d chunk events, got %d", len(deltas), len(chunkEvents))
	}

	for i, e := range chunkEvents {
		chunk := e.Payload.(StreamChunk)
		if chunk.Content != deltas[i] {
			t.Errorf("chunk[%d]: expected %q, got %q", i, deltas[i], chunk.Content)
		}
		if chunk.SessionID != "session-1" {
			t.Errorf("chunk[%d]: expected SessionID=session-1, got %q", i, chunk.SessionID)
		}
		if chunk.CPNID != "test-cpn" {
			t.Errorf("chunk[%d]: expected CPNID=test-cpn, got %q", i, chunk.CPNID)
		}
		if chunk.CPNRole != "worker" {
			t.Errorf("chunk[%d]: expected CPNRole=worker, got %q", i, chunk.CPNRole)
		}
	}

	// Verify done sentinel.
	if len(doneEvents) != 1 {
		t.Fatalf("expected 1 done sentinel, got %d", len(doneEvents))
	}
	doneSentinel := doneEvents[0].Payload.(StreamChunk)
	if doneSentinel.Content != "" {
		t.Errorf("done sentinel content should be empty, got %q", doneSentinel.Content)
	}

	// Verify output was still deposited.
	output := cpn.Places["P:OUTPUT"]
	if output.Len() != 1 {
		t.Fatalf("expected 1 output token, got %d", output.Len())
	}
	tokens, _ := output.Peek()
	if tokens[0].Payload != "Hello, world!" {
		t.Errorf("expected 'Hello, world!', got %v", tokens[0].Payload)
	}
}

func TestFireLLM_StreamOutput_False_NoChunks(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			if req.Stream {
				t.Error("expected req.Stream=false")
			}
			return LLMResponse{Content: "no stream"}, nil
		},
	}

	trans := newBasicLLMTransition()
	trans.LLMConfig.StreamOutput = false

	ec := &eventCollector{}
	cpn := newTestCPNForLLM(mock, map[string]*Transition{trans.ID: trans})
	cpn.EventSink = ec.sink

	consumed := []Token{{Color: ColorString, Payload: "input"}}

	_, _, err := fireLLM(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	events := ec.getEvents()
	for _, e := range events {
		if e.Type == EventStreamChunk {
			t.Error("expected no EventStreamChunk when StreamOutput=false")
		}
	}
}

// TestFireLLM_HistoryAppendIsThreadSafe verifies that concurrent fireLLM calls
// do not race on c.History. The -race flag will catch unsynchronized access.
func TestFireLLM_HistoryAppendIsThreadSafe(t *testing.T) {
	t.Parallel()

	const goroutines = 10

	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *LLMRequest) (LLMResponse, error) {
			return LLMResponse{Content: "response"}, nil
		},
	}

	// Build a CPN with enough input/output places for parallel fireLLM calls.
	places := make(map[string]*Place)
	transitions := make(map[string]*Transition)

	for i := range goroutines {
		inID := fmt.Sprintf("p-in-%d", i)
		outID := fmt.Sprintf("p-out-%d", i)
		tID := fmt.Sprintf("t-llm-%d", i)

		places[inID] = NewPlace(inID, ColorString, SpaceSurface)
		places[outID] = NewPlace(outID, ColorArtifact, SpaceSurface)

		tr := NewTransition(tID, NodeKindLLM, []string{inID}, []string{outID})
		tr.SystemPrompt = "test"
		tr.LLMConfig = &LLMConfig{MaxTokens: 64}
		transitions[tID] = tr
	}

	c := &CPN{
		ID:                "race-test-cpn",
		Role:              "worker",
		SessionID:         "race-session",
		LLMClient:         mock,
		ContextWindowSize: 10,
		Places:            places,
		Transitions:       transitions,
	}

	// Fire all transitions concurrently — race detector will flag unsynchronized History access.
	var wg sync.WaitGroup
	errs := make(chan error, goroutines)
	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tID := fmt.Sprintf("t-llm-%d", idx)
			consumed := []Token{{Color: ColorString, Payload: fmt.Sprintf("msg-%d", idx)}}
			if _, _, err := fireLLM(context.Background(), transitions[tID], c, consumed); err != nil {
				errs <- fmt.Errorf("fireLLM %d: %w", idx, err)
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}

	// Each fireLLM appends 2 entries (user + assistant) = goroutines * 2.
	c.mu.RLock()
	histLen := len(c.History)
	c.mu.RUnlock()

	expected := goroutines * 2
	if histLen != expected {
		t.Errorf("expected %d history entries, got %d", expected, histLen)
	}
}

// ── buildToolSchema tests ───────────────────────────────────────────────────

func TestHandleToolCalls_HITLChannelClosed(t *testing.T) {
	var callCount atomic.Int32
	mock := &mockLLMClient{
		completeFunc: func(ctx context.Context, req *LLMRequest) (LLMResponse, error) {
			count := callCount.Add(1)
			if count == 1 {
				// First call: request a tool that requires HITL.
				return LLMResponse{
					ToolCalls: []*LLMToolCall{
						{ID: "tc-hitl", ToolName: "dangerous_tool", Arguments: json.RawMessage(`{"action":"delete"}`)},
					},
				}, nil
			}
			return LLMResponse{Content: "should not reach here"}, nil
		},
	}

	hitlChan := make(chan Token)
	close(hitlChan) // Immediately closed — simulates dropped connection.

	dangerousTool := &Transition{
		ID:       "dangerous_tool",
		Kind:     NodeKindTool,
		ToolName: "dangerous_tool",
		ToolMeta: &ToolMeta{
			Description:  "A dangerous tool",
			RequiresHITL: true,
		},
		Executor: func(ctx context.Context, in Token) (Token, error) {
			t.Error("executor should NOT be called when HITL channel is closed")
			return Token{Payload: "nope"}, nil
		},
	}

	trans := newBasicLLMTransition()
	trans.LLMTools = []string{"dangerous_tool"}
	trans.HITLConfig = &HITLConfig{
		Channel: hitlChan,
	}

	transitions := map[string]*Transition{
		trans.ID:         trans,
		dangerousTool.ID: dangerousTool,
	}
	c := newTestCPNForLLM(mock, transitions)

	consumed := []Token{{Color: ColorString, Payload: "do something dangerous"}}

	_, _, err := fireLLM(context.Background(), trans, c, consumed)
	if err == nil {
		t.Fatal("expected error when HITL channel is closed")
	}
	if !strings.Contains(err.Error(), "HITL channel closed") {
		t.Errorf("error = %q, want to contain 'HITL channel closed'", err)
	}
}

func TestBuildToolSchema_WithToolMeta(t *testing.T) {
	params := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)
	tr := &Transition{
		ToolName: "system/search",
		ToolMeta: &ToolMeta{
			Description:  "Search the web",
			Parameters:   params,
			RequiresHITL: false,
			Namespace:    "system",
		},
	}

	schema := buildToolSchema(tr)
	if schema.Name != "system/search" {
		t.Errorf("name = %q, want %q", schema.Name, "system/search")
	}
	if schema.Description != "Search the web" {
		t.Errorf("description = %q, want %q", schema.Description, "Search the web")
	}
	if schema.Parameters == nil {
		t.Fatal("expected non-nil Parameters")
	}
	if string(schema.Parameters) != string(params) {
		t.Errorf("parameters = %s, want %s", schema.Parameters, params)
	}
}

func TestBuildToolSchema_WithoutToolMeta(t *testing.T) {
	tr := &Transition{
		ToolName: "system/search",
	}

	schema := buildToolSchema(tr)
	if schema.Name != "system/search" {
		t.Errorf("name = %q, want %q", schema.Name, "system/search")
	}
	if schema.Description != "Execute tool: system/search" {
		t.Errorf("description = %q, want %q", schema.Description, "Execute tool: system/search")
	}
	if schema.Parameters != nil {
		t.Errorf("expected nil Parameters, got %s", schema.Parameters)
	}
}
