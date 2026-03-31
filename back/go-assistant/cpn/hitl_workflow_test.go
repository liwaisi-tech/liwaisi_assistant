package cpn

import (
	"context"
	"errors"
	"testing"
	"time"
)

// ── Paper Fig. 7: Centaurian Approval Pattern + HITL Workflows ──────────────

// TestHITLWorkflow_SpaceBridgeTriggersCentaurian_E2E validates the end-to-end
// pattern from Fig. 7: HITL bridges Surface→Computation, triggering Centaurian
// mode, then a computation transition fires only with both human+AI tokens.
//
// Topology:
//
//	P:QUERY (STRING, Surface) → [T:01_HITL (HITL)] → P:APPROVED (HUMAN, Computation)
//	P:WORK  (ARTIFACT, Computation, pre-loaded AI)
//	P:APPROVED + P:WORK → [T:02_EXECUTE (Tool)] → P:RESULT (ARTIFACT, Computation)
func TestHITLWorkflow_SpaceBridgeTriggersCentaurian_E2E(t *testing.T) {
	t.Parallel()

	hitlCh := make(chan Token, 1)
	ec := &threadSafeEventCollector{}

	places := newPlaces(
		placeSpec{"P:QUERY", ColorString, SpaceSurface},
		placeSpec{"P:APPROVED", ColorHuman, SpaceComputation},
		placeSpec{"P:WORK", ColorArtifact, SpaceComputation},
		placeSpec{"P:RESULT", ColorArtifact, SpaceComputation},
	)

	// Seed: user query + pre-loaded AI work token.
	_ = places["P:QUERY"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "query"})
	_ = places["P:WORK"].Deposit(&Token{
		Color: ColorArtifact, Space: SpaceComputation, Payload: "ai-plan",
		OriginKind: NodeKindLLM, // AI origin
	})

	transitions := map[string]*Transition{
		"T:01_HITL": {
			ID: "T:01_HITL", Kind: NodeKindHITL,
			InputPlaces: []string{"P:QUERY"}, OutputPlaces: []string{"P:APPROVED"},
			HITLConfig: &HITLConfig{Channel: hitlCh, Prompt: "Approve plan?"},
		},
		"T:02_EXECUTE": {
			ID: "T:02_EXECUTE", Kind: NodeKindTool,
			InputPlaces:  []string{"P:APPROVED", "P:WORK"},
			OutputPlaces: []string{"P:RESULT"},
			Executor:     prefixTool("executed:", ColorArtifact, SpaceComputation),
		},
	}

	c := NewCPN("fig7-e2e", "test", 0, ModeMAS, "sess-1", places, transitions)
	c.EventSink = ec.sink

	// Feed HITL approval with a small delay so the executor can start.
	feedHITLAsync(hitlCh, 50*time.Millisecond, makeApproveToken())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := c.Run(ctx)

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertCompleted(t, c)

	// P:RESULT should have a token.
	assertPlaceLen(t, places["P:RESULT"], 1)

	// Verify mode switched to Centaurian (human token entered computation).
	events := ec.getEvents()
	modeEvents := collectModeEvents(events)
	hasMASToC := false
	for _, me := range modeEvents {
		if me["from"] == ModeMAS && me["to"] == ModeCentaurian {
			hasMASToC = true
		}
	}
	if !hasMASToC {
		t.Fatal("expected MAS→Centaurian mode switch event")
	}
}

// TestHITLWorkflow_MultiStageApproval validates two sequential HITL checkpoints,
// each producing a Centaurian-gated computation transition.
//
// This topology works because HITL transitions are exempt from centaurianGuard
// (they PRODUCE human tokens, they can't require one). The second HITL fires
// even in Centaurian mode because effectiveGuard skips NodeKindHITL.
//
// Topology:
//
//	P:INPUT (STRING, Surface) → [T:01_HITL_1 (HITL)] → P:STAGE1 (HUMAN, Computation)
//	P:WORK1 (AI, Computation) + P:STAGE1 → [T:02_EXEC_1 (Tool)] → P:PROCESSED (STRING, Surface)
//	P:PROCESSED → [T:03_HITL_2 (HITL)] → P:STAGE2 (HUMAN, Computation)
//	P:WORK2 (AI, Computation) + P:STAGE2 → [T:04_EXEC_2 (Tool)] → P:FINAL (ARTIFACT, Computation)
func TestHITLWorkflow_MultiStageApproval(t *testing.T) {
	t.Parallel()

	hitlCh1 := make(chan Token, 1)
	hitlCh2 := make(chan Token, 1)

	places := newPlaces(
		placeSpec{"P:INPUT", ColorString, SpaceSurface},
		placeSpec{"P:STAGE1", ColorHuman, SpaceComputation},
		placeSpec{"P:WORK1", ColorArtifact, SpaceComputation},
		placeSpec{"P:PROCESSED", ColorString, SpaceSurface},
		placeSpec{"P:STAGE2", ColorHuman, SpaceComputation},
		placeSpec{"P:WORK2", ColorArtifact, SpaceComputation},
		placeSpec{"P:FINAL", ColorArtifact, SpaceComputation},
	)

	// Seed inputs.
	_ = places["P:INPUT"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "start"})
	_ = places["P:WORK1"].Deposit(&Token{
		Color: ColorArtifact, Space: SpaceComputation, Payload: "work-v1",
		OriginKind: NodeKindLLM,
	})
	_ = places["P:WORK2"].Deposit(&Token{
		Color: ColorArtifact, Space: SpaceComputation, Payload: "work-v2",
		OriginKind: NodeKindLLM,
	})

	transitions := map[string]*Transition{
		"T:01_HITL_1": {
			ID: "T:01_HITL_1", Kind: NodeKindHITL,
			InputPlaces: []string{"P:INPUT"}, OutputPlaces: []string{"P:STAGE1"},
			HITLConfig: &HITLConfig{Channel: hitlCh1, Prompt: "Approve stage 1?"},
		},
		"T:02_EXEC_1": {
			ID: "T:02_EXEC_1", Kind: NodeKindTool,
			InputPlaces:  []string{"P:STAGE1", "P:WORK1"},
			OutputPlaces: []string{"P:PROCESSED"},
			Executor:     prefixTool("stage1:", ColorString, SpaceSurface),
		},
		"T:03_HITL_2": {
			ID: "T:03_HITL_2", Kind: NodeKindHITL,
			InputPlaces: []string{"P:PROCESSED"}, OutputPlaces: []string{"P:STAGE2"},
			HITLConfig: &HITLConfig{Channel: hitlCh2, Prompt: "Approve stage 2?"},
		},
		"T:04_EXEC_2": {
			ID: "T:04_EXEC_2", Kind: NodeKindTool,
			InputPlaces:  []string{"P:STAGE2", "P:WORK2"},
			OutputPlaces: []string{"P:FINAL"},
			Executor:     prefixTool("final:", ColorArtifact, SpaceComputation),
		},
	}

	c := NewCPN("multi-stage", "test", 0, ModeMAS, "sess-1", places, transitions)

	ec := &threadSafeEventCollector{}
	c.EventSink = ec.sink

	// Feed HITL approvals with staggered timing.
	feedHITLAsync(hitlCh1, 50*time.Millisecond, makeApproveToken())
	feedHITLAsync(hitlCh2, 300*time.Millisecond, makeApproveToken())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := c.Run(ctx)

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertCompleted(t, c)

	// Final output should exist.
	assertPlaceLen(t, places["P:FINAL"], 1)

	// Verify both HITL events were emitted.
	events := ec.getEvents()
	hitlRequested := 0
	hitlResolved := 0
	for _, e := range events {
		if e.Type == EventHITLRequested {
			hitlRequested++
		}
		if e.Type == EventHITLResolved {
			hitlResolved++
		}
	}
	if hitlRequested != 2 {
		t.Fatalf("expected 2 HITL requested events, got %d", hitlRequested)
	}
	if hitlResolved != 2 {
		t.Fatalf("expected 2 HITL resolved events, got %d", hitlResolved)
	}
}

// TestHITLWorkflow_RejectionStopsDownstream verifies that a HITL rejection
// prevents all downstream transitions from firing.
//
// Topology:
//
//	P:IN (STRING, Surface) → [T:HITL (HITL)] → P:APPROVED (HUMAN, Computation)
//	P:APPROVED → [T:PROCESS (Tool)] → P:FINAL (ARTIFACT, Computation)
func TestHITLWorkflow_RejectionStopsDownstream(t *testing.T) {
	t.Parallel()

	hitlCh := make(chan Token, 1)

	places := newPlaces(
		placeSpec{"P:IN", ColorString, SpaceSurface},
		placeSpec{"P:APPROVED", ColorHuman, SpaceComputation},
		placeSpec{"P:FINAL", ColorArtifact, SpaceComputation},
	)
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "data"})

	transitions := map[string]*Transition{
		"T:01_HITL": {
			ID: "T:01_HITL", Kind: NodeKindHITL,
			InputPlaces: []string{"P:IN"}, OutputPlaces: []string{"P:APPROVED"},
			HITLConfig: &HITLConfig{Channel: hitlCh, Prompt: "approve?"},
		},
		"T:02_PROCESS": {
			ID: "T:02_PROCESS", Kind: NodeKindTool,
			InputPlaces:  []string{"P:APPROVED"},
			OutputPlaces: []string{"P:FINAL"},
			Executor:     prefixTool("done:", ColorArtifact, SpaceComputation),
		},
	}

	c := NewCPN("reject-test", "test", 0, ModeMAS, "sess-1", places, transitions)

	// Feed rejection.
	hitlCh <- makeRejectToken()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.Run(ctx)

	if !errors.Is(err, ErrHITLRejected) {
		t.Fatalf("expected ErrHITLRejected, got: %v", err)
	}

	// P:APPROVED and P:FINAL should be empty.
	assertPlaceLen(t, places["P:APPROVED"], 0)
	assertPlaceLen(t, places["P:FINAL"], 0)
}
