package cpn

import (
	"context"
	"sync/atomic"
	"testing"
)

// TestObserver_ReactsToBashStdout_AC001 asserts spec GAP-9 AC-001:
// given a NodeKindObserver subscribed to EventProcessStdout, when a
// bash transition emits three stdout lines via a mock adapter + the
// streaming path, the observer deposits exactly three ColorEvent tokens.
//
// We exercise fireBash directly so the terminal-result deposit doesn't
// matter for the observer assertion, and then run drainObservers once
// to flush the self-bus into the observation place.
func TestObserver_ReactsToBashStdout_AC001(t *testing.T) {
	pIn := NewPlace("p-in", ColorShellCmd, SpaceComputation)
	pOut := NewPlace("p-out", ColorShellChunk, SpaceComputation)
	pEvents := NewPlace("p-events", ColorEvent, SpaceObservation)
	_ = pIn.Deposit(&Token{Color: ColorShellCmd, Space: SpaceComputation, Payload: "go"})

	bash := &Transition{
		ID:           "t-bash",
		Kind:         NodeKindBash,
		InputPlaces:  []string{"p-in"},
		OutputPlaces: []string{"p-out"},
		BashConfig: &BashConfig{
			Command:   "echo",
			Streaming: true,
		},
	}

	obs := &Transition{
		ID:           "obs-stdout",
		Kind:         NodeKindObserver,
		OutputPlaces: []string{"p-events"},
		EventFilter: func(e Event) bool {
			return e.Type == EventProcessStdout
		},
	}

	places := map[string]*Place{"p-in": pIn, "p-out": pOut, "p-events": pEvents}
	transitions := map[string]*Transition{bash.ID: bash, obs.ID: obs}
	c := NewCPN("cpn-obs-proc", "test", 0, ModeMAS, "sess-obs", places, transitions)
	c.HostRuntime = &HostRuntime{
		Adapter: &mockHostAdapter{
			execResult: ExecResult{ExitCode: 0, Stdout: []byte("1\n2\n3\n")},
		},
		Gate: denyGate{allow: true},
	}

	// Fire bash directly so the eventual ColorShellResult→ColorShellChunk
	// deposit error doesn't short-circuit the test. The stdout events
	// have already landed on the self-bus by the time fireBash returns.
	_, _, _ = fireBash(context.Background(), bash, c, nil)

	// Drain once — observer now deposits the three EventProcessStdout
	// events into p-events.
	drainObservers(context.Background(), c)

	// Exactly three observation tokens, one per echoed line.
	if pEvents.Len() != 3 {
		t.Fatalf("observation tokens = %d, want 3", pEvents.Len())
	}

	for i := 0; i < 3; i++ {
		tok, err := pEvents.Consume()
		if err != nil {
			t.Fatalf("Consume #%d: %v", i, err)
		}
		if tok.Color != ColorEvent {
			t.Errorf("tok[%d].Color = %q, want %q", i, tok.Color, ColorEvent)
		}
		ev, ok := tok.Payload.(Event)
		if !ok {
			t.Fatalf("tok[%d].Payload type = %T, want Event", i, tok.Payload)
		}
		if ev.Type != EventProcessStdout {
			t.Errorf("tok[%d].Event.Type = %q, want %q", i, ev.Type, EventProcessStdout)
		}
		pe, ok := CastProcessEvent(ev)
		if !ok {
			t.Fatalf("tok[%d]: CastProcessEvent failed", i)
		}
		if pe.Kind != EventProcessStdout {
			t.Errorf("tok[%d].ProcessEvent.Kind = %q, want %q", i, pe.Kind, EventProcessStdout)
		}
	}
}

// TestObserver_ReactsToBashExit_AC003 asserts spec GAP-9 AC-003:
// given a NodeKindObserver without a filter, when a process exits
// exactly one EventProcessExit observation token is deposited.
func TestObserver_ReactsToBashExit_AC003(t *testing.T) {
	pIn := NewPlace("p-in", ColorShellCmd, SpaceComputation)
	pOut := NewPlace("p-out", ColorShellResult, SpaceComputation)
	pEvents := NewPlace("p-events", ColorEvent, SpaceObservation)
	_ = pIn.Deposit(&Token{Color: ColorShellCmd, Space: SpaceComputation, Payload: "go"})

	bash := &Transition{
		ID:           "t-bash",
		Kind:         NodeKindBash,
		InputPlaces:  []string{"p-in"},
		OutputPlaces: []string{"p-out"},
		BashConfig: &BashConfig{
			Command: "echo",
		},
	}

	obs := &Transition{
		ID:           "obs-exit",
		Kind:         NodeKindObserver,
		OutputPlaces: []string{"p-events"},
		// No EventFilter — accept all events.
	}

	places := map[string]*Place{"p-in": pIn, "p-out": pOut, "p-events": pEvents}
	transitions := map[string]*Transition{bash.ID: bash, obs.ID: obs}
	c := NewCPN("cpn-obs-exit", "test", 0, ModeMAS, "sess-obs-exit", places, transitions)
	c.HostRuntime = &HostRuntime{
		Adapter: &mockHostAdapter{
			execResult: ExecResult{ExitCode: 0, Stdout: []byte("hi\n")},
		},
		Gate: denyGate{allow: true},
	}

	_, _, _ = fireBash(context.Background(), bash, c, nil)
	drainObservers(context.Background(), c)

	// Drain the observation place; count exit events.
	var exitSeen int
	for pEvents.Len() > 0 {
		tok, err := pEvents.Consume()
		if err != nil {
			break
		}
		ev, ok := tok.Payload.(Event)
		if !ok {
			continue
		}
		if ev.Type == EventProcessExit {
			exitSeen++
		}
	}
	if exitSeen != 1 {
		t.Fatalf("exit observation tokens = %d, want 1", exitSeen)
	}
}

// TestObserver_SameCPNObservesOwnBash confirms that process events
// emitted by a NodeKindBash transition in CPN X are visible to a
// NodeKindObserver in the SAME CPN X (not only to parent observers
// watching X as a sub-CPN). This guards the "self-bus" wiring added
// by GAP-9.
func TestObserver_SameCPNObservesOwnBash(t *testing.T) {
	pIn := NewPlace("p-in", ColorShellCmd, SpaceComputation)
	pOut := NewPlace("p-out", ColorShellChunk, SpaceComputation)
	pEvents := NewPlace("p-events", ColorEvent, SpaceObservation)
	_ = pIn.Deposit(&Token{Color: ColorShellCmd, Space: SpaceComputation, Payload: "go"})

	bash := &Transition{
		ID:           "t-bash",
		Kind:         NodeKindBash,
		InputPlaces:  []string{"p-in"},
		OutputPlaces: []string{"p-out"},
		BashConfig: &BashConfig{
			Command:   "echo",
			Streaming: true,
		},
	}
	obs := &Transition{
		ID:           "obs-self",
		Kind:         NodeKindObserver,
		OutputPlaces: []string{"p-events"},
		EventFilter: func(e Event) bool {
			_, ok := CastProcessEvent(e)
			return ok
		},
	}

	places := map[string]*Place{"p-in": pIn, "p-out": pOut, "p-events": pEvents}
	transitions := map[string]*Transition{bash.ID: bash, obs.ID: obs}
	c := NewCPN("cpn-self", "test", 0, ModeMAS, "sess-self", places, transitions)
	c.HostRuntime = &HostRuntime{
		Adapter: &mockHostAdapter{execResult: ExecResult{ExitCode: 0, Stdout: []byte("a\nb\n")}},
		Gate:    denyGate{allow: true},
	}

	_, _, _ = fireBash(context.Background(), bash, c, nil)
	drainObservers(context.Background(), c)

	// Expected events: started + 2 stdout + exit = 4 observations.
	if pEvents.Len() < 3 {
		t.Fatalf("p-events = %d, want >= 3 (started+stdout*2+exit)", pEvents.Len())
	}
}

// fakeRateLimiter lets tests deterministically steer Allow outcomes.
type fakeRateLimiter struct {
	allow int32 // set to 0 to drop
	calls int64
}

func (f *fakeRateLimiter) Allow(string) bool {
	atomic.AddInt64(&f.calls, 1)
	return atomic.LoadInt32(&f.allow) == 1
}

// TestEmitProcessEvent_RateLimiterDropsBumpMetrics verifies that when
// the rate limiter denies an event, the metrics port's OnDrop is
// bumped and NO event reaches EventSink — satisfying AC-005.
func TestEmitProcessEvent_RateLimiterDropsBumpMetrics(t *testing.T) {
	places := map[string]*Place{
		"p-events": NewPlace("p-events", ColorEvent, SpaceObservation),
	}
	c := NewCPN("c", "test", 0, ModeMAS, "sess", places, nil)

	limiter := &fakeRateLimiter{allow: 0}
	metrics := &captureMetrics{}
	c.HostRuntime = &HostRuntime{RateLimiter: limiter, Metrics: metrics}

	var sunk int32
	c.EventSink = func(*Event) { atomic.AddInt32(&sunk, 1) }

	emitProcessEvent(c, nil, EventProcessStdout, ProcessOutputPayload{SessionID: "x"})
	emitProcessEvent(c, nil, EventProcessStdout, ProcessOutputPayload{SessionID: "x"})

	if metrics.drops != 2 {
		t.Fatalf("drops = %d, want 2", metrics.drops)
	}
	if metrics.emits != 0 {
		t.Fatalf("emits = %d, want 0", metrics.emits)
	}
	if atomic.LoadInt32(&sunk) != 0 {
		t.Fatalf("EventSink reached %d times, want 0 — drops should never reach the sink", sunk)
	}
}

// captureMetrics is a minimal in-memory ProcessEventMetrics.
type captureMetrics struct {
	emits int
	drops int
}

func (m *captureMetrics) OnEmit(string, EventType)         { m.emits++ }
func (m *captureMetrics) OnDrop(string, EventType, string) { m.drops++ }

// TestCastProcessEvent_RejectsNonProcessEvents proves CastProcessEvent
// returns ok=false for non-process event kinds — important so observer
// filters can use it as an exclusion predicate.
func TestCastProcessEvent_RejectsNonProcessEvents(t *testing.T) {
	cases := []Event{
		{Type: EventTransitionFired, Payload: nil},
		{Type: EventProcessStdout, Payload: "not a payload"},
		{Type: EventProcessStdout, Payload: ProcessOutputPayload{Line: "ok"}}, // this one should pass
	}
	results := []bool{false, false, true}
	for i, e := range cases {
		_, ok := CastProcessEvent(e)
		if ok != results[i] {
			t.Errorf("case %d: ok=%v, want %v", i, ok, results[i])
		}
	}
}
