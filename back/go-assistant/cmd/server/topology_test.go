package main

import (
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
)

func TestHITLTopology_HasExpectedPlacesAndTransitions(t *testing.T) {
	c := hitlTopologyFactory("test-session")

	expectedPlaces := []string{"p-input", "p-plan", "p-reviewed", "p-output"}
	for _, id := range expectedPlaces {
		if _, ok := c.Places[id]; !ok {
			t.Errorf("missing place %q", id)
		}
	}

	expectedTransitions := []string{"t-plan", "t-review", "t-execute"}
	for _, id := range expectedTransitions {
		if _, ok := c.Transitions[id]; !ok {
			t.Errorf("missing transition %q", id)
		}
	}
}

func TestHITLTopology_HITLTransitionHasConfig(t *testing.T) {
	c := hitlTopologyFactory("test-session")

	tReview, ok := c.Transitions["t-review"]
	if !ok {
		t.Fatal("missing t-review transition")
	}

	if tReview.Kind != cpn.NodeKindHITL {
		t.Errorf("t-review kind = %q, want %q", tReview.Kind, cpn.NodeKindHITL)
	}

	if tReview.HITLConfig == nil {
		t.Fatal("t-review HITLConfig is nil")
	}

	if tReview.HITLConfig.Prompt == "" {
		t.Error("t-review HITLConfig.Prompt is empty")
	}

	// Channel must be nil — wired by SessionService.CreateSession.
	if tReview.HITLConfig.Channel != nil {
		t.Error("t-review HITLConfig.Channel should be nil (wired by session service)")
	}
}

func TestHITLTopology_PassesValidationAfterWiring(t *testing.T) {
	c := hitlTopologyFactory("test-session")

	// Simulate what SessionService.CreateSession does: wire HITL channels.
	for _, tr := range c.Transitions {
		if tr.Kind == cpn.NodeKindHITL && tr.HITLConfig != nil {
			tr.HITLConfig.Channel = make(chan cpn.Token, 1)
		}
	}

	if err := cpn.Validate(c.Places, c.Transitions); err != nil {
		t.Errorf("topology validation failed: %v", err)
	}
}

func TestHITLTopology_SpaceAssignment(t *testing.T) {
	c := hitlTopologyFactory("test-session")

	tests := []struct {
		place string
		space cpn.SpaceKind
	}{
		{"p-input", cpn.SpaceSurface},
		{"p-plan", cpn.SpaceSurface},
		{"p-reviewed", cpn.SpaceComputation},
		{"p-output", cpn.SpaceSurface},
	}

	for _, tt := range tests {
		p, ok := c.Places[tt.place]
		if !ok {
			t.Errorf("missing place %q", tt.place)
			continue
		}
		if p.Space != tt.space {
			t.Errorf("place %q space = %q, want %q", tt.place, p.Space, tt.space)
		}
	}
}

func TestDefaultTopology_StillWorks(t *testing.T) {
	c := defaultTopologyFactory("test-session")

	if _, ok := c.Places["p-input"]; !ok {
		t.Error("missing p-input")
	}
	if _, ok := c.Places["p-output"]; !ok {
		t.Error("missing p-output")
	}
	if _, ok := c.Transitions["t-llm"]; !ok {
		t.Error("missing t-llm")
	}

	if err := cpn.Validate(c.Places, c.Transitions); err != nil {
		t.Errorf("topology validation failed: %v", err)
	}
}
