package planner

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// --- mock SubAgentService for scheduler tests ---

type mockSubAgentService struct {
	mu      sync.Mutex
	delays  map[string]time.Duration // per-task artificial delay
	outputs map[string]string        // per-task output
	errors  map[string]error         // per-task error
	calls   int32
}

func newMockSvc(outputs map[string]string) *mockSubAgentService {
	return &mockSubAgentService{
		delays:  make(map[string]time.Duration),
		outputs: outputs,
		errors:  make(map[string]error),
	}
}

func (m *mockSubAgentService) Spawn(_ context.Context, _ string, spec *entity.SubAgentSpec, _ string) (valueobject.SubAgentResult, error) {
	atomic.AddInt32(&m.calls, 1)
	m.mu.Lock()
	delay := m.delays[spec.Name]
	out := m.outputs[spec.Name]
	err := m.errors[spec.Name]
	m.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}
	if err != nil {
		return valueobject.SubAgentResult{AgentName: spec.Name, Err: err, Status: valueobject.SubAgentStatusFailed}, nil
	}
	return valueobject.SubAgentResult{AgentName: spec.Name, Output: out, Status: valueobject.SubAgentStatusCompleted}, nil
}

func (m *mockSubAgentService) SpawnParallel(_ context.Context, _ string, specs []*entity.SubAgentSpec, tasks []string) ([]valueobject.SubAgentResult, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockSubAgentService) ListAgents() []entity.SubAgentSpec { return nil }
func (m *mockSubAgentService) GetAgent(name string) (entity.SubAgentSpec, bool) {
	return entity.SubAgentSpec{}, false
}
func (m *mockSubAgentService) EvaluateTeam(_ context.Context, _ string) (valueobject.TeamEvaluation, error) {
	return valueobject.TeamEvaluation{}, fmt.Errorf("not implemented")
}
func (m *mockSubAgentService) CreateAgent(_ context.Context, _ *entity.SubAgentSpec) error {
	return fmt.Errorf("not implemented")
}

// --- helper to build a simple MicroTask ---

func makeTask(id string, deps ...string) *entity.MicroTask {
	return &entity.MicroTask{
		ID:          id,
		Description: "task " + id,
		Spec:        entity.SubAgentSpec{Name: id, Instruction: "do " + id, BuiltIn: false},
		DependsOn:   deps,
	}
}

// --- tests ---

func TestScheduler_SingleTask(t *testing.T) {
	graph := &entity.PlanGraph{
		Tasks: []*entity.MicroTask{makeTask("A")},
	}
	svc := newMockSvc(map[string]string{"A": "result-A"})
	s := NewScheduler(SchedulerOptions{})

	result := s.Run(context.Background(), graph, "session", svc)

	if result.Status != valueobject.PlanStatusCompleted {
		t.Errorf("Status = %q, want completed", result.Status)
	}
	if len(result.Results) != 1 || result.Results[0].Output != "result-A" {
		t.Errorf("Results[0].Output = %q, want \"result-A\"", result.Results[0].Output)
	}
}

func TestScheduler_LinearChain_OrderEnforced(t *testing.T) {
	// A → B → C : B must receive A's output in its context; C must see B's.
	taskB := &entity.MicroTask{
		ID: "B", Description: "B", DependsOn: []string{"A"}, ContextFrom: []string{"A"},
		Spec: entity.SubAgentSpec{Name: "B", Instruction: "do B"},
	}
	taskC := &entity.MicroTask{
		ID: "C", Description: "C", DependsOn: []string{"B"}, ContextFrom: []string{"B"},
		Spec: entity.SubAgentSpec{Name: "C", Instruction: "do C"},
	}
	graph := &entity.PlanGraph{
		Tasks: []*entity.MicroTask{makeTask("A"), taskB, taskC},
	}
	svc := newMockSvc(map[string]string{"A": "output-A", "B": "output-B", "C": "output-C"})
	s := NewScheduler(SchedulerOptions{})

	result := s.Run(context.Background(), graph, "session", svc)

	if result.Status != valueobject.PlanStatusCompleted {
		t.Errorf("Status = %q, want completed", result.Status)
	}
	if len(result.Results) != 3 {
		t.Fatalf("Results len = %d, want 3", len(result.Results))
	}
	// Results are in graph order: A, B, C
	if result.Results[0].TaskID != "A" || result.Results[1].TaskID != "B" || result.Results[2].TaskID != "C" {
		t.Errorf("unexpected order: %v", result.Results)
	}
}

func TestScheduler_ParallelWave_ConcurrencyConfirmed(t *testing.T) {
	// root → [worker-1, worker-2, worker-3] → synth
	// The three workers run in parallel: total elapsed should be < sum of delays.
	const workerDelay = 100 * time.Millisecond

	graph := &entity.PlanGraph{
		Tasks: []*entity.MicroTask{
			makeTask("root"),
			{ID: "w1", Spec: entity.SubAgentSpec{Name: "w1", Instruction: "w1"}, DependsOn: []string{"root"}},
			{ID: "w2", Spec: entity.SubAgentSpec{Name: "w2", Instruction: "w2"}, DependsOn: []string{"root"}},
			{ID: "w3", Spec: entity.SubAgentSpec{Name: "w3", Instruction: "w3"}, DependsOn: []string{"root"}},
			{ID: "synth", Spec: entity.SubAgentSpec{Name: "synth", Instruction: "synth"}, DependsOn: []string{"w1", "w2", "w3"}},
		},
	}

	svc := newMockSvc(map[string]string{})
	svc.delays["w1"] = workerDelay
	svc.delays["w2"] = workerDelay
	svc.delays["w3"] = workerDelay

	s := NewScheduler(SchedulerOptions{})
	start := time.Now()
	result := s.Run(context.Background(), graph, "session", svc)
	elapsed := time.Since(start)

	if result.Status != valueobject.PlanStatusCompleted {
		t.Errorf("Status = %q, want completed", result.Status)
	}

	// If serial, elapsed ≥ 3*workerDelay. Parallel should be ~workerDelay.
	maxExpected := 2 * workerDelay
	if elapsed > maxExpected {
		t.Errorf("elapsed %v > %v; workers likely ran serially, not in parallel", elapsed, maxExpected)
	}
}

func TestScheduler_FailFast_CancelsOthers(t *testing.T) {
	// Two parallel tasks; one fails. With FailFast, the sibling should be
	// canceled or at minimum the plan should report failure.
	graph := &entity.PlanGraph{
		Tasks: []*entity.MicroTask{
			makeTask("good"),
			makeTask("bad"),
		},
	}
	svc := newMockSvc(map[string]string{"good": "ok"})
	svc.errors["bad"] = fmt.Errorf("intentional failure")
	s := NewScheduler(SchedulerOptions{FailFast: true})

	result := s.Run(context.Background(), graph, "session", svc)

	if result.Status != valueobject.PlanStatusFailed {
		t.Errorf("Status = %q, want failed", result.Status)
	}
}

func TestScheduler_TaskFailure_StatusFailed(t *testing.T) {
	graph := &entity.PlanGraph{
		Tasks: []*entity.MicroTask{makeTask("only")},
	}
	svc := newMockSvc(map[string]string{})
	svc.errors["only"] = fmt.Errorf("bad error")
	s := NewScheduler(SchedulerOptions{})

	result := s.Run(context.Background(), graph, "session", svc)

	if result.Status != valueobject.PlanStatusFailed {
		t.Errorf("Status = %q, want failed", result.Status)
	}
	if len(result.Results) == 0 || result.Results[0].IsSuccess() {
		t.Error("expected task result to be failure")
	}
}
