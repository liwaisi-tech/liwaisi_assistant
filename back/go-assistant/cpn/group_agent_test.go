package cpn

import (
	"sync"
	"testing"
)

// testCPN creates a minimal *CPN with the given ID and state for GroupAgent testing.
func testCPN(id string, state State) *CPN {
	return &CPN{ID: id, State: state}
}

// testCPNWithEmitter creates a *CPN with a buffered EventEmitter channel.
func testCPNWithEmitter(id string, state State, bufSize int) (*CPN, chan Event) {
	ch := make(chan Event, bufSize)
	return &CPN{ID: id, State: state, EventEmitter: ch}, ch
}

func TestNewGroupAgent(t *testing.T) {
	g := NewGroupAgent("g1", "analysis")

	if g.ID != "g1" {
		t.Errorf("ID = %q, want %q", g.ID, "g1")
	}
	if g.Topic != "analysis" {
		t.Errorf("Topic = %q, want %q", g.Topic, "analysis")
	}
	active, nonActive := g.Len()
	if active != 0 || nonActive != 0 {
		t.Errorf("Len() = (%d, %d), want (0, 0)", active, nonActive)
	}
}

func TestGroupAgent_Register_ActiveStates(t *testing.T) {
	tests := []struct {
		name  string
		state State
	}{
		{"Running", StateRunning},
		{"Idle", StateIdle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGroupAgent("g1", "test")
			c := testCPN("c1", tt.state)

			g.Register(c)

			active, nonActive := g.Len()
			if active != 1 {
				t.Errorf("active = %d, want 1", active)
			}
			if nonActive != 0 {
				t.Errorf("nonActive = %d, want 0", nonActive)
			}
		})
	}
}

func TestGroupAgent_Register_NonActiveStates(t *testing.T) {
	tests := []struct {
		name  string
		state State
	}{
		{"Completed", StateCompleted},
		{"Failed", StateFailed},
		{"Waiting", StateWaiting},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGroupAgent("g1", "test")
			c := testCPN("c1", tt.state)

			g.Register(c)

			active, nonActive := g.Len()
			if active != 0 {
				t.Errorf("active = %d, want 0", active)
			}
			if nonActive != 1 {
				t.Errorf("nonActive = %d, want 1", nonActive)
			}
		})
	}
}

func TestGroupAgent_Deliver_FanOut(t *testing.T) {
	g := NewGroupAgent("g1", "test")

	c1, ch1 := testCPNWithEmitter("c1", StateRunning, 1)
	c2, ch2 := testCPNWithEmitter("c2", StateRunning, 1)
	c3, ch3 := testCPNWithEmitter("c3", StateRunning, 1)

	g.Register(c1)
	g.Register(c2)
	g.Register(c3)

	evt := Event{ID: "e1", Type: EventStreamChunk}
	g.Deliver(&evt)

	for i, ch := range []chan Event{ch1, ch2, ch3} {
		select {
		case got := <-ch:
			if got.ID != "e1" {
				t.Errorf("channel %d: got event ID %q, want %q", i, got.ID, "e1")
			}
		default:
			t.Errorf("channel %d: expected event, got none", i)
		}
	}
}

func TestGroupAgent_Deliver_SkipsNilEmitter(t *testing.T) {
	g := NewGroupAgent("g1", "test")

	// CPN with nil EventEmitter — should not panic.
	c1 := testCPN("c1", StateRunning)
	c2, ch2 := testCPNWithEmitter("c2", StateRunning, 1)

	g.Register(c1)
	g.Register(c2)

	evt := Event{ID: "e1", Type: EventStreamChunk}
	g.Deliver(&evt) // must not panic

	select {
	case got := <-ch2:
		if got.ID != "e1" {
			t.Errorf("got event ID %q, want %q", got.ID, "e1")
		}
	default:
		t.Error("expected event on ch2, got none")
	}
}

func TestGroupAgent_Deliver_NonBlocking(t *testing.T) {
	g := NewGroupAgent("g1", "test")

	// Buffer size 0 — channel is full immediately.
	c1, _ := testCPNWithEmitter("c1", StateRunning, 0)
	g.Register(c1)

	evt := Event{ID: "e1", Type: EventStreamChunk}
	// Must not block — if this hangs, the test will timeout.
	g.Deliver(&evt)
}

func TestGroupAgent_Deliver_SkipsNonActive(t *testing.T) {
	g := NewGroupAgent("g1", "test")

	cActive, chActive := testCPNWithEmitter("c1", StateRunning, 1)
	cNonActive, chNonActive := testCPNWithEmitter("c2", StateCompleted, 1)

	g.Register(cActive)
	g.Register(cNonActive)

	evt := Event{ID: "e1", Type: EventStreamChunk}
	g.Deliver(&evt)

	select {
	case <-chActive:
		// expected
	default:
		t.Error("expected event on active CPN channel")
	}

	select {
	case <-chNonActive:
		t.Error("non-active CPN should not receive events")
	default:
		// expected
	}
}

func TestGroupAgent_Deregister(t *testing.T) {
	g := NewGroupAgent("g1", "test")

	c1 := testCPN("c1", StateRunning)
	c2 := testCPN("c2", StateRunning)
	g.Register(c1)
	g.Register(c2)

	g.Deregister("c1")

	active, _ := g.Len()
	if active != 1 {
		t.Errorf("active = %d, want 1", active)
	}

	// Verify correct CPN remains.
	remaining := g.Active()
	if len(remaining) != 1 || remaining[0].ID != "c2" {
		t.Errorf("remaining active CPN ID = %q, want %q", remaining[0].ID, "c2")
	}
}

func TestGroupAgent_Deregister_UnknownID(t *testing.T) {
	g := NewGroupAgent("g1", "test")

	c1 := testCPN("c1", StateRunning)
	g.Register(c1)

	// Should be a no-op, no panic.
	g.Deregister("unknown")

	active, _ := g.Len()
	if active != 1 {
		t.Errorf("active = %d, want 1", active)
	}
}

func TestGroupAgent_SwitchCMP_ActiveToNonActive(t *testing.T) {
	tests := []struct {
		name     string
		newState State
	}{
		{"Completed", StateCompleted},
		{"Failed", StateFailed},
		{"Waiting", StateWaiting},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGroupAgent("g1", "test")
			c := testCPN("c1", StateRunning)
			g.Register(c)

			// Change state, then switch.
			c.State = tt.newState
			g.SwitchCMP("c1")

			active, nonActive := g.Len()
			if active != 0 {
				t.Errorf("active = %d, want 0", active)
			}
			if nonActive != 1 {
				t.Errorf("nonActive = %d, want 1", nonActive)
			}
		})
	}
}

func TestGroupAgent_SwitchCMP_NonActiveToActive(t *testing.T) {
	tests := []struct {
		name     string
		newState State
	}{
		{"Running", StateRunning},
		{"Idle", StateIdle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGroupAgent("g1", "test")
			c := testCPN("c1", StateCompleted)
			g.Register(c) // goes to nonActive

			// Change state, then switch.
			c.State = tt.newState
			g.SwitchCMP("c1")

			active, nonActive := g.Len()
			if active != 1 {
				t.Errorf("active = %d, want 1", active)
			}
			if nonActive != 0 {
				t.Errorf("nonActive = %d, want 0", nonActive)
			}
		})
	}
}

func TestGroupAgent_SwitchCMP_NoMatch(t *testing.T) {
	g := NewGroupAgent("g1", "test")
	c := testCPN("c1", StateRunning)
	g.Register(c)

	// State is still Running — already in active, no transition condition met.
	g.SwitchCMP("c1")

	active, nonActive := g.Len()
	if active != 1 {
		t.Errorf("active = %d, want 1", active)
	}
	if nonActive != 0 {
		t.Errorf("nonActive = %d, want 0", nonActive)
	}
}

func TestGroupAgent_SwitchCMP_UnknownID(t *testing.T) {
	g := NewGroupAgent("g1", "test")
	c := testCPN("c1", StateRunning)
	g.Register(c)

	// Should be a no-op, no panic.
	g.SwitchCMP("unknown")

	active, _ := g.Len()
	if active != 1 {
		t.Errorf("active = %d, want 1", active)
	}
}

func TestGroupAgent_Active_ReturnsCopy(t *testing.T) {
	g := NewGroupAgent("g1", "test")
	c := testCPN("c1", StateRunning)
	g.Register(c)

	got := g.Active()
	if len(got) != 1 {
		t.Fatalf("Active() len = %d, want 1", len(got))
	}

	// Mutate the returned slice — internal state must not change.
	got[0] = nil

	internal := g.Active()
	if internal[0] == nil {
		t.Error("mutating Active() return value affected internal state")
	}
}

func TestGroupAgent_NonActive_ReturnsCopy(t *testing.T) {
	g := NewGroupAgent("g1", "test")
	c := testCPN("c1", StateCompleted)
	g.Register(c)

	got := g.NonActive()
	if len(got) != 1 {
		t.Fatalf("NonActive() len = %d, want 1", len(got))
	}

	got[0] = nil

	internal := g.NonActive()
	if internal[0] == nil {
		t.Error("mutating NonActive() return value affected internal state")
	}
}

func TestGroupAgent_Len(t *testing.T) {
	g := NewGroupAgent("g1", "test")

	active, nonActive := g.Len()
	if active != 0 || nonActive != 0 {
		t.Errorf("empty: Len() = (%d, %d), want (0, 0)", active, nonActive)
	}

	g.Register(testCPN("c1", StateRunning))
	g.Register(testCPN("c2", StateIdle))
	g.Register(testCPN("c3", StateCompleted))

	active, nonActive = g.Len()
	if active != 2 {
		t.Errorf("active = %d, want 2", active)
	}
	if nonActive != 1 {
		t.Errorf("nonActive = %d, want 1", nonActive)
	}
}

func TestGroupAgent_Concurrent_RegisterDeregister(t *testing.T) {
	g := NewGroupAgent("g1", "test")
	const n = 1000

	var wg sync.WaitGroup
	wg.Add(n * 2)

	for i := range n {
		id := "c" + string(rune('A'+i%26)) + string(rune('0'+i%10))
		go func(id string) {
			defer wg.Done()
			c := testCPN(id, StateRunning)
			g.Register(c)
		}(id)

		go func(id string) {
			defer wg.Done()
			g.Deregister(id)
		}(id)
	}

	wg.Wait()

	// No assertion on counts — the goal is race-free execution.
	// Just verify Len doesn't panic.
	g.Len()
}

func TestGroupAgent_Concurrent_DeliverDuringRegister(t *testing.T) {
	g := NewGroupAgent("g1", "test")
	const n = 1000

	var wg sync.WaitGroup
	wg.Add(n * 2)

	for i := range n {
		go func(i int) {
			defer wg.Done()
			c, _ := testCPNWithEmitter("c"+string(rune('0'+i%10)), StateRunning, 1)
			g.Register(c)
		}(i)

		go func() {
			defer wg.Done()
			g.Deliver(&Event{ID: "e1", Type: EventStreamChunk})
		}()
	}

	wg.Wait()
	g.Len()
}

func TestGroupAgent_PaperProtocol_FullLifecycle(t *testing.T) {
	group := NewGroupAgent("team-1", "data-analysis")

	// 1. Register two running workers.
	child1, ch1 := testCPNWithEmitter("worker-1", StateRunning, 10)
	child2, ch2 := testCPNWithEmitter("worker-2", StateRunning, 10)

	group.Register(child1)
	group.Register(child2)

	active, nonActive := group.Len()
	if active != 2 || nonActive != 0 {
		t.Fatalf("after register: (%d, %d), want (2, 0)", active, nonActive)
	}

	// 2. Deliver event — both receive it.
	group.Deliver(&Event{ID: "e1", Type: EventStreamChunk})

	select {
	case <-ch1:
	default:
		t.Error("worker-1 did not receive event")
	}
	select {
	case <-ch2:
	default:
		t.Error("worker-2 did not receive event")
	}

	// 3. worker-1 enters StateWaiting (HITL) — switch to nonActive.
	child1.State = StateWaiting
	group.SwitchCMP("worker-1")

	active, nonActive = group.Len()
	if active != 1 || nonActive != 1 {
		t.Fatalf("after switchCMP(waiting): (%d, %d), want (1, 1)", active, nonActive)
	}

	// 4. Deliver — only worker-2 receives.
	group.Deliver(&Event{ID: "e2", Type: EventStreamChunk})

	select {
	case <-ch2:
	default:
		t.Error("worker-2 did not receive event e2")
	}
	select {
	case <-ch1:
		t.Error("worker-1 (nonActive) should not receive event e2")
	default:
	}

	// 5. HITL resolved — worker-1 back to Running.
	child1.State = StateRunning
	group.SwitchCMP("worker-1")

	active, nonActive = group.Len()
	if active != 2 || nonActive != 0 {
		t.Fatalf("after switchCMP(running): (%d, %d), want (2, 0)", active, nonActive)
	}

	// 6. worker-1 completes — deregister from active.
	child1.State = StateCompleted
	group.Deregister("worker-1")

	active, nonActive = group.Len()
	if active != 1 || nonActive != 0 {
		t.Fatalf("after deregister: (%d, %d), want (1, 0)", active, nonActive)
	}

	// 7. worker-2 completes — deregister.
	child2.State = StateCompleted
	group.Deregister("worker-2")

	active, nonActive = group.Len()
	if active != 0 || nonActive != 0 {
		t.Fatalf("final: (%d, %d), want (0, 0)", active, nonActive)
	}
}
