package entity

import "fmt"

// MicroTask is a node in the PlanGraph. It represents an atomic unit of
// work that can be delegated to a subagent.
type MicroTask struct {
	// ID is a unique kebab-case identifier for this task within the graph.
	ID string
	// Description is a human-readable summary of the task.
	Description string
	// Spec defines the subagent 4-tuple that will execute this task.
	Spec SubAgentSpec
	// DependsOn holds the IDs of upstream MicroTasks whose results must be
	// available before this task can start.
	DependsOn []string
	// ContextFrom lists the upstream IDs whose output should be injected as
	// context into this task's Spec before it runs.
	ContextFrom []string
}

// PlanGraph is a validated directed acyclic graph (DAG) of MicroTasks.
// It represents the full execution plan decomposed from a user request.
type PlanGraph struct {
	Tasks []*MicroTask
}

// TaskByID returns the MicroTask with the given ID, or (nil, false) if not found.
func (g *PlanGraph) TaskByID(id string) (*MicroTask, bool) {
	for _, t := range g.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return nil, false
}

// Validate checks the graph for structural integrity:
//   - no empty task IDs
//   - all DependsOn and ContextFrom IDs reference existing tasks
//   - no directed cycles (detected via DFS)
func (g *PlanGraph) Validate() error {
	if len(g.Tasks) == 0 {
		return fmt.Errorf("plan graph: must contain at least one task")
	}

	// Build ID set for existence checks.
	ids := make(map[string]struct{}, len(g.Tasks))
	for _, t := range g.Tasks {
		if t.ID == "" {
			return fmt.Errorf("plan graph: task has empty ID")
		}
		if _, dup := ids[t.ID]; dup {
			return fmt.Errorf("plan graph: duplicate task ID %q", t.ID)
		}
		ids[t.ID] = struct{}{}
	}

	// Validate dependency references and build adjacency list.
	adj := make(map[string][]string, len(g.Tasks))
	for _, t := range g.Tasks {
		for _, dep := range t.DependsOn {
			if _, exists := ids[dep]; !exists {
				return fmt.Errorf("plan graph: task %q depends on unknown ID %q", t.ID, dep)
			}
		}
		for _, cf := range t.ContextFrom {
			if _, exists := ids[cf]; !exists {
				return fmt.Errorf("plan graph: task %q has unknown ContextFrom ID %q", t.ID, cf)
			}
		}
		adj[t.ID] = t.DependsOn
	}

	// Detect cycles via DFS with three-colour marking.
	// white (0) → not visited, grey (1) → in stack, black (2) → done.
	const (
		white = 0
		grey  = 1
		black = 2
	)
	colour := make(map[string]int, len(g.Tasks))

	var dfs func(id string) error
	dfs = func(id string) error {
		colour[id] = grey
		for _, dep := range adj[id] {
			switch colour[dep] {
			case grey:
				return fmt.Errorf("plan graph: cycle detected between %q and %q", id, dep)
			case white:
				if err := dfs(dep); err != nil {
					return err
				}
			}
		}
		colour[id] = black
		return nil
	}

	for _, t := range g.Tasks {
		if colour[t.ID] == white {
			if err := dfs(t.ID); err != nil {
				return err
			}
		}
	}

	return nil
}
