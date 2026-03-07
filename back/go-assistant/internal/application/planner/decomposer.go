package planner

import (
	"context"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
)

// Decomposer uses an LLM to turn a goal into a PlanGraph.
type Decomposer struct{}

// Decompose turns a goal string into a PlanGraph structure.
func (d *Decomposer) Decompose(ctx context.Context, goal string) (*entity.PlanGraph, error) {
	// In reality, this would call an LLM client with a system prompt.
	// For now, we return a mock graph for integration testing.
	
	graph := &entity.PlanGraph{
		ID:    "plan-001",
		Tasks: map[string]*entity.MicroTask{
			"task-1": {ID: "task-1", Description: "Initial research", Status: entity.StatusPending},
			"task-2": {ID: "task-2", Description: "Synthesize findings", Dependencies: []string{"task-1"}, Status: entity.StatusPending},
		},
		Root: []string{"task-1"},
	}
	return graph, nil
}
