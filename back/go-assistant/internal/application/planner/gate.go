package planner

import (
	"strings"
)

// PlanGate performs a heuristic check to see if a complex planner is needed.
type PlanGate struct{}

// ShouldPlan returns true if the task requires decomposition.
func (g *PlanGate) ShouldPlan(task string) bool {
	// Simple heuristic: if task is long or contains keywords, plan it.
	if len(task) > 40 {
		return true
	}
	keywords := []string{"research", "analyze", "synthesize", "find", "implement"}
	for _, k := range keywords {
		if strings.Contains(strings.ToLower(task), k) {
			return true
		}
	}
	return false
}
