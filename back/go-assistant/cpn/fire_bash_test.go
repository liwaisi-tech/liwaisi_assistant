package cpn

import (
	"context"
	"errors"
	"testing"
)

// fakeHITLHandler records the invocation and returns a canned error.
type fakeHITLHandler struct {
	called   int
	lastOp   GateOp
	lastErr  error
	response error
}

func (f *fakeHITLHandler) HandleHITL(_ context.Context, _ *Transition, _ *CPN, op GateOp, hitlErr error) error {
	f.called++
	f.lastOp = op
	f.lastErr = hitlErr
	return f.response
}

// requiresHITLErr mimics the opaque error returned by the gate when a
// require-HITL decision is reached. The cpn package does not import the
// gate package so the struct stays local to the test.
type requiresHITLErr struct{ msg string }

func (e *requiresHITLErr) Error() string { return e.msg }

// gateEmittingHITL returns the sentinel once per Check call.
type gateEmittingHITL struct{ err error }

func (g gateEmittingHITL) Check(_ context.Context, _ GateOp) error { return g.err }

func TestFireBash_GateRequiresHITL_ApproveFallsThroughToExec(t *testing.T) {
	adapter := &mockHostAdapter{
		execResult: ExecResult{ExitCode: 0, Stdout: []byte("ok\n")},
	}
	handler := &fakeHITLHandler{response: nil} // approve
	rt := &HostRuntime{
		Adapter:     adapter,
		Gate:        gateEmittingHITL{err: &requiresHITLErr{msg: "requires HITL"}},
		HITLHandler: handler,
	}
	c, tr := buildBashCPN(t, &BashConfig{Command: "uname"}, rt)

	_, _, err := fireBash(context.Background(), tr, c, nil)
	if err != nil {
		t.Fatalf("fireBash: %v", err)
	}
	if handler.called != 1 {
		t.Fatalf("handler.called = %d, want 1", handler.called)
	}
	if adapter.calls != 1 {
		t.Fatalf("adapter.calls = %d, want 1 (Exec should fire after approve)", adapter.calls)
	}
	if handler.lastOp.Command != "uname" {
		t.Fatalf("lastOp.Command = %q, want uname", handler.lastOp.Command)
	}
}

func TestFireBash_GateRequiresHITL_DenyReturnsHostError(t *testing.T) {
	adapter := &mockHostAdapter{}
	denied := NewHostError(HostErrCodeGateDenied, "denied by HITL", nil)
	handler := &fakeHITLHandler{response: denied}
	rt := &HostRuntime{
		Adapter:     adapter,
		Gate:        gateEmittingHITL{err: &requiresHITLErr{msg: "requires HITL"}},
		HITLHandler: handler,
	}
	c, tr := buildBashCPN(t, &BashConfig{Command: "rm"}, rt)

	_, _, err := fireBash(context.Background(), tr, c, nil)
	if err == nil {
		t.Fatalf("expected deny error, got nil")
	}
	if !errors.Is(err, ErrGateDenied) {
		t.Fatalf("err %v, want ErrGateDenied", err)
	}
	if adapter.calls != 0 {
		t.Fatalf("adapter.calls = %d, want 0 on deny", adapter.calls)
	}
}

func TestFireBash_GateNonHITLError_PropagatesUnchanged(t *testing.T) {
	adapter := &mockHostAdapter{}
	rawErr := errors.New("network down")
	handler := &fakeHITLHandler{response: rawErr} // handler returns orig err for non-HITL
	rt := &HostRuntime{
		Adapter:     adapter,
		Gate:        gateEmittingHITL{err: rawErr},
		HITLHandler: handler,
	}
	c, tr := buildBashCPN(t, &BashConfig{Command: "ls"}, rt)

	_, _, err := fireBash(context.Background(), tr, c, nil)
	if err == nil || err.Error() != "network down" {
		t.Fatalf("err = %v, want network down", err)
	}
}

func TestPublishHostApprovalSurface_EmitsStreamChunkWithMarker(t *testing.T) {
	events := make(chan Event, 4)
	c := NewCPN("cpn-1", "test", 0, ModeMAS, "s1",
		map[string]*Place{}, map[string]*Transition{})
	c.EventEmitter = events

	const body = A2UIMarker + `{"schema":"host.approval"}`
	c.PublishHostApprovalSurface("t-bash", body)

	select {
	case e := <-events:
		if e.Type != EventStreamChunk {
			t.Fatalf("type = %s, want stream_chunk", e.Type)
		}
		if e.TransitionID != "t-bash" {
			t.Fatalf("transition id = %s", e.TransitionID)
		}
		chunk, ok := e.Payload.(StreamChunk)
		if !ok {
			t.Fatalf("payload type = %T", e.Payload)
		}
		if chunk.Content != body {
			t.Fatalf("content = %q, want %q", chunk.Content, body)
		}
		if !chunk.Done {
			t.Fatalf("chunk.Done = false, want true")
		}
	default:
		t.Fatalf("no event emitted")
	}
}

func TestPublishHITLRequested_EmitsTypedPayload(t *testing.T) {
	events := make(chan Event, 4)
	c := NewCPN("cpn-1", "test", 0, ModeMAS, "s1",
		map[string]*Place{}, map[string]*Transition{})
	c.EventEmitter = events

	c.PublishHITLRequested("t-bash", HITLRequestedPayload{Prompt: "approve?", CustomSurface: true})

	e := <-events
	if e.Type != EventHITLRequested {
		t.Fatalf("type = %s, want hitl_requested", e.Type)
	}
	p, ok := e.Payload.(HITLRequestedPayload)
	if !ok || !p.CustomSurface {
		t.Fatalf("payload = %+v", e.Payload)
	}
}

func TestPublishHostApproval_NilReceiver_NoPanic(t *testing.T) {
	var c *CPN
	c.PublishHostApprovalSurface("t", "x")
	c.PublishHITLRequested("t", HITLRequestedPayload{})
}

func TestFireBash_GateHITLError_NilHandler_ReturnsRawError(t *testing.T) {
	adapter := &mockHostAdapter{}
	raw := &requiresHITLErr{msg: "requires HITL"}
	rt := &HostRuntime{
		Adapter: adapter,
		Gate:    gateEmittingHITL{err: raw},
	}
	c, tr := buildBashCPN(t, &BashConfig{Command: "uname"}, rt)

	_, _, err := fireBash(context.Background(), tr, c, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	if err.Error() != "requires HITL" {
		t.Fatalf("err = %v, want requires HITL", err)
	}
	if adapter.calls != 0 {
		t.Fatalf("adapter called unexpectedly: %d", adapter.calls)
	}
}
