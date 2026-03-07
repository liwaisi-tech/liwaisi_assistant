package input

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// PlannerService defines the input port for DAG-based task decomposition
// and parallel/sequential execution via the Planner subagent.
type PlannerService interface {
	// Plan decomposes a task into a validated PlanGraph.
	// For simple tasks the PlanGate returns a single-node graph without
	// any LLM call. Complex tasks go through the LLM Decomposer.
	Plan(ctx context.Context, task string) (*entity.PlanGraph, error)

	// Execute runs a pre-built PlanGraph, scheduling MicroTasks according
	// to their dependency order and running independent waves in parallel.
	Execute(ctx context.Context, sessionID string, graph *entity.PlanGraph) (valueobject.PlanResult, error)

	// PlanAndExecute is a convenience method that calls Plan followed by Execute.
	PlanAndExecute(ctx context.Context, sessionID string, task string) (valueobject.PlanResult, error)
}
