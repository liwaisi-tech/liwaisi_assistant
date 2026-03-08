package service

import (
	"context"
	"fmt"
	"strings"
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

// --- PlanStore mock ---

type mockPlanStore struct {
	docs map[string]*entity.PlanDocument
}

func newMockPlanStore() *mockPlanStore {
	return &mockPlanStore{docs: make(map[string]*entity.PlanDocument)}
}

func (m *mockPlanStore) Save(_ context.Context, doc *entity.PlanDocument) error {
	m.docs[doc.SessionID] = doc
	return nil
}

func (m *mockPlanStore) Load(_ context.Context, sessionID string) (*entity.PlanDocument, error) {
	doc, ok := m.docs[sessionID]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return doc, nil
}

func (m *mockPlanStore) List(_ context.Context) ([]*entity.PlanDocument, error) {
	result := make([]*entity.PlanDocument, 0, len(m.docs))
	for _, doc := range m.docs {
		result = append(result, doc)
	}
	return result, nil
}

func (m *mockPlanStore) GetActive(_ context.Context) (*entity.PlanDocument, error) {
	for _, doc := range m.docs {
		if doc.Lifecycle == valueobject.PlanLifecycleActive {
			return doc, nil
		}
	}
	return nil, nil
}

// --- Legacy tests (backward compat) ---

func TestPlannerService_Plan_GateBypass(t *testing.T) {
	gate := planner.NewPlanGate()
	// NullLLM panics if Complete is called — proves gate bypass skip the LLM.
	decomp := planner.NewDecomposer(&plannerNullLLM{t: t}, "noop")
	svc := NewPlannerService(gate, decomp, nil)

	graph, err := svc.Plan(context.Background(), "session-1", "say hi")
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
	graph, err := svc.Plan(context.Background(), "session-2", "research the best Go concurrency patterns and synthesize them")
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

// --- Improved pipeline tests ---

func TestImprovedPlannerService_Plan_Pipeline(t *testing.T) {
	// LLM responses for Phase 1 (team), Phase 2 (decompose), Phase 3 (evaluate).
	fakeLLM := &plannerFakeLLM{
		responses: []string{
			// Phase 1: Team Assembly
			`{"needs_team":true,"confidence":0.9,"roles":[{"name":"software-architect","perspective":"design","instruction":"arch"},{"name":"qa-engineer","perspective":"testing","instruction":"qa"}],"reasoning":"needs team"}`,
			// Phase 2: Enhanced Decomposition
			`[{"id":"t1","description":"do t1","instruction":"inst t1"},{"id":"t2","description":"do t2","instruction":"inst t2","depends_on":["t1"]}]`,
			// Phase 3: Evaluation (passes)
			`{"score":0.85,"feedback":"good plan","passed":true}`,
		},
	}

	store := newMockPlanStore()
	svc := NewImprovedPlannerService(PlannerServiceDeps{
		Gate:           planner.NewPlanGate(),
		Decomposer:     planner.NewDecomposer(fakeLLM, "test"),
		EnhancedDecomp: planner.NewEnhancedDecomposer(fakeLLM, "test"),
		TeamAssembler:  planner.NewTeamAssembler(fakeLLM, "test"),
		Evaluator:      planner.NewPlanEvaluator(fakeLLM, "test"),
		SubAgentSvc:    &plannerMockSubAgent{output: "done"},
		Store:          store,
	})

	graph, err := svc.Plan(context.Background(), "session-3", "research and analyze Go concurrency patterns for the project")
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(graph.Tasks) != 2 {
		t.Errorf("Tasks len = %d, want 2", len(graph.Tasks))
	}
}

func TestImprovedPlannerService_Plan_GateBypassStillWorks(t *testing.T) {
	svc := NewImprovedPlannerService(PlannerServiceDeps{
		Gate:           planner.NewPlanGate(),
		Decomposer:     planner.NewDecomposer(&plannerNullLLM{t: t}, "noop"),
		EnhancedDecomp: planner.NewEnhancedDecomposer(&plannerNullLLM{t: t}, "noop"),
		TeamAssembler:  planner.NewTeamAssembler(&plannerNullLLM{t: t}, "noop"),
		Evaluator:      planner.NewPlanEvaluator(&plannerNullLLM{t: t}, "noop"),
	})

	// Simple task should still bypass the pipeline.
	graph, err := svc.Plan(context.Background(), "session-1", "say hi")
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if len(graph.Tasks) != 1 || graph.Tasks[0].ID != "planner-shortcut" {
		t.Errorf("expected gate bypass, got %v", graph.Tasks)
	}
}

func TestPlannerService_ShowPlan_NilStore(t *testing.T) {
	svc := NewPlannerService(planner.NewPlanGate(), planner.NewDecomposer(&plannerNullLLM{t: t}, "noop"), nil)

	_, err := svc.ShowPlan(context.Background(), "any")
	if err == nil {
		t.Error("ShowPlan without store should error")
	}
}

func TestPlannerService_ClosePlan_NilStore(t *testing.T) {
	svc := NewPlannerService(planner.NewPlanGate(), planner.NewDecomposer(&plannerNullLLM{t: t}, "noop"), nil)

	err := svc.ClosePlan(context.Background(), "any")
	if err == nil {
		t.Error("ClosePlan without store should error")
	}
}

func TestPlannerService_ListPlans_NilStore(t *testing.T) {
	svc := NewPlannerService(planner.NewPlanGate(), planner.NewDecomposer(&plannerNullLLM{t: t}, "noop"), nil)

	_, err := svc.ListPlans(context.Background())
	if err == nil {
		t.Error("ListPlans without store should error")
	}
}

func TestImprovedPlannerService_ShowPlan(t *testing.T) {
	store := newMockPlanStore()
	doc := entity.NewPlanDocument("sess-1", "task", valueobject.TeamEvaluation{}, nil, 0.9, "ok")
	_ = store.Save(context.Background(), doc)

	svc := NewImprovedPlannerService(PlannerServiceDeps{
		Gate:           planner.NewPlanGate(),
		Decomposer:     planner.NewDecomposer(&plannerNullLLM{t: t}, "noop"),
		EnhancedDecomp: planner.NewEnhancedDecomposer(&plannerNullLLM{t: t}, "noop"),
		TeamAssembler:  planner.NewTeamAssembler(&plannerNullLLM{t: t}, "noop"),
		Evaluator:      planner.NewPlanEvaluator(&plannerNullLLM{t: t}, "noop"),
		Store:          store,
	})

	got, err := svc.ShowPlan(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("ShowPlan error = %v", err)
	}
	if got.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want sess-1", got.SessionID)
	}
}

func TestImprovedPlannerService_ClosePlan(t *testing.T) {
	store := newMockPlanStore()
	doc := entity.NewPlanDocument("sess-close", "task", valueobject.TeamEvaluation{}, nil, 0.9, "ok")
	_ = store.Save(context.Background(), doc)

	svc := NewImprovedPlannerService(PlannerServiceDeps{
		Gate:           planner.NewPlanGate(),
		Decomposer:     planner.NewDecomposer(&plannerNullLLM{t: t}, "noop"),
		EnhancedDecomp: planner.NewEnhancedDecomposer(&plannerNullLLM{t: t}, "noop"),
		TeamAssembler:  planner.NewTeamAssembler(&plannerNullLLM{t: t}, "noop"),
		Evaluator:      planner.NewPlanEvaluator(&plannerNullLLM{t: t}, "noop"),
		Store:          store,
	})

	if err := svc.ClosePlan(context.Background(), "sess-close"); err != nil {
		t.Fatalf("ClosePlan error = %v", err)
	}

	got, _ := store.Load(context.Background(), "sess-close")
	if got.Lifecycle != valueobject.PlanLifecycleClosed {
		t.Errorf("Lifecycle = %q, want closed", got.Lifecycle)
	}
}

func TestImprovedPlannerService_ListPlans(t *testing.T) {
	store := newMockPlanStore()
	_ = store.Save(context.Background(), entity.NewPlanDocument("s1", "t1", valueobject.TeamEvaluation{}, nil, 0.8, ""))
	_ = store.Save(context.Background(), entity.NewPlanDocument("s2", "t2", valueobject.TeamEvaluation{}, nil, 0.9, ""))

	svc := NewImprovedPlannerService(PlannerServiceDeps{
		Gate:           planner.NewPlanGate(),
		Decomposer:     planner.NewDecomposer(&plannerNullLLM{t: t}, "noop"),
		EnhancedDecomp: planner.NewEnhancedDecomposer(&plannerNullLLM{t: t}, "noop"),
		TeamAssembler:  planner.NewTeamAssembler(&plannerNullLLM{t: t}, "noop"),
		Evaluator:      planner.NewPlanEvaluator(&plannerNullLLM{t: t}, "noop"),
		Store:          store,
	})

	docs, err := svc.ListPlans(context.Background())
	if err != nil {
		t.Fatalf("ListPlans error = %v", err)
	}
	if len(docs) != 2 {
		t.Errorf("ListPlans = %d docs, want 2", len(docs))
	}
}

func TestImprovedPlannerService_Plan_OverrideFailure(t *testing.T) {
	fakeLLM := &plannerFakeLLM{
		responses: []string{
			`{"needs_team":true,"roles":[{"name":"arch","perspective":"p","instruction":"i"}],"passed":true}`,
			`[{"id":"t1","description":"d"}]`,
			`{"score":0.9,"passed":true}`,
		},
	}

	// Custom mock that returns a closed plan as if it were active.
	store := &overrideFailureStore{
		mockPlanStore: newMockPlanStore(),
	}
	closedPlan := entity.NewPlanDocument("old", "task", valueobject.TeamEvaluation{}, nil, 0.9, "")
	_ = closedPlan.Close()
	store.docs["old"] = closedPlan

	svc := NewImprovedPlannerService(PlannerServiceDeps{
		Gate:           planner.NewPlanGate(),
		Decomposer:     planner.NewDecomposer(fakeLLM, "test"),
		EnhancedDecomp: planner.NewEnhancedDecomposer(fakeLLM, "test"),
		TeamAssembler:  planner.NewTeamAssembler(fakeLLM, "test"),
		Evaluator:      planner.NewPlanEvaluator(fakeLLM, "test"),
		Store:          store,
	})

	_, err := svc.Plan(context.Background(), "new", "research and analyze the best way to handle plan overrides in a persistent store")
	if err == nil {
		t.Fatal("Plan should fail if active plan override fails")
	}
	if !strings.Contains(err.Error(), "overlapping active plan") {
		t.Errorf("expected 'overlapping active plan' error, got: %v", err)
	}
}

type overrideFailureStore struct {
	*mockPlanStore
}

func (s *overrideFailureStore) GetActive(_ context.Context) (*entity.PlanDocument, error) {
	// Return the first plan regardless of state to force override failure.
	for _, doc := range s.docs {
		return doc, nil
	}
	return nil, nil
}
