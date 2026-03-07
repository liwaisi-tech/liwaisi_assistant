package port

import (
	"context"
	"github.com/liwaisi/go-assistant/internal/domain/entity"
)

// PlannerService defines the interface for decomposing complex goals into a DAG of micro-tasks.
type PlannerService interface {
	// CreatePlan decomposes a high-level goal into a PlanGraph.
	CreatePlan(ctx context.Context, goal string) (*entity.PlanGraph, error)
	
	// GetTask returns a specific micro-task by ID.
	GetTask(ctx context.Context, taskID string) (*entity.MicroTask, error)
	
	// UpdateTaskStatus updates the status of a micro-task and potentially triggers downstream tasks.
	UpdateTaskStatus(ctx context.Context, taskID string, status entity.TaskStatus) error
}
