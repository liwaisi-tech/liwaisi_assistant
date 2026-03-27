package cpn

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"
)

// ── Test Helpers ─────────────────────────────────────────────────────────────

// hitlTestCPN creates a CPN with a single HITL transition for testing.
// outputSpace controls the Space of the output place (SpaceSurface or SpaceComputation).
// channelBuf sets the buffer size for the HITL channel (0 for unbuffered).
func hitlTestCPN(outputSpace SpaceKind, channelBuf int) (*CPN, chan Token) {
	ch := make(chan Token, channelBuf)
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorHuman, outputSpace),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "request"})
	transitions := map[string]*Transition{
		"T:HITL": {
			ID: "T:HITL", Kind: NodeKindHITL,
			InputPlaces: []string{"P:IN"}, OutputPlaces: []string{"P:OUT"},
			HITLConfig: &HITLConfig{Channel: ch, Prompt: "Please approve"},
		},
	}
	cpn := NewCPN("test-cpn", "worker", 0, ModeMAS, "sess-1", places, transitions)
	return cpn, ch
}

// approveToken returns a ColorHuman approval token.
func approveToken() Token {
	return Token{
		Color:   ColorHuman,
		Space:   SpaceSurface,
		Payload: HITLResponse{Action: HITLApprove},
	}
}

// mockGroupNotifier records SwitchCMP calls.
type mockGroupNotifier struct {
	mu    sync.Mutex
	calls []string
}

func (m *mockGroupNotifier) SwitchCMP(cpnID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, cpnID)
}

func (m *mockGroupNotifier) getCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]string, len(m.calls))
	copy(cp, m.calls)
	return cp
}

// eventCollector captures events via EventSink.
type eventCollector struct {
	mu     sync.Mutex
	events []Event
}

func (ec *eventCollector) sink(e *Event) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.events = append(ec.events, *e)
}

func (ec *eventCollector) getEvents() []Event {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	cp := make([]Event, len(ec.events))
	copy(cp, ec.events)
	return cp
}

// ── Happy Path ──────────────────────────────────────────────────────────────

func TestFireHITL_BasicApproval(t *testing.T) {
	c, ch := hitlTestCPN(SpaceSurface, 1)
	ch <- approveToken()

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if c.State != StateCompleted {
		t.Errorf("expected StateCompleted, got %s", c.State)
	}
	if c.Places["P:OUT"].Len() != 1 {
		t.Errorf("expected 1 token in P:OUT, got %d", c.Places["P:OUT"].Len())
	}
}

func TestFireHITL_BlocksUntilInput(t *testing.T) {
	c, ch := hitlTestCPN(SpaceSurface, 0) // unbuffered

	waiting := make(chan struct{})
	done := make(chan error, 1)

	// Monitor state changes to detect StateWaiting.
	ec := &eventCollector{}
	c.EventSink = func(e *Event) {
		ec.sink(e)
		if e.Type == EventHITLRequested {
			close(waiting)
		}
	}

	go func() {
		done <- c.Run(context.Background())
	}()

	// Wait for CPN to enter waiting state.
	select {
	case <-waiting:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for StateWaiting")
	}

	if c.getState() != StateWaiting {
		t.Errorf("expected StateWaiting, got %s", c.getState())
	}

	// Send approval to unblock.
	ch <- approveToken()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for completion")
	}

	if c.getState() != StateCompleted {
		t.Errorf("expected StateCompleted, got %s", c.getState())
	}
}

// ── Timeout ─────────────────────────────────────────────────────────────────

func TestFireHITL_ContextTimeout(t *testing.T) {
	c, _ := hitlTestCPN(SpaceSurface, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := c.Run(ctx)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got %v", err)
	}
	if c.getState() != StateFailed {
		t.Errorf("expected StateFailed, got %s", c.getState())
	}
}

// ── Color Validation ────────────────────────────────────────────────────────

func TestFireHITL_RejectsNonHumanColor(t *testing.T) {
	c, ch := hitlTestCPN(SpaceSurface, 1)
	// Send a ColorString token instead of ColorHuman.
	ch <- Token{Color: ColorString, Space: SpaceSurface, Payload: "not human"}

	err := c.Run(context.Background())
	if !errors.Is(err, ErrColorMismatch) {
		t.Fatalf("expected ErrColorMismatch, got %v", err)
	}
}

// ── Rejection ───────────────────────────────────────────────────────────────

func TestFireHITL_HITLReject(t *testing.T) {
	c, ch := hitlTestCPN(SpaceSurface, 1)
	ch <- Token{
		Color:   ColorHuman,
		Space:   SpaceSurface,
		Payload: HITLResponse{Action: HITLReject},
	}

	err := c.Run(context.Background())
	if !errors.Is(err, ErrHITLRejected) {
		t.Fatalf("expected ErrHITLRejected, got %v", err)
	}
}

// ── Mode Switch ─────────────────────────────────────────────────────────────

func TestFireHITL_ModeSwitchCentaurian(t *testing.T) {
	// Output place in SpaceComputation → triggers Centaurian mode switch.
	c, ch := hitlTestCPN(SpaceComputation, 1)
	ch <- approveToken()

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	c.mu.RLock()
	mode := c.Mode
	c.mu.RUnlock()
	if mode != ModeCentaurian {
		t.Errorf("expected ModeCentaurian, got %s", mode)
	}
}

func TestFireHITL_NoModeSwitchSurface(t *testing.T) {
	// Output place in SpaceSurface → ModeMAS unchanged.
	c, ch := hitlTestCPN(SpaceSurface, 1)
	ch <- approveToken()

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	c.mu.RLock()
	mode := c.Mode
	c.mu.RUnlock()
	if mode != ModeMAS {
		t.Errorf("expected ModeMAS, got %s", mode)
	}
}

// ── Events ──────────────────────────────────────────────────────────────────

func TestFireHITL_EmitsEvents(t *testing.T) {
	c, ch := hitlTestCPN(SpaceSurface, 1)
	ec := &eventCollector{}
	c.EventSink = ec.sink

	ch <- approveToken()

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	events := ec.getEvents()

	// Must have at least EventHITLRequested and EventHITLResolved.
	var hasRequested, hasResolved bool
	for _, e := range events {
		switch e.Type {
		case EventHITLRequested:
			hasRequested = true
			if e.Payload != "Please approve" {
				t.Errorf("expected prompt payload, got %v", e.Payload)
			}
			if e.CPNID != "test-cpn" {
				t.Errorf("expected CPNID=test-cpn, got %s", e.CPNID)
			}
			if e.SessionID != "sess-1" {
				t.Errorf("expected SessionID=sess-1, got %s", e.SessionID)
			}
		case EventHITLResolved:
			hasResolved = true
		}
	}
	if !hasRequested {
		t.Error("EventHITLRequested not emitted")
	}
	if !hasResolved {
		t.Error("EventHITLResolved not emitted")
	}
}

func TestFireHITL_EventModeSwitch(t *testing.T) {
	c, ch := hitlTestCPN(SpaceComputation, 1)
	ec := &eventCollector{}
	c.EventSink = ec.sink

	ch <- approveToken()

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	events := ec.getEvents()
	var hasModeSwitch bool
	for _, e := range events {
		if e.Type == EventModeSwitch {
			hasModeSwitch = true
			if e.Payload != ModeCentaurian {
				t.Errorf("expected ModeCentaurian payload, got %v", e.Payload)
			}
		}
	}
	if !hasModeSwitch {
		t.Error("EventModeSwitch not emitted")
	}
}

// ── GroupNotifier ───────────────────────────────────────────────────────────

func TestFireHITL_GroupNotifierCalled(t *testing.T) {
	c, ch := hitlTestCPN(SpaceSurface, 1)
	gn := &mockGroupNotifier{}
	c.GroupNotifier = gn

	ch <- approveToken()

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	calls := gn.getCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 SwitchCMP calls, got %d: %v", len(calls), calls)
	}
	for _, call := range calls {
		if call != "test-cpn" {
			t.Errorf("expected cpnID=test-cpn, got %s", call)
		}
	}
}

func TestFireHITL_NilGroupNotifier(t *testing.T) {
	// Nil GroupNotifier → no panic, events still emitted.
	c, ch := hitlTestCPN(SpaceSurface, 1)
	c.GroupNotifier = nil
	ec := &eventCollector{}
	c.EventSink = ec.sink

	ch <- approveToken()

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	events := ec.getEvents()
	if len(events) == 0 {
		t.Error("expected events to be emitted even with nil GroupNotifier")
	}
}

// ── Space Bridging ──────────────────────────────────────────────────────────

func TestFireHITL_SpaceBridging(t *testing.T) {
	// Output place is SpaceComputation — token Space must be adjusted.
	c, ch := hitlTestCPN(SpaceComputation, 1)
	ch <- approveToken()

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	tokens, ok := c.Places["P:OUT"].Peek()
	if !ok || len(tokens) == 0 {
		t.Fatal("expected token in P:OUT")
	}
	if tokens[0].Space != SpaceComputation {
		t.Errorf("expected token Space=SpaceComputation, got %s", tokens[0].Space)
	}
	if tokens[0].OriginKind != NodeKindHITL {
		t.Errorf("expected OriginKind=NodeKindHITL, got %s", tokens[0].OriginKind)
	}
}

// ── Closed Channel Edge Case ────────────────────────────────────────────────

func TestFireHITL_ClosedChannel(t *testing.T) {
	c, ch := hitlTestCPN(SpaceSurface, 0)
	close(ch)

	err := c.Run(context.Background())
	// Closed channel returns zero-value Token (Color="") → ErrColorMismatch.
	if !errors.Is(err, ErrColorMismatch) {
		t.Fatalf("expected ErrColorMismatch from closed channel, got %v", err)
	}
}

// ── Validate ────────────────────────────────────────────────────────────────

func TestValidate_HITLMissingConfig(t *testing.T) {
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorHuman, SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:HITL": {
			ID: "T:HITL", Kind: NodeKindHITL,
			InputPlaces: []string{"P:IN"}, OutputPlaces: []string{"P:OUT"},
			// HITLConfig deliberately nil.
		},
	}

	err := Validate(places, transitions)
	if !errors.Is(err, ErrHITLMisconfigured) {
		t.Fatalf("expected ErrHITLMisconfigured, got %v", err)
	}
}

func TestValidate_HITLNilChannel(t *testing.T) {
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorHuman, SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:HITL": {
			ID: "T:HITL", Kind: NodeKindHITL,
			InputPlaces: []string{"P:IN"}, OutputPlaces: []string{"P:OUT"},
			HITLConfig: &HITLConfig{
				Channel: nil, // Deliberately nil.
				Prompt:  "approve?",
			},
		},
	}

	err := Validate(places, transitions)
	if !errors.Is(err, ErrHITLMisconfigured) {
		t.Fatalf("expected ErrHITLMisconfigured, got %v", err)
	}
}

func TestValidate_HITLValid(t *testing.T) {
	ch := make(chan Token, 1)
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorHuman, SpaceSurface),
	}
	transitions := map[string]*Transition{
		"T:HITL": {
			ID: "T:HITL", Kind: NodeKindHITL,
			InputPlaces: []string{"P:IN"}, OutputPlaces: []string{"P:OUT"},
			HITLConfig: &HITLConfig{Channel: ch, Prompt: "approve?"},
		},
	}

	err := Validate(places, transitions)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// ── Goroutine Leak Detection ────────────────────────────────────────────────

func TestFireHITL_NoGoroutineLeak(t *testing.T) {
	// Baseline goroutine count.
	runtime.GC()
	time.Sleep(10 * time.Millisecond)
	before := runtime.NumGoroutine()

	for i := 0; i < 5; i++ {
		c, _ := hitlTestCPN(SpaceSurface, 0)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		_ = c.Run(ctx)
		cancel()
	}

	// Allow goroutines to settle.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	after := runtime.NumGoroutine()

	// Allow small variance (runtime overhead), but no unbounded growth.
	if after > before+3 {
		t.Errorf("possible goroutine leak: before=%d, after=%d", before, after)
	}
}
