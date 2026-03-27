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
	completeFunc     func(ctx context.Context, req *LLMRequest) (LLMResponse, error)
	estimateCostFunc func(req *LLMRequest) (float64, error)
	mu               sync.Mutex
	calls            []*LLMRequest
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

	err := fireLLM(context.Background(), trans, cpn, consumed)
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
	if req.Messages[0].Content != "Classify the input." {
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

	err := fireLLM(context.Background(), trans, cpn, consumed)
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

	err := fireLLM(context.Background(), trans, cpn, consumed)
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

	err := fireLLM(context.Background(), trans, cpn, consumed)
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

	err := fireLLM(context.Background(), trans, cpn, consumed)
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

	err := fireLLM(context.Background(), trans, cpn, consumed)
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

	err := fireLLM(context.Background(), trans, cpn, consumed)
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

	err := fireLLM(ctx, trans, cpn, consumed)
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

	err := fireLLM(context.Background(), trans, cpn, consumed)
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

	err := fireLLM(context.Background(), trans, cpn, consumed)
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

	consumed := []Token{
		{Color: ColorString, Payload: "first input"},
		{Color: ColorJSON, Payload: `{"key":"value"}`},
	}

	err := fireLLM(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := mock.getCalls()
	last := calls[0].Messages[len(calls[0].Messages)-1]
	if !strings.Contains(last.Content, "Token 1") || !strings.Contains(last.Content, "Token 2") {
		t.Errorf("expected labeled sections for multiple tokens, got: %q", last.Content)
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

	err := fireLLM(context.Background(), trans, cpn, consumed)
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

	err := fireLLM(context.Background(), trans, cpn, consumed)
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

	err := fireLLM(context.Background(), trans, cpn, consumed)
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

	err := fireLLM(context.Background(), trans, cpn, consumed)
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

	err := fireLLM(context.Background(), trans, cpn, consumed)
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

	err := fireLLM(context.Background(), trans, cpn, consumed)
	if err != nil {
		t.Fatalf("expected graceful degradation, got error: %v", err)
	}

	output := cpn.Places["P:OUTPUT"]
	if output.Len() != 1 {
		t.Fatalf("expected 1 output token, got %d", output.Len())
	}
}
