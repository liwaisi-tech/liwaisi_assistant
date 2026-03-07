package entity

import (
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
