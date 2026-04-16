package cpn

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
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
			payload, ok := e.Payload.(map[string]Mode)
			if !ok {
				t.Errorf("expected map[string]Mode payload, got %T", e.Payload)
			} else if payload["to"] != ModeCentaurian {
				t.Errorf("expected to=ModeCentaurian, got %v", payload["to"])
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
	// FIX-003: Closed channel is now detected via two-value receive,
	// returning an explicit "HITL channel closed" error instead of ErrColorMismatch.
	if err == nil {
		t.Fatal("expected error from closed channel, got nil")
	}
	if !strings.Contains(err.Error(), "HITL channel closed") {
		t.Fatalf("expected 'HITL channel closed' error, got %v", err)
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

// ── Structured submit (OutputBuilder + A2UIPayloadBuilder) ──────────────────

func TestFireHITL_StructuredSubmitWithOutputBuilder(t *testing.T) {
	ch := make(chan Token, 1)
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorJSON, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
	}
	// Seed the input place with a "questionnaire" JSON token (simulating the
	// upstream t-ask LLM output).
	_ = places["P:IN"].Deposit(&Token{
		Color:   ColorJSON,
		Space:   SpaceSurface,
		Payload: `{"questions":[{"id":"q1","prompt":"Audience?","options":[{"id":"opt-a","label":"Beginners"},{"id":"opt-b","label":"Experts"}]}]}`,
	})

	var (
		gotConsumedQuestions int
		gotAnswers           map[string]string
		gotResponseAction    HITLAction
	)

	transitions := map[string]*Transition{
		"T:HITL": {
			ID: "T:HITL", Kind: NodeKindHITL,
			InputPlaces: []string{"P:IN"}, OutputPlaces: []string{"P:OUT"},
			HITLConfig: &HITLConfig{
				Channel: ch,
				Prompt:  "Answer please",
				A2UIPayloadBuilder: func(consumed []Token) (any, error) {
					if len(consumed) != 1 {
						t.Errorf("A2UIPayloadBuilder consumed len = %d, want 1", len(consumed))
					}
					return map[string]any{"components": []any{"q1"}}, nil
				},
				OutputBuilder: func(consumed []Token, resp HITLResponse) (Token, error) {
					gotResponseAction = resp.Action
					if len(consumed) == 1 {
						if s, ok := consumed[0].Payload.(string); ok && strings.Contains(s, "Audience") {
							gotConsumedQuestions = 1
						}
					}
					_ = json.Unmarshal([]byte(resp.Content), &gotAnswers)
					return Token{
						Color:   ColorString,
						Payload: "Clarification answers:\n- Audience?: Beginners\n",
					}, nil
				},
			},
		},
	}
	c := NewCPN("test-cpn", "worker", 0, ModeMAS, "sess-1", places, transitions)

	// Capture emitted events to confirm the A2UI stream chunk fired.
	ec := &eventCollector{}
	c.EventSink = ec.sink

	// Submit the structured response.
	ch <- Token{
		Color: ColorHuman,
		Space: SpaceSurface,
		Payload: HITLResponse{
			Action:  HITLSubmit,
			Content: `{"q1":"opt-a"}`,
		},
	}

	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if gotResponseAction != HITLSubmit {
		t.Errorf("OutputBuilder action = %q, want submit", gotResponseAction)
	}
	if gotConsumedQuestions != 1 {
		t.Error("OutputBuilder did not receive the consumed questionnaire token")
	}
	if gotAnswers["q1"] != "opt-a" {
		t.Errorf("answers[q1] = %q, want opt-a", gotAnswers["q1"])
	}

	// Output token must be present, ColorString, with merged content.
	tokens, ok := c.Places["P:OUT"].Peek()
	if !ok || len(tokens) != 1 {
		t.Fatalf("expected 1 token in P:OUT, got %d", len(tokens))
	}
	if tokens[0].Color != ColorString {
		t.Errorf("output color = %s, want ColorString", tokens[0].Color)
	}
	if s, ok := tokens[0].Payload.(string); !ok || !strings.Contains(s, "Clarification answers") {
		t.Errorf("unexpected output payload: %v", tokens[0].Payload)
	}

	// Verify an A2UI stream chunk was emitted before the HITL request.
	var sawA2UI bool
	for _, e := range ec.getEvents() {
		if e.Type == EventStreamChunk {
			if chunk, ok := e.Payload.(StreamChunk); ok && strings.HasPrefix(chunk.Content, "$$a2ui:") {
				sawA2UI = true
			}
		}
	}
	if !sawA2UI {
		t.Error("expected an EventStreamChunk with $$a2ui: prefix")
	}
}

// ── A2UI Persistence (REQ-001/002/005, INV-002) ─────────────────────────────

// hitlA2UITestCPN builds a CPN with a single HITL transition whose
// A2UIPayloadBuilder is configurable per-test. The channel is pre-seeded
// with an approval token on buffer so fireHITL unblocks immediately.
func hitlA2UITestCPN(builder func(consumed []Token) (any, error)) (*CPN, chan Token) {
	ch := make(chan Token, 1)
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorHuman, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "request"})
	transitions := map[string]*Transition{
		"T:HITL": {
			ID: "T:HITL", Kind: NodeKindHITL,
			InputPlaces: []string{"P:IN"}, OutputPlaces: []string{"P:OUT"},
			HITLConfig: &HITLConfig{
				Channel:            ch,
				Prompt:             "Please approve",
				A2UIPayloadBuilder: builder,
			},
		},
	}
	cpn := NewCPN("test-cpn", "worker", 0, ModeMAS, "sess-1", places, transitions)
	return cpn, ch
}

func TestA2UIMarkerConstant_Value(t *testing.T) {
	if A2UIMarker != "$$a2ui:" {
		t.Fatalf("A2UIMarker = %q, want %q", A2UIMarker, "$$a2ui:")
	}
}

func TestFireHITL_AppendsA2UIMarkerToHistory_OnSuccess(t *testing.T) {
	builder := func(consumed []Token) (any, error) {
		return map[string]any{
			"components": []any{
				map[string]any{
					"type": "questionnaire",
					"props": map[string]any{
						"componentId": "T:HITL",
					},
				},
			},
		}, nil
	}
	c, ch := hitlA2UITestCPN(builder)
	ec := &eventCollector{}
	c.EventSink = ec.sink

	// Capture pre-existing history length (fireHITL MUST only append one new row).
	c.mu.RLock()
	before := len(c.History)
	c.mu.RUnlock()

	ch <- approveToken()

	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c.mu.RLock()
	after := append([]*Message(nil), c.History...)
	c.mu.RUnlock()

	// Two new entries: the A2UI surface (RoleAssistant) + the response
	// row (RoleUser) appended by fireHITL after resolution
	// (spec-process-bugfix-a2ui-hitl-response-persistence.md REQ-001/002).
	if got := len(after) - before; got != 2 {
		t.Fatalf("expected exactly 2 new history entries, got %d (total=%d)", got, len(after))
	}
	m := after[before]
	if m.Role != RoleAssistant {
		t.Errorf("Role = %q, want %q", m.Role, RoleAssistant)
	}
	if !strings.HasPrefix(m.Content, A2UIMarker) {
		t.Errorf("Content does not start with %q: %q", A2UIMarker, m.Content)
	}
	if m.CPNID != c.ID {
		t.Errorf("CPNID = %q, want %q", m.CPNID, c.ID)
	}
	if m.CPNRole != c.Role {
		t.Errorf("CPNRole = %q, want %q", m.CPNRole, c.Role)
	}
	if m.CPNDepth != c.Depth {
		t.Errorf("CPNDepth = %d, want %d", m.CPNDepth, c.Depth)
	}

	// Byte-identical match: find the emitted StreamChunk and compare.
	var emitted string
	var sawChunk bool
	for _, e := range ec.getEvents() {
		if e.Type != EventStreamChunk {
			continue
		}
		chunk, ok := e.Payload.(StreamChunk)
		if !ok {
			continue
		}
		if strings.HasPrefix(chunk.Content, A2UIMarker) {
			emitted = chunk.Content
			sawChunk = true
			break
		}
	}
	if !sawChunk {
		t.Fatal("expected an EventStreamChunk with A2UI prefix")
	}
	if m.Content != emitted {
		t.Errorf("history content does not match emitted chunk:\nhistory:  %q\nemitted:  %q", m.Content, emitted)
	}
}

func TestFireHITL_NoHistoryAppend_WhenBuilderReturnsNil(t *testing.T) {
	builder := func(consumed []Token) (any, error) { return nil, nil }
	c, ch := hitlA2UITestCPN(builder)

	c.mu.RLock()
	before := len(c.History)
	c.mu.RUnlock()

	ch <- approveToken()
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c.mu.RLock()
	after := len(c.History)
	c.mu.RUnlock()
	if after != before {
		t.Errorf("history length changed: before=%d after=%d (expected no append)", before, after)
	}
}

func TestFireHITL_NoHistoryAppend_WhenBuilderErrors(t *testing.T) {
	builder := func(consumed []Token) (any, error) {
		return map[string]any{"x": 1}, errors.New("boom")
	}
	c, ch := hitlA2UITestCPN(builder)

	c.mu.RLock()
	before := len(c.History)
	c.mu.RUnlock()

	ch <- approveToken()
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c.mu.RLock()
	after := len(c.History)
	c.mu.RUnlock()
	if after != before {
		t.Errorf("history length changed: before=%d after=%d (expected no append)", before, after)
	}
}

func TestFireHITL_NoHistoryAppend_WhenMarshalFails(t *testing.T) {
	// channels cannot be JSON-marshaled; json.Marshal returns an error.
	builder := func(consumed []Token) (any, error) {
		return map[string]any{"bad": make(chan int)}, nil
	}
	c, ch := hitlA2UITestCPN(builder)

	c.mu.RLock()
	before := len(c.History)
	c.mu.RUnlock()

	ch <- approveToken()
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c.mu.RLock()
	after := len(c.History)
	c.mu.RUnlock()
	if after != before {
		t.Errorf("history length changed: before=%d after=%d (expected no append)", before, after)
	}
}

func TestFireHITL_DoneSentinel_StillEmittedAfterA2UI(t *testing.T) {
	builder := func(consumed []Token) (any, error) {
		return map[string]any{"components": []any{}}, nil
	}
	c, ch := hitlA2UITestCPN(builder)
	ec := &eventCollector{}
	c.EventSink = ec.sink

	ch <- approveToken()
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []StreamChunk
	for _, e := range ec.getEvents() {
		if e.Type != EventStreamChunk {
			continue
		}
		if chunk, ok := e.Payload.(StreamChunk); ok {
			chunks = append(chunks, chunk)
		}
	}
	if len(chunks) < 2 {
		t.Fatalf("expected at least 2 StreamChunk events, got %d", len(chunks))
	}
	if !strings.HasPrefix(chunks[0].Content, A2UIMarker) {
		t.Errorf("first chunk Content = %q, want prefix %q", chunks[0].Content, A2UIMarker)
	}
	if chunks[0].Done {
		t.Error("first chunk Done = true, want false")
	}
	if chunks[1].Content != "" {
		t.Errorf("second chunk Content = %q, want empty", chunks[1].Content)
	}
	if !chunks[1].Done {
		t.Error("second chunk Done = false, want true (sentinel)")
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

// ── HITL response persistence (REQ-001..005, INV-001) ───────────────────────
// See spec-process-bugfix-a2ui-hitl-response-persistence.md. After fireHITL
// receives the human response on its channel, it MUST append a RoleUser
// Message to c.History whose CPNID equals the CPN id, whose Content reflects
// the action, and whose ParentMessageID links to the most recent A2UI surface
// row from the same CPN. Reject and error paths MUST NOT append.

const submitAnswersJSON = `{"answers":{"q1":"opt-a","q2":"Mi respuesta personalizada"}}`

func submitToken(content string) Token {
	return Token{
		Color:   ColorHuman,
		Space:   SpaceSurface,
		Payload: HITLResponse{Action: HITLSubmit, Content: content},
	}
}

// hitlClarifyTestCPN builds a CPN with an A2UI builder + an OutputBuilder
// that mirrors t-clarify's structured-output path. The output place is
// ColorString so the test exercises the OutputBuilder branch of fireHITL.
func hitlClarifyTestCPN(builder func(consumed []Token) (any, error)) (*CPN, chan Token) {
	ch := make(chan Token, 1)
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "request"})
	transitions := map[string]*Transition{
		"T:HITL": {
			ID: "T:HITL", Kind: NodeKindHITL,
			InputPlaces: []string{"P:IN"}, OutputPlaces: []string{"P:OUT"},
			HITLConfig: &HITLConfig{
				Channel:            ch,
				Prompt:             "Please respond",
				A2UIPayloadBuilder: builder,
				OutputBuilder: func(consumed []Token, resp HITLResponse) (Token, error) {
					return Token{
						Color:   ColorString,
						Payload: "synthesized:" + resp.Content,
					}, nil
				},
			},
		},
	}
	cpn := NewCPN("test-cpn", "worker", 0, ModeMAS, "sess-1", places, transitions)
	return cpn, ch
}

func defaultA2UIBuilder() func(consumed []Token) (any, error) {
	return func(consumed []Token) (any, error) {
		return map[string]any{
			"components": []any{
				map[string]any{
					"type":  "questionnaire",
					"props": map[string]any{"componentId": "T:HITL"},
				},
			},
		}, nil
	}
}

func TestFireHITL_AppendsResponseToHistory_OnSubmit_OutputBuilderPath(t *testing.T) {
	c, ch := hitlClarifyTestCPN(defaultA2UIBuilder())
	ch <- submitToken(submitAnswersJSON)

	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c.mu.RLock()
	hist := append([]*Message(nil), c.History...)
	c.mu.RUnlock()

	// Expect 2 history rows: assistant A2UI surface (from fireHITL append)
	// + user response row (REQ-001/002 OutputBuilder branch).
	if len(hist) != 2 {
		t.Fatalf("expected 2 history rows, got %d: %+v", len(hist), hist)
	}
	a2ui, resp := hist[0], hist[1]

	if a2ui.Role != RoleAssistant || !strings.HasPrefix(a2ui.Content, A2UIMarker) {
		t.Fatalf("first row should be the assistant A2UI surface; got role=%q content=%q", a2ui.Role, a2ui.Content)
	}
	if resp.Role != RoleUser {
		t.Errorf("response Role = %q, want %q", resp.Role, RoleUser)
	}
	if resp.Content != submitAnswersJSON {
		t.Errorf("response Content = %q, want byte-identical %q", resp.Content, submitAnswersJSON)
	}
	if resp.CPNID != c.ID {
		t.Errorf("response CPNID = %q, want %q", resp.CPNID, c.ID)
	}
	if resp.CPNRole != c.Role {
		t.Errorf("response CPNRole = %q, want %q", resp.CPNRole, c.Role)
	}
	if resp.ParentMessageID != a2ui.ID {
		t.Errorf("response ParentMessageID = %q, want %q (the A2UI row's ID)", resp.ParentMessageID, a2ui.ID)
	}
}

func TestFireHITL_AppendsResponseToHistory_OnSubmit_RawDepositPath(t *testing.T) {
	c, ch := hitlA2UITestCPN(defaultA2UIBuilder())
	ch <- submitToken(submitAnswersJSON)

	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c.mu.RLock()
	hist := append([]*Message(nil), c.History...)
	c.mu.RUnlock()

	if len(hist) != 2 {
		t.Fatalf("expected 2 history rows, got %d: %+v", len(hist), hist)
	}
	resp := hist[1]
	if resp.Role != RoleUser || resp.Content != submitAnswersJSON {
		t.Errorf("raw-deposit branch did not append RoleUser response row: %+v", resp)
	}
	if resp.ParentMessageID != hist[0].ID {
		t.Errorf("ParentMessageID = %q, want %q", resp.ParentMessageID, hist[0].ID)
	}
}

func TestFireHITL_AppendsResponseToHistory_OnRevise_WrapsAsCanonicalJSON(t *testing.T) {
	c, ch := hitlA2UITestCPN(defaultA2UIBuilder())
	ch <- reviseToken("tighten the budget section")

	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c.mu.RLock()
	hist := append([]*Message(nil), c.History...)
	c.mu.RUnlock()

	if len(hist) != 2 {
		t.Fatalf("expected 2 history rows, got %d", len(hist))
	}
	resp := hist[1]
	wantContent := `{"action":"revise","content":"tighten the budget section"}`
	if resp.Content != wantContent {
		t.Errorf("revise content wrap mismatch:\n got  %q\n want %q", resp.Content, wantContent)
	}
}

func TestFireHITL_AppendsResponseToHistory_OnApprove_WithEmptyContent(t *testing.T) {
	c, ch := hitlA2UITestCPN(defaultA2UIBuilder())
	ch <- approveToken() // Action=approve, Content=""

	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c.mu.RLock()
	hist := append([]*Message(nil), c.History...)
	c.mu.RUnlock()

	if len(hist) != 2 {
		t.Fatalf("expected 2 history rows, got %d", len(hist))
	}
	resp := hist[1]
	if resp.Content != `{"action":"approve"}` {
		t.Errorf("approve content = %q, want %q", resp.Content, `{"action":"approve"}`)
	}
}

func TestFireHITL_DoesNotAppend_OnReject(t *testing.T) {
	c, ch := hitlA2UITestCPN(defaultA2UIBuilder())
	ch <- rejectToken()

	err := c.Run(context.Background())
	if !errors.Is(err, ErrHITLRejected) {
		t.Fatalf("expected ErrHITLRejected, got %v", err)
	}

	c.mu.RLock()
	hist := append([]*Message(nil), c.History...)
	c.mu.RUnlock()

	// Only the A2UI surface row should be present (REQ-003).
	if len(hist) != 1 {
		t.Fatalf("expected exactly 1 history row (the A2UI surface), got %d: %+v", len(hist), hist)
	}
	if hist[0].Role != RoleAssistant {
		t.Errorf("only row should be the assistant A2UI; got role=%q", hist[0].Role)
	}
}

func TestFireHITL_DoesNotAppend_OnContextCancellation(t *testing.T) {
	c, _ := hitlA2UITestCPN(defaultA2UIBuilder()) // channel never receives
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_ = c.Run(ctx) // expect ErrTimeout

	c.mu.RLock()
	hist := append([]*Message(nil), c.History...)
	c.mu.RUnlock()

	// Only the A2UI surface row; no response row (REQ-004).
	for _, m := range hist {
		if m.Role == RoleUser {
			t.Errorf("unexpected RoleUser row appended on context cancellation: %+v", m)
		}
	}
}

func TestFireHITL_DoesNotAppend_OnFailedOutputBuilder(t *testing.T) {
	ch := make(chan Token, 1)
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorString, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "request"})
	transitions := map[string]*Transition{
		"T:HITL": {
			ID: "T:HITL", Kind: NodeKindHITL,
			InputPlaces: []string{"P:IN"}, OutputPlaces: []string{"P:OUT"},
			HITLConfig: &HITLConfig{
				Channel:            ch,
				Prompt:             "Please respond",
				A2UIPayloadBuilder: defaultA2UIBuilder(),
				OutputBuilder: func(consumed []Token, resp HITLResponse) (Token, error) {
					return Token{}, errors.New("intentional builder failure")
				},
			},
		},
	}
	c := NewCPN("test-cpn", "worker", 0, ModeMAS, "sess-1", places, transitions)
	ch <- submitToken(submitAnswersJSON)

	_ = c.Run(context.Background()) // expect failure

	c.mu.RLock()
	hist := append([]*Message(nil), c.History...)
	c.mu.RUnlock()

	for _, m := range hist {
		if m.Role == RoleUser {
			t.Errorf("REQ-005: no RoleUser append when OutputBuilder fails; got: %+v", m)
		}
	}
}

// ── t-review flavored persistence (raw-deposit branch) ──────────────────────
// spec-process-bugfix-treview-surface-and-locked-parser.md REQ-BE-001..006,
// INV-002/003. t-review has no OutputBuilder so fireHITL takes the raw-deposit
// path. With a transition-owned A2UIPayloadBuilder attached (REQ-BE-002), the
// surface row is appended to c.History and the response row (for approve /
// revise; never for reject) follows. Reject MUST leave the surface in place
// but MUST NOT append a response row.

// tReviewPayloadBuilder is a miniature stand-in for
// cmd/server/topologies.go::buildReviewA2UIPayload. The cpn package cannot
// import cmd/server (CON-005), so the shape is replicated here.
func tReviewPayloadBuilder() func(consumed []Token) (any, error) {
	return func(_ []Token) (any, error) {
		return map[string]any{
			"components": []any{
				map[string]any{
					"type":  "card",
					"props": map[string]any{"title": "Review Required"},
				},
				map[string]any{"type": "button", "props": map[string]any{
					"label": "Approve", "actionType": "hitl:approve", "id": "t-review",
				}},
				map[string]any{"type": "button", "props": map[string]any{
					"label": "Revise", "actionType": "hitl:revise", "id": "t-review",
				}},
				map[string]any{"type": "button", "props": map[string]any{
					"label": "Reject", "actionType": "hitl:reject", "id": "t-review",
				}},
			},
		}, nil
	}
}

func TestFireHITL_TReview_Approve_PersistsSurfaceAndResponse(t *testing.T) {
	c, ch := hitlA2UITestCPN(tReviewPayloadBuilder())
	ch <- approveToken()

	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c.mu.RLock()
	hist := append([]*Message(nil), c.History...)
	c.mu.RUnlock()

	if len(hist) != 2 {
		t.Fatalf("expected 2 history rows (surface + response), got %d: %+v", len(hist), hist)
	}
	a2ui, resp := hist[0], hist[1]
	if a2ui.Role != RoleAssistant || !strings.HasPrefix(a2ui.Content, A2UIMarker) {
		t.Errorf("surface row malformed: role=%q content=%q", a2ui.Role, a2ui.Content)
	}
	if !strings.Contains(a2ui.Content, "hitl:approve") ||
		!strings.Contains(a2ui.Content, "hitl:revise") ||
		!strings.Contains(a2ui.Content, "hitl:reject") {
		t.Errorf("surface MUST include all three action buttons; got %q", a2ui.Content)
	}
	if resp.Role != RoleUser {
		t.Errorf("response Role = %q, want %q", resp.Role, RoleUser)
	}
	if resp.Content != `{"action":"approve"}` {
		t.Errorf("approve content = %q, want %q", resp.Content, `{"action":"approve"}`)
	}
	if resp.ParentMessageID != a2ui.ID {
		t.Errorf("ParentMessageID = %q, want %q (surface row ID)", resp.ParentMessageID, a2ui.ID)
	}
}

func TestFireHITL_TReview_Revise_WrapsCanonically(t *testing.T) {
	c, ch := hitlA2UITestCPN(tReviewPayloadBuilder())
	ch <- reviseToken("tighten the budget")

	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c.mu.RLock()
	hist := append([]*Message(nil), c.History...)
	c.mu.RUnlock()

	if len(hist) != 2 {
		t.Fatalf("expected 2 history rows, got %d", len(hist))
	}
	resp := hist[1]
	want := `{"action":"revise","content":"tighten the budget"}`
	if resp.Content != want {
		t.Errorf("revise content = %q, want %q", resp.Content, want)
	}
	if resp.ParentMessageID != hist[0].ID {
		t.Errorf("ParentMessageID = %q, want %q", resp.ParentMessageID, hist[0].ID)
	}
}

func TestFireHITL_TReview_Reject_PersistsSurfaceButNotResponse(t *testing.T) {
	c, ch := hitlA2UITestCPN(tReviewPayloadBuilder())
	ch <- rejectToken()

	err := c.Run(context.Background())
	if !errors.Is(err, ErrHITLRejected) {
		t.Fatalf("expected ErrHITLRejected, got %v", err)
	}

	c.mu.RLock()
	hist := append([]*Message(nil), c.History...)
	c.mu.RUnlock()

	if len(hist) != 1 {
		t.Fatalf("expected exactly 1 history row (surface only), got %d: %+v", len(hist), hist)
	}
	if hist[0].Role != RoleAssistant || !strings.HasPrefix(hist[0].Content, A2UIMarker) {
		t.Errorf("expected surface row, got role=%q content=%q", hist[0].Role, hist[0].Content)
	}
	for _, m := range hist {
		if m.Role == RoleUser {
			t.Errorf("REQ-003: reject MUST NOT append RoleUser row; got %+v", m)
		}
	}
}
