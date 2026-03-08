package entity

import (
	"fmt"
	"strings"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

// PlanDocument represents a persisted plan that can be saved, loaded, and
// managed through its lifecycle. It contains the full context of a planning
// session: the original task, team evaluation, execution graph, evaluation
// score, and lifecycle state.
type PlanDocument struct {
	// SessionID uniquely identifies this planning session.
	SessionID string
	// Task is the original user request that triggered planning.
	Task string
	// Team holds the team evaluation result from Phase 1.
	Team valueobject.TeamEvaluation
	// Graph is the validated DAG of MicroTasks from Phase 2.
	Graph *PlanGraph
	// Score is the evaluation score from Phase 3 (0.0–1.0).
	Score float64
	// Feedback is the evaluator's qualitative feedback on the plan.
	Feedback string
	// Lifecycle tracks the plan's current state (active, closed, overridden).
	Lifecycle valueobject.PlanLifecycle
	// CreatedAt records when the plan was first persisted.
	CreatedAt time.Time
	// ClosedAt records when the plan was closed or overridden. Nil if still active.
	ClosedAt *time.Time
}

// NewPlanDocument creates an active PlanDocument with the given fields.
func NewPlanDocument(
	sessionID, task string,
	team valueobject.TeamEvaluation,
	graph *PlanGraph,
	score float64,
	feedback string,
) *PlanDocument {
	return &PlanDocument{
		SessionID: sessionID,
		Task:      task,
		Team:      team,
		Graph:     graph,
		Score:     score,
		Feedback:  feedback,
		Lifecycle: valueobject.PlanLifecycleActive,
		CreatedAt: time.Now().UTC(),
	}
}

// Close transitions the plan to the closed lifecycle state.
// Returns an error if the plan is already in a terminal state.
func (d *PlanDocument) Close() error {
	if d.Lifecycle.IsTerminal() {
		return fmt.Errorf("plan %q is already %s", d.SessionID, d.Lifecycle)
	}
	now := time.Now().UTC()
	d.Lifecycle = valueobject.PlanLifecycleClosed
	d.ClosedAt = &now
	return nil
}

// Override transitions the plan to the overridden lifecycle state.
// Returns an error if the plan is already in a terminal state.
func (d *PlanDocument) Override() error {
	if d.Lifecycle.IsTerminal() {
		return fmt.Errorf("plan %q is already %s", d.SessionID, d.Lifecycle)
	}
	now := time.Now().UTC()
	d.Lifecycle = valueobject.PlanLifecycleOverridden
	d.ClosedAt = &now
	return nil
}

// IsActive reports whether the plan is in the active lifecycle state.
func (d *PlanDocument) IsActive() bool {
	return d.Lifecycle == valueobject.PlanLifecycleActive
}

// ToMarkdown serializes the PlanDocument as structured Markdown suitable
// for human review and file persistence.
func (d *PlanDocument) ToMarkdown() string {
	var sb strings.Builder

	sb.WriteString("# Plan: " + d.SessionID + "\n\n")
	sb.WriteString("**Status:** " + d.Lifecycle.String() + "\n")
	sb.WriteString("**Created:** " + d.CreatedAt.Format(time.RFC3339) + "\n")
	if d.ClosedAt != nil {
		sb.WriteString("**Closed:** " + d.ClosedAt.Format(time.RFC3339) + "\n")
	}
	sb.WriteString(fmt.Sprintf("**Score:** %.2f\n", d.Score))
	sb.WriteString("\n## Task\n\n" + d.Task + "\n")

	// Team section.
	if d.Team.NeedsTeam && len(d.Team.Roles) > 0 {
		sb.WriteString("\n## Team\n\n")
		sb.WriteString("| Role | Perspective |\n")
		sb.WriteString("|------|-------------|\n")
		for _, r := range d.Team.Roles {
			sb.WriteString(fmt.Sprintf("| %s | %s |\n", r.Name, r.Perspective))
		}
	}

	// Graph section.
	if d.Graph != nil && len(d.Graph.Tasks) > 0 {
		sb.WriteString("\n## Execution Graph\n\n")
		sb.WriteString("| # | Task ID | Description | Depends On |\n")
		sb.WriteString("|---|---------|-------------|------------|\n")
		for i, t := range d.Graph.Tasks {
			deps := "—"
			if len(t.DependsOn) > 0 {
				deps = strings.Join(t.DependsOn, ", ")
			}
			sb.WriteString(fmt.Sprintf("| %d | %s | %s | %s |\n", i+1, t.ID, t.Description, deps))
		}
	}

	// Feedback section.
	if d.Feedback != "" {
		sb.WriteString("\n## Evaluator Feedback\n\n" + d.Feedback + "\n")
	}

	return sb.String()
}
