package cpn

import (
	"context"
	"testing"
	"time"
)

// ── Paper Section 4.1: Centaurian Topology Tests ────────────────────────────
// Tests validate the Centaurian paradigm — deep human-AI co-trigger where
// computation transitions require BOTH human-origin AND AI-origin tokens.

// TestCentaurianTopology_MultiStepApprovalWorkflow validates two sequential
// computation transitions, each gated by centaurianGuard. All tokens are
// pre-loaded to isolate the co-trigger mechanism.
//
// Topology (all in Computation space):
//
//	P:PLAN_1 (ARTIFACT, AI) + P:HUMAN_1 (HUMAN) → [T:01_DECIDE (Tool)] → P:MID (ARTIFACT)
//	P:MID + P:HUMAN_2 (HUMAN) → [T:02_FINALIZE (Tool)] → P:FINAL (ARTIFACT)
func TestCentaurianTopology_MultiStepApprovalWorkflow(t *testing.T) {
	t.Parallel()

	places := newPlaces(
		placeSpec{"P:PLAN_1", ColorArtifact, SpaceComputation},
		placeSpec{"P:HUMAN_1", ColorHuman, SpaceComputation},
		placeSpec{"P:MID", ColorArtifact, SpaceComputation},
		placeSpec{"P:HUMAN_2", ColorHuman, SpaceComputation},
		placeSpec{"P:FINAL", ColorArtifact, SpaceComputation},
	)

	// Pre-load all tokens.
	_ = places["P:PLAN_1"].Deposit(&Token{
		Color: ColorArtifact, Space: SpaceComputation, Payload: "plan",
		OriginKind: NodeKindLLM,
	})
	_ = places["P:HUMAN_1"].Deposit(humanToken())
	_ = places["P:HUMAN_2"].Deposit(humanToken())

	transitions := newTransitions(
		transSpec{
			ID: "T:01_DECIDE", Kind: NodeKindTool,
			Inputs: []string{"P:PLAN_1", "P:HUMAN_1"}, Outputs: []string{"P:MID"},
			Executor: prefixTool("decided:", ColorArtifact, SpaceComputation),
		},
		transSpec{
			ID: "T:02_FINALIZE", Kind: NodeKindTool,
			Inputs: []string{"P:MID", "P:HUMAN_2"}, Outputs: []string{"P:FINAL"},
			Executor: prefixTool("finalized:", ColorArtifact, SpaceComputation),
		},
	)

	c := NewCPN("centaurian-multi", "test", 0, ModeCentaurian, "sess-1", places, transitions)

	err := runWithTimeout(t, c, 5*time.Second)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertCompleted(t, c)
	assertPlaceLen(t, places["P:FINAL"], 1)
}

// TestCentaurianTopology_ModeSwitchDuringExecution validates dynamic mode
// switching: MAS → Centaurian (via HITL space bridge) → computation fires →
// mode reverts to MAS.
//
// Topology:
//
//	P:QUERY (STRING, Surface) → [T:01_HITL (HITL)] → P:APPROVED (HUMAN, Computation)
//	P:WORK  (ARTIFACT, Computation, pre-loaded AI)
//	P:APPROVED + P:WORK → [T:02_COMPUTE (Tool)] → P:RESULT (ARTIFACT, Computation)
func TestCentaurianTopology_ModeSwitchDuringExecution(t *testing.T) {
	t.Parallel()

	hitlCh := make(chan Token, 1)
	ec := &threadSafeEventCollector{}

	places := newPlaces(
		placeSpec{"P:QUERY", ColorString, SpaceSurface},
		placeSpec{"P:APPROVED", ColorHuman, SpaceComputation},
		placeSpec{"P:WORK", ColorArtifact, SpaceComputation},
		placeSpec{"P:RESULT", ColorArtifact, SpaceComputation},
	)
	_ = places["P:QUERY"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "query"})
	_ = places["P:WORK"].Deposit(&Token{
		Color: ColorArtifact, Space: SpaceComputation, Payload: "ai-work",
		OriginKind: NodeKindLLM,
	})

	transitions := map[string]*Transition{
		"T:01_HITL": {
			ID: "T:01_HITL", Kind: NodeKindHITL,
			InputPlaces: []string{"P:QUERY"}, OutputPlaces: []string{"P:APPROVED"},
			HITLConfig: &HITLConfig{Channel: hitlCh, Prompt: "Approve?"},
		},
		"T:02_COMPUTE": {
			ID: "T:02_COMPUTE", Kind: NodeKindTool,
			InputPlaces:  []string{"P:APPROVED", "P:WORK"},
			OutputPlaces: []string{"P:RESULT"},
			Executor:     prefixTool("computed:", ColorArtifact, SpaceComputation),
		},
	}

	c := NewCPN("mode-switch", "test", 0, ModeMAS, "sess-1", places, transitions)
	c.EventSink = ec.sink

	feedHITLAsync(hitlCh, 50*time.Millisecond, makeApproveToken())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := c.Run(ctx)

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertCompleted(t, c)
	assertPlaceLen(t, places["P:RESULT"], 1)

	// Verify mode switch events: MAS→Centaurian, then Centaurian→MAS.
	events := ec.getEvents()
	modeEvents := collectModeEvents(events)

	hasMAStoC := false
	hasCtoMAS := false
	for _, me := range modeEvents {
		if me["from"] == ModeMAS && me["to"] == ModeCentaurian {
			hasMAStoC = true
		}
		if me["from"] == ModeCentaurian && me["to"] == ModeMAS {
			hasCtoMAS = true
		}
	}
	if !hasMAStoC {
		t.Fatal("expected MAS→Centaurian mode switch")
	}
	if !hasCtoMAS {
		t.Fatal("expected Centaurian→MAS mode switch (after human token consumed)")
	}
}

// TestCentaurianTopology_DeadlockWithoutHumanCoTrigger validates that in
// Centaurian mode, computation transitions deadlock without human tokens.
func TestCentaurianTopology_DeadlockWithoutHumanCoTrigger(t *testing.T) {
	t.Parallel()

	places := newPlaces(
		placeSpec{"P:IN", ColorArtifact, SpaceComputation},
		placeSpec{"P:OUT", ColorArtifact, SpaceComputation},
	)
	_ = places["P:IN"].Deposit(&Token{
		Color: ColorArtifact, Space: SpaceComputation, Payload: "ai-only",
		OriginKind: NodeKindLLM,
	})

	transitions := newTransitions(transSpec{
		ID: "T:ACT", Kind: NodeKindTool,
		Inputs: []string{"P:IN"}, Outputs: []string{"P:OUT"},
		Executor: prefixTool("done:", ColorArtifact, SpaceComputation),
	})

	c := NewCPN("deadlock-test", "test", 0, ModeCentaurian, "sess-1", places, transitions)

	err := runWithTimeout(t, c, 1*time.Second)
	assertDeadlock(t, err)

	// P:OUT should remain empty — transition was blocked.
	assertPlaceLen(t, places["P:OUT"], 0)
}
