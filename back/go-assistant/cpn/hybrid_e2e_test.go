package cpn

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// ── Paper Section 4.3: Hybrid MAS + Centaurian E2E Tests ────────────────────
// Tests validate dynamic mode switching between MAS and Centaurian during a
// single CPN execution, exercising both paradigms in one workflow.

// TestHybrid_MASAnalyze_CentaurianReview_MASFormat validates the full
// paradigm lifecycle from the paper:
//
//	Phase 1 (MAS): Autonomous analysis
//	Phase 2 (HITL → Centaurian): Human review gates computation
//	Phase 3 (Centaurian → MAS): After co-triggered synthesis, mode reverts
//	Phase 4 (MAS): Autonomous formatting
//
// Topology:
//
//	P:QUERY (Surface) → [T:01_ANALYZE (Tool)] → P:ANALYSIS (Computation)
//	P:ANALYSIS → [T:02_PREPARE (Tool)] → P:REVIEW_PROMPT (Surface)
//	P:REVIEW_PROMPT → [T:03_HITL (HITL)] → P:REVIEWED (HUMAN, Computation)
//	P:WORK (ARTIFACT, Computation, pre-loaded AI) + P:REVIEWED → [T:04_SYNTHESIZE (Tool)] → P:SYNTHESIS (Computation)
//	P:SYNTHESIS → [T:05_FORMAT (Tool)] → P:REPORT (Computation)
func TestHybrid_MASAnalyze_CentaurianReview_MASFormat(t *testing.T) {
	t.Parallel()

	hitlCh := make(chan Token, 1)
	ec := &threadSafeEventCollector{}

	// All Computation-space places for the main chain; only the HITL bridge
	// touches Surface. This avoids Surface→Computation Validate violations.
	places := newPlaces(
		placeSpec{"P:QUERY", ColorString, SpaceComputation},
		placeSpec{"P:ANALYSIS", ColorString, SpaceComputation},
		placeSpec{"P:REVIEW_PROMPT", ColorString, SpaceSurface},
		placeSpec{"P:REVIEWED", ColorHuman, SpaceComputation},
		placeSpec{"P:WORK", ColorArtifact, SpaceComputation},
		placeSpec{"P:SYNTHESIS", ColorArtifact, SpaceComputation},
		placeSpec{"P:REPORT", ColorArtifact, SpaceComputation},
	)
	_ = places["P:QUERY"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "user question"})
	_ = places["P:WORK"].Deposit(&Token{
		Color: ColorArtifact, Space: SpaceComputation, Payload: "ai-research",
		OriginKind: NodeKindLLM,
	})

	transitions := map[string]*Transition{
		// Phase 1 (MAS): Analyze query (Computation → Computation, then bridge to Surface).
		"T:01_ANALYZE": {
			ID: "T:01_ANALYZE", Kind: NodeKindTool,
			InputPlaces: []string{"P:QUERY"}, OutputPlaces: []string{"P:ANALYSIS"},
			Executor: prefixTool("analyzed:", ColorString, SpaceComputation),
		},
		// Bridge: Computation → Surface for HITL.
		"T:02_PREPARE": {
			ID: "T:02_PREPARE", Kind: NodeKindTool,
			InputPlaces:  []string{"P:ANALYSIS"},
			OutputPlaces: []string{"P:REVIEW_PROMPT"},
			Executor:     passThroughTool(ColorString, SpaceSurface),
		},
		// Phase 2: HITL bridges Surface → Computation (triggers Centaurian).
		"T:03_HITL": {
			ID: "T:03_HITL", Kind: NodeKindHITL,
			InputPlaces: []string{"P:REVIEW_PROMPT"}, OutputPlaces: []string{"P:REVIEWED"},
			HITLConfig: &HITLConfig{Channel: hitlCh, Prompt: "Review analysis?"},
		},
		// Phase 3 (Centaurian): Co-triggered synthesis — needs both human + AI.
		"T:04_SYNTHESIZE": {
			ID: "T:04_SYNTHESIZE", Kind: NodeKindTool,
			InputPlaces:  []string{"P:REVIEWED", "P:WORK"},
			OutputPlaces: []string{"P:SYNTHESIS"},
			Executor:     prefixTool("synthesized:", ColorArtifact, SpaceComputation),
		},
		// Phase 4 (MAS after human token consumed): Autonomous formatting.
		"T:05_FORMAT": {
			ID: "T:05_FORMAT", Kind: NodeKindTool,
			InputPlaces:  []string{"P:SYNTHESIS"},
			OutputPlaces: []string{"P:REPORT"},
			Executor:     prefixTool("formatted:", ColorArtifact, SpaceComputation),
		},
	}

	c := NewCPN("hybrid-e2e", "coordinator", 0, ModeMAS, "sess-1", places, transitions)
	c.EventSink = ec.sink

	feedHITLAsync(hitlCh, 100*time.Millisecond, makeApproveToken())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := c.Run(ctx)

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertCompleted(t, c)

	// P:REPORT should have the final formatted result.
	assertPlaceLen(t, places["P:REPORT"], 1)

	// Verify mode switch events.
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
		t.Fatal("expected MAS→Centaurian mode switch when HITL deposits to Computation")
	}
	if !hasCtoMAS {
		t.Fatal("expected Centaurian→MAS mode switch after human token consumed by T:04_SYNTHESIZE")
	}
}

// TestHybrid_MultipleHITLCheckpoints validates two HITL checkpoints in a single
// execution, each triggering a separate Centaurian episode.
//
// Topology (two Centaurian episodes connected by a Computation→Surface bridge):
//
//	P:INPUT (Computation) → [T:01_HITL_IN (Surface)] is not possible; instead:
//	P:INPUT (Surface) → [T:01_HITL_1 (HITL)] → P:STAGE1 (HUMAN, Computation)
//	P:WORK_1 (AI, Computation) + P:STAGE1 → [T:02_EXEC_1 (Tool)] → P:MID (Computation)
//	P:MID → [T:03_BRIDGE (Tool)] → P:PROMPT_2 (Surface)
//	P:PROMPT_2 → [T:04_HITL_2 (HITL)] → P:STAGE2 (HUMAN, Computation)
//	P:WORK_2 (AI, Computation) + P:STAGE2 → [T:05_EXEC_2 (Tool)] → P:FINAL (Computation)
func TestHybrid_MultipleHITLCheckpoints(t *testing.T) {
	t.Parallel()

	hitlCh1 := make(chan Token, 1)
	hitlCh2 := make(chan Token, 1)
	ec := &threadSafeEventCollector{}

	places := newPlaces(
		placeSpec{"P:INPUT", ColorString, SpaceSurface},
		placeSpec{"P:STAGE1", ColorHuman, SpaceComputation},
		placeSpec{"P:WORK_1", ColorArtifact, SpaceComputation},
		placeSpec{"P:MID", ColorString, SpaceComputation},
		placeSpec{"P:PROMPT_2", ColorString, SpaceSurface},
		placeSpec{"P:STAGE2", ColorHuman, SpaceComputation},
		placeSpec{"P:WORK_2", ColorArtifact, SpaceComputation},
		placeSpec{"P:FINAL", ColorArtifact, SpaceComputation},
	)
	_ = places["P:INPUT"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "start"})
	_ = places["P:WORK_1"].Deposit(&Token{
		Color: ColorArtifact, Space: SpaceComputation, Payload: "w1",
		OriginKind: NodeKindLLM,
	})
	_ = places["P:WORK_2"].Deposit(&Token{
		Color: ColorArtifact, Space: SpaceComputation, Payload: "w2",
		OriginKind: NodeKindLLM,
	})

	transitions := map[string]*Transition{
		"T:01_HITL_1": {
			ID: "T:01_HITL_1", Kind: NodeKindHITL,
			InputPlaces: []string{"P:INPUT"}, OutputPlaces: []string{"P:STAGE1"},
			HITLConfig: &HITLConfig{Channel: hitlCh1, Prompt: "Checkpoint 1?"},
		},
		"T:02_EXEC_1": {
			ID: "T:02_EXEC_1", Kind: NodeKindTool,
			InputPlaces:  []string{"P:STAGE1", "P:WORK_1"},
			OutputPlaces: []string{"P:MID"},
			Executor:     prefixTool("exec1:", ColorString, SpaceComputation),
		},
		"T:03_BRIDGE": {
			ID: "T:03_BRIDGE", Kind: NodeKindTool,
			InputPlaces:  []string{"P:MID"},
			OutputPlaces: []string{"P:PROMPT_2"},
			Executor:     passThroughTool(ColorString, SpaceSurface),
		},
		"T:04_HITL_2": {
			ID: "T:04_HITL_2", Kind: NodeKindHITL,
			InputPlaces: []string{"P:PROMPT_2"}, OutputPlaces: []string{"P:STAGE2"},
			HITLConfig: &HITLConfig{Channel: hitlCh2, Prompt: "Checkpoint 2?"},
		},
		"T:05_EXEC_2": {
			ID: "T:05_EXEC_2", Kind: NodeKindTool,
			InputPlaces:  []string{"P:STAGE2", "P:WORK_2"},
			OutputPlaces: []string{"P:FINAL"},
			Executor:     prefixTool("exec2:", ColorArtifact, SpaceComputation),
		},
	}

	c := NewCPN("multi-hitl", "test", 0, ModeMAS, "sess-1", places, transitions)
	c.EventSink = ec.sink

	feedHITLAsync(hitlCh1, 50*time.Millisecond, makeApproveToken())
	feedHITLAsync(hitlCh2, 300*time.Millisecond, makeApproveToken())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := c.Run(ctx)

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertCompleted(t, c)
	assertPlaceLen(t, places["P:FINAL"], 1)

	// Verify two Centaurian episodes via mode switch events.
	events := ec.getEvents()
	modeEvents := collectModeEvents(events)
	masToC := 0
	cToMAS := 0
	for _, me := range modeEvents {
		if me["from"] == ModeMAS && me["to"] == ModeCentaurian {
			masToC++
		}
		if me["from"] == ModeCentaurian && me["to"] == ModeMAS {
			cToMAS++
		}
	}
	if masToC < 2 {
		t.Fatalf("expected at least 2 MAS→Centaurian switches, got %d", masToC)
	}
	if cToMAS < 2 {
		t.Fatalf("expected at least 2 Centaurian→MAS switches, got %d", cToMAS)
	}
}

// TestHybrid_CentaurianDeadlockOnRejection validates that a HITL rejection
// in a hybrid workflow stops the entire CPN execution.
func TestHybrid_CentaurianDeadlockOnRejection(t *testing.T) {
	t.Parallel()

	hitlCh := make(chan Token, 1)

	places := newPlaces(
		placeSpec{"P:QUERY", ColorString, SpaceSurface},
		placeSpec{"P:APPROVED", ColorHuman, SpaceComputation},
		placeSpec{"P:WORK", ColorArtifact, SpaceComputation},
		placeSpec{"P:RESULT", ColorArtifact, SpaceComputation},
	)
	_ = places["P:QUERY"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "query"})
	_ = places["P:WORK"].Deposit(&Token{
		Color: ColorArtifact, Space: SpaceComputation, Payload: "ai",
		OriginKind: NodeKindLLM,
	})

	transitions := map[string]*Transition{
		"T:01_HITL": {
			ID: "T:01_HITL", Kind: NodeKindHITL,
			InputPlaces: []string{"P:QUERY"}, OutputPlaces: []string{"P:APPROVED"},
			HITLConfig: &HITLConfig{Channel: hitlCh, Prompt: "Approve?"},
		},
		"T:02_EXEC": {
			ID: "T:02_EXEC", Kind: NodeKindTool,
			InputPlaces:  []string{"P:APPROVED", "P:WORK"},
			OutputPlaces: []string{"P:RESULT"},
			Executor:     prefixTool("done:", ColorArtifact, SpaceComputation),
		},
	}

	c := NewCPN("reject-hybrid", "test", 0, ModeMAS, "sess-1", places, transitions)

	hitlCh <- makeRejectToken()

	err := runWithTimeout(t, c, 5*time.Second)
	if !errors.Is(err, ErrHITLRejected) {
		t.Fatalf("expected ErrHITLRejected, got: %v", err)
	}
	assertPlaceLen(t, places["P:RESULT"], 0)
}

// TestHybrid_SubNetInCentaurianContext validates that a SubNet transition
// fires in Centaurian mode when both human and AI tokens are present in
// its input places. The child CPN runs independently in MAS mode.
func TestHybrid_SubNetInCentaurianContext(t *testing.T) {
	t.Parallel()

	places := newPlaces(
		placeSpec{"P:PLAN", ColorArtifact, SpaceComputation},
		placeSpec{"P:HUMAN", ColorHuman, SpaceComputation},
		placeSpec{"P:RESULT", ColorString, SpaceComputation},
	)
	_ = places["P:PLAN"].Deposit(&Token{
		Color: ColorArtifact, Space: SpaceComputation, Payload: "plan",
		OriginKind: NodeKindLLM,
	})
	_ = places["P:HUMAN"].Deposit(humanToken())

	transitions := map[string]*Transition{
		"T:SPAWN": {
			ID: "T:SPAWN", Kind: NodeKindSubNet,
			InputPlaces:  []string{"P:PLAN", "P:HUMAN"},
			OutputPlaces: []string{"P:RESULT"},
			SubNetFactory: func() *CPN {
				ps := map[string]*Place{
					"P:IN":  NewPlace("P:IN", ColorString, SpaceComputation),
					"P:OUT": NewPlace("P:OUT", ColorString, SpaceComputation),
				}
				ts := map[string]*Transition{
					"T:WORK": {
						ID: "T:WORK", Kind: NodeKindTool,
						InputPlaces: []string{"P:IN"}, OutputPlaces: []string{"P:OUT"},
						Executor: func(_ context.Context, in Token) (Token, error) {
							return Token{
								Color: ColorString, Space: SpaceComputation,
								Payload: fmt.Sprintf("child-result:%v", in.Payload),
							}, nil
						},
					},
				}
				return &CPN{Role: "worker", Mode: ModeMAS, State: StateIdle, Places: ps, Transitions: ts}
			},
		},
	}

	c := NewCPN("centaurian-subnet", "test", 0, ModeCentaurian, "sess-1", places, transitions)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := c.Run(ctx)
	c.childWg.Wait()

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertCompleted(t, c)
	assertPlaceLen(t, places["P:RESULT"], 1)
}
