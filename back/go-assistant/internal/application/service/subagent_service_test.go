package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/memory"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/subagent"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

type mockClient struct {
	mu        sync.Mutex
	calls     int
	responses []*output.ChatResponse
	errs      []error
}

func (m *mockClient) Complete(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
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

func (m *mockClient) CompleteStream(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

func newTestSubagentService(mock *mockClient, specs []entity.SubAgentSpec) (*subagentService, *tool.Registry) {
	mem := memory.NewConversationMemory()
	memFactory := memory.NewSubAgentMemoryFactory(mem)

	clientFactory := func(_ valueobject.ModelTier) (output.LLMClient, string) {
		return mock, "test-model"
	}

	router := subagent.NewRouter()
	for i := range specs {
		if err := router.Register(&specs[i]); err != nil {
			panic("unexpected Register error in test helper: " + err.Error())
		}
	}

	runner := subagent.NewRunner(clientFactory, memFactory)

	registry := tool.NewRegistry()
	registry.Register(
		valueobject.ToolDefinition{
			Type:     "function",
			Function: valueobject.FunctionDefinition{Name: "read_file", Description: "Read file", Parameters: json.RawMessage(`{}`)},
		},
		func(_ context.Context, _ json.RawMessage) (string, error) { return `{"ok":true}`, nil },
	)
	registry.Register(
		valueobject.ToolDefinition{
			Type:     "function",
			Function: valueobject.FunctionDefinition{Name: "write_file", Description: "Write file", Parameters: json.RawMessage(`{}`)},
		},
		func(_ context.Context, _ json.RawMessage) (string, error) { return `{"ok":true}`, nil },
	)
	registry.Register(
		valueobject.ToolDefinition{
			Type:     "function",
			Function: valueobject.FunctionDefinition{Name: "shell_exec", Description: "Run command", Parameters: json.RawMessage(`{}`)},
		},
		func(_ context.Context, _ json.RawMessage) (string, error) { return `{"ok":true}`, nil },
	)

	svc := NewSubAgentService(router, runner, registry).(*subagentService)
	return svc, registry
}

func TestSubagentService_Spawn_Success(t *testing.T) {
	mock := &mockClient{
		responses: []*output.ChatResponse{
			{Content: "Code reviewed successfully.", PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
	}

	spec := entity.SubAgentSpec{
		Name:        "code-reviewer",
		Instruction: "You review code.",
		MaxTurns:    5,
	}
	svc, _ := newTestSubagentService(mock, []entity.SubAgentSpec{spec})

	result, err := svc.Spawn(context.Background(), "parent-session", &spec, "Review this code")
	if err != nil {
		t.Fatalf("Spawn() error = %v", err)
	}

	if result.Status != valueobject.SubAgentStatusCompleted {
		t.Errorf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCompleted)
	}
	if result.Output != "Code reviewed successfully." {
		t.Errorf("Output = %q, want %q", result.Output, "Code reviewed successfully.")
	}
	if result.AgentName != "code-reviewer" {
		t.Errorf("AgentName = %q, want %q", result.AgentName, "code-reviewer")
	}
}

func TestSubagentService_Spawn_InvalidSpec(t *testing.T) {
	mock := &mockClient{
		responses: []*output.ChatResponse{{Content: "ok"}},
	}
	svc, _ := newTestSubagentService(mock, nil)

	spec := entity.SubAgentSpec{Name: "", Instruction: "test"}
	_, err := svc.Spawn(context.Background(), "parent", &spec, "task")
	if err == nil {
		t.Fatal("Spawn() expected error for invalid spec, got nil")
	}
}

func TestSubagentService_Spawn_WithToolRestrictions(t *testing.T) {
	tests := []struct {
		name         string
		allowedTools []string
		deniedTools  []string
		toolCall     string
		wantOutput   string
	}{
		{
			name:         "allowed tools restrict available tools",
			allowedTools: []string{"read_file"},
			deniedTools:  nil,
			toolCall:     "read_file",
			wantOutput:   "Used read_file only.",
		},
		{
			name:         "denied tools hide specific tools",
			allowedTools: nil,
			deniedTools:  []string{"shell_exec"},
			toolCall:     "read_file",
			wantOutput:   "Avoided shell_exec.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockClient{
				responses: []*output.ChatResponse{
					{
						ToolCalls: []valueobject.ToolCall{
							{ID: "c1", Type: "function", Function: valueobject.FunctionCall{Name: tt.toolCall, Arguments: `{}`}},
						},
						FinishReason: "tool_calls",
					},
					{Content: tt.wantOutput},
				},
			}

			spec := entity.SubAgentSpec{
				Name:         "scoped-agent",
				Instruction:  "test",
				AllowedTools: tt.allowedTools,
				DeniedTools:  tt.deniedTools,
				MaxTurns:     5,
			}
			svc, _ := newTestSubagentService(mock, []entity.SubAgentSpec{spec})

			result, err := svc.Spawn(context.Background(), "parent", &spec, "do it")
			if err != nil {
				t.Fatalf("Spawn() error = %v", err)
			}
			if result.Status != valueobject.SubAgentStatusCompleted {
				t.Errorf("Status = %q, want %q", result.Status, valueobject.SubAgentStatusCompleted)
			}
			if result.Output != tt.wantOutput {
				t.Errorf("Output = %q, want %q", result.Output, tt.wantOutput)
			}
		})
	}
}

func TestSubagentService_SpawnParallel(t *testing.T) {
	callCount := 0
	var mu sync.Mutex

	mock := &mockClient{}
	mock.responses = nil
	mock.errs = nil

	mem := memory.NewConversationMemory()
	memFactory := memory.NewSubAgentMemoryFactory(mem)

	clientFactory := func(_ valueobject.ModelTier) (output.LLMClient, string) {
		return &countingClient{mu: &mu, count: &callCount}, "test-model"
	}

	specValues := []entity.SubAgentSpec{
		{Name: "agent-a", Instruction: "a", MaxTurns: 3},
		{Name: "agent-b", Instruction: "b", MaxTurns: 3},
		{Name: "agent-c", Instruction: "c", MaxTurns: 3},
	}

	router := subagent.NewRouter()
	for i := range specValues {
		if err := router.Register(&specValues[i]); err != nil {
			t.Fatalf("Register() unexpected error: %v", err)
		}
	}
	runner := subagent.NewRunner(clientFactory, memFactory)
	registry := tool.NewRegistry()
	svc := NewSubAgentService(router, runner, registry)

	specs := make([]*entity.SubAgentSpec, len(specValues))
	for i := range specValues {
		specs[i] = &specValues[i]
	}
	tasks := []string{"task a", "task b", "task c"}
	results, err := svc.SpawnParallel(context.Background(), "parent", specs, tasks)
	if err != nil {
		t.Fatalf("SpawnParallel() error = %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("SpawnParallel() returned %d results, want 3", len(results))
	}

	for i, r := range results {
		if r.Status != valueobject.SubAgentStatusCompleted {
			t.Errorf("results[%d].Status = %q, want %q", i, r.Status, valueobject.SubAgentStatusCompleted)
		}
		if r.AgentName != specs[i].Name {
			t.Errorf("results[%d].AgentName = %q, want %q", i, r.AgentName, specs[i].Name)
		}
	}

	mu.Lock()
	if callCount != 3 {
		t.Errorf("LLM called %d times, want 3 (once per agent)", callCount)
	}
	mu.Unlock()
}

func TestSubagentService_SpawnParallel_MismatchedLengths(t *testing.T) {
	mock := &mockClient{}
	svc, _ := newTestSubagentService(mock, nil)

	specs := []*entity.SubAgentSpec{{Name: "a", Instruction: "a"}}
	tasks := []string{"task a", "task b"}

	_, err := svc.SpawnParallel(context.Background(), "parent", specs, tasks)
	if err == nil {
		t.Fatal("SpawnParallel() expected error for mismatched lengths, got nil")
	}
}

func TestSubagentService_ListAgents(t *testing.T) {
	mock := &mockClient{}
	specs := []entity.SubAgentSpec{
		{Name: "zeta", Instruction: "z"},
		{Name: "alpha", Instruction: "a"},
	}
	svc, _ := newTestSubagentService(mock, specs)

	list := svc.ListAgents()
	if len(list) != 2 {
		t.Fatalf("ListAgents() returned %d, want 2", len(list))
	}
	if list[0].Name != "alpha" {
		t.Errorf("ListAgents()[0].Name = %q, want %q", list[0].Name, "alpha")
	}
	if list[1].Name != "zeta" {
		t.Errorf("ListAgents()[1].Name = %q, want %q", list[1].Name, "zeta")
	}
}

func TestSubagentService_GetAgent(t *testing.T) {
	mock := &mockClient{}
	specs := []entity.SubAgentSpec{
		{Name: "researcher", Instruction: "research", Description: "Research agent"},
	}
	svc, _ := newTestSubagentService(mock, specs)

	spec, ok := svc.GetAgent("researcher")
	if !ok {
		t.Fatal("GetAgent(researcher) returned false")
	}
	if spec.Description != "Research agent" {
		t.Errorf("Description = %q, want %q", spec.Description, "Research agent")
	}

	_, ok = svc.GetAgent("nonexistent")
	if ok {
		t.Error("GetAgent(nonexistent) should return false")
	}
}

type countingClient struct {
	mu    *sync.Mutex
	count *int
}

func (c *countingClient) Complete(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
	c.mu.Lock()
	*c.count++
	c.mu.Unlock()
	return &output.ChatResponse{Content: "done"}, nil
}

func (c *countingClient) CompleteStream(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestSubagentService_SpawnParallel_MixedValidInvalid(t *testing.T) {
	var mu sync.Mutex
	callCount := 0

	mem := memory.NewConversationMemory()
	memFactory := memory.NewSubAgentMemoryFactory(mem)

	clientFactory := func(_ valueobject.ModelTier) (output.LLMClient, string) {
		return &countingClient{mu: &mu, count: &callCount}, "test-model"
	}

	validSpecs := []entity.SubAgentSpec{
		{Name: "agent-ok-1", Instruction: "valid a", MaxTurns: 3},
		{Name: "agent-ok-2", Instruction: "valid b", MaxTurns: 3},
	}

	router := subagent.NewRouter()
	for i := range validSpecs {
		if err := router.Register(&validSpecs[i]); err != nil {
			t.Fatalf("Register() unexpected error: %v", err)
		}
	}
	runner := subagent.NewRunner(clientFactory, memFactory)
	registry := tool.NewRegistry()
	svc := NewSubAgentService(router, runner, registry)

	invalidSpec := entity.SubAgentSpec{Name: "", Instruction: "invalid"}
	specs := []*entity.SubAgentSpec{&validSpecs[0], &invalidSpec, &validSpecs[1]}
	tasks := []string{"task a", "task b", "task c"}

	results, err := svc.SpawnParallel(context.Background(), "parent", specs, tasks)
	if err != nil {
		t.Fatalf("SpawnParallel() error = %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("SpawnParallel() returned %d results, want 3", len(results))
	}

	if results[0].Status != valueobject.SubAgentStatusCompleted {
		t.Errorf("results[0].Status = %q, want %q", results[0].Status, valueobject.SubAgentStatusCompleted)
	}

	if results[1].Status != valueobject.SubAgentStatusFailed {
		t.Errorf("results[1].Status = %q, want %q", results[1].Status, valueobject.SubAgentStatusFailed)
	}
	if results[1].Err == nil {
		t.Error("results[1].Err should be non-nil for invalid spec")
	}

	if results[2].Status != valueobject.SubAgentStatusCompleted {
		t.Errorf("results[2].Status = %q, want %q", results[2].Status, valueobject.SubAgentStatusCompleted)
	}

	mu.Lock()
	if callCount != 2 {
		t.Errorf("LLM called %d times, want 2 (only valid agents)", callCount)
	}
	mu.Unlock()
}
