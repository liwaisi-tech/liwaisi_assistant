package cpn

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ── Revision Test Helpers ───────────────────────────────────────────────────

// sequentialMockLLM returns a mockLLMClient that returns responses in order.
func sequentialMockLLM(responses []string) *mockLLMClient {
	var idx atomic.Int32
	return &mockLLMClient{
		completeFunc: func(_ context.Context, _ *LLMRequest) (LLMResponse, error) {
			i := int(idx.Add(1) - 1)
			if i >= len(responses) {
				return LLMResponse{}, fmt.Errorf("no more mock responses (call %d)", i)
			}
			return LLMResponse{Content: responses[i]}, nil
		},
	}
}

// revisionTestCPN creates a CPN with a revision-loop HITL transition
// and a correction LLM transition, with a mock LLMClient.
func revisionTestCPN(maxRevisions int, mockResponses []string) (*CPN, chan Token) {
	ch := make(chan Token, 10)
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorHuman, SpaceSurface),
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "request"})

	transitions := map[string]*Transition{
		"T:HITL_REV": {
			ID: "T:HITL_REV", Kind: NodeKindHITL,
			InputPlaces: []string{"P:IN"}, OutputPlaces: []string{"P:OUT"},
			HITLConfig: &HITLConfig{
				Channel:         ch,
				Prompt:          "Review this draft:",
				RevisionLoop:    true,
				CorrectionLLMID: "T:CORR_LLM",
				MaxRevisions:    maxRevisions,
			},
		},
		"T:CORR_LLM": {
			ID: "T:CORR_LLM", Kind: NodeKindLLM,
			SystemPrompt: "You are a revision assistant.",
			LLMConfig: &LLMConfig{
				Model:     "test-model",
				MaxTokens: 512,
			},
		},
	}

	var mock *mockLLMClient
	if len(mockResponses) > 0 {
		mock = sequentialMockLLM(mockResponses)
	} else {
		mock = &mockLLMClient{}
	}
	cpn := NewCPN("test-rev-cpn", "worker", 0, ModeMAS, "sess-rev", places, transitions)
	cpn.LLMClient = mock
	return cpn, ch
}

// reviseToken returns a ColorHuman revise token with feedback.
func reviseToken(feedback string) Token {
	return Token{
		Color:   ColorHuman,
		Space:   SpaceSurface,
		Payload: HITLResponse{Action: HITLRevise, Content: feedback},
	}
}

// rejectToken returns a ColorHuman reject token.
func rejectToken() Token {
	return Token{
		Color:   ColorHuman,
		Space:   SpaceSurface,
		Payload: HITLResponse{Action: HITLReject},
	}
}

// revApproveToken returns a ColorHuman approval token for revision tests.
func revApproveToken() Token {
	return Token{
		Color:   ColorHuman,
		Space:   SpaceSurface,
		Payload: HITLResponse{Action: HITLApprove, Content: "looks good"},
	}
}

// ── Happy Path ──────────────────────────────────────────────────────────────

func TestFireHITLWithRevision_ApproveFirstRound(t *testing.T) {
	c, ch := revisionTestCPN(3, nil)
	ch <- revApproveToken()

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if c.getState() != StateCompleted {
		t.Errorf("expected StateCompleted, got %s", c.getState())
	}
	if c.Places["P:OUT"].Len() != 1 {
		t.Errorf("expected 1 token in P:OUT, got %d", c.Places["P:OUT"].Len())
	}
}

func TestFireHITLWithRevision_ReviseOnceApprove(t *testing.T) {
	c, ch := revisionTestCPN(3, []string{"revised draft v1"})

	go func() {
		// Wait for first HITL request, then revise.
		time.Sleep(20 * time.Millisecond)
		ch <- reviseToken("make it shorter")
		// Wait for correction LLM, then approve.
		time.Sleep(50 * time.Millisecond)
		ch <- revApproveToken()
	}()

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if c.Places["P:OUT"].Len() != 1 {
		t.Errorf("expected 1 token in P:OUT, got %d", c.Places["P:OUT"].Len())
	}
	// Verify the mock was called once for the correction.
	mock := c.LLMClient.(*mockLLMClient)
	if len(mock.getCalls()) != 1 {
		t.Errorf("expected 1 LLM call, got %d", len(mock.getCalls()))
	}
}

func TestFireHITLWithRevision_ReviseTwiceApprove(t *testing.T) {
	c, ch := revisionTestCPN(3, []string{"revised v1", "revised v2"})

	go func() {
		time.Sleep(20 * time.Millisecond)
		ch <- reviseToken("fix typo")
		time.Sleep(50 * time.Millisecond)
		ch <- reviseToken("also shorten it")
		time.Sleep(50 * time.Millisecond)
		ch <- revApproveToken()
	}()

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	mock := c.LLMClient.(*mockLLMClient)
	if len(mock.getCalls()) != 2 {
		t.Errorf("expected 2 LLM calls, got %d", len(mock.getCalls()))
	}
}

// ── Rejection ───────────────────────────────────────────────────────────────

func TestFireHITLWithRevision_Reject(t *testing.T) {
	c, ch := revisionTestCPN(3, nil)
	ch <- rejectToken()

	err := c.Run(context.Background())
	if !errors.Is(err, ErrHITLRejected) {
		t.Fatalf("expected ErrHITLRejected, got %v", err)
	}
}

func TestFireHITLWithRevision_RejectAfterRevisions(t *testing.T) {
	c, ch := revisionTestCPN(3, []string{"rev1"})

	go func() {
		time.Sleep(20 * time.Millisecond)
		ch <- reviseToken("fix it")
		time.Sleep(50 * time.Millisecond)
		ch <- rejectToken()
	}()

	err := c.Run(context.Background())
	if !errors.Is(err, ErrHITLRejected) {
		t.Fatalf("expected ErrHITLRejected after revision, got %v", err)
	}
}

// ── Max Revisions ───────────────────────────────────────────────────────────

func TestFireHITLWithRevision_MaxRevisions(t *testing.T) {
	// MaxRevisions=2, send 3 revisions: third should trigger ErrHITLMaxRevisions.
	c, ch := revisionTestCPN(2, []string{"rev1", "rev2"})

	go func() {
		time.Sleep(20 * time.Millisecond)
		ch <- reviseToken("change 1")
		time.Sleep(50 * time.Millisecond)
		ch <- reviseToken("change 2")
		time.Sleep(50 * time.Millisecond)
		ch <- reviseToken("change 3") // This one exceeds the cap.
	}()

	err := c.Run(context.Background())
	if !errors.Is(err, ErrHITLMaxRevisions) {
		t.Fatalf("expected ErrHITLMaxRevisions, got %v", err)
	}
}

func TestFireHITLWithRevision_MaxRevisionsZero(t *testing.T) {
	// MaxRevisions=0, first Revise should immediately return ErrHITLMaxRevisions.
	c, ch := revisionTestCPN(0, nil)
	ch <- reviseToken("any feedback")

	err := c.Run(context.Background())
	if !errors.Is(err, ErrHITLMaxRevisions) {
		t.Fatalf("expected ErrHITLMaxRevisions, got %v", err)
	}
}

// ── Context Timeout ─────────────────────────────────────────────────────────

func TestFireHITLWithRevision_ContextTimeout(t *testing.T) {
	c, _ := revisionTestCPN(3, nil)
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

// ── Defensive Payload Handling ──────────────────────────────────────────────

func TestFireHITLWithRevision_InvalidPayloadType(t *testing.T) {
	c, ch := revisionTestCPN(3, nil)
	// Send an integer payload (not HITLResponse or string).
	ch <- Token{Color: ColorHuman, Space: SpaceSurface, Payload: 42}

	err := c.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid payload type, got nil")
	}
	if errors.Is(err, ErrHITLRejected) || errors.Is(err, ErrHITLMaxRevisions) {
		t.Fatalf("wrong error type: %v", err)
	}
}

func TestFireHITLWithRevision_StringPayloadAsApprove(t *testing.T) {
	c, ch := revisionTestCPN(3, nil)
	// Bare string payload treated as approve (backward compat).
	ch <- Token{Color: ColorHuman, Space: SpaceSurface, Payload: "approved text"}

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error for string payload, got %v", err)
	}
	if c.Places["P:OUT"].Len() != 1 {
		t.Errorf("expected 1 token in P:OUT, got %d", c.Places["P:OUT"].Len())
	}
}

// ── Missing / Empty Correction Transition ────────────────────────────────────

func TestFireHITLWithRevision_EmptyCorrectionLLMID(t *testing.T) {
	c, ch := revisionTestCPN(3, nil)
	c.Transitions["T:HITL_REV"].HITLConfig.CorrectionLLMID = ""

	go func() {
		time.Sleep(20 * time.Millisecond)
		ch <- reviseToken("fix it")
	}()

	err := c.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for empty CorrectionLLMID, got nil")
	}
}

func TestFireHITLWithRevision_MissingCorrectionTransition(t *testing.T) {
	c, ch := revisionTestCPN(3, []string{"shouldn't be called"})
	// Remove the correction transition.
	delete(c.Transitions, "T:CORR_LLM")

	go func() {
		time.Sleep(20 * time.Millisecond)
		ch <- reviseToken("fix it")
	}()

	err := c.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for missing correction transition, got nil")
	}
	if !strings.Contains(err.Error(), "T:CORR_LLM") {
		t.Errorf("expected error to mention T:CORR_LLM, got: %v", err)
	}
}

// ── Correction LLM Error ────────────────────────────────────────────────────

func TestFireHITLWithRevision_CorrectionLLMError(t *testing.T) {
	c, ch := revisionTestCPN(3, nil)
	// Override with error-returning mock.
	c.LLMClient = &mockLLMClient{
		completeFunc: func(_ context.Context, _ *LLMRequest) (LLMResponse, error) {
			return LLMResponse{}, fmt.Errorf("LLM service unavailable")
		},
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		ch <- reviseToken("fix it")
	}()

	err := c.Run(context.Background())
	if err == nil {
		t.Fatal("expected error from LLM failure, got nil")
	}
	if !strings.Contains(err.Error(), "LLM service unavailable") {
		t.Errorf("expected LLM error message, got: %v", err)
	}
}

// ── Event Round Numbers ─────────────────────────────────────────────────────

func TestFireHITLWithRevision_EventRoundNumbers(t *testing.T) {
	c, ch := revisionTestCPN(3, []string{"revised draft"})
	ec := &eventCollector{}
	c.EventSink = ec.sink

	go func() {
		time.Sleep(20 * time.Millisecond)
		ch <- reviseToken("fix it")
		time.Sleep(50 * time.Millisecond)
		ch <- revApproveToken()
	}()

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	events := ec.getEvents()
	var rounds []int
	for _, e := range events {
		if e.Type == EventHITLRequested {
			if payload, ok := e.Payload.(map[string]any); ok {
				if r, ok := payload["round"].(int); ok {
					rounds = append(rounds, r)
				}
			}
		}
	}

	if len(rounds) != 2 {
		t.Fatalf("expected 2 HITL requested events, got %d", len(rounds))
	}
	if rounds[0] != 0 {
		t.Errorf("expected round 0, got %d", rounds[0])
	}
	if rounds[1] != 1 {
		t.Errorf("expected round 1, got %d", rounds[1])
	}
}

// ── State Transitions Per Round ─────────────────────────────────────────────

func TestFireHITLWithRevision_StateTransitionsPerRound(t *testing.T) {
	c, ch := revisionTestCPN(3, nil)

	// Track state transitions via events.
	var states []State
	var mu sync.Mutex
	origSink := c.EventSink
	c.EventSink = func(e *Event) {
		if origSink != nil {
			origSink(e)
		}
		if e.Type == EventHITLRequested {
			mu.Lock()
			states = append(states, c.getState())
			mu.Unlock()
		}
	}

	ch <- revApproveToken()

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// Should have recorded StateWaiting at each HITL requested event.
	if len(states) < 1 {
		t.Fatal("expected at least 1 state observation")
	}
	if states[0] != StateWaiting {
		t.Errorf("expected StateWaiting at round 0, got %s", states[0])
	}
}

// ── GroupNotifier Called Per Round ───────────────────────────────────────────

func TestFireHITLWithRevision_GroupNotifierCalledPerRound(t *testing.T) {
	c, ch := revisionTestCPN(3, []string{"revised"})
	gn := &mockGroupNotifier{}
	c.GroupNotifier = gn

	go func() {
		time.Sleep(20 * time.Millisecond)
		ch <- reviseToken("fix it")
		time.Sleep(50 * time.Millisecond)
		ch <- revApproveToken()
	}()

	err := c.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	calls := gn.getCalls()
	// 2 rounds: each round has Waiting + Running = 2 calls per round = 4 total.
	if len(calls) != 4 {
		t.Fatalf("expected 4 SwitchCMP calls (2 rounds x 2), got %d: %v", len(calls), calls)
	}
	for _, call := range calls {
		if call != "test-rev-cpn" {
			t.Errorf("expected cpnID=test-rev-cpn, got %s", call)
		}
	}
}

// ── Centaurian Mode Switch on Approve ───────────────────────────────────────

func TestFireHITLWithRevision_CentaurianOnApprove(t *testing.T) {
	ch := make(chan Token, 10)
	places := map[string]*Place{
		"P:IN":  NewPlace("P:IN", ColorString, SpaceSurface),
		"P:OUT": NewPlace("P:OUT", ColorHuman, SpaceComputation), // Computation space triggers Centaurian.
	}
	_ = places["P:IN"].Deposit(&Token{Color: ColorString, Space: SpaceSurface, Payload: "request"})
	transitions := map[string]*Transition{
		"T:HITL_REV": {
			ID: "T:HITL_REV", Kind: NodeKindHITL,
			InputPlaces: []string{"P:IN"}, OutputPlaces: []string{"P:OUT"},
			HITLConfig: &HITLConfig{
				Channel:         ch,
				Prompt:          "Review:",
				RevisionLoop:    true,
				CorrectionLLMID: "T:CORR_LLM",
				MaxRevisions:    3,
			},
		},
		"T:CORR_LLM": {
			ID: "T:CORR_LLM", Kind: NodeKindLLM,
			SystemPrompt: "Correct.",
			LLMConfig:    &LLMConfig{Model: "test", MaxTokens: 256},
		},
	}

	cpn := NewCPN("test-centaurian", "worker", 0, ModeMAS, "sess-c", places, transitions)
	cpn.LLMClient = &mockLLMClient{}
	ch <- revApproveToken()

	err := cpn.Run(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	cpn.mu.RLock()
	mode := cpn.Mode
	cpn.mu.RUnlock()
	if mode != ModeCentaurian {
		t.Errorf("expected ModeCentaurian, got %s", mode)
	}
}

// ── fireLLMDirect Tests ─────────────────────────────────────────────────────

func TestFireLLMDirect_Success(t *testing.T) {
	mock := sequentialMockLLM([]string{"corrected output"})
	places := map[string]*Place{}
	transitions := map[string]*Transition{
		"T:CORR": {
			ID: "T:CORR", Kind: NodeKindLLM,
			SystemPrompt: "Fix it.",
			LLMConfig:    &LLMConfig{Model: "test-model", MaxTokens: 512},
		},
	}
	c := NewCPN("test-direct", "worker", 0, ModeMAS, "sess-d", places, transitions)
	c.LLMClient = mock

	corrTransition := transitions["T:CORR"]
	input := Token{Color: ColorString, Payload: "bad content"}

	result, err := fireLLMDirect(context.Background(), corrTransition, c, &input)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != "corrected output" {
		t.Errorf("expected 'corrected output', got %q", result)
	}
}

func TestFireLLMDirect_Error(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *LLMRequest) (LLMResponse, error) {
			return LLMResponse{}, fmt.Errorf("connection refused")
		},
	}
	places := map[string]*Place{}
	transitions := map[string]*Transition{
		"T:CORR": {
			ID: "T:CORR", Kind: NodeKindLLM,
			SystemPrompt: "Fix it.",
			LLMConfig:    &LLMConfig{Model: "test-model", MaxTokens: 512},
		},
	}
	c := NewCPN("test-direct", "worker", 0, ModeMAS, "sess-d", places, transitions)
	c.LLMClient = mock

	corrTransition := transitions["T:CORR"]
	input := Token{Color: ColorString, Payload: "bad content"}

	_, err := fireLLMDirect(context.Background(), corrTransition, c, &input)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected connection refused error, got: %v", err)
	}
}

func TestFireLLMDirect_NilLLMClient(t *testing.T) {
	transitions := map[string]*Transition{
		"T:CORR": {
			ID: "T:CORR", Kind: NodeKindLLM,
			SystemPrompt: "Fix it.",
			LLMConfig:    &LLMConfig{Model: "test", MaxTokens: 512},
		},
	}
	c := NewCPN("test-nil", "worker", 0, ModeMAS, "sess-n", map[string]*Place{}, transitions)
	// LLMClient deliberately nil.

	input := Token{Color: ColorString, Payload: "bad"}
	_, err := fireLLMDirect(context.Background(), transitions["T:CORR"], c, &input)
	if err == nil {
		t.Fatal("expected error for nil LLMClient, got nil")
	}
	if !strings.Contains(err.Error(), "nil LLMClient") {
		t.Errorf("expected nil LLMClient error, got: %v", err)
	}
}

func TestFireLLMDirect_RequireJSON(t *testing.T) {
	mock := sequentialMockLLM([]string{`{"result":"ok"}`})
	transitions := map[string]*Transition{
		"T:CORR": {
			ID: "T:CORR", Kind: NodeKindLLM,
			SystemPrompt: "Fix JSON.",
			LLMConfig:    &LLMConfig{Model: "test", MaxTokens: 512, RequireJSON: true},
		},
	}
	c := NewCPN("test-json", "worker", 0, ModeMAS, "sess-j", map[string]*Place{}, transitions)
	c.LLMClient = mock

	input := Token{Color: ColorString, Payload: "bad json"}
	result, err := fireLLMDirect(context.Background(), transitions["T:CORR"], c, &input)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != `{"result":"ok"}` {
		t.Errorf("expected JSON result, got %q", result)
	}

	// Verify the request had ResponseFmt set.
	calls := mock.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].ResponseFmt != "json_object" {
		t.Errorf("expected ResponseFmt=json_object, got %q", calls[0].ResponseFmt)
	}
}

func TestFireLLMDirect_FallbackDefaults(t *testing.T) {
	mock := sequentialMockLLM([]string{"fixed"})
	transitions := map[string]*Transition{
		"T:CORR": {
			ID:   "T:CORR",
			Kind: NodeKindLLM,
			// No SystemPrompt, no LLMConfig — exercises all fallback paths.
		},
	}
	c := NewCPN("test-defaults", "worker", 0, ModeMAS, "sess-def", map[string]*Place{}, transitions)
	c.LLMClient = mock

	input := Token{Color: ColorString, Payload: "bad"}
	result, err := fireLLMDirect(context.Background(), transitions["T:CORR"], c, &input)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != "fixed" {
		t.Errorf("expected 'fixed', got %q", result)
	}

	// Verify fallback model and maxTokens.
	calls := mock.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Model != "structured" {
		t.Errorf("expected fallback model 'structured', got %q", calls[0].Model)
	}
	if calls[0].MaxTokens != 1024 {
		t.Errorf("expected fallback maxTokens 1024, got %d", calls[0].MaxTokens)
	}
}

// ── formatRevisionPrompt Test ───────────────────────────────────────────────

func TestFormatRevisionPrompt(t *testing.T) {
	result := formatRevisionPrompt("The revised draft text here")
	if !strings.Contains(result, "The revised draft text here") {
		t.Error("expected prompt to contain the revised text")
	}
	if !strings.Contains(result, "review") || !strings.Contains(result, "approve") {
		t.Error("expected prompt to contain review instructions")
	}
}
