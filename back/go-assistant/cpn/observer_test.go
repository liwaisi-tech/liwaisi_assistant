package cpn

import (
	"context"
	"testing"
	"time"
)

// --- Test Helpers ---

// observerTestSetup creates a parent CPN with SubNetBuses pre-loaded with events
// and observer transitions configured for testing.
func observerTestSetup(events []Event, observers []*Transition) *CPN {
	places := make(map[string]*Place)
	for _, obs := range observers {
		for _, pid := range obs.OutputPlaces {
			if _, exists := places[pid]; !exists {
				places[pid] = NewPlace(pid, ColorEvent, SpaceObservation)
			}
		}
	}

	transitions := make(map[string]*Transition)
	for _, obs := range observers {
		transitions[obs.ID] = obs
	}

	bus := make(chan Event, len(events)+1)
	for _, e := range events {
		bus <- e
	}

	cpn := NewCPN("parent", "root", 0, ModeMAS, "sess-1", places, transitions)
	cpn.subNetBuses = map[string]<-chan Event{"child-1": bus}
	return cpn
}

// makeEvent creates a test event with the given type and CPNID.
func makeEvent(typ EventType, cpnID string) Event {
	return Event{
		Type:      typ,
		CPNID:     cpnID,
		CPNDepth:  1,
		CPNRole:   "worker",
		SessionID: "sess-1",
		Timestamp: time.Now(),
	}
}

// makeObserver creates an observer transition with the given ID and output places.
func makeObserver(id string, outputPlaces []string) *Transition {
	return &Transition{
		ID:           id,
		Kind:         NodeKindObserver,
		OutputPlaces: outputPlaces,
	}
}

// --- Unit Tests ---

func TestDrainObservers_MatchingEvent(t *testing.T) {
	event := makeEvent(EventTransitionFired, "child-1")
	obs := makeObserver("obs:all", []string{"P:OBS"})

	cpn := observerTestSetup([]Event{event}, []*Transition{obs})

	drainObservers(context.Background(), cpn)

	p := cpn.Places["P:OBS"]
	if p.Len() != 1 {
		t.Fatalf("P:OBS token count = %d, want 1", p.Len())
	}
	tok, _ := p.Consume()
	if tok.Color != ColorEvent {
		t.Errorf("Color = %q, want %q", tok.Color, ColorEvent)
	}
	if tok.Space != SpaceObservation {
		t.Errorf("Space = %q, want %q", tok.Space, SpaceObservation)
	}
	if tok.OriginKind != NodeKindObserver {
		t.Errorf("OriginKind = %q, want %q", tok.OriginKind, NodeKindObserver)
	}
}

func TestDrainObservers_FilteredEventIgnored(t *testing.T) {
	event := makeEvent(EventTransitionFired, "child-1")
	obs := makeObserver("obs:filtered", []string{"P:OBS"})
	obs.EventFilter = func(e Event) bool {
		return e.Type == EventSubNetCompleted // won't match
	}

	cpn := observerTestSetup([]Event{event}, []*Transition{obs})

	drainObservers(context.Background(), cpn)

	if cpn.Places["P:OBS"].Len() != 0 {
		t.Fatalf("P:OBS token count = %d, want 0 (event should be filtered)", cpn.Places["P:OBS"].Len())
	}
}

func TestDrainObservers_ObservedCPNID_Match(t *testing.T) {
	event := makeEvent(EventTransitionFired, "child-1")
	obs := makeObserver("obs:specific", []string{"P:OBS"})
	obs.ObservedCPNID = "child-1"

	cpn := observerTestSetup([]Event{event}, []*Transition{obs})

	drainObservers(context.Background(), cpn)

	if cpn.Places["P:OBS"].Len() != 1 {
		t.Fatalf("P:OBS token count = %d, want 1", cpn.Places["P:OBS"].Len())
	}
}

func TestDrainObservers_ObservedCPNID_Mismatch(t *testing.T) {
	event := makeEvent(EventTransitionFired, "child-1")
	obs := makeObserver("obs:wrong-id", []string{"P:OBS"})
	obs.ObservedCPNID = "other-child"

	cpn := observerTestSetup([]Event{event}, []*Transition{obs})

	drainObservers(context.Background(), cpn)

	if cpn.Places["P:OBS"].Len() != 0 {
		t.Fatalf("P:OBS token count = %d, want 0 (CPNID mismatch)", cpn.Places["P:OBS"].Len())
	}
}

func TestDrainObservers_NilFilter_AcceptsAll(t *testing.T) {
	events := []Event{
		makeEvent(EventTransitionFired, "child-1"),
		makeEvent(EventSubNetCompleted, "child-1"),
		makeEvent(EventSubNetFailed, "child-1"),
	}
	obs := makeObserver("obs:all", []string{"P:OBS"})
	// EventFilter is nil — accepts all events.

	cpn := observerTestSetup(events, []*Transition{obs})

	drainObservers(context.Background(), cpn)

	if cpn.Places["P:OBS"].Len() != 3 {
		t.Fatalf("P:OBS token count = %d, want 3", cpn.Places["P:OBS"].Len())
	}
}

func TestDrainObservers_MultipleObservers_SameEvent(t *testing.T) {
	event := makeEvent(EventTransitionFired, "child-1")

	obsA := makeObserver("obs:A", []string{"P:OBS_A"})
	obsB := makeObserver("obs:B", []string{"P:OBS_B"})

	cpn := observerTestSetup([]Event{event}, []*Transition{obsA, obsB})

	drainObservers(context.Background(), cpn)

	if cpn.Places["P:OBS_A"].Len() != 1 {
		t.Errorf("P:OBS_A token count = %d, want 1", cpn.Places["P:OBS_A"].Len())
	}
	if cpn.Places["P:OBS_B"].Len() != 1 {
		t.Errorf("P:OBS_B token count = %d, want 1", cpn.Places["P:OBS_B"].Len())
	}
}

func TestDrainObservers_EmptyBus(t *testing.T) {
	obs := makeObserver("obs:all", []string{"P:OBS"})

	// No events in the bus.
	cpn := observerTestSetup(nil, []*Transition{obs})

	drainObservers(context.Background(), cpn)

	if cpn.Places["P:OBS"].Len() != 0 {
		t.Fatalf("P:OBS token count = %d, want 0", cpn.Places["P:OBS"].Len())
	}
}

func TestDrainObservers_NilSubNetBuses(t *testing.T) {
	places := map[string]*Place{
		"P:OBS": NewPlace("P:OBS", ColorEvent, SpaceObservation),
	}
	transitions := map[string]*Transition{
		"obs:all": makeObserver("obs:all", []string{"P:OBS"}),
	}
	cpn := NewCPN("parent", "root", 0, ModeMAS, "sess-1", places, transitions)
	// subNetBuses is nil by default.

	// Must not panic.
	drainObservers(context.Background(), cpn)

	if places["P:OBS"].Len() != 0 {
		t.Fatalf("P:OBS token count = %d, want 0", places["P:OBS"].Len())
	}
}

func TestDrainObservers_MultipleEventsMultipleBuses(t *testing.T) {
	obs := makeObserver("obs:all", []string{"P:OBS"})

	places := map[string]*Place{
		"P:OBS": NewPlace("P:OBS", ColorEvent, SpaceObservation),
	}
	transitions := map[string]*Transition{"obs:all": obs}

	bus1 := make(chan Event, 4)
	bus1 <- makeEvent(EventTransitionFired, "child-1")
	bus1 <- makeEvent(EventSubNetCompleted, "child-1")

	bus2 := make(chan Event, 4)
	bus2 <- makeEvent(EventTransitionFired, "child-2")

	cpn := NewCPN("parent", "root", 0, ModeMAS, "sess-1", places, transitions)
	cpn.subNetBuses = map[string]<-chan Event{
		"child-1": bus1,
		"child-2": bus2,
	}

	drainObservers(context.Background(), cpn)

	if places["P:OBS"].Len() != 3 {
		t.Fatalf("P:OBS token count = %d, want 3", places["P:OBS"].Len())
	}
}

func TestDrainObservers_NonBlocking(t *testing.T) {
	obs := makeObserver("obs:all", []string{"P:OBS"})

	places := map[string]*Place{
		"P:OBS": NewPlace("P:OBS", ColorEvent, SpaceObservation),
	}
	transitions := map[string]*Transition{"obs:all": obs}

	// Create a bus with no events — should return immediately.
	bus := make(chan Event, 4)
	cpn := NewCPN("parent", "root", 0, ModeMAS, "sess-1", places, transitions)
	cpn.subNetBuses = map[string]<-chan Event{"child-1": bus}

	done := make(chan struct{})
	go func() {
		drainObservers(context.Background(), cpn)
		close(done)
	}()

	select {
	case <-done:
		// OK — returned immediately.
	case <-time.After(100 * time.Millisecond):
		t.Fatal("drainObservers blocked — expected non-blocking return")
	}
}

func TestDrainObservers_TokenMetadata(t *testing.T) {
	event := Event{
		Type:           EventSubNetCompleted,
		CPNID:          "child-42",
		CPNDepth:       2,
		CPNRole:        "analyst",
		TransitionID:   "T:ANALYZE",
		TransitionKind: NodeKindTool,
		SessionID:      "sess-99",
		Timestamp:      time.Now(),
	}
	obs := makeObserver("obs:meta", []string{"P:OBS"})
	cpn := observerTestSetup([]Event{event}, []*Transition{obs})
	cpn.SessionID = "parent-sess"

	drainObservers(context.Background(), cpn)

	tok, err := cpn.Places["P:OBS"].Consume()
	if err != nil {
		t.Fatalf("Consume error: %v", err)
	}

	// REQ-007: Verify token metadata.
	if tok.Color != ColorEvent {
		t.Errorf("Color = %q, want %q", tok.Color, ColorEvent)
	}
	if tok.Space != SpaceObservation {
		t.Errorf("Space = %q, want %q", tok.Space, SpaceObservation)
	}
	if tok.OriginID != "child-42" {
		t.Errorf("OriginID = %q, want %q", tok.OriginID, "child-42")
	}
	if tok.OriginDepth != 2 {
		t.Errorf("OriginDepth = %d, want 2", tok.OriginDepth)
	}
	if tok.OriginKind != NodeKindObserver {
		t.Errorf("OriginKind = %q, want %q", tok.OriginKind, NodeKindObserver)
	}
	if tok.SessionID != "parent-sess" {
		t.Errorf("SessionID = %q, want %q", tok.SessionID, "parent-sess")
	}
	if tok.Timestamp.IsZero() {
		t.Error("Timestamp is zero")
	}

	// Verify the event payload is the original event.
	payload, ok := tok.Payload.(Event)
	if !ok {
		t.Fatalf("Payload type = %T, want cpn.Event", tok.Payload)
	}
	if payload.CPNID != "child-42" {
		t.Errorf("Payload.CPNID = %q, want %q", payload.CPNID, "child-42")
	}
	if payload.Type != EventSubNetCompleted {
		t.Errorf("Payload.Type = %q, want %q", payload.Type, EventSubNetCompleted)
	}
}

func TestDrainObservers_TokenCopyPerPlace(t *testing.T) {
	event := makeEvent(EventTransitionFired, "child-1")
	obs := makeObserver("obs:multi-out", []string{"P:OBS_A", "P:OBS_B"})

	cpn := observerTestSetup([]Event{event}, []*Transition{obs})

	drainObservers(context.Background(), cpn)

	if cpn.Places["P:OBS_A"].Len() != 1 {
		t.Fatalf("P:OBS_A token count = %d, want 1", cpn.Places["P:OBS_A"].Len())
	}
	if cpn.Places["P:OBS_B"].Len() != 1 {
		t.Fatalf("P:OBS_B token count = %d, want 1", cpn.Places["P:OBS_B"].Len())
	}

	tokA, _ := cpn.Places["P:OBS_A"].Consume()
	tokB, _ := cpn.Places["P:OBS_B"].Consume()

	// Verify they are independent copies (different pointers).
	if tokA == tokB {
		t.Error("tokens in different output places are the same pointer — expected independent copies")
	}
}

func TestDrainObservers_MultipleOutputPlaces(t *testing.T) {
	event := makeEvent(EventTransitionFired, "child-1")
	obs := makeObserver("obs:fan-out", []string{"P:OUT_1", "P:OUT_2", "P:OUT_3"})

	cpn := observerTestSetup([]Event{event}, []*Transition{obs})

	drainObservers(context.Background(), cpn)

	for _, pid := range []string{"P:OUT_1", "P:OUT_2", "P:OUT_3"} {
		if cpn.Places[pid].Len() != 1 {
			t.Errorf("%s token count = %d, want 1", pid, cpn.Places[pid].Len())
		}
	}
}

// --- Integration Test ---

func TestCPN_Run_WithObserver(t *testing.T) {
	// Topology:
	//   P:IN -> [T:TOOL] -> P:OUT
	//   [obs:events] (observer, no input places) -> P:EVENTS
	//
	// SubNetBuses are pre-loaded with events so the observer deposits tokens
	// in the first iteration. Both P:OUT and P:EVENTS are terminal places.
	// drainObservers runs before collectFirable, so observer tokens are deposited
	// first, then T:TOOL fires. Both terminals have tokens → completion.

	places := map[string]*Place{
		"P:IN":     NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT":    NewPlace("P:OUT", ColorString, SpaceSurface),
		"P:EVENTS": NewPlace("P:EVENTS", ColorEvent, SpaceObservation),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "hello"})

	tTool := NewTransition("T:TOOL", NodeKindTool, []string{"P:IN"}, []string{"P:OUT"})
	tTool.Executor = identityTool()

	obs := &Transition{
		ID:           "obs:events",
		Kind:         NodeKindObserver,
		OutputPlaces: []string{"P:EVENTS"},
	}

	transitions := map[string]*Transition{
		"T:TOOL":     tTool,
		"obs:events": obs,
	}

	c := NewCPN("integration-test", "root", 0, ModeMAS, "sess-int", places, transitions)

	// Pre-load event bus with events (simulating a child that already emitted).
	bus := make(chan Event, 4)
	bus <- makeEvent(EventTransitionFired, "child-1")
	bus <- makeEvent(EventSubNetCompleted, "child-1")
	c.subNetBuses = map[string]<-chan Event{"child-1": bus}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := c.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if c.State != StateCompleted {
		t.Fatalf("State = %q, want %q", c.State, StateCompleted)
	}

	// P:OUT should have the tool output.
	if places["P:OUT"].Len() != 1 {
		t.Errorf("P:OUT token count = %d, want 1", places["P:OUT"].Len())
	}

	// P:EVENTS should have observer tokens from the pre-loaded bus events.
	if places["P:EVENTS"].Len() != 2 {
		t.Errorf("P:EVENTS token count = %d, want 2", places["P:EVENTS"].Len())
	}

	// Verify observer token metadata.
	tok, err := places["P:EVENTS"].Consume()
	if err != nil {
		t.Fatalf("Consume error: %v", err)
	}
	if tok.Color != ColorEvent {
		t.Errorf("Color = %q, want %q", tok.Color, ColorEvent)
	}
	if tok.OriginKind != NodeKindObserver {
		t.Errorf("OriginKind = %q, want %q", tok.OriginKind, NodeKindObserver)
	}
}
