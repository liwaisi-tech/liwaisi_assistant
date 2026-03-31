package cpn

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// ── Paper Section 4.1: MAS Topology Tests ───────────────────────────────────
// Tests validate multi-agent coordination with autonomous agents, loose coupling,
// and independent firing — the MAS paradigm from the paper.

// TestMASTopology_CoordinatorSpawnsTwoDomainExperts validates that a parent CPN
// can spawn two child sub-CPNs concurrently and merge their results. After both
// children complete, the parent's main loop re-evaluates firable transitions
// and fires T:03_MERGE with the child-deposited tokens.
//
// Topology:
//
//	P:Q_A → [T:01_SPAWN_ANALYST (SubNet)] → P:ANALYSIS
//	P:Q_B → [T:02_SPAWN_WRITER  (SubNet)] → P:DRAFT
//	P:ANALYSIS + P:DRAFT → [T:03_MERGE (Tool)] → P:REPORT
func TestMASTopology_CoordinatorSpawnsTwoDomainExperts(t *testing.T) {
	t.Parallel()

	ec := &threadSafeEventCollector{}

	places := newPlaces(
		placeSpec{"P:Q_A", ColorString, SpaceComputation},
		placeSpec{"P:Q_B", ColorString, SpaceComputation},
		placeSpec{"P:ANALYSIS", ColorString, SpaceComputation},
		placeSpec{"P:DRAFT", ColorString, SpaceComputation},
		placeSpec{"P:REPORT", ColorArtifact, SpaceComputation},
	)
	_ = places["P:Q_A"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "analyze this"})
	_ = places["P:Q_B"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "write this"})

	transitions := map[string]*Transition{
		"T:01_SPAWN_ANALYST": {
			ID: "T:01_SPAWN_ANALYST", Kind: NodeKindSubNet,
			InputPlaces: []string{"P:Q_A"}, OutputPlaces: []string{"P:ANALYSIS"},
			SubNetFactory: workerChildFactory("analyst"),
		},
		"T:02_SPAWN_WRITER": {
			ID: "T:02_SPAWN_WRITER", Kind: NodeKindSubNet,
			InputPlaces: []string{"P:Q_B"}, OutputPlaces: []string{"P:DRAFT"},
			SubNetFactory: workerChildFactory("writer"),
		},
		"T:03_MERGE": {
			ID: "T:03_MERGE", Kind: NodeKindTool,
			InputPlaces:  []string{"P:ANALYSIS", "P:DRAFT"},
			OutputPlaces: []string{"P:REPORT"},
			Executor:     mergeTool(ColorArtifact, SpaceComputation),
		},
	}

	parent := NewCPN("coordinator", "coordinator", 0, ModeMAS, "sess-1", places, transitions)
	parent.EventSink = ec.sink

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := parent.Run(ctx)
	parent.childWg.Wait()

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertCompleted(t, parent)

	// Merge should have consumed both inputs and produced the report.
	assertPlaceLen(t, places["P:ANALYSIS"], 0) // consumed by merge
	assertPlaceLen(t, places["P:DRAFT"], 0)    // consumed by merge
	assertPlaceLen(t, places["P:REPORT"], 1)   // merged result

	// Group should have been created.
	if parent.Group == nil {
		t.Fatal("parent.Group should be initialized")
	}

	// History should contain sub-CPN summaries from both children.
	if len(parent.History) < 2 {
		t.Fatalf("parent.History should have at least 2 summaries, got %d", len(parent.History))
	}
}

// TestMASTopology_DiamondFanOutFanIn validates a diamond-shaped topology where
// a split transition fans out to two parallel branches that reconverge at a join.
//
// Topology:
//
//	P:IN → [T:01_PROC_A (Tool)] → P:A_OUT
//	P:IN → [T:02_PROC_B (Tool)] → P:B_OUT
//	P:A_OUT + P:B_OUT → [T:03_JOIN (Tool)] → P:OUT
//
// Note: Both T:01 and T:02 read from P:IN, which requires 2 tokens.
func TestMASTopology_DiamondFanOutFanIn(t *testing.T) {
	t.Parallel()

	places := newPlaces(
		placeSpec{"P:IN", ColorString, SpaceComputation},
		placeSpec{"P:A_OUT", ColorString, SpaceComputation},
		placeSpec{"P:B_OUT", ColorString, SpaceComputation},
		placeSpec{"P:OUT", ColorArtifact, SpaceComputation},
	)
	// Deposit 2 tokens so both branches can fire.
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "input-A"})
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "input-B"})

	transitions := newTransitions(
		transSpec{
			ID: "T:01_PROC_A", Kind: NodeKindTool,
			Inputs: []string{"P:IN"}, Outputs: []string{"P:A_OUT"},
			Executor: prefixTool("branch-A:", ColorString, SpaceComputation),
		},
		transSpec{
			ID: "T:02_PROC_B", Kind: NodeKindTool,
			Inputs: []string{"P:IN"}, Outputs: []string{"P:B_OUT"},
			Executor: prefixTool("branch-B:", ColorString, SpaceComputation),
		},
		transSpec{
			ID: "T:03_JOIN", Kind: NodeKindTool,
			Inputs: []string{"P:A_OUT", "P:B_OUT"}, Outputs: []string{"P:OUT"},
			Executor: mergeTool(ColorArtifact, SpaceComputation),
		},
	)

	c := NewCPN("diamond", "test", 0, ModeMAS, "sess-1", places, transitions)
	err := runWithTimeout(t, c, 5*time.Second)

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertCompleted(t, c)

	// Both branches should have produced output.
	assertPlaceLen(t, places["P:A_OUT"], 0) // consumed by JOIN
	assertPlaceLen(t, places["P:B_OUT"], 0) // consumed by JOIN
	assertPlaceLen(t, places["P:OUT"], 1)   // merged result
}

// TestMASTopology_ObserverMonitorsSubNets validates that EventSink captures
// lifecycle events (SubNetStarted, SubNetCompleted) from multiple child CPNs.
func TestMASTopology_ObserverMonitorsSubNets(t *testing.T) {
	t.Parallel()

	ec := &threadSafeEventCollector{}

	places := newPlaces(
		placeSpec{"P:Q1", ColorString, SpaceComputation},
		placeSpec{"P:Q2", ColorString, SpaceComputation},
		placeSpec{"P:OUT1", ColorString, SpaceComputation},
		placeSpec{"P:OUT2", ColorString, SpaceComputation},
	)
	_ = places["P:Q1"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "q1"})
	_ = places["P:Q2"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "q2"})

	transitions := map[string]*Transition{
		"T:01_SPAWN_1": {
			ID: "T:01_SPAWN_1", Kind: NodeKindSubNet,
			InputPlaces: []string{"P:Q1"}, OutputPlaces: []string{"P:OUT1"},
			SubNetFactory: workerChildFactory("W1"),
		},
		"T:02_SPAWN_2": {
			ID: "T:02_SPAWN_2", Kind: NodeKindSubNet,
			InputPlaces: []string{"P:Q2"}, OutputPlaces: []string{"P:OUT2"},
			SubNetFactory: workerChildFactory("W2"),
		},
	}

	parent := NewCPN("parent-monitor", "coordinator", 0, ModeMAS, "sess-1", places, transitions)
	parent.EventSink = ec.sink

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := parent.Run(ctx)
	parent.childWg.Wait()

	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertCompleted(t, parent)

	// Count SubNetStarted events.
	events := ec.getEvents()
	starts := 0
	for _, e := range events {
		if e.Type == EventSubNetStarted {
			starts++
		}
	}
	if starts != 2 {
		t.Fatalf("expected 2 SubNetStarted events, got %d", starts)
	}
}

// TestMASTopology_ChildFailureWithErrorRouting validates that when one child
// in a MAS topology fails, the error is routed to ErrorPlace while the parent
// can still reach completion via the healthy child's output.
func TestMASTopology_ChildFailureWithErrorRouting(t *testing.T) {
	t.Parallel()

	places := newPlaces(
		placeSpec{"P:Q_OK", ColorString, SpaceComputation},
		placeSpec{"P:Q_FAIL", ColorString, SpaceComputation},
		placeSpec{"P:OUT_OK", ColorString, SpaceComputation},
		placeSpec{"P:ERRORS", ColorError, SpaceComputation},
	)
	_ = places["P:Q_OK"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "ok"})
	_ = places["P:Q_FAIL"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "fail"})

	transitions := map[string]*Transition{
		"T:01_OK": {
			ID: "T:01_OK", Kind: NodeKindSubNet,
			InputPlaces: []string{"P:Q_OK"}, OutputPlaces: []string{"P:OUT_OK"},
			SubNetFactory: workerChildFactory("OK"),
		},
		"T:02_FAIL": {
			ID: "T:02_FAIL", Kind: NodeKindSubNet,
			InputPlaces:  []string{"P:Q_FAIL"},
			OutputPlaces: []string{},
			ErrorPlace:   "P:ERRORS",
			SubNetFactory: func() *CPN {
				places := map[string]*Place{
					"P:IN":  NewPlace("P:IN", ColorString, SpaceComputation),
					"P:OUT": NewPlace("P:OUT", ColorString, SpaceComputation),
				}
				transitions := map[string]*Transition{
					"T:FAIL": {
						ID: "T:FAIL", Kind: NodeKindTool,
						InputPlaces: []string{"P:IN"}, OutputPlaces: []string{"P:OUT"},
						Executor: func(_ context.Context, _ Token) (Token, error) {
							return Token{}, fmt.Errorf("child tool failed")
						},
					},
				}
				return &CPN{Role: "failing", Mode: ModeMAS, State: StateIdle, Places: places, Transitions: transitions}
			},
		},
	}

	parent := NewCPN("parent-err", "coordinator", 0, ModeMAS, "sess-1", places, transitions)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := parent.Run(ctx)
	parent.childWg.Wait()

	if err != nil {
		t.Fatalf("parent.Run() = %v (expected nil with error routing)", err)
	}

	assertPlaceLen(t, places["P:OUT_OK"], 1)
	if places["P:ERRORS"].Len() == 0 {
		t.Fatal("P:ERRORS should have an error token from the failing child")
	}
}
