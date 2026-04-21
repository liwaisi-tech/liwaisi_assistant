package awakens

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPromptPlan_ContainsExample asserts the worked example is embedded in
// the plan system prompt so the LLM has a concrete shape to mimic
// (spec §3 REQ-009). This test must fail if someone edits the prompt and
// drops or detaches the example block.
func TestPromptPlan_ContainsExample(t *testing.T) {
	t.Parallel()
	if !strings.Contains(SystemPromptPlan, ExamplePlan) {
		t.Fatal("SystemPromptPlan does not embed ExamplePlan verbatim; REQ-009 broken")
	}
	if !strings.Contains(SystemPromptPlan, "AwakeningProbePlan") && !strings.Contains(SystemPromptPlan, `"probes"`) {
		t.Error("SystemPromptPlan should reference the probe-plan schema")
	}
	if !strings.Contains(SystemPromptPlan, "MaxProbes=16") && !strings.Contains(SystemPromptPlan, "16 probes") {
		t.Error("SystemPromptPlan should advertise the MaxProbes=16 cap")
	}
}

// TestPromptPlan_ExampleParsesAsStructurallyValidPlan decodes ExamplePlan
// into the shape the composer will validate. This is the package-local
// structural mirror of fanout.ValidatePlan; it catches broken JSON and
// out-of-range fields without taking an import dependency on
// cpn/awakens/fanout (which is still in flight — the full
// fanout.ValidatePlan round-trip lives in the build-tagged companion test
// prompt_plan_fanout_test.go and will activate once that package ships).
func TestPromptPlan_ExampleParsesAsStructurallyValidPlan(t *testing.T) {
	t.Parallel()

	type probeEntry struct {
		ID      string `json:"id"`
		Kind    string `json:"kind"`
		Target  string `json:"target"`
		Command string `json:"command"`
	}
	type plan struct {
		Rationale         string       `json:"rationale"`
		TimeoutPerProbeMs int          `json:"timeout_per_probe_ms"`
		Probes            []probeEntry `json:"probes"`
	}

	var p plan
	if err := json.Unmarshal([]byte(ExamplePlan), &p); err != nil {
		t.Fatalf("ExamplePlan is not valid JSON: %v", err)
	}

	// CON-002: MaxProbes=16. Prompt example is capped lower (<=6) to keep
	// token budget down per the prompt-author brief.
	if len(p.Probes) == 0 {
		t.Fatal("ExamplePlan has zero probes")
	}
	if len(p.Probes) > 16 {
		t.Fatalf("ExamplePlan probe count %d exceeds MaxProbes=16", len(p.Probes))
	}
	if len(p.Probes) > 6 {
		t.Errorf("ExamplePlan probe count %d exceeds the ≤6 prompt-budget guideline", len(p.Probes))
	}

	// Plan-level rationale: ≤160 chars per prompt contract.
	if p.Rationale == "" {
		t.Error("ExamplePlan rationale is empty")
	}
	if len(p.Rationale) > 160 {
		t.Errorf("ExamplePlan rationale %d chars exceeds 160-char contract", len(p.Rationale))
	}

	// Composer will clamp to [500, 2000]; the prompt asks the model to stay
	// inside that range already.
	if p.TimeoutPerProbeMs < 500 || p.TimeoutPerProbeMs > 2000 {
		t.Errorf("ExamplePlan timeout_per_probe_ms=%d outside [500,2000]", p.TimeoutPerProbeMs)
	}

	seen := make(map[string]bool, len(p.Probes))
	for i, pr := range p.Probes {
		if pr.ID == "" {
			t.Errorf("probe[%d] has empty id", i)
		}
		if seen[pr.ID] {
			t.Errorf("probe[%d] duplicate id %q", i, pr.ID)
		}
		seen[pr.ID] = true

		// GUD-002: short lowercase slug; prefer cmd-<name>.
		if pr.ID != strings.ToLower(pr.ID) {
			t.Errorf("probe[%d] id %q is not lowercase (GUD-002)", i, pr.ID)
		}
		if !strings.HasPrefix(pr.ID, "cmd-") {
			t.Errorf("probe[%d] id %q does not match cmd-<name> pattern (GUD-002)", i, pr.ID)
		}

		switch pr.Kind {
		case "binary", "capability":
			// ok
		default:
			t.Errorf("probe[%d] invalid kind %q (want binary|capability)", i, pr.Kind)
		}

		if pr.Target == "" {
			t.Errorf("probe[%d] empty target", i)
		}
		if pr.Command == "" {
			t.Errorf("probe[%d] empty command", i)
		}
		// CON-003: MaxCommandLen=256 bytes.
		if len(pr.Command) > 256 {
			t.Errorf("probe[%d] command %d bytes exceeds CON-003 256-byte cap", i, len(pr.Command))
		}
	}
}

// TestSelectSystemPrompt_Routes verifies the helper routes to the plan
// prompt by default and to the legacy prompt only for explicit legacy roles.
func TestSelectSystemPrompt_Routes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		role string
		want string
	}{
		{"", SystemPromptPlan},
		{"plan", SystemPromptPlan},
		{"unknown", SystemPromptPlan},
		{"followup", SystemPrompt},
		{"legacy", SystemPrompt},
		{"report", SystemPrompt},
	}
	for _, tc := range cases {
		if got := SelectSystemPrompt(tc.role); got != tc.want {
			t.Errorf("SelectSystemPrompt(%q): got prompt of length %d, want length %d",
				tc.role, len(got), len(tc.want))
		}
	}
}
