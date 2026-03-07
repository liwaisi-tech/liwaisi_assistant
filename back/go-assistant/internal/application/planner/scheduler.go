package planner

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/entity"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/port/input"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// SchedulerOptions configure optional Scheduler behaviour.
type SchedulerOptions struct {
	// FailFast causes the scheduler to cancel all in-progress goroutines
	// as soon as any single MicroTask fails.
	FailFast bool
}

// Scheduler executes a PlanGraph by running independent tasks concurrently
// (wave-based Kahn's topological sort) and passing upstream outputs to
// downstream tasks via an InputMapper.
//
// The Scheduler itself makes no LLM calls; it delegates task execution to
// a SubAgentService.
type Scheduler struct {
	opts SchedulerOptions
}

// NewScheduler creates a Scheduler with the given options.
func NewScheduler(opts SchedulerOptions) *Scheduler {
	return &Scheduler{opts: opts}
}

// Run executes the PlanGraph, respecting dependency order and running
// independent nodes in each wave as parallel goroutines.
//
// It returns a PlanResult aggregating the outcome of every MicroTask.
// If FailFast is enabled, the first failure cancels sibling goroutines.
func (s *Scheduler) Run(
	ctx context.Context,
	graph *entity.PlanGraph,
	sessionID string,
	svc input.SubAgentService,
) valueobject.PlanResult {
	start := time.Now()
	results := make(map[string]valueobject.MicroTaskResult, len(graph.Tasks))
	var mu sync.Mutex

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Build an index of tasks by ID for quick lookup.
	taskByID := make(map[string]*entity.MicroTask, len(graph.Tasks))
	for _, t := range graph.Tasks {
		taskByID[t.ID] = t
	}

	// Kahn's algorithm: compute in-degree for each task.
	inDegree := make(map[string]int, len(graph.Tasks))
	for _, t := range graph.Tasks {
		if _, ok := inDegree[t.ID]; !ok {
			inDegree[t.ID] = 0
		}
		for _, dep := range t.DependsOn {
			inDegree[dep] = inDegree[dep] // ensure dep exists in map
			inDegree[t.ID]++
		}
	}

	// Collect wave 0 (tasks with no dependencies).
	ready := make([]string, 0)
	for _, t := range graph.Tasks {
		if inDegree[t.ID] == 0 {
			ready = append(ready, t.ID)
		}
	}

	planFailed := false

	// Process waves until all tasks are executed or context is cancelled.
	for len(ready) > 0 {
		// Stop processing new waves if context was cancelled (e.g. by FailFast).
		if runCtx.Err() != nil {
			break
		}

		wave := ready
		ready = nil

		var wg sync.WaitGroup
		waveResults := make([]valueobject.MicroTaskResult, len(wave))

		for i, taskID := range wave {
			wg.Add(1)
			go func(idx int, tid string) {
				defer wg.Done()
				task := taskByID[tid]
				res := s.runTask(runCtx, task, sessionID, svc, results, &mu)
				waveResults[idx] = res
				mu.Lock()
				results[tid] = res
				if !res.IsSuccess() && s.opts.FailFast {
					cancel()
				}
				mu.Unlock()
			}(i, taskID)
		}

		wg.Wait()

		// Check for failures and advance Kahn's in-degree reduction.
		for _, res := range waveResults {
			if !res.IsSuccess() {
				planFailed = true
			}
		}

		if planFailed && s.opts.FailFast {
			break
		}

		// Reduce in-degree for tasks downstream of completed wave.
		for _, waveTaskID := range wave {
			for _, t := range graph.Tasks {
				for _, dep := range t.DependsOn {
					if dep == waveTaskID {
						inDegree[t.ID]--
						if inDegree[t.ID] == 0 {
							ready = append(ready, t.ID)
						}
					}
				}
			}
		}
	}

	// Collect final ordered results.
	ordered := make([]valueobject.MicroTaskResult, 0, len(graph.Tasks))
	for _, t := range graph.Tasks {
		ordered = append(ordered, results[t.ID])
	}

	status := valueobject.PlanStatusCompleted
	if planFailed {
		status = valueobject.PlanStatusFailed
	}

	return valueobject.PlanResult{
		Results: ordered,
		Status:  status,
		Elapsed: time.Since(start),
	}
}

// runTask executes a single MicroTask, injecting context from upstream results
// as specified by task.ContextFrom.
func (s *Scheduler) runTask(
	ctx context.Context,
	task *entity.MicroTask,
	sessionID string,
	svc input.SubAgentService,
	completedResults map[string]valueobject.MicroTaskResult,
	mu *sync.Mutex,
) valueobject.MicroTaskResult {
	taskStart := time.Now()

	// Build the task spec, injecting upstream context.
	spec := task.Spec
	if len(task.ContextFrom) > 0 {
		var sb strings.Builder
		if spec.Context != "" {
			sb.WriteString(spec.Context)
			sb.WriteString("\n\n")
		}
		sb.WriteString("--- Upstream results ---\n")
		mu.Lock()
		for _, upstreamID := range task.ContextFrom {
			if upstreamRes, ok := completedResults[upstreamID]; ok {
				sb.WriteString(fmt.Sprintf("[%s]:\n%s\n\n", upstreamID, upstreamRes.Output))
			}
		}
		mu.Unlock()
		spec.Context = sb.String()
	}

	agentResult, err := svc.Spawn(ctx, sessionID, &spec, task.Description)
	if err != nil {
		return valueobject.MicroTaskResult{
			TaskID:  task.ID,
			Err:     fmt.Errorf("micro-task %q: %w", task.ID, err),
			Status:  valueobject.PlanStatusFailed,
			Elapsed: time.Since(taskStart),
		}
	}

	status := valueobject.PlanStatusCompleted
	var taskErr error
	if agentResult.Err != nil {
		status = valueobject.PlanStatusFailed
		taskErr = agentResult.Err
	}

	return valueobject.MicroTaskResult{
		TaskID:  task.ID,
		Output:  agentResult.Output,
		Err:     taskErr,
		Status:  status,
		Elapsed: time.Since(taskStart),
	}
}
