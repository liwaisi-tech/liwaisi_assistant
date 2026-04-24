package cpn

import (
	"context"
	"strings"
	"testing"
)

// fakeSubAgentHook lets tests assert hook invocation order and inject
// Validate/Gate failures.
type fakeSubAgentHook struct {
	validateErr error
	gateErr     error
	calls       []string
}

func (h *fakeSubAgentHook) Validate(_ *Transition, _ []byte) error {
	h.calls = append(h.calls, "validate")
	return h.validateErr
}

func (h *fakeSubAgentHook) Gate(_ *Transition, _ []byte) error {
	h.calls = append(h.calls, "gate")
	return h.gateErr
}

func newSubAgentLLMTransition() *Transition {
	t := newBasicLLMTransition()
	t.Meta = map[string]string{
		"kind":       "subagent",
		"profile_id": "qa",
		"action_id":  "review-spec",
	}
	t.ErrorPlace = "P:ERROR"
	t.LLMConfig.RequireJSON = true
	return t
}

func newSubAgentTestCPN(mock *mockLLMClient, hook SubAgentOutputHook) (*CPN, *Transition) {
	tr := newSubAgentLLMTransition()
	c := newTestCPNForLLM(mock, map[string]*Transition{tr.ID: tr})
	// Override OUTPUT color to JSON since RequireJSON=true.
	c.Places["P:OUTPUT"].Color = ColorJSON
	c.Places["P:ERROR"] = &Place{ID: "P:ERROR", Space: SpaceComputation, Color: ColorError}
	c.SubAgentHook = hook
	return c, tr
}

// TestFireLLM_SubAgentHook_HappyPath: hook is called with Validate then
// Gate, and the token deposits to OUTPUT when both pass.
func TestFireLLM_SubAgentHook_HappyPath(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *LLMRequest) (LLMResponse, error) {
			return LLMResponse{Content: `{"role":"qa","approved":true,"blocking_issues":[],"suggestions":[]}`}, nil
		},
	}
	hook := &fakeSubAgentHook{}
	c, tr := newSubAgentTestCPN(mock, hook)

	_, _, err := fireLLM(context.Background(), tr, c, []Token{{Color: ColorString, Payload: "spec"}})
	if err != nil {
		t.Fatalf("fireLLM: %v", err)
	}
	if got := strings.Join(hook.calls, ","); got != "validate,gate" {
		t.Errorf("hook call order = %q, want validate,gate", got)
	}
	out, ok := c.Places["P:OUTPUT"].Peek()
	if !ok || len(out) != 1 {
		t.Fatalf("expected one OUTPUT token, got %v", out)
	}
	if errToks, _ := c.Places["P:ERROR"].Peek(); len(errToks) != 0 {
		t.Errorf("ERROR place should be empty on happy path, got %d tokens", len(errToks))
	}
}

// TestFireLLM_SubAgentHook_ValidateFail: Validate failure routes to
// ErrorPlace with a structured error payload, OUTPUT stays empty,
// fireLLM returns nil (not an error) so the executor keeps running.
func TestFireLLM_SubAgentHook_ValidateFail(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *LLMRequest) (LLMResponse, error) { return LLMResponse{Content: `{"junk":1}`}, nil },
	}
	hook := &fakeSubAgentHook{validateErr: errBoom("missing required: role")}
	c, tr := newSubAgentTestCPN(mock, hook)

	_, _, err := fireLLM(context.Background(), tr, c, []Token{{Color: ColorString, Payload: "spec"}})
	if err != nil {
		t.Fatalf("fireLLM should swallow validate failure when ErrorPlace is set, got %v", err)
	}
	// Gate must NOT have been called after Validate failed.
	if got := strings.Join(hook.calls, ","); got != "validate" {
		t.Errorf("hook calls = %q, want validate only", got)
	}
	if out, _ := c.Places["P:OUTPUT"].Peek(); len(out) != 0 {
		t.Errorf("OUTPUT should be empty on validate fail, got %d tokens", len(out))
	}
	errToks, _ := c.Places["P:ERROR"].Peek()
	if len(errToks) != 1 {
		t.Fatalf("ERROR should have 1 token, got %d", len(errToks))
	}
	payload := errToks[0].Payload.(string)
	for _, want := range []string{`"transition_id":"llm-classify"`, `"profile_id":"qa"`, `"action_id":"review-spec"`, `"stage":"validate"`} {
		if !strings.Contains(payload, want) {
			t.Errorf("error payload missing %q; got %s", want, payload)
		}
	}
}

// TestFireLLM_SubAgentHook_GateFail: Gate failure routes the same way.
func TestFireLLM_SubAgentHook_GateFail(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *LLMRequest) (LLMResponse, error) { return LLMResponse{Content: `{"role":"go-eng","approved":false,"findings":[],"suggested_patches":[{"path":"x","diff":"+os.Setenv(\"x\",\"y\")"}]}`}, nil },
	}
	hook := &fakeSubAgentHook{gateErr: errBoom("forbidden pattern: os.Setenv")}
	c, tr := newSubAgentTestCPN(mock, hook)

	_, _, err := fireLLM(context.Background(), tr, c, []Token{{Color: ColorString, Payload: "spec"}})
	if err != nil {
		t.Fatalf("fireLLM should swallow gate failure when ErrorPlace is set, got %v", err)
	}
	if got := strings.Join(hook.calls, ","); got != "validate,gate" {
		t.Errorf("hook calls = %q, want validate,gate", got)
	}
	if out, _ := c.Places["P:OUTPUT"].Peek(); len(out) != 0 {
		t.Errorf("OUTPUT should be empty on gate fail, got %d tokens", len(out))
	}
	errToks, _ := c.Places["P:ERROR"].Peek()
	if len(errToks) != 1 {
		t.Fatalf("ERROR should have 1 token, got %d", len(errToks))
	}
	if !strings.Contains(errToks[0].Payload.(string), `"stage":"gate"`) {
		t.Errorf("expected stage=gate in error payload")
	}
}

// TestFireLLM_SubAgentHook_NotASubAgent: hook is skipped entirely for
// transitions whose Meta.kind != "subagent".
func TestFireLLM_SubAgentHook_NotASubAgent(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *LLMRequest) (LLMResponse, error) { return LLMResponse{Content: `{"x":1}`}, nil },
	}
	hook := &fakeSubAgentHook{validateErr: errBoom("would fire if called")}
	c, tr := newSubAgentTestCPN(mock, hook)
	tr.Meta = nil // not a sub-agent

	_, _, err := fireLLM(context.Background(), tr, c, []Token{{Color: ColorString, Payload: "spec"}})
	if err != nil {
		t.Fatalf("fireLLM: %v", err)
	}
	if len(hook.calls) != 0 {
		t.Errorf("hook should not have been called on non-subagent transition, calls=%v", hook.calls)
	}
}

// TestFireLLM_SubAgentEvents asserts started+finished events are
// emitted in order, sharing the same firing_id, with OK=true on
// success and OK=false plus stage on validate/gate failure.
func TestFireLLM_SubAgentEvents_HappyPath(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *LLMRequest) (LLMResponse, error) {
			return LLMResponse{Content: `{"role":"qa","approved":true,"blocking_issues":[],"suggestions":[]}`}, nil
		},
	}
	hook := &fakeSubAgentHook{}
	c, tr := newSubAgentTestCPN(mock, hook)

	var events []Event
	c.EventSink = func(e *Event) { events = append(events, *e) }

	if _, _, err := fireLLM(context.Background(), tr, c, []Token{{Color: ColorString, Payload: "spec"}}); err != nil {
		t.Fatalf("fireLLM: %v", err)
	}
	var started, finished *Event
	for i := range events {
		switch events[i].Type {
		case EventSubAgentStarted:
			started = &events[i]
		case EventSubAgentFinished:
			finished = &events[i]
		}
	}
	if started == nil || finished == nil {
		t.Fatalf("expected both started and finished events, got %d events", len(events))
	}
	sp := started.Payload.(SubAgentStartedPayload)
	fp := finished.Payload.(SubAgentFinishedPayload)
	if sp.FiringID == "" || sp.FiringID != fp.FiringID {
		t.Errorf("firing_id mismatch: started=%q finished=%q", sp.FiringID, fp.FiringID)
	}
	if !fp.OK {
		t.Errorf("finished.OK should be true on happy path")
	}
	if sp.ProfileID != "qa" || fp.ProfileID != "qa" {
		t.Errorf("profile mismatch")
	}
	if fp.Stage != "" || fp.Error != "" {
		t.Errorf("Stage/Error should be empty on success, got stage=%q err=%q", fp.Stage, fp.Error)
	}
}

func TestFireLLM_SubAgentEvents_ValidateFail(t *testing.T) {
	mock := &mockLLMClient{
		completeFunc: func(_ context.Context, _ *LLMRequest) (LLMResponse, error) {
			return LLMResponse{Content: `{"junk":1}`}, nil
		},
	}
	hook := &fakeSubAgentHook{validateErr: errBoom("missing field")}
	c, tr := newSubAgentTestCPN(mock, hook)

	var events []Event
	c.EventSink = func(e *Event) { events = append(events, *e) }

	if _, _, err := fireLLM(context.Background(), tr, c, []Token{{Color: ColorString, Payload: "spec"}}); err != nil {
		t.Fatalf("fireLLM should swallow validate fail: %v", err)
	}
	var fp *SubAgentFinishedPayload
	for i := range events {
		if events[i].Type == EventSubAgentFinished {
			p := events[i].Payload.(SubAgentFinishedPayload)
			fp = &p
		}
	}
	if fp == nil {
		t.Fatal("expected subagent_finished event")
	}
	if fp.OK {
		t.Error("OK should be false on validate failure")
	}
	if fp.Stage != "validate" {
		t.Errorf("stage = %q, want validate", fp.Stage)
	}
	if !strings.Contains(fp.Error, "missing field") {
		t.Errorf("error should include cause: %q", fp.Error)
	}
}

type errBoomT string

func (e errBoomT) Error() string { return string(e) }

func errBoom(s string) error { return errBoomT(s) }
