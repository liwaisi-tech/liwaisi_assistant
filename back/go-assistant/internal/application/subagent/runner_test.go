package subagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/memory"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// mockLLMClient implements output.LLMClient for testing.
type mockLLMClient struct {
	mu        sync.Mutex
	calls     int
	responses []*output.ChatResponse
	errs      []error
}

func (m *mockLLMClient) Complete(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.calls
	m.calls++

	if idx < len(m.errs) && m.errs[idx] != nil {
		return nil, m.errs[idx]
	}
	if idx < len(m.responses) {
		return m.responses[idx], nil
	}
	return &output.ChatResponse{Content: "fallback"}, nil
}

func (m *mockLLMClient) CompleteStream(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockLLMClient) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func newTestRunner(client output.LLMClient, mem *memory.ConversationMemory, opts ...RunnerOption) *Runner {
	factory := func(_ valueobject.ModelTier) (output.LLMClient, string) {
		return client, "test-model"
	}
	memFactory := memory.NewSubAgentMemoryFactory(mem)
	return NewRunner(factory, memFactory, opts...)
}

// noRetryRecovery returns a RecoveryConfig with retries disabled and
// minimal delays, useful for tests that want the pre-recovery behavior.
func noRetryRecovery() RecoveryConfig {
	return RecoveryConfig{
		Timeout:       2 * time.Minute,
		MaxLLMRetries: 0,
		BaseDelay:     time.Millisecond,
		MaxDelay:      time.Millisecond,
	}
}

// fastRetryRecovery returns a RecoveryConfig with fast retries for tests.
func fastRetryRecovery() RecoveryConfig {
	return RecoveryConfig{
		Timeout:       2 * time.Minute,
		MaxLLMRetries: 2,
		BaseDelay:     time.Millisecond,
		MaxDelay:      5 * time.Millisecond,
	}
}

func TestRunner_SimpleCompletion(t *testing.T) {
	mock := &mockLLMClient{
		responses: []*output.ChatResponse{
			{
				Content:          "The answer is 42.",
				PromptTokens:     10,
				CompletionTokens: 5,
				TotalTokens:      15,
			},
		},
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem)

	spec := entity.SubAgentSpec{
		Name:        "simple-agent",
		Instruction: "You are a helpful assistant.",
		MaxTurns:    5,
	}

	result := runner.Run(context.Background(), &spec, nil, "What is the answer?", "parent-session")

	if result.Status != valueobject.SubAgentStatusCompleted {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCompleted)
	}
	if result.Output != "The answer is 42." {
		t.Errorf("Output = %q, want %q", result.Output, "The answer is 42.")
	}
	if result.AgentName != "simple-agent" {
		t.Errorf("AgentName = %q, want %q", result.AgentName, "simple-agent")
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("Usage.TotalTokens = %d, want 15", result.Usage.TotalTokens)
	}
	if result.Elapsed <= 0 {
		t.Error("Elapsed should be positive")
	}
	if !result.IsSuccess() {
		t.Error("IsSuccess() should be true")
	}
}

func TestRunner_WithToolCalls(t *testing.T) {
	mock := &mockLLMClient{
		responses: []*output.ChatResponse{
			{
				ToolCalls: []valueobject.ToolCall{
					{
						ID:   "call_1",
						Type: "function",
						Function: valueobject.FunctionCall{
							Name:      "read_file",
							Arguments: `{"path":"/tmp/test.txt"}`,
						},
					},
				},
				PromptTokens:     20,
				CompletionTokens: 10,
				TotalTokens:      30,
				FinishReason:     "tool_calls",
			},
			{
				Content:          "File contains: hello world",
				PromptTokens:     30,
				CompletionTokens: 8,
				TotalTokens:      38,
			},
		},
	}

	registry := tool.NewRegistry()
	registry.Register(
		valueobject.ToolDefinition{
			Type: "function",
			Function: valueobject.FunctionDefinition{
				Name:        "read_file",
				Description: "Read a file",
				Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
			},
		},
		func(_ context.Context, args json.RawMessage) (string, error) {
			return `{"content":"hello world"}`, nil
		},
	)

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem)

	spec := entity.SubAgentSpec{
		Name:        "tool-agent",
		Instruction: "You read files.",
		MaxTurns:    5,
	}

	result := runner.Run(context.Background(), &spec, registry, "Read /tmp/test.txt", "parent-session")

	if result.Status != valueobject.SubAgentStatusCompleted {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCompleted)
	}
	if result.Output != "File contains: hello world" {
		t.Errorf("Output = %q, want %q", result.Output, "File contains: hello world")
	}
	if mock.callCount() != 2 {
		t.Errorf("LLM called %d times, want 2", mock.callCount())
	}
	if result.Usage.TotalTokens != 68 {
		t.Errorf("Usage.TotalTokens = %d, want 68 (30+38)", result.Usage.TotalTokens)
	}
}

func TestRunner_MultipleToolCalls(t *testing.T) {
	mock := &mockLLMClient{
		responses: []*output.ChatResponse{
			{
				ToolCalls: []valueobject.ToolCall{
					{ID: "call_1", Type: "function", Function: valueobject.FunctionCall{Name: "tool_a", Arguments: `{}`}},
					{ID: "call_2", Type: "function", Function: valueobject.FunctionCall{Name: "tool_b", Arguments: `{}`}},
				},
				FinishReason: "tool_calls",
			},
			{Content: "Both tools executed."},
		},
	}

	registry := tool.NewRegistry()
	for _, name := range []string{"tool_a", "tool_b"} {
		n := name
		registry.Register(
			valueobject.ToolDefinition{
				Type:     "function",
				Function: valueobject.FunctionDefinition{Name: n, Description: n, Parameters: json.RawMessage(`{}`)},
			},
			func(_ context.Context, _ json.RawMessage) (string, error) {
				return fmt.Sprintf(`{"tool":"%s"}`, n), nil
			},
		)
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem)

	spec := entity.SubAgentSpec{Name: "multi-tool", Instruction: "Use tools.", MaxTurns: 5}
	result := runner.Run(context.Background(), &spec, registry, "Do both", "parent")

	if result.Status != valueobject.SubAgentStatusCompleted {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCompleted)
	}
	if result.Output != "Both tools executed." {
		t.Errorf("Output = %q, want %q", result.Output, "Both tools executed.")
	}
}

func TestRunner_MaxTurnsExhausted(t *testing.T) {
	mock := &mockLLMClient{
		responses: []*output.ChatResponse{
			{ToolCalls: []valueobject.ToolCall{{ID: "c1", Type: "function", Function: valueobject.FunctionCall{Name: "t", Arguments: `{}`}}}, FinishReason: "tool_calls"},
			{ToolCalls: []valueobject.ToolCall{{ID: "c2", Type: "function", Function: valueobject.FunctionCall{Name: "t", Arguments: `{}`}}}, FinishReason: "tool_calls"},
			{ToolCalls: []valueobject.ToolCall{{ID: "c3", Type: "function", Function: valueobject.FunctionCall{Name: "t", Arguments: `{}`}}}, FinishReason: "tool_calls"},
		},
	}

	registry := tool.NewRegistry()
	registry.Register(
		valueobject.ToolDefinition{Type: "function", Function: valueobject.FunctionDefinition{Name: "t", Description: "t", Parameters: json.RawMessage(`{}`)}},
		func(_ context.Context, _ json.RawMessage) (string, error) { return `{}`, nil },
	)

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem)

	spec := entity.SubAgentSpec{Name: "looper", Instruction: "loop", MaxTurns: 3}
	result := runner.Run(context.Background(), &spec, registry, "loop forever", "parent")

	if result.Status != valueobject.SubAgentStatusMaxTurns {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusMaxTurns)
	}
	if mock.callCount() != 3 {
		t.Errorf("LLM called %d times, want 3 (maxTurns)", mock.callCount())
	}
}

func TestRunner_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mock := &mockLLMClient{
		responses: []*output.ChatResponse{
			{Content: "should not reach"},
		},
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem)

	spec := entity.SubAgentSpec{Name: "canceled-agent", Instruction: "test", MaxTurns: 5}
	result := runner.Run(ctx, &spec, nil, "task", "parent")

	if result.Status != valueobject.SubAgentStatusCanceled {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCanceled)
	}
	if result.Err == nil {
		t.Error("Err should be non-nil for canceled result")
	}
	if result.IsSuccess() {
		t.Error("IsSuccess() should be false for canceled result")
	}
}

func TestRunner_LLMError(t *testing.T) {
	mock := &mockLLMClient{
		errs: []error{errors.New("API rate limit exceeded")},
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem, WithRunnerRecovery(noRetryRecovery()))

	spec := entity.SubAgentSpec{Name: "error-agent", Instruction: "test", MaxTurns: 5}
	result := runner.Run(context.Background(), &spec, nil, "task", "parent")

	if result.Status != valueobject.SubAgentStatusFailed {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusFailed)
	}
	if result.Err == nil {
		t.Error("Err should be non-nil for failed result")
	}
	if result.IsSuccess() {
		t.Error("IsSuccess() should be false for failed result")
	}
}

func TestRunner_ToolExecutionError(t *testing.T) {
	mock := &mockLLMClient{
		responses: []*output.ChatResponse{
			{
				ToolCalls:    []valueobject.ToolCall{{ID: "c1", Type: "function", Function: valueobject.FunctionCall{Name: "bad_tool", Arguments: `{}`}}},
				FinishReason: "tool_calls",
			},
			{Content: "Tool failed, but I recovered."},
		},
	}

	registry := tool.NewRegistry()
	registry.Register(
		valueobject.ToolDefinition{Type: "function", Function: valueobject.FunctionDefinition{Name: "bad_tool", Description: "fails", Parameters: json.RawMessage(`{}`)}},
		func(_ context.Context, _ json.RawMessage) (string, error) {
			return "", fmt.Errorf("disk full")
		},
	)

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem)

	spec := entity.SubAgentSpec{Name: "resilient", Instruction: "handle errors", MaxTurns: 5}
	result := runner.Run(context.Background(), &spec, registry, "do something", "parent")

	if result.Status != valueobject.SubAgentStatusCompleted {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCompleted)
	}
	if result.Output != "Tool failed, but I recovered." {
		t.Errorf("Output = %q, want %q", result.Output, "Tool failed, but I recovered.")
	}
}

func TestRunner_ChildContextCleanup(t *testing.T) {
	mock := &mockLLMClient{
		responses: []*output.ChatResponse{
			{Content: "done"},
		},
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem)

	spec := entity.SubAgentSpec{Name: "cleanup-agent", Instruction: "test", MaxTurns: 5}
	_ = runner.Run(context.Background(), &spec, nil, "task", "parent-session")

	if mem.MessageCount("parent-session") != 0 {
		t.Error("parent session should not have subagent messages")
	}
}

func TestRunner_NoTools(t *testing.T) {
	mock := &mockLLMClient{
		responses: []*output.ChatResponse{
			{Content: "response without tools"},
		},
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem)

	spec := entity.SubAgentSpec{Name: "no-tools", Instruction: "answer questions", MaxTurns: 3}
	result := runner.Run(context.Background(), &spec, nil, "What is Go?", "parent")

	if result.Status != valueobject.SubAgentStatusCompleted {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCompleted)
	}
	if result.Output != "response without tools" {
		t.Errorf("Output = %q, want %q", result.Output, "response without tools")
	}
}

func TestRunner_SpecContextProvided(t *testing.T) {
	var capturedReq *output.ChatRequest
	mock := &mockLLMClient{
		responses: []*output.ChatResponse{
			{Content: "analyzed"},
		},
	}
	mock2 := &capturingMockClient{
		inner:      mock,
		onComplete: func(req *output.ChatRequest) { capturedReq = req },
	}

	mem := memory.NewConversationMemory()
	factory := func(_ valueobject.ModelTier) (output.LLMClient, string) {
		return mock2, "test-model"
	}
	memFactory := memory.NewSubAgentMemoryFactory(mem)
	runner := NewRunner(factory, memFactory)

	spec := entity.SubAgentSpec{
		Name:        "context-agent",
		Instruction: "You analyze data.",
		Context:     "Here is some context data.",
		MaxTurns:    5,
	}

	result := runner.Run(context.Background(), &spec, nil, "Analyze this", "parent")

	if result.Status != valueobject.SubAgentStatusCompleted {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCompleted)
	}

	if capturedReq == nil {
		t.Fatal("LLM was not called")
	}
	// Messages should contain: system (instruction) + context (user) + task (user)
	userMsgCount := 0
	for _, msg := range capturedReq.Messages {
		if msg.Role == valueobject.RoleUser {
			userMsgCount++
		}
	}
	if userMsgCount != 2 {
		t.Errorf("expected 2 user messages (context + task), got %d", userMsgCount)
	}
}

type capturingMockClient struct {
	inner      *mockLLMClient
	onComplete func(req *output.ChatRequest)
}

func (c *capturingMockClient) Complete(ctx context.Context, req *output.ChatRequest) (*output.ChatResponse, error) {
	if c.onComplete != nil {
		c.onComplete(req)
	}
	return c.inner.Complete(ctx, req)
}

func (c *capturingMockClient) CompleteStream(ctx context.Context, req *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
	return c.inner.CompleteStream(ctx, req)
}

// --- Recovery integration tests ---

func TestRunner_RetryOnTransientLLMError(t *testing.T) {
	transientErr := errors.New("rate limit exceeded")
	mock := &mockLLMClient{
		errs: []error{transientErr, transientErr, nil},
		responses: []*output.ChatResponse{
			nil, nil,
			{Content: "recovered after retries", PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem, WithRunnerRecovery(fastRetryRecovery()))

	spec := entity.SubAgentSpec{Name: "retry-agent", Instruction: "test", MaxTurns: 5}
	result := runner.Run(context.Background(), &spec, nil, "task", "parent")

	if result.Status != valueobject.SubAgentStatusCompleted {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCompleted)
	}
	if result.Output != "recovered after retries" {
		t.Errorf("Output = %q, want %q", result.Output, "recovered after retries")
	}
	if mock.callCount() != 3 {
		t.Errorf("LLM called %d times, want 3 (2 failures + 1 success)", mock.callCount())
	}
	if result.Usage.TotalTokens != 15 {
		t.Errorf("Usage.TotalTokens = %d, want 15", result.Usage.TotalTokens)
	}
}

func TestRunner_RecoveryTimeout(t *testing.T) {
	slowClient := &slowMockClient{
		delay: 200 * time.Millisecond,
		resp:  &output.ChatResponse{Content: "too slow"},
	}

	mem := memory.NewConversationMemory()
	cfg := RecoveryConfig{
		Timeout:       50 * time.Millisecond,
		MaxLLMRetries: 0,
		BaseDelay:     time.Millisecond,
		MaxDelay:      time.Millisecond,
	}
	runner := newTestRunner(slowClient, mem, WithRunnerRecovery(cfg))

	spec := entity.SubAgentSpec{Name: "timeout-agent", Instruction: "test", MaxTurns: 5, Timeout: 50 * time.Millisecond}
	result := runner.Run(context.Background(), &spec, nil, "task", "parent")

	if result.Status != valueobject.SubAgentStatusCanceled {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCanceled)
	}
	if result.Err == nil {
		t.Error("Err should be non-nil on timeout")
	}
}

func TestRunner_AllRetriesExhausted(t *testing.T) {
	persistentErr := errors.New("server error")
	mock := &mockLLMClient{
		errs: []error{persistentErr, persistentErr, persistentErr},
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem, WithRunnerRecovery(fastRetryRecovery()))

	spec := entity.SubAgentSpec{Name: "exhaust-agent", Instruction: "test", MaxTurns: 5}
	result := runner.Run(context.Background(), &spec, nil, "task", "parent")

	if result.Status != valueobject.SubAgentStatusFailed {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusFailed)
	}
	if result.Err == nil {
		t.Error("Err should be non-nil when retries exhausted")
	}
	if mock.callCount() != 3 {
		t.Errorf("LLM called %d times, want 3 (1 initial + 2 retries)", mock.callCount())
	}
}

func TestRunner_PartialResultOnFailure(t *testing.T) {
	persistentErr := errors.New("server crash")
	mock := &mockLLMClient{
		responses: []*output.ChatResponse{
			{
				ToolCalls:    []valueobject.ToolCall{{ID: "c1", Type: "function", Function: valueobject.FunctionCall{Name: "t", Arguments: `{}`}}},
				FinishReason: "tool_calls",
				TotalTokens:  20,
			},
		},
		errs: []error{nil, persistentErr},
	}

	registry := tool.NewRegistry()
	registry.Register(
		valueobject.ToolDefinition{Type: "function", Function: valueobject.FunctionDefinition{Name: "t", Description: "t", Parameters: json.RawMessage(`{}`)}},
		func(_ context.Context, _ json.RawMessage) (string, error) { return `{"ok":true}`, nil },
	)

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem, WithRunnerRecovery(noRetryRecovery()))

	spec := entity.SubAgentSpec{Name: "partial-agent", Instruction: "test", MaxTurns: 5}
	result := runner.Run(context.Background(), &spec, registry, "task", "parent")

	if result.Status != valueobject.SubAgentStatusFailed {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusFailed)
	}
	if result.Usage.TotalTokens != 20 {
		t.Errorf("Usage.TotalTokens = %d, want 20 (partial usage preserved)", result.Usage.TotalTokens)
	}
}

// slowMockClient simulates a slow LLM that respects context cancellation.
type slowMockClient struct {
	delay time.Duration
	resp  *output.ChatResponse
}

func (s *slowMockClient) Complete(ctx context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(s.delay):
		return s.resp, nil
	}
}

func (s *slowMockClient) CompleteStream(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

// --- Approval integration tests ---

type mockApprovalGate struct {
	mu        sync.Mutex
	decisions []ApprovalDecision
	errs      []error
	calls     int
}

func (m *mockApprovalGate) Review(_ context.Context, _ *valueobject.SubAgentResult) (ApprovalDecision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.calls
	m.calls++

	if idx < len(m.errs) && m.errs[idx] != nil {
		return ApprovalDecision{}, m.errs[idx]
	}
	if idx < len(m.decisions) {
		return m.decisions[idx], nil
	}
	return ApprovalDecision{Approved: true}, nil
}

func (m *mockApprovalGate) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestRunner_WithApproval_Approved(t *testing.T) {
	mock := &mockLLMClient{
		responses: []*output.ChatResponse{
			{Content: "approved output", TotalTokens: 10},
		},
	}
	gate := &mockApprovalGate{
		decisions: []ApprovalDecision{{Approved: true}},
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem, WithApproval(ApprovalConfig{
		Gate:         gate,
		MaxRevisions: 3,
	}))

	spec := entity.SubAgentSpec{Name: "approve-agent", Instruction: "test", MaxTurns: 5}
	result := runner.Run(context.Background(), &spec, nil, "task", "parent")

	if result.Status != valueobject.SubAgentStatusCompleted {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCompleted)
	}
	if result.Output != "approved output" {
		t.Errorf("Output = %q, want %q", result.Output, "approved output")
	}
	if gate.callCount() != 1 {
		t.Errorf("gate called %d times, want 1", gate.callCount())
	}
}

func TestRunner_WithApproval_Rejected_ThenApproved(t *testing.T) {
	mock := &mockLLMClient{
		responses: []*output.ChatResponse{
			{Content: "first draft", TotalTokens: 10},
			{Content: "revised output", TotalTokens: 15},
		},
	}
	gate := &mockApprovalGate{
		decisions: []ApprovalDecision{
			{Approved: false, Feedback: "needs more detail"},
			{Approved: true},
		},
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem, WithApproval(ApprovalConfig{
		Gate:         gate,
		MaxRevisions: 3,
	}))

	spec := entity.SubAgentSpec{Name: "revise-agent", Instruction: "test", MaxTurns: 5}
	result := runner.Run(context.Background(), &spec, nil, "task", "parent")

	if result.Status != valueobject.SubAgentStatusCompleted {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCompleted)
	}
	if result.Output != "revised output" {
		t.Errorf("Output = %q, want %q", result.Output, "revised output")
	}
	if gate.callCount() != 2 {
		t.Errorf("gate called %d times, want 2", gate.callCount())
	}
	if mock.callCount() != 2 {
		t.Errorf("LLM called %d times, want 2", mock.callCount())
	}
	if result.Usage.TotalTokens != 25 {
		t.Errorf("Usage.TotalTokens = %d, want 25 (cumulative)", result.Usage.TotalTokens)
	}
}

func TestRunner_WithApproval_MaxRevisions(t *testing.T) {
	mock := &mockLLMClient{
		responses: []*output.ChatResponse{
			{Content: "draft 1", TotalTokens: 5},
			{Content: "draft 2", TotalTokens: 5},
			{Content: "draft 3", TotalTokens: 5},
			{Content: "draft 4", TotalTokens: 5},
		},
	}
	gate := &mockApprovalGate{
		decisions: []ApprovalDecision{
			{Approved: false, Feedback: "try again"},
			{Approved: false, Feedback: "still wrong"},
			{Approved: false, Feedback: "nope"},
			{Approved: false, Feedback: "give up"},
		},
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem, WithApproval(ApprovalConfig{
		Gate:         gate,
		MaxRevisions: 2,
	}))

	spec := entity.SubAgentSpec{Name: "stubborn-agent", Instruction: "test", MaxTurns: 5}
	result := runner.Run(context.Background(), &spec, nil, "task", "parent")

	if result.Status != valueobject.SubAgentStatusFailed {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusFailed)
	}
	if result.Err == nil {
		t.Error("Err should be non-nil when max revisions exhausted")
	}
	// 1 initial + 2 revisions = 3 runs, 3 gate calls
	if gate.callCount() != 3 {
		t.Errorf("gate called %d times, want 3", gate.callCount())
	}
	if mock.callCount() != 3 {
		t.Errorf("LLM called %d times, want 3", mock.callCount())
	}
}

func TestRunner_WithApproval_Canceled(t *testing.T) {
	mock := &mockLLMClient{
		responses: []*output.ChatResponse{
			{Content: "output to cancel", TotalTokens: 10},
		},
	}
	gate := &mockApprovalGate{
		decisions: []ApprovalDecision{{Canceled: true}},
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem, WithApproval(ApprovalConfig{
		Gate:         gate,
		MaxRevisions: 3,
	}))

	spec := entity.SubAgentSpec{Name: "cancel-agent", Instruction: "test", MaxTurns: 5}
	result := runner.Run(context.Background(), &spec, nil, "task", "parent")

	if result.Status != valueobject.SubAgentStatusCanceled {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCanceled)
	}
	if result.Err == nil {
		t.Error("Err should be non-nil for canceled result")
	}
}

func TestRunner_WithApproval_GateError(t *testing.T) {
	mock := &mockLLMClient{
		responses: []*output.ChatResponse{
			{Content: "some output", TotalTokens: 10},
		},
	}
	gate := &mockApprovalGate{
		errs: []error{fmt.Errorf("gate connection failed")},
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem, WithApproval(ApprovalConfig{
		Gate:         gate,
		MaxRevisions: 3,
	}))

	spec := entity.SubAgentSpec{Name: "gate-err-agent", Instruction: "test", MaxTurns: 5}
	result := runner.Run(context.Background(), &spec, nil, "task", "parent")

	if result.Status != valueobject.SubAgentStatusFailed {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusFailed)
	}
	if result.Err == nil {
		t.Error("Err should be non-nil on gate error")
	}
}

// --- Progress reporter integration tests ---

// collectingReporter captures all ToolEvents for test assertions.
type collectingReporter struct {
	mu     sync.Mutex
	events []valueobject.ToolEvent
}

func (c *collectingReporter) Report(event valueobject.ToolEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

func (c *collectingReporter) Events() []valueobject.ToolEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := make([]valueobject.ToolEvent, len(c.events))
	copy(cp, c.events)
	return cp
}

func TestRunner_Progress_SimpleCompletion(t *testing.T) {
	mock := &mockLLMClient{
		responses: []*output.ChatResponse{
			{Content: "done", TotalTokens: 5},
		},
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem)

	reporter := &collectingReporter{}
	ctx := WithProgressReporter(context.Background(), reporter)

	spec := entity.SubAgentSpec{Name: "prog-agent", Instruction: "test", MaxTurns: 5}
	result := runner.Run(ctx, &spec, nil, "task", "parent")

	if result.Status != valueobject.SubAgentStatusCompleted {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCompleted)
	}

	events := reporter.Events()
	if len(events) < 3 {
		t.Fatalf("expected at least 3 events (started, thinking, completed), got %d: %+v", len(events), events)
	}

	if events[0].Kind != valueobject.ToolEventSubAgentStarted {
		t.Errorf("events[0].Kind = %q, want %q", events[0].Kind, valueobject.ToolEventSubAgentStarted)
	}
	if events[0].ToolName != "prog-agent" {
		t.Errorf("events[0].ToolName = %q, want %q", events[0].ToolName, "prog-agent")
	}

	if events[1].Kind != valueobject.ToolEventSubAgentThinking {
		t.Errorf("events[1].Kind = %q, want %q", events[1].Kind, valueobject.ToolEventSubAgentThinking)
	}

	last := events[len(events)-1]
	if last.Kind != valueobject.ToolEventSubAgentCompleted {
		t.Errorf("last event Kind = %q, want %q", last.Kind, valueobject.ToolEventSubAgentCompleted)
	}
}

func TestRunner_Progress_WithToolCalls(t *testing.T) {
	mock := &mockLLMClient{
		responses: []*output.ChatResponse{
			{
				ToolCalls: []valueobject.ToolCall{
					{ID: "c1", Type: "function", Function: valueobject.FunctionCall{Name: "read_file", Arguments: `{}`}},
					{ID: "c2", Type: "function", Function: valueobject.FunctionCall{Name: "write_file", Arguments: `{}`}},
				},
				FinishReason: "tool_calls",
			},
			{Content: "done with tools", TotalTokens: 10},
		},
	}

	registry := tool.NewRegistry()
	for _, name := range []string{"read_file", "write_file"} {
		n := name
		registry.Register(
			valueobject.ToolDefinition{Type: "function", Function: valueobject.FunctionDefinition{Name: n, Description: n, Parameters: json.RawMessage(`{}`)}},
			func(_ context.Context, _ json.RawMessage) (string, error) { return `{"ok":true}`, nil },
		)
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem)

	reporter := &collectingReporter{}
	ctx := WithProgressReporter(context.Background(), reporter)

	spec := entity.SubAgentSpec{Name: "tool-prog", Instruction: "test", MaxTurns: 5}
	result := runner.Run(ctx, &spec, registry, "do tools", "parent")

	if result.Status != valueobject.SubAgentStatusCompleted {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCompleted)
	}

	events := reporter.Events()

	var toolCallEvents []valueobject.ToolEvent
	for _, e := range events {
		if e.Kind == valueobject.ToolEventSubAgentToolCall {
			toolCallEvents = append(toolCallEvents, e)
		}
	}
	if len(toolCallEvents) != 2 {
		t.Errorf("expected 2 tool_call events, got %d", len(toolCallEvents))
	}
	if len(toolCallEvents) > 0 && toolCallEvents[0].Detail != "read_file" {
		t.Errorf("first tool_call Detail = %q, want %q", toolCallEvents[0].Detail, "read_file")
	}
	if len(toolCallEvents) > 1 && toolCallEvents[1].Detail != "write_file" {
		t.Errorf("second tool_call Detail = %q, want %q", toolCallEvents[1].Detail, "write_file")
	}
}

func TestRunner_Progress_CanceledContext(t *testing.T) {
	slowClient := &slowMockClient{
		delay: 200 * time.Millisecond,
		resp:  &output.ChatResponse{Content: "too slow"},
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(slowClient, mem, WithRunnerRecovery(RecoveryConfig{
		Timeout:       50 * time.Millisecond,
		MaxLLMRetries: 0,
		BaseDelay:     time.Millisecond,
		MaxDelay:      time.Millisecond,
	}))

	reporter := &collectingReporter{}
	ctx := WithProgressReporter(context.Background(), reporter)

	spec := entity.SubAgentSpec{Name: "cancel-prog", Instruction: "test", MaxTurns: 5, Timeout: 50 * time.Millisecond}
	result := runner.Run(ctx, &spec, nil, "task", "parent")

	if result.Status != valueobject.SubAgentStatusCanceled {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCanceled)
	}

	events := reporter.Events()
	if len(events) == 0 {
		t.Fatal("expected at least one progress event")
	}

	if events[0].Kind != valueobject.ToolEventSubAgentStarted {
		t.Errorf("events[0].Kind = %q, want %q", events[0].Kind, valueobject.ToolEventSubAgentStarted)
	}
}

func TestRunner_Progress_NilReporter_NoPanic(t *testing.T) {
	mock := &mockLLMClient{
		responses: []*output.ChatResponse{{Content: "no reporter"}},
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem)

	spec := entity.SubAgentSpec{Name: "nil-reporter", Instruction: "test", MaxTurns: 5}
	result := runner.Run(context.Background(), &spec, nil, "task", "parent")

	if result.Status != valueobject.SubAgentStatusCompleted {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCompleted)
	}
}

func TestRunner_WithApproval_NonSuccessSkipsGate(t *testing.T) {
	mock := &mockLLMClient{
		errs: []error{errors.New("LLM exploded")},
	}
	gate := &mockApprovalGate{
		decisions: []ApprovalDecision{{Approved: true}},
	}

	mem := memory.NewConversationMemory()
	runner := newTestRunner(mock, mem,
		WithRunnerRecovery(noRetryRecovery()),
		WithApproval(ApprovalConfig{
			Gate:         gate,
			MaxRevisions: 3,
		}),
	)

	spec := entity.SubAgentSpec{Name: "fail-agent", Instruction: "test", MaxTurns: 5}
	result := runner.Run(context.Background(), &spec, nil, "task", "parent")

	if result.Status != valueobject.SubAgentStatusFailed {
		t.Fatalf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusFailed)
	}
	if gate.callCount() != 0 {
		t.Errorf("gate called %d times, want 0 (should not be called on failure)", gate.callCount())
	}
}
