package cpn

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestTransitionStartedPayload_OmitsNilDisplayLabel(t *testing.T) {
	p := TransitionStartedPayload{InputTokens: []TokenSnapshot{}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "display_label") {
		t.Errorf("display_label should be absent when nil, got %s", string(b))
	}
}

func TestTransitionStartedPayload_IncludesDisplayLabelWhenSet(t *testing.T) {
	p := TransitionStartedPayload{
		InputTokens:  []TokenSnapshot{},
		DisplayLabel: &DisplayLabel{Verb: "Thinking"},
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"display_label":{"verb":"Thinking"}`) {
		t.Errorf("expected display_label in JSON, got %s", string(b))
	}
}

func TestTransitionCompletedPayload_OmitsNilDisplayLabel(t *testing.T) {
	p := TransitionCompletedPayload{}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "display_label") {
		t.Errorf("display_label should be absent when nil, got %s", string(b))
	}
}

// TestEmit_ToolTransitionAttachesDisplayLabel verifies the executor emit
// path attaches a DisplayLabel for a non-silent Tool transition (REQ-008).
func TestEmit_ToolTransitionAttachesDisplayLabel(t *testing.T) {
	c := buildSimpleCPN(identityTool())
	c.Transitions["T:TOOL"].ToolName = "web_search"

	events := runAndCapture(t, c)

	var startedDL *DisplayLabel
	for _, e := range events {
		if e.Type == EventTransitionStarted {
			if p, ok := e.Payload.(TransitionStartedPayload); ok {
				startedDL = p.DisplayLabel
				break
			}
		}
	}
	if startedDL == nil {
		t.Fatal("expected DisplayLabel on transition_started for Tool")
	}
	if startedDL.Verb != "Searching" {
		t.Errorf("verb = %q, want %q", startedDL.Verb, "Searching")
	}
}

// TestEmit_ToolTransitionAttachesDisplayLabelOnCompleted verifies the
// completed event also carries the DisplayLabel so the UI can match
// start/complete pairs even when the start was coalesced (REQ-003).
func TestEmit_ToolTransitionAttachesDisplayLabelOnCompleted(t *testing.T) {
	c := buildSimpleCPN(identityTool())
	c.Transitions["T:TOOL"].ToolName = "fs_read"

	events := runAndCapture(t, c)

	var completedDL *DisplayLabel
	for _, e := range events {
		if e.Type == EventTransitionCompleted {
			if p, ok := e.Payload.(TransitionCompletedPayload); ok {
				completedDL = p.DisplayLabel
				break
			}
		}
	}
	if completedDL == nil {
		t.Fatal("expected DisplayLabel on transition_completed")
	}
	if completedDL.Verb != "Reading" {
		t.Errorf("verb = %q, want %q", completedDL.Verb, "Reading")
	}
}

// TestEmit_SilentTransitionOmitsDisplayLabel verifies REQ-006: when a
// transition is marked Silent, its lifecycle events MUST NOT carry a
// DisplayLabel.
func TestEmit_SilentTransitionOmitsDisplayLabel(t *testing.T) {
	c := buildSimpleCPN(identityTool())
	c.Transitions["T:TOOL"].Silent = true
	c.Transitions["T:TOOL"].ToolName = "web_search"

	events := runAndCapture(t, c)

	for _, e := range events {
		switch p := e.Payload.(type) {
		case TransitionStartedPayload:
			if p.DisplayLabel != nil {
				t.Errorf("silent transition leaked DisplayLabel on %s: %+v", e.Type, p.DisplayLabel)
			}
		case TransitionCompletedPayload:
			if p.DisplayLabel != nil {
				t.Errorf("silent transition leaked DisplayLabel on %s: %+v", e.Type, p.DisplayLabel)
			}
		}
	}
}

// runAndCapture runs the CPN to completion and returns every event emitted
// via EventSink, in order.
func runAndCapture(t *testing.T, c *CPN) []*Event {
	t.Helper()
	var mu sync.Mutex
	var captured []*Event
	c.EventSink = func(e *Event) {
		mu.Lock()
		captured = append(captured, e)
		mu.Unlock()
	}

	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	out := make([]*Event, len(captured))
	copy(out, captured)
	return out
}
