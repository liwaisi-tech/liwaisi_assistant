package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/application/planner"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/output"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// compile-time interface verification.
var _ input.PlannerService = (*plannerService)(nil)

type plannerService struct {
	gate           *planner.PlanGate
	decomp         *planner.Decomposer
	enhancedDecomp *planner.EnhancedDecomposer
	teamAssembler  *planner.TeamAssembler
	evaluator      *planner.PlanEvaluator
	scheduler      *planner.Scheduler
	subagent       input.SubAgentService
	store          output.PlanStore

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

// PlannerServiceDeps contains the dependencies for the improved planner service.
type PlannerServiceDeps struct {
	Gate           *planner.PlanGate
	Decomposer     *planner.Decomposer
	EnhancedDecomp *planner.EnhancedDecomposer
	TeamAssembler  *planner.TeamAssembler
	Evaluator      *planner.PlanEvaluator
	SubAgentSvc    input.SubAgentService
	Store          output.PlanStore
}

// NewPlannerService wires the gate, decomposer, scheduler, and subagent
// service together into a PlannerService.
func NewPlannerService(
	gate *planner.PlanGate,
	decomp *planner.Decomposer,
	subagentSvc input.SubAgentService,
) input.PlannerService {
	return &plannerService{
		gate:        gate,
		decomp:      decomp,
		scheduler:   planner.NewScheduler(planner.SchedulerOptions{}),
		subagent:    subagentSvc,
		defaultSpec: PlannerBuiltInSpec,
	}
}

// NewImprovedPlannerService creates a planner with the full 3-phase pipeline:
// Team Assembly → Expert-Informed Decomposition → Evaluation & Refinement.
func NewImprovedPlannerService(deps PlannerServiceDeps) input.PlannerService {
	return &plannerService{
		gate:           deps.Gate,
		decomp:         deps.Decomposer,
		enhancedDecomp: deps.EnhancedDecomp,
		teamAssembler:  deps.TeamAssembler,
		evaluator:      deps.Evaluator,
		scheduler:      planner.NewScheduler(planner.SchedulerOptions{}),
		subagent:       deps.SubAgentSvc,
		store:          deps.Store,
		defaultSpec:    PlannerBuiltInSpec,
	}
}

// Plan decomposes a task into a PlanGraph.
// Simple tasks (gate bypass) produce a single-node graph directly.
// Complex tasks go through the 3-phase pipeline if the improved components
// are configured, or fall back to the legacy single-pass decomposer.
func (p *plannerService) Plan(ctx context.Context, sessionID, task string) (*entity.PlanGraph, error) {
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

	// Use the improved 3-phase pipeline if team assembler is configured.
	if p.teamAssembler != nil && p.enhancedDecomp != nil && p.evaluator != nil {
		return p.planWithPipeline(ctx, sessionID, task)
	}

	// Legacy single-pass decomposition.
	graph, err := p.decomp.Decompose(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("planner: decompose: %w", err)
	}
	return graph, nil
}

// planWithPipeline implements the 3-phase improved pipeline:
// Phase 1: Team Assembly → Phase 2: Expert-Informed Decomposition → Phase 3: Evaluation & Refinement.
func (p *plannerService) planWithPipeline(ctx context.Context, sessionID, task string) (*entity.PlanGraph, error) {
	// Phase 1: Team Assembly.
	slog.Info("planner: phase 1 — assembling expert team")
	team, err := p.teamAssembler.Assemble(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("planner: team assembly: %w", err)
	}
	slog.Info("planner: team assembled",
		"roles", len(team.Roles),
		"confidence", team.Confidence,
	)

	// Phase 2: Expert-Informed Decomposition.
	slog.Info("planner: phase 2 — expert-informed decomposition")
	graph, err := p.enhancedDecomp.Decompose(ctx, task, team.Roles)
	if err != nil {
		return nil, fmt.Errorf("planner: enhanced decompose: %w", err)
	}

	// Phase 3: Evaluation & Refinement (max 2 iterations).
	for i := range planner.MaxRefinementIterations {
		slog.Info("planner: phase 3 — evaluating plan", "iteration", i+1)
		eval, err := p.evaluator.Evaluate(ctx, task, team.Roles, graph)
		if err != nil {
			slog.Warn("planner: evaluation failed, using current graph", "error", err)
			break
		}

		slog.Info("planner: evaluation result",
			"score", eval.Score,
			"passed", eval.Passed,
		)

		if eval.Passed {
			// Plan passes — persist if store available.
			if p.store != nil {
				doc := entity.NewPlanDocument(sessionID, task, team, graph, eval.Score, eval.Feedback)
				if saveErr := p.persistPlan(ctx, doc); saveErr != nil {
					slog.Warn("planner: failed to persist plan", "error", saveErr)
				}
			}
			return graph, nil
		}

		// Refinement: feed evaluation feedback back into decomposer.
		slog.Info("planner: plan below threshold, refining", "feedback", eval.Feedback)
		graph, err = p.enhancedDecomp.DecomposeWithFeedback(ctx, task, team.Roles, eval.Feedback)
		if err != nil {
			return nil, fmt.Errorf("planner: refinement iteration %d: %w", i+1, err)
		}
	}

	// After max iterations, use the last graph.
	if p.store != nil {
		doc := entity.NewPlanDocument(sessionID, task, team, graph, 0, "max refinement iterations reached")
		if saveErr := p.persistPlan(ctx, doc); saveErr != nil {
			slog.Warn("planner: failed to persist plan", "error", saveErr)
		}
	}
	return graph, nil
}

// persistPlan saves a plan document, first closing any existing active plan.
func (p *plannerService) persistPlan(ctx context.Context, doc *entity.PlanDocument) error {
	// Close any existing active plan.
	active, err := p.store.GetActive(ctx)
	if err != nil {
		slog.Warn("planner: failed to check active plan", "error", err)
	}
	if active != nil {
		if overrideErr := active.Override(); overrideErr != nil {
			slog.Warn("planner: override active plan", "error", overrideErr)
		} else {
			_ = p.store.Save(ctx, active)
		}
	}

	return p.store.Save(ctx, doc)
}

// Execute runs a pre-built PlanGraph using the Scheduler.
func (p *plannerService) Execute(ctx context.Context, sessionID string, graph *entity.PlanGraph, opts ...input.PlannerServiceOption) (valueobject.PlanResult, error) {
	options := &input.PlannerServiceOptions{}
	for _, opt := range opts {
		opt(options)
	}

	sched := p.scheduler
	if options.FailFast {
		sched = planner.NewScheduler(planner.SchedulerOptions{FailFast: true})
	}

	result := sched.Run(ctx, graph, sessionID, p.subagent)
	return result, nil
}

// PlanAndExecute generates a plan and immediately executes it.
func (p *plannerService) PlanAndExecute(ctx context.Context, sessionID, task string, opts ...input.PlannerServiceOption) (valueobject.PlanResult, error) {
	graph, err := p.Plan(ctx, sessionID, task)
	if err != nil {
		return valueobject.PlanResult{}, err
	}
	return p.Execute(ctx, sessionID, graph, opts...)
}

// ShowPlan retrieves a persisted plan by session ID.
func (p *plannerService) ShowPlan(ctx context.Context, sessionID string) (*entity.PlanDocument, error) {
	if p.store == nil {
		return nil, fmt.Errorf("plan persistence not configured")
	}
	if sessionID == "" {
		return p.store.GetActive(ctx)
	}
	return p.store.Load(ctx, sessionID)
}

// ClosePlan transitions a plan to the closed lifecycle state.
func (p *plannerService) ClosePlan(ctx context.Context, sessionID string) error {
	if p.store == nil {
		return fmt.Errorf("plan persistence not configured")
	}

	var doc *entity.PlanDocument
	var err error
	if sessionID == "" {
		doc, err = p.store.GetActive(ctx)
	} else {
		doc, err = p.store.Load(ctx, sessionID)
	}
	if err != nil {
		return fmt.Errorf("loading plan: %w", err)
	}
	if doc == nil {
		return fmt.Errorf("no plan found to close")
	}

	if err := doc.Close(); err != nil {
		return fmt.Errorf("closing plan: %w", err)
	}
	return p.store.Save(ctx, doc)
}

// ListPlans returns all persisted plans.
func (p *plannerService) ListPlans(ctx context.Context) ([]*entity.PlanDocument, error) {
	if p.store == nil {
		return nil, fmt.Errorf("plan persistence not configured")
	}
	return p.store.List(ctx)
}
