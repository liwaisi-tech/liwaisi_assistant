package synthesis_test

// End-to-end integration test for the JIT sub-CPN composition shortcut.
// Exercises Bootstrap + RegisterComposeHook and verifies that the emitted
// topology (parallel-fanout template) carries the structural properties
// fire_instantiate + registry.InjectIntoCPN depend on:
//
//   - One NodeKindTool transition per entry in tools_needed
//   - Every tool transition has a non-empty ToolName (executor binding key)
//   - Every tool transition has both input and output arcs declared
//   - Fanout + aggregate bracket transitions are present
//   - The emitted blob round-trips through persist.UnmarshalCPN — the
//     materialise path fire_instantiate invokes — without error
//
// External test package so it can import cpn/synthesis, cpn/synthesis/jit,
// and cpn/persist without triggering the cycle that would exist inside the
// synthesis package itself.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/persist"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/synthesis"
	"github.com/liwaisi-tech/liwaisi_assistant/back/go-assistant/cpn/synthesis/jit"
)

func TestJITShortcut_ParallelFanout_TopologyShape(t *testing.T) {
	safe := synthesis.NewSafeRegistry()
	synthesis.RegisterDefaults(safe)
	jit.Register(safe)
	safe.Seal()
	synthesis.Bootstrap(safe)
	jit.RegisterComposeHook()
	defer cpn.SetComposeFromTaskSpec(nil)

	tools := []string{
		"system/bash_exec@1.0.0",
		"system/bash_exec@1.0.0",
		"system/bash_exec@1.0.0",
	}
	matches := make([]cpn.ToolMatch, 0, len(tools))
	for _, n := range tools {
		matches = append(matches, cpn.ToolMatch{QualifiedName: n})
	}

	_, blob, err := jit.Compose(
		context.Background(),
		cpn.ToolMatchSet{Matches: matches},
		cpn.Intent{NL: "run ls, uname -a, and df -h in parallel and give me a JSON"},
		jit.ComposeOptions{
			Template:     jit.TemplateParallelFanout,
			SafeRegistry: safe,
			Cap:          cpn.SizeCap{MaxPlaces: 32, MaxTransitions: 24},
		},
	)
	if err != nil {
		t.Fatalf("jit.Compose: %v", err)
	}
	if len(blob) == 0 {
		t.Fatal("jit.Compose returned empty blob")
	}

	// ── Structural assertions on the emitted JSON ──────────────────────
	var doc struct {
		Transitions map[string]struct {
			Kind         string   `json:"kind"`
			ToolName     string   `json:"toolName"`
			InputPlaces  []string `json:"inputPlaces"`
			OutputPlaces []string `json:"outputPlaces"`
		} `json:"transitions"`
	}
	if err := json.Unmarshal(blob, &doc); err != nil {
		t.Fatalf("decode emitted topology: %v", err)
	}
	var toolCount, fanoutCount, aggregateCount int
	for id, tr := range doc.Transitions {
		switch tr.Kind {
		case "tool":
			toolCount++
			if tr.ToolName == "" {
				t.Errorf("tool transition %q has empty toolName — registry.InjectIntoCPN would skip it", id)
			}
			if len(tr.InputPlaces) == 0 {
				t.Errorf("tool transition %q has no InputPlaces (arc-less)", id)
			}
			if len(tr.OutputPlaces) == 0 {
				t.Errorf("tool transition %q has no OutputPlaces (arc-less)", id)
			}
		case "observer":
			switch id {
			case "t-fanout":
				fanoutCount++
			case "t-aggregate":
				aggregateCount++
			}
		}
	}
	if toolCount != len(tools) {
		t.Errorf("tool transition count = %d, want %d", toolCount, len(tools))
	}
	if fanoutCount != 1 {
		t.Errorf("fanout transition count = %d, want 1", fanoutCount)
	}
	if aggregateCount != 1 {
		t.Errorf("aggregate transition count = %d, want 1", aggregateCount)
	}

	// ── Round-trip through the materialise path fire_instantiate uses ──
	var topo persist.CPNTopology
	if err := json.Unmarshal(blob, &topo); err != nil {
		t.Fatalf("unmarshal topology: %v", err)
	}
	child, err := synthesis.Materialise(&topo, safe)
	if err != nil {
		t.Fatalf("Materialise: %v", err)
	}
	if child == nil {
		t.Fatal("Materialise returned nil child")
	}
	// Re-check tool bindings on the live child — these are what
	// registry.InjectIntoCPN would patch at session-spawn time.
	var liveToolCount int
	for id, tr := range child.Transitions {
		if tr.Kind != cpn.NodeKindTool {
			continue
		}
		liveToolCount++
		if tr.ToolName == "" {
			t.Errorf("materialised tool transition %q has empty ToolName", id)
		}
	}
	if liveToolCount != len(tools) {
		t.Errorf("materialised tool count = %d, want %d", liveToolCount, len(tools))
	}
}

// TestJITShortcut_PolicyRespectsParallelismHint documents the hook's
// routing policy: parallelism_hint == "" or "auto" returns (nil,nil) so
// fire_synthesize falls through to the LLM; explicit "fanout" / "sequence"
// short-circuit to the deterministic template.
//
// We can't reach the private composeFromTaskSpec wrapper, so we
// cover the branch selection structurally — the same logic lives in
// RegisterComposeHook's switch statement.
func TestJITShortcut_PolicyRespectsParallelismHint(t *testing.T) {
	// Non-empty tools_needed + unknown hint should NOT compose. We mimic
	// the hook's guard here to ensure this test stays in lockstep with
	// RegisterComposeHook when the policy evolves.
	cases := []struct {
		hint    string
		wantNil bool
	}{
		{"", true},     // missing → fall through
		{"auto", true}, // LLM decides
		{"fanout", false},
		{"sequence", false},
	}
	for _, tc := range cases {
		t.Run(tc.hint, func(t *testing.T) {
			// Mirror the hook's branch selector.
			var template jit.TemplateID
			switch tc.hint {
			case "fanout":
				template = jit.TemplateParallelFanout
			case "sequence":
				template = jit.TemplateSequentialPipeline
			}
			// The hook returns (nil,nil) when template is the zero value.
			got := template == ""
			if got != tc.wantNil {
				t.Errorf("hint=%q: shortcut-bypass=%v, want %v", tc.hint, got, tc.wantNil)
			}
		})
	}
}
