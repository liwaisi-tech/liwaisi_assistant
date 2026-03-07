// Package planner implements the PlannerService building blocks:
// PlanGate, Decomposer, and Scheduler.
package planner

import "strings"

// complexKeywords are indicators that a task needs multi-step decomposition.
var complexKeywords = []string{
	"research", "find", "analyze",
	"synthesize",
	"compare", "summarize", "investigate", "evaluate",
	"compile", "gather", "aggregate", "explore",
}

// PlanGate decides whether a task is simple enough to bypass the LLM
// decomposer and use a single-node PlanGraph instead.
//
// A task is considered "simple" when:
//   - its trimmed length is under 40 characters, AND
//   - it contains none of the complexKeywords.
type PlanGate struct {
	maxSimpleLen int
}

// NewPlanGate creates a PlanGate with the default 40-character threshold.
func NewPlanGate() *PlanGate {
	return &PlanGate{maxSimpleLen: 40}
}

// ShouldDecompose reports whether the task requires LLM decomposition.
// Returns false (bypass) for short/simple tasks; true for complex ones.
func (g *PlanGate) ShouldDecompose(task string) bool {
	trimmed := strings.TrimSpace(task)
	if len(trimmed) >= g.maxSimpleLen {
		return true
	}
	lower := strings.ToLower(trimmed)
	for _, kw := range complexKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
