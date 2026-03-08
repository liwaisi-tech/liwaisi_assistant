package entity

import (
	"strings"
	"testing"
	"time"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/internal/domain/valueobject"
)

func TestNewPlanDocument(t *testing.T) {
	team := valueobject.TeamEvaluation{NeedsTeam: true, Roles: []valueobject.RoleSpec{{Name: "architect"}}}
	graph := &PlanGraph{Tasks: []*MicroTask{{ID: "t1", Description: "do t1"}}}
	doc := NewPlanDocument("session-1", "build a thing", team, graph, 0.85, "looks good")

	if doc.SessionID != "session-1" {
		t.Errorf("SessionID = %q, want %q", doc.SessionID, "session-1")
	}
	if doc.Lifecycle != valueobject.PlanLifecycleActive {
		t.Errorf("Lifecycle = %q, want %q", doc.Lifecycle, valueobject.PlanLifecycleActive)
	}
	if doc.ClosedAt != nil {
		t.Error("ClosedAt should be nil for a new document")
	}
	if !doc.IsActive() {
		t.Error("IsActive() = false, want true")
	}
}

func TestPlanDocument_Close(t *testing.T) {
	doc := NewPlanDocument("s1", "task", valueobject.TeamEvaluation{}, nil, 0.9, "")

	if err := doc.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if doc.Lifecycle != valueobject.PlanLifecycleClosed {
		t.Errorf("Lifecycle = %q, want %q", doc.Lifecycle, valueobject.PlanLifecycleClosed)
	}
	if doc.ClosedAt == nil {
		t.Error("ClosedAt should be set after Close()")
	}
	if doc.IsActive() {
		t.Error("IsActive() = true after Close(), want false")
	}

	// Double-close should error.
	if err := doc.Close(); err == nil {
		t.Error("second Close() should return error")
	}
}

func TestPlanDocument_Override(t *testing.T) {
	doc := NewPlanDocument("s1", "task", valueobject.TeamEvaluation{}, nil, 0.9, "")

	if err := doc.Override(); err != nil {
		t.Fatalf("Override() error = %v", err)
	}
	if doc.Lifecycle != valueobject.PlanLifecycleOverridden {
		t.Errorf("Lifecycle = %q, want %q", doc.Lifecycle, valueobject.PlanLifecycleOverridden)
	}

	// Overriding a terminal plan should error.
	if err := doc.Override(); err == nil {
		t.Error("Override() on terminal plan should return error")
	}
}

func TestPlanDocument_CloseAfterOverride(t *testing.T) {
	doc := NewPlanDocument("s1", "task", valueobject.TeamEvaluation{}, nil, 0.9, "")
	_ = doc.Override()

	if err := doc.Close(); err == nil {
		t.Error("Close() after Override() should return error")
	}
}

func TestPlanDocument_ToMarkdown(t *testing.T) {
	team := valueobject.TeamEvaluation{
		NeedsTeam: true,
		Roles: []valueobject.RoleSpec{
			{Name: "architect", Perspective: "system design"},
			{Name: "qa-engineer", Perspective: "quality assurance"},
		},
	}
	graph := &PlanGraph{Tasks: []*MicroTask{
		{ID: "t1", Description: "analyze requirements"},
		{ID: "t2", Description: "implement solution", DependsOn: []string{"t1"}},
	}}
	doc := NewPlanDocument("plan-123", "build the feature", team, graph, 0.82, "generally solid plan")
	doc.CreatedAt = time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)

	md := doc.ToMarkdown()

	checks := []string{
		"# Plan: plan-123",
		"**Status:** active",
		"**Score:** 0.82",
		"build the feature",
		"| architect | system design |",
		"| qa-engineer | quality assurance |",
		"| t1 | analyze requirements | — |",
		"| t2 | implement solution | t1 |",
		"generally solid plan",
	}

	for _, check := range checks {
		if !strings.Contains(md, check) {
			t.Errorf("ToMarkdown() missing: %q", check)
		}
	}
}

func TestPlanDocument_ToMarkdown_NoTeam(t *testing.T) {
	doc := NewPlanDocument("s1", "simple task", valueobject.TeamEvaluation{NeedsTeam: false}, nil, 0.95, "")
	md := doc.ToMarkdown()

	if strings.Contains(md, "## Team") {
		t.Error("ToMarkdown() should not include Team section when NeedsTeam is false")
	}
}
