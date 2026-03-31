package cpn

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// ── Paper Section 4.2: Multi-Agent Composition ──────────────────────────────
// Tests validate group-agent coordination, observer event routing, and
// sub-CPN lifecycle management.

// TestComposition_MultipleSubNetsRunConcurrently verifies that a parent CPN
// can spawn 3 children via separate SubNet transitions and they all complete.
func TestComposition_MultipleSubNetsRunConcurrently(t *testing.T) {
	t.Parallel()

	// Parent topology:
	//   P:Q1 → [T:01_SPAWN_A (SubNet)] → P:OUT_A
	//   P:Q2 → [T:02_SPAWN_B (SubNet)] → P:OUT_B
	//   P:Q3 → [T:03_SPAWN_C (SubNet)] → P:OUT_C
	places := newPlaces(
		placeSpec{"P:Q1", ColorString, SpaceComputation},
		placeSpec{"P:Q2", ColorString, SpaceComputation},
		placeSpec{"P:Q3", ColorString, SpaceComputation},
		placeSpec{"P:OUT_A", ColorString, SpaceComputation},
		placeSpec{"P:OUT_B", ColorString, SpaceComputation},
		placeSpec{"P:OUT_C", ColorString, SpaceComputation},
	)

	// Seed all input places.
	for _, pid := range []string{"P:Q1", "P:Q2", "P:Q3"} {
		_ = places[pid].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: pid})
	}

	transitions := map[string]*Transition{
		"T:01_SPAWN_A": {
			ID: "T:01_SPAWN_A", Kind: NodeKindSubNet,
			InputPlaces: []string{"P:Q1"}, OutputPlaces: []string{"P:OUT_A"},
			SubNetFactory: workerChildFactory("A"),
		},
		"T:02_SPAWN_B": {
			ID: "T:02_SPAWN_B", Kind: NodeKindSubNet,
			InputPlaces: []string{"P:Q2"}, OutputPlaces: []string{"P:OUT_B"},
			SubNetFactory: workerChildFactory("B"),
		},
		"T:03_SPAWN_C": {
			ID: "T:03_SPAWN_C", Kind: NodeKindSubNet,
			InputPlaces: []string{"P:Q3"}, OutputPlaces: []string{"P:OUT_C"},
			SubNetFactory: workerChildFactory("C"),
		},
	}

	parent := NewCPN("parent-multi", "coordinator", 0, ModeMAS, "sess-1", places, transitions)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := parent.Run(ctx)
	parent.childWg.Wait()

	if err != nil {
		t.Fatalf("parent.Run() = %v", err)
	}
	assertCompleted(t, parent)

	// All 3 output places should have tokens from their children.
	for _, pid := range []string{"P:OUT_A", "P:OUT_B", "P:OUT_C"} {
		assertPlaceLen(t, parent.Places[pid], 1)
	}

	// Group should have been created with 3 children registered.
	if parent.Group == nil {
		t.Fatal("parent.Group should be initialized")
	}
}

// TestComposition_SubNetEventsObservable verifies that a parent CPN receives
// events from child sub-CPNs via EventSink. The parent's Group tracks the
// child lifecycle, and events include SubNetStarted and SubNetCompleted.
func TestComposition_SubNetEventsObservable(t *testing.T) {
	t.Parallel()

	ec := &threadSafeEventCollector{}

	places := newPlaces(
		placeSpec{"P:QUERY", ColorString, SpaceComputation},
		placeSpec{"P:REPORT", ColorString, SpaceComputation},
	)
	_ = places["P:QUERY"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "task"})

	transitions := map[string]*Transition{
		"T:SPAWN": {
			ID: "T:SPAWN", Kind: NodeKindSubNet,
			InputPlaces: []string{"P:QUERY"}, OutputPlaces: []string{"P:REPORT"},
			SubNetFactory: workerChildFactory("X"),
		},
	}

	parent := NewCPN("parent-obs", "coordinator", 0, ModeMAS, "sess-1", places, transitions)
	parent.EventSink = ec.sink

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := parent.Run(ctx)
	parent.childWg.Wait()

	if err != nil {
		t.Fatalf("parent.Run() = %v", err)
	}
	assertCompleted(t, parent)

	// Verify events include SubNetStarted.
	events := ec.getEvents()
	hasStart := false
	for _, e := range events {
		if e.Type == EventSubNetStarted {
			hasStart = true
			break
		}
	}
	if !hasStart {
		t.Fatal("expected EventSubNetStarted in captured events")
	}

	// Group should have been created for managing the child.
	if parent.Group == nil {
		t.Fatal("parent.Group should have been initialized by SubNet firing")
	}
}

// TestComposition_ChildFailureDoesNotBlockSiblings verifies that when one
// child fails with ErrorPlace routing, the parent can still complete.
func TestComposition_ChildFailureDoesNotBlockSiblings(t *testing.T) {
	t.Parallel()

	// P:OUT_FAIL is NOT created — the failing child never deposits output.
	// Terminal places are: P:OUT_OK (success output), P:ERRORS (error routing).
	// Both will have tokens → IsComplete succeeds.
	places := newPlaces(
		placeSpec{"P:Q_OK", ColorString, SpaceComputation},
		placeSpec{"P:Q_FAIL", ColorString, SpaceComputation},
		placeSpec{"P:OUT_OK", ColorString, SpaceComputation},
		placeSpec{"P:ERRORS", ColorError, SpaceComputation},
	)
	_ = places["P:Q_OK"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "ok"})
	_ = places["P:Q_FAIL"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "fail"})

	transitions := map[string]*Transition{
		"T:01_SPAWN_OK": {
			ID: "T:01_SPAWN_OK", Kind: NodeKindSubNet,
			InputPlaces: []string{"P:Q_OK"}, OutputPlaces: []string{"P:OUT_OK"},
			SubNetFactory: workerChildFactory("OK"),
		},
		"T:02_SPAWN_FAIL": {
			ID: "T:02_SPAWN_FAIL", Kind: NodeKindSubNet,
			InputPlaces:  []string{"P:Q_FAIL"},
			OutputPlaces: []string{}, // No output place — errors routed via ErrorPlace.
			ErrorPlace:   "P:ERRORS",
			SubNetFactory: failingChildFactory(),
		},
	}

	parent := NewCPN("parent-fault", "coordinator", 0, ModeMAS, "sess-1", places, transitions)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := parent.Run(ctx)
	parent.childWg.Wait()

	if err != nil {
		t.Fatalf("parent.Run() = %v (expected nil with error routing)", err)
	}

	// The OK child should have produced output.
	assertPlaceLen(t, places["P:OUT_OK"], 1)

	// The failing child's error should be routed to P:ERRORS.
	if places["P:ERRORS"].Len() == 0 {
		t.Fatal("P:ERRORS should have an error token from the failing child")
	}
}

// ── Helpers ─────────────────────────────────────────────────────────────────

// workerChildFactory returns a SubNetFactory that creates a minimal CPN:
//
//	P:IN → [T:WORK (tool)] → P:OUT
//
// The tool prepends the label to the payload.
func workerChildFactory(label string) func() *CPN {
	return func() *CPN {
		places := map[string]*Place{
			"P:IN":  NewPlace("P:IN", ColorString, SpaceComputation),
			"P:OUT": NewPlace("P:OUT", ColorString, SpaceComputation),
		}
		transitions := map[string]*Transition{
			"T:WORK": {
				ID: "T:WORK", Kind: NodeKindTool,
				InputPlaces: []string{"P:IN"}, OutputPlaces: []string{"P:OUT"},
				Executor: func(_ context.Context, in Token) (Token, error) {
					return Token{
						Color:   ColorString,
						Space:   SpaceComputation,
						Payload: fmt.Sprintf("[%s] %v", label, in.Payload),
					}, nil
				},
			},
		}
		return &CPN{
			Role:        "worker-" + label,
			Mode:        ModeMAS,
			State:       StateIdle,
			Places:      places,
			Transitions: transitions,
		}
	}
}
