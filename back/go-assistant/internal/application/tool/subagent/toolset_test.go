package subagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/tool"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

type stubSubAgentService struct {
	specs     []entity.SubAgentSpec
	result    valueobject.SubAgentResult
	err       error
	teamEval  valueobject.TeamEvaluation
	teamErr   error
	createErr error
	created   []*entity.SubAgentSpec
}

func (s *stubSubAgentService) Spawn(_ context.Context, _ string, _ *entity.SubAgentSpec, _ string) (valueobject.SubAgentResult, error) {
	return s.result, s.err
}

func (s *stubSubAgentService) SpawnParallel(_ context.Context, _ string, _ []*entity.SubAgentSpec, _ []string) ([]valueobject.SubAgentResult, error) {
	return nil, nil
}

func (s *stubSubAgentService) ListAgents() []entity.SubAgentSpec {
	return s.specs
}

func (s *stubSubAgentService) GetAgent(name string) (entity.SubAgentSpec, bool) {
	for _, spec := range s.specs {
		if spec.Name == name {
			return spec, true
		}
	}
	return entity.SubAgentSpec{}, false
}

func (s *stubSubAgentService) EvaluateTeam(_ context.Context, _ string) (valueobject.TeamEvaluation, error) {
	return s.teamEval, s.teamErr
}

func (s *stubSubAgentService) CreateAgent(_ context.Context, spec *entity.SubAgentSpec) error {
	s.created = append(s.created, spec)
	return s.createErr
}

func TestRegisterSubAgentTools(t *testing.T) {
	svc := &stubSubAgentService{}
	registry := tool.NewRegistry()
	RegisterSubAgentTools(registry, svc, func() string { return "session-1" })

	defs := registry.Definitions()
	if len(defs) != 4 {
		t.Fatalf("expected 4 tools registered, got %d", len(defs))
	}

	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	for _, want := range []string{"spawn_subagent", "list_subagents", "evaluate_team", "create_subagent"} {
		if !names[want] {
			t.Errorf("%s not registered", want)
		}
	}
}

func TestSpawnSubagentTool_Success(t *testing.T) {
	svc := &stubSubAgentService{
		specs: []entity.SubAgentSpec{
			{Name: "code-reviewer", Description: "Reviews code", Instruction: "Review code."},
		},
		result: valueobject.SubAgentResult{
			AgentName: "code-reviewer",
			Output:    "Code looks good!",
			Status:    valueobject.SubAgentStatusCompleted,
			Usage:     valueobject.SubAgentUsage{TotalTokens: 42},
		},
	}

	registry := tool.NewRegistry()
	RegisterSubAgentTools(registry, svc, func() string { return "parent-session" })

	args := `{"agent_name":"code-reviewer","task":"Review my code"}`
	result, err := registry.Execute(context.Background(), "spawn_subagent", json.RawMessage(args))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var sr spawnResult
	if err := json.Unmarshal([]byte(result), &sr); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if sr.AgentName != "code-reviewer" {
		t.Errorf("AgentName = %q, want %q", sr.AgentName, "code-reviewer")
	}
	if sr.Output != "Code looks good!" {
		t.Errorf("Output = %q, want %q", sr.Output, "Code looks good!")
	}
	if sr.Status != "completed" {
		t.Errorf("Status = %q, want %q", sr.Status, "completed")
	}
	if sr.Tokens != 42 {
		t.Errorf("Tokens = %d, want 42", sr.Tokens)
	}
}

func TestSpawnSubagentTool_UnknownAgent(t *testing.T) {
	svc := &stubSubAgentService{
		specs: []entity.SubAgentSpec{
			{Name: "known-agent", Instruction: "test"},
		},
	}

	registry := tool.NewRegistry()
	RegisterSubAgentTools(registry, svc, func() string { return "sess" })

	args := `{"agent_name":"nonexistent","task":"do something"}`
	result, err := registry.Execute(context.Background(), "spawn_subagent", json.RawMessage(args))
	if err != nil {
		t.Fatalf("Execute() should return structured error, not error: %v", err)
	}

	if !strings.Contains(result, "nonexistent") {
		t.Errorf("result should mention unknown agent name, got: %s", result)
	}
	if !strings.Contains(result, "known-agent") {
		t.Errorf("result should list available agents, got: %s", result)
	}
}

func TestSpawnSubagentTool_MissingAgentName(t *testing.T) {
	svc := &stubSubAgentService{}
	registry := tool.NewRegistry()
	RegisterSubAgentTools(registry, svc, nil)

	args := `{"task":"do something"}`
	_, err := registry.Execute(context.Background(), "spawn_subagent", json.RawMessage(args))
	if err == nil {
		t.Fatal("expected error for missing agent_name")
	}
}

func TestSpawnSubagentTool_MissingTask(t *testing.T) {
	svc := &stubSubAgentService{}
	registry := tool.NewRegistry()
	RegisterSubAgentTools(registry, svc, nil)

	args := `{"agent_name":"something"}`
	_, err := registry.Execute(context.Background(), "spawn_subagent", json.RawMessage(args))
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

func TestSpawnSubagentTool_WithContext(t *testing.T) {
	var capturedSpec entity.SubAgentSpec
	svc := &stubSubAgentService{
		specs: []entity.SubAgentSpec{
			{Name: "analyst", Instruction: "Analyze data."},
		},
		result: valueobject.SubAgentResult{
			AgentName: "analyst",
			Output:    "Analysis complete.",
			Status:    valueobject.SubAgentStatusCompleted,
		},
	}

	capturingSvc := &capturingSubAgentService{
		inner:   svc,
		onSpawn: func(spec entity.SubAgentSpec) { capturedSpec = spec },
	}

	registry := tool.NewRegistry()
	RegisterSubAgentTools(registry, capturingSvc, func() string { return "sess" })

	args := `{"agent_name":"analyst","task":"analyze","context":"extra context data"}`
	_, err := registry.Execute(context.Background(), "spawn_subagent", json.RawMessage(args))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if capturedSpec.Context != "extra context data" {
		t.Errorf("spec.Context = %q, want %q", capturedSpec.Context, "extra context data")
	}
}

func TestSpawnSubagentTool_InvalidJSON(t *testing.T) {
	svc := &stubSubAgentService{}
	registry := tool.NewRegistry()
	RegisterSubAgentTools(registry, svc, nil)

	_, err := registry.Execute(context.Background(), "spawn_subagent", json.RawMessage(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestListSubagentsTool_Empty(t *testing.T) {
	svc := &stubSubAgentService{specs: nil}
	registry := tool.NewRegistry()
	RegisterSubAgentTools(registry, svc, nil)

	result, err := registry.Execute(context.Background(), "list_subagents", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var lr listResult
	if err := json.Unmarshal([]byte(result), &lr); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if lr.Count != 0 {
		t.Errorf("Count = %d, want 0", lr.Count)
	}
	if len(lr.Agents) != 0 {
		t.Errorf("Agents = %d, want 0", len(lr.Agents))
	}
}

func TestListSubagentsTool_WithAgents(t *testing.T) {
	svc := &stubSubAgentService{
		specs: []entity.SubAgentSpec{
			{Name: "code-reviewer", Description: "Reviews code", Instruction: "review", ModelTier: valueobject.ModelTierFast, MaxTurns: 5},
			{Name: "researcher", Description: "Researches topics", Instruction: "research", ModelTier: valueobject.ModelTierBalanced},
		},
	}

	registry := tool.NewRegistry()
	RegisterSubAgentTools(registry, svc, nil)

	result, err := registry.Execute(context.Background(), "list_subagents", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var lr listResult
	if err := json.Unmarshal([]byte(result), &lr); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if lr.Count != 2 {
		t.Fatalf("Count = %d, want 2", lr.Count)
	}

	if lr.Agents[0].Name != "code-reviewer" {
		t.Errorf("Agents[0].Name = %q, want %q", lr.Agents[0].Name, "code-reviewer")
	}
	if lr.Agents[0].MaxTurns != 5 {
		t.Errorf("Agents[0].MaxTurns = %d, want 5", lr.Agents[0].MaxTurns)
	}
	if lr.Agents[1].MaxTurns != 10 {
		t.Errorf("Agents[1].MaxTurns = %d, want 10 (default)", lr.Agents[1].MaxTurns)
	}
}

type capturingSubAgentService struct {
	inner   *stubSubAgentService
	onSpawn func(spec entity.SubAgentSpec)
}

func (c *capturingSubAgentService) Spawn(ctx context.Context, parentSessionID string, spec *entity.SubAgentSpec, task string) (valueobject.SubAgentResult, error) {
	if c.onSpawn != nil {
		c.onSpawn(*spec)
	}
	return c.inner.Spawn(ctx, parentSessionID, spec, task)
}

func (c *capturingSubAgentService) SpawnParallel(ctx context.Context, parentSessionID string, specs []*entity.SubAgentSpec, tasks []string) ([]valueobject.SubAgentResult, error) {
	return c.inner.SpawnParallel(ctx, parentSessionID, specs, tasks)
}

func (c *capturingSubAgentService) ListAgents() []entity.SubAgentSpec {
	return c.inner.ListAgents()
}

func (c *capturingSubAgentService) GetAgent(name string) (entity.SubAgentSpec, bool) {
	return c.inner.GetAgent(name)
}

func (c *capturingSubAgentService) EvaluateTeam(ctx context.Context, task string) (valueobject.TeamEvaluation, error) {
	return c.inner.EvaluateTeam(ctx, task)
}

func (c *capturingSubAgentService) CreateAgent(ctx context.Context, spec *entity.SubAgentSpec) error {
	return c.inner.CreateAgent(ctx, spec)
}
