package entity

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type TaskStatus string

const (
	StatusPending   TaskStatus = "PENDING"
	StatusRunning   TaskStatus = "RUNNING"
	StatusCompleted TaskStatus = "COMPLETED"
	StatusFailed    TaskStatus = "FAILED"
)

// MicroTask represents an individual unit of work in the DAG.
type MicroTask struct {
	ID           string
	Description  string
	Dependencies []string // IDs of tasks that must complete first
	Status       TaskStatus
	CreatedAt    time.Time
}

// PlanGraph represents the DAG of tasks.
type PlanGraph struct {
	ID    string
	Tasks map[string]*MicroTask
	Root  []string // Entry point task IDs
}

// MicroTaskResult defines the result of a micro-task execution.
type MicroTaskResult struct {
	TaskID string
	Status TaskStatus
	Output string
}

// PlanResult defines the result of a full plan execution.
type PlanResult struct {
	Results map[string]MicroTaskResult
}

// Scheduler executes a PlanGraph as a DAG.
type Scheduler struct{}

// Execute runs the tasks in the graph, respecting dependencies.
func (s *Scheduler) Execute(ctx context.Context, graph *PlanGraph) (PlanResult, error) {
	// 1. Validate graph
	if err := s.validate(graph); err != nil {
		return PlanResult{}, err
	}

	// 2. Track task completion
	results := make(map[string]MicroTaskResult)
	var mu sync.Mutex
	done := make(map[string]chan struct{})
	for id := range graph.Tasks {
		done[id] = make(chan struct{})
	}

	// 3. Launch tasks
	var wg sync.WaitGroup
	for id, task := range graph.Tasks {
		wg.Add(1)
		go func(id string, task *MicroTask) {
			defer wg.Done()

			// Wait for dependencies
			for _, depID := range task.Dependencies {
				select {
				case <-done[depID]:
				case <-ctx.Done():
					return
				}
			}

			// Execute task (Simplified for now)
			fmt.Printf("Executing task: %s\n", id)
			
			mu.Lock()
			results[id] = MicroTaskResult{TaskID: id, Status: StatusCompleted}
			mu.Unlock()

			close(done[id])
		}(id, task)
	}

	wg.Wait()
	return PlanResult{Results: results}, nil
}

func (s *Scheduler) validate(graph *PlanGraph) error {
	return nil
}
