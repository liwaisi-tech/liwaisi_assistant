package cpn

import (
	"sync"
	"testing"
)

func TestNewCPN(t *testing.T) {
	places := map[string]*Place{
		"P1": NewPlace("P1", ColorString, SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T1": NewTransition("T1", NodeKindTool, []string{"P1"}, []string{"P2"}),
	}

	c := NewCPN("cpn-1", "worker", 2, ModeMAS, "sess-1", places, transitions)

	if c.ID != "cpn-1" {
		t.Fatalf("ID = %q, want %q", c.ID, "cpn-1")
	}
	if c.Role != "worker" {
		t.Fatalf("Role = %q, want %q", c.Role, "worker")
	}
	if c.Depth != 2 {
		t.Fatalf("Depth = %d, want %d", c.Depth, 2)
	}
	if c.Mode != ModeMAS {
		t.Fatalf("Mode = %q, want %q", c.Mode, ModeMAS)
	}
	if c.State != StateIdle {
		t.Fatalf("State = %q, want %q", c.State, StateIdle)
	}
	if c.Error != nil {
		t.Fatalf("Error = %v, want nil", c.Error)
	}
	if c.SessionID != "sess-1" {
		t.Fatalf("SessionID = %q, want %q", c.SessionID, "sess-1")
	}
	if len(c.Places) != 1 {
		t.Fatalf("Places count = %d, want 1", len(c.Places))
	}
	if len(c.Transitions) != 1 {
		t.Fatalf("Transitions count = %d, want 1", len(c.Transitions))
	}
}

func TestCPN_TerminalPlaces(t *testing.T) {
	// P:IN -> [T:A] -> P:MID -> [T:B] -> P:OUT
	// Terminal: P:OUT (not referenced as input)
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:MID": NewPlace("P:MID", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:A": NewTransition("T:A", NodeKindTool, []string{"P:IN"}, []string{"P:MID"}),
		"T:B": NewTransition("T:B", NodeKindTool, []string{"P:MID"}, []string{"P:OUT"}),
	}

	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)
	terminals := c.TerminalPlaces()

	if len(terminals) != 1 {
		t.Fatalf("TerminalPlaces count = %d, want 1", len(terminals))
	}
	if terminals[0].ID != "P:OUT" {
		t.Fatalf("TerminalPlaces[0].ID = %q, want %q", terminals[0].ID, "P:OUT")
	}
}

func TestCPN_TerminalPlaces_AllReferenced(t *testing.T) {
	// Circular: P:A -> [T:1] -> P:B -> [T:2] -> P:A
	// Both places are inputs — no terminals.
	places := map[string]*Place{
		"P:A": NewPlace("P:A", ColorString, SpaceSurface),
		"P:B": NewPlace("P:B", ColorString, SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:1": NewTransition("T:1", NodeKindTool, []string{"P:A"}, []string{"P:B"}),
		"T:2": NewTransition("T:2", NodeKindTool, []string{"P:B"}, []string{"P:A"}),
	}

	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)
	terminals := c.TerminalPlaces()

	if len(terminals) != 0 {
		t.Fatalf("TerminalPlaces count = %d, want 0", len(terminals))
	}
}

func TestCPN_TerminalPlaces_NoTransitions(t *testing.T) {
	places := map[string]*Place{
		"P:A": NewPlace("P:A", ColorString, SpaceSurface),
		"P:B": NewPlace("P:B", ColorString, SpaceSurface),
	}

	c := NewCPN("test", "w", 0, ModeMAS, "s", places, map[string]*Transition{})
	terminals := c.TerminalPlaces()

	// All places are terminal when there are no transitions.
	if len(terminals) != 2 {
		t.Fatalf("TerminalPlaces count = %d, want 2", len(terminals))
	}
}

func TestCPN_IsComplete(t *testing.T) {
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:A": NewTransition("T:A", NodeKindTool, []string{"P:IN"}, []string{"P:OUT"}),
	}

	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	// Terminal is P:OUT, which is empty.
	if c.IsComplete() {
		t.Fatal("IsComplete() = true, want false (terminal empty)")
	}

	// Deposit a token into P:OUT.
	_ = places["P:OUT"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "done"})

	if !c.IsComplete() {
		t.Fatal("IsComplete() = false, want true (terminal has token)")
	}
}

func TestCPN_IsComplete_EmptyTerminal(t *testing.T) {
	places := map[string]*Place{
		"P:IN":   NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT1": NewPlace("P:OUT1", ColorString, SpaceSurface),
		"P:OUT2": NewPlace("P:OUT2", ColorString, SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:A": NewTransition("T:A", NodeKindTool, []string{"P:IN"}, []string{"P:OUT1", "P:OUT2"}),
	}

	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	// Only one terminal has a token.
	_ = places["P:OUT1"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "x"})

	if c.IsComplete() {
		t.Fatal("IsComplete() = true, want false (P:OUT2 empty)")
	}
}

func TestCPN_IsComplete_NoTerminals(t *testing.T) {
	// Circular topology — no terminals.
	places := map[string]*Place{
		"P:A": NewPlace("P:A", ColorString, SpaceSurface),
		"P:B": NewPlace("P:B", ColorString, SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:1": NewTransition("T:1", NodeKindTool, []string{"P:A"}, []string{"P:B"}),
		"T:2": NewTransition("T:2", NodeKindTool, []string{"P:B"}, []string{"P:A"}),
	}

	c := NewCPN("test", "w", 0, ModeMAS, "s", places, transitions)

	if c.IsComplete() {
		t.Fatal("IsComplete() = true, want false (no terminal places)")
	}
}

func TestCPN_SetGetState_ThreadSafety(t *testing.T) {
	c := NewCPN("test", "w", 0, ModeMAS, "s", nil, nil)

	const n = 1000
	var wg sync.WaitGroup
	wg.Add(2 * n)

	for range n {
		go func() {
			defer wg.Done()
			c.setState(StateRunning)
		}()
		go func() {
			defer wg.Done()
			_ = c.getState()
		}()
	}
	wg.Wait()

	// No race condition — just verify we can read the state.
	s := c.getState()
	if s != StateRunning {
		t.Fatalf("State = %q, want %q", s, StateRunning)
	}
}
