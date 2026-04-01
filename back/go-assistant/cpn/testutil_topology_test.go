package cpn

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// ── Topology Builder Helpers ────────────────────────────────────────────────

// placeSpec describes a place for bulk creation.
type placeSpec struct {
	ID    string
	Color ColorSet
	Space SpaceKind
}

// newPlaces creates a map of places from specs.
func newPlaces(specs ...placeSpec) map[string]*Place {
	m := make(map[string]*Place, len(specs))
	for _, s := range specs {
		m[s.ID] = NewPlace(s.ID, s.Color, s.Space)
	}
	return m
}

// transSpec describes a transition for bulk creation.
type transSpec struct {
	ID       string
	Kind     NodeKind
	Inputs   []string
	Outputs  []string
	Error    string
	Executor func(context.Context, Token) (Token, error)
	HITL     *HITLConfig
}

// newTransitions creates a map of transitions from specs.
func newTransitions(specs ...transSpec) map[string]*Transition {
	m := make(map[string]*Transition, len(specs))
	for _, s := range specs {
		t := NewTransition(s.ID, s.Kind, s.Inputs, s.Outputs)
		t.Executor = s.Executor
		t.ErrorPlace = s.Error
		if s.HITL != nil {
			t.HITLConfig = s.HITL
		}
		m[s.ID] = t
	}
	return m
}

// ── Run Helpers ─────────────────────────────────────────────────────────────

// runWithTimeout runs the CPN with a context deadline and returns the error.
func runWithTimeout(t *testing.T, c *CPN, d time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return c.Run(ctx)
}

// ── Assertion Helpers ───────────────────────────────────────────────────────

// assertCompleted checks the CPN reached StateCompleted.
func assertCompleted(t *testing.T, c *CPN) {
	t.Helper()
	if c.State != StateCompleted {
		t.Fatalf("CPN state = %s, want %s", c.State, StateCompleted)
	}
}

// assertDeadlock checks that the error is ErrDeadlock.
func assertDeadlock(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrDeadlock) {
		t.Fatalf("expected ErrDeadlock, got %v", err)
	}
}

// assertPlaceLen checks the number of tokens in a place.
func assertPlaceLen(t *testing.T, p *Place, want int) {
	t.Helper()
	if got := p.Len(); got != want {
		t.Fatalf("place %s: len = %d, want %d", p.ID, got, want)
	}
}

// ── Event Helpers ───────────────────────────────────────────────────────────

// collectModeEvents extracts mode switch payloads from a slice of Events.
func collectModeEvents(events []Event) []map[string]Mode {
	var result []map[string]Mode
	for _, e := range events {
		if e.Type != EventModeSwitch {
			continue
		}
		if m, ok := e.Payload.(map[string]Mode); ok {
			result = append(result, m)
		}
	}
	return result
}

// threadSafeEventCollector captures events via EventSink in a thread-safe manner.
type threadSafeEventCollector struct {
	mu     sync.Mutex
	events []Event
}

func (ec *threadSafeEventCollector) sink(e *Event) {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	ec.events = append(ec.events, *e)
}

func (ec *threadSafeEventCollector) getEvents() []Event {
	ec.mu.Lock()
	defer ec.mu.Unlock()
	cp := make([]Event, len(ec.events))
	copy(cp, ec.events)
	return cp
}

// ── HITL Helpers ────────────────────────────────────────────────────────────

// feedHITLAsync sends a token to the HITL channel after a delay in a goroutine.
func feedHITLAsync(ch chan Token, delay time.Duration, tok Token) {
	go func() {
		time.Sleep(delay)
		ch <- tok
	}()
}

// makeApproveToken creates a ColorHuman approval token.
func makeApproveToken() Token {
	return Token{
		Color:   ColorHuman,
		Space:   SpaceSurface,
		Payload: HITLResponse{Action: HITLApprove},
	}
}

// makeRejectToken creates a ColorHuman rejection token.
func makeRejectToken() Token {
	return Token{
		Color:   ColorHuman,
		Space:   SpaceSurface,
		Payload: HITLResponse{Action: HITLReject},
	}
}

// ── Tool Executor Helpers ───────────────────────────────────────────────────

// prefixTool returns a tool executor that prepends prefix to the payload string.
func prefixTool(prefix string, outColor ColorSet, outSpace SpaceKind) func(context.Context, Token) (Token, error) {
	return func(_ context.Context, in Token) (Token, error) {
		return Token{
			Color:   outColor,
			Space:   outSpace,
			Payload: prefix + fmt.Sprintf("%v", in.Payload),
		}, nil
	}
}

// mergeTool returns a tool executor that concatenates the payload string
// with a separator. It uses the first consumed token only (as per fireTool).
func mergeTool(outColor ColorSet, outSpace SpaceKind) func(context.Context, Token) (Token, error) {
	return func(_ context.Context, in Token) (Token, error) {
		return Token{
			Color:   outColor,
			Space:   outSpace,
			Payload: fmt.Sprintf("merged:%v", in.Payload),
		}, nil
	}
}

// passThroughTool returns a tool executor that passes the payload unchanged
// but outputs in the specified color and space.
func passThroughTool(outColor ColorSet, outSpace SpaceKind) func(context.Context, Token) (Token, error) {
	return func(_ context.Context, in Token) (Token, error) {
		return Token{
			Color:   outColor,
			Space:   outSpace,
			Payload: in.Payload,
		}, nil
	}
}
