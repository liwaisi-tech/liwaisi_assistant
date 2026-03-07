package service

import (
	"context"
	"fmt"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/planner"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// compile-time interface verification.
var _ input.PlannerService = (*plannerService)(nil)

type plannerService struct {
	gate      *planner.PlanGate
	decomp    *planner.Decomposer
	scheduler *planner.Scheduler
	subagent  input.SubAgentService

	// defaultSpec is used to spawn the single-node shortcut task.
	defaultSpec entity.SubAgentSpec
}

// PlannerBuiltInSpec is the SubAgentSpec registered in the Router at startup.
// It is exported so that the application bootstrap can call
// router.RegisterBuiltIn(&PlannerBuiltInSpec) at startup.
var PlannerBuiltInSpec = entity.SubAgentSpec{
	Name:        "planner",
	Description: "Built-in planner that decomposes and executes multi-step tasks.",
	Instruction: "You are a general-purpose executor. Complete the given task to the best of your ability and return a concise result.",
	ModelTier:   valueobject.ModelTierBalanced,
	BuiltIn:     true,
}

// PlannerServiceOption configures optional plannerService behaviour.
type PlannerServiceOption func(*plannerService)

// WithFailFast enables fast-fail semantics: the first MicroTask failure
// cancels all in-flight sibling goroutines.
func WithFailFast() PlannerServiceOption {
	return func(p *plannerService) {
		p.scheduler = planner.NewScheduler(planner.SchedulerOptions{FailFast: true})
	}
}

// NewPlannerService wires the gate, decomposer, scheduler, and subagent
// service together into a PlannerService.
func NewPlannerService(
	gate *planner.PlanGate,
	decomp *planner.Decomposer,
	subagentSvc input.SubAgentService,
	opts ...PlannerServiceOption,
) input.PlannerService {
	p := &plannerService{
		gate:        gate,
		decomp:      decomp,
		scheduler:   planner.NewScheduler(planner.SchedulerOptions{}),
		subagent:    subagentSvc,
		defaultSpec: PlannerBuiltInSpec,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Plan decomposes a task into a PlanGraph.
// Simple tasks (gate bypass) produce a single-node graph directly.
// Complex tasks go through the LLM Decomposer.
func (p *plannerService) Plan(ctx context.Context, task string) (*entity.PlanGraph, error) {
	if !p.gate.ShouldDecompose(task) {
		// Single-node shortcut — no LLM call.
		spec := p.defaultSpec
		spec.Name = "planner-shortcut"
		return &entity.PlanGraph{
			Tasks: []*entity.MicroTask{
				{
					ID:          "planner-shortcut",
					Description: task,
					Spec:        spec,
				},
			},
		}, nil
	}

	graph, err := p.decomp.Decompose(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("planner: decompose: %w", err)
	}
	return graph, nil
}

// Execute runs a pre-built PlanGraph using the Scheduler.
func (p *plannerService) Execute(ctx context.Context, sessionID string, graph *entity.PlanGraph) (valueobject.PlanResult, error) {
	result := p.scheduler.Run(ctx, graph, sessionID, p.subagent)
	return result, nil
}

// PlanAndExecute is a convenience method: Plan then Execute.
func (p *plannerService) PlanAndExecute(ctx context.Context, sessionID string, task string) (valueobject.PlanResult, error) {
	graph, err := p.Plan(ctx, task)
	if err != nil {
		return valueobject.PlanResult{}, err
	}
	return p.Execute(ctx, sessionID, graph)
}
