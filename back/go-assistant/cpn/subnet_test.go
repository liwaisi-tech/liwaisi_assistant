package cpn

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// simpleChildFactory returns a SubNetFactory that creates a minimal CPN
// with one tool transition: P:IN → uppercase → P:OUT.
func simpleChildFactory() func() *CPN {
	return func() *CPN {
		places := map[string]*Place{
			"P:IN":  NewPlace("P:IN", ColorString, SpaceComputation),
			"P:OUT": NewPlace("P:OUT", ColorString, SpaceComputation),
		}
		transitions := map[string]*Transition{
			"T:WORK": {
				ID:           "T:WORK",
				Kind:         NodeKindTool,
				InputPlaces:  []string{"P:IN"},
				OutputPlaces: []string{"P:OUT"},
				Executor: func(_ context.Context, in Token) (Token, error) {
					return Token{
						Color:   ColorString,
						Space:   SpaceComputation,
						Payload: fmt.Sprintf("processed: %v", in.Payload),
					}, nil
				},
			},
		}
		return &CPN{
			Role:        "worker",
			Mode:        ModeMAS,
			State:       StateIdle,
			Places:      places,
			Transitions: transitions,
		}
	}
}

// failingChildFactory returns a SubNetFactory that creates a CPN whose
// tool executor always returns an error.
func failingChildFactory() func() *CPN {
	return func() *CPN {
		places := map[string]*Place{
			"P:IN":  NewPlace("P:IN", ColorString, SpaceComputation),
			"P:OUT": NewPlace("P:OUT", ColorString, SpaceComputation),
		}
		transitions := map[string]*Transition{
			"T:FAIL": {
				ID:           "T:FAIL",
				Kind:         NodeKindTool,
				InputPlaces:  []string{"P:IN"},
				OutputPlaces: []string{"P:OUT"},
				Executor: func(_ context.Context, _ Token) (Token, error) {
					return Token{}, fmt.Errorf("child tool failed")
				},
			},
		}
		return &CPN{
			Role:        "failing-worker",
			Mode:        ModeMAS,
			State:       StateIdle,
			Places:      places,
			Transitions: transitions,
		}
	}
}

// makeSubNetParent creates a parent CPN with a subnet transition wired to the given factory.
// Parent topology: P:QUERY → T:SPAWN (subnet) → P:REPORT
func makeSubNetParent(factory func() *CPN) *CPN {
	places := map[string]*Place{
		"P:QUERY":  NewPlace("P:QUERY", ColorString, SpaceComputation),
		"P:REPORT": NewPlace("P:REPORT", ColorString, SpaceComputation),
	}
	transitions := map[string]*Transition{
		"T:SPAWN": {
			ID:            "T:SPAWN",
			Kind:          NodeKindSubNet,
			InputPlaces:   []string{"P:QUERY"},
			OutputPlaces:  []string{"P:REPORT"},
			SubNetFactory: factory,
		},
	}
	parent := NewCPN("parent-1", "coordinator", 0, ModeMAS, "session-1", places, transitions)
	return parent
}

// seedParent deposits an initial token in P:QUERY.
func seedParent(t *testing.T, parent *CPN, payload string) {
	t.Helper()
	if err := parent.Places["P:QUERY"].Deposit(&Token{
		Color:   ColorString,
		Space:   SpaceComputation,
		Payload: payload,
	}); err != nil {
		t.Fatalf("seedParent: %v", err)
	}
}

// waitForParent runs the parent and waits for children to finish.
func waitForParent(ctx context.Context, parent *CPN) error {
	err := parent.Run(ctx)
	parent.childWg.Wait()
	return err
}

// --- Happy Path Tests ---

func TestFireSubNet_ChildCompletes(t *testing.T) {
	parent := makeSubNetParent(simpleChildFactory())
	seedParent(t, parent, "hello")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := waitForParent(ctx, parent)
	if err != nil {
		t.Fatalf("parent.Run() = %v, want nil", err)
	}
	if parent.getState() != StateCompleted {
		t.Errorf("parent.State = %v, want %v", parent.getState(), StateCompleted)
	}
}

func TestFireSubNet_OutputTokensInParent(t *testing.T) {
	parent := makeSubNetParent(simpleChildFactory())
	seedParent(t, parent, "hello")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := waitForParent(ctx, parent); err != nil {
		t.Fatalf("parent.Run() = %v", err)
	}

	report := parent.Places["P:REPORT"]
	toks, ok := report.Peek()
	if !ok || len(toks) == 0 {
		t.Fatal("expected output tokens in P:REPORT")
	}

	tok := toks[0]
	if tok.OriginID == "" {
		t.Error("OriginID is empty, want child ID")
	}
	if tok.OriginDepth != 1 {
		t.Errorf("OriginDepth = %d, want 1", tok.OriginDepth)
	}
	payload, ok := tok.Payload.(string)
	if !ok {
		t.Fatalf("payload type = %T, want string", tok.Payload)
	}
	if !strings.Contains(payload, "processed:") {
		t.Errorf("payload = %q, want to contain 'processed:'", payload)
	}
}

func TestFireSubNet_SummaryInHistory(t *testing.T) {
	parent := makeSubNetParent(simpleChildFactory())
	seedParent(t, parent, "hello")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := waitForParent(ctx, parent); err != nil {
		t.Fatalf("parent.Run() = %v", err)
	}

	parent.mu.RLock()
	defer parent.mu.RUnlock()

	found := false
	for _, m := range parent.History {
		if m.Role == RoleObserver {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected RoleObserver message in parent.History")
	}
}

func TestFireSubNet_EventsEmitted(t *testing.T) {
	parentBus := make(chan Event, 128)
	parent := makeSubNetParent(simpleChildFactory())
	parent.EventEmitter = parentBus
	seedParent(t, parent, "hello")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := waitForParent(ctx, parent); err != nil {
		t.Fatalf("parent.Run() = %v", err)
	}

	var started, completed bool
	close(parentBus)
	for e := range parentBus {
		switch e.Type {
		case EventSubNetStarted:
			started = true
		case EventSubNetCompleted:
			completed = true
		}
	}
	if !started {
		t.Error("expected EventSubNetStarted")
	}
	if !completed {
		t.Error("expected EventSubNetCompleted")
	}
}

// --- Error Tests ---

func TestFireSubNet_ChildFails(t *testing.T) {
	parent := makeSubNetParent(failingChildFactory())
	seedParent(t, parent, "hello")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Parent may complete (if error propagation sets parent failed before check)
	// or return nil (deadlock/completed). The key assertion: parent is failed.
	_ = waitForParent(ctx, parent)

	if parent.getState() != StateFailed {
		t.Errorf("parent.State = %v, want %v", parent.getState(), StateFailed)
	}
}

func TestFireSubNet_ChildFails_ErrorPlace(t *testing.T) {
	places := map[string]*Place{
		"P:QUERY":  NewPlace("P:QUERY", ColorString, SpaceComputation),
		"P:REPORT": NewPlace("P:REPORT", ColorString, SpaceComputation),
		"P:ERR":    NewPlace("P:ERR", ColorError, SpaceComputation),
	}
	transitions := map[string]*Transition{
		"T:SPAWN": {
			ID:            "T:SPAWN",
			Kind:          NodeKindSubNet,
			InputPlaces:   []string{"P:QUERY"},
			OutputPlaces:  []string{"P:REPORT"},
			ErrorPlace:    "P:ERR",
			SubNetFactory: failingChildFactory(),
		},
	}
	parent := NewCPN("parent-1", "coordinator", 0, ModeMAS, "session-1", places, transitions)
	seedParent(t, parent, "hello")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = waitForParent(ctx, parent)

	errPlace := parent.Places["P:ERR"]
	if errPlace.Len() == 0 {
		t.Error("expected error token in P:ERR")
	}
}

// --- Clone/Factory Tests ---

func TestFireSubNet_CloneCPNPath(t *testing.T) {
	prototype := simpleChildFactory()()
	places := map[string]*Place{
		"P:QUERY":  NewPlace("P:QUERY", ColorString, SpaceComputation),
		"P:REPORT": NewPlace("P:REPORT", ColorString, SpaceComputation),
	}
	transitions := map[string]*Transition{
		"T:SPAWN": {
			ID:           "T:SPAWN",
			Kind:         NodeKindSubNet,
			InputPlaces:  []string{"P:QUERY"},
			OutputPlaces: []string{"P:REPORT"},
			SubNet:       prototype,
		},
	}
	parent := NewCPN("parent-1", "coordinator", 0, ModeMAS, "session-1", places, transitions)
	seedParent(t, parent, "hello")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := waitForParent(ctx, parent); err != nil {
		t.Fatalf("parent.Run() = %v", err)
	}

	report := parent.Places["P:REPORT"]
	if report.Len() == 0 {
		t.Error("expected output tokens in P:REPORT via cloneCPN path")
	}
}

func TestFireSubNet_FactoryTakesPrecedence(t *testing.T) {
	factoryUsed := false
	factory := func() *CPN {
		factoryUsed = true
		return simpleChildFactory()()
	}
	prototype := simpleChildFactory()()

	places := map[string]*Place{
		"P:QUERY":  NewPlace("P:QUERY", ColorString, SpaceComputation),
		"P:REPORT": NewPlace("P:REPORT", ColorString, SpaceComputation),
	}
	transitions := map[string]*Transition{
		"T:SPAWN": {
			ID:            "T:SPAWN",
			Kind:          NodeKindSubNet,
			InputPlaces:   []string{"P:QUERY"},
			OutputPlaces:  []string{"P:REPORT"},
			SubNet:        prototype,
			SubNetFactory: factory,
		},
	}
	parent := NewCPN("parent-1", "coordinator", 0, ModeMAS, "session-1", places, transitions)
	seedParent(t, parent, "hello")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := waitForParent(ctx, parent); err != nil {
		t.Fatalf("parent.Run() = %v", err)
	}
	if !factoryUsed {
		t.Error("SubNetFactory should have been used over SubNet")
	}
}

// --- Concurrency Tests ---

func TestFireSubNet_ContextCancellation(t *testing.T) {
	// Create a child that blocks forever.
	blockingFactory := func() *CPN {
		places := map[string]*Place{
			"P:IN":  NewPlace("P:IN", ColorString, SpaceComputation),
			"P:OUT": NewPlace("P:OUT", ColorString, SpaceComputation),
		}
		transitions := map[string]*Transition{
			"T:BLOCK": {
				ID:           "T:BLOCK",
				Kind:         NodeKindTool,
				InputPlaces:  []string{"P:IN"},
				OutputPlaces: []string{"P:OUT"},
				Executor: func(ctx context.Context, _ Token) (Token, error) {
					<-ctx.Done()
					return Token{}, ctx.Err()
				},
			},
		}
		return &CPN{
			Role: "blocker", Mode: ModeMAS, State: StateIdle,
			Places: places, Transitions: transitions,
		}
	}

	parent := makeSubNetParent(blockingFactory)
	seedParent(t, parent, "hello")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = waitForParent(ctx, parent)
	// Key assertion: no goroutine leak — childWg.Wait() completed.
}

func TestFireSubNet_MultipleChildren(t *testing.T) {
	places := map[string]*Place{
		"P:Q1":     NewPlace("P:Q1", ColorString, SpaceComputation),
		"P:Q2":     NewPlace("P:Q2", ColorString, SpaceComputation),
		"P:REPORT": NewPlace("P:REPORT", ColorString, SpaceComputation),
	}
	transitions := map[string]*Transition{
		"T:SPAWN1": {
			ID:            "T:SPAWN1",
			Kind:          NodeKindSubNet,
			InputPlaces:   []string{"P:Q1"},
			OutputPlaces:  []string{"P:REPORT"},
			SubNetFactory: simpleChildFactory(),
		},
		"T:SPAWN2": {
			ID:            "T:SPAWN2",
			Kind:          NodeKindSubNet,
			InputPlaces:   []string{"P:Q2"},
			OutputPlaces:  []string{"P:REPORT"},
			SubNetFactory: simpleChildFactory(),
		},
	}
	parent := NewCPN("parent-1", "coordinator", 0, ModeMAS, "session-1", places, transitions)
	if err := parent.Places["P:Q1"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "one"}); err != nil {
		t.Fatalf("deposit Q1: %v", err)
	}
	if err := parent.Places["P:Q2"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "two"}); err != nil {
		t.Fatalf("deposit Q2: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := waitForParent(ctx, parent); err != nil {
		t.Fatalf("parent.Run() = %v", err)
	}

	report := parent.Places["P:REPORT"]
	if report.Len() < 2 {
		t.Errorf("P:REPORT has %d tokens, want >= 2", report.Len())
	}
}

func TestFireSubNet_NoGoroutineLeaks(t *testing.T) {
	parent := makeSubNetParent(simpleChildFactory())
	seedParent(t, parent, "hello")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_ = waitForParent(ctx, parent)

	// childWg.Wait() already returned in waitForParent — no leak.
	// Double-check by calling Wait with a short timeout.
	done := make(chan struct{})
	go func() {
		parent.childWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// OK — no outstanding goroutines.
	case <-time.After(1 * time.Second):
		t.Fatal("childWg.Wait() timed out — goroutine leak detected")
	}
}

// --- cloneCPN Unit Tests ---

func TestCloneCPN_NewPlaces(t *testing.T) {
	proto := simpleChildFactory()()
	if err := proto.Places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "x"}); err != nil {
		t.Fatalf("deposit: %v", err)
	}

	clone, err := cloneCPN(proto)
	if err != nil {
		t.Fatalf("cloneCPN() error = %v", err)
	}

	for id, cp := range clone.Places {
		if cp.Len() != 0 {
			t.Errorf("clone place %s has %d tokens, want 0", id, cp.Len())
		}
		orig := proto.Places[id]
		if cp.ID != orig.ID || cp.Color != orig.Color || cp.Space != orig.Space {
			t.Errorf("clone place %s identity mismatch", id)
		}
	}
}

func TestCloneCPN_SharedTransitions(t *testing.T) {
	proto := simpleChildFactory()()

	clone, err := cloneCPN(proto)
	if err != nil {
		t.Fatalf("cloneCPN() error = %v", err)
	}

	for id, ct := range clone.Transitions {
		if ct != proto.Transitions[id] {
			t.Errorf("transition %s is not shared (pointer differs)", id)
		}
	}
}

func TestCloneCPN_OriginalUnmodified(t *testing.T) {
	proto := simpleChildFactory()()
	if err := proto.Places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "x"}); err != nil {
		t.Fatalf("deposit: %v", err)
	}
	origLen := proto.Places["P:IN"].Len()

	clone, err := cloneCPN(proto)
	if err != nil {
		t.Fatalf("cloneCPN() error = %v", err)
	}

	// Modify clone.
	if err := clone.Places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceComputation, Payload: "y"}); err != nil {
		t.Fatalf("deposit clone: %v", err)
	}

	if proto.Places["P:IN"].Len() != origLen {
		t.Errorf("prototype P:IN len changed from %d to %d", origLen, proto.Places["P:IN"].Len())
	}
}

func TestCloneCPN_NilPrototype(t *testing.T) {
	_, err := cloneCPN(nil)
	if err == nil {
		t.Fatal("cloneCPN(nil) should return error")
	}
}

// --- fireSubNet edge case tests ---

func TestFireSubNet_NilFactory(t *testing.T) {
	parent := makeSubNetParent(func() *CPN { return nil })
	seedParent(t, parent, "hello")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := waitForParent(ctx, parent)
	if err == nil {
		t.Fatal("expected error for nil factory return")
	}
}

// --- injectTokens Unit Tests ---

func TestInjectTokens_SingleSource(t *testing.T) {
	child := simpleChildFactory()()
	tokens := []Token{
		{Color: ColorString, Space: SpaceComputation, Payload: "a"},
		{Color: ColorString, Space: SpaceComputation, Payload: "b"},
	}

	injectTokens(child, tokens)

	// P:IN is the only source place (P:OUT is an output of T:WORK).
	if child.Places["P:IN"].Len() != 2 {
		t.Errorf("P:IN has %d tokens, want 2", child.Places["P:IN"].Len())
	}
}

func TestInjectTokens_MultipleSourcesByColor(t *testing.T) {
	places := map[string]*Place{
		"P:STR":  NewPlace("P:STR", ColorString, SpaceComputation),
		"P:JSON": NewPlace("P:JSON", ColorJSON, SpaceComputation),
		"P:OUT":  NewPlace("P:OUT", ColorString, SpaceComputation),
	}
	transitions := map[string]*Transition{
		"T:WORK": {
			ID:           "T:WORK",
			Kind:         NodeKindTool,
			InputPlaces:  []string{"P:STR", "P:JSON"},
			OutputPlaces: []string{"P:OUT"},
		},
	}
	child := &CPN{
		Role: "multi-input", Mode: ModeMAS, State: StateIdle,
		Places: places, Transitions: transitions,
	}

	tokens := []Token{
		{Color: ColorString, Space: SpaceComputation, Payload: "text"},
		{Color: ColorJSON, Space: SpaceComputation, Payload: `{"key":"val"}`},
	}

	injectTokens(child, tokens)

	if places["P:STR"].Len() != 1 {
		t.Errorf("P:STR has %d tokens, want 1", places["P:STR"].Len())
	}
	if places["P:JSON"].Len() != 1 {
		t.Errorf("P:JSON has %d tokens, want 1", places["P:JSON"].Len())
	}
}

func TestInjectTokens_FallbackToFirst(t *testing.T) {
	child := simpleChildFactory()()
	// Inject a token with a color that doesn't match any source place.
	tokens := []Token{
		{Color: ColorJSON, Space: SpaceComputation, Payload: `{"x":1}`},
	}

	injectTokens(child, tokens)

	// Should fall back to first source place (P:IN).
	if child.Places["P:IN"].Len() != 1 {
		t.Errorf("P:IN has %d tokens, want 1 (fallback)", child.Places["P:IN"].Len())
	}
}

// --- emit Unit Tests ---

func TestEmit_NonBlocking(t *testing.T) {
	ch := make(chan Event, 1)
	c := &CPN{
		ID: "test", Depth: 0, Role: "tester",
		EventEmitter: ch,
	}

	// Fill the channel.
	c.emit(&Event{Type: EventTransitionFired})

	// This must not block — event should be silently dropped.
	done := make(chan struct{})
	go func() {
		c.emit(&Event{Type: EventTransitionFired})
		close(done)
	}()

	select {
	case <-done:
		// OK — non-blocking.
	case <-time.After(1 * time.Second):
		t.Fatal("emit blocked on full channel")
	}
}

func TestEmit_NilEmitter(t *testing.T) {
	c := &CPN{ID: "test", Depth: 0, Role: "tester"}
	// Must not panic.
	c.emit(&Event{Type: EventTransitionFired})
}

func TestEmit_StampsMetadata(t *testing.T) {
	ch := make(chan Event, 1)
	c := &CPN{
		ID: "cpn-42", Depth: 2, Role: "analyst",
		EventEmitter: ch,
	}

	c.emit(&Event{Type: EventTransitionFired})

	e := <-ch
	if e.CPNID != "cpn-42" {
		t.Errorf("CPNID = %q, want %q", e.CPNID, "cpn-42")
	}
	if e.CPNDepth != 2 {
		t.Errorf("CPNDepth = %d, want 2", e.CPNDepth)
	}
	if e.CPNRole != "analyst" {
		t.Errorf("CPNRole = %q, want %q", e.CPNRole, "analyst")
	}
}

// --- registerSubNetBus Unit Test ---

func TestRegisterSubNetBus(t *testing.T) {
	c := &CPN{ID: "parent"}
	bus1 := make(chan Event, SubNetEventBusCapacity)
	bus2 := make(chan Event, SubNetEventBusCapacity)

	c.registerSubNetBus("child-1", bus1)
	c.registerSubNetBus("child-2", bus2)

	c.subNetMu.Lock()
	defer c.subNetMu.Unlock()

	if len(c.subNetBuses) != 2 {
		t.Fatalf("subNetBuses len = %d, want 2", len(c.subNetBuses))
	}

	if c.subNetBuses["child-1"] == nil {
		t.Error("child-1 bus not registered")
	}
	if c.subNetBuses["child-2"] == nil {
		t.Error("child-2 bus not registered")
	}
}

// --- registerSubNetBus Thread-Safety Test ---

func TestRegisterSubNetBus_Concurrent(t *testing.T) {
	c := &CPN{ID: "parent"}

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			bus := make(chan Event, 1)
			c.registerSubNetBus(fmt.Sprintf("child-%d", idx), bus)
		}(i)
	}
	wg.Wait()

	c.subNetMu.Lock()
	defer c.subNetMu.Unlock()

	if len(c.subNetBuses) != 100 {
		t.Errorf("subNetBuses len = %d, want 100", len(c.subNetBuses))
	}
}
