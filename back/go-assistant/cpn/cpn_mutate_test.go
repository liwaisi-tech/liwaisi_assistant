package cpn

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// fakeMutationLog records every LogMutation call for assertions.
type fakeMutationLog struct {
	mu      sync.Mutex
	entries []mutationLogEntry
}

type mutationLogEntry struct {
	cpnID          string
	sessionID      string
	mutation       Mutation
	approved       bool
	rejectedReason string
}

func (f *fakeMutationLog) LogMutation(_ context.Context, cpnID, sessionID string, m Mutation, approved bool, rejectedReason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = append(f.entries, mutationLogEntry{
		cpnID:          cpnID,
		sessionID:      sessionID,
		mutation:       m,
		approved:       approved,
		rejectedReason: rejectedReason,
	})
	return nil
}

func (f *fakeMutationLog) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.entries)
}

func (f *fakeMutationLog) last() mutationLogEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.entries) == 0 {
		panic("no entries")
	}
	return f.entries[len(f.entries)-1]
}

// newMutableCPN builds a minimal CPN with MutableAfterStart=true and
// a single place to start with.
func newMutableCPN(t *testing.T) *CPN {
	t.Helper()
	pIn := NewPlace("p-seed", ColorString, SpaceComputation)
	pOut := NewPlace("p-out", ColorArtifact, SpaceComputation)
	tr := NewTransition("t-start", NodeKindTool, []string{"p-seed"}, []string{"p-out"})
	tr.Executor = func(_ context.Context, tok Token) (Token, error) {
		out := tok
		out.Color = ColorArtifact
		return out, nil
	}
	c := NewCPN("cpn-1", "test", 0, ModeMAS, "sess-1",
		map[string]*Place{
			"p-seed": pIn,
			"p-out":  pOut,
		},
		map[string]*Transition{"t-start": tr},
	)
	c.MutableAfterStart = true
	return c
}

// ── Mutate — rejected when not mutable ─────────────────────────────────────

func TestMutate_Rejected_WhenNotMutable(t *testing.T) {
	c := newMutableCPN(t)
	c.MutableAfterStart = false

	events := collectEvents(c)

	err := c.Mutate(context.Background(), Mutation{
		Kind:     MutationAddPlace,
		AddPlace: &PlaceDef{ID: "p-new", Color: ColorArtifact, Space: SpaceComputation},
	})

	if !errors.Is(err, ErrMutationNotPermitted) {
		t.Fatalf("expected ErrMutationNotPermitted, got %v", err)
	}

	// Verify rejection event was emitted.
	evs := *events
	if len(evs) == 0 {
		t.Fatal("expected EventTopologyMutationRejected, got no events")
	}
	found := false
	for _, e := range evs {
		if e.Type == EventTopologyMutationRejected {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected EventTopologyMutationRejected event, got %v", evs)
	}
}

// ── Mutate — add_place success ──────────────────────────────────────────────

func TestMutate_AddPlace_Success(t *testing.T) {
	c := newMutableCPN(t)
	log := &fakeMutationLog{}
	c.MutationLog = log

	events := collectEvents(c)

	err := c.Mutate(context.Background(), Mutation{
		Kind:        MutationAddPlace,
		RequestedBy: "agent-1",
		Reason:      "test add",
		AddPlace:    &PlaceDef{ID: "p-new", Color: ColorArtifact, Space: SpaceComputation},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := c.Places["p-new"]; !ok {
		t.Fatal("p-new not found in places after mutation")
	}
	if c.Places["p-new"].Color != ColorArtifact {
		t.Errorf("expected ColorArtifact, got %s", c.Places["p-new"].Color)
	}

	// Validate still passes.
	if err := Validate(c.Places, c.Transitions); err != nil {
		t.Fatalf("Validate after mutation: %v", err)
	}

	// Audit log should have one approved entry.
	if log.count() != 1 {
		t.Fatalf("expected 1 log entry, got %d", log.count())
	}
	entry := log.last()
	if !entry.approved {
		t.Errorf("expected approved=true, got false (reason: %s)", entry.rejectedReason)
	}

	// Verify success event was emitted.
	evs := *events
	found := false
	for _, e := range evs {
		if e.Type == EventTopologyMutated {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected EventTopologyMutated event, events: %v", evs)
	}
}

// ── Mutate — add_place duplicate ID ────────────────────────────────────────

func TestMutate_AddPlace_DuplicateID(t *testing.T) {
	c := newMutableCPN(t)
	log := &fakeMutationLog{}
	c.MutationLog = log

	// Add once — should succeed.
	if err := c.Mutate(context.Background(), Mutation{
		Kind:     MutationAddPlace,
		AddPlace: &PlaceDef{ID: "p-dup", Color: ColorArtifact, Space: SpaceComputation},
	}); err != nil {
		t.Fatalf("first add failed: %v", err)
	}

	initialCount := len(c.Places)

	// Second add with same ID must fail.
	err := c.Mutate(context.Background(), Mutation{
		Kind:     MutationAddPlace,
		AddPlace: &PlaceDef{ID: "p-dup", Color: ColorArtifact, Space: SpaceComputation},
	})

	if err == nil {
		t.Fatal("expected error for duplicate place ID, got nil")
	}

	// Topology must be unchanged (rollback).
	if len(c.Places) != initialCount {
		t.Errorf("topology changed after rejected mutation: before=%d after=%d", initialCount, len(c.Places))
	}

	// Log should have one rejected entry for the second mutation.
	if log.count() < 2 {
		t.Fatalf("expected at least 2 log entries, got %d", log.count())
	}
	last := log.last()
	if last.approved {
		t.Error("expected rejected log entry for duplicate add, got approved")
	}
}

// ── Mutate — add_transition success ────────────────────────────────────────

func TestMutate_AddTransition_Success(t *testing.T) {
	c := newMutableCPN(t)

	// First add a place that the new transition can reference.
	if err := c.Mutate(context.Background(), Mutation{
		Kind:     MutationAddPlace,
		AddPlace: &PlaceDef{ID: "p-mid", Color: ColorArtifact, Space: SpaceComputation},
	}); err != nil {
		t.Fatalf("pre-add place: %v", err)
	}

	err := c.Mutate(context.Background(), Mutation{
		Kind:        MutationAddTransition,
		RequestedBy: "agent-1",
		AddTransition: &TransitionDef{
			ID:           "t-new",
			Kind:         NodeKindTool,
			InputPlaces:  []string{"p-out"},
			OutputPlaces: []string{"p-mid"},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := c.Transitions["t-new"]; !ok {
		t.Fatal("t-new not found in transitions after mutation")
	}
}

// ── Mutate — add_transition missing input place ─────────────────────────────

func TestMutate_AddTransition_MissingInputPlace(t *testing.T) {
	c := newMutableCPN(t)

	before := len(c.Transitions)

	err := c.Mutate(context.Background(), Mutation{
		Kind:        MutationAddTransition,
		RequestedBy: "agent-1",
		AddTransition: &TransitionDef{
			ID:           "t-bad",
			Kind:         NodeKindTool,
			InputPlaces:  []string{"p-does-not-exist"},
			OutputPlaces: []string{"p-out"},
		},
	})

	if err == nil {
		t.Fatal("expected CON-002 error, got nil")
	}

	// Rollback: transitions count unchanged.
	if len(c.Transitions) != before {
		t.Errorf("expected transition count to roll back to %d, got %d", before, len(c.Transitions))
	}
}

// ── Mutate — deprecate transition success ──────────────────────────────────

func TestMutate_DeprecateTransition_Success(t *testing.T) {
	c := newMutableCPN(t)

	err := c.Mutate(context.Background(), Mutation{
		Kind:                  MutationDeprecateTransition,
		RequestedBy:           "admin",
		DeprecateTransitionID: "t-start",
		DeprecateReason:       "replaced by t-v2",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr := c.Transitions["t-start"]
	if !tr.Deprecated {
		t.Error("expected Deprecated=true after deprecate mutation")
	}
	if tr.DeprecateReason != "replaced by t-v2" {
		t.Errorf("unexpected DeprecateReason: %q", tr.DeprecateReason)
	}

	// Deprecated transition must not be firable.
	if tr.CanFire(c.Places) {
		t.Error("deprecated transition should not be firable")
	}
}

// ── Mutate — deprecate transition not found ────────────────────────────────

func TestMutate_DeprecateTransition_NotFound(t *testing.T) {
	c := newMutableCPN(t)

	err := c.Mutate(context.Background(), Mutation{
		Kind:                  MutationDeprecateTransition,
		DeprecateTransitionID: "t-nonexistent",
	})

	if err == nil {
		t.Fatal("expected error for nonexistent transition, got nil")
	}
}

// ── Mutate — race condition test ────────────────────────────────────────────

func TestMutate_RaceCondition(t *testing.T) {
	c := newMutableCPN(t)

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func() {
			defer wg.Done()
			err := c.Mutate(context.Background(), Mutation{
				Kind: MutationAddPlace,
				AddPlace: &PlaceDef{
					ID:    fmt.Sprintf("p-race-%d", i),
					Color: ColorArtifact,
					Space: SpaceComputation,
				},
			})
			// Errors are acceptable (e.g., duplicate if somehow same ID), but
			// we are primarily checking for data races via -race flag.
			_ = err
		}()
	}
	wg.Wait()

	// All 100 unique places should have been added (no duplicates in IDs).
	for i := range goroutines {
		id := fmt.Sprintf("p-race-%d", i)
		if _, ok := c.Places[id]; !ok {
			t.Errorf("place %s not found after concurrent mutations", id)
		}
	}
}

// ── Integration: fireTopologyMutate (NodeKindTopologyMutate) ───────────────

// TestFireTopologyMutate_IntegrationAddPlace builds a small CPN with a
// topology_mutate transition and calls fireTopologyMutate directly to verify
// the new place is added and the output token deposited.
func TestFireTopologyMutate_IntegrationAddPlace(t *testing.T) {
	// Input place carries the mutation token.
	pIn := NewPlace("p-mut-in", ColorArtifact, SpaceComputation)
	// Output place receives the result token.
	pOut := NewPlace("p-mut-out", ColorArtifact, SpaceComputation)

	tMut := NewTransition("t-mutate", NodeKindTopologyMutate, []string{"p-mut-in"}, []string{"p-mut-out"})

	c := NewCPN("cpn-integ", "test", 0, ModeMAS, "sess-integ",
		map[string]*Place{
			"p-mut-in":  pIn,
			"p-mut-out": pOut,
		},
		map[string]*Transition{"t-mutate": tMut},
	)
	c.MutableAfterStart = true

	// Build the mutation payload.
	m := Mutation{
		Kind:        MutationAddPlace,
		RequestedBy: "integration-test",
		Reason:      "fire_topology_mutate integration",
		AddPlace:    &PlaceDef{ID: "p-injected", Color: ColorArtifact, Space: SpaceComputation},
	}
	consumed := []Token{{
		Color:     ColorArtifact,
		Payload:   m,
		Space:     SpaceComputation,
		SessionID: "sess-integ",
	}}

	// Invoke the fire function directly (integration-level, not e2e Run).
	ctx := context.Background()
	outputSnaps, cost, err := fireTopologyMutate(ctx, tMut, c, consumed)
	if err != nil {
		t.Fatalf("fireTopologyMutate: %v", err)
	}
	if cost != 0 {
		t.Errorf("expected zero cost, got %v", cost)
	}

	// Verify the new place was injected into the topology.
	if _, ok := c.Places["p-injected"]; !ok {
		t.Fatal("expected p-injected to exist after topology_mutate fired, not found")
	}

	// Verify output token was deposited in p-mut-out.
	if pOut.Len() == 0 {
		t.Fatal("expected output token in p-mut-out, got none")
	}

	// Verify snapshot was returned.
	if len(outputSnaps) != 1 {
		t.Errorf("expected 1 output snapshot, got %d", len(outputSnaps))
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

// collectEvents wires a simple EventSink that records all events.
// The returned slice pointer is safe to read after the test action.
func collectEvents(c *CPN) *[]Event {
	var mu sync.Mutex
	var evs []Event
	c.EventSink = func(e *Event) {
		mu.Lock()
		evs = append(evs, *e)
		mu.Unlock()
	}
	return &evs
}
