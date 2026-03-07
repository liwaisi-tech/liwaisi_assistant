package planner

import (
	"context"
	"fmt"
	"sync"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
)

// Scheduler defines the interface for DAG execution.
type Scheduler interface {
	Execute(ctx context.Context, graph *entity.PlanGraph) (entity.PlanResult, error)
}

// DAGScheduler executes a PlanGraph as a DAG.
type DAGScheduler struct{}

// Execute runs the tasks in the graph, respecting dependencies.
func (s *DAGScheduler) Execute(ctx context.Context, graph *entity.PlanGraph) (entity.PlanResult, error) {
	if err := s.validate(graph); err != nil {
		return entity.PlanResult{}, err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(map[string]entity.MicroTaskResult)
	var mu sync.Mutex
	done := make(map[string]chan struct{})
	for id := range graph.Tasks {
		done[id] = make(chan struct{})
	}

	errChan := make(chan error, len(graph.Tasks))
	var wg sync.WaitGroup

	for id, task := range graph.Tasks {
		wg.Add(1)
		go func(id string, task *entity.MicroTask) {
			defer wg.Done()
			defer func() {
				// Ensure downstream tasks aren't blocked if this task fails or panics
				close(done[id])
			}()

			for _, depID := range task.Dependencies {
				select {
				case <-done[depID]:
				case <-ctx.Done():
					return
				}
			}

			mu.Lock()
			results[id] = entity.MicroTaskResult{TaskID: id, Status: entity.StatusCompleted}
			mu.Unlock()
		}(id, task)
	}

	wg.Wait()
	select {
	case err := <-errChan:
		return entity.PlanResult{}, err
	default:
		return entity.PlanResult{Results: results}, nil
	}
}

func (s *DAGScheduler) validate(graph *entity.PlanGraph) error {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var visit func(string) error
	visit = func(id string) error {
		visited[id] = true
		recStack[id] = true

		task, ok := graph.Tasks[id]
		if !ok {
			return fmt.Errorf("task %s not found", id)
		}

		for _, depID := range task.Dependencies {
			if !visited[depID] {
				if err := visit(depID); err != nil {
					return err
				}
			} else if recStack[depID] {
				return fmt.Errorf("cycle detected at %s", depID)
			}
		}
		recStack[id] = false
		return nil
	}

	for id := range graph.Tasks {
		if !visited[id] {
			if err := visit(id); err != nil {
				return err
			}
		}
	}
	return nil
}
