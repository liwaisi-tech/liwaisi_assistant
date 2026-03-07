package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/planner"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// --- LLM mocks for planner service tests ---

type plannerNullLLM struct{ t *testing.T }

func (m *plannerNullLLM) Complete(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
	m.t.Fatal("decomposer LLM must NOT be called on a simple (gate-bypass) task")
	return nil, nil
}

func (m *plannerNullLLM) CompleteStream(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

type plannerFakeLLM struct {
	responses []string
	calls     int
}

func (m *plannerFakeLLM) Complete(_ context.Context, _ *output.ChatRequest) (*output.ChatResponse, error) {
	idx := m.calls
	m.calls++
	if idx < len(m.responses) {
		return &output.ChatResponse{Content: m.responses[idx]}, nil
	}
	return &output.ChatResponse{Content: "[]"}, nil
}

func (m *plannerFakeLLM) CompleteStream(_ context.Context, _ *output.ChatRequest) (<-chan valueobject.StreamChunk, error) {
	return nil, fmt.Errorf("not implemented")
}

// --- SubAgentService mock for planner service tests ---

type plannerMockSubAgent struct {
	output string
}

func (m *plannerMockSubAgent) Spawn(_ context.Context, _ string, spec *entity.SubAgentSpec, _ string) (valueobject.SubAgentResult, error) {
	return valueobject.SubAgentResult{AgentName: spec.Name, Output: m.output, Status: valueobject.SubAgentStatusCompleted}, nil
}
func (m *plannerMockSubAgent) SpawnParallel(_ context.Context, _ string, _ []*entity.SubAgentSpec, _ []string) ([]valueobject.SubAgentResult, error) {
	return nil, nil
}
func (m *plannerMockSubAgent) ListAgents() []entity.SubAgentSpec { return nil }
func (m *plannerMockSubAgent) GetAgent(_ string) (entity.SubAgentSpec, bool) {
	return entity.SubAgentSpec{}, false
}
func (m *plannerMockSubAgent) EvaluateTeam(_ context.Context, _ string) (valueobject.TeamEvaluation, error) {
	return valueobject.TeamEvaluation{}, nil
}
func (m *plannerMockSubAgent) CreateAgent(_ context.Context, _ *entity.SubAgentSpec) error {
	return nil
}

// --- tests ---

func TestPlannerService_Plan_GateBypass(t *testing.T) {
	gate := planner.NewPlanGate()
	// NullLLM panics if Complete is called — proves gate bypass skip the LLM.
	decomp := planner.NewDecomposer(&plannerNullLLM{t: t}, "noop")
	svc := NewPlannerService(gate, decomp, nil)

	graph, err := svc.Plan(context.Background(), "say hi")
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(graph.Tasks) != 1 {
		t.Errorf("Tasks len = %d, want 1 (bypass path)", len(graph.Tasks))
	}
	if graph.Tasks[0].ID != "planner-shortcut" {
		t.Errorf("Tasks[0].ID = %q, want \"planner-shortcut\"", graph.Tasks[0].ID)
	}
}

func TestPlannerService_Plan_Decompose(t *testing.T) {
	gate := planner.NewPlanGate()
	fakeLLM := &plannerFakeLLM{
		responses: []string{`[{"id":"t1","description":"do t1","instruction":"inst"}]`},
	}
	decomp := planner.NewDecomposer(fakeLLM, "test-model")
	svc := NewPlannerService(gate, decomp, nil)

	// Long complex task → decomposer
	graph, err := svc.Plan(context.Background(), "research the best Go concurrency patterns and synthesize them")
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(graph.Tasks) != 1 || graph.Tasks[0].ID != "t1" {
		t.Errorf("Tasks = %v, want [t1]", graph.Tasks)
	}
}

func TestPlannerService_PlanAndExecute_SimpleTask(t *testing.T) {
	gate := planner.NewPlanGate()
	decomp := planner.NewDecomposer(&plannerNullLLM{t: t}, "noop")
	subSvc := &plannerMockSubAgent{output: "done"}
	svc := NewPlannerService(gate, decomp, subSvc)

	result, err := svc.PlanAndExecute(context.Background(), "session-1", "say hello")
	if err != nil {
		t.Fatalf("PlanAndExecute() error = %v", err)
	}
	if result.Status != valueobject.PlanStatusCompleted {
		t.Errorf("Status = %q, want completed", result.Status)
	}
}

func TestPlannerService_Execute_UsesScheduler(t *testing.T) {
	gate := planner.NewPlanGate()
	decomp := planner.NewDecomposer(&plannerNullLLM{t: t}, "noop")
	subSvc := &plannerMockSubAgent{output: "executed"}
	svc := NewPlannerService(gate, decomp, subSvc)

	graph := &entity.PlanGraph{
		Tasks: []*entity.MicroTask{
			{ID: "x", Description: "do x", Spec: entity.SubAgentSpec{Name: "x", Instruction: "inst"}},
		},
	}

	result, err := svc.Execute(context.Background(), "session-2", graph)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if result.Status != valueobject.PlanStatusCompleted {
		t.Errorf("Status = %q, want completed", result.Status)
	}
	if len(result.Results) == 0 || result.Results[0].Output != "executed" {
		t.Errorf("Results[0].Output = %q, want \"executed\"", result.Results[0].Output)
	}
}
