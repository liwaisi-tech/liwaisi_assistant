package valueobject

import "time"

// PlanStatus represents the lifecycle state of a plan or micro-task.
type PlanStatus string

const (
	// PlanStatusPending means the task has not yet started.
	PlanStatusPending PlanStatus = "pending"
	// PlanStatusRunning means the task is currently executing.
	PlanStatusRunning PlanStatus = "running"
	// PlanStatusCompleted means the task finished successfully.
	PlanStatusCompleted PlanStatus = "completed"
	// PlanStatusFailed means the task finished with an error.
	PlanStatusFailed PlanStatus = "failed"
)

// IsTerminal reports whether the status represents a finished state.
func (s PlanStatus) IsTerminal() bool {
	return s == PlanStatusCompleted || s == PlanStatusFailed
}

// MicroTaskResult captures the outcome of a single MicroTask execution.
type MicroTaskResult struct {
	// TaskID matches the MicroTask.ID that produced this result.
	TaskID  string     `json:"task_id"`
	// Output is the textual result produced by the subagent.
	Output  string     `json:"output"`
	// Err holds any execution error. Nil means success.
	Err     error      `json:"-"`
	// Status reflects the terminal state of this micro-task.
	Status  PlanStatus `json:"status"`
	// Elapsed is the wall-clock time the task took to complete.
	Elapsed time.Duration `json:"elapsed"`
}

// IsSuccess reports whether the micro-task completed without error.
func (r *MicroTaskResult) IsSuccess() bool {
	return r.Err == nil && r.Status == PlanStatusCompleted
}

// PlanResult aggregates all MicroTaskResults from a PlanGraph execution.
type PlanResult struct {
	// Results holds one entry per MicroTask, in execution order.
	Results []MicroTaskResult `json:"results"`
	// Status is the overall plan status. It is Failed if any micro-task failed.
	Status  PlanStatus        `json:"status"`
	// Elapsed is the total wall-clock time from plan start to finish.
	Elapsed time.Duration     `json:"elapsed"`
}

// IsSuccess reports whether all micro-tasks completed successfully.
func (r *PlanResult) IsSuccess() bool {
	return r.Status == PlanStatusCompleted
}

// FailedTasks returns only the results where execution failed.
func (r *PlanResult) FailedTasks() []MicroTaskResult {
	var failed []MicroTaskResult
	for _, res := range r.Results {
		if !res.IsSuccess() {
			failed = append(failed, res)
		}
	}
	return failed
}
